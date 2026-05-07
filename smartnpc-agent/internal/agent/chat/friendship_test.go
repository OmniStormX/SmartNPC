package chat

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/smartnpc/smartnpc-agent/internal/llm"
)

// friendshipInput mirrors the MCP tool's input so AddTool can derive its schema.
type friendshipInput struct {
	NPC string `json:"npc" jsonschema:"NPC name"`
}

type friendshipOutput struct {
	OK        bool   `json:"ok"`
	NPC       string `json:"npc"`
	Points    int    `json:"points"`
	Hearts    int    `json:"hearts"`
	MaxHearts int    `json:"max_hearts"`
	Status    string `json:"status"`
}

// newAgentWithFriendship wires an in-memory MCP server that exposes
// friendship_get (returning the caller-controlled hearts/status/ok) plus
// chat_say, and returns an Agent already using a fresh Persona with a full
// four-tier friendship_behaviors map.
//
// The returned *int64 can be swapped at test time: tests Atomic-swap the
// desired hearts just before dispatching a message, so a single helper covers
// the whole heart-range table.
func newAgentWithFriendship(t *testing.T) (
	agent *Agent,
	heartsCell *int64,
	okCell *atomic.Bool,
	chatSayCalls map[string]map[string]any,
) {
	t.Helper()
	ctx := context.Background()

	server := mcp.NewServer(&mcp.Implementation{Name: "fake", Version: "t"}, nil)

	var hearts int64 = 0
	heartsCell = &hearts
	okCell = &atomic.Bool{}
	okCell.Store(true)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "friendship_get",
		Description: "fake friendship",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in friendshipInput) (*mcp.CallToolResult, friendshipOutput, error) {
		h := int(atomic.LoadInt64(heartsCell))
		if !okCell.Load() {
			return nil, friendshipOutput{OK: false, NPC: in.NPC}, nil
		}
		return nil, friendshipOutput{
			OK:        true,
			NPC:       in.NPC,
			Points:    h * 250,
			Hearts:    h,
			MaxHearts: 10,
			Status:    "friendly",
		}, nil
	})

	chatSayCalls = make(map[string]map[string]any)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "chat_say",
		Description: "fake chat_say",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in fakeToolInput) (*mcp.CallToolResult, fakeToolOutput, error) {
		chatSayCalls["chat_say"] = map[string]any{"speaker": in.Speaker, "text": in.Text}
		return nil, fakeToolOutput{OK: true}, nil
	})

	t1, t2 := mcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, t1, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "agent-test", Version: "t"}, nil)
	cs, err := client.Connect(ctx, t2, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })

	persona := newTestPersona()
	agent = New(Config{
		Provider:          &mockProvider{},
		Speaker:           "NPC",
		SystemPrompt:      persona.SystemPrompt,
		Persona:           persona,
		MaxHistory:        10,
		FriendshipTimeout: 500 * time.Millisecond,
	})
	agent.SetSession(cs)
	return agent, heartsCell, okCell, chatSayCalls
}

// newTestPersona returns a Persona with the canonical 4-tier behavior map so
// every heart count from 0..10 hits exactly one range.
func newTestPersona() *Persona {
	p := &Persona{
		Speaker:     "NPC",
		Name:        "NPC",
		Personality: "curious tester",
		FriendshipBehaviors: map[string]FriendshipBehavior{
			"0-2":  {Tone: "cold", Willingness: "low", Greeting: "...oh, you again.", Notes: "keeps distance"},
			"3-5":  {Tone: "polite", Willingness: "medium", Greeting: "hi there!", Notes: "chats casually"},
			"6-8":  {Tone: "warm", Willingness: "high", Greeting: "so glad you came by!"},
			"9-10": {Tone: "devoted", Willingness: "very_high", Greeting: "I was waiting for you."},
		},
	}
	p.SystemPrompt = p.buildSystemPrompt()
	return p
}

// TestFormatFriendshipContext_TableDriven locks the exact addendum shape.
// Keeping this deterministic matters: the LLM prompt is part of the product,
// and we don't want field order drift between releases.
func TestFormatFriendshipContext_TableDriven(t *testing.T) {
	b := FriendshipBehavior{
		Tone:        "warm",
		Willingness: "high",
		Greeting:    "hey, you're here!",
		Notes:       "treats player as close friend",
	}
	got := formatFriendshipContext(7, "friendly", "6-8", b)

	wantSubs := []string{
		"[Current friendship: 7 hearts",
		"friendly",
		"tier 6-8",
		"Act with this tone: warm",
		"Openness level: high",
		`borrow the spirit of: "hey, you're here!"`,
		"Notes: treats player as close friend",
		"Never quote the numeric heart value",
	}
	for _, s := range wantSubs {
		if !strings.Contains(got, s) {
			t.Errorf("addendum missing %q:\n%s", s, got)
		}
	}
}

func TestFormatFriendshipContext_OmitsEmptyFields(t *testing.T) {
	got := formatFriendshipContext(0, "none", "0-2", FriendshipBehavior{Tone: "cold"})
	if strings.Contains(got, "Openness level") {
		t.Errorf("should not mention openness when willingness is empty: %s", got)
	}
	if strings.Contains(got, "· none") {
		t.Errorf("status=none should be suppressed: %s", got)
	}
	if !strings.Contains(got, "tier 0-2") {
		t.Errorf("tier key missing: %s", got)
	}
	if !strings.Contains(got, "Act with this tone: cold") {
		t.Errorf("tone missing: %s", got)
	}
}

// TestRespond_InjectsFriendshipTier_TableDriven walks 0/4/7/10 hearts through
// a real in-memory MCP session + mock LLM, and asserts the system message
// sent to the LLM carries the right tier descriptor each time.
func TestRespond_InjectsFriendshipTier_TableDriven(t *testing.T) {
	agent, heartsCell, _, _ := newAgentWithFriendship(t)
	mp := &mockProvider{}
	agent.cfg.Provider = mp

	cases := []struct {
		name   string
		hearts int
		tier   string
		tone   string
	}{
		{"cold", 0, "0-2", "cold"},
		{"polite", 4, "3-5", "polite"},
		{"warm", 7, "6-8", "warm"},
		{"devoted", 10, "9-10", "devoted"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			atomic.StoreInt64(heartsCell, int64(tc.hearts))
			mp.mu.Lock()
			mp.calls = nil
			mp.idx = 0
			mp.replies = []llm.ChatResponse{{Content: "ok", FinishReason: "stop"}}
			mp.mu.Unlock()

			if _, err := agent.respond(context.Background(), "hi"); err != nil {
				t.Fatalf("respond: %v", err)
			}
			mp.mu.Lock()
			calls := append([]llm.ChatRequest(nil), mp.calls...)
			mp.mu.Unlock()
			if len(calls) != 1 {
				t.Fatalf("expected 1 LLM call, got %d", len(calls))
			}
			sys := calls[0].Messages[0]
			if sys.Role != llm.RoleSystem {
				t.Fatalf("first message should be system, got %v", sys.Role)
			}
			if !strings.Contains(sys.Content, "tier "+tc.tier) {
				t.Errorf("system prompt missing tier %q:\n%s", tc.tier, sys.Content)
			}
			if !strings.Contains(sys.Content, "Act with this tone: "+tc.tone) {
				t.Errorf("system prompt missing tone %q:\n%s", tc.tone, sys.Content)
			}
		})
	}
}

// TestRespond_FriendshipFailure_Graceful verifies that when friendship_get
// returns ok=false, no tier addendum is appended and the turn still
// completes — the LLM just sees the static persona prompt.
func TestRespond_FriendshipFailure_Graceful(t *testing.T) {
	agent, _, okCell, _ := newAgentWithFriendship(t)
	okCell.Store(false) // friendship_get now returns ok=false

	mp := &mockProvider{replies: []llm.ChatResponse{
		{Content: "fallback reply", FinishReason: "stop"},
	}}
	agent.cfg.Provider = mp

	reply, err := agent.respond(context.Background(), "hello")
	if err != nil {
		t.Fatalf("respond should not fail on friendship error: %v", err)
	}
	if reply != "fallback reply" {
		t.Errorf("got reply=%q", reply)
	}
	if len(mp.calls) != 1 {
		t.Fatalf("expected 1 LLM call, got %d", len(mp.calls))
	}
	sys := mp.calls[0].Messages[0].Content
	if strings.Contains(sys, "Current friendship:") {
		t.Errorf("tier addendum should be absent when friendship_get fails:\n%s", sys)
	}
}

// TestRespond_NoPersona_SkipsFriendship verifies the friendship query is
// skipped entirely when no Persona is attached — the agent should work
// whether personas define behaviors or not.
func TestRespond_NoPersona_SkipsFriendship(t *testing.T) {
	agent, heartsCell, _, _ := newAgentWithFriendship(t)
	// Strip the persona so getFriendshipContext returns early.
	agent.mu.Lock()
	agent.cfg.Persona = nil
	agent.mu.Unlock()
	atomic.StoreInt64(heartsCell, 10)

	mp := &mockProvider{replies: []llm.ChatResponse{
		{Content: "k", FinishReason: "stop"},
	}}
	agent.cfg.Provider = mp

	if _, err := agent.respond(context.Background(), "hi"); err != nil {
		t.Fatalf("respond: %v", err)
	}
	if len(mp.calls) != 1 {
		t.Fatalf("expected 1 LLM call, got %d", len(mp.calls))
	}
	sys := mp.calls[0].Messages[0].Content
	if strings.Contains(sys, "Current friendship:") {
		t.Errorf("tier addendum should be absent without persona:\n%s", sys)
	}
}

// TestRespond_FriendshipTimeoutDisabled verifies FriendshipTimeout < 0
// skips the lookup entirely (opt-out escape hatch for integration tests).
func TestRespond_FriendshipTimeoutDisabled(t *testing.T) {
	agent, heartsCell, _, _ := newAgentWithFriendship(t)
	agent.mu.Lock()
	agent.cfg.FriendshipTimeout = -1
	agent.mu.Unlock()
	atomic.StoreInt64(heartsCell, 7)

	mp := &mockProvider{replies: []llm.ChatResponse{
		{Content: "k", FinishReason: "stop"},
	}}
	agent.cfg.Provider = mp

	if _, err := agent.respond(context.Background(), "hi"); err != nil {
		t.Fatalf("respond: %v", err)
	}
	sys := mp.calls[0].Messages[0].Content
	if strings.Contains(sys, "Current friendship:") {
		t.Errorf("tier addendum should be absent when timeout disabled:\n%s", sys)
	}
}
