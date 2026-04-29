package tools

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/smartnpc/smartnpc-mcp/internal/bridge"
)

func TestChatSayEndToEnd(t *testing.T) {
	var got ChatSayInput
	srv := bridge.NewTestServer(func(_ context.Context, action string, params json.RawMessage) (any, error) {
		if action != bridge.ActionChatSay {
			t.Errorf("action=%s", action)
		}
		_ = json.Unmarshal(params, &got)
		return ChatSayOutput{OK: true}, nil
	})
	defer srv.Close()

	br := bridge.NewWSClient(bridge.WSClientOptions{URL: srv.URL_WS()})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := br.Connect(ctx); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer br.Close()

	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "test"}, nil)
	registerChat(server, br)

	t1, t2 := mcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, t1, nil); err != nil {
		t.Fatalf("server: %v", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "t"}, nil)
	cs, err := client.Connect(ctx, t2, nil)
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	defer cs.Close()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "chat_say",
		Arguments: map[string]any{
			"speaker": "SmartNPC",
			"text":    "hello there",
			"color":   "yellow",
		},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError: %v", res.Content)
	}
	if got.Speaker != "SmartNPC" || got.Text != "hello there" || got.Color != "yellow" {
		t.Errorf("got=%+v", got)
	}
}

func TestChatSay_RejectsMissingFields(t *testing.T) {
	srv := bridge.NewTestServer(func(context.Context, string, json.RawMessage) (any, error) {
		t.Fatal("handler should not be reached")
		return nil, nil
	})
	defer srv.Close()
	br := bridge.NewWSClient(bridge.WSClientOptions{URL: srv.URL_WS()})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := br.Connect(ctx); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer br.Close()

	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "test"}, nil)
	registerChat(server, br)
	t1, t2 := mcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, t1, nil); err != nil {
		t.Fatalf("server: %v", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "t"}, nil)
	cs, err := client.Connect(ctx, t2, nil)
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	defer cs.Close()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "chat_say",
		Arguments: map[string]any{"speaker": "SmartNPC"}, // missing text
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Error("expected IsError=true")
	}
}
