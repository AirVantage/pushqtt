[![Go Report Card](https://goreportcard.com/badge/github.com/AirVantage/pushqtt)](https://goreportcard.com/report/github.com/AirVantage/pushqtt)
[![GitHub release](https://img.shields.io/github/release/AirVantage/pushqtt.svg)](https://github.com/AirVantage/pushqtt/releases/)

PushQTT
=======

PushQTT is a simple service that simulates a fleet of MQTT devices that publish messages to a broker regularly. Errors and statistics are exposed as metrics (expvar). This is useful for monitoring or benchmarking a broker.

## Usage
    pushqtt [FLAGS...]

    -c uint
            Concurrency - number of devices (default 1)
    -e string
            expvar listening address (default none, metrics disabled)
    -f duration
            Publish frequency (default 1s)
    -h string
            Broker hostname and port (default "localhost:1883")
    -m string
            Message in JSON (default "{\"threadId\":\"%d\"}")
    -P string
            Password
    -q uint
            QoS: 0, 1 or 2
    -t string
            Topic
    -u string
            Username
    -v      Print paho warning messages
    -w duration
            Wait timeout on connect and publish (default 15s)

## New releases

This project uses [goreleaser](https://goreleaser.com/) and semantic versioning. Dependencies are automatically tracked by go modules.

1. Commit and push your changes to master
2. Tag a new release number and `git push --tags`
3. `GITHIB_TOKEN=[...] goreleaser`
