package tools

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/OmniStormX/SmartNPC/pkg/workflow"
)

// newWorkflowClientServer creates an in-memory MCP client/server with the
// workflow tools wired. The registry is initialised from embedded builtins.
// When debug is true, workflow_run_inline is also registered.
func newWorkflowClientServer(t *testing.T, debug bool) (*mcp.ClientSession, context.Context, func()) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)

	reg := workflow.NewRegistry()
	if err := reg.Init(""); err != nil {
		cancel()
		t.Fatalf("registry init: %v", err)
	}

	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "test"}, nil)
	RegisterWorkflow(server, reg, debug)

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
	return cs, ctx, cleanup
}

func TestWorkflowList_EndToEnd(t *testing.T) {
	cs, ctx, cleanup := newWorkflowClientServer(t, false)
	defer cleanup()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "workflow_list",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError: %v", res.Content)
	}

	b, _ := json.Marshal(res.StructuredContent)
	var out WorkflowListOutput
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !out.OK {
		t.Error("expected ok=true")
	}

	wantIDs := map[string]bool{
		"farm_extension":       false,
		"farm_care":            false,
		"farm_cleanup":         false,
		"resource_gather":      false,
		"social_interact":      false,
		"farm_evening_close":   false,
		"mine_crawl":           false,
		"social_round_robin":   false,
		"weather_bookkeeping":  false,
	}
	for _, w := range out.Workflows {
		if _, ok := wantIDs[w.ID]; ok {
			wantIDs[w.ID] = true
		}
		if w.Description == "" {
			t.Errorf("workflow %s has empty description", w.ID)
		}
	}
	for id, found := range wantIDs {
		if !found {
			t.Errorf("missing workflow %q in list", id)
		}
	}
}

func TestWorkflowGet_EndToEnd(t *testing.T) {
	cs, ctx, cleanup := newWorkflowClientServer(t, false)
	defer cleanup()

	// Get a known workflow.
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "workflow_get",
		Arguments: map[string]any{
			"id": "farm_cleanup",
		},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError: %v", res.Content)
	}

	b, _ := json.Marshal(res.StructuredContent)
	var out WorkflowGetOutput
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !out.OK {
		t.Errorf("expected ok=true, got message=%q", out.Message)
	}
	if out.Definition == nil {
		t.Error("definition is nil")
	} else {
		// Definition is serialised as map[string]any via JSON round-trip.
		defMap, ok := out.Definition.(map[string]any)
		if !ok {
			t.Fatalf("definition is %T, want map[string]any", out.Definition)
		}
		if defMap["id"] != "farm_cleanup" {
			t.Errorf("definition id = %v, want farm_cleanup", defMap["id"])
		}
		steps, _ := defMap["steps"].([]any)
		if len(steps) != 2 {
			t.Errorf("expected 2 steps in farm_cleanup, got %d", len(steps))
		}
	}

	// Unknown ID returns ok=false, not error.
	res2, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "workflow_get",
		Arguments: map[string]any{
			"id": "nonexistent",
		},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	b2, _ := json.Marshal(res2.StructuredContent)
	var out2 WorkflowGetOutput
	_ = json.Unmarshal(b2, &out2)
	if out2.OK {
		t.Error("expected ok=false for unknown workflow")
	}
}

func TestWorkflowRunInline_ValidatesAndRuns(t *testing.T) {
	cs, ctx, cleanup := newWorkflowClientServer(t, true) // debug=true
	defer cleanup()

	// Run a valid inline workflow. Pass as map to avoid Go struct ,inline
	// JSON marshaling edge cases across the MCP transport.
	inlineMap := map[string]any{
		"id": "test_inline",
		"steps": []any{
			map[string]any{
				"kind":    "tool",
				"name":    "npc_inspect_object",
				"save_as": "obs",
			},
			map[string]any{
				"kind": "tool",
				"name": "npc_show_text_bubble",
			},
		},
	}

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "workflow_run_inline",
		Arguments: map[string]any{
			"npc":    "TestNPC",
			"inline": inlineMap,
		},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError: %v", res.Content)
	}

	b, _ := json.Marshal(res.StructuredContent)
	var out WorkflowRunInlineOutput
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !out.OK {
		t.Errorf("expected ok=true, got %+v", out)
	}
	// noopRunner always returns success — both tools should "fire".
	if out.ToolCalls != 2 {
		t.Errorf("expected 2 tool calls (noop runner), got %d", out.ToolCalls)
	}
	if out.StepCount < 2 {
		t.Errorf("step_count = %d, want >= 2", out.StepCount)
	}
}

func TestWorkflowRunInline_RunsBuiltinByID(t *testing.T) {
	cs, ctx, cleanup := newWorkflowClientServer(t, true) // debug=true
	defer cleanup()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "workflow_run_inline",
		Arguments: map[string]any{
			"npc":         "TestNPC",
			"workflow_id": "farm_cleanup",
		},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError: %v", res.Content)
	}

	b, _ := json.Marshal(res.StructuredContent)
	var out WorkflowRunInlineOutput
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !out.OK {
		t.Errorf("expected ok=true, got %+v", out)
	}
	if out.ToolCalls != 2 {
		t.Errorf("farm_cleanup has 2 tool steps, got %d tool calls", out.ToolCalls)
	}
}

func TestWorkflowRunInline_RejectsInvalid(t *testing.T) {
	cs, ctx, cleanup := newWorkflowClientServer(t, true)
	defer cleanup()

	// Inline def with missing tool name (kind=tool but no name).
	inlineMap := map[string]any{
		"id": "bad",
		"steps": []any{
			map[string]any{"kind": "tool"},
		},
	}

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "workflow_run_inline",
		Arguments: map[string]any{
			"npc":    "TestNPC",
			"inline": inlineMap,
		},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if !res.IsError {
		t.Error("expected IsError for invalid inline definition")
	}
}

func TestWorkflowRunInline_NotRegisteredWithoutDebug(t *testing.T) {
	cs, ctx, cleanup := newWorkflowClientServer(t, false) // debug=false
	defer cleanup()

	// workflow_run_inline should not be in the tool list.
	listed, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, tool := range listed.Tools {
		if tool.Name == "workflow_run_inline" {
			t.Error("workflow_run_inline should NOT be registered when debug=false")
		}
	}
	// But list and get should be there.
	found := map[string]bool{}
	for _, tool := range listed.Tools {
		found[tool.Name] = true
	}
	for _, want := range []string{"workflow_list", "workflow_get", "workflow_choice_reply", "workflow_run_history"} {
		if !found[want] {
			t.Errorf("tool %q missing when debug=false", want)
		}
	}
}


// ── P5: workflow run history ─────────────────────────────────────────────

func TestWorkflowRunHistory_EndToEnd(t *testing.T) {
	cs, ctx, cleanup := newWorkflowClientServer(t, false)
	defer cleanup()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "workflow_run_history",
		Arguments: map[string]any{
			"npc":    "Abigail",
			"season": "spring",
			"day":    15,
			"year":   1,
		},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError: %v", res.Content)
	}

	b, _ := json.Marshal(res.StructuredContent)
	var out WorkflowRunHistoryOutput
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !out.OK {
		t.Errorf("expected ok=true, got message=%q", out.Message)
	}
}

func TestWorkflowRunHistory_RequiresNPC(t *testing.T) {
	cs, ctx, cleanup := newWorkflowClientServer(t, false)
	defer cleanup()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "workflow_run_history",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if !res.IsError {
		t.Error("expected IsError when npc is missing")
	}
}

func TestLogWorkflowRun_RoundTrip(t *testing.T) {
	npcs := "TestNPC"
	season := "summer"
	day := 10
	year := 1

	res := &workflow.RunResult{
		WorkflowID:    "test_workflow",
		NPC:           npcs,
		StepCount:     5,
		ToolCalls:     3,
		NothingToDoCt: 1,
		Stopped:       false,
	}
	LogWorkflowRun(npcs, season, day, year, res, nil, 1234*time.Millisecond, map[string]any{"radius": 10})

	records := ReadWorkflowRunHistory(npcs, season, day, year, 5)
	if len(records) < 1 {
		t.Fatal("expected at least 1 record after write")
	}
	r := records[0]
	if r.WorkflowID != "test_workflow" {
		t.Errorf("WorkflowID = %q", r.WorkflowID)
	}
	if r.StepCount != 5 {
		t.Errorf("StepCount = %d", r.StepCount)
	}
	if r.ToolCalls != 3 {
		t.Errorf("ToolCalls = %d", r.ToolCalls)
	}
	if r.NothingToDo != 1 {
		t.Errorf("NothingToDo = %d", r.NothingToDo)
	}
	if r.DurationMS < 1000 {
		t.Errorf("DurationMS = %d, want >= 1000", r.DurationMS)
	}
}

func TestLogWorkflowRun_MultipleWrites(t *testing.T) {
	res := &workflow.RunResult{
		WorkflowID: "farm_morning_round",
		NPC:        "Penny",
		StepCount:  7,
		ToolCalls:  5,
	}
	// Write two records, read back — should get 2+.
	LogWorkflowRun("Penny", "spring", 1, 1, res, nil, 500*time.Millisecond, nil)
	LogWorkflowRun("Penny", "spring", 1, 1, res, nil, 600*time.Millisecond, nil)

	records := ReadWorkflowRunHistory("Penny", "spring", 1, 1, 10)
	if len(records) < 2 {
		t.Errorf("expected at least 2 records, got %d", len(records))
	}
}
