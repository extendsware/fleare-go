package handler

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/extendsware/fleare-go/comm"
	"google.golang.org/protobuf/proto"
)

type Connection struct {
	conn             net.Conn
	clientID         string
	host             string
	port             int
	isConnected      bool
	readQueue        []pendingRead
	readQueueMu      sync.Mutex
	writeMu          sync.Mutex
	buffer           []byte
	maxReadQueueSize int
	requestCount     int64
	lastUsed         time.Time
	connectTime      time.Time
	closeOnce        sync.Once
	closed           chan struct{}
}

type pendingRead struct {
	resultChan chan readResult
	ctx        context.Context
}

type readResult struct {
	response *comm.Response
	err      error
}

func NewConnection(host string, port int) *Connection {
	return &Connection{
		host:             host,
		port:             port,
		maxReadQueueSize: 100000,
		buffer:           make([]byte, 0),
		closed:           make(chan struct{}),
		lastUsed:         time.Now(),
	}
}

// Connect establishes a TCP connection to the server
func (c *Connection) Connect(ctx context.Context) error {
	if c.isConnected {
		return nil
	}

	dialer := &net.Dialer{}
	conn, err := dialer.DialContext(ctx, "tcp", fmt.Sprintf("%s:%d", c.host, c.port))
	if err != nil {
		return fmt.Errorf("failed to connect to %s:%d: %w", c.host, c.port, err)
	}

	c.conn = conn
	c.isConnected = true
	c.connectTime = time.Now()

	// Start the read goroutine
	go c.readLoop()

	return nil
}

// readLoop continuously reads messages from the connection
func (c *Connection) readLoop() {
	defer func() {
		c.clearReadQueue(fmt.Errorf("connection closed"))
	}()

	for {
		select {
		case <-c.closed:
			fmt.Println("readLoop: closed read loop...")
			return
		default:
			// Set read deadline to prevent hanging indefinitely
			c.conn.SetReadDeadline(time.Now().Add(2 * time.Second))

			var resp comm.Response
			if err := c.ReadProto(&resp); err != nil {
				c.handleError(err)
				continue
			}
			// Deliver the response to the waiting reader
			c.deliverResponse(&resp)
		}
	}
}

func (c *Connection) ReadProto(msg proto.Message) error {

	var length uint32

	// Read the length prefix
	if err := binary.Read(c.conn, binary.BigEndian, &length); err != nil {
		return err
	}

	// Read the message data
	data := make([]byte, length)
	_, err := c.conn.Read(data)
	if err != nil {
		return err
	}

	// Unmarshal the data
	return proto.Unmarshal(data, msg)
}

// deliverResponse delivers a response to the first waiting reader
func (c *Connection) deliverResponse(response *comm.Response) {
	c.readQueueMu.Lock()
	defer c.readQueueMu.Unlock()

	if len(c.readQueue) > 0 {
		pending := c.readQueue[0]
		c.readQueue = c.readQueue[1:]

		select {
		case pending.resultChan <- readResult{response: response, err: nil}:
		case <-pending.ctx.Done():
			// Context was cancelled, put response back for next reader
			if len(c.readQueue) > 0 {
				nextPending := c.readQueue[0]
				c.readQueue = c.readQueue[1:]
				select {
				case nextPending.resultChan <- readResult{response: response, err: nil}:
				case <-nextPending.ctx.Done():
					// If next reader is also cancelled, drop the response
				}
			}
		}
	}
}

// Write sends a command to the server
func (c *Connection) Write(ctx context.Context, cmd *comm.Command) error {
	if !c.isConnected {
		return fmt.Errorf("connection is not established")
	}

	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	// Serialize the command with length prefix
	data, err := proto.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("failed to marshal command: %w", err)
	}

	// Set write deadline
	// if deadline, ok := ctx.Deadline(); ok {
	// 	c.conn.SetWriteDeadline(deadline)
	// } else {
	// 	c.conn.SetWriteDeadline(time.Now().Add(30 * time.Second))
	// }

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

	// Write the complete message (length + data)
	_, err = c.conn.Write(buf.Bytes())
	if err != nil {
		return fmt.Errorf("failed to write message data: %w", err)
	}

	c.requestCount++
	c.lastUsed = time.Now()

	return nil
}

// Read reads a response from the server
func (c *Connection) Read(ctx context.Context) (*comm.Response, error) {
	if !c.isConnected {
		return nil, fmt.Errorf("connection is not established")
	}

	c.readQueueMu.Lock()
	if len(c.readQueue) >= c.maxReadQueueSize {
		c.readQueueMu.Unlock()
		return nil, fmt.Errorf("read queue is full (max: %d)", c.maxReadQueueSize)
	}

	resultChan := make(chan readResult, 1)
	pending := pendingRead{
		resultChan: resultChan,
		ctx:        ctx,
	}
	c.readQueue = append(c.readQueue, pending)
	c.readQueueMu.Unlock()

	select {
	case result := <-resultChan:
		if result.err != nil {
			return nil, result.err
		}
		return result.response, result.err
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.closed:
		return nil, fmt.Errorf("connection closed")
	}
}

// handleError handles errors during read operations
func (c *Connection) handleError(err error) {
	c.clearReadQueue(err)
}

// clearReadQueue clears all pending read operations with an error
func (c *Connection) clearReadQueue(err error) {
	c.readQueueMu.Lock()
	defer c.readQueueMu.Unlock()

	for _, pending := range c.readQueue {
		select {
		case pending.resultChan <- readResult{err: err}:
		case <-pending.ctx.Done():
		}
	}
	c.readQueue = nil
}

// Close closes the connection
func (c *Connection) Close() error {
	var closeErr error
	c.closeOnce.Do(func() {
		close(c.closed)
		c.isConnected = false
		if c.conn != nil {
			closeErr = c.conn.Close()
		}
		c.clearReadQueue(fmt.Errorf("connection closed"))
	})
	return closeErr
}

// IsHealthy returns true if the connection is healthy
func (c *Connection) IsHealthy() bool {
	c.readQueueMu.Lock()
	queueSize := len(c.readQueue)
	c.readQueueMu.Unlock()

	return c.isConnected &&
		c.conn != nil &&
		queueSize < c.maxReadQueueSize
}

// SetClientID sets the client ID for this connection
func (c *Connection) SetClientID(clientID string) {
	c.clientID = clientID
}

// GetClientID returns the client ID for this connection
func (c *Connection) GetClientID() string {
	return c.clientID
}
