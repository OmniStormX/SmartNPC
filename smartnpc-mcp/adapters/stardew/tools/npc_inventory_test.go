package tools

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/OmniStormX/SmartNPC/adapters/stardew/bridge"
)

func newInventoryClientServer(t *testing.T) (*mcp.ClientSession, context.Context, func()) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)

	srv := bridge.NewTestServer(func(_ context.Context, action string, params json.RawMessage) (any, error) {
		var p struct {
			NPC    string `json:"npc"`
			ItemId string `json:"item_id"`
			Count  int    `json:"count"`
		}
		_ = json.Unmarshal(params, &p)
		switch action {
		case bridge.ActionNpcInventoryGet:
			return map[string]any{
				"ok":  true,
				"npc": p.NPC,
				"items": []map[string]any{
					{"item_id": "(O)390", "count": 3, "quality": 0},
				},
			}, nil
		case bridge.ActionNpcInventoryPut:
			cnt := p.Count
			if cnt <= 0 {
				cnt = 1
			}
			return map[string]any{"ok": true, "npc": p.NPC, "new_total": cnt}, nil
		case bridge.ActionNpcInventoryTake:
			return map[string]any{"ok": true, "npc": p.NPC, "taken": 0}, nil
		}
		return map[string]any{"ok": true, "npc": p.NPC}, nil
	})

	br := bridge.NewWSClient(bridge.WSClientOptions{URL: srv.URL_WS()})
	if err := br.Connect(ctx); err != nil {
		cancel()
		srv.Close()
		t.Fatalf("ws connect: %v", err)
	}

	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "test"}, nil)
	registerNpcInventory(server, br)

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

func TestNpcInventoryGet_ReturnsItems(t *testing.T) {
	cs, ctx, cleanup := newInventoryClientServer(t)
	defer cleanup()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "npc_inventory_get",
		Arguments: map[string]any{"npc": "Abigail"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError: %v", res.Content)
	}
	b, _ := json.Marshal(res.StructuredContent)
	var out NpcInventoryGetOutput
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !out.OK {
		t.Fatal("expected ok=true")
	}
	if len(out.Items) != 1 || out.Items[0].ItemId != "(O)390" || out.Items[0].Count != 3 {
		t.Fatalf("unexpected items: %+v", out.Items)
	}
}

func TestNpcInventoryPut_ReturnsNewTotal(t *testing.T) {
	cs, ctx, cleanup := newInventoryClientServer(t)
	defer cleanup()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "npc_inventory_put",
		Arguments: map[string]any{"npc": "Abigail", "item_id": "(O)390", "count": 1},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError: %v", res.Content)
	}
	b, _ := json.Marshal(res.StructuredContent)
	var out NpcInventoryPutOutput
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !out.OK || out.NewTotal != 1 {
		t.Fatalf("unexpected output: %+v", out)
	}
}

func TestNpcInventoryTake_ReturnsZeroWhenEmpty(t *testing.T) {
	cs, ctx, cleanup := newInventoryClientServer(t)
	defer cleanup()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "npc_inventory_take",
		Arguments: map[string]any{"npc": "Abigail", "item_id": "(O)999", "count": 5},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError: %v", res.Content)
	}
	b, _ := json.Marshal(res.StructuredContent)
	var out NpcInventoryTakeOutput
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Taken != 0 {
		t.Fatalf("expected taken=0, got %d", out.Taken)
	}
}
