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
	"github.com/paulbellamy/ratecounter"
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
	connectedDevices     = new(expvar.Int)
	published            = ratecounter.NewRateCounter(time.Minute)
	cnxErrors            = ratecounter.NewRateCounter(time.Minute)
	cnxTimeoutErrors     = ratecounter.NewRateCounter(time.Minute)
	publishErrors        = ratecounter.NewRateCounter(time.Minute)
	publishTimeoutErrors = ratecounter.NewRateCounter(time.Minute)
)

// Print messages from the "errors" channel
func errorHandler(c mqtt.Client, m mqtt.Message) {
	log.Printf("msgid:%d %s\n", m.MessageID(), string(m.Payload()))
}

func buildClient(id uint) mqtt.Client {
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
		log.Printf("[dev %d] connection timeout\n", id)
		cnxTimeoutErrors.Incr(1)
	} else if err := token.Error(); err != nil {
		log.Printf("[dev %d] %s\n", id, err.Error())
		cnxErrors.Incr(1)
	} else {
		connectedDevices.Add(1)
		if *verbose {
			log.Printf("device %d connected\n", id)
		}
	}

	return client
}

func publish(wg *sync.WaitGroup, stop *bool, id uint) {
	defer wg.Done()

	client := buildClient(id)
	msg := fmt.Sprintf(*message, id)

	for *stop == false {
		for !client.IsConnected() && !*stop {
			connectedDevices.Add(-1)
			client = buildClient(id)
		}
		token := client.Publish(*topic, byte(*qos), false, msg)
		if !token.WaitTimeout(*timeout) {
			log.Printf("[dev %d] publish timeout\n", id)
			publishTimeoutErrors.Incr(1)
			client.Disconnect(0)
		}
		if err := token.Error(); err != nil {
			log.Printf("[dev %d] %s\n", id, err.Error())
			publishErrors.Incr(1)
			client.Disconnect(0)
		}
		published.Incr(1)
		time.Sleep(*frequency)
	}

	client.Disconnect(0)
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
		expvar.Publish("published", published)
		expvar.Publish("errors-connection-timeout", cnxTimeoutErrors)
		expvar.Publish("errors-connection-other", cnxErrors)
		expvar.Publish("errors-publish-timeout", publishTimeoutErrors)
		expvar.Publish("errors-publish-other", publishErrors)
		go log.Fatal(http.ListenAndServe(*expAddr, nil))
	}

	// Listen for errors
	client := buildClient(0)
	token := client.Subscribe("errors", 0, errorHandler)
	if !token.WaitTimeout(*timeout) {
		log.Fatal("subscribing to errors channel: timeout")
	}
	if err := token.Error(); err != nil {
		log.Fatal("subscribing to errors channel:", err)
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
