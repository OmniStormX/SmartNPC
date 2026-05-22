// Test helper: an in-process ws server speaking our protocol. Used by tools'
// unit tests to exercise the full ws client without spawning the SMAPI mod.
//
// Build tag-free so it can be reused from other packages' _test.go files.

package bridge

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// TestActionHandler is invoked by TestServer for each Request.
// Return data on success, or non-nil err to send a Response with ok=false.
type TestActionHandler func(ctx context.Context, action string, params json.RawMessage) (data any, err error)

// TestServer wraps an httptest.Server speaking the bridge protocol.
type TestServer struct {
	*httptest.Server
	OnRequest TestActionHandler

	mu     sync.Mutex
	conn   *websocket.Conn
	closed bool
}

// NewTestServer starts a ws server at /ws.
func NewTestServer(handler TestActionHandler) *TestServer {
	ts := &TestServer{OnRequest: handler}
	ts.Server = httptest.NewServer(http.HandlerFunc(ts.serveHTTP))
	return ts
}

// URL returns ws://... pointing at /ws.
func (s *TestServer) URL_WS() string {
	return "ws" + strings.TrimPrefix(s.Server.URL, "http") + "/ws"
}

// PushEvent sends a server event to the connected client (no-op if none).
func (s *TestServer) PushEvent(name string, data any) error {
	s.mu.Lock()
	conn := s.conn
	s.mu.Unlock()
	if conn == nil {
		return fmt.Errorf("no connected client")
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	evt := Event{Type: TypeEvent, Name: name, Data: raw, Timestamp: time.Now().UnixMilli()}
	body, _ := json.Marshal(evt)
	return conn.Write(context.Background(), websocket.MessageText, body)
}

// Close shuts down the test server.
func (s *TestServer) Close() {
	s.mu.Lock()
	s.closed = true
	conn := s.conn
	s.mu.Unlock()
	if conn != nil {
		_ = conn.Close(websocket.StatusNormalClosure, "shutdown")
	}
	s.Server.Close()
}

func (s *TestServer) serveHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/ws" {
		http.NotFound(w, r)
		return
	}
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	c.SetReadLimit(1 << 20)

	s.mu.Lock()
	s.conn = c
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		if s.conn == c {
			s.conn = nil
		}
		s.mu.Unlock()
		c.Close(websocket.StatusNormalClosure, "")
	}()

	ctx := r.Context()
	for {
		_, data, err := c.Read(ctx)
		if err != nil {
			return
		}

		var req Request
		if err := json.Unmarshal(data, &req); err != nil {
			continue
		}
		if req.Type != TypeRequest {
			continue
		}

		var paramsRaw json.RawMessage
		if req.Params != nil {
			paramsRaw, _ = json.Marshal(req.Params)
		}

		var resp Response
		if s.OnRequest == nil {
			resp = Response{Type: TypeResponse, ID: req.ID, OK: false,
				Error: &ResponseError{Code: "unhandled", Message: "no test handler"}}
		} else {
			result, herr := s.OnRequest(ctx, req.Action, paramsRaw)
			if herr != nil {
				resp = Response{Type: TypeResponse, ID: req.ID, OK: false,
					Error: &ResponseError{Code: "test_error", Message: herr.Error()}}
			} else {
				raw, _ := json.Marshal(result)
				resp = Response{Type: TypeResponse, ID: req.ID, OK: true, Data: raw}
			}
		}

		body, _ := json.Marshal(resp)
		_ = c.Write(ctx, websocket.MessageText, body)
	}
}
