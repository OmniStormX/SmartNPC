package tools

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/OmniStormX/SmartNPC/adapters/stardew/bridge"
)

// ── npc_inventory_get ─────────────────────────────────────────

type NpcInventoryGetInput struct {
	NPC string `json:"npc" jsonschema:"NPC internal name"`
}

type ItemSlotOutput struct {
	ItemId  string `json:"item_id"           jsonschema:"SDV qualified item id"`
	Count   int    `json:"count"             jsonschema:"stack size"`
	Quality int    `json:"quality"           jsonschema:"item quality: normal=0 silver=1 gold=2 iridium=4"`
}

type NpcInventoryGetOutput struct {
	OK    bool             `json:"ok"`
	NPC   string           `json:"npc"`
	Items []ItemSlotOutput `json:"items"`
}

// ── npc_inventory_put ─────────────────────────────────────────

type NpcInventoryPutInput struct {
	NPC     string `json:"npc"               jsonschema:"NPC internal name"`
	ItemId  string `json:"item_id"           jsonschema:"SDV qualified item id, e.g. \"(O)390\""`
	Count   int    `json:"count"             jsonschema:"amount to add (default 1)"`
	Quality int    `json:"quality,omitempty" jsonschema:"item quality: normal=0 silver=1 gold=2 iridium=4"`
}

type NpcInventoryPutOutput struct {
	OK       bool   `json:"ok"`
	NPC      string `json:"npc"`
	NewTotal int    `json:"new_total"          jsonschema:"new count for this stack after adding"`
	Message  string `json:"message,omitempty"`
}

// ── npc_inventory_take ────────────────────────────────────────

type NpcInventoryTakeInput struct {
	NPC    string `json:"npc"     jsonschema:"NPC internal name"`
	ItemId string `json:"item_id" jsonschema:"SDV qualified item id to remove"`
	Count  int    `json:"count"   jsonschema:"amount to remove"`
}

type NpcInventoryTakeOutput struct {
	OK      bool   `json:"ok"`
	NPC     string `json:"npc"`
	Taken   int    `json:"taken"            jsonschema:"how many were actually removed"`
	Message string `json:"message,omitempty"`
}

// ── registration ──────────────────────────────────────────────

func registerNpcInventory(s *mcp.Server, br *bridge.WSClient) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "npc_inventory_get",
		Description: "Return the current contents of an NPC's backpack.\n\n" +
			"When to call: before deciding whether to deliver items, after clear_debris " +
			"or forage_collect, or when the player asks what the NPC is carrying.\n\n" +
			"Side-effect: READ (no world mutation).",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in NpcInventoryGetInput) (*mcp.CallToolResult, NpcInventoryGetOutput, error) {
		if in.NPC == "" {
			return nil, NpcInventoryGetOutput{}, fmt.Errorf("npc is required")
		}
		logToolCall("npc_inventory_get", in)
		out, err := callBridge[NpcInventoryGetOutput](ctx, req, br, bridge.ActionNpcInventoryGet, in, "npc_inventory_get")
		return nil, out, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "npc_inventory_put",
		Description: "Add an item to an NPC's backpack (for scripted gift-giving or testing).\n\n" +
			"When to call: manually seeding an NPC's inventory in scenarios where the NPC " +
			"should be carrying something specific.\n\n" +
			"Side-effect: WRITE (modifies NPC inventory, no world object removed).",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in NpcInventoryPutInput) (*mcp.CallToolResult, NpcInventoryPutOutput, error) {
		if in.NPC == "" {
			return nil, NpcInventoryPutOutput{}, fmt.Errorf("npc is required")
		}
		if in.Count <= 0 {
			in.Count = 1
		}
		logToolCall("npc_inventory_put", in)
		out, err := callBridge[NpcInventoryPutOutput](ctx, req, br, bridge.ActionNpcInventoryPut, in, "npc_inventory_put")
		return nil, out, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "npc_inventory_take",
		Description: "Remove items from an NPC's backpack (does NOT give them to the player — " +
			"use npc_deliver_items for that).\n\n" +
			"When to call: discarding unwanted collected items, or adjusting inventory state.\n\n" +
			"Side-effect: WRITE (removes from NPC inventory only).",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in NpcInventoryTakeInput) (*mcp.CallToolResult, NpcInventoryTakeOutput, error) {
		if in.NPC == "" {
			return nil, NpcInventoryTakeOutput{}, fmt.Errorf("npc is required")
		}
		logToolCall("npc_inventory_take", in)
		out, err := callBridge[NpcInventoryTakeOutput](ctx, req, br, bridge.ActionNpcInventoryTake, in, "npc_inventory_take")
		return nil, out, err
	})
}
