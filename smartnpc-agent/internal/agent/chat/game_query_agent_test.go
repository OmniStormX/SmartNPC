package chat

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/smartnpc/smartnpc-agent/internal/llm"
)

// ── fake game-query inputs/outputs (mirror smartnpc-mcp shape) ──────

type fakeTimeIn struct{}
type fakeTimeOut struct {
	OK        bool   `json:"ok"`
	Hour      int    `json:"hour"`
	Minute    int    `json:"minute"`
	TimeOfDay int    `json:"timeOfDay"`
	Day       int    `json:"day"`
	DayOfWeek string `json:"day_of_week"`
	Season    string `json:"season"`
	Year      int    `json:"year"`
}

type fakeWeatherIn struct{}
type fakeWeatherOut struct {
	OK          bool   `json:"ok"`
	Weather     string `json:"weather"`
	IsRaining   bool   `json:"is_raining"`
	IsSnowing   bool   `json:"is_snowing"`
	IsLightning bool   `json:"is_lightning"`
	Season      string `json:"season"`
}

type fakeFriendIn struct {
	NPC string `json:"npc" jsonschema:"NPC internal name"`
}
type fakeFriendOut struct {
	OK        bool   `json:"ok"`
	NPC       string `json:"npc"`
	Points    int    `json:"points"`
	Hearts    int    `json:"hearts"`
	MaxHearts int    `json:"max_hearts"`
	Status    string `json:"status"`
}

// newAgentWithGameQueryTools wires an in-memory MCP server with fake
// implementations of the three query tools and attaches it to a fresh Agent.
// Returned map lets tests inspect which tools were actually called and with
// what args.
func newAgentWithGameQueryTools(t *testing.T) (*Agent, map[string]map[string]any) {
	t.Helper()
	ctx := context.Background()

	server := mcp.NewServer(&mcp.Implementation{Name: "fake-game", Version: "t"}, nil)
	called := make(map[string]map[string]any)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "game_get_time",
		Description: "read in-game time",
	}, func(_ context.Context, _ *mcp.CallToolRequest, _ fakeTimeIn) (*mcp.CallToolResult, fakeTimeOut, error) {
		called["game_get_time"] = map[string]any{}
		return nil, fakeTimeOut{
			OK: true, Hour: 8, Minute: 30, TimeOfDay: 830,
			Day: 5, DayOfWeek: "Mon", Season: "spring", Year: 1,
		}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "game_get_weather",
		Description: "read in-game weather",
	}, func(_ context.Context, _ *mcp.CallToolRequest, _ fakeWeatherIn) (*mcp.CallToolResult, fakeWeatherOut, error) {
		called["game_get_weather"] = map[string]any{}
		return nil, fakeWeatherOut{
			OK: true, Weather: "rainy", IsRaining: true, Season: "spring",
		}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "friendship_get",
		Description: "read friendship",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in fakeFriendIn) (*mcp.CallToolResult, fakeFriendOut, error) {
		called["friendship_get"] = map[string]any{"npc": in.NPC}
		return nil, fakeFriendOut{
			OK: true, NPC: in.NPC, Points: 1250, Hearts: 5, MaxHearts: 10, Status: "friendly",
		}, nil
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

	agent := New(Config{
		Provider:     &mockProvider{},
		Speaker:      "NPC",
		SystemPrompt: "test",
		MaxHistory:   10,
	})
	agent.SetSession(cs)
	return agent, called
}

// TestAgent_GameGetTime_EndToEnd — mock LLM asks for game_get_time, agent runs
// it against in-memory MCP server, the JSON result is fed back into the next
// LLM turn, and the final text reply references the morning greeting.
func TestAgent_GameGetTime_EndToEnd(t *testing.T) {
	agent, called := newAgentWithGameQueryTools(t)
	if err := agent.LoadTools(context.Background()); err != nil {
		t.Fatalf("LoadTools: %v", err)
	}

	mp := &mockProvider{replies: []llm.ChatResponse{
		// Round 1: model requests the time.
		{
			ToolCalls: []llm.ToolCall{
				{ID: "c1", Name: "game_get_time", Arguments: map[string]any{}},
			},
			FinishReason: "tool_calls",
		},
		// Round 2: model sees 08:30 and greets.
		{Content: "早上好，今天打算做什么？", FinishReason: "stop"},
	}}
	agent.cfg.Provider = mp

	reply, err := agent.respond(context.Background(), "你好")
	if err != nil {
		t.Fatalf("respond: %v", err)
	}
	if reply != "早上好，今天打算做什么？" {
		t.Errorf("reply = %q", reply)
	}
	if called["game_get_time"] == nil {
		t.Fatal("game_get_time was not called on MCP server")
	}
	if len(mp.calls) != 2 {
		t.Fatalf("expected 2 LLM calls, got %d", len(mp.calls))
	}

	// The second LLM call MUST include a tool-result message containing the
	// JSON output the MCP server produced — that's what lets the LLM say
	// "morning" instead of guessing.
	second := mp.calls[1].Messages
	var toolMsg *llm.Message
	for i := range second {
		if second[i].Role == llm.RoleTool && second[i].Name == "game_get_time" {
			toolMsg = &second[i]
			break
		}
	}
	if toolMsg == nil {
		t.Fatalf("2nd LLM call missing tool result; msgs=%+v", second)
	}
	var parsed fakeTimeOut
	if err := json.Unmarshal([]byte(toolMsg.Content), &parsed); err != nil {
		t.Fatalf("tool result not JSON: %v (raw=%s)", err, toolMsg.Content)
	}
	if !parsed.OK || parsed.Hour != 8 || parsed.Minute != 30 || parsed.Season != "spring" {
		t.Errorf("tool result payload: %+v", parsed)
	}
}

// TestAgent_GameGetWeather_EndToEnd — weather tool round-trip.
func TestAgent_GameGetWeather_EndToEnd(t *testing.T) {
	agent, called := newAgentWithGameQueryTools(t)
	if err := agent.LoadTools(context.Background()); err != nil {
		t.Fatalf("LoadTools: %v", err)
	}

	mp := &mockProvider{replies: []llm.ChatResponse{
		{
			ToolCalls: []llm.ToolCall{
				{ID: "c2", Name: "game_get_weather", Arguments: map[string]any{}},
			},
			FinishReason: "tool_calls",
		},
		{Content: "今天下雨了，记得带伞。", FinishReason: "stop"},
	}}
	agent.cfg.Provider = mp

	reply, err := agent.respond(context.Background(), "今天天气怎么样？")
	if err != nil {
		t.Fatalf("respond: %v", err)
	}
	if called["game_get_weather"] == nil {
		t.Fatal("game_get_weather was not called")
	}
	if !strings.Contains(reply, "雨") {
		t.Errorf("reply did not reflect rainy weather: %q", reply)
	}
	// Verify tool result is raw JSON and contains is_raining=true.
	second := mp.calls[1].Messages
	var toolContent string
	for _, m := range second {
		if m.Role == llm.RoleTool && m.Name == "game_get_weather" {
			toolContent = m.Content
			break
		}
	}
	if toolContent == "" {
		t.Fatal("no tool result for game_get_weather")
	}
	var parsed fakeWeatherOut
	if err := json.Unmarshal([]byte(toolContent), &parsed); err != nil {
		t.Fatalf("tool result not JSON: %v", err)
	}
	if !parsed.IsRaining || parsed.Weather != "rainy" {
		t.Errorf("tool payload: %+v", parsed)
	}
}

// TestAgent_FriendshipGet_ArgForwarded — the NPC argument from the model's
// tool call must arrive at the MCP server verbatim.
func TestAgent_FriendshipGet_ArgForwarded(t *testing.T) {
	agent, called := newAgentWithGameQueryTools(t)
	if err := agent.LoadTools(context.Background()); err != nil {
		t.Fatalf("LoadTools: %v", err)
	}

	mp := &mockProvider{replies: []llm.ChatResponse{
		{
			ToolCalls: []llm.ToolCall{
				{ID: "c3", Name: "friendship_get", Arguments: map[string]any{"npc": "Abigail"}},
			},
			FinishReason: "tool_calls",
		},
		{Content: "五颗心了，关系还不错。", FinishReason: "stop"},
	}}
	agent.cfg.Provider = mp

	if _, err := agent.respond(context.Background(), "我和 Abigail 关系如何"); err != nil {
		t.Fatalf("respond: %v", err)
	}
	got := called["friendship_get"]
	if got == nil {
		t.Fatal("friendship_get was not invoked")
	}
	if got["npc"] != "Abigail" {
		t.Errorf("npc arg = %v, want Abigail", got["npc"])
	}
}

// TestAgent_LoadTools_ExposesGameQueryTools — sanity check that after
// LoadTools the three game-query specs are forwarded to the LLM with
// non-empty descriptions + object input schemas.
func TestAgent_LoadTools_ExposesGameQueryTools(t *testing.T) {
	agent, _ := newAgentWithGameQueryTools(t)
	if err := agent.LoadTools(context.Background()); err != nil {
		t.Fatalf("LoadTools: %v", err)
	}
	got := agent.Tools()
	byName := map[string]llm.ToolSpec{}
	for _, tool := range got {
		byName[tool.Name] = tool
	}
	for _, want := range []string{"game_get_time", "game_get_weather", "friendship_get"} {
		tool, ok := byName[want]
		if !ok {
			t.Errorf("tool %s missing", want)
			continue
		}
		if tool.Description == "" {
			t.Errorf("tool %s: empty description", want)
		}
		if tool.InputSchema == nil || tool.InputSchema["type"] != "object" {
			t.Errorf("tool %s: bad schema %+v", want, tool.InputSchema)
		}
	}
}
