package main

import (
	"expvar"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

var (
	host       = flag.String("h", "localhost:1883", "Broker hostname and port")
	concurency = flag.Uint("c", 1, "Concurrency - number of devices")
	qos        = flag.Uint("q", 0, "QoS: 0, 1 or 2")
	frequency  = flag.Duration("f", 1*time.Second, "Publish frequency")
	user       = flag.String("u", "", "Username")
	pass       = flag.String("P", "", "Password")
	topic      = flag.String("t", "", "Topic")
	message    = flag.String("m", `{"threadId":"%d"}`, "Message in JSON")
	timeout    = flag.Duration("w", 15*time.Second, "Wait timeout on connect and publish")
	verbose    = flag.Bool("v", false, "Print paho warning messages")
	expAddr    = flag.String("e", "", "expvar listening address (e.g. :8080)")

	// Metrics
	connectedDevices = new(expvar.Int)
	published        = new(expvar.Int) // rate
	errors           = new(expvar.Map) // rate
)

func publishRate() interface{} {
	defer published.Set(0)
	return published.Value()
}

func errorRate() interface{} {
	defer errors.Init()
	return errors.String()
}

func errorInc(err string) {
	if *verbose {
		log.Print(err)
	}
	errors.Add(err, 1)
}

// Print messages from the "errors" channel
func errorHandler(c mqtt.Client, m mqtt.Message) {
	log.Printf("msgid:%d %s\n", m.MessageID(), string(m.Payload()))
}

func buildClient() mqtt.Client {
	proto := "tcp://"
	if strings.HasSuffix(*host, ":8883") {
		proto = "ssl://"
	}

	opt := mqtt.NewClientOptions().
		AddBroker(proto + *host).
		SetAutoReconnect(false).
		SetDefaultPublishHandler(errorHandler).
		SetUsername(*user).
		SetPassword(*pass)
	client := mqtt.NewClient(opt)

	// Sleep some random time to prevent devices to connect all at the same time,
	// which leads to connection timeouts.
	maxDelay := int(*concurency * 20)
	time.Sleep(time.Duration(rand.Intn(maxDelay)) * time.Millisecond)

	token := client.Connect()
	if !token.WaitTimeout(*timeout) {
		errorInc("connection timeout")
	} else if err := token.Error(); err != nil {
		errorInc(err.Error())
	}

	return client
}

func publish(wg *sync.WaitGroup, stop *bool, id uint) {
	defer wg.Done()

	client := buildClient()
	msg := fmt.Sprintf(*message, id)

	for *stop == false {
		for !client.IsConnected() && !*stop {
			connectedDevices.Add(-1)
			client = buildClient()
		}
		connectedDevices.Add(1)
		if *verbose {
			log.Printf("device %d connected", id)
		}
		token := client.Publish(*topic, byte(*qos), false, msg)
		if !token.WaitTimeout(*timeout) {
			errorInc("publish timeout")
			client.Disconnect(0)
		}
		if token.Error() != nil {
			errorInc(token.Error().Error())
			client.Disconnect(0)
		}
		published.Add(1)
		time.Sleep(*frequency)
	}
}

func main() {
	flag.Parse()
	log.SetFlags(0)

	tracer := log.New(os.Stderr, "paho", 0)
	mqtt.CRITICAL = tracer
	mqtt.ERROR = tracer
	if *verbose {
		mqtt.WARN = tracer
	}

	// Metrics
	if *expAddr != "" {
		expvar.Publish("connected-devices", connectedDevices)
		expvar.Publish("published", expvar.Func(publishRate))
		expvar.Publish("errors", expvar.Func(errorRate))
		http.ListenAndServe(*expAddr, nil)
	}

	// Listen for errors
	client := buildClient()
	token := client.Subscribe("errors", 0, errorHandler)
	token.Wait()
	if err := token.Error(); err != nil {
		log.Fatal(err)
	}

	var wg sync.WaitGroup
	var stop bool

	log.Printf("Starting %d devices", *concurency)

	// Start the devices
	for i := uint(0); i < *concurency; i++ {
		wg.Add(1)
		go publish(&wg, &stop, i)
	}

	// Stop the devices on ctrl+c
	intchan := make(chan os.Signal, 1)
	signal.Notify(intchan, os.Interrupt)
	go func() {
		<-intchan
		stop = true
		println("")
	}()

	// Wait for all devices to be stopped
	wg.Wait()
	client.Unsubscribe("errors").WaitTimeout(*timeout)
}
