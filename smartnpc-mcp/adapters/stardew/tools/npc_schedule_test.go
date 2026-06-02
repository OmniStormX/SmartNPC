package tools

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/OmniStormX/SmartNPC/adapters/stardew/scheduler"
)

func newScheduleClientServer(t *testing.T) (*mcp.ClientSession, context.Context, *scheduler.Scheduler, func()) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	sched := scheduler.New()

	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "test"}, nil)
	registerNpcSchedule(server, sched, nil, false)

	t1, t2 := mcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, t1, nil); err != nil {
		cancel()
		t.Fatalf("server connect: %v", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "t"}, nil)
	cs, err := client.Connect(ctx, t2, nil)
	if err != nil {
		cancel()
		t.Fatalf("client connect: %v", err)
	}
	cleanup := func() {
		cs.Close()
		cancel()
	}
	return cs, ctx, sched, cleanup
}

func TestNpcSchedule_ListTools(t *testing.T) {
	cs, ctx, _, cleanup := newScheduleClientServer(t)
	defer cleanup()

	listed, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	want := map[string]bool{
		"npc_plan_day":     false,
		"npc_get_schedule": false,
	}
	for _, tool := range listed.Tools {
		if _, ok := want[tool.Name]; ok {
			want[tool.Name] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("tool %q missing from ListTools", name)
		}
	}
}

func TestNpcPlanDay_EndToEnd(t *testing.T) {
	cs, ctx, sched, cleanup := newScheduleClientServer(t)
	defer cleanup()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "npc_plan_day",
		Arguments: map[string]any{
			"npc":    "XiaMi",
			"day":    15,
			"season": "spring",
			"entries": []any{
				map[string]any{"game_hour": 7, "action": "npc_water_crops", "reason": "早起浇水"},
				map[string]any{"game_hour": 9, "action": "npc_wander", "reason": "去镇上"},
				map[string]any{"game_hour": 18, "action": "npc_approach_and_speak", "reason": "找玩家"},
			},
		},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError: %v", res.Content)
	}

	b, _ := json.Marshal(res.StructuredContent)
	var out NpcPlanDayOutput
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !out.OK || out.NPC != "XiaMi" || out.Accepted != 3 {
		t.Errorf("output: %+v", out)
	}

	// Verify scheduler actually stored it.
	got := sched.GetSchedule("XiaMi")
	if got == nil {
		t.Fatal("scheduler has no schedule for XiaMi")
	}
	if len(got.Entries) != 3 {
		t.Errorf("expected 3 entries, got %d", len(got.Entries))
	}
}

func TestNpcPlanDay_RejectsEmptyNPC(t *testing.T) {
	cs, ctx, _, cleanup := newScheduleClientServer(t)
	defer cleanup()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "npc_plan_day",
		Arguments: map[string]any{
			"npc":     "",
			"day":     1,
			"season":  "spring",
			"entries": []any{map[string]any{"game_hour": 7, "action": "npc_wander"}},
		},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if !res.IsError {
		t.Error("expected IsError=true")
	}
}

func TestNpcPlanDay_RejectsEmptyEntries(t *testing.T) {
	cs, ctx, _, cleanup := newScheduleClientServer(t)
	defer cleanup()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "npc_plan_day",
		Arguments: map[string]any{
			"npc":     "XiaMi",
			"day":     1,
			"season":  "spring",
			"entries": []any{},
		},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if !res.IsError {
		t.Error("expected IsError=true for empty entries")
	}
}

func TestNpcPlanDay_SkipsInvalidHours(t *testing.T) {
	cs, ctx, sched, cleanup := newScheduleClientServer(t)
	defer cleanup()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "npc_plan_day",
		Arguments: map[string]any{
			"npc":    "XiaMi",
			"day":    1,
			"season": "spring",
			"entries": []any{
				map[string]any{"game_hour": 3, "action": "npc_wander"},  // too early — skipped
				map[string]any{"game_hour": 30, "action": "npc_wander"}, // too late — skipped
				map[string]any{"game_hour": 12, "action": "npc_idle_activity"},
			},
		},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError: %v", res.Content)
	}
	b, _ := json.Marshal(res.StructuredContent)
	var out NpcPlanDayOutput
	_ = json.Unmarshal(b, &out)
	if out.Accepted != 1 {
		t.Errorf("expected 1 accepted (only hour 12), got %d", out.Accepted)
	}

	got := sched.GetSchedule("XiaMi")
	if got == nil || len(got.Entries) != 1 {
		t.Errorf("scheduler entries: %+v", got)
	}
}

func TestNpcGetSchedule_EndToEnd(t *testing.T) {
	cs, ctx, sched, cleanup := newScheduleClientServer(t)
	defer cleanup()

	// Pre-populate a schedule.
	sched.SetSchedule(scheduler.DaySchedule{
		NPC:    "XiaMi",
		Day:    10,
		Season: "summer",
		Year:   1,
		Entries: []scheduler.Entry{
			{GameHour: 7, Action: "npc_water_crops", Reason: "浇水"},
			{GameHour: 12, Action: "npc_idle_activity", Reason: "休息"},
			{GameHour: 18, Action: "npc_approach_and_speak", Reason: "聊天"},
		},
	})

	// Fire hour 7 so it becomes non-pending.
	sched.Tick(7)

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "npc_get_schedule",
		Arguments: map[string]any{"npc": "XiaMi"},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError: %v", res.Content)
	}

	b, _ := json.Marshal(res.StructuredContent)
	var out NpcGetScheduleOutput
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !out.OK || out.NPC != "XiaMi" {
		t.Errorf("output meta: %+v", out)
	}
	if len(out.Entries) != 2 {
		t.Fatalf("expected 2 pending entries, got %d", len(out.Entries))
	}
	if out.Entries[0].GameHour != 12 || out.Entries[1].GameHour != 18 {
		t.Errorf("unexpected entries: %+v", out.Entries)
	}
}

func TestNpcGetSchedule_NoSchedule(t *testing.T) {
	cs, ctx, _, cleanup := newScheduleClientServer(t)
	defer cleanup()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "npc_get_schedule",
		Arguments: map[string]any{"npc": "Nobody"},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError: %v", res.Content)
	}

	b, _ := json.Marshal(res.StructuredContent)
	var out NpcGetScheduleOutput
	_ = json.Unmarshal(b, &out)
	if !out.OK || len(out.Entries) != 0 {
		t.Errorf("expected OK with 0 entries, got %+v", out)
	}
}
