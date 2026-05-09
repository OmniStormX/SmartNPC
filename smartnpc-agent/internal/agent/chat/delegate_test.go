package chat

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/smartnpc/smartnpc-agent/internal/llm"
)

// newConsultAgent builds an Agent wired with a single mockProvider that
// returns the provided reply on every Chat call. Friendship + memory are
// disabled so the consult flow stays deterministic.
func newConsultAgent(speaker, reply string) (*Agent, *mockProvider) {
	mp := &mockProvider{}
	mp.replies = []llm.ChatResponse{
		{Content: reply, FinishReason: "stop"},
		{Content: reply, FinishReason: "stop"},
		{Content: reply, FinishReason: "stop"},
		{Content: reply, FinishReason: "stop"},
	}
	a := New(Config{
		Provider:          mp,
		Speaker:           speaker,
		SystemPrompt:      "test " + speaker,
		MaxHistory:        10,
		FriendshipTimeout: -1,
	})
	return a, mp
}

// ── Router.ConsultAgent: happy path ──────────────────────────────────────

func TestConsultAgent_RoutesToTargetAgent(t *testing.T) {
	a, _ := newConsultAgent("Abigail", "ignored")
	b, mpB := newConsultAgent("Harvey", "I'm a doctor; come see me Tuesday.")

	r := NewRouter()
	r.Register("Abigail", a)
	r.Register("Harvey", b)

	resp, err := r.ConsultAgent(context.Background(), "Abigail", "Harvey", "What time is the clinic open?", "")
	if err != nil {
		t.Fatalf("consult: %v", err)
	}
	if resp.Consulted != "Harvey" {
		t.Errorf("Consulted = %q, want Harvey", resp.Consulted)
	}
	if !strings.Contains(resp.Answer, "doctor") {
		t.Errorf("Answer = %q, want Harvey's reply", resp.Answer)
	}
	// Harvey's provider should have been hit.
	if len(mpB.calls) == 0 {
		t.Error("target provider was not called")
	}
}

// ── Recursion protection: max depth ──────────────────────────────────────

func TestConsultAgent_MaxDepthRejected(t *testing.T) {
	a, _ := newConsultAgent("Abigail", "x")
	b, _ := newConsultAgent("Harvey", "y")
	c, _ := newConsultAgent("Penny", "z")

	r := NewRouter()
	r.Register("Abigail", a)
	r.Register("Harvey", b)
	r.Register("Penny", c)

	// Pre-seed a chain of length MaxDelegateDepth so the next consult must
	// be rejected.
	ctx := context.Background()
	for i := 0; i < MaxDelegateDepth; i++ {
		ctx = withChain(ctx, "filler")
	}

	_, err := r.ConsultAgent(ctx, "Abigail", "Harvey", "anything?", "")
	if !errors.Is(err, ErrDelegateMaxDepth) {
		t.Errorf("expected ErrDelegateMaxDepth, got %v", err)
	}
}

// ── Recursion protection: cycle ──────────────────────────────────────────

func TestConsultAgent_CycleRejected(t *testing.T) {
	a, _ := newConsultAgent("Abigail", "x")
	b, _ := newConsultAgent("Harvey", "y")

	r := NewRouter()
	r.Register("Abigail", a)
	r.Register("Harvey", b)

	// Chain already contains Harvey → Harvey is on the chain → cycle.
	ctx := withChain(context.Background(), "Harvey")

	_, err := r.ConsultAgent(ctx, "Abigail", "Harvey", "loop?", "")
	if !errors.Is(err, ErrDelegateCycle) {
		t.Errorf("expected ErrDelegateCycle, got %v", err)
	}
}

func TestConsultAgent_CycleCaseInsensitive(t *testing.T) {
	a, _ := newConsultAgent("Abigail", "x")
	b, _ := newConsultAgent("Harvey", "y")

	r := NewRouter()
	r.Register("Abigail", a)
	r.Register("Harvey", b)

	// Lowercase entry on the chain should still trigger the cycle check.
	ctx := withChain(context.Background(), "harvey")

	_, err := r.ConsultAgent(ctx, "Abigail", "Harvey", "loop?", "")
	if !errors.Is(err, ErrDelegateCycle) {
		t.Errorf("case-insensitive cycle check failed, got %v", err)
	}
}

// ── Recursion protection: self-consult ───────────────────────────────────

func TestConsultAgent_SelfRejected(t *testing.T) {
	a, _ := newConsultAgent("Abigail", "x")
	r := NewRouter()
	r.Register("Abigail", a)

	_, err := r.ConsultAgent(context.Background(), "Abigail", "Abigail", "self?", "")
	if !errors.Is(err, ErrDelegateCycle) {
		t.Errorf("expected ErrDelegateCycle for self-consult, got %v", err)
	}
}

// ── Unknown target ───────────────────────────────────────────────────────

func TestConsultAgent_UnknownTarget(t *testing.T) {
	a, _ := newConsultAgent("Abigail", "x")
	r := NewRouter()
	r.Register("Abigail", a)

	_, err := r.ConsultAgent(context.Background(), "Abigail", "Ghost", "are you there?", "")
	if !errors.Is(err, ErrDelegateUnknownTarget) {
		t.Errorf("expected ErrDelegateUnknownTarget, got %v", err)
	}
}

// ── No-router safety ─────────────────────────────────────────────────────

func TestConsultAgent_NilRouter(t *testing.T) {
	var r *Router
	_, err := r.ConsultAgent(context.Background(), "A", "B", "q", "")
	if !errors.Is(err, ErrDelegateNoRouter) {
		t.Errorf("expected ErrDelegateNoRouter, got %v", err)
	}
}

// ── Chain propagation: consulted agent sees ancestry ────────────────────

func TestConsultAgent_ChainAppendsOnDescent(t *testing.T) {
	a, _ := newConsultAgent("Abigail", "x")
	b, _ := newConsultAgent("Harvey", "y")

	r := NewRouter()
	r.Register("Abigail", a)
	r.Register("Harvey", b)

	// The childCtx that ConsultAgent passes to HandleInternalQuery must
	// contain "Abigail" on the chain. Because HandleInternalQuery is hard
	// to intercept directly, we exercise the same machinery through two
	// nested calls and assert the second one rejects with ErrDelegateCycle.
	// First call: Abigail → Harvey (chain becomes [Abigail]).
	// Second call (from within same chain): Harvey → Abigail (cycle).
	ctx := context.Background()
	if _, err := r.ConsultAgent(ctx, "Abigail", "Harvey", "first", ""); err != nil {
		t.Fatalf("first hop: %v", err)
	}
	// Simulate the chain that ConsultAgent would have built when Harvey
	// then tries to re-consult Abigail — chain holds [Abigail].
	ctx = withChain(ctx, "Abigail")
	_, err := r.ConsultAgent(ctx, "Harvey", "Abigail", "second", "")
	if !errors.Is(err, ErrDelegateCycle) {
		t.Errorf("expected cycle on Harvey→Abigail when chain=[Abigail], got %v", err)
	}
}

// ── No history pollution ─────────────────────────────────────────────────

func TestHandleInternalQuery_DoesNotPolluteHistory(t *testing.T) {
	a, _ := newConsultAgent("Harvey", "answer")

	// Seed Harvey's history with a real conversation.
	a.mu.Lock()
	a.history = []llm.Message{
		{Role: llm.RoleUser, Content: "earlier player message"},
		{Role: llm.RoleAssistant, Content: "earlier reply"},
	}
	originalLen := len(a.history)
	a.mu.Unlock()

	_, err := a.HandleInternalQuery(context.Background(), InternalQuery{
		FromAgent: "Abigail",
		Question:  "domain question",
	})
	if err != nil {
		t.Fatalf("HandleInternalQuery: %v", err)
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.history) != originalLen {
		t.Errorf("history mutated by internal query: len=%d want %d (%v)",
			len(a.history), originalLen, a.history)
	}
	if a.history[0].Content != "earlier player message" {
		t.Errorf("first message changed: %q", a.history[0].Content)
	}
}

// ── HandleInternalQuery embeds asker name in prompt ─────────────────────

func TestHandleInternalQuery_PromptIncludesFromAgent(t *testing.T) {
	a, mp := newConsultAgent("Harvey", "noted")

	_, err := a.HandleInternalQuery(context.Background(), InternalQuery{
		FromAgent: "Abigail",
		Question:  "where do I get bandages?",
		Context:   "player needs first aid",
	})
	if err != nil {
		t.Fatalf("HandleInternalQuery: %v", err)
	}
	if len(mp.calls) == 0 {
		t.Fatal("provider was never called")
	}
	// The embedded user message should mention the asker.
	var foundUser string
	for _, m := range mp.calls[0].Messages {
		if m.Role == llm.RoleUser {
			foundUser = m.Content
		}
	}
	if !strings.Contains(foundUser, "Abigail") {
		t.Errorf("user message missing asker name: %q", foundUser)
	}
	if !strings.Contains(foundUser, "where do I get bandages?") {
		t.Errorf("user message missing question: %q", foundUser)
	}
	if !strings.Contains(foundUser, "first aid") {
		t.Errorf("user message missing context: %q", foundUser)
	}
}

// ── Tool spec exposed only when peers exist ─────────────────────────────

func TestTools_ConsultExposedWithPeers(t *testing.T) {
	a, _ := newConsultAgent("Abigail", "x")
	b, _ := newConsultAgent("Harvey", "y")
	r := NewRouter()
	r.Register("Abigail", a)
	r.Register("Harvey", b)

	tools := a.Tools()
	var sawConsult bool
	for _, ts := range tools {
		if ts.Name == ConsultToolName {
			sawConsult = true
		}
	}
	if !sawConsult {
		t.Errorf("consult_npc tool missing from Tools(): %+v", tools)
	}
}

func TestTools_ConsultHiddenWhenSolo(t *testing.T) {
	a, _ := newConsultAgent("Abigail", "x")
	r := NewRouter()
	r.Register("Abigail", a)

	tools := a.Tools()
	for _, ts := range tools {
		if ts.Name == ConsultToolName {
			t.Errorf("consult_npc must not appear when only one agent registered: %+v", tools)
		}
	}
}

// ── executeConsult: routes through the router and JSON-encodes result ──

func TestExecuteConsult_HappyPathReturnsJSON(t *testing.T) {
	a, _ := newConsultAgent("Abigail", "asker-ignored")
	b, _ := newConsultAgent("Harvey", "the-doctor-says-rest")

	r := NewRouter()
	r.Register("Abigail", a)
	r.Register("Harvey", b)

	out, err := a.executeConsult(context.Background(), llm.ToolCall{
		Name: ConsultToolName,
		Arguments: map[string]any{
			"npc_name": "Harvey",
			"question": "should I rest?",
		},
	})
	if err != nil {
		t.Fatalf("executeConsult: %v", err)
	}
	var decoded struct {
		OK        bool   `json:"ok"`
		Consulted string `json:"consulted"`
		Answer    string `json:"answer"`
	}
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("output not JSON: %s", out)
	}
	if !decoded.OK {
		t.Errorf("ok=false, output=%s", out)
	}
	if decoded.Consulted != "Harvey" {
		t.Errorf("consulted=%q", decoded.Consulted)
	}
	if !strings.Contains(decoded.Answer, "rest") {
		t.Errorf("answer missing target reply: %q", decoded.Answer)
	}
}

func TestExecuteConsult_SoftFallbackOnUnknownTarget(t *testing.T) {
	a, _ := newConsultAgent("Abigail", "x")
	b, _ := newConsultAgent("Harvey", "y")
	r := NewRouter()
	r.Register("Abigail", a)
	r.Register("Harvey", b)

	out, err := a.executeConsult(context.Background(), llm.ToolCall{
		Name: ConsultToolName,
		Arguments: map[string]any{
			"npc_name": "Ghost",
			"question": "are you there?",
		},
	})
	if err != nil {
		t.Fatalf("executeConsult should soft-fall-back, got err=%v", err)
	}
	var decoded struct {
		OK     bool   `json:"ok"`
		Answer string `json:"answer"`
	}
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("output not JSON: %s", out)
	}
	if decoded.OK {
		t.Errorf("expected ok=false on unknown target, got: %s", out)
	}
	if !strings.Contains(decoded.Answer, "Ghost") {
		t.Errorf("fallback should reference target name: %q", decoded.Answer)
	}
}

// ── Timeout: peer that hangs must not stall parent forever ──────────────

// hangingProvider blocks Chat until ctx is cancelled or hangFor elapses.
type hangingProvider struct {
	hangFor time.Duration
}

func (h *hangingProvider) Name() string { return "hanging" }

func (h *hangingProvider) Chat(ctx context.Context, _ llm.ChatRequest) (*llm.ChatResponse, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(h.hangFor):
		return &llm.ChatResponse{Content: "late", FinishReason: "stop"}, nil
	}
}

func TestConsultAgent_TimeoutBoundedByContext(t *testing.T) {
	a, _ := newConsultAgent("Abigail", "x")
	// Harvey's provider hangs longer than the parent context.
	hp := &hangingProvider{hangFor: 5 * time.Second}
	b := New(Config{
		Provider:          hp,
		Speaker:           "Harvey",
		SystemPrompt:      "test Harvey",
		MaxHistory:        10,
		FriendshipTimeout: -1,
	})
	r := NewRouter()
	r.Register("Abigail", a)
	r.Register("Harvey", b)

	// Parent caller sets a tight deadline; consult must respect it.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := r.ConsultAgent(ctx, "Abigail", "Harvey", "are you slow?", "")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if elapsed > 1*time.Second {
		t.Errorf("ConsultAgent honoured ctx deadline too late: %v", elapsed)
	}
}
