package tools

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/OmniStormX/SmartNPC/internal/bridge"
)

func TestMailSendEndToEnd(t *testing.T) {
	var receivedText string
	srv := bridge.NewTestServer(func(_ context.Context, action string, params json.RawMessage) (any, error) {
		if action != bridge.ActionMailSend {
			t.Errorf("action=%s want %s", action, bridge.ActionMailSend)
		}
		var p MailSendInput
		if err := json.Unmarshal(params, &p); err != nil {
			t.Fatalf("decode params: %v", err)
		}
		receivedText = p.Text
		return MailSendOutput{OK: true, Message: "displayed"}, nil
	})
	defer srv.Close()

	br := bridge.NewWSClient(bridge.WSClientOptions{URL: srv.URL_WS()})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := br.Connect(ctx); err != nil {
		t.Fatalf("ws connect: %v", err)
	}
	defer br.Close()

	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "test"}, nil)
	registerMail(server, br)

	t1, t2 := mcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, t1, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "client", Version: "test"}, nil)
	cs, err := client.Connect(ctx, t2, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer cs.Close()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "mail_send",
		Arguments: map[string]any{"text": "Hi from SmartNPC!"},
	})
	if err != nil {
		t.Fatalf("call mail_send: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool returned IsError: %v", res.Content)
	}
	if receivedText != "Hi from SmartNPC!" {
		t.Errorf("mod received %q", receivedText)
	}
	b, _ := json.Marshal(res.StructuredContent)
	var out MailSendOutput
	_ = json.Unmarshal(b, &out)
	if !out.OK || out.Message != "displayed" {
		t.Errorf("output = %+v", out)
	}
}

func TestMailSend_RejectsEmptyText(t *testing.T) {
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "test"}, nil)
	// nil bridge would crash before we reach the empty-text check, so use a
	// real client pointing at a server that always errors.
	srv := bridge.NewTestServer(func(context.Context, string, json.RawMessage) (any, error) {
		t.Fatal("handler should not be reached for empty text")
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
	registerMail(server, br)

	t1, t2 := mcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, t1, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "t"}, nil)
	cs, err := client.Connect(ctx, t2, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer cs.Close()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "mail_send",
		Arguments: map[string]any{"text": ""},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Error("expected IsError=true for empty text")
	}
}
