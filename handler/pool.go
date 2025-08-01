package handler

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/extendsware/fleare-go/comm"
)

type Pool struct {
	ctx            context.Context
	connections    []*connectionState
	host           string
	port           int
	username       string
	password       string
	poolSize       int
	connectTimeout time.Duration
	requestTimeout time.Duration
	retryInterval  time.Duration
	maxRetries     int
	maxQueueSize   int

	requestQueue   []queuedRequest
	queueMu        sync.Mutex
	processing     int32
	requestCounter int64

	eventChan chan PoolEvent
	closeChan chan struct{}
	closeOnce sync.Once
}

// connectionState wraps a connection with its state
type connectionState struct {
	conn         *Connection
	busy         int32 // atomic
	requestCount int64
	lastUsed     time.Time
	mu           sync.RWMutex
}

// queuedRequest represents a queued command request
type queuedRequest struct {
	command    string
	args       []string
	resultChan chan queuedResult
	ctx        context.Context
}

// queuedResult represents the result of a queued request
type queuedResult struct {
	result interface{}
	err    error
}

// PoolEvent represents events from the connection pool
type PoolEvent struct {
	Type EventType
	Err  error
}

const (
	EventConnectionError EventType = iota
	EventConnectionClosed
	EventPoolClosed
)

type EventType int

type PoolOptions struct {
	Host           string
	Port           int
	Username       string
	Password       string
	PoolSize       int
	ConnectTimeout time.Duration
	RequestTimeout time.Duration
	RetryInterval  time.Duration
	MaxRetries     int
	MaxQueueSize   int
}

func NewPool(ctx context.Context, opts PoolOptions) *Pool {
	return &Pool{
		ctx:            ctx,
		host:           opts.Host,
		port:           opts.Port,
		username:       opts.Username,
		password:       opts.Password,
		poolSize:       opts.PoolSize,
		connectTimeout: opts.ConnectTimeout,
		requestTimeout: opts.RequestTimeout,
		retryInterval:  opts.RetryInterval,
		maxRetries:     opts.MaxRetries,
		maxQueueSize:   opts.MaxQueueSize,
		eventChan:      make(chan PoolEvent, 100),
		closeChan:      make(chan struct{}),
	}
}

// Initialize creates and authenticates all connections in the pool
func (p *Pool) Initialize() error {
	p.connections = make([]*connectionState, 0, p.poolSize)

	for range p.poolSize {
		conn := NewConnection(p.host, p.port)

		// Connect with timeout
		connectCtx, cancel := context.WithTimeout(p.ctx, p.connectTimeout)
		err := conn.Connect(connectCtx)
		defer cancel()

		if err != nil {
			_ = p.closeAllConnections()
			return err
		}

		// Authenticate
		err = p.authenticateConnection(conn)
		if err != nil {
			_ = conn.Close()
			_ = p.closeAllConnections()
			return err
		}

		// Create connection state
		connState := &connectionState{
			conn:     conn,
			lastUsed: time.Now(),
		}

		p.connections = append(p.connections, connState)

		// Optionally start monitoring this connection
		// go p.monitorConnection(connState)
	}

	return nil
}

// monitorConnection monitors a connection for errors and closure
func (p *Pool) monitorConnection(connState *connectionState) {
	for {
		select {
		case <-p.closeChan:
			return
		default:
			time.Sleep(time.Second)

			if !connState.conn.IsHealthy() {
				p.eventChan <- PoolEvent{
					Type: EventConnectionError,
					Err:  fmt.Errorf("connection %s became unhealthy", connState.conn.GetClientID()),
				}
			}
		}
	}
}

func (p *Pool) authenticateConnection(conn *Connection) error {

	cmd := &comm.Command{
		Command: "auth",
		Args:    []string{p.username, p.password},
	}

	err := conn.Write(p.ctx, cmd)
	if err != nil {
		return err
	}

	response, err := conn.Read(p.ctx)
	if err != nil {
		return err
	}

	if response.GetStatus() != "Ok" {
		return fmt.Errorf("%s", string(response.GetResult()))
	}

	conn.SetClientID(response.GetClientId())
	return nil
}

// SendCommand sends a command to the server using an available connection
func (p *Pool) SendCommand(ctx context.Context, command string, args []string) (interface{}, error) {
	resultChan := make(chan queuedResult, 1)

	queuedReq := queuedRequest{
		command:    command,
		args:       args,
		resultChan: resultChan,
		ctx:        ctx,
	}

	// Add to queue
	p.queueMu.Lock()
	if len(p.requestQueue) >= p.maxQueueSize {
		p.queueMu.Unlock()
		return nil, fmt.Errorf("request queue is full (max: %d)", p.maxQueueSize)
	}

	p.requestQueue = append(p.requestQueue, queuedReq)
	p.queueMu.Unlock()

	// Process queue
	if atomic.CompareAndSwapInt32(&p.processing, 0, 1) {
		go p.processQueue()
	}

	// Wait for result
	select {
	case result := <-resultChan:
		return result.result, result.err
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-p.closeChan:
		return nil, fmt.Errorf("pool is closed")
	}
}

func (p *Pool) processQueue() {
	defer atomic.StoreInt32(&p.processing, 0)

	for {
		p.queueMu.Lock()
		if len(p.requestQueue) == 0 {
			p.queueMu.Unlock()
			return
		}
		// fmt.Println("Processing request queue...")
		req := p.requestQueue[0]
		p.requestQueue = p.requestQueue[1:]
		p.queueMu.Unlock()

		// Find available connection
		connState := p.getAvailableConnection()
		if connState == nil {
			// No available connections, put request back
			p.queueMu.Lock()
			p.requestQueue = append([]queuedRequest{req}, p.requestQueue...)
			p.queueMu.Unlock()

			// Wait a bit before retrying
			time.Sleep(10 * time.Millisecond)
			continue
		}

		// Execute request asynchronously
		go p.executeRequest(connState, req)
	}
}

// getAvailableConnection returns an available connection using round-robin
func (p *Pool) getAvailableConnection() *connectionState {

	if len(p.connections) == 0 {
		return nil
	}
	// Use atomic counter for round-robin selection
	counter := atomic.AddInt64(&p.requestCounter, 1)
	startIndex := int(counter) % len(p.connections)

	// Try to find an available connection starting from the calculated index
	for i := range p.connections {
		index := (startIndex + i) % len(p.connections)

		connState := p.connections[index]

		if atomic.CompareAndSwapInt32(&connState.busy, 0, 1) {
			if connState.conn.IsHealthy() {
				return connState
			}
			// Connection is not healthy, mark as not busy and continue
			atomic.StoreInt32(&connState.busy, 0)
		}
	}

	return nil
}

// executeRequest executes a request on the given connection
func (p *Pool) executeRequest(connState *connectionState, req queuedRequest) {
	defer func() {
		atomic.StoreInt32(&connState.busy, 0)
		connState.mu.Lock()
		connState.lastUsed = time.Now()
		connState.requestCount++
		connState.mu.Unlock()
	}()

	cmd := &comm.Command{
		Command: req.command,
		Args:    req.args,
	}

	// Create timeout context
	ctx := req.ctx
	if p.requestTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(req.ctx, p.requestTimeout)
		defer cancel()
	}

	// Send command
	err := connState.conn.Write(ctx, cmd)
	if err != nil {
		select {
		case req.resultChan <- queuedResult{err: fmt.Errorf("write error: %w", err)}:
		case <-req.ctx.Done():
		}
		return
	}

	// Read response
	response, err := connState.conn.Read(ctx)
	if err != nil {
		select {
		case req.resultChan <- queuedResult{err: fmt.Errorf("read error: %w", err)}:
		case <-req.ctx.Done():
		}
		return
	}
	// Parse result
	result, err := p.parseResult(response)

	select {
	case req.resultChan <- queuedResult{result: result, err: err}:
	case <-req.ctx.Done():
	}
}

// parseResult parses the response and returns the result
func (p *Pool) parseResult(response *comm.Response) (interface{}, error) {
	if response.GetStatus() == "Error" {
		return nil, fmt.Errorf("%s", string(response.GetResult()))
	}

	// Try to parse as JSON first, fallback to string
	resultStr := string(response.GetResult())

	// Simple JSON parsing - in a real implementation, you might want to use encoding/json
	if resultStr == "" {
		return nil, nil
	}

	// For now, return as string. You can enhance this to parse JSON properly
	return resultStr, nil
}

func (p *Pool) Close() error {
	var closeErr error
	p.closeOnce.Do(func() {
		close(p.closeChan)

		// Clear the request queue
		p.queueMu.Lock()
		for _, req := range p.requestQueue {
			select {
			case req.resultChan <- queuedResult{err: fmt.Errorf("pool is closing")}:
			default:
			}
		}
		p.requestQueue = nil
		p.queueMu.Unlock()

		closeErr = p.closeAllConnections()
		close(p.eventChan)
	})

	return closeErr
}

// closeAllConnections closes all connections
func (p *Pool) closeAllConnections() error {
	var lastErr error

	for _, connState := range p.connections {
		if err := connState.conn.Close(); err != nil {
			lastErr = err
		}
	}

	p.connections = nil
	return lastErr
}
