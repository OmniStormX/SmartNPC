package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/smartnpc/smartnpc-mcp/internal/bridge"
)

// newClientServer wires an MCP server + client over in-memory transports with
// a bridge.TestServer backing any mod-backed tools. Returns the client session
// and a cleanup func; t.Cleanup-style usage keeps tests short.
func newClientServer(t *testing.T, h bridge.TestActionHandler) (*mcp.ClientSession, context.Context, func()) {
	t.Helper()

	srv := bridge.NewTestServer(h)
	br := bridge.NewWSClient(bridge.WSClientOptions{URL: srv.URL_WS()})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	if err := br.Connect(ctx); err != nil {
		srv.Close()
		cancel()
		t.Fatalf("ws connect: %v", err)
	}

	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "test"}, nil)
	registerGameQuery(server, br)

	t1, t2 := mcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, t1, nil); err != nil {
		cancel()
		br.Close()
		srv.Close()
		t.Fatalf("server connect: %v", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "t"}, nil)
	cs, err := client.Connect(ctx, t2, nil)
	if err != nil {
		cancel()
		br.Close()
		srv.Close()
		t.Fatalf("client connect: %v", err)
	}

	cleanup := func() {
		cs.Close()
		br.Close()
		srv.Close()
		cancel()
	}
	return cs, ctx, cleanup
}

// ── listing: all three tools must be discoverable ──────────────

func TestGameQuery_ListTools(t *testing.T) {
	cs, ctx, cleanup := newClientServer(t, func(context.Context, string, json.RawMessage) (any, error) {
		return nil, nil
	})
	defer cleanup()

	listed, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	want := map[string]bool{
		"game_get_time":    false,
		"game_get_weather": false,
		"friendship_get":   false,
	}
	for _, tool := range listed.Tools {
		if _, ok := want[tool.Name]; ok {
			want[tool.Name] = true
			if tool.Description == "" {
				t.Errorf("tool %s has empty description", tool.Name)
			}
			if tool.InputSchema == nil {
				t.Errorf("tool %s has nil InputSchema", tool.Name)
			}
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("tool %q missing from ListTools", name)
		}
	}
}

// ── game_get_time ──────────────────────────────────────────────

func TestGameGetTime_EndToEnd(t *testing.T) {
	cs, ctx, cleanup := newClientServer(t, func(_ context.Context, action string, params json.RawMessage) (any, error) {
		if action != bridge.ActionGameGetTime {
			t.Errorf("action=%s want %s", action, bridge.ActionGameGetTime)
		}
		return map[string]any{
			"ok":          true,
			"hour":        14,
			"minute":      30,
			"timeOfDay":   1430,
			"day":         12,
			"day_of_week": "Mon",
			"season":      "spring",
			"year":        2,
		}, nil
	})
	defer cleanup()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "game_get_time"})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError: %v", res.Content)
	}
	b, _ := json.Marshal(res.StructuredContent)
	var out GameGetTimeOutput
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !out.OK || out.Hour != 14 || out.Minute != 30 || out.TimeOfDay != 1430 {
		t.Errorf("time fields: %+v", out)
	}
	if out.Season != "spring" || out.Year != 2 || out.DayOfWeek != "Mon" {
		t.Errorf("date fields: %+v", out)
	}
}

func TestGameGetTime_ModError(t *testing.T) {
	cs, ctx, cleanup := newClientServer(t, func(context.Context, string, json.RawMessage) (any, error) {
		return nil, fmt.Errorf("no save loaded")
	})
	defer cleanup()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "game_get_time"})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if !res.IsError {
		t.Error("expected IsError=true when mod reports failure")
	}
}

// ── game_get_weather ───────────────────────────────────────────

func TestGameGetWeather_EndToEnd(t *testing.T) {
	cs, ctx, cleanup := newClientServer(t, func(_ context.Context, action string, _ json.RawMessage) (any, error) {
		if action != bridge.ActionGameGetWeather {
			t.Errorf("action=%s", action)
		}
		return map[string]any{
			"ok":           true,
			"weather":      "rainy",
			"is_raining":   true,
			"is_snowing":   false,
			"is_lightning": false,
			"season":       "summer",
		}, nil
	})
	defer cleanup()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "game_get_weather"})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError: %v", res.Content)
	}
	b, _ := json.Marshal(res.StructuredContent)
	var out GameGetWeatherOutput
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !out.OK || out.Weather != "rainy" || !out.IsRaining || out.IsSnowing || out.IsLightning {
		t.Errorf("weather flags: %+v", out)
	}
	if out.Season != "summer" {
		t.Errorf("season=%q want summer", out.Season)
	}
}

func TestGameGetWeather_ModError(t *testing.T) {
	cs, ctx, cleanup := newClientServer(t, func(context.Context, string, json.RawMessage) (any, error) {
		return nil, fmt.Errorf("world not ready")
	})
	defer cleanup()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "game_get_weather"})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if !res.IsError {
		t.Error("expected IsError=true")
	}
}

// ── friendship_get ─────────────────────────────────────────────

func TestFriendshipGet_EndToEnd(t *testing.T) {
	var got FriendshipGetInput
	cs, ctx, cleanup := newClientServer(t, func(_ context.Context, action string, params json.RawMessage) (any, error) {
		if action != bridge.ActionFriendshipGet {
			t.Errorf("action=%s", action)
		}
		if err := json.Unmarshal(params, &got); err != nil {
			t.Fatalf("decode params: %v", err)
		}
		return map[string]any{
			"ok":         true,
			"npc":        got.NPC,
			"points":     1250,
			"hearts":     5,
			"max_hearts": 10,
			"status":     "friendly",
		}, nil
	})
	defer cleanup()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "friendship_get",
		Arguments: map[string]any{"npc": "Abigail"},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError: %v", res.Content)
	}
	if got.NPC != "Abigail" {
		t.Errorf("mod received npc=%q", got.NPC)
	}
	b, _ := json.Marshal(res.StructuredContent)
	var out FriendshipGetOutput
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !out.OK || out.NPC != "Abigail" || out.Points != 1250 || out.Hearts != 5 || out.MaxHearts != 10 {
		t.Errorf("friendship output: %+v", out)
	}
	if out.Status != "friendly" {
		t.Errorf("status=%q want friendly", out.Status)
	}
}

func TestFriendshipGet_RejectsEmptyNPC(t *testing.T) {
	cs, ctx, cleanup := newClientServer(t, func(context.Context, string, json.RawMessage) (any, error) {
		t.Fatal("handler should not be reached for empty npc")
		return nil, nil
	})
	defer cleanup()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "friendship_get",
		Arguments: map[string]any{"npc": ""},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if !res.IsError {
		t.Error("expected IsError=true for empty npc")
	}
}

func TestFriendshipGet_ModError(t *testing.T) {
	cs, ctx, cleanup := newClientServer(t, func(context.Context, string, json.RawMessage) (any, error) {
		return nil, fmt.Errorf("unknown npc")
	})
	defer cleanup()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "friendship_get",
		Arguments: map[string]any{"npc": "Nobody"},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if !res.IsError {
		t.Error("expected IsError=true when mod fails")
	}
}
