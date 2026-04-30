package chat

import (
	"context"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/smartnpc/smartnpc-agent/internal/llm"
)

// mockProvider is a test double for llm.Provider.
type mockProvider struct {
	mu      sync.Mutex
	calls   []llm.ChatRequest
	replies []string
	idx     int
}

func (m *mockProvider) Name() string { return "mock" }

func (m *mockProvider) Chat(_ context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, req)
	reply := "mock reply"
	if m.idx < len(m.replies) {
		reply = m.replies[m.idx]
		m.idx++
	}
	return &llm.ChatResponse{Content: reply, FinishReason: "stop"}, nil
}

func TestRespond_BasicReply(t *testing.T) {
	mp := &mockProvider{replies: []string{"Hello farmer!"}}
	agent := New(Config{
		Provider:     mp,
		Speaker:      "TestNPC",
		SystemPrompt: "You are a test NPC.",
		MaxHistory:   10,
	})

	reply, err := agent.respond(context.Background(), "Hi there")
	if err != nil {
		t.Fatal(err)
	}
	if reply != "Hello farmer!" {
		t.Errorf("got %q, want %q", reply, "Hello farmer!")
	}

	// Verify the request sent to provider
	if len(mp.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(mp.calls))
	}
	msgs := mp.calls[0].Messages
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages (system+user), got %d", len(msgs))
	}
	if msgs[0].Role != llm.RoleSystem || msgs[0].Content != "You are a test NPC." {
		t.Errorf("unexpected system msg: %+v", msgs[0])
	}
	if msgs[1].Role != llm.RoleUser || msgs[1].Content != "Hi there" {
		t.Errorf("unexpected user msg: %+v", msgs[1])
	}
}

func TestRespond_HistoryAccumulates(t *testing.T) {
	mp := &mockProvider{replies: []string{"r1", "r2", "r3"}}
	agent := New(Config{
		Provider:   mp,
		Speaker:    "NPC",
		MaxHistory: 10,
	})

	for _, input := range []string{"a", "b", "c"} {
		_, err := agent.respond(context.Background(), input)
		if err != nil {
			t.Fatal(err)
		}
	}

	// Third call should include system + 5 history messages (a,r1,b,r2,c)
	last := mp.calls[2]
	// system + user "a" + asst "r1" + user "b" + asst "r2" + user "c" = 6
	if len(last.Messages) != 6 {
		t.Errorf("expected 6 messages in 3rd call, got %d", len(last.Messages))
	}
}

func TestRespond_HistoryTrimmed(t *testing.T) {
	mp := &mockProvider{}
	agent := New(Config{
		Provider:   mp,
		Speaker:    "NPC",
		MaxHistory: 2, // Keep only last 4 messages (2 pairs)
	})

	// Send 5 messages to overflow history
	for i := 0; i < 5; i++ {
		_, _ = agent.respond(context.Background(), "msg")
	}

	// History should be capped at 4 (MaxHistory*2)
	agent.mu.Lock()
	hLen := len(agent.history)
	agent.mu.Unlock()
	if hLen > 4 {
		t.Errorf("history not trimmed: got %d, want <= 4", hLen)
	}
}

func TestExtractChatText_Valid(t *testing.T) {
	req := &mcp.LoggingMessageRequest{
		Params: &mcp.LoggingMessageParams{
			Data: map[string]any{
				"kind": "stardew/event",
				"name": "chat_received",
				"data": map[string]any{"text": "hello", "source": "player"},
			},
		},
	}
	text, ok := extractChatText(req, "NPC")
	if !ok || text != "hello" {
		t.Errorf("got (%q, %v), want (\"hello\", true)", text, ok)
	}
}

func TestExtractChatText_IgnoresSelf(t *testing.T) {
	req := &mcp.LoggingMessageRequest{
		Params: &mcp.LoggingMessageParams{
			Data: map[string]any{
				"kind": "stardew/event",
				"name": "chat_received",
				"data": map[string]any{"text": "echo", "source": "NPC"},
			},
		},
	}
	_, ok := extractChatText(req, "NPC")
	if ok {
		t.Error("should ignore own messages")
	}
}
