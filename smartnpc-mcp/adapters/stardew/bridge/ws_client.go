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

	// Agent registry: maps an MCP server-session id (string returned by
	// mcp.ServerSession.ID()) to the NPC profile name that session belongs
	// to. Populated by the agent_register_self tool. Read on every Call to
	// stamp the outbound Request.FromNPC field.
	agentMu sync.RWMutex
	agents  map[string]string // sessionID → npcName
}

// callCtxKey is the unexported context key used to propagate the originating
// MCP session ID into Call. Tool handlers should call WithCallSession before
// invoking br.Call so the resulting ws Request carries the right from_npc.
type callCtxKey struct{}

// WithCallSession returns a child context tagged with the given MCP server
// session ID. Tool handlers wrap their incoming context with this before
// calling br.Call so the ws Request frame can be stamped with the
// originating NPC profile.
//
// sessionID may be empty (e.g. unit tests using a single in-memory transport
// without a registered agent); in that case Call falls back to params.npc
// or skips the from_npc tag entirely.
func WithCallSession(ctx context.Context, sessionID string) context.Context {
	return context.WithValue(ctx, callCtxKey{}, sessionID)
}

// SessionFromContext returns the session ID previously stored by
// WithCallSession, or "" if none.
func SessionFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(callCtxKey{}).(string); ok {
		return v
	}
	return ""
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
		agents:  make(map[string]string),
	}
}

// soloSessionKey is the registry key used when the underlying MCP transport
// does not allocate per-session ids — namely stdio (single client by
// definition) and the in-memory transport used in tests. The HTTP
// streamable transport always issues a non-empty Mcp-Session-Id, so this
// fallback never collides with multi-client production usage.
const soloSessionKey = "__solo_session__"

// RegisterAgent records that the MCP session identified by sessionID is
// driven by the given NPC profile. Called by the agent_register_self tool.
// Re-registering with a different name overwrites the previous mapping.
//
// An empty sessionID is mapped to a synthetic single-session key so that
// stdio / InMemoryTransport (which don't issue session ids) still get a
// working binding. An empty npcName removes the mapping (de-registration);
// useful in tests.
func (c *WSClient) RegisterAgent(sessionID, npcName string) bool {
	key := sessionID
	if key == "" {
		key = soloSessionKey
	}
	c.agentMu.Lock()
	defer c.agentMu.Unlock()
	if npcName == "" {
		delete(c.agents, key)
		return true
	}
	c.agents[key] = npcName
	return true
}

// AgentForSession returns the NPC name registered for the given session ID,
// or "" if the session has not yet called agent_register_self.
//
// Empty sessionID resolves through the synthetic soloSessionKey so stdio /
// InMemoryTransport callers see the same binding they registered (see
// RegisterAgent for the rationale).
func (c *WSClient) AgentForSession(sessionID string) string {
	key := sessionID
	if key == "" {
		key = soloSessionKey
	}
	c.agentMu.RLock()
	defer c.agentMu.RUnlock()
	return c.agents[key]
}

// AgentForContext is the convenience wrapper used by tool handlers: it
// extracts the session ID from ctx (set by WithCallSession) and looks it up
// in the agent registry. Returns "" if either step fails.
func (c *WSClient) AgentForContext(ctx context.Context) string {
	return c.AgentForSession(SessionFromContext(ctx))
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
// The outbound ws Request frame's FromNPC field is auto-populated from the
// per-session agent registry when ctx carries a session id (set by
// WithCallSession in tool handlers). Operator-side callers that already
// know the originating NPC (e.g. scheduler debug fan-out) should use
// CallAs to stamp FromNPC explicitly.
//
// On a non-OK response, Call returns an error wrapping the server-supplied
// code and message.
//
// Logs every call's wall-clock duration at INFO so an operator can attribute
// Hermes round-trip latency to specific tool calls when reading mcp.log
// alongside hermesrelay's elapsed_ms summary.
func (c *WSClient) Call(ctx context.Context, action string, params any) (json.RawMessage, error) {
	return c.callInternal(ctx, c.AgentForContext(ctx), action, params)
}

// CallAs is identical to Call but stamps the outbound ws Request's FromNPC
// field with the given npcName, bypassing the per-session agent registry.
// Use when the caller has out-of-band knowledge of which NPC a tool call
// belongs to — currently the scheduler-debug schedule_trigger fan-out and
// the echo-mode chat_say are the only two such sites.
//
// An empty npcName is equivalent to Call (FromNPC unset, mod falls back to
// params.npc if any).
func (c *WSClient) CallAs(ctx context.Context, npcName, action string, params any) (json.RawMessage, error) {
	return c.callInternal(ctx, npcName, action, params)
}

func (c *WSClient) callInternal(ctx context.Context, fromNPC, action string, params any) (json.RawMessage, error) {
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

	req := Request{
		Type:    TypeRequest,
		ID:      id,
		Action:  action,
		Params:  params,
		FromNPC: fromNPC,
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	callCtx, cancel := context.WithTimeout(ctx, c.opts.CallTimeout)
	defer cancel()

	started := time.Now()
	if err := conn.Write(callCtx, websocket.MessageText, body); err != nil {
		c.log.Warn("ws action failed (write)",
			"action", action, "elapsed_ms", time.Since(started).Milliseconds(), "err", err)
		return nil, fmt.Errorf("ws write: %w", err)
	}

	select {
	case <-callCtx.Done():
		c.log.Warn("ws action timed out",
			"action", action, "elapsed_ms", time.Since(started).Milliseconds(), "err", callCtx.Err())
		return nil, callCtx.Err()
	case resp := <-ch:
		elapsed := time.Since(started).Milliseconds()
		if resp == nil {
			c.log.Warn("ws action lost connection",
				"action", action, "elapsed_ms", elapsed)
			return nil, errors.New("ws connection lost during call")
		}
		if !resp.OK {
			c.log.Warn("ws action returned not-ok",
				"action", action, "elapsed_ms", elapsed)
			if resp.Error != nil {
				return nil, fmt.Errorf("mod error %s: %s", resp.Error.Code, resp.Error.Message)
			}
			return nil, errors.New("mod returned ok=false without error")
		}
		c.log.Info("ws action",
			"action", action, "elapsed_ms", elapsed, "bytes_in", len(resp.Data))
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

	// Start a keepalive ping loop to prevent idle disconnects from the
	// C# HttpListener WebSocket server which may timeout idle connections.
	go c.pingLoop(ctx, conn)
	return nil
}

// pingLoop sends a Ping frame every 30 seconds to keep the connection alive.
// Exits when ctx is cancelled or the connection is replaced.
func (c *WSClient) pingLoop(ctx context.Context, conn *websocket.Conn) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.mu.Lock()
			current := c.conn
			c.mu.Unlock()
			// Stop if connection has been replaced or closed.
			if current != conn {
				return
			}
			pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			err := conn.Ping(pingCtx)
			cancel()
			if err != nil {
				return
			}
		}
	}
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
			c.log.Warn("ws read ended", "err", err, "detail", fmt.Sprintf("%+v", err))
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

// Connected reports whether the client currently has a live ws connection to
// the mod. Used by status reporters. Racy by design — a caller may see true
// here and still get EOF on the next Call; that is OK for status display.
func (c *WSClient) Connected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn != nil && !c.closed
}
