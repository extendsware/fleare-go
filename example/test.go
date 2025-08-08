package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"github.com/extendsware/fleare-go/example/comm"
	"google.golang.org/protobuf/proto"
)

const (
	serverAddr        = "127.0.0.1:9219"
	concurrentClients = 300
	maxRetries        = 3
	logResponses      = false // Set to true to print each response
)

type result struct {
	latency time.Duration
	success bool
	retries int
}

func runLoadTest(addr string, clients, messages int) []result {
	var wg sync.WaitGroup
	resultChan := make(chan result, clients*messages)
	wg.Add(clients)

	for i := 0; i < clients; i++ {
		go func(clientID int) {
			defer wg.Done()

			conn, err := net.Dial("tcp", addr)
			if err != nil {
				log.Printf("Client %d: connection error: %v", clientID, err)
				return
			}
			defer conn.Close()

			auth := &comm.Command{
				Command: "auth",
				Args:    []string{"", ""},
			}

			if err := Write(conn, auth); err != nil {
				log.Printf("Auth %d: write error: %v", clientID, err)
				// resultChan <- result{time.Since(start), false, retries}
				return
			}

			var r comm.Response
			err = Read(conn, &r)
			if err != nil {
				log.Printf("Auth %d: read error: %v", clientID, err)
				// resultChan <- result{time.Since(start), false, retries}
				return
			}

			for msgID := 0; msgID < messages; msgID++ {
				msg := fmt.Sprintf("client%d-message%d", clientID, msgID)
				retries := 0
				success := false

				start := time.Now()

				// Build the Command message
				cmd := &comm.Command{
					Command: "ping",
					Args:    []string{msg},
				}

				if err := Write(conn, cmd); err != nil {
					log.Printf("Client %d: write error: %v", clientID, err)
					resultChan <- result{time.Since(start), false, retries}
					continue
				}

				var resp comm.Response
				err = Read(conn, &resp)
				if err != nil {
					log.Printf("Client %d: read error: %v", clientID, err)
					resultChan <- result{time.Since(start), false, retries}
					continue
				}

				// fmt.Printf("%d %s %s\n", time.Since(start).Microseconds(), resp.Status, string(resp.Result))

				if string(resp.Result) != fmt.Sprintf("%s %s", "PONG", msg) {
					log.Printf("Client %d: bad response: got %s, want %s", clientID, string(resp.Result), msg)
					success = false
				} else {
					success = true
					if logResponses {
						fmt.Printf("Client %d Message %d OK\n", clientID, msgID)
					}
				}

				resultChan <- result{time.Since(start), success, retries}
			}
		}(i)
	}

	go func() {
		wg.Wait()
		close(resultChan)
	}()

	// Collect results
	var results []result
	for r := range resultChan {
		results = append(results, r)
	}

	return results
}

// Write a framed Protobuf message
func Write(conn net.Conn, msg proto.Message) error {
	// Encode protobuf
	data, err := proto.Marshal(msg)
	if err != nil {
		return err
	}

	// Create a buffer
	var buf bytes.Buffer

	// Write the length prefix (4 bytes, big endian)
	if err := binary.Write(&buf, binary.BigEndian, uint32(len(data))); err != nil {
		return err
	}

	// Write the protobuf data
	if _, err := buf.Write(data); err != nil {
		return err
	}

	// Send everything
	_, err = conn.Write(buf.Bytes())
	return err
}

// Read a framed Protobuf message
func Read(conn net.Conn, msg proto.Message) error {
	var length uint32

	// Read the length prefix
	if err := binary.Read(conn, binary.BigEndian, &length); err != nil {
		return err
	}

	// Read the message data
	data := make([]byte, length)
	_, err := conn.Read(data)
	if err != nil {
		return err
	}

	// Unmarshal the data
	return proto.Unmarshal(data, msg)
}

func main() {
	fmt.Println("Starting load test...")
	start := time.Now()

	// Set the number of clients and messages per client
	clients := 300
	messagesPerClient := 100

	results := runLoadTest(serverAddr, clients, messagesPerClient)

	// Analyze results
	var totalLatency time.Duration
	successCount := 0
	totalMessages := len(results)

	if totalMessages > 0 {
		for _, r := range results {
			totalLatency += r.latency
			if r.success {
				successCount++
			}
		}

		avgLatency := totalLatency / time.Duration(totalMessages)
		successRate := float64(successCount) / float64(totalMessages) * 100

		// Output results
		fmt.Printf("=== Load Test Results ===\n")
		fmt.Printf("Clients: %d\n", clients)
		fmt.Printf("Messages per client: %d\n", messagesPerClient)
		fmt.Printf("Total messages: %d\n", totalMessages)
		fmt.Printf("Success: %d\n", successCount)
		fmt.Printf("Success rate: %.2f%%\n", successRate)
		fmt.Printf("Average latency: %v\n", avgLatency)
		fmt.Printf("Total test duration: %v\n", time.Since(start))
	} else {
		fmt.Println("No results collected")
	}
}
