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

// newBehaviorClientServer mirrors newMovementClientServer but registers the
// behavior tools. Kept local to this file so the movement tests stay
// self-contained.
func newBehaviorClientServer(t *testing.T, h bridge.TestActionHandler) (*mcp.ClientSession, context.Context, func()) {
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
	registerNpcBehavior(server, br)

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

// ── listing ────────────────────────────────────────────────────

func TestNpcBehavior_ListTools(t *testing.T) {
	cs, ctx, cleanup := newBehaviorClientServer(t, func(context.Context, string, json.RawMessage) (any, error) {
		return nil, nil
	})
	defer cleanup()

	listed, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	want := map[string]bool{
		"npc_summon":       false,
		"npc_follow_start": false,
		"npc_follow_stop":  false,
		"npc_lead_to":      false,
		"npc_get_behavior": false,
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

// ── npc_summon ────────────────────────────────────────────────

func TestNpcSummon_EndToEnd(t *testing.T) {
	var got NpcSummonInput
	cs, ctx, cleanup := newBehaviorClientServer(t, func(_ context.Context, action string, params json.RawMessage) (any, error) {
		if action != bridge.ActionNpcSummon {
			t.Errorf("action=%s want %s", action, bridge.ActionNpcSummon)
		}
		if err := json.Unmarshal(params, &got); err != nil {
			t.Fatalf("decode params: %v", err)
		}
		return map[string]any{
			"ok":      true,
			"npc":     got.NPC,
			"message": "approaching",
		}, nil
	})
	defer cleanup()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "npc_summon",
		Arguments: map[string]any{"npc": "XiaMi"},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError: %v", res.Content)
	}
	if got.NPC != "XiaMi" {
		t.Errorf("mod received: %+v", got)
	}
	b, _ := json.Marshal(res.StructuredContent)
	var out NpcSummonOutput
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !out.OK || out.NPC != "XiaMi" || out.Message != "approaching" {
		t.Errorf("output: %+v", out)
	}
}

func TestNpcSummon_RejectsEmptyNPC(t *testing.T) {
	cs, ctx, cleanup := newBehaviorClientServer(t, func(context.Context, string, json.RawMessage) (any, error) {
		t.Fatal("handler should not be reached for empty npc")
		return nil, nil
	})
	defer cleanup()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "npc_summon",
		Arguments: map[string]any{"npc": ""},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if !res.IsError {
		t.Error("expected IsError=true")
	}
}

func TestNpcSummon_ModError(t *testing.T) {
	cs, ctx, cleanup := newBehaviorClientServer(t, func(context.Context, string, json.RawMessage) (any, error) {
		return nil, fmt.Errorf("unknown_npc")
	})
	defer cleanup()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "npc_summon",
		Arguments: map[string]any{"npc": "Nobody"},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if !res.IsError {
		t.Error("expected IsError=true when mod fails")
	}
}

// ── npc_follow_start / npc_follow_stop ────────────────────────

func TestNpcFollowStart_EndToEnd(t *testing.T) {
	var got NpcFollowStartInput
	cs, ctx, cleanup := newBehaviorClientServer(t, func(_ context.Context, action string, params json.RawMessage) (any, error) {
		if action != bridge.ActionNpcFollowStart {
			t.Errorf("action=%s", action)
		}
		if err := json.Unmarshal(params, &got); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return map[string]any{"ok": true, "npc": got.NPC}, nil
	})
	defer cleanup()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "npc_follow_start",
		Arguments: map[string]any{"npc": "XiaMi"},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError: %v", res.Content)
	}
	b, _ := json.Marshal(res.StructuredContent)
	var out NpcFollowStartOutput
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !out.OK || out.NPC != "XiaMi" {
		t.Errorf("output: %+v", out)
	}
}

func TestNpcFollowStart_RejectsEmptyNPC(t *testing.T) {
	cs, ctx, cleanup := newBehaviorClientServer(t, func(context.Context, string, json.RawMessage) (any, error) {
		t.Fatal("handler should not be reached")
		return nil, nil
	})
	defer cleanup()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "npc_follow_start",
		Arguments: map[string]any{"npc": ""},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if !res.IsError {
		t.Error("expected IsError=true")
	}
}

func TestNpcFollowStop_EndToEnd(t *testing.T) {
	var got NpcFollowStopInput
	cs, ctx, cleanup := newBehaviorClientServer(t, func(_ context.Context, action string, params json.RawMessage) (any, error) {
		if action != bridge.ActionNpcFollowStop {
			t.Errorf("action=%s", action)
		}
		if err := json.Unmarshal(params, &got); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return map[string]any{"ok": true, "npc": got.NPC}, nil
	})
	defer cleanup()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "npc_follow_stop",
		Arguments: map[string]any{"npc": "XiaMi"},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError: %v", res.Content)
	}
	b, _ := json.Marshal(res.StructuredContent)
	var out NpcFollowStopOutput
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !out.OK || out.NPC != "XiaMi" {
		t.Errorf("output: %+v", out)
	}
}

func TestNpcFollowStop_RejectsEmptyNPC(t *testing.T) {
	cs, ctx, cleanup := newBehaviorClientServer(t, func(context.Context, string, json.RawMessage) (any, error) {
		t.Fatal("handler should not be reached")
		return nil, nil
	})
	defer cleanup()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "npc_follow_stop",
		Arguments: map[string]any{"npc": ""},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if !res.IsError {
		t.Error("expected IsError=true")
	}
}

// ── npc_lead_to ────────────────────────────────────────────────

func TestNpcLeadTo_EndToEnd(t *testing.T) {
	var got NpcLeadToInput
	cs, ctx, cleanup := newBehaviorClientServer(t, func(_ context.Context, action string, params json.RawMessage) (any, error) {
		if action != bridge.ActionNpcLeadTo {
			t.Errorf("action=%s", action)
		}
		if err := json.Unmarshal(params, &got); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return map[string]any{
			"ok":  true,
			"npc": got.NPC,
			"x":   got.X,
			"y":   got.Y,
			"map": "Farm",
		}, nil
	})
	defer cleanup()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "npc_lead_to",
		Arguments: map[string]any{
			"npc": "XiaMi",
			"x":   45,
			"y":   30,
		},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError: %v", res.Content)
	}
	if got.NPC != "XiaMi" || got.X != 45 || got.Y != 30 {
		t.Errorf("mod received: %+v", got)
	}
	b, _ := json.Marshal(res.StructuredContent)
	var out NpcLeadToOutput
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !out.OK || out.NPC != "XiaMi" || out.Map != "Farm" || out.X != 45 || out.Y != 30 {
		t.Errorf("output: %+v", out)
	}
}

func TestNpcLeadTo_RejectsEmptyNPC(t *testing.T) {
	cs, ctx, cleanup := newBehaviorClientServer(t, func(context.Context, string, json.RawMessage) (any, error) {
		t.Fatal("handler should not be reached")
		return nil, nil
	})
	defer cleanup()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "npc_lead_to",
		Arguments: map[string]any{"npc": "", "x": 1, "y": 2},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if !res.IsError {
		t.Error("expected IsError=true")
	}
}

// ── npc_get_behavior ──────────────────────────────────────────

func TestNpcGetBehavior_EndToEnd(t *testing.T) {
	var got NpcGetBehaviorInput
	cs, ctx, cleanup := newBehaviorClientServer(t, func(_ context.Context, action string, params json.RawMessage) (any, error) {
		if action != bridge.ActionNpcGetBehavior {
			t.Errorf("action=%s", action)
		}
		if err := json.Unmarshal(params, &got); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return map[string]any{
			"ok":   true,
			"npc":  got.NPC,
			"mode": "following",
		}, nil
	})
	defer cleanup()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "npc_get_behavior",
		Arguments: map[string]any{"npc": "XiaMi"},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError: %v", res.Content)
	}
	b, _ := json.Marshal(res.StructuredContent)
	var out NpcGetBehaviorOutput
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !out.OK || out.NPC != "XiaMi" || out.Mode != "following" {
		t.Errorf("output: %+v", out)
	}
}

func TestNpcGetBehavior_RejectsEmptyNPC(t *testing.T) {
	cs, ctx, cleanup := newBehaviorClientServer(t, func(context.Context, string, json.RawMessage) (any, error) {
		t.Fatal("handler should not be reached")
		return nil, nil
	})
	defer cleanup()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "npc_get_behavior",
		Arguments: map[string]any{"npc": ""},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if !res.IsError {
		t.Error("expected IsError=true")
	}
}

func TestNpcGetBehavior_ModError(t *testing.T) {
	cs, ctx, cleanup := newBehaviorClientServer(t, func(context.Context, string, json.RawMessage) (any, error) {
		return nil, fmt.Errorf("unknown_npc")
	})
	defer cleanup()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "npc_get_behavior",
		Arguments: map[string]any{"npc": "Nobody"},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if !res.IsError {
		t.Error("expected IsError=true when mod fails")
	}
}
