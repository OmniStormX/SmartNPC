package chat

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/smartnpc/smartnpc-agent/internal/llm"
)

// moveToInput mirrors the npc_move_to input so AddTool derives a schema.
type moveToInput struct {
	NPC string `json:"npc"           jsonschema:"NPC name"`
	X   int    `json:"x"             jsonschema:"target tile X"`
	Y   int    `json:"y"             jsonschema:"target tile Y"`
	Map string `json:"map,omitempty" jsonschema:"target map"`
}

type moveToOutput struct {
	OK bool `json:"ok"`
}

// newIntentAgent wires an in-memory MCP server with a fake npc_move_to tool
// that records its latest invocation args. The returned pointer is populated
// under lock whenever the tool fires.
func newIntentAgent(t *testing.T) (*Agent, *mockProvider, *struct {
	mu   sync.Mutex
	args *moveToInput
}) {
	t.Helper()
	ctx := context.Background()

	captured := &struct {
		mu   sync.Mutex
		args *moveToInput
	}{}

	server := mcp.NewServer(&mcp.Implementation{Name: "fake", Version: "t"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "npc_move_to", Description: "fake move tool"},
		func(_ context.Context, _ *mcp.CallToolRequest, in moveToInput) (*mcp.CallToolResult, moveToOutput, error) {
			captured.mu.Lock()
			copy := in
			captured.args = &copy
			captured.mu.Unlock()
			return nil, moveToOutput{OK: true}, nil
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

	mp := &mockProvider{replies: []llm.ChatResponse{
		{Content: "好吧，我这就过去。", FinishReason: "stop"},
	}}
	agent := New(Config{
		Provider:          mp,
		Speaker:           "XiaMi",
		SystemPrompt:      "test",
		MaxHistory:        10,
		FriendshipTimeout: -1, // disable friendship lookup to keep the test fast
	})
	agent.SetSession(cs)
	return agent, mp, captured
}

func TestMoveIntent_AutoExecutesMoveTo(t *testing.T) {
	agent, mp, captured := newIntentAgent(t)

	reply, err := agent.respond(context.Background(), "陪我去湖边")
	if err != nil {
		t.Fatalf("respond: %v", err)
	}
	if reply == "" {
		t.Error("empty reply")
	}

	captured.mu.Lock()
	got := captured.args
	captured.mu.Unlock()
	if got == nil {
		t.Fatal("npc_move_to was not invoked")
	}
	if got.NPC != "XiaMi" {
		t.Errorf("npc arg = %q, want XiaMi", got.NPC)
	}
	if got.X != 45 || got.Y != 30 {
		t.Errorf("coords = (%d,%d), want (45,30) for 湖边", got.X, got.Y)
	}
	if got.Map != "Farm" {
		t.Errorf("map = %q, want Farm", got.Map)
	}

	// The user message sent to the LLM must carry the movement hint.
	if len(mp.calls) == 0 {
		t.Fatal("LLM not called")
	}
	var userMsg string
	for _, m := range mp.calls[0].Messages {
		if m.Role == llm.RoleUser {
			userMsg = m.Content
		}
	}
	if !strings.Contains(userMsg, "湖边") {
		t.Errorf("user message should reference 湖边, got: %q", userMsg)
	}
	if !strings.Contains(userMsg, "[系统") {
		t.Errorf("user message should include bracketed system hint, got: %q", userMsg)
	}
}

func TestMoveIntent_UnknownDestination_AsksForClarification(t *testing.T) {
	agent, mp, captured := newIntentAgent(t)

	// Use a move-intent phrase without a landmark that is NOT also a
	// behavior-intent keyword — "过去看看" hits move keyword "过去" but does
	// not match any summon/follow/lead/stop keyword, so applyMoveIntent
	// runs and requests clarification.
	_, err := agent.respond(context.Background(), "过去看看")
	if err != nil {
		t.Fatalf("respond: %v", err)
	}

	captured.mu.Lock()
	got := captured.args
	captured.mu.Unlock()
	if got != nil {
		t.Errorf("npc_move_to should NOT be called when destination is unknown, got %+v", got)
	}

	if len(mp.calls) == 0 {
		t.Fatal("LLM not called")
	}
	var userMsg string
	for _, m := range mp.calls[0].Messages {
		if m.Role == llm.RoleUser {
			userMsg = m.Content
		}
	}
	if !strings.Contains(userMsg, "不确定具体位置") && !strings.Contains(userMsg, "请询问更明确的目的地") {
		t.Errorf("user message should ask for clarification, got: %q", userMsg)
	}
}

func TestMoveIntent_NonMoveMessageIsUntouched(t *testing.T) {
	agent, mp, captured := newIntentAgent(t)

	_, err := agent.respond(context.Background(), "你今天过得怎么样")
	if err != nil {
		t.Fatalf("respond: %v", err)
	}

	captured.mu.Lock()
	got := captured.args
	captured.mu.Unlock()
	if got != nil {
		t.Errorf("npc_move_to must not fire for non-move messages, got %+v", got)
	}

	if len(mp.calls) == 0 {
		t.Fatal("LLM not called")
	}
	var userMsg string
	for _, m := range mp.calls[0].Messages {
		if m.Role == llm.RoleUser {
			userMsg = m.Content
		}
	}
	if userMsg != "你今天过得怎么样" {
		t.Errorf("non-move user text must pass through unchanged, got: %q", userMsg)
	}
}

func TestMoveIntent_PersonaOverridesLocations(t *testing.T) {
	// Persona provides a single custom location; Farm defaults must not apply.
	p := &Persona{
		Speaker: "XiaMi",
		Name:    "夏弥",
		NamedLocations: []NamedLocation{
			{Name: "秘密基地", Aliases: []string{"秘密基地"}, Map: "Cellar", X: 5, Y: 5},
		},
	}
	mp := &mockProvider{replies: []llm.ChatResponse{
		{Content: "好。", FinishReason: "stop"},
	}}
	agent := New(Config{
		Provider:          mp,
		Speaker:           "XiaMi",
		SystemPrompt:      "test",
		Persona:           p,
		MaxHistory:        10,
		FriendshipTimeout: -1,
	})

	// Custom alias should resolve.
	mi := agent.locations.DetectMoveIntent("带我去秘密基地")
	if mi.Location == nil || mi.Location.Name != "秘密基地" {
		t.Errorf("custom alias should resolve, got: %+v", mi.Location)
	}
	// A default-only alias must NOT match.
	mi = agent.locations.DetectMoveIntent("带我去湖边")
	if mi.Location != nil {
		t.Errorf("default aliases must not leak when persona overrides, got: %+v", mi.Location)
	}
}
