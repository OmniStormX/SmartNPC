package tools

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/OmniStormX/SmartNPC/adapters/stardew/scheduler"
	"github.com/OmniStormX/SmartNPC/pkg/workflow"
)

func newScheduleClientServer(t *testing.T) (*mcp.ClientSession, context.Context, *scheduler.Scheduler, func()) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	sched := scheduler.New()

	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "test"}, nil)
	registerNpcSchedule(server, sched, nil, nil, false)

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
	cs, ctx, sched, cleanup := newScheduleClientServerWithReg(t)
	defer cleanup()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "npc_plan_day",
		Arguments: map[string]any{
			"npc":    "XiaMi",
			"day":    15,
			"season": "spring",
			"entries": []any{
				map[string]any{"game_hour": 7, "workflow_id": "farm_care", "reason": "早起浇水"},
				map[string]any{"game_hour": 9, "workflow_id": "resource_gather", "reason": "采集资源"},
				map[string]any{"game_hour": 18, "workflow_id": "social_interact", "reason": "找玩家"},
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
			"entries": []any{map[string]any{"game_hour": 7, "workflow_id": "farm_care"}},
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
	cs, ctx, sched, cleanup := newScheduleClientServerWithReg(t)
	defer cleanup()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "npc_plan_day",
		Arguments: map[string]any{
			"npc":    "XiaMi",
			"day":    1,
			"season": "spring",
			"entries": []any{
				map[string]any{"game_hour": 3, "workflow_id": "farm_care"},  // too early — skipped
				map[string]any{"game_hour": 30, "workflow_id": "farm_care"}, // too late — skipped
				map[string]any{"game_hour": 12, "workflow_id": "farm_care"},
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

// ── P3 workflow-aware helpers ─────────────────────────────────────────────

func newScheduleClientServerWithReg(t *testing.T) (*mcp.ClientSession, context.Context, *scheduler.Scheduler, func()) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	sched := scheduler.New()

	reg := workflow.NewRegistry()
	if err := reg.Init(""); err != nil {
		cancel()
		t.Fatalf("registry init: %v", err)
	}

	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "test"}, nil)
	registerNpcSchedule(server, sched, nil, reg, false)

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

// ── P3: npc_plan_day three-form compatibility ─────────────────────────────

func TestPlanDay_WorkflowID_Accepted(t *testing.T) {
	cs, ctx, sched, cleanup := newScheduleClientServerWithReg(t)
	defer cleanup()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "npc_plan_day",
		Arguments: map[string]any{
			"npc":    "Abigail",
			"day":    15,
			"season": "spring",
			"entries": []any{
				map[string]any{
					"game_hour":   8,
					"workflow_id": "farm_extension",
					"args":        map[string]any{"inspect_radius": 20},
				},
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
	if !out.OK || out.Accepted != 1 {
		t.Errorf("output: %+v", out)
	}

	got := sched.GetSchedule("Abigail")
	if got == nil || len(got.Entries) != 1 {
		t.Fatalf("schedule not stored")
	}
	e := got.Entries[0]
	if e.WorkflowID != "farm_extension" {
		t.Errorf("WorkflowID = %q, want farm_extension", e.WorkflowID)
	}
	if e.Args == nil || e.Args["inspect_radius"] != float64(20) {
		t.Errorf("Args = %v, want {inspect_radius: 20}", e.Args)
	}
	if e.Action != "" {
		t.Errorf("Action = %q, want empty", e.Action)
	}
}

func TestPlanDay_WorkflowID_Unknown(t *testing.T) {
	cs, ctx, sched, cleanup := newScheduleClientServerWithReg(t)
	defer cleanup()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "npc_plan_day",
		Arguments: map[string]any{
			"npc":    "Abigail",
			"day":    15,
			"season": "spring",
			"entries": []any{
				map[string]any{
					"game_hour":   8,
					"workflow_id": "nonexistent_workflow",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	// Unknown workflow_id should be skipped (warned), not error.
	if res.IsError {
		t.Fatalf("IsError: %v (should skip invalid, not fail)", res.Content)
	}

	b, _ := json.Marshal(res.StructuredContent)
	var out NpcPlanDayOutput
	_ = json.Unmarshal(b, &out)
	if out.Accepted != 0 {
		t.Errorf("expected 0 accepted (unknown workflow_id skipped), got %d", out.Accepted)
	}

	got := sched.GetSchedule("Abigail")
	if got == nil || len(got.Entries) != 0 {
		t.Errorf("schedule should be empty, got %d entries", len(got.Entries))
	}
}

func TestPlanDay_Inline_Accepted(t *testing.T) {
	cs, ctx, sched, cleanup := newScheduleClientServerWithReg(t)
	defer cleanup()

	inline := map[string]any{
		"id": "custom_test_workflow",
		"steps": []any{
			map[string]any{
				"kind": "tool",
				"name": "npc_inspect_object",
				"args": map[string]any{"what": "farm_actions", "radius": float64(10)},
			},
			map[string]any{
				"kind": "branch",
				"when": "$obs.actions_available.water.count > 0",
				"then": []any{
					map[string]any{
						"kind": "tool",
						"name": "npc_water_crops",
					},
				},
			},
		},
	}

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "npc_plan_day",
		Arguments: map[string]any{
			"npc":    "Abigail",
			"day":    16,
			"season": "summer",
			"entries": []any{
				map[string]any{
					"game_hour": 9,
					"workflow":  inline,
					"reason":    "ad-hoc test workflow",
				},
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
		t.Errorf("expected 1 accepted, got %d", out.Accepted)
	}

	got := sched.GetSchedule("Abigail")
	if got == nil || len(got.Entries) != 1 {
		t.Fatalf("schedule not stored")
	}
	e := got.Entries[0]
	if e.Workflow == nil {
		t.Fatal("Workflow is nil — inline should be stored")
	}
	if e.Workflow.ID != "custom_test_workflow" {
		t.Errorf("workflow ID = %q", e.Workflow.ID)
	}
	if len(e.Workflow.Steps) != 2 {
		t.Errorf("expected 2 steps, got %d", len(e.Workflow.Steps))
	}
}

func TestPlanDay_Inline_RejectsInvalid(t *testing.T) {
	cs, ctx, sched, cleanup := newScheduleClientServerWithReg(t)
	defer cleanup()

	invalid := map[string]any{
		"id": "bad",
		"steps": []any{
			map[string]any{"kind": "tool"}, // no name
		},
	}

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "npc_plan_day",
		Arguments: map[string]any{
			"npc":    "Abigail",
			"day":    16,
			"season": "summer",
			"entries": []any{
				map[string]any{
					"game_hour": 9,
					"workflow":  invalid,
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError: %v (invalid inline should skip, not fail whole plan)", res.Content)
	}

	b, _ := json.Marshal(res.StructuredContent)
	var out NpcPlanDayOutput
	_ = json.Unmarshal(b, &out)
	if out.Accepted != 0 {
		t.Errorf("expected 0 accepted, got %d", out.Accepted)
	}
	_ = sched
}

func TestPlanDay_LegacyAction_Rejected(t *testing.T) {
	cs, ctx, _, cleanup := newScheduleClientServerWithReg(t)
	defer cleanup()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "npc_plan_day",
		Arguments: map[string]any{
			"npc":    "Haley",
			"day":    20,
			"season": "fall",
			"entries": []any{
				map[string]any{
					"game_hour": 10,
					"action":    "npc_water_crops",
					"reason":    "legacy action is rejected",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}

	b, _ := json.Marshal(res.StructuredContent)
	var out NpcPlanDayOutput
	_ = json.Unmarshal(b, &out)
	if out.Accepted != 0 {
		t.Fatalf("expected 0 accepted (legacy action rejected), got %d", out.Accepted)
	}
}

func TestPlanDay_Mixed_Day(t *testing.T) {
	cs, ctx, sched, cleanup := newScheduleClientServerWithReg(t)
	defer cleanup()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "npc_plan_day",
		Arguments: map[string]any{
			"npc":    "Penny",
			"day":    1,
			"season": "spring",
			"entries": []any{
				map[string]any{
					"game_hour": 6, "game_minute": 30,
					"workflow_id": "farm_extension",
				},
				map[string]any{
					"game_hour": 12,
					"workflow": map[string]any{
						"id": "lunch_break",
						"steps": []any{
							map[string]any{"kind": "tool", "name": "npc_idle_activity"},
						},
					},
				},
				map[string]any{
					"game_hour":   18,
					"workflow_id": "social_interact",
					"reason":      "傍晚社交",
				},
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
	if out.Accepted != 3 {
		t.Fatalf("expected 3 accepted, got %d", out.Accepted)
	}

	got := sched.GetSchedule("Penny")
	if got == nil || len(got.Entries) != 3 {
		t.Fatalf("expected 3 entries, got %+v", got)
	}
	if got.Entries[0].WorkflowID != "farm_extension" {
		t.Errorf("entry 0: WorkflowID = %q", got.Entries[0].WorkflowID)
	}
	if got.Entries[1].Workflow == nil || got.Entries[1].Workflow.ID != "lunch_break" {
		t.Errorf("entry 1: Workflow = %+v", got.Entries[1].Workflow)
	}
	if got.Entries[2].WorkflowID != "social_interact" {
		t.Errorf("entry 2: WorkflowID = %q, want social_interact", got.Entries[2].WorkflowID)
	}
}
func TestSchedule_TickReturnsWorkflowRef(t *testing.T) {
	sched := scheduler.New()

	sched.SetSchedule(scheduler.DaySchedule{
		NPC:    "Abigail",
		Day:    1,
		Season: "spring",
		Entries: []scheduler.Entry{
			{
				GameHour:   7,
				WorkflowID: "farm_extension",
				Args:       map[string]any{"inspect_radius": float64(15)},
			},
		},
	})

	fired := sched.Tick(7 * 60) // 07:00 = 420 minutes
	if len(fired) != 1 {
		t.Fatalf("expected 1 fired, got %d", len(fired))
	}
	f := fired[0]
	if f.WorkflowID != "farm_extension" {
		t.Errorf("WorkflowID = %q, want farm_extension", f.WorkflowID)
	}
	if f.Args == nil || f.Args["inspect_radius"] != float64(15) {
		t.Errorf("Args = %v", f.Args)
	}
	if f.Action != "" {
		t.Errorf("Action = %q, want empty", f.Action)
	}
}
