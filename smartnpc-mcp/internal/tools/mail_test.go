package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/smartnpc/smartnpc-mcp/internal/bridge"
)

// TestMailSendEndToEnd wires up an MCP server that delegates to a fake mod
// (httptest.Server) and verifies the full path: CallTool -> bridge.Client ->
// HTTP POST -> structured response back through MCP.
func TestMailSendEndToEnd(t *testing.T) {
	// Fake SMAPI mod: accepts POST /mail_send, echoes ok:true.
	var receivedText string
	mod := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/mail_send" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		var body MailSendInput
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		receivedText = body.Text
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(MailSendOutput{OK: true, Message: "displayed"})
	}))
	defer mod.Close()

	// MCP server with the mail tool wired to our fake mod.
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "test"}, nil)
	registerMail(server, bridge.NewClient(mod.URL))

	ctx := context.Background()
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
		t.Errorf("mod received %q, want %q", receivedText, "Hi from SmartNPC!")
	}

	b, _ := json.Marshal(res.StructuredContent)
	var out MailSendOutput
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !out.OK || out.Message != "displayed" {
		t.Errorf("output = %+v, want OK=true Message=displayed", out)
	}
}

func TestMailSend_RejectsEmptyText(t *testing.T) {
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "test"}, nil)
	// Use a bogus URL — the call must be rejected before any HTTP traffic.
	registerMail(server, bridge.NewClient("http://127.0.0.1:1"))

	ctx := context.Background()
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
		t.Fatalf("CallTool error: %v", err)
	}
	if !res.IsError {
		t.Error("expected IsError=true for empty text")
	}
}
