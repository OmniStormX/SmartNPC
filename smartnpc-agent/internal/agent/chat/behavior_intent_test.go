package chat

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/smartnpc/smartnpc-agent/internal/llm"
)

// behaviorCaptured tracks which behavior tool(s) fired on the fake MCP
// server during a single chat turn, so tests can assert both the dispatch
// target and its arguments.
type behaviorCaptured struct {
	mu   sync.Mutex
	name string
	args map[string]any
}

func (c *behaviorCaptured) set(name string, args map[string]any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	// Keep the first call so tests aren't racing on overwrites.
	if c.name == "" {
		c.name = name
		c.args = args
	}
}

func (c *behaviorCaptured) get() (string, map[string]any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.name, c.args
}

// behaviorInput is a permissive input struct that covers all five behavior
// tools — summon / follow_start / follow_stop / lead_to / get_behavior.
// `x,y,map` stay optional because only lead_to populates them.
type behaviorInput struct {
	NPC string `json:"npc"           jsonschema:"NPC name"`
	X   int    `json:"x,omitempty"   jsonschema:"target tile X"`
	Y   int    `json:"y,omitempty"   jsonschema:"target tile Y"`
	Map string `json:"map,omitempty" jsonschema:"target map"`
}

type behaviorOutput struct {
	OK bool `json:"ok"`
}

// newBehaviorAgent wires an in-memory MCP server with fake npc_* behavior
// tools and returns an agent + capture handle for assertions. Separate from
// newIntentAgent because we need the four behavior tools + npc_move_to to
// verify that behavior-intent short-circuits move-intent.
func newBehaviorAgent(t *testing.T) (*Agent, *mockProvider, *behaviorCaptured) {
	t.Helper()
	ctx := context.Background()

	captured := &behaviorCaptured{}

	server := mcp.NewServer(&mcp.Implementation{Name: "fake", Version: "t"}, nil)

	mcp.AddTool(server, &mcp.Tool{Name: "npc_summon", Description: "fake"},
		func(_ context.Context, _ *mcp.CallToolRequest, in behaviorInput) (*mcp.CallToolResult, behaviorOutput, error) {
			captured.set("npc_summon", map[string]any{"npc": in.NPC})
			return nil, behaviorOutput{OK: true}, nil
		})
	mcp.AddTool(server, &mcp.Tool{Name: "npc_follow_start", Description: "fake"},
		func(_ context.Context, _ *mcp.CallToolRequest, in behaviorInput) (*mcp.CallToolResult, behaviorOutput, error) {
			captured.set("npc_follow_start", map[string]any{"npc": in.NPC})
			return nil, behaviorOutput{OK: true}, nil
		})
	mcp.AddTool(server, &mcp.Tool{Name: "npc_follow_stop", Description: "fake"},
		func(_ context.Context, _ *mcp.CallToolRequest, in behaviorInput) (*mcp.CallToolResult, behaviorOutput, error) {
			captured.set("npc_follow_stop", map[string]any{"npc": in.NPC})
			return nil, behaviorOutput{OK: true}, nil
		})
	mcp.AddTool(server, &mcp.Tool{Name: "npc_lead_to", Description: "fake"},
		func(_ context.Context, _ *mcp.CallToolRequest, in behaviorInput) (*mcp.CallToolResult, behaviorOutput, error) {
			captured.set("npc_lead_to", map[string]any{
				"npc": in.NPC, "x": in.X, "y": in.Y, "map": in.Map,
			})
			return nil, behaviorOutput{OK: true}, nil
		})
	// Move-to exists so we can assert it is NOT invoked when a behavior
	// intent wins.
	mcp.AddTool(server, &mcp.Tool{Name: "npc_move_to", Description: "fake"},
		func(_ context.Context, _ *mcp.CallToolRequest, in behaviorInput) (*mcp.CallToolResult, behaviorOutput, error) {
			captured.set("npc_move_to", map[string]any{
				"npc": in.NPC, "x": in.X, "y": in.Y, "map": in.Map,
			})
			return nil, behaviorOutput{OK: true}, nil
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
		{Content: "好，我知道了。", FinishReason: "stop"},
	}}
	agent := New(Config{
		Provider:          mp,
		Speaker:           "XiaMi",
		SystemPrompt:      "test",
		MaxHistory:        10,
		FriendshipTimeout: -1,
	})
	agent.SetSession(cs)
	return agent, mp, captured
}

func TestBehaviorIntent_Summon(t *testing.T) {
	agent, mp, captured := newBehaviorAgent(t)

	if _, err := agent.respond(context.Background(), "过来"); err != nil {
		t.Fatalf("respond: %v", err)
	}

	name, args := captured.get()
	if name != "npc_summon" {
		t.Fatalf("tool = %q, want npc_summon", name)
	}
	if args["npc"] != "XiaMi" {
		t.Errorf("npc = %v, want XiaMi", args["npc"])
	}

	userMsg := lastUserMessage(mp)
	if !strings.Contains(userMsg, "赶往玩家身边") {
		t.Errorf("user message missing summon hint: %q", userMsg)
	}
}

func TestBehaviorIntent_FollowStart(t *testing.T) {
	agent, mp, captured := newBehaviorAgent(t)

	if _, err := agent.respond(context.Background(), "跟着我"); err != nil {
		t.Fatalf("respond: %v", err)
	}

	name, _ := captured.get()
	if name != "npc_follow_start" {
		t.Fatalf("tool = %q, want npc_follow_start", name)
	}
	userMsg := lastUserMessage(mp)
	if !strings.Contains(userMsg, "跟着玩家") {
		t.Errorf("user message missing follow hint: %q", userMsg)
	}
}

func TestBehaviorIntent_Stop(t *testing.T) {
	agent, mp, captured := newBehaviorAgent(t)

	if _, err := agent.respond(context.Background(), "别跟了"); err != nil {
		t.Fatalf("respond: %v", err)
	}

	name, _ := captured.get()
	if name != "npc_follow_stop" {
		t.Fatalf("tool = %q, want npc_follow_stop", name)
	}
	userMsg := lastUserMessage(mp)
	if !strings.Contains(userMsg, "停止跟随") {
		t.Errorf("user message missing stop hint: %q", userMsg)
	}
}

func TestBehaviorIntent_LeadTo_OverridesMoveTo(t *testing.T) {
	// "带我去湖边" matches BOTH lead and move keywords — behavior-intent must
	// win so only npc_lead_to fires, not npc_move_to.
	agent, mp, captured := newBehaviorAgent(t)

	if _, err := agent.respond(context.Background(), "带我去湖边"); err != nil {
		t.Fatalf("respond: %v", err)
	}

	name, args := captured.get()
	if name != "npc_lead_to" {
		t.Fatalf("tool = %q, want npc_lead_to (behavior must beat move)", name)
	}
	if args["x"] != 45 || args["y"] != 30 {
		t.Errorf("coords = (%v,%v), want (45,30) for 湖边", args["x"], args["y"])
	}
	if args["map"] != "Farm" {
		t.Errorf("map = %v, want Farm", args["map"])
	}

	userMsg := lastUserMessage(mp)
	if !strings.Contains(userMsg, "湖边") {
		t.Errorf("user message should mention 湖边, got: %q", userMsg)
	}
}

func TestBehaviorIntent_LeadWithoutDestination_FallsBackToFollow(t *testing.T) {
	agent, _, captured := newBehaviorAgent(t)

	if _, err := agent.respond(context.Background(), "带路"); err != nil {
		t.Fatalf("respond: %v", err)
	}

	name, _ := captured.get()
	if name != "npc_follow_start" {
		t.Fatalf("tool = %q, want npc_follow_start (lead w/o dest falls back)", name)
	}
}

func TestBehaviorIntent_NoIntent_LeavesMoveIntentAlone(t *testing.T) {
	agent, _, captured := newBehaviorAgent(t)

	// "走到湖边" is move-intent only, no behavior keyword. npc_move_to must
	// fire normally.
	if _, err := agent.respond(context.Background(), "走到湖边"); err != nil {
		t.Fatalf("respond: %v", err)
	}

	name, _ := captured.get()
	if name != "npc_move_to" {
		t.Fatalf("tool = %q, want npc_move_to", name)
	}
}

// lastUserMessage extracts the final user message from a mockProvider's last
// recorded call; keeps the behavior-intent asserts small and readable.
func lastUserMessage(mp *mockProvider) string {
	if len(mp.calls) == 0 {
		return ""
	}
	last := mp.calls[len(mp.calls)-1]
	var out string
	for _, m := range last.Messages {
		if m.Role == llm.RoleUser {
			out = m.Content
		}
	}
	return out
}
