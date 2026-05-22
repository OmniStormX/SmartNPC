package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/OmniStormX/SmartNPC/internal/bridge"
)

// newPerceptionClient wires an MCP server/client pair with only the perception
// tools registered, routed through a TestServer to assert mod-side contracts.
func newPerceptionClient(t *testing.T, h bridge.TestActionHandler) (*mcp.ClientSession, context.Context, func()) {
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
	registerNpcPerception(server, br)

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

// ── listing ───────────────────────────────────────────────────

func TestNpcPerception_ListTools(t *testing.T) {
	cs, ctx, cleanup := newPerceptionClient(t, func(context.Context, string, json.RawMessage) (any, error) {
		return nil, nil
	})
	defer cleanup()

	listed, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	want := map[string]bool{
		"npc_get_nearby":      false,
		"npc_get_environment": false,
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

// ── npc_get_nearby ────────────────────────────────────────────

func TestNpcGetNearby_EndToEnd(t *testing.T) {
	var got NpcGetNearbyInput
	cs, ctx, cleanup := newPerceptionClient(t, func(_ context.Context, action string, params json.RawMessage) (any, error) {
		if action != bridge.ActionNpcGetNearby {
			t.Errorf("action=%s want %s", action, bridge.ActionNpcGetNearby)
		}
		if err := json.Unmarshal(params, &got); err != nil {
			t.Fatalf("decode params: %v", err)
		}
		return map[string]any{
			"ok":     true,
			"npc":    got.NPC,
			"map":    "Farm",
			"radius": 10.0,
			"count":  2,
			"nearby": []map[string]any{
				{
					"name":     "Abigail",
					"type":     "npc",
					"x":        66.0,
					"y":        15.0,
					"distance": 2.0,
					"facing":   2,
					"map":      "Farm",
				},
				{
					"name":     "Farmer",
					"type":     "player",
					"x":        68.0,
					"y":        18.0,
					"distance": 5.0,
					"facing":   0,
					"map":      "Farm",
					"action":   "walking",
				},
			},
		}, nil
	})
	defer cleanup()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "npc_get_nearby",
		Arguments: map[string]any{"npc": "XiaMi", "radius": 10},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError: %v", res.Content)
	}
	if got.NPC != "XiaMi" || got.Radius != 10 {
		t.Errorf("mod received input: %+v", got)
	}

	b, _ := json.Marshal(res.StructuredContent)
	var out NpcGetNearbyOutput
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !out.OK || out.NPC != "XiaMi" || out.Map != "Farm" || out.Count != 2 {
		t.Errorf("header: %+v", out)
	}
	if len(out.Nearby) != 2 {
		t.Fatalf("nearby len=%d want 2", len(out.Nearby))
	}
	if out.Nearby[0].Name != "Abigail" || out.Nearby[0].Type != "npc" || out.Nearby[0].Distance != 2.0 {
		t.Errorf("nearby[0]: %+v", out.Nearby[0])
	}
	if out.Nearby[1].Name != "Farmer" || out.Nearby[1].Type != "player" || out.Nearby[1].Action != "walking" {
		t.Errorf("nearby[1]: %+v", out.Nearby[1])
	}
}

func TestNpcGetNearby_RejectsEmptyNPC(t *testing.T) {
	cs, ctx, cleanup := newPerceptionClient(t, func(context.Context, string, json.RawMessage) (any, error) {
		t.Fatal("handler should not be reached for empty npc")
		return nil, nil
	})
	defer cleanup()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "npc_get_nearby",
		Arguments: map[string]any{"npc": ""},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if !res.IsError {
		t.Error("expected IsError=true for empty npc")
	}
}

func TestNpcGetNearby_ModError(t *testing.T) {
	cs, ctx, cleanup := newPerceptionClient(t, func(context.Context, string, json.RawMessage) (any, error) {
		return nil, fmt.Errorf("npc not found")
	})
	defer cleanup()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "npc_get_nearby",
		Arguments: map[string]any{"npc": "Ghost"},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if !res.IsError {
		t.Error("expected IsError=true when mod fails")
	}
}

// ── npc_get_environment ───────────────────────────────────────

func TestNpcGetEnvironment_EndToEnd(t *testing.T) {
	var got NpcGetEnvironmentInput
	cs, ctx, cleanup := newPerceptionClient(t, func(_ context.Context, action string, params json.RawMessage) (any, error) {
		if action != bridge.ActionNpcGetEnvironment {
			t.Errorf("action=%s", action)
		}
		if err := json.Unmarshal(params, &got); err != nil {
			t.Fatalf("decode params: %v", err)
		}
		return map[string]any{
			"ok":          true,
			"npc":         got.NPC,
			"map":         "Farm",
			"x":           64.0,
			"y":           15.0,
			"facing":      2,
			"time_of_day": 1430,
			"hour":        14,
			"minute":      30,
			"season":      "spring",
			"weather":     "sunny",
			"nearby_objects": []map[string]any{
				{
					"name":     "Parsnip",
					"category": "crop",
					"x":        65.0,
					"y":        16.0,
					"distance": 1.4,
				},
			},
		}, nil
	})
	defer cleanup()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "npc_get_environment",
		Arguments: map[string]any{"npc": "XiaMi"},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError: %v", res.Content)
	}
	if got.NPC != "XiaMi" {
		t.Errorf("mod received npc=%q", got.NPC)
	}

	b, _ := json.Marshal(res.StructuredContent)
	var out NpcGetEnvironmentOutput
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !out.OK || out.NPC != "XiaMi" || out.Map != "Farm" {
		t.Errorf("env header: %+v", out)
	}
	if out.Hour != 14 || out.Minute != 30 || out.Season != "spring" || out.Weather != "sunny" {
		t.Errorf("env fields: %+v", out)
	}
	if len(out.NearbyObjects) != 1 || out.NearbyObjects[0].Category != "crop" {
		t.Errorf("nearby_objects: %+v", out.NearbyObjects)
	}
}

func TestNpcGetEnvironment_RejectsEmptyNPC(t *testing.T) {
	cs, ctx, cleanup := newPerceptionClient(t, func(context.Context, string, json.RawMessage) (any, error) {
		t.Fatal("handler should not be reached for empty npc")
		return nil, nil
	})
	defer cleanup()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "npc_get_environment",
		Arguments: map[string]any{"npc": ""},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if !res.IsError {
		t.Error("expected IsError=true for empty npc")
	}
}

func TestNpcGetEnvironment_ModError(t *testing.T) {
	cs, ctx, cleanup := newPerceptionClient(t, func(context.Context, string, json.RawMessage) (any, error) {
		return nil, fmt.Errorf("world not ready")
	})
	defer cleanup()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "npc_get_environment",
		Arguments: map[string]any{"npc": "XiaMi"},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if !res.IsError {
		t.Error("expected IsError=true when mod fails")
	}
}
