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
	"fmt"

	"github.com/extendsware/fleare-go"
)

func main() {
	// Create client
	client := fleare.CreateClient(&fleare.Options{
		Host: "127.0.0.1",
		Port: 9219,
		Username: "admin",
		Password: "password",
		PoolSize: 10,
	})

	go monitorEvents(client)

	err := client.Connect()
	if err != nil {
		fmt.Println(err)
		return
	}

	// example of getting a value
	res, err := client.Get("key-1-1")

	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println("Value for 'key':", res)

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
			fmt.Println("Event Disconnected")
		case fleare.StateError:
			fmt.Printf("Event error: %v", event.Error)
		}
	}
}

```

## Documentation

Full documentation is available at [fleare.com](https://fleare.com/docs/clients/setup).