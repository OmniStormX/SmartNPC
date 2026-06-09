package tools

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/OmniStormX/SmartNPC/adapters/stardew/bridge"
)

// newWorldActionClientServer creates an in-memory MCP client/server with the
// world-action tools wired through a real bridge.WSClient backed by a fake
// mod ws server. The fake just echoes {ok:true, npc:<npc>} so tools can
// assert their plumbing without needing the real Mod.
func newWorldActionClientServer(t *testing.T) (*mcp.ClientSession, context.Context, func()) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)

	srv := bridge.NewTestServer(func(_ context.Context, _ string, params json.RawMessage) (any, error) {
		var p struct {
			NPC string `json:"npc"`
		}
		_ = json.Unmarshal(params, &p)
		return map[string]any{"ok": true, "npc": p.NPC, "message": "stub: mod ack"}, nil
	})
	br := bridge.NewWSClient(bridge.WSClientOptions{URL: srv.URL_WS()})
	if err := br.Connect(ctx); err != nil {
		cancel()
		srv.Close()
		t.Fatalf("ws connect: %v", err)
	}

	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "test"}, nil)
	registerNpcWorldAction(server, br)

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

// ── ListTools ─────────────────────────────────────────────────────

func TestNpcWorldAction_ListTools(t *testing.T) {
	cs, ctx, cleanup := newWorldActionClientServer(t)
	defer cleanup()

	listed, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	want := map[string]bool{
		"npc_wander":         false,
		"npc_clear_debris":   false,
		"npc_water_crops":    false,
		"npc_harvest_crops":  false,
		"npc_deposit_items":  false,
		"npc_deliver_items":  false,
		"npc_forage_collect": false,
		"npc_pet_animal":     false,
		"npc_plant_seeds":    false,
		"npc_till_soil":      false,
		"npc_inspect_object": false,
		"npc_place_object":   false,
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

// ── npc_wander ────────────────────────────────────────────────────

func TestNpcWander_EndToEnd(t *testing.T) {
	cs, ctx, cleanup := newWorldActionClientServer(t)
	defer cleanup()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "npc_wander",
		Arguments: map[string]any{"npc": "XiaMi", "duration_ticks": 300},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError: %v", res.Content)
	}
	b, _ := json.Marshal(res.StructuredContent)
	var out NpcWanderOutput
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !out.OK || out.NPC != "XiaMi" {
		t.Errorf("output: %+v", out)
	}
}

func TestNpcWander_RejectsEmptyNPC(t *testing.T) {
	cs, ctx, cleanup := newWorldActionClientServer(t)
	defer cleanup()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "npc_wander",
		Arguments: map[string]any{"npc": ""},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if !res.IsError {
		t.Error("expected IsError=true")
	}
}

// ── npc_water_crops ───────────────────────────────────────────────

func TestNpcWaterCrops_EndToEnd(t *testing.T) {
	cs, ctx, cleanup := newWorldActionClientServer(t)
	defer cleanup()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "npc_water_crops",
		Arguments: map[string]any{"npc": "XiaMi", "radius": 5, "max_count": 10},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError: %v", res.Content)
	}
	b, _ := json.Marshal(res.StructuredContent)
	var out NpcWaterCropsOutput
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !out.OK || out.NPC != "XiaMi" {
		t.Errorf("output: %+v", out)
	}
}

// ── npc_plant_seeds ───────────────────────────────────────────────

func TestNpcPlantSeeds_EndToEnd(t *testing.T) {
	cs, ctx, cleanup := newWorldActionClientServer(t)
	defer cleanup()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "npc_plant_seeds",
		Arguments: map[string]any{"npc": "XiaMi", "seed_id": "(O)472", "radius": 3},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError: %v", res.Content)
	}
	b, _ := json.Marshal(res.StructuredContent)
	var out NpcPlantSeedsOutput
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !out.OK || out.NPC != "XiaMi" {
		t.Errorf("output: %+v", out)
	}
}

func TestNpcPlantSeeds_RejectsEmptySeedID(t *testing.T) {
	cs, ctx, cleanup := newWorldActionClientServer(t)
	defer cleanup()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "npc_plant_seeds",
		Arguments: map[string]any{"npc": "XiaMi", "seed_id": ""},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if !res.IsError {
		t.Error("expected IsError=true for empty seed_id")
	}
}

// ── npc_inspect_object ────────────────────────────────────────────

func TestNpcInspectObject_EndToEnd(t *testing.T) {
	cs, ctx, cleanup := newWorldActionClientServer(t)
	defer cleanup()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "npc_inspect_object",
		Arguments: map[string]any{"npc": "XiaMi", "x": 10, "y": 15},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError: %v", res.Content)
	}
	b, _ := json.Marshal(res.StructuredContent)
	var out NpcInspectObjectOutput
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !out.OK || out.NPC != "XiaMi" {
		t.Errorf("output: %+v", out)
	}
}

// ── npc_forage_collect ────────────────────────────────────────────

func TestNpcForageCollect_EndToEnd(t *testing.T) {
	cs, ctx, cleanup := newWorldActionClientServer(t)
	defer cleanup()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "npc_forage_collect",
		Arguments: map[string]any{"npc": "XiaMi", "radius": 8, "max_count": 5},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError: %v", res.Content)
	}
	b, _ := json.Marshal(res.StructuredContent)
	var out NpcForageCollectOutput
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !out.OK || out.NPC != "XiaMi" {
		t.Errorf("output: %+v", out)
	}
}

func TestNpcForageCollect_RejectsEmptyNPC(t *testing.T) {
	cs, ctx, cleanup := newWorldActionClientServer(t)
	defer cleanup()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "npc_forage_collect",
		Arguments: map[string]any{"npc": ""},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if !res.IsError {
		t.Error("expected IsError=true for empty npc")
	}
}
