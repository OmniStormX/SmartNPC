package chat

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/smartnpc/smartnpc-agent/internal/llm"
)

// newDelegateAgent builds an Agent backed by a mockProvider that always
// returns the given reply. FriendshipTimeout is disabled so delegate paths
// don't try to talk to an MCP session we never wire up.
func newDelegateAgent(speaker, reply string) (*Agent, *mockProvider) {
	mp := &mockProvider{replies: []llm.ChatResponse{
		{Content: reply, FinishReason: "stop"},
	}}
	a := New(Config{
		Provider:          mp,
		Speaker:           speaker,
		SystemPrompt:      "test " + speaker,
		MaxHistory:        10,
		Timeout:           5 * time.Second,
		FriendshipTimeout: -1,
	})
	return a, mp
}

// parseDelegateResult decodes the JSON blob returned by handleNpcDelegate.
// Failures become a zero-value response so callers can inspect OK / Error
// with plain conditionals.
func parseDelegateResult(t *testing.T, raw string) delegateResponse {
	t.Helper()
	var r delegateResponse
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		t.Fatalf("invalid JSON %q: %v", raw, err)
	}
	return r
}

// TestNpcDelegate_Success covers the happy path: A delegates to B, B's mock
// provider produces a reply, and the JSON returned to A's decision layer
// carries {ok:true, from, reply}.
func TestNpcDelegate_Success(t *testing.T) {
	agentA, _ := newDelegateAgent("Abigail", "(unused — A never calls its own provider here)")
	agentB, mpB := newDelegateAgent("Sebastian", "I can help with that.")

	r := NewRouterFromAgents([]*Agent{agentA, agentB})
	r.WireAgentRouters()

	tc := llm.ToolCall{
		ID:   "tc_delegate_ok",
		Name: "npc_delegate",
		Arguments: map[string]any{
			"to":      "Sebastian",
			"request": "帮我看看矿洞里有什么",
		},
	}
	raw, handled := agentA.executeLocalTool(tc)
	if !handled {
		t.Fatal("npc_delegate should be handled as a local tool")
	}

	res := parseDelegateResult(t, raw)
	if !res.OK {
		t.Fatalf("expected ok=true, got raw=%s", raw)
	}
	if res.From != "Sebastian" {
		t.Errorf("from = %q, want %q", res.From, "Sebastian")
	}
	if res.Reply != "I can help with that." {
		t.Errorf("reply = %q, want %q", res.Reply, "I can help with that.")
	}

	// B's provider must have been called exactly once via its respond pipeline,
	// with the delegation prompt appearing on the user side and mentioning the
	// caller's name.
	if len(mpB.calls) != 1 {
		t.Fatalf("Sebastian provider should fire once, got %d", len(mpB.calls))
	}
	var lastUser string
	for _, m := range mpB.calls[0].Messages {
		if m.Role == llm.RoleUser {
			lastUser = m.Content
		}
	}
	if !strings.Contains(lastUser, "Abigail") {
		t.Errorf("delegate prompt should cite the caller, got: %q", lastUser)
	}
	if !strings.Contains(lastUser, "帮我看看矿洞里有什么") {
		t.Errorf("delegate prompt should carry the request body, got: %q", lastUser)
	}
}

// TestNpcDelegate_SelfDelegation rejects A→A loops regardless of casing.
func TestNpcDelegate_SelfDelegation(t *testing.T) {
	agentA, mpA := newDelegateAgent("Abigail", "nope")

	r := NewRouterFromAgents([]*Agent{agentA})
	r.WireAgentRouters()

	cases := []string{"Abigail", "abigail", "ABIGAIL"}
	for _, to := range cases {
		t.Run(to, func(t *testing.T) {
			tc := llm.ToolCall{
				ID:   "tc_self",
				Name: "npc_delegate",
				Arguments: map[string]any{
					"to":      to,
					"request": "hi",
				},
			}
			raw, handled := agentA.executeLocalTool(tc)
			if !handled {
				t.Fatal("should be handled")
			}
			res := parseDelegateResult(t, raw)
			if res.OK {
				t.Fatalf("self-delegation should fail, got raw=%s", raw)
			}
			if !strings.Contains(res.Error, "self") {
				t.Errorf("error should mention self-delegation, got: %q", res.Error)
			}
		})
	}
	if len(mpA.calls) != 0 {
		t.Errorf("self-delegation must not invoke the provider, got %d calls", len(mpA.calls))
	}
}

// TestNpcDelegate_UnknownTarget rejects delegate calls targeting NPCs that
// aren't registered on the router.
func TestNpcDelegate_UnknownTarget(t *testing.T) {
	agentA, _ := newDelegateAgent("Abigail", "ignored")
	r := NewRouterFromAgents([]*Agent{agentA})
	r.WireAgentRouters()

	tc := llm.ToolCall{
		ID:   "tc_unknown",
		Name: "npc_delegate",
		Arguments: map[string]any{
			"to":      "Ghost",
			"request": "are you there?",
		},
	}
	raw, handled := agentA.executeLocalTool(tc)
	if !handled {
		t.Fatal("should be handled")
	}
	res := parseDelegateResult(t, raw)
	if res.OK {
		t.Fatalf("unknown target should fail, got raw=%s", raw)
	}
	if !strings.Contains(res.Error, "Ghost") {
		t.Errorf("error should name the missing NPC, got: %q", res.Error)
	}
}

// TestNpcDelegate_MissingArgs covers both missing "to" and missing "request".
func TestNpcDelegate_MissingArgs(t *testing.T) {
	agentA, _ := newDelegateAgent("Abigail", "x")
	agentB, _ := newDelegateAgent("Sebastian", "y")
	r := NewRouterFromAgents([]*Agent{agentA, agentB})
	r.WireAgentRouters()

	cases := []struct {
		name string
		args map[string]any
	}{
		{"missing to", map[string]any{"request": "hello"}},
		{"missing request", map[string]any{"to": "Sebastian"}},
		{"both empty", map[string]any{"to": "", "request": ""}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tc := llm.ToolCall{ID: "tc_missing", Name: "npc_delegate", Arguments: c.args}
			raw, _ := agentA.executeLocalTool(tc)
			res := parseDelegateResult(t, raw)
			if res.OK {
				t.Fatalf("expected ok=false, got %s", raw)
			}
			if !strings.Contains(res.Error, "missing") {
				t.Errorf("error should mention missing fields, got %q", res.Error)
			}
		})
	}
}

// TestNpcDelegate_MaxDepth asserts that a caller already at maxDelegateDepth
// cannot originate another delegate call — the A→B→C→D chain stops at C.
func TestNpcDelegate_MaxDepth(t *testing.T) {
	agentA, mpA := newDelegateAgent("Abigail", "-")
	agentB, mpB := newDelegateAgent("Sebastian", "-")
	r := NewRouterFromAgents([]*Agent{agentA, agentB})
	r.WireAgentRouters()

	// Simulate that A is already at the delegation-depth ceiling (e.g. it is
	// itself being driven by a nested delegate chain). The new delegate call
	// must be refused before B is invoked.
	agentA.mu.Lock()
	agentA.delegateDepth = maxDelegateDepth
	agentA.mu.Unlock()

	tc := llm.ToolCall{
		ID:   "tc_max_depth",
		Name: "npc_delegate",
		Arguments: map[string]any{
			"to":      "Sebastian",
			"request": "would normally ask",
		},
	}
	raw, handled := agentA.executeLocalTool(tc)
	if !handled {
		t.Fatal("should be handled")
	}
	res := parseDelegateResult(t, raw)
	if res.OK {
		t.Fatalf("delegate at max depth should fail, got raw=%s", raw)
	}
	if !strings.Contains(res.Error, "depth") {
		t.Errorf("error should mention depth, got: %q", res.Error)
	}
	if len(mpA.calls) != 0 || len(mpB.calls) != 0 {
		t.Errorf("no provider should run when blocked by depth (A=%d B=%d)",
			len(mpA.calls), len(mpB.calls))
	}
}

// TestNpcDelegate_DepthPropagation walks an A→B→C chain to prove the depth
// counter is bumped on the callee for the duration of its respond() and then
// restored. C, running at depth == maxDelegateDepth, should refuse any further
// delegate it tries to originate.
func TestNpcDelegate_DepthPropagation(t *testing.T) {
	// A's provider is never called — it's only the initiator of the outer
	// delegate. B will decide to delegate to C mid-respond, so B's provider
	// emits a tool_call first, then a final text reply after the tool result.
	agentA, _ := newDelegateAgent("Abigail", "-")
	agentC, _ := newDelegateAgent("Leah", "-")

	// B's mock provider: first round asks C via npc_delegate; second round
	// returns a final text reply using whatever tool result C produced.
	mpB := &mockProvider{replies: []llm.ChatResponse{
		{
			ToolCalls: []llm.ToolCall{{
				ID:        "tc_b_to_c",
				Name:      "npc_delegate",
				Arguments: map[string]any{"to": "Leah", "request": "further"},
			}},
			FinishReason: "tool_calls",
		},
		{Content: "B's final reply", FinishReason: "stop"},
	}}
	agentB := New(Config{
		Provider:          mpB,
		Speaker:           "Sebastian",
		SystemPrompt:      "test Sebastian",
		MaxHistory:        10,
		Timeout:           5 * time.Second,
		FriendshipTimeout: -1,
	})

	r := NewRouterFromAgents([]*Agent{agentA, agentB, agentC})
	r.WireAgentRouters()

	// Sanity check: before the outer call, A/B/C should all have depth 0.
	if got := agentA.delegateDepth; got != 0 {
		t.Fatalf("A pre-depth = %d, want 0", got)
	}

	tc := llm.ToolCall{
		ID:   "tc_a_to_b",
		Name: "npc_delegate",
		Arguments: map[string]any{
			"to":      "Sebastian",
			"request": "please consult Leah",
		},
	}
	raw, handled := agentA.executeLocalTool(tc)
	if !handled {
		t.Fatal("should be handled")
	}

	res := parseDelegateResult(t, raw)
	if !res.OK {
		t.Fatalf("outer delegate should succeed, raw=%s", raw)
	}

	// After the chain unwinds, all depths must return to zero.
	if got := agentA.delegateDepth; got != 0 {
		t.Errorf("A post-depth = %d, want 0", got)
	}
	if got := agentB.delegateDepth; got != 0 {
		t.Errorf("B post-depth = %d, want 0", got)
	}
	if got := agentC.delegateDepth; got != 0 {
		t.Errorf("C post-depth = %d, want 0", got)
	}
}

// TestNpcDelegate_RouterMissing covers the defensive branch when an agent has
// no router wired yet (e.g. misconfiguration in tests / dev).
func TestNpcDelegate_RouterMissing(t *testing.T) {
	agentA, _ := newDelegateAgent("Abigail", "-")
	// Intentionally skip WireAgentRouters.

	tc := llm.ToolCall{
		ID:   "tc_no_router",
		Name: "npc_delegate",
		Arguments: map[string]any{
			"to":      "Sebastian",
			"request": "hello",
		},
	}
	raw, handled := agentA.executeLocalTool(tc)
	if !handled {
		t.Fatal("should be handled")
	}
	res := parseDelegateResult(t, raw)
	if res.OK {
		t.Fatalf("expected failure when router is unset, got %s", raw)
	}
	if !strings.Contains(res.Error, "router") {
		t.Errorf("error should mention missing router, got: %q", res.Error)
	}
}

// TestNpcDelegate_ToolSpec locks the schema contract so name / required
// fields don't silently drift away from what the protocol docs advertise.
func TestNpcDelegate_ToolSpec(t *testing.T) {
	a, _ := newDelegateAgent("Abigail", "-")
	specs := a.localToolSpecs()

	var spec *llm.ToolSpec
	for i := range specs {
		if specs[i].Name == "npc_delegate" {
			spec = &specs[i]
			break
		}
	}
	if spec == nil {
		t.Fatal("npc_delegate not found in localToolSpecs")
	}
	if spec.Description == "" {
		t.Error("npc_delegate must have a description")
	}

	schema := spec.InputSchema
	if schema == nil {
		t.Fatal("npc_delegate InputSchema should not be nil")
	}
	if schema["type"] != "object" {
		t.Errorf("schema.type = %v, want object", schema["type"])
	}
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("schema.properties missing or wrong type")
	}
	for _, key := range []string{"to", "request"} {
		if _, ok := props[key]; !ok {
			t.Errorf("schema missing property %q", key)
		}
	}
	required, ok := schema["required"].([]string)
	if !ok {
		t.Fatalf("schema.required should be []string, got %T", schema["required"])
	}
	wantRequired := map[string]bool{"to": false, "request": false}
	for _, k := range required {
		if _, known := wantRequired[k]; known {
			wantRequired[k] = true
		}
	}
	for k, seen := range wantRequired {
		if !seen {
			t.Errorf("required field %q not listed in schema", k)
		}
	}
}
