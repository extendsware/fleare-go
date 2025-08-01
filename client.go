package fleare

import (
	"context"
	"fmt"
	"sync"

	"github.com/extendsware/fleare-go/cmd"
	"github.com/extendsware/fleare-go/handler"
)

type Client struct {
	ctx       context.Context
	pool      *handler.Pool
	options   *Options
	state     ClientState
	stateMu   sync.RWMutex
	eventChan chan Event
	closeChan chan struct{}
	closeOnce sync.Once

	basicCommands *cmd.BasicCommands
}

// NewClient creates a new Fleare client with the given options
func NewClient(ctx context.Context, opts *Options) *Client {
	if opts == nil {
		opts = &Options{}
	}
	opts.setDefaults()

	poolOpts := handler.PoolOptions{
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
	}

	client := &Client{
		ctx:       ctx,
		pool:      handler.NewPool(ctx, poolOpts),
		options:   opts,
		state:     StateDisconnected,
		eventChan: make(chan Event, 100),
		closeChan: make(chan struct{}),
	}

	client.basicCommands = cmd.NewBasicCommands(client)

	return client
}

func (c *Client) Connect() error {
	c.stateMu.Lock()
	if c.state != StateDisconnected {
		c.stateMu.Unlock()
		return nil
	}
	c.setState(StateConnecting)
	c.stateMu.Unlock()

	c.emitEvent(Event{Type: EventConnecting})

	err := c.pool.Initialize()
	if err != nil {
		c.setState(StateError)
		c.emitEvent(Event{Type: EventError, Error: err})
		return fmt.Errorf("%w", err)
	}

	c.setState(StateConnected)
	c.emitEvent(Event{Type: EventConnected})

	return nil
}

func (c *Client) ExecuteCommand(command string, args []string) (any, error) {
	c.stateMu.RLock()
	if c.state != StateConnected {
		c.stateMu.RUnlock()
		return nil, fmt.Errorf("client is not connected (state: %s)", c.state)
	}
	c.stateMu.RUnlock()

	return c.pool.SendCommand(c.ctx, command, args)
}

// setState sets the client state and emits a state change event
func (c *Client) setState(newState ClientState) {
	c.state = newState
	c.emitEvent(Event{Type: EventStateChanged, State: newState})
}

// emitEvent sends an event to the event channel
func (c *Client) emitEvent(event Event) {
	select {
	case c.eventChan <- event:
	case <-c.closeChan:
	default:
		// Channel is full, drop the event
	}
}

func (c *Client) Events() <-chan Event {
	return c.eventChan
}

// Close closes all connections and shuts down the client
func (c *Client) Close() error {
	var closeErr error
	c.closeOnce.Do(func() {
		c.stateMu.Lock()
		if c.state == StateDisconnected {
			c.stateMu.Unlock()
			return
		}
		c.setState(StateDisconnecting)
		c.stateMu.Unlock()

		close(c.closeChan)
		closeErr = c.pool.Close()

		c.setState(StateDisconnected)
		c.emitEvent(Event{Type: EventClosed})
		close(c.eventChan)
	})

	return closeErr
}

func (c *Client) Ping(value ...string) (string, error) {
	return c.basicCommands.Ping(value...)
}

func (c *Client) Set(key string, value interface{}) (interface{}, error) {
	return c.basicCommands.Set(key, value)
}

// Get retrieves a value by key, with optional JSON path
func (c *Client) Get(key string, path ...string) (interface{}, error) {
	return c.basicCommands.Get(key, path...)
}
