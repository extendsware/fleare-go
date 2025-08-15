package fleare

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
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

// Basic Operations - delegating to basicCommands

// Set sets a key-value pair in the database
func (c *Client) Set(key string, value interface{}) (interface{}, error) {
	return c.basicCommands.Set(key, value)
}

// Get retrieves a value by key, with optional JSON path
func (c *Client) Get(key string, path ...string) (interface{}, error) {
	return c.basicCommands.Get(key, path...)
}

// Del deletes a key from the database
func (c *Client) Del(key string) (interface{}, error) {
	return c.basicCommands.Del(key)
}

// Exists checks if a key exists in the database
func (c *Client) Exists(key string) (bool, error) {
	return c.basicCommands.Exists(key)
}

// Ping sends a ping to the server
func (c *Client) Ping(value ...string) (string, error) {
	return c.basicCommands.Ping(value...)
}

// Status gets the server status
func (c *Client) Status() (interface{}, error) {
	return c.basicCommands.Status()
}

// String Operations

// StrSet sets a string value
func (c *Client) StrSet(key string, value string) (interface{}, error) {
	args := []string{key, value}
	return c.ExecuteCommand("STR.SET", args)
}

// StrGet gets a string value
func (c *Client) StrGet(key string) (string, error) {
	args := []string{key}
	result, err := c.ExecuteCommand("STR.GET", args)
	if err != nil {
		return "", err
	}

	if resultStr, ok := result.(string); ok {
		return resultStr, nil
	}

	return "", fmt.Errorf("unexpected result type for StrGet")
}

// StrAppend appends to a string value
func (c *Client) StrAppend(key string, value string) (interface{}, error) {
	args := []string{key, value}
	return c.ExecuteCommand("STR.APPEND", args)
}

// StrRange gets a substring
func (c *Client) StrRange(key string, start, end int) (string, error) {
	args := []string{key, strconv.Itoa(start), strconv.Itoa(end)}
	result, err := c.ExecuteCommand("STR.RANGE", args)
	if err != nil {
		return "", err
	}

	if resultStr, ok := result.(string); ok {
		return resultStr, nil
	}

	return "", fmt.Errorf("unexpected result type for StrRange")
}

// StrLength gets the length of a string
func (c *Client) StrLength(key string) (int, error) {
	args := []string{key}
	result, err := c.ExecuteCommand("STR.LENGTH", args)
	if err != nil {
		return 0, err
	}

	if resultStr, ok := result.(string); ok {
		return strconv.Atoi(resultStr)
	}

	return 0, fmt.Errorf("unexpected result type for StrLength")
}

// Number Operations

// NumSet sets a number value
func (c *Client) NumSet(key string, value float64) (interface{}, error) {
	args := []string{key, fmt.Sprintf("%g", value)}
	return c.ExecuteCommand("NUM.SET", args)
}

// NumGet gets a number value
func (c *Client) NumGet(key string) (float64, error) {
	args := []string{key}
	result, err := c.ExecuteCommand("NUM.GET", args)
	if err != nil {
		return 0, err
	}

	if resultStr, ok := result.(string); ok {
		return strconv.ParseFloat(resultStr, 64)
	}

	return 0, fmt.Errorf("unexpected result type for NumGet")
}

// NumIncr increments a number value
func (c *Client) NumIncr(key string, increment ...float64) (float64, error) {
	args := []string{key}
	if len(increment) > 0 {
		args = append(args, fmt.Sprintf("%g", increment[0]))
	}

	result, err := c.ExecuteCommand("NUM.INCR", args)
	if err != nil {
		return 0, err
	}

	if resultStr, ok := result.(string); ok {
		return strconv.ParseFloat(resultStr, 64)
	}

	return 0, fmt.Errorf("unexpected result type for NumIncr")
}

// NumDecr decrements a number value
func (c *Client) NumDecr(key string, decrement ...float64) (float64, error) {
	args := []string{key}
	if len(decrement) > 0 {
		args = append(args, fmt.Sprintf("%g", decrement[0]))
	}

	result, err := c.ExecuteCommand("NUM.DECR", args)
	if err != nil {
		return 0, err
	}

	if resultStr, ok := result.(string); ok {
		return strconv.ParseFloat(resultStr, 64)
	}

	return 0, fmt.Errorf("unexpected result type for NumDecr")
}

// List Operations

// ListSet sets a list value
func (c *Client) ListSet(key string, list []interface{}) (interface{}, error) {
	args := []string{key}

	// Convert list to JSON string
	jsonBytes, err := json.Marshal(list)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal list to JSON: %w", err)
	}

	args = append(args, string(jsonBytes))
	return c.ExecuteCommand("LIST.SET", args)
}

// ListPush pushes values to a list
func (c *Client) ListPush(key string, values ...interface{}) (interface{}, error) {
	args := []string{key}

	for _, value := range values {
		valueStr, err := c.valueToString(value)
		if err != nil {
			return nil, fmt.Errorf("failed to convert value to string: %w", err)
		}
		args = append(args, valueStr)
	}

	return c.ExecuteCommand("LIST.PUSH", args)
}

// ListPop pops a value from a list
func (c *Client) ListPop(key string) (interface{}, error) {
	args := []string{key}
	result, err := c.ExecuteCommand("LIST.POP", args)
	if err != nil {
		return nil, err
	}

	return result, nil
}

// ListGet gets a list item by index
func (c *Client) ListGet(key string, index ...int) (interface{}, error) {
	args := []string{key}
	if len(index) > 0 {
		args = append(args, strconv.Itoa(index[0]))
	}

	result, err := c.ExecuteCommand("LIST.GET", args)
	if err != nil {
		return nil, err
	}

	return result, nil
}

// ListLen gets the length of a list
func (c *Client) ListLen(key string) (int, error) {
	args := []string{key}
	result, err := c.ExecuteCommand("LIST.LEN", args)
	if err != nil {
		return 0, err
	}

	if resultStr, ok := result.(string); ok {
		return strconv.Atoi(resultStr)
	}

	return 0, fmt.Errorf("unexpected result type for ListLen")
}

// ListFind finds a value in a list
func (c *Client) ListFind(key string, value interface{}) (int, error) {
	args := []string{key}

	valueStr, err := c.valueToString(value)
	if err != nil {
		return -1, fmt.Errorf("failed to convert value to string: %w", err)
	}
	args = append(args, valueStr)

	result, err := c.ExecuteCommand("LIST.FIND", args)
	if err != nil {
		return -1, err
	}

	if resultStr, ok := result.(string); ok {
		return strconv.Atoi(resultStr)
	}

	return -1, fmt.Errorf("unexpected result type for ListFind")
}

// JSON Operations

// JsonSet sets a JSON value at the specified path
func (c *Client) JsonSet(key string, path string, value interface{}) (interface{}, error) {
	args := []string{key, path}

	valueStr, err := c.valueToString(value)
	if err != nil {
		return nil, fmt.Errorf("failed to convert value to string: %w", err)
	}
	args = append(args, valueStr)

	return c.ExecuteCommand("JSON.SET", args)
}

// JsonGet gets a JSON value at the specified path
func (c *Client) JsonGet(key string, path ...string) (interface{}, error) {
	args := []string{key}
	if len(path) > 0 && path[0] != "" {
		args = append(args, path[0])
	}

	result, err := c.ExecuteCommand("JSON.GET", args)
	if err != nil {
		return nil, err
	}

	return result, nil
}

// JsonAdd adds to a JSON array
func (c *Client) JsonAdd(key string, path string, value interface{}) (interface{}, error) {
	args := []string{key, path}

	valueStr, err := c.valueToString(value)
	if err != nil {
		return nil, fmt.Errorf("failed to convert value to string: %w", err)
	}
	args = append(args, valueStr)

	return c.ExecuteCommand("JSON.ADD", args)
}

// JsonMerge merges JSON objects
func (c *Client) JsonMerge(key string, path string, value interface{}) (interface{}, error) {
	args := []string{key, path}

	valueStr, err := c.valueToString(value)
	if err != nil {
		return nil, fmt.Errorf("failed to convert value to string: %w", err)
	}
	args = append(args, valueStr)

	return c.ExecuteCommand("JSON.MERGE", args)
}

// Map Operations

// MapSet sets a field in a map
func (c *Client) MapSet(key string, field string, value interface{}) (interface{}, error) {
	args := []string{key, field}

	valueStr, err := c.valueToString(value)
	if err != nil {
		return nil, fmt.Errorf("failed to convert value to string: %w", err)
	}
	args = append(args, valueStr)

	return c.ExecuteCommand("MAP.SET", args)
}

// MapGet gets field(s) from a map
func (c *Client) MapGet(key string, field ...string) (interface{}, error) {
	args := []string{key}
	args = append(args, field...)

	result, err := c.ExecuteCommand("MAP.GET", args)
	if err != nil {
		return nil, err
	}

	return result, nil
}

// MapDel deletes a field from a map
func (c *Client) MapDel(key string, field string) (interface{}, error) {
	args := []string{key, field}
	return c.ExecuteCommand("MAP.DEL", args)
}

// TTL Operations

// TTLExpire sets an expiration time for a key
func (c *Client) TTLExpire(key string, seconds int) (interface{}, error) {
	args := []string{key, strconv.Itoa(seconds)}
	return c.ExecuteCommand("TTL.EXPIRE", args)
}

// TTL gets the time-to-live for a key
func (c *Client) TTL(key string) (int, error) {
	args := []string{key}
	result, err := c.ExecuteCommand("TTL", args)
	if err != nil {
		return 0, err
	}

	if resultStr, ok := result.(string); ok {
		return strconv.Atoi(resultStr)
	}

	return 0, fmt.Errorf("unexpected result type for TTL")
}

// Helper methods

// valueToString converts a value to its string representation for transmission
func (c *Client) valueToString(value interface{}) (string, error) {
	switch v := value.(type) {
	case string:
		return v, nil
	case int, int8, int16, int32, int64:
		return fmt.Sprintf("%d", v), nil
	case uint, uint8, uint16, uint32, uint64:
		return fmt.Sprintf("%d", v), nil
	case float32, float64:
		return fmt.Sprintf("%g", v), nil
	case bool:
		return strconv.FormatBool(v), nil
	case nil:
		return "", nil
	default:
		// For complex types, marshal to JSON
		jsonBytes, err := json.Marshal(value)
		if err != nil {
			return "", fmt.Errorf("failed to marshal value to JSON: %w", err)
		}
		return string(jsonBytes), nil
	}
}

// parseResult attempts to parse the result into appropriate Go types
func (c *Client) parseResult(result interface{}) (interface{}, error) {
	if result == nil {
		return nil, nil
	}

	resultStr, ok := result.(string)
	if !ok {
		return result, nil
	}

	if resultStr == "" {
		return nil, nil
	}

	// Try to parse as JSON first
	var jsonResult interface{}
	if err := json.Unmarshal([]byte(resultStr), &jsonResult); err == nil {
		return jsonResult, nil
	}

	// If JSON parsing fails, return as string
	return resultStr, nil
}
