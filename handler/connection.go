package handler

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/extendsware/fleare-go/comm"
	"google.golang.org/protobuf/proto"
)

const (
	defaultReadDeadline = 2 * time.Second
	defaultWriteTimeout = 30 * time.Second
)

type Connection struct {
	conn        net.Conn
	clientID    string
	host        string
	port        int
	lastUsed    int64 // atomic
	connectTime int64 // atomic
	closeOnce   sync.Once
	closed      chan struct{}
	isConnected atomic.Bool
}

func NewConnection(host string, port int) *Connection {
	return &Connection{
		host:     host,
		port:     port,
		closed:   make(chan struct{}),
		lastUsed: time.Now().UnixNano(),
	}
}

func (c *Connection) Connect(ctx context.Context) error {
	if c.isConnected.Load() {
		return nil
	}

	addr := fmt.Sprintf("%s:%d", c.host, c.port)
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("connect to %s: %w", addr, err)
	}

	c.conn = conn
	c.isConnected.Store(true)
	now := time.Now().UnixNano()
	atomic.StoreInt64(&c.connectTime, now)
	atomic.StoreInt64(&c.lastUsed, now)

	return nil
}

func (c *Connection) readProto(msg proto.Message) error {
	var length uint32
	if err := binary.Read(c.conn, binary.BigEndian, &length); err != nil {
		return err
	}

	buf := make([]byte, length)
	if _, err := io.ReadFull(c.conn, buf); err != nil {
		return err
	}

	return proto.Unmarshal(buf, msg)
}

func (c *Connection) Execute(ctx context.Context, cmd *comm.Command) (*comm.Response, error) {
	if !c.isConnected.Load() || c.conn == nil {
		return nil, errors.New("connection not available")
	}

	data, err := proto.Marshal(cmd)
	if err != nil {
		return nil, fmt.Errorf("marshal command: %w", err)
	}

	var buf bytes.Buffer
	if err := binary.Write(&buf, binary.BigEndian, uint32(len(data))); err != nil {
		return nil, fmt.Errorf("write length prefix: %w", err)
	}
	if _, err := buf.Write(data); err != nil {
		return nil, fmt.Errorf("write data: %w", err)
	}

	deadline, hasDeadline := ctx.Deadline()
	if hasDeadline {
		_ = c.conn.SetWriteDeadline(deadline)
	} else {
		_ = c.conn.SetWriteDeadline(time.Now().Add(defaultWriteTimeout))
	}

	if _, err := c.conn.Write(buf.Bytes()); err != nil {
		return nil, fmt.Errorf("write to connection: %w", err)
	}

	var resp comm.Response
	if err := c.readProto(&resp); err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	atomic.StoreInt64(&c.lastUsed, time.Now().UnixNano())
	return &resp, nil
}

func (c *Connection) Close() error {
	var closeErr error
	c.closeOnce.Do(func() {
		close(c.closed)
		c.isConnected.Store(false)
		if c.conn != nil {
			closeErr = c.conn.Close()
		}
	})
	return closeErr
}

func (c *Connection) IsHealthy() bool {
	return c.isConnected.Load() && c.conn != nil
}

func (c *Connection) SetClientID(id string) {
	c.clientID = id
}

func (c *Connection) GetClientID() string {
	return c.clientID
}

func (c *Connection) GetLastUsed() time.Time {
	return time.Unix(0, atomic.LoadInt64(&c.lastUsed))
}

func (c *Connection) GetConnectTime() time.Time {
	return time.Unix(0, atomic.LoadInt64(&c.connectTime))
}
