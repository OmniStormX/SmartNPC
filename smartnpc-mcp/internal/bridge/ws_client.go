// WebSocket client for the SMAPI mod bridge.
//
// Responsibilities:
//   - Connect (and re-connect with backoff) to ws://127.0.0.1:18745/ws
//   - Send Request frames and correlate Response frames by ID
//   - Surface server-pushed Event frames via OnEvent
//
// The client is safe for concurrent Call invocations from multiple goroutines.
package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"
)

// DefaultWSURL is the address the SMAPI mod listens on by default.
const DefaultWSURL = "ws://127.0.0.1:18745/ws"

// Default timeouts. Override via WSClientOptions.
const (
	defaultCallTimeout  = 10 * time.Second
	defaultDialTimeout  = 5 * time.Second
	defaultRetryBackoff = 2 * time.Second
)

// EventHandler receives server-pushed events. Called from the read loop, so
// long work should be offloaded to a goroutine.
type EventHandler func(ctx context.Context, name string, data json.RawMessage)

// WSClientOptions configures NewWSClient.
type WSClientOptions struct {
	URL          string
	Logger       *slog.Logger
	CallTimeout  time.Duration
	DialTimeout  time.Duration
	RetryBackoff time.Duration
	OnEvent      EventHandler
}

// WSClient is a connection-managed JSON-RPC client to the mod's ws server.
type WSClient struct {
	opts WSClientOptions
	log  *slog.Logger

	mu      sync.Mutex
	conn    *websocket.Conn
	pending map[string]chan *Response
	closed  bool
}

// NewWSClient creates an unconnected client. Call Connect to establish.
func NewWSClient(opts WSClientOptions) *WSClient {
	if opts.URL == "" {
		opts.URL = DefaultWSURL
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.CallTimeout <= 0 {
		opts.CallTimeout = defaultCallTimeout
	}
	if opts.DialTimeout <= 0 {
		opts.DialTimeout = defaultDialTimeout
	}
	if opts.RetryBackoff <= 0 {
		opts.RetryBackoff = defaultRetryBackoff
	}
	return &WSClient{
		opts:    opts,
		log:     opts.Logger,
		pending: make(map[string]chan *Response),
	}
}

// Connect dials the server and starts the read loop. The loop will
// auto-reconnect with backoff until ctx is cancelled.
func (c *WSClient) Connect(ctx context.Context) error {
	if err := c.dial(ctx); err != nil {
		// First connect failure is returned so callers can log it, but the
		// background reconnect loop still starts.
		go c.readLoopForever(ctx)
		return err
	}
	go c.readLoopForever(ctx)
	return nil
}

// Close terminates the connection and prevents further calls.
func (c *WSClient) Close() error {
	c.mu.Lock()
	c.closed = true
	conn := c.conn
	c.conn = nil
	c.mu.Unlock()
	if conn != nil {
		return conn.Close(websocket.StatusNormalClosure, "")
	}
	return nil
}

// SetEventHandler swaps the event handler. Useful when the handler needs to
// reference the *WSClient itself (chicken-and-egg during construction).
// Concurrency-safe; the next received event uses the new handler.
func (c *WSClient) SetEventHandler(h EventHandler) {
	c.mu.Lock()
	c.opts.OnEvent = h
	c.mu.Unlock()
}

// Call sends a request and blocks until the server responds, the per-call
// timeout fires, or ctx is cancelled.
//
// On a non-OK response, Call returns an error wrapping the server-supplied
// code and message.
func (c *WSClient) Call(ctx context.Context, action string, params any) (json.RawMessage, error) {
	id := uuid.NewString()
	ch := make(chan *Response, 1)

	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, errors.New("ws client closed")
	}
	if c.conn == nil {
		c.mu.Unlock()
		return nil, errors.New("ws client not connected")
	}
	c.pending[id] = ch
	conn := c.conn
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}()

	req := Request{Type: TypeRequest, ID: id, Action: action, Params: params}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	callCtx, cancel := context.WithTimeout(ctx, c.opts.CallTimeout)
	defer cancel()

	if err := conn.Write(callCtx, websocket.MessageText, body); err != nil {
		return nil, fmt.Errorf("ws write: %w", err)
	}

	select {
	case <-callCtx.Done():
		return nil, callCtx.Err()
	case resp := <-ch:
		if resp == nil {
			return nil, errors.New("ws connection lost during call")
		}
		if !resp.OK {
			if resp.Error != nil {
				return nil, fmt.Errorf("mod error %s: %s", resp.Error.Code, resp.Error.Message)
			}
			return nil, errors.New("mod returned ok=false without error")
		}
		return resp.Data, nil
	}
}

// ── connection management ───────────────────────────────────────────

func (c *WSClient) dial(ctx context.Context) error {
	dialCtx, cancel := context.WithTimeout(ctx, c.opts.DialTimeout)
	defer cancel()
	conn, _, err := websocket.Dial(dialCtx, c.opts.URL, nil)
	if err != nil {
		return fmt.Errorf("ws dial %s: %w", c.opts.URL, err)
	}
	conn.SetReadLimit(1 << 20) // 1MiB per frame
	c.mu.Lock()
	c.conn = conn
	c.mu.Unlock()
	c.log.Info("ws connected", "url", c.opts.URL)
	return nil
}

func (c *WSClient) readLoopForever(ctx context.Context) {
	for {
		c.readLoopOnce(ctx)

		c.mu.Lock()
		closed := c.closed
		c.failPending(errors.New("connection lost"))
		c.conn = nil
		c.mu.Unlock()
		if closed || ctx.Err() != nil {
			return
		}

		c.log.Warn("ws disconnected, reconnecting", "after", c.opts.RetryBackoff)
		select {
		case <-ctx.Done():
			return
		case <-time.After(c.opts.RetryBackoff):
		}
		if err := c.dial(ctx); err != nil {
			c.log.Warn("ws reconnect failed", "err", err)
		}
	}
}

func (c *WSClient) readLoopOnce(ctx context.Context) {
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()
	if conn == nil {
		return
	}

	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			c.log.Debug("ws read ended", "err", err)
			return
		}
		c.dispatch(ctx, data)
	}
}

func (c *WSClient) dispatch(ctx context.Context, data []byte) {
	var head frameType
	if err := json.Unmarshal(data, &head); err != nil {
		c.log.Warn("ws frame: invalid json", "err", err)
		return
	}
	switch head.Type {
	case TypeResponse:
		var resp Response
		if err := json.Unmarshal(data, &resp); err != nil {
			c.log.Warn("ws response: invalid", "err", err)
			return
		}
		c.mu.Lock()
		ch, ok := c.pending[resp.ID]
		c.mu.Unlock()
		if !ok {
			c.log.Warn("ws response: no pending call", "id", resp.ID)
			return
		}
		select {
		case ch <- &resp:
		default:
		}
	case TypeEvent:
		var evt Event
		if err := json.Unmarshal(data, &evt); err != nil {
			c.log.Warn("ws event: invalid", "err", err)
			return
		}
		if c.opts.OnEvent != nil {
			c.mu.Lock()
			h := c.opts.OnEvent
			c.mu.Unlock()
			h(ctx, evt.Name, evt.Data)
		}
	default:
		c.log.Warn("ws frame: unknown type", "type", head.Type)
	}
}

func (c *WSClient) failPending(err error) {
	for id, ch := range c.pending {
		select {
		case ch <- nil:
		default:
		}
		_ = id
	}
}
