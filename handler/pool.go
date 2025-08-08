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

	requestQueue chan queuedRequest // Using channel instead of slice for better concurrency

	eventChan chan PoolEvent
	closeChan chan struct{}
	closeOnce sync.Once
	wg        sync.WaitGroup // For graceful shutdown
}

type connectionState struct {
	conn         *Connection
	busy         int32 // atomic
	requestCount int64
	lastUsed     time.Time
}

type queuedRequest struct {
	command    string
	args       []string
	resultChan chan queuedResult
	ctx        context.Context
}

type queuedResult struct {
	result interface{}
	err    error
}

type PoolEvent struct {
	Type EventType
	Err  error
}

type EventType int

const (
	EventConnectionError EventType = iota
	EventConnectionClosed
	EventPoolClosed
)

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
		requestQueue:   make(chan queuedRequest, opts.MaxQueueSize),
		eventChan:      make(chan PoolEvent, 100),
		closeChan:      make(chan struct{}),
	}
}

func (p *Pool) Initialize() error {
	p.connections = make([]*connectionState, 0, p.poolSize)

	for range p.poolSize {
		conn := NewConnection(p.host, p.port)

		connectCtx, cancel := context.WithTimeout(p.ctx, p.connectTimeout)
		err := conn.Connect(connectCtx)
		cancel()

		if err != nil {
			_ = p.closeAllConnections()
			return err
		}

		if err := p.authenticateConnection(conn); err != nil {
			_ = conn.Close()
			_ = p.closeAllConnections()
			return err
		}

		p.connections = append(p.connections, &connectionState{
			conn:     conn,
			lastUsed: time.Now(),
		})

		// Start a fixed number of worker goroutines
		p.wg.Add(1)
		go p.worker()
	}

	return nil
}

func (p *Pool) authenticateConnection(conn *Connection) error {
	cmd := &comm.Command{
		Command: "auth",
		Args:    []string{p.username, p.password},
	}

	response, err := conn.Execute(p.ctx, cmd)
	if err != nil {
		return err
	}

	if response.GetStatus() != "Ok" {
		return fmt.Errorf("%s", string(response.GetResult()))
	}

	conn.SetClientID(response.GetClientId())
	return nil
}

func (p *Pool) worker() {
	defer p.wg.Done()

	for {
		select {
		case req, ok := <-p.requestQueue:
			if !ok {
				return
			}
			p.processRequest(req)
		case <-p.closeChan:
			return
		}
	}
}

func (p *Pool) processRequest(req queuedRequest) {
	connState := p.getAvailableConnection()
	if connState == nil {
		select {
		case req.resultChan <- queuedResult{err: fmt.Errorf("no available connections")}:
		case <-req.ctx.Done():
		}
		return
	}

	defer atomic.StoreInt32(&connState.busy, 0)

	cmd := &comm.Command{
		Command: req.command,
		Args:    req.args,
	}

	// fmt.Println("Processing command:", cmd.Command, "with args:", cmd.Args)

	ctx := req.ctx
	if p.requestTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(req.ctx, p.requestTimeout)
		defer cancel()
	}

	// Update connection stats
	defer func() {
		connState.lastUsed = time.Now()
		atomic.AddInt64(&connState.requestCount, 1)
	}()

	// Send command
	response, err := connState.conn.Execute(ctx, cmd)
	if err != nil {
		select {
		case req.resultChan <- queuedResult{err: err}:
		case <-req.ctx.Done():
		}
		return
	}
	result, err := p.parseResult(response)
	select {
	case req.resultChan <- queuedResult{result: result, err: err}:
	case <-req.ctx.Done():
	}
}

func (p *Pool) SendCommand(ctx context.Context, command string, args []string) (interface{}, error) {
	resultChan := make(chan queuedResult, 1)

	select {
	case p.requestQueue <- queuedRequest{
		command:    command,
		args:       args,
		resultChan: resultChan,
		ctx:        ctx,
	}:
		select {
		case result := <-resultChan:
			return result.result, result.err
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-p.closeChan:
			return nil, fmt.Errorf("pool is closed")
		}
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-p.closeChan:
		return nil, fmt.Errorf("pool is closed")
	}
}

func (p *Pool) getAvailableConnection() *connectionState {
	if len(p.connections) == 0 {
		return nil
	}

	for i := range p.connections {
		index := i % len(p.connections)
		connState := p.connections[index]

		if atomic.CompareAndSwapInt32(&connState.busy, 0, 1) {
			if connState.conn.IsHealthy() {
				return connState
			}
			atomic.StoreInt32(&connState.busy, 0)
		}
	}

	return nil
}

func (p *Pool) parseResult(response *comm.Response) (interface{}, error) {
	if response.GetStatus() == "Error" {
		return nil, fmt.Errorf("server error: %s", string(response.GetResult()))
	}

	resultStr := string(response.GetResult())
	if resultStr == "" {
		return nil, nil
	}

	return resultStr, nil
}

func (p *Pool) Close() error {
	var closeErr error
	p.closeOnce.Do(func() {
		close(p.closeChan)
		close(p.requestQueue)
		p.wg.Wait()
		closeErr = p.closeAllConnections()
		close(p.eventChan)
	})
	return closeErr
}

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
