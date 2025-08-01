package main

import (
	"fmt"
	"sync"
	"time"

	"github.com/extendsware/fleare-go"
)

func main() {
	// Create a client with options
	client := fleare.CreateClient(&fleare.Options{
		Host:     "127.0.0.1",
		Port:     9219,
		PoolSize: 10,
		// Username: "root",
		// Password: "root",
	})

	// Set up event monitoring
	// go monitorEvents(client)

	err := client.Connect()
	if err != nil {
		fmt.Println(err)
	}

	start := time.Now()

	th := 20
	var wg sync.WaitGroup
	numRequests := 10
	wg.Add(th)

	for i := 0; i < th; i++ {
		go func(i int) {
			defer wg.Done()
			for i := 0; i < numRequests; i++ {
				_, err := client.Ping()
				if err != nil {
					fmt.Println("Error getting value:", err)
				}
			}

		}(i)
	}

	wg.Wait()

	elapsed := time.Since(start)
	fmt.Println("Execution took %s for %d requests.", elapsed, (numRequests * th))

	// Block main goroutine to keep connection alive
	// select {}
}

func monitorEvents(client *fleare.Client) {
	for event := range client.Events() {
		switch event.Type {
		case fleare.EventConnecting:
			fmt.Println("Event is connecting...")
		case fleare.EventConnected:
			fmt.Println("Event connected successfully!")
		case fleare.EventDisconnected:
			fmt.Println("Event disconnected")
		case fleare.EventError:
			fmt.Printf("Event error: %v", event.Error)
		case fleare.EventClosed:
			fmt.Println("Event connection closed")
		}
	}
}
