# Fleare Go Client

[![Go Reference](https://pkg.go.dev/badge/github.com/extendsware/fleare-go.svg)](https://pkg.go.dev/github.com/extendsware/fleare-go)
[![CI](https://github.com/extendsware/fleare-go/actions/workflows/ci.yml/badge.svg)](https://github.com/extendsware/fleare-go/actions/workflows/ci.yml)

Official Go client library for Fleare servers.

## Features

- Connection pooling
- Asynchronous operations
- Protobuf-based communication
- Event-driven architecture
- Command abstractions

## Installation

```bash
go get github.com/extendsware/fleare-go
```

## Quick Start

```go
package main

import (
	"context"
	"log"
	"time"

	"github.com/extendsware/fleare-go"
	"github.com/extendsware/fleare-go/commands"
)

func main() {
	// Create client
	client := fleare.CreateClient(&fleare.Options{
		Host:     "127.0.0.1",
		Port:     9219,
		PoolSize: 10,
	})

    go monitorEvents(client)

	err := client.Connect()
	if err != nil {
		fmt.Println(err)
		return
	}

	defer client.Close()
}

func monitorEvents(client *fleare.Client) {
	for event := range client.Events() {
		switch event.State {
		case fleare.StateConnecting:
			fmt.Println("Event is connecting...")
		case fleare.StateConnected:
			fmt.Println("Event connected successfully!")
		case fleare.StateDisconnecting:
			fmt.Println("Event Disconnecting")
		case fleare.StateDisconnected:
			fmt.Println("EventStateChanged", event.State)
		case fleare.StateError:
			fmt.Printf("Event error: %v", event.Error)
		}
	}
}

```

## Documentation

Full documentation is available at [fleare.com](https://fleare.com/docs/clients/setup).