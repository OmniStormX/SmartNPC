package chat

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/smartnpc/smartnpc-agent/internal/llm"
)

// mockProvider is a test double for llm.Provider.
type mockProvider struct {
	mu      sync.Mutex
	calls   []llm.ChatRequest
	replies []llm.ChatResponse
	idx     int
}

func (m *mockProvider) Name() string { return "mock" }

func (m *mockProvider) Chat(_ context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, req)
	if m.idx < len(m.replies) {
		r := m.replies[m.idx]
		m.idx++
		return &r, nil
	}
	return &llm.ChatResponse{Content: "mock reply", FinishReason: "stop"}, nil
}

func TestRespond_BasicReply(t *testing.T) {
	mp := &mockProvider{replies: []llm.ChatResponse{
		{Content: "Hello farmer!", FinishReason: "stop"},
	}}
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

	if len(mp.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(mp.calls))
	}
	msgs := mp.calls[0].Messages
	if msgs[0].Role != llm.RoleSystem || msgs[0].Content != "You are a test NPC." {
		t.Errorf("unexpected system msg: %+v", msgs[0])
	}
	if msgs[1].Role != llm.RoleUser || msgs[1].Content != "Hi there" {
		t.Errorf("unexpected user msg: %+v", msgs[1])
	}
}

func TestRespond_ToolCallLoop(t *testing.T) {
	mp := &mockProvider{replies: []llm.ChatResponse{
		// Round 1: model requests a tool call
		{
			ToolCalls: []llm.ToolCall{
				{ID: "call_1", Name: "ping", Arguments: map[string]any{"message": "test"}},
			},
			FinishReason: "tool_calls",
		},
		// Round 2: model gives final text after seeing tool result
		{Content: "Pong received!", FinishReason: "stop"},
	}}
	agent := New(Config{
		Provider:     mp,
		Speaker:      "NPC",
		SystemPrompt: "test",
		MaxHistory:   10,
	})
	// Give agent a fake session — tool execution will fail but we test the loop.
	// We can't easily mock MCP session, so just verify the provider gets called twice.

	reply, err := agent.respond(context.Background(), "ping please")
	if err != nil {
		t.Fatal(err)
	}
	if reply != "Pong received!" {
		t.Errorf("got %q, want %q", reply, "Pong received!")
	}
	if len(mp.calls) != 2 {
		t.Fatalf("expected 2 LLM calls (tool loop), got %d", len(mp.calls))
	}
	// Second call should include tool result message
	secondMsgs := mp.calls[1].Messages
	foundTool := false
	for _, m := range secondMsgs {
		if m.Role == llm.RoleTool {
			foundTool = true
			break
		}
	}
	if !foundTool {
		t.Error("second LLM call should contain a tool result message")
	}
}

func TestRespond_HistoryAccumulates(t *testing.T) {
	mp := &mockProvider{replies: []llm.ChatResponse{
		{Content: "r1", FinishReason: "stop"},
		{Content: "r2", FinishReason: "stop"},
		{Content: "r3", FinishReason: "stop"},
	}}
	agent := New(Config{Provider: mp, Speaker: "NPC", MaxHistory: 10})

	for _, input := range []string{"a", "b", "c"} {
		_, err := agent.respond(context.Background(), input)
		if err != nil {
			t.Fatal(err)
		}
	}

	// Third call: system + user "a" + asst "r1" + user "b" + asst "r2" + user "c" = 6
	last := mp.calls[2]
	if len(last.Messages) != 6 {
		t.Errorf("expected 6 messages in 3rd call, got %d", len(last.Messages))
	}
}

func TestRespond_HistoryTrimmed(t *testing.T) {
	mp := &mockProvider{}
	agent := New(Config{Provider: mp, Speaker: "NPC", MaxHistory: 2})

	for i := 0; i < 5; i++ {
		_, _ = agent.respond(context.Background(), "msg")
	}

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

func TestLoadPersona(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "test_persona.json")
	data := `{
		"speaker": "Sebastian",
		"name": "Sebastian",
		"personality": "Introverted programmer",
		"speaking_style": "Dry humor, short sentences",
		"background": "Lives in the basement of his parents house"
	}`
	if err := os.WriteFile(tmp, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}

	p, err := LoadPersona(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if p.Speaker != "Sebastian" {
		t.Errorf("speaker = %q, want Sebastian", p.Speaker)
	}
	if p.SystemPrompt == "" {
		t.Error("system prompt should not be empty")
	}
	if !contains(p.SystemPrompt, "Introverted programmer") {
		t.Errorf("system prompt should contain personality: %s", p.SystemPrompt)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsHelper(s, sub))
}

func containsHelper(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
