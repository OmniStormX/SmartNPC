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

// newMovementClientServer mirrors newClientServer but registers the movement tools.
func newMovementClientServer(t *testing.T, h bridge.TestActionHandler) (*mcp.ClientSession, context.Context, func()) {
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
	registerNpcMovement(server, br)

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

// ── listing: all three movement tools must be discoverable ─────

func TestNpcMovement_ListTools(t *testing.T) {
	cs, ctx, cleanup := newMovementClientServer(t, func(context.Context, string, json.RawMessage) (any, error) {
		return nil, nil
	})
	defer cleanup()

	listed, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	want := map[string]bool{
		"npc_move_to":        false,
		"npc_face_direction": false,
		"npc_get_position":   false,
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

// ── npc_move_to ───────────────────────────────────────────────

func TestNpcMoveTo_EndToEnd(t *testing.T) {
	var got NpcMoveToInput
	cs, ctx, cleanup := newMovementClientServer(t, func(_ context.Context, action string, params json.RawMessage) (any, error) {
		if action != bridge.ActionNpcMoveTo {
			t.Errorf("action=%s want %s", action, bridge.ActionNpcMoveTo)
		}
		if err := json.Unmarshal(params, &got); err != nil {
			t.Fatalf("decode params: %v", err)
		}
		return map[string]any{
			"ok":      true,
			"npc":     got.NPC,
			"map":     "Farm",
			"x":       got.X,
			"y":       got.Y,
			"message": "pathing",
		}, nil
	})
	defer cleanup()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "npc_move_to",
		Arguments: map[string]any{
			"npc": "XiaMi",
			"x":   68,
			"y":   18,
		},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError: %v", res.Content)
	}
	if got.NPC != "XiaMi" || got.X != 68 || got.Y != 18 {
		t.Errorf("mod received: %+v", got)
	}
	b, _ := json.Marshal(res.StructuredContent)
	var out NpcMoveToOutput
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !out.OK || out.NPC != "XiaMi" || out.Map != "Farm" || out.X != 68 || out.Y != 18 {
		t.Errorf("output: %+v", out)
	}
	if out.Message != "pathing" {
		t.Errorf("message=%q want pathing", out.Message)
	}
}

func TestNpcMoveTo_RejectsEmptyNPC(t *testing.T) {
	cs, ctx, cleanup := newMovementClientServer(t, func(context.Context, string, json.RawMessage) (any, error) {
		t.Fatal("handler should not be reached for empty npc")
		return nil, nil
	})
	defer cleanup()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "npc_move_to",
		Arguments: map[string]any{"npc": "", "x": 1, "y": 2},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if !res.IsError {
		t.Error("expected IsError=true")
	}
}

func TestNpcMoveTo_ModError(t *testing.T) {
	cs, ctx, cleanup := newMovementClientServer(t, func(context.Context, string, json.RawMessage) (any, error) {
		return nil, fmt.Errorf("no_route")
	})
	defer cleanup()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "npc_move_to",
		Arguments: map[string]any{"npc": "XiaMi", "x": 999, "y": 999},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if !res.IsError {
		t.Error("expected IsError=true when mod fails")
	}
}

// ── npc_face_direction ────────────────────────────────────────

func TestNpcFaceDirection_EndToEnd(t *testing.T) {
	var got NpcFaceDirectionInput
	cs, ctx, cleanup := newMovementClientServer(t, func(_ context.Context, action string, params json.RawMessage) (any, error) {
		if action != bridge.ActionNpcFaceDirection {
			t.Errorf("action=%s", action)
		}
		if err := json.Unmarshal(params, &got); err != nil {
			t.Fatalf("decode params: %v", err)
		}
		dirToInt := map[string]int{"up": 0, "right": 1, "down": 2, "left": 3}
		return map[string]any{
			"ok":        true,
			"npc":       got.NPC,
			"direction": got.Direction,
			"facing":    dirToInt[got.Direction],
		}, nil
	})
	defer cleanup()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "npc_face_direction",
		Arguments: map[string]any{"npc": "XiaMi", "direction": "LEFT"},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError: %v", res.Content)
	}
	if got.Direction != "left" {
		t.Errorf("expected direction normalized to lowercase, got %q", got.Direction)
	}
	b, _ := json.Marshal(res.StructuredContent)
	var out NpcFaceDirectionOutput
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !out.OK || out.NPC != "XiaMi" || out.Direction != "left" || out.Facing != 3 {
		t.Errorf("output: %+v", out)
	}
}

func TestNpcFaceDirection_RejectsInvalidDirection(t *testing.T) {
	cs, ctx, cleanup := newMovementClientServer(t, func(context.Context, string, json.RawMessage) (any, error) {
		t.Fatal("handler should not be reached for invalid direction")
		return nil, nil
	})
	defer cleanup()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "npc_face_direction",
		Arguments: map[string]any{"npc": "XiaMi", "direction": "northwest"},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if !res.IsError {
		t.Error("expected IsError=true for invalid direction")
	}
}

func TestNpcFaceDirection_RejectsEmptyNPC(t *testing.T) {
	cs, ctx, cleanup := newMovementClientServer(t, func(context.Context, string, json.RawMessage) (any, error) {
		t.Fatal("handler should not be reached for empty npc")
		return nil, nil
	})
	defer cleanup()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "npc_face_direction",
		Arguments: map[string]any{"npc": "", "direction": "up"},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if !res.IsError {
		t.Error("expected IsError=true")
	}
}

// ── npc_get_position ──────────────────────────────────────────

func TestNpcGetPosition_EndToEnd(t *testing.T) {
	var got NpcGetPositionInput
	cs, ctx, cleanup := newMovementClientServer(t, func(_ context.Context, action string, params json.RawMessage) (any, error) {
		if action != bridge.ActionNpcGetPosition {
			t.Errorf("action=%s", action)
		}
		if err := json.Unmarshal(params, &got); err != nil {
			t.Fatalf("decode params: %v", err)
		}
		return map[string]any{
			"ok":        true,
			"npc":       got.NPC,
			"x":         64.5,
			"y":         15.0,
			"map":       "Farm",
			"facing":    2,
			"direction": "down",
			"is_moving": false,
		}, nil
	})
	defer cleanup()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "npc_get_position",
		Arguments: map[string]any{"npc": "XiaMi"},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError: %v", res.Content)
	}
	b, _ := json.Marshal(res.StructuredContent)
	var out NpcGetPositionOutput
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !out.OK || out.NPC != "XiaMi" || out.Map != "Farm" || out.Facing != 2 || out.Direction != "down" {
		t.Errorf("output: %+v", out)
	}
	if out.X != 64.5 || out.Y != 15.0 {
		t.Errorf("coords: %+v", out)
	}
	if out.IsMoving {
		t.Errorf("is_moving: got true, want false")
	}
}

func TestNpcGetPosition_RejectsEmptyNPC(t *testing.T) {
	cs, ctx, cleanup := newMovementClientServer(t, func(context.Context, string, json.RawMessage) (any, error) {
		t.Fatal("handler should not be reached")
		return nil, nil
	})
	defer cleanup()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "npc_get_position",
		Arguments: map[string]any{"npc": ""},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if !res.IsError {
		t.Error("expected IsError=true for empty npc")
	}
}

func TestNpcGetPosition_ModError(t *testing.T) {
	cs, ctx, cleanup := newMovementClientServer(t, func(context.Context, string, json.RawMessage) (any, error) {
		return nil, fmt.Errorf("unknown npc")
	})
	defer cleanup()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "npc_get_position",
		Arguments: map[string]any{"npc": "Nobody"},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if !res.IsError {
		t.Error("expected IsError=true when mod fails")
	}
}
