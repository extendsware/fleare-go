package fleare

import (
	"time"
)

// ClientState represents the current state of the client connection
type ClientState string

const (
	StateDisconnected  ClientState = "disconnected"
	StateConnecting    ClientState = "connecting"
	StateConnected     ClientState = "connected"
	StateDisconnecting ClientState = "disconnecting"
	StateError         ClientState = "error"
)

// String returns the string representation of the client state
func (s ClientState) String() string {
	return string(s)
}

// EventType represents different types of client events
type EventType string

const (
	EventConnecting   EventType = "connecting"
	EventConnected    EventType = "connected"
	EventDisconnected EventType = "disconnected"
	EventError        EventType = "error"
	EventClosed       EventType = "closed"
	EventStateChanged EventType = "stateChanged"
)

// Event represents a client lifecycle event
type Event struct {
	Type  EventType
	State ClientState
	Error error
}

// Options holds configuration options for the Fleare client
type Options struct {
	// Host is the server hostname or IP address (default: "127.0.0.1")
	Host string

	// Port is the server port number (default: 9219)
	Port int

	// Username for authentication (optional)
	Username string

	// Password for authentication (optional)
	Password string

	// PoolSize is the number of connections in the pool (default: 10)
	PoolSize int

	// ConnectTimeout is the timeout for establishing connections (default: 30s)
	ConnectTimeout time.Duration

	// RequestTimeout is the timeout for individual requests (default: 30s)
	RequestTimeout time.Duration

	// RetryInterval is the interval between retry attempts (default: 10s)
	RetryInterval time.Duration

	// MaxRetries is the maximum number of retry attempts (default: 3)
	MaxRetries int

	// MaxQueueSize is the maximum number of queued requests (default: 100000)
	MaxQueueSize int
}

// setDefaults sets default values for unspecified options
func (o *Options) setDefaults() {
	if o.Host == "" {
		o.Host = "127.0.0.1"
	}
	if o.Port == 0 {
		o.Port = 9219
	}
	if o.PoolSize == 0 {
		o.PoolSize = 10
	}
	if o.ConnectTimeout == 0 {
		o.ConnectTimeout = 30 * time.Second
	}
	if o.RequestTimeout == 0 {
		o.RequestTimeout = 30 * time.Second
	}
	if o.RetryInterval == 0 {
		o.RetryInterval = 10 * time.Second
	}
	if o.MaxRetries == 0 {
		o.MaxRetries = 3
	}
	if o.MaxQueueSize == 0 {
		o.MaxQueueSize = 10
	}
}
