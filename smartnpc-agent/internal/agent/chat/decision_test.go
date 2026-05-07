package chat

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/smartnpc/smartnpc-agent/internal/llm"
)

// errDeciderDown is the sentinel used by decision-stage-failure tests.
var errDeciderDown = errors.New("decider is down")

// ── formatActionResult (singular, per-tool semantic paraphrase) ──

func TestFormatActionResult_MoveTo(t *testing.T) {
	got := formatActionResult(actionResult{
		ToolName: "npc_move_to",
		Args:     map[string]any{"npc": "XiaMi", "x": 45, "y": 30},
		Output:   `{"ok":true,"map":"Farm"}`,
	})
	if !strings.Contains(got, "走去") && !strings.Contains(got, "走向") {
		t.Errorf("move_to should paraphrase as 走去/走向 motion, got %q", got)
	}
	// Must NOT leak tool name or coordinates.
	if strings.Contains(got, "npc_move_to") || strings.Contains(got, "45") {
		t.Errorf("paraphrase leaked tool name or coords: %q", got)
	}
}

func TestFormatActionResult_GetNearby(t *testing.T) {
	// Empty list → "附近没有别人"
	got := formatActionResult(actionResult{
		ToolName: "npc_get_nearby",
		Output:   `{"ok":true,"count":0,"nearby":[]}`,
	})
	if !strings.Contains(got, "没有别人") && !strings.Contains(got, "没有") {
		t.Errorf("empty nearby should say no-one around, got %q", got)
	}
	// Non-empty list → "附近有..."
	got = formatActionResult(actionResult{
		ToolName: "npc_get_nearby",
		Output:   `{"ok":true,"count":2,"nearby":[{"name":"Farmer"}]}`,
	})
	if !strings.Contains(got, "其他人") && !strings.Contains(got, "附近") {
		t.Errorf("non-empty nearby should mention others, got %q", got)
	}
}

func TestFormatActionResult_GetPosition(t *testing.T) {
	// Moving → note motion
	got := formatActionResult(actionResult{
		ToolName: "npc_get_position",
		Output:   `{"ok":true,"x":45,"y":30,"map":"Farm","is_moving":true}`,
	})
	if !strings.Contains(got, "移动") && !strings.Contains(got, "还没") {
		t.Errorf("moving position should mention motion, got %q", got)
	}
	// Static → generic
	got = formatActionResult(actionResult{
		ToolName: "npc_get_position",
		Output:   `{"ok":true,"x":45,"y":30,"map":"Farm","is_moving":false}`,
	})
	if !strings.Contains(got, "位置") {
		t.Errorf("static position should mention 位置, got %q", got)
	}
	// Must not leak coords.
	if strings.Contains(got, "45") {
		t.Errorf("position paraphrase leaked coords: %q", got)
	}
}

func TestFormatActionResult_Environment(t *testing.T) {
	got := formatActionResult(actionResult{
		ToolName: "npc_get_environment",
		Output:   `{"ok":true,"map":"Farm","weather":"rainy"}`,
	})
	if got == "" {
		t.Error("environment should produce a non-empty summary")
	}
	if strings.Contains(got, "npc_get_environment") {
		t.Errorf("environment paraphrase leaked tool name: %q", got)
	}
}

func TestFormatActionResult_ErrorPath(t *testing.T) {
	got := formatActionResult(actionResult{
		ToolName: "npc_move_to",
		Err:      "npc_not_found",
	})
	if !strings.Contains(got, "npc_not_found") {
		t.Errorf("error should surface cause, got %q", got)
	}
	if !strings.Contains(got, "没能完成") && !strings.Contains(got, "没完成") {
		t.Errorf("error phrasing should signal failure, got %q", got)
	}
}

func TestFormatActionResult_UnknownToolFallsBack(t *testing.T) {
	got := formatActionResult(actionResult{
		ToolName: "some_future_tool",
		Output:   `raw-payload`,
	})
	if !strings.Contains(got, "raw-payload") {
		t.Errorf("unknown tool should pass raw output through, got %q", got)
	}
}

// ── formatActionResults (plural, header + concatenation) ──

func TestFormatActionResults_Empty(t *testing.T) {
	if got := formatActionResults(nil); got != "" {
		t.Errorf("empty slice should yield empty string, got %q", got)
	}
	if got := formatActionResults([]actionResult{}); got != "" {
		t.Errorf("zero-length slice should yield empty string, got %q", got)
	}
}

func TestFormatActionResults_HeaderAndMulti(t *testing.T) {
	out := formatActionResults([]actionResult{
		{ToolName: "npc_move_to", Output: `{"ok":true}`},
		{ToolName: "npc_get_nearby", Output: `{"ok":true,"count":0,"nearby":[]}`},
	})
	if !strings.Contains(out, "[系统观察") {
		t.Errorf("missing 系统观察 header: %q", out)
	}
	// Two bullets.
	if strings.Count(out, "\n- ") != 2 {
		t.Errorf("want 2 bullets, got: %q", out)
	}
	// No raw JSON leak.
	if strings.Contains(out, `"ok":true`) || strings.Contains(out, "npc_move_to") {
		t.Errorf("raw tool name or JSON should not appear: %q", out)
	}
}

func TestTruncateForPrompt(t *testing.T) {
	short := "hello"
	if got := truncateForPrompt(short, 100); got != short {
		t.Errorf("short string should pass through, got %q", got)
	}
	long := strings.Repeat("a", 400)
	got := truncateForPrompt(long, 100)
	if !strings.HasSuffix(got, "…(truncated)") {
		t.Errorf("long string should be marked truncated, got %q", got)
	}
	if len(got) > 200 {
		t.Errorf("truncated output too long: len=%d", len(got))
	}
}

// ── decision system prompt ──

func TestBuildDecisionSystemPrompt_SpeakerSubstituted(t *testing.T) {
	p := buildDecisionSystemPrompt("XiaMi", "", "")
	if !strings.Contains(p, `"XiaMi"`) {
		t.Errorf("speaker not substituted: %q", p)
	}
	if strings.Contains(p, "{speaker}") {
		t.Errorf("template placeholder leaked: %q", p)
	}
}

func TestBuildDecisionSystemPrompt_StateAppended(t *testing.T) {
	p := buildDecisionSystemPrompt("XiaMi", "Friendship: 5 hearts", "Time: 14:30 | Weather: sunny")
	if !strings.Contains(p, "Current state:") {
		t.Errorf("state header missing: %q", p)
	}
	if !strings.Contains(p, "Friendship") || !strings.Contains(p, "sunny") {
		t.Errorf("state content missing: %q", p)
	}
}

// ── routing: single-model stays on Provider, dual-mode on DecisionProvider ──

func TestRespond_RoutingSingleModel(t *testing.T) {
	mp := &mockProvider{replies: []llm.ChatResponse{
		{Content: "single-model reply", FinishReason: "stop"},
	}}
	agent := New(Config{
		Provider:          mp,
		Speaker:           "XiaMi",
		SystemPrompt:      "test",
		MaxHistory:        10,
		FriendshipTimeout: -1,
	})
	reply, err := agent.respond(context.Background(), "hi")
	if err != nil {
		t.Fatalf("respond: %v", err)
	}
	if reply != "single-model reply" {
		t.Errorf("reply = %q", reply)
	}
	if len(mp.calls) != 1 {
		t.Errorf("single Provider should be called once, got %d", len(mp.calls))
	}
}

func TestRespond_RoutingDualMode(t *testing.T) {
	decider := &mockProvider{replies: []llm.ChatResponse{
		{Content: "done", FinishReason: "stop"}, // no tool_calls
	}}
	personaLLM := &mockProvider{replies: []llm.ChatResponse{
		{Content: "persona reply", FinishReason: "stop"},
	}}
	agent := New(Config{
		Provider:          personaLLM, // fallback for PersonaProvider
		DecisionProvider:  decider,
		Speaker:           "XiaMi",
		SystemPrompt:      "test",
		MaxHistory:        10,
		FriendshipTimeout: -1,
	})
	reply, err := agent.respond(context.Background(), "你好")
	if err != nil {
		t.Fatalf("respond: %v", err)
	}
	if reply != "persona reply" {
		t.Errorf("reply = %q, want persona reply", reply)
	}
	if len(decider.calls) != 1 {
		t.Errorf("decider should be called once, got %d", len(decider.calls))
	}
	if len(personaLLM.calls) != 1 {
		t.Errorf("persona should be called once, got %d", len(personaLLM.calls))
	}
	// Decision stage must see the NPC-specific decision system prompt.
	decisionSys := decider.calls[0].Messages[0]
	if decisionSys.Role != llm.RoleSystem {
		t.Fatalf("decision first msg role = %v", decisionSys.Role)
	}
	if !strings.Contains(decisionSys.Content, "behavior controller") {
		t.Errorf("decision system prompt mismatch: %q", decisionSys.Content)
	}
	if !strings.Contains(decisionSys.Content, "XiaMi") {
		t.Errorf("decision prompt missing speaker name: %q", decisionSys.Content)
	}
	// Persona stage must NOT receive tools.
	if len(personaLLM.calls[0].Tools) != 0 {
		t.Errorf("persona stage must not receive tools, got %d", len(personaLLM.calls[0].Tools))
	}
	// Persona stage should carry the user message.
	var sawUser bool
	for _, m := range personaLLM.calls[0].Messages {
		if m.Role == llm.RoleUser && strings.Contains(m.Content, "你好") {
			sawUser = true
		}
	}
	if !sawUser {
		t.Errorf("persona stage missing user message; msgs=%+v", personaLLM.calls[0].Messages)
	}
}

// ── full decision loop: tool_calls are executed and threaded into persona ──

// dualMoveInput is a stand-in for npc_move_to when we register an in-memory
// MCP server for the dual test. Mirrors the production schema shape.
type dualMoveInput struct {
	NPC string `json:"npc" jsonschema:"NPC name"`
	X   int    `json:"x"   jsonschema:"target X"`
	Y   int    `json:"y"   jsonschema:"target Y"`
}

type dualMoveOutput struct {
	OK bool `json:"ok"`
}

// newDualAgent wires an in-memory MCP server + client with a capturing
// npc_move_to tool and returns the Agent ready for dual-mode invocation.
// If fallback is non-nil it is wired as Config.Provider (distinct from the
// persona provider), enabling the respond() fallback path on decision failure.
func newDualAgent(t *testing.T, decider, personaLLM, fallback llm.Provider) (*Agent, *struct {
	mu    sync.Mutex
	calls []dualMoveInput
}) {
	t.Helper()
	ctx := context.Background()

	captured := &struct {
		mu    sync.Mutex
		calls []dualMoveInput
	}{}

	server := mcp.NewServer(&mcp.Implementation{Name: "fake", Version: "t"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "npc_move_to", Description: "fake move tool"},
		func(_ context.Context, _ *mcp.CallToolRequest, in dualMoveInput) (*mcp.CallToolResult, dualMoveOutput, error) {
			captured.mu.Lock()
			captured.calls = append(captured.calls, in)
			captured.mu.Unlock()
			return nil, dualMoveOutput{OK: true}, nil
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

	prov := fallback
	if prov == nil {
		// Default: Provider == personaLLM (dual mode with no distinct
		// fallback). The respond() fallback path won't trigger in this mode.
		prov = personaLLM
	}
	agent := New(Config{
		Provider:          prov,
		DecisionProvider:  decider,
		PersonaProvider:   personaLLM,
		Speaker:           "XiaMi",
		SystemPrompt:      "test persona",
		MaxHistory:        10,
		FriendshipTimeout: -1,
	})
	agent.SetSession(cs)
	if err := agent.LoadTools(ctx); err != nil {
		t.Fatalf("LoadTools: %v", err)
	}
	return agent, captured
}

func TestRespondDual_ToolCallExecuted(t *testing.T) {
	decider := &mockProvider{replies: []llm.ChatResponse{
		// Round 1: decider emits a tool call
		{
			ToolCalls: []llm.ToolCall{{
				ID:        "call_1",
				Name:      "npc_move_to",
				Arguments: map[string]any{"npc": "XiaMi", "x": 45, "y": 30},
			}},
			FinishReason: "tool_calls",
		},
		// Round 2: decider is satisfied (no more tool calls)
		{Content: "done", FinishReason: "stop"},
	}}
	personaLLM := &mockProvider{replies: []llm.ChatResponse{
		{Content: "好吧，我这就过去。", FinishReason: "stop"},
	}}
	agent, captured := newDualAgent(t, decider, personaLLM, nil)

	// Message that does NOT match the move-intent keyword to isolate the
	// decision-layer's call.
	reply, err := agent.respond(context.Background(), "请帮我确认一下你的状态")
	if err != nil {
		t.Fatalf("respond: %v", err)
	}
	if reply != "好吧，我这就过去。" {
		t.Errorf("reply = %q", reply)
	}

	captured.mu.Lock()
	n := len(captured.calls)
	captured.mu.Unlock()
	if n != 1 {
		t.Fatalf("npc_move_to should be called exactly once, got %d", n)
	}

	// Decider called twice (round 1: tool_calls, round 2: done).
	if len(decider.calls) != 2 {
		t.Errorf("decider should be called 2 times, got %d", len(decider.calls))
	}
	// Persona stage sees the action log (paraphrased) in its system prompt.
	sys := personaLLM.calls[0].Messages[0].Content
	if !strings.Contains(sys, "[系统观察") {
		t.Errorf("persona prompt missing 系统观察 header: %q", sys)
	}
	// Must NOT leak tool name or coords.
	if strings.Contains(sys, "npc_move_to") {
		t.Errorf("persona prompt leaked tool name: %q", sys)
	}
	if strings.Contains(sys, "45") || strings.Contains(sys, "30") {
		t.Errorf("persona prompt leaked coords: %q", sys)
	}
}

func TestRespondDual_PureConversation(t *testing.T) {
	// Decider never asks for a tool (pure small talk).
	decider := &mockProvider{replies: []llm.ChatResponse{
		{Content: "done", FinishReason: "stop"},
	}}
	personaLLM := &mockProvider{replies: []llm.ChatResponse{
		{Content: "嗯，今天感觉还不错。", FinishReason: "stop"},
	}}
	agent, captured := newDualAgent(t, decider, personaLLM, nil)

	reply, err := agent.respond(context.Background(), "你今天心情怎么样")
	if err != nil {
		t.Fatalf("respond: %v", err)
	}
	if reply != "嗯，今天感觉还不错。" {
		t.Errorf("reply = %q", reply)
	}
	// No tool fired.
	captured.mu.Lock()
	n := len(captured.calls)
	captured.mu.Unlock()
	if n != 0 {
		t.Errorf("no tool should fire for small talk, got %d", n)
	}
	// Persona prompt must NOT include an observations header.
	sys := personaLLM.calls[0].Messages[0].Content
	if strings.Contains(sys, "系统观察") || strings.Contains(sys, "System observations") {
		t.Errorf("observations should be omitted for pure conversation, got: %q", sys)
	}
}

func TestRespondDual_FallbackOnError(t *testing.T) {
	// Decider errors; Provider (distinct from PersonaProvider) handles the
	// fallback single-LLM turn.
	decider := &mockProvider{err: errDeciderDown}
	personaLLM := &mockProvider{replies: []llm.ChatResponse{
		{Content: "persona (should not be used)", FinishReason: "stop"},
	}}
	fallback := &mockProvider{replies: []llm.ChatResponse{
		{Content: "fallback single-llm reply", FinishReason: "stop"},
	}}
	agent, _ := newDualAgent(t, decider, personaLLM, fallback)

	reply, err := agent.respond(context.Background(), "你今天心情怎么样")
	if err != nil {
		t.Fatalf("respond: %v", err)
	}
	if reply != "fallback single-llm reply" {
		t.Errorf("reply = %q, want fallback single-llm reply", reply)
	}
	// Decider called exactly once (then failed).
	if len(decider.calls) != 1 {
		t.Errorf("decider calls = %d, want 1", len(decider.calls))
	}
	// Fallback Provider handled the turn.
	if len(fallback.calls) != 1 {
		t.Errorf("fallback calls = %d, want 1", len(fallback.calls))
	}
	// Persona should NOT be invoked in fallback mode.
	if len(personaLLM.calls) != 0 {
		t.Errorf("persona should not be called on fallback; got %d calls", len(personaLLM.calls))
	}
}

func TestRespondDual_FallbackRestoresDualConfigAfterTurn(t *testing.T) {
	// After a fallback turn the agent must stay in dual mode for the next
	// turn — the fallback is per-call, not sticky.
	decider := &mockProvider{
		err: errDeciderDown,
		replies: []llm.ChatResponse{
			// Second turn: decider recovers.
			{Content: "done", FinishReason: "stop"},
		},
	}
	personaLLM := &mockProvider{replies: []llm.ChatResponse{
		{Content: "recovered persona reply", FinishReason: "stop"},
	}}
	fallback := &mockProvider{replies: []llm.ChatResponse{
		{Content: "fallback first turn", FinishReason: "stop"},
	}}
	agent, _ := newDualAgent(t, decider, personaLLM, fallback)

	// Turn 1: decider fails → fallback path.
	if _, err := agent.respond(context.Background(), "第一轮对话"); err != nil {
		t.Fatalf("turn 1: %v", err)
	}

	// Allow decider to recover for turn 2: clear its err field.
	decider.mu.Lock()
	decider.err = nil
	decider.mu.Unlock()

	if _, err := agent.respond(context.Background(), "第二轮对话"); err != nil {
		t.Fatalf("turn 2: %v", err)
	}
	// Turn 2 should have routed through dual mode again (persona LLM
	// produced the reply, decider fired once, fallback untouched).
	if len(personaLLM.calls) != 1 {
		t.Errorf("persona should handle turn 2; got %d calls", len(personaLLM.calls))
	}
	if len(fallback.calls) != 1 {
		t.Errorf("fallback should NOT fire on turn 2; got %d calls", len(fallback.calls))
	}
}

func TestRespondDual_NoFallbackWhenProviderSameAsPersona(t *testing.T) {
	// When Config.Provider == PersonaProvider, there is no distinct
	// fallback; we must NOT invoke respond() recursively (that would call
	// persona again without observations, fine, but we want to short-circuit
	// to persona-only so the test stays simple and reliable).
	decider := &mockProvider{err: errDeciderDown}
	shared := &mockProvider{replies: []llm.ChatResponse{
		{Content: "shared persona/provider reply", FinishReason: "stop"},
	}}
	agent, _ := newDualAgent(t, decider, shared, shared) // Provider == Persona

	reply, err := agent.respond(context.Background(), "hi")
	if err != nil {
		t.Fatalf("respond: %v", err)
	}
	if reply != "shared persona/provider reply" {
		t.Errorf("reply = %q", reply)
	}
	// shared is called exactly once (the persona stage), not twice.
	if len(shared.calls) != 1 {
		t.Errorf("shared provider should be called once, got %d", len(shared.calls))
	}
}
