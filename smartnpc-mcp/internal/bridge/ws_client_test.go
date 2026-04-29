package bridge

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"
)

func TestWSClient_CallSuccess(t *testing.T) {
	srv := NewTestServer(func(_ context.Context, action string, params json.RawMessage) (any, error) {
		if action != "echo" {
			t.Errorf("action=%s", action)
		}
		var p struct{ Text string }
		_ = json.Unmarshal(params, &p)
		return map[string]any{"got": p.Text}, nil
	})
	defer srv.Close()

	c := NewWSClient(WSClientOptions{URL: srv.URL_WS()})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := c.Connect(ctx); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer c.Close()

	data, err := c.Call(ctx, "echo", map[string]any{"Text": "hi"})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	var got struct{ Got string }
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("decode: %v (raw=%s)", err, data)
	}
	if got.Got != "hi" {
		t.Errorf("got=%q want hi", got.Got)
	}
}

func TestWSClient_ServerError(t *testing.T) {
	srv := NewTestServer(func(context.Context, string, json.RawMessage) (any, error) {
		return nil, &testErr{msg: "boom"}
	})
	defer srv.Close()

	c := NewWSClient(WSClientOptions{URL: srv.URL_WS()})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := c.Connect(ctx); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer c.Close()

	_, err := c.Call(ctx, "fail", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestWSClient_EventDispatch(t *testing.T) {
	var (
		mu       sync.Mutex
		gotName  string
		gotData  json.RawMessage
		received = make(chan struct{}, 1)
	)
	srv := NewTestServer(func(context.Context, string, json.RawMessage) (any, error) {
		return map[string]any{"ok": true}, nil
	})
	defer srv.Close()

	c := NewWSClient(WSClientOptions{
		URL: srv.URL_WS(),
		OnEvent: func(_ context.Context, name string, data json.RawMessage) {
			mu.Lock()
			gotName = name
			gotData = data
			mu.Unlock()
			received <- struct{}{}
		},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := c.Connect(ctx); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer c.Close()

	// Trigger one Call to ensure the connection is fully established before pushing.
	if _, err := c.Call(ctx, "warmup", nil); err != nil {
		t.Fatalf("warmup: %v", err)
	}
	if err := srv.PushEvent("chat_received", map[string]any{"text": "hello", "source": "player"}); err != nil {
		t.Fatalf("push: %v", err)
	}

	select {
	case <-received:
	case <-time.After(2 * time.Second):
		t.Fatal("event not received")
	}
	mu.Lock()
	defer mu.Unlock()
	if gotName != "chat_received" {
		t.Errorf("name=%q", gotName)
	}
	var d struct{ Text, Source string }
	if err := json.Unmarshal(gotData, &d); err != nil {
		t.Fatalf("decode: %v (raw=%s)", err, gotData)
	}
	if d.Text != "hello" || d.Source != "player" {
		t.Errorf("data=%+v", d)
	}
}

type testErr struct{ msg string }

func (e *testErr) Error() string { return e.msg }
