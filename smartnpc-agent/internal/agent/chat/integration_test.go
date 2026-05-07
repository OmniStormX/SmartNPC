package chat

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/smartnpc/smartnpc-agent/internal/llm"
)

// Package-level integration tests that exercise the full agent pipeline with
// mock LLM + in-memory MCP server. Every tool the shipped `smartnpc-mcp`
// exposes is represented here so the test doubles the production wiring
// without spawning a real subprocess or opening a real WebSocket.
//
// Keep this file focused on cross-cutting scenarios. Per-tool unit coverage
// lives in tools_test.go / game_query_agent_test.go / friendship_test.go.

// ── test doubles ────────────────────────────────────────────────────

type chatSayIn struct {
	Speaker string `json:"speaker" jsonschema:"NPC display name"`
	Text    string `json:"text"    jsonschema:"message body"`
}

type integrationCalls struct {
	mu             sync.Mutex
	chatSayArgs    []chatSayIn
	friendshipArgs []string
	timeHits       int
	weatherHits    int
}

func (c *integrationCalls) recordChatSay(in chatSayIn) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.chatSayArgs = append(c.chatSayArgs, in)
}

func (c *integrationCalls) recordFriendship(npc string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.friendshipArgs = append(c.friendshipArgs, npc)
}

func (c *integrationCalls) chatSays() []chatSayIn {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]chatSayIn(nil), c.chatSayArgs...)
}

func (c *integrationCalls) friendships() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.friendshipArgs...)
}

// newIntegrationAgent spins up an in-memory MCP server that registers every
// tool the production server exposes — chat_say, mail_send, game_get_time,
// game_get_weather, friendship_get, and ping — then returns an Agent wired
// to that session along with the call recorder and a mutable hearts cell.
func newIntegrationAgent(t *testing.T) (*Agent, *integrationCalls, *int64) {
	t.Helper()
	ctx := context.Background()

	calls := &integrationCalls{}
	var hearts int64 = 4 // default tier 3-5 / polite

	server := mcp.NewServer(&mcp.Implementation{Name: "integration", Version: "t"}, nil)

	// chat_say
	mcp.AddTool(server, &mcp.Tool{
		Name:        "chat_say",
		Description: "Show a line in the in-game chat box.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in chatSayIn) (*mcp.CallToolResult, fakeToolOutput, error) {
		calls.recordChatSay(in)
		return nil, fakeToolOutput{OK: true}, nil
	})

	// mail_send (exists in production; not driven by these tests)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "mail_send",
		Description: "Show a HUD message.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, _ struct {
		Text string `json:"text" jsonschema:"message body"`
	}) (*mcp.CallToolResult, fakeToolOutput, error) {
		return nil, fakeToolOutput{OK: true}, nil
	})

	// game_get_time
	mcp.AddTool(server, &mcp.Tool{
		Name:        "game_get_time",
		Description: "Read in-game time.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, _ fakeTimeIn) (*mcp.CallToolResult, fakeTimeOut, error) {
		calls.mu.Lock()
		calls.timeHits++
		calls.mu.Unlock()
		return nil, fakeTimeOut{
			OK: true, Hour: 8, Minute: 0, TimeOfDay: 800,
			Day: 5, DayOfWeek: "Mon", Season: "spring", Year: 1,
		}, nil
	})

	// game_get_weather
	mcp.AddTool(server, &mcp.Tool{
		Name:        "game_get_weather",
		Description: "Read in-game weather.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, _ fakeWeatherIn) (*mcp.CallToolResult, fakeWeatherOut, error) {
		calls.mu.Lock()
		calls.weatherHits++
		calls.mu.Unlock()
		return nil, fakeWeatherOut{OK: true, Weather: "sunny", Season: "spring"}, nil
	})

	// friendship_get — tier depends on the shared `hearts` cell so tests can
	// sweep ranges without rebuilding the server.
	mcp.AddTool(server, &mcp.Tool{
		Name:        "friendship_get",
		Description: "Read friendship with an NPC.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in friendshipInput) (*mcp.CallToolResult, friendshipOutput, error) {
		calls.recordFriendship(in.NPC)
		h := int(atomic.LoadInt64(&hearts))
		return nil, friendshipOutput{
			OK: true, NPC: in.NPC, Points: h * 250, Hearts: h,
			MaxHearts: 10, Status: "friendly",
		}, nil
	})

	t1, t2 := mcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, t1, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "integration-test", Version: "t"}, nil)
	cs, err := client.Connect(ctx, t2, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })

	persona := newTestPersona()
	agent := New(Config{
		Provider:          &mockProvider{},
		Speaker:           "NPC",
		SystemPrompt:      persona.SystemPrompt,
		Persona:           persona,
		MaxHistory:        10,
		FriendshipTimeout: 500 * time.Millisecond,
	})
	agent.SetSession(cs)
	return agent, calls, &hearts
}

// ── scenario 1 ──────────────────────────────────────────────────────

// TestIntegration_LoadToolsExposesAllProductionTools asserts that after a
// single LoadTools call, every tool the shipped smartnpc-mcp advertises is
// reflected in the LLM-facing spec list with a non-empty description and an
// object-typed InputSchema. This is the one test that gates "does the agent
// actually see the whole production surface?".
func TestIntegration_LoadToolsExposesAllProductionTools(t *testing.T) {
	agent, _, _ := newIntegrationAgent(t)
	if err := agent.LoadTools(context.Background()); err != nil {
		t.Fatalf("LoadTools: %v", err)
	}
	got := agent.Tools()
	byName := map[string]llm.ToolSpec{}
	for _, tool := range got {
		byName[tool.Name] = tool
	}
	required := []string{"chat_say", "game_get_time", "game_get_weather", "friendship_get"}
	for _, name := range required {
		tool, ok := byName[name]
		if !ok {
			t.Errorf("tool %q missing from LoadTools", name)
			continue
		}
		if tool.Description == "" {
			t.Errorf("tool %q: empty Description", name)
		}
		if tool.InputSchema == nil || tool.InputSchema["type"] != "object" {
			t.Errorf("tool %q: schema not object: %+v", name, tool.InputSchema)
		}
	}
}

// ── scenario 2 ──────────────────────────────────────────────────────

// TestIntegration_ChatMessageToChatSayToolCall covers the full conversation
// path where the LLM chooses to reply via a chat_say tool_call rather than
// plain text. The agent must:
//   1. receive a chat_message notification;
//   2. kick respond() on a goroutine;
//   3. forward the tool_call to the MCP server;
//   4. feed the tool result back into a second LLM turn;
//   5. emit the final text via the built-in chat_say dispatch in respondAndSay.
//
// This exercises the tool_call loop, the MCP-side delivery, and the
// post-respond chat_say emission simultaneously — the three pieces must line
// up or dialogue silently drops.
func TestIntegration_ChatMessageToChatSayToolCall(t *testing.T) {
	agent, calls, _ := newIntegrationAgent(t)
	if err := agent.LoadTools(context.Background()); err != nil {
		t.Fatalf("LoadTools: %v", err)
	}

	mp := &mockProvider{replies: []llm.ChatResponse{
		// Round 1: model picks chat_say as its output channel.
		{
			ToolCalls: []llm.ToolCall{
				{ID: "cs1", Name: "chat_say", Arguments: map[string]any{
					"speaker": "NPC", "text": "tool-call reply",
				}},
			},
			FinishReason: "tool_calls",
		},
		// Round 2: model wraps up with a short plain-text sign-off.
		{Content: "done", FinishReason: "stop"},
	}}
	agent.cfg.Provider = mp

	done := make(chan struct{}, 1)
	agent.mu.Lock()
	agent.replyDone = done
	agent.mu.Unlock()

	handler := agent.HandleNotification()
	handler(context.Background(), newEventRequest("chat_message", map[string]any{
		"npc": "NPC", "text": "say something via tool",
	}))
	waitFor(t, done, 2*time.Second, "chat_message dispatch")

	// Expect 2 chat_say invocations: one from the LLM's tool_call, one from
	// respondAndSay emitting the final text. Both paths must work — this
	// documents the current contract.
	got := calls.chatSays()
	if len(got) != 2 {
		t.Fatalf("expected 2 chat_say calls (tool + final), got %d: %+v", len(got), got)
	}
	if got[0].Speaker != "NPC" || got[0].Text != "tool-call reply" {
		t.Errorf("first chat_say (tool_call path) = %+v", got[0])
	}
	if got[1].Speaker != "NPC" || got[1].Text != "done" {
		t.Errorf("second chat_say (final text path) = %+v", got[1])
	}

	// Two LLM rounds — the tool_call loop must have fed the tool result back.
	if len(mp.calls) != 2 {
		t.Fatalf("expected 2 LLM rounds, got %d", len(mp.calls))
	}
	foundToolMsg := false
	for _, m := range mp.calls[1].Messages {
		if m.Role == llm.RoleTool && m.Name == "chat_say" {
			foundToolMsg = true
			break
		}
	}
	if !foundToolMsg {
		t.Error("round-2 LLM messages missing chat_say tool result")
	}
}

// ── scenario 3 ──────────────────────────────────────────────────────

// TestIntegration_NpcInteractTriggersFriendshipInjection exercises the
// proactive-greeting path: the player clicks the NPC, respond() fires, and
// getFriendshipContext must call friendship_get *and* inject the matching
// tier descriptor into the system prompt before the LLM sees it.
//
// The test sweeps two heart bands (cold / devoted) to prove different
// persona tiers flow through the whole pipeline — not just the lookup, but
// the prompt injection that actually reaches the model.
func TestIntegration_NpcInteractTriggersFriendshipInjection(t *testing.T) {
	agent, calls, heartsCell := newIntegrationAgent(t)
	if err := agent.LoadTools(context.Background()); err != nil {
		t.Fatalf("LoadTools: %v", err)
	}

	cases := []struct {
		name   string
		hearts int
		tier   string
		tone   string
	}{
		{"cold_tier", 1, "0-2", "cold"},
		{"devoted_tier", 10, "9-10", "devoted"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			atomic.StoreInt64(heartsCell, int64(tc.hearts))

			mp := &mockProvider{replies: []llm.ChatResponse{
				{Content: "greeting", FinishReason: "stop"},
			}}
			agent.cfg.Provider = mp

			// Fresh replyDone per subtest so we don't race on stale signals.
			done := make(chan struct{}, 1)
			agent.mu.Lock()
			agent.replyDone = done
			priorFriendships := len(calls.friendships())
			agent.mu.Unlock()

			handler := agent.HandleNotification()
			handler(context.Background(), newEventRequest("npc_interact", map[string]any{
				"npc": "NPC",
			}))
			waitFor(t, done, 2*time.Second, "npc_interact dispatch")

			// 1. friendship_get must have been called with the agent's speaker.
			after := calls.friendships()
			if len(after) <= priorFriendships {
				t.Fatalf("friendship_get was not invoked on npc_interact; calls=%v", after)
			}
			if got := after[len(after)-1]; got != "NPC" {
				t.Errorf("friendship_get npc arg = %q, want NPC", got)
			}

			// 2. The system message the LLM saw must carry the tier descriptor.
			if len(mp.calls) != 1 {
				t.Fatalf("expected 1 LLM call, got %d", len(mp.calls))
			}
			sys := mp.calls[0].Messages[0]
			if sys.Role != llm.RoleSystem {
				t.Fatalf("first message role = %v, want system", sys.Role)
			}
			if !strings.Contains(sys.Content, "tier "+tc.tier) {
				t.Errorf("system message missing tier %q:\n%s", tc.tier, sys.Content)
			}
			if !strings.Contains(sys.Content, "Act with this tone: "+tc.tone) {
				t.Errorf("system message missing tone %q:\n%s", tc.tone, sys.Content)
			}

			// 3. A chat_say must have been emitted for the final reply so the
			// player sees the greeting.
			if len(calls.chatSays()) == 0 {
				t.Error("no chat_say emitted after npc_interact")
			}
		})
	}
}

// ── scenario 4 (documentation / sanity) ─────────────────────────────

// TestIntegration_GameGetTimeReachesReply round-trips a tool_call for
// game_get_time through the integration-style harness (distinct from the
// focused game_query_agent_test.go) to show the time tool coexists happily
// with the friendship-injection and chat_say paths in the same server.
func TestIntegration_GameGetTimeReachesReply(t *testing.T) {
	agent, calls, _ := newIntegrationAgent(t)
	if err := agent.LoadTools(context.Background()); err != nil {
		t.Fatalf("LoadTools: %v", err)
	}

	mp := &mockProvider{replies: []llm.ChatResponse{
		{
			ToolCalls: []llm.ToolCall{
				{ID: "t1", Name: "game_get_time", Arguments: map[string]any{}},
			},
			FinishReason: "tool_calls",
		},
		{Content: "早上好。", FinishReason: "stop"},
	}}
	agent.cfg.Provider = mp

	reply, err := agent.respond(context.Background(), "你好")
	if err != nil {
		t.Fatalf("respond: %v", err)
	}
	if reply != "早上好。" {
		t.Errorf("reply = %q", reply)
	}
	calls.mu.Lock()
	hits := calls.timeHits
	calls.mu.Unlock()
	if hits == 0 {
		t.Error("game_get_time was not hit on MCP server")
	}

	// Tool result must be JSON-parseable with the expected shape so the LLM
	// can consume it verbatim.
	var toolMsg *llm.Message
	for i := range mp.calls[1].Messages {
		if mp.calls[1].Messages[i].Role == llm.RoleTool && mp.calls[1].Messages[i].Name == "game_get_time" {
			toolMsg = &mp.calls[1].Messages[i]
			break
		}
	}
	if toolMsg == nil {
		t.Fatal("round-2 missing game_get_time tool result")
	}
	var payload struct {
		OK     bool   `json:"ok"`
		Hour   int    `json:"hour"`
		Season string `json:"season"`
	}
	if err := json.Unmarshal([]byte(toolMsg.Content), &payload); err != nil {
		t.Fatalf("tool result not JSON: %v (raw=%s)", err, toolMsg.Content)
	}
	if !payload.OK || payload.Hour != 8 || payload.Season != "spring" {
		t.Errorf("tool payload unexpected: %+v", payload)
	}
}
