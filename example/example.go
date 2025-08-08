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
		PoolSize: 150,
	})

	err := client.Connect()
	if err != nil {
		fmt.Println(err)
		return
	}

	start := time.Now()

	th := 100
	numRequests := 1000
	var wg sync.WaitGroup
	wg.Add(th)

	var mu sync.Mutex
	var successCount, errorCount int

	for i := 0; i < th; i++ {
		go func(i int) {
			defer wg.Done()
			for j := 0; j < numRequests; j++ {
				_, err := client.Ping(fmt.Sprintf("%d-%d", i, j))
				mu.Lock()
				if err != nil {
					errorCount++
				} else {
					successCount++
				}
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()

	elapsed := time.Since(start)
	totalRequests := numRequests * th

	fmt.Printf("Execution took %s for %d requests.\n", elapsed, totalRequests)
	fmt.Printf("Success: %d\n", successCount)
	fmt.Printf("Errors: %d\n", errorCount)
	if elapsed > 0 {
		fmt.Printf("Requests per second: %.2f\n", float64(totalRequests)/elapsed.Seconds())
	}
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
