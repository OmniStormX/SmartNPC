package chat

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/smartnpc/smartnpc-agent/internal/llm"
)

// mockProvider is a test double for llm.Provider.
type mockProvider struct {
	mu      sync.Mutex
	calls   []llm.ChatRequest
	replies []llm.ChatResponse
	idx     int
	// err, when non-nil, is returned from every Chat call so tests can
	// exercise error-handling paths (e.g. decision-stage failure fallbacks).
	err error
}

func (m *mockProvider) Name() string { return "mock" }

func (m *mockProvider) Chat(_ context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, req)
	if m.err != nil {
		return nil, m.err
	}
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

func TestConvertMCPTools_TableDriven(t *testing.T) {
	cases := []struct {
		name  string
		tools []*mcp.Tool
		want  []llm.ToolSpec
	}{
		{
			name:  "nil input returns empty slice",
			tools: nil,
			want:  []llm.ToolSpec{},
		},
		{
			name:  "skips tool with empty name",
			tools: []*mcp.Tool{{Name: "", Description: "no-op"}},
			want:  []llm.ToolSpec{},
		},
		{
			name: "map schema passed through",
			tools: []*mcp.Tool{{
				Name:        "chat_say",
				Description: "send chat",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"text": map[string]any{"type": "string"},
					},
					"required": []any{"text"},
				},
			}},
			want: []llm.ToolSpec{{
				Name:        "chat_say",
				Description: "send chat",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"text": map[string]any{"type": "string"},
					},
					"required": []any{"text"},
				},
			}},
		},
		{
			name: "json.RawMessage schema is normalized to map",
			tools: []*mcp.Tool{{
				Name:        "game_get_time",
				InputSchema: json.RawMessage(`{"type":"object","properties":{}}`),
			}},
			want: []llm.ToolSpec{{
				Name: "game_get_time",
				InputSchema: map[string]any{
					"type":       "object",
					"properties": map[string]any{},
				},
			}},
		},
		{
			name: "nil schema falls back to permissive object",
			tools: []*mcp.Tool{{
				Name:        "ping",
				InputSchema: nil,
			}},
			want: []llm.ToolSpec{{
				Name: "ping",
				InputSchema: map[string]any{
					"type":       "object",
					"properties": map[string]any{},
				},
			}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := convertMCPTools(tc.tools)
			if len(got) != len(tc.want) {
				t.Fatalf("len mismatch: got %d, want %d (%+v)", len(got), len(tc.want), got)
			}
			for i := range got {
				if got[i].Name != tc.want[i].Name {
					t.Errorf("name[%d]: got %q, want %q", i, got[i].Name, tc.want[i].Name)
				}
				if got[i].Description != tc.want[i].Description {
					t.Errorf("desc[%d]: got %q, want %q", i, got[i].Description, tc.want[i].Description)
				}
				gotJSON, _ := json.Marshal(got[i].InputSchema)
				wantJSON, _ := json.Marshal(tc.want[i].InputSchema)
				if string(gotJSON) != string(wantJSON) {
					t.Errorf("schema[%d]: got %s, want %s", i, gotJSON, wantJSON)
				}
			}
		})
	}
}

func TestNormalizeSchema_TableDriven(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want string // JSON representation for easy comparison
	}{
		{"nil", nil, `{"properties":{},"type":"object"}`},
		{
			"map passthrough",
			map[string]any{"type": "object", "properties": map[string]any{"x": map[string]any{"type": "number"}}},
			`{"properties":{"x":{"type":"number"}},"type":"object"}`,
		},
		{
			"raw message",
			json.RawMessage(`{"type":"object","required":["a"]}`),
			`{"properties":{},"required":["a"],"type":"object"}`,
		},
		{
			"struct with json tags",
			struct {
				Type string `json:"type"`
			}{Type: "object"},
			`{"properties":{},"type":"object"}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeSchema(tc.in)
			b, _ := json.Marshal(got)
			if string(b) != tc.want {
				t.Errorf("got %s, want %s", b, tc.want)
			}
		})
	}
}

// newEventRequest builds an MCP logging notification carrying a
// `stardew/event`-shaped payload, matching what tools.MakeEventForwarder
// actually produces on the wire.
func newEventRequest(name string, data map[string]any) *mcp.LoggingMessageRequest {
	return &mcp.LoggingMessageRequest{
		Params: &mcp.LoggingMessageParams{
			Data: map[string]any{
				"kind": "stardew/event",
				"name": name,
				"data": data,
			},
		},
	}
}

func TestExtractChatMessage_TableDriven(t *testing.T) {
	cases := []struct {
		name    string
		req     *mcp.LoggingMessageRequest
		wantNpc string
		wantTxt string
		wantOk  bool
	}{
		{
			name:    "valid",
			req:     newEventRequest("chat_message", map[string]any{"npc": "Abigail", "text": "hi"}),
			wantNpc: "Abigail",
			wantTxt: "hi",
			wantOk:  true,
		},
		{
			name:   "wrong event name",
			req:    newEventRequest("chat_received", map[string]any{"npc": "Abigail", "text": "hi"}),
			wantOk: false,
		},
		{
			name:   "missing text",
			req:    newEventRequest("chat_message", map[string]any{"npc": "Abigail"}),
			wantOk: false,
		},
		{
			name:   "missing npc",
			req:    newEventRequest("chat_message", map[string]any{"text": "hi"}),
			wantOk: false,
		},
		{
			name:   "nil request",
			req:    nil,
			wantOk: false,
		},
		{
			name: "json.RawMessage data payload",
			req: &mcp.LoggingMessageRequest{
				Params: &mcp.LoggingMessageParams{
					Data: map[string]any{
						"kind": "stardew/event",
						"name": "chat_message",
						"data": json.RawMessage(`{"npc":"Xiami","text":"hello"}`),
					},
				},
			},
			wantNpc: "Xiami",
			wantTxt: "hello",
			wantOk:  true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			npc, text, ok := extractChatMessage(tc.req)
			if ok != tc.wantOk || npc != tc.wantNpc || text != tc.wantTxt {
				t.Errorf("got (%q,%q,%v), want (%q,%q,%v)",
					npc, text, ok, tc.wantNpc, tc.wantTxt, tc.wantOk)
			}
		})
	}
}

func TestExtractNpcInteract_TableDriven(t *testing.T) {
	cases := []struct {
		name    string
		req     *mcp.LoggingMessageRequest
		wantNpc string
		wantOk  bool
	}{
		{
			name:    "valid",
			req:     newEventRequest("npc_interact", map[string]any{"npc": "Xiami"}),
			wantNpc: "Xiami",
			wantOk:  true,
		},
		{
			name:   "wrong event",
			req:    newEventRequest("chat_message", map[string]any{"npc": "Xiami"}),
			wantOk: false,
		},
		{
			name:   "missing npc",
			req:    newEventRequest("npc_interact", map[string]any{}),
			wantOk: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			npc, ok := extractNpcInteract(tc.req)
			if ok != tc.wantOk || npc != tc.wantNpc {
				t.Errorf("got (%q,%v), want (%q,%v)", npc, ok, tc.wantNpc, tc.wantOk)
			}
		})
	}
}

// waitFor blocks until ch fires or the deadline expires. Keeps event-driven
// tests deterministic without sleep loops.
func waitFor(t *testing.T, ch <-chan struct{}, d time.Duration, why string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(d):
		t.Fatalf("timeout waiting for %s", why)
	}
}

// TestHandleNotification_ChatMessage verifies the full event path:
// MCP notification -> HandleNotification -> respond() -> chat_say tool call.
// Uses an in-memory MCP session + mock LLM so no sleep / real process is needed.
func TestHandleNotification_ChatMessage(t *testing.T) {
	agent, calls := newInMemoryAgent(t, "chat_say")
	mp := &mockProvider{replies: []llm.ChatResponse{
		{Content: "hi back", FinishReason: "stop"},
	}}
	agent.cfg.Provider = mp

	done := make(chan struct{}, 1)
	agent.mu.Lock()
	agent.replyDone = done
	agent.mu.Unlock()

	handler := agent.HandleNotification()
	handler(context.Background(), newEventRequest("chat_message", map[string]any{
		"npc": "NPC", "text": "hello there",
	}))

	waitFor(t, done, 2*time.Second, "chat_message dispatch")

	if len(mp.calls) != 1 {
		t.Fatalf("expected 1 LLM call, got %d", len(mp.calls))
	}
	lastUser := mp.calls[0].Messages[len(mp.calls[0].Messages)-1]
	if lastUser.Role != llm.RoleUser || lastUser.Content != "hello there" {
		t.Errorf("expected final user msg = 'hello there', got %+v", lastUser)
	}
	got := calls["chat_say"]
	if got == nil {
		t.Fatal("chat_say was not invoked on the MCP server")
	}
	if got["speaker"] != "NPC" || got["text"] != "hi back" {
		t.Errorf("chat_say args = %v", got)
	}
}

// TestHandleNotification_NpcInteract verifies clicking on an NPC triggers a
// proactive greeting (respond() + chat_say), without the player typing.
func TestHandleNotification_NpcInteract(t *testing.T) {
	agent, calls := newInMemoryAgent(t, "chat_say")
	mp := &mockProvider{replies: []llm.ChatResponse{
		{Content: "hey farmer", FinishReason: "stop"},
	}}
	agent.cfg.Provider = mp

	done := make(chan struct{}, 1)
	agent.mu.Lock()
	agent.replyDone = done
	agent.mu.Unlock()

	handler := agent.HandleNotification()
	handler(context.Background(), newEventRequest("npc_interact", map[string]any{
		"npc": "NPC",
	}))

	waitFor(t, done, 2*time.Second, "npc_interact dispatch")

	if len(mp.calls) != 1 {
		t.Fatalf("expected 1 LLM call, got %d", len(mp.calls))
	}
	if calls["chat_say"] == nil {
		t.Fatal("chat_say was not invoked after npc_interact")
	}
	if calls["chat_say"]["speaker"] != "NPC" || calls["chat_say"]["text"] != "hey farmer" {
		t.Errorf("chat_say args = %v", calls["chat_say"])
	}
}

// TestHandleNotification_IgnoresOtherNpcs verifies the agent drops events
// targeted at a different NPC (critical once multi-NPC support lands).
func TestHandleNotification_IgnoresOtherNpcs(t *testing.T) {
	agent, calls := newInMemoryAgent(t, "chat_say")
	mp := &mockProvider{}
	agent.cfg.Provider = mp

	done := make(chan struct{}, 1)
	agent.mu.Lock()
	agent.replyDone = done
	agent.mu.Unlock()

	handler := agent.HandleNotification()
	// Target a different NPC; handler should return before dispatching.
	handler(context.Background(), newEventRequest("chat_message", map[string]any{
		"npc": "OtherNPC", "text": "hey",
	}))

	// Give the goroutine a chance to run (if it was wrongly spawned).
	select {
	case <-done:
		t.Fatal("respondAndSay was dispatched for a non-matching NPC")
	case <-time.After(50 * time.Millisecond):
	}

	if len(mp.calls) != 0 {
		t.Errorf("expected 0 LLM calls, got %d", len(mp.calls))
	}
	if calls["chat_say"] != nil {
		t.Errorf("chat_say should not fire for other NPC, got %v", calls["chat_say"])
	}
}

// TestHandleNotification_MultiTurnHistory verifies conversation history
// accumulates across separate chat_message events dispatched to the same agent.
func TestHandleNotification_MultiTurnHistory(t *testing.T) {
	agent, _ := newInMemoryAgent(t, "chat_say")
	mp := &mockProvider{replies: []llm.ChatResponse{
		{Content: "first", FinishReason: "stop"},
		{Content: "second", FinishReason: "stop"},
	}}
	agent.cfg.Provider = mp

	done := make(chan struct{}, 2)
	agent.mu.Lock()
	agent.replyDone = done
	agent.mu.Unlock()

	handler := agent.HandleNotification()
	handler(context.Background(), newEventRequest("chat_message", map[string]any{
		"npc": "NPC", "text": "turn1",
	}))
	waitFor(t, done, 2*time.Second, "first dispatch")
	handler(context.Background(), newEventRequest("chat_message", map[string]any{
		"npc": "NPC", "text": "turn2",
	}))
	waitFor(t, done, 2*time.Second, "second dispatch")

	if len(mp.calls) != 2 {
		t.Fatalf("expected 2 LLM calls, got %d", len(mp.calls))
	}
	// Second call must contain turn1's user+assistant history plus turn2.
	second := mp.calls[1].Messages
	// system + user(turn1) + assistant(first) + user(turn2)
	if len(second) != 4 {
		t.Fatalf("expected 4 messages in 2nd LLM call, got %d: %+v", len(second), second)
	}
	if second[1].Content != "turn1" || second[2].Content != "first" || second[3].Content != "turn2" {
		t.Errorf("history order wrong: %+v", second)
	}
}
