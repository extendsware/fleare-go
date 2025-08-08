package fleare

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/extendsware/fleare-go/cmd"
	"github.com/extendsware/fleare-go/handler"
)

type Client struct {
	ctx       context.Context
	pool      *handler.Pool
	options   *Options
	state     atomic.Value // For storing ClientState (string)
	eventChan chan Event
	closeOnce sync.Once
	closeChan chan struct{}

	basicCommands *cmd.BasicCommands
}

// NewClient creates a new Fleare client with optimized initialization
func NewClient(ctx context.Context, opts *Options) *Client {
	if opts == nil {
		opts = &Options{}
	}
	opts.setDefaults()

	client := &Client{
		ctx:       ctx,
		options:   opts,
		eventChan: make(chan Event, 100),
		closeChan: make(chan struct{}),
	}
	client.state.Store(StateDisconnected)

	// Initialize pool with context that respects client's lifecycle
	poolCtx, cancel := context.WithCancel(ctx)
	go func() {
		<-client.closeChan
		cancel()
	}()

	client.pool = handler.NewPool(poolCtx, handler.PoolOptions{
		Host:           opts.Host,
		Port:           opts.Port,
		Username:       opts.Username,
		Password:       opts.Password,
		PoolSize:       opts.PoolSize,
		ConnectTimeout: opts.ConnectTimeout,
		RequestTimeout: opts.RequestTimeout,
		RetryInterval:  opts.RetryInterval,
		MaxRetries:     opts.MaxRetries,
		MaxQueueSize:   opts.MaxQueueSize,
	})

	client.basicCommands = cmd.NewBasicCommands(client)
	return client
}

func (c *Client) Connect() error {
	// Fast path check
	if state := c.getState(); state != StateDisconnected {
		return nil
	}

	// Try to transition to connecting state
	if !c.compareAndSwapState(StateDisconnected, StateConnecting) {
		return nil
	}

	c.emitEvent(Event{Type: EventConnecting})

	if err := c.pool.Initialize(); err != nil {
		c.setState(StateError)
		c.emitEvent(Event{Type: EventError, Error: err})
		return fmt.Errorf("connection failed: %w", err)
	}

	c.setState(StateConnected)
	c.emitEvent(Event{Type: EventConnected})
	return nil
}

// State management helpers
func (c *Client) getState() ClientState {
	return c.state.Load().(ClientState)
}

func (c *Client) setState(newState ClientState) {
	c.state.Store(newState)
	c.emitEvent(Event{Type: EventStateChanged, State: newState})
}

func (c *Client) compareAndSwapState(old, new ClientState) bool {
	// Note: This isn't truly atomic for strings in Go, but provides better
	// semantics than separate load/store operations
	current := c.getState()
	if current != old {
		return false
	}
	c.setState(new)
	return true
}

// Rest of the methods remain the same as in the previous optimized version...
func (c *Client) ExecuteCommand(command string, args []string) (any, error) {
	if c.getState() != StateConnected {
		return nil, fmt.Errorf("client is not connected (state: %s)", c.getState())
	}
	return c.pool.SendCommand(c.ctx, command, args)
}

func (c *Client) emitEvent(event Event) {
	select {
	case c.eventChan <- event:
	case <-c.closeChan:
	default:
		// Metrics could be incremented here for dropped events
	}
}

func (c *Client) Events() <-chan Event {
	return c.eventChan
}

func (c *Client) Close() error {
	var closeErr error
	c.closeOnce.Do(func() {
		if c.getState() == StateDisconnected {
			return
		}
		c.setState(StateDisconnecting)
		close(c.closeChan)
		closeErr = c.pool.Close()
		c.setState(StateDisconnected)
		c.emitEvent(Event{Type: EventClosed})
		safeCloseEventChan(c.eventChan)
	})
	return closeErr
}

func safeCloseEventChan(ch chan Event) {
	defer func() {
		if recover() != nil {
			// Channel already closed
		}
	}()
	close(ch)
}

// Basic command wrappers
func (c *Client) Ping(value ...string) (string, error) {
	return c.basicCommands.Ping(value...)
}

func (c *Client) Set(key string, value interface{}) (interface{}, error) {
	return c.basicCommands.Set(key, value)
}

func (c *Client) Get(key string, path ...string) (interface{}, error) {
	return c.basicCommands.Get(key, path...)
}
