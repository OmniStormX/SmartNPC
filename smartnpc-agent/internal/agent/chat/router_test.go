package chat

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/smartnpc/smartnpc-agent/internal/llm"
)

// newRoutedAgent constructs an Agent with a mockProvider and the agent's
// replyDone channel wired so tests can block until a dispatch completes
// without sleeping.
func newRoutedAgent(speaker string, reply string, done chan<- struct{}) (*Agent, *mockProvider) {
	mp := &mockProvider{replies: []llm.ChatResponse{
		{Content: reply, FinishReason: "stop"},
	}}
	a := New(Config{
		Provider:          mp,
		Speaker:           speaker,
		SystemPrompt:      "test " + speaker,
		MaxHistory:        10,
		FriendshipTimeout: -1,
	})
	a.replyDone = done
	return a, mp
}

// ── construction ─────────────────────────────────────────────

func TestNewRouter_SkipsNilAndEmptySpeakers(t *testing.T) {
	a, _ := newRoutedAgent("Abigail", "reply", nil)
	r := NewRouterFromAgents([]*Agent{nil, a, New(Config{Speaker: "", Provider: &mockProvider{}})})
	if got := r.Speakers(); len(got) != 1 || got[0] != "Abigail" {
		t.Errorf("speakers = %v, want [Abigail]", got)
	}
}

func TestNewRouter_OrderPreserved(t *testing.T) {
	a1, _ := newRoutedAgent("Abigail", "a", nil)
	a2, _ := newRoutedAgent("XiaMi", "x", nil)
	a3, _ := newRoutedAgent("Harvey", "h", nil)
	r := NewRouterFromAgents([]*Agent{a1, a2, a3})
	got := r.Speakers()
	want := []string{"Abigail", "XiaMi", "Harvey"}
	if len(got) != len(want) {
		t.Fatalf("speakers len=%d want %d", len(got), len(want))
	}
	for i, s := range want {
		if got[i] != s {
			t.Errorf("speakers[%d]=%q want %q", i, got[i], s)
		}
	}
}

// ── routing: chat_message with npc field ─────────────────────

// fakeLoggingRequest builds an mcp.LoggingMessageRequest carrying a
// stardew/event payload exactly the way mcpclient re-wraps them. Using the
// same layout as the production path keeps the test honest.
func fakeLoggingRequest(name string, data map[string]any) *mcp.LoggingMessageRequest {
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

func waitDone(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for respondAndSay to signal done")
	}
}

func TestRouter_ChatMessageRoutesToMatchingAgent(t *testing.T) {
	done := make(chan struct{}, 4)
	a1, mp1 := newRoutedAgent("Abigail", "a-reply", done)
	a2, mp2 := newRoutedAgent("XiaMi", "x-reply", done)

	r := NewRouterFromAgents([]*Agent{a1, a2})
	h := r.HandleNotification()

	h(context.Background(), fakeLoggingRequest("chat_message", map[string]any{
		"npc": "XiaMi", "text": "hi xiami", "source": "player",
	}))
	waitDone(t, done)

	if len(mp2.calls) != 1 {
		t.Errorf("XiaMi provider calls = %d, want 1", len(mp2.calls))
	}
	if len(mp1.calls) != 0 {
		t.Errorf("Abigail provider must not fire, got %d calls", len(mp1.calls))
	}
}

func TestRouter_ChatMessageIgnoredWhenSpeakerUnknown(t *testing.T) {
	done := make(chan struct{}, 2)
	a1, mp1 := newRoutedAgent("Abigail", "a-reply", done)
	r := NewRouterFromAgents([]*Agent{a1})
	h := r.HandleNotification()

	// Single-agent router short-circuits to the agent's own handler which
	// filters by its own speaker; "Ghost" must not fire Abigail.
	h(context.Background(), fakeLoggingRequest("chat_message", map[string]any{
		"npc": "Ghost", "text": "who?", "source": "player",
	}))

	// No dispatch expected → drain the done channel briefly; if nothing
	// arrives in 50ms we're good.
	select {
	case <-done:
		t.Error("unknown speaker should not trigger dispatch")
	case <-time.After(50 * time.Millisecond):
	}
	if len(mp1.calls) != 0 {
		t.Errorf("Abigail should not fire for unknown speaker, got %d calls", len(mp1.calls))
	}
}

func TestRouter_ChatMessageCaseInsensitive(t *testing.T) {
	done := make(chan struct{}, 2)
	a, mp := newRoutedAgent("Abigail", "reply", done)
	r := NewRouterFromAgents([]*Agent{a, mustAgent("XiaMi", done)})
	h := r.HandleNotification()

	// Event lowercases the speaker name.
	h(context.Background(), fakeLoggingRequest("chat_message", map[string]any{
		"npc": "abigail", "text": "hey", "source": "player",
	}))
	waitDone(t, done)
	if len(mp.calls) != 1 {
		t.Errorf("case-insensitive match failed, calls=%d", len(mp.calls))
	}
}

func mustAgent(speaker string, done chan<- struct{}) *Agent {
	a, _ := newRoutedAgent(speaker, "reply", done)
	return a
}

// ── routing: npc_interact ────────────────────────────────────

func TestRouter_NpcInteractRoutesAndInjectsGreetingStub(t *testing.T) {
	done := make(chan struct{}, 2)
	a1, mp1 := newRoutedAgent("Abigail", "a", done)
	a2, mp2 := newRoutedAgent("Harvey", "h", done)
	r := NewRouterFromAgents([]*Agent{a1, a2})
	h := r.HandleNotification()

	h(context.Background(), fakeLoggingRequest("npc_interact", map[string]any{
		"npc": "Harvey", "source": "player",
	}))
	waitDone(t, done)

	if len(mp2.calls) != 1 {
		t.Fatalf("Harvey provider should fire once, got %d", len(mp2.calls))
	}
	if len(mp1.calls) != 0 {
		t.Errorf("Abigail must not fire, got %d", len(mp1.calls))
	}
	// The stub prompt injected by the router must reach the LLM.
	var sawStub bool
	for _, m := range mp2.calls[0].Messages {
		if m.Role == llm.RoleUser && strings.Contains(m.Content, "玩家走过来点击") {
			sawStub = true
		}
	}
	if !sawStub {
		t.Errorf("npc_interact stub prompt not found in LLM messages")
	}
}

// ── routing: chat_received falls back to lastActive ──────────

func TestRouter_ChatReceivedFollowsLastActive(t *testing.T) {
	done := make(chan struct{}, 4)
	a1, mp1 := newRoutedAgent("Abigail", "a", done)
	a2, mp2 := newRoutedAgent("XiaMi", "x", done)
	r := NewRouterFromAgents([]*Agent{a1, a2})
	h := r.HandleNotification()

	// Turn 1: XiaMi is addressed explicitly → becomes lastActive.
	h(context.Background(), fakeLoggingRequest("chat_message", map[string]any{
		"npc": "XiaMi", "text": "hello xiami", "source": "player",
	}))
	waitDone(t, done)

	// Turn 2: global chat_received (no npc field) → should route to XiaMi.
	h(context.Background(), fakeLoggingRequest("chat_received", map[string]any{
		"text": "follow-up without name", "source": "player",
	}))
	waitDone(t, done)

	if len(mp2.calls) != 2 {
		t.Errorf("XiaMi should receive both turns, got %d", len(mp2.calls))
	}
	if len(mp1.calls) != 0 {
		t.Errorf("Abigail must stay silent, got %d", len(mp1.calls))
	}
}

func TestRouter_ChatReceivedDroppedWhenNoLastActive(t *testing.T) {
	done := make(chan struct{}, 2)
	a1, mp1 := newRoutedAgent("Abigail", "a", done)
	a2, mp2 := newRoutedAgent("XiaMi", "x", done)
	r := NewRouterFromAgents([]*Agent{a1, a2})
	h := r.HandleNotification()

	// No prior targeted event → chat_received should be dropped.
	h(context.Background(), fakeLoggingRequest("chat_received", map[string]any{
		"text": "hi anyone", "source": "player",
	}))

	select {
	case <-done:
		t.Error("chat_received should be dropped when no lastActive speaker")
	case <-time.After(50 * time.Millisecond):
	}
	if len(mp1.calls)+len(mp2.calls) != 0 {
		t.Errorf("no agent should fire, got %d+%d calls", len(mp1.calls), len(mp2.calls))
	}
}

// ── isolated history per agent ───────────────────────────────

func TestRouter_HistoryIsolatedPerAgent(t *testing.T) {
	done := make(chan struct{}, 4)
	a1, _ := newRoutedAgent("Abigail", "a", done)
	a2, _ := newRoutedAgent("XiaMi", "x", done)
	r := NewRouterFromAgents([]*Agent{a1, a2})
	h := r.HandleNotification()

	h(context.Background(), fakeLoggingRequest("chat_message", map[string]any{
		"npc": "Abigail", "text": "secret to Abigail", "source": "player",
	}))
	waitDone(t, done)
	h(context.Background(), fakeLoggingRequest("chat_message", map[string]any{
		"npc": "XiaMi", "text": "different question to XiaMi", "source": "player",
	}))
	waitDone(t, done)

	// Abigail's history must not leak into XiaMi's and vice versa.
	a1.mu.Lock()
	a1History := append([]llm.Message(nil), a1.history...)
	a1.mu.Unlock()
	a2.mu.Lock()
	a2History := append([]llm.Message(nil), a2.history...)
	a2.mu.Unlock()

	for _, m := range a1History {
		if strings.Contains(m.Content, "XiaMi") || strings.Contains(m.Content, "different question") {
			t.Errorf("Abigail history leaked XiaMi turn: %+v", m)
		}
	}
	for _, m := range a2History {
		if strings.Contains(m.Content, "secret to Abigail") {
			t.Errorf("XiaMi history leaked Abigail turn: %+v", m)
		}
	}
}

// ── Register() API per spec ──────────────────────────────────

// TestRegister_AddsAgentByExplicitSpeaker verifies the spec'd flow:
// NewRouter() → Register(speaker, agent) → dispatch by speaker.
func TestRegister_AddsAgentByExplicitSpeaker(t *testing.T) {
	done := make(chan struct{}, 2)
	a1, mp1 := newRoutedAgent("Abigail", "a-reply", done)
	a2, mp2 := newRoutedAgent("XiaMi", "x-reply", done)

	r := NewRouter()
	r.Register("Abigail", a1)
	r.Register("XiaMi", a2)

	if got := r.Speakers(); len(got) != 2 || got[0] != "Abigail" || got[1] != "XiaMi" {
		t.Errorf("speakers after Register = %v", got)
	}

	h := r.HandleNotification()
	h(context.Background(), fakeLoggingRequest("chat_message", map[string]any{
		"npc": "XiaMi", "text": "via Register", "source": "player",
	}))
	waitDone(t, done)

	if len(mp2.calls) != 1 || len(mp1.calls) != 0 {
		t.Errorf("Register routing failed: xiami=%d abigail=%d", len(mp2.calls), len(mp1.calls))
	}
}

// TestRegister_IgnoresEmptyAndNil covers the "no-op" contract: empty
// speaker or nil agent must not panic or insert bogus rows.
func TestRegister_IgnoresEmptyAndNil(t *testing.T) {
	r := NewRouter()
	r.Register("", nil)
	r.Register("", &Agent{}) // empty speaker
	r.Register("Abigail", nil)
	if got := r.Speakers(); len(got) != 0 {
		t.Errorf("expected no speakers after no-op Register, got %v", got)
	}
}

// TestRegister_OverwriteKeepsOrder verifies that re-registering a speaker
// replaces the stored Agent but does not shuffle the insertion order.
func TestRegister_OverwriteKeepsOrder(t *testing.T) {
	done := make(chan struct{}, 2)
	a1, _ := newRoutedAgent("Abigail", "first", done)
	a1b, mp1b := newRoutedAgent("Abigail", "replaced", done)
	a2, _ := newRoutedAgent("XiaMi", "x", done)

	r := NewRouter()
	r.Register("Abigail", a1)
	r.Register("XiaMi", a2)
	r.Register("Abigail", a1b) // overwrite

	speakers := r.Speakers()
	if len(speakers) != 2 || speakers[0] != "Abigail" || speakers[1] != "XiaMi" {
		t.Errorf("order drifted after overwrite: %v", speakers)
	}

	h := r.HandleNotification()
	h(context.Background(), fakeLoggingRequest("chat_message", map[string]any{
		"npc": "Abigail", "text": "after overwrite", "source": "player",
	}))
	waitDone(t, done)
	if len(mp1b.calls) != 1 {
		t.Errorf("overwrite agent did not receive dispatch; calls=%d", len(mp1b.calls))
	}
}
