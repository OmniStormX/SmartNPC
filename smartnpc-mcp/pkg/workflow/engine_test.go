package workflow

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// ── stub runner ─────────────────────────────────────────────────────────
//
// stubRunner records every interaction and lets each test wire in
// canned tool outputs. We deliberately avoid using `gomock` or similar
// — the surface is tiny and string-keyed, hand-written stubs are
// shorter and more readable in the failure path.

type stubRunner struct {
	toolOut    map[string]map[string]any // tool name → output payload
	toolErr    map[string]error
	toolCalls  []toolCallRecord
	skillCalls []string
	llmChoice  string
	llmErr     error
	waitOK     bool
	waitErr    error
	calls      atomic.Int32
}

type toolCallRecord struct {
	Name string
	Args map[string]any
}

func (s *stubRunner) CallTool(_ context.Context, _, name string, args map[string]any) (map[string]any, error) {
	s.calls.Add(1)
	s.toolCalls = append(s.toolCalls, toolCallRecord{Name: name, Args: args})
	if err, ok := s.toolErr[name]; ok && err != nil {
		return nil, err
	}
	if out, ok := s.toolOut[name]; ok {
		// Return a copy so the engine's save_as binding can't mutate
		// the test fixture — paranoia, since engine treats it as
		// immutable, but cheap.
		clone := make(map[string]any, len(out))
		for k, v := range out {
			clone[k] = v
		}
		return clone, nil
	}
	return map[string]any{"ok": true}, nil
}

func (s *stubRunner) CallSkill(_ context.Context, _, skill string, _ map[string]any) error {
	s.skillCalls = append(s.skillCalls, skill)
	return nil
}

func (s *stubRunner) LLMChoice(_ context.Context, _, _ string, options []string) (string, error) {
	if s.llmErr != nil {
		return "", s.llmErr
	}
	if s.llmChoice != "" {
		return s.llmChoice, nil
	}
	return options[0], nil
}

func (s *stubRunner) WaitIdle(_ context.Context, _ string, _ time.Duration) (bool, error) {
	return s.waitOK, s.waitErr
}

// ── scope / expressions ─────────────────────────────────────────────────

func TestScope_GetSetChain(t *testing.T) {
	parent := NewScope()
	if err := parent.Set("a", 1); err != nil {
		t.Fatalf("set a: %v", err)
	}
	child := parent.Child()
	if err := child.Set("b", 2); err != nil {
		t.Fatalf("set b: %v", err)
	}
	v, ok := child.Get("a")
	if !ok || v.(int) != 1 {
		t.Errorf("child should see parent's a; got %v ok=%v", v, ok)
	}
	if _, ok := parent.Get("b"); ok {
		t.Errorf("parent should NOT see child's b")
	}
	// Re-bind in same frame: error.
	if err := parent.Set("a", 99); err == nil {
		t.Errorf("expected error on duplicate Set in same scope")
	}
	// Re-bind in child frame is allowed (shadowing parent).
	if err := child.Set("a", 99); err != nil {
		t.Errorf("shadow in child should be allowed: %v", err)
	}
}

func TestScope_ResolvePath(t *testing.T) {
	s := NewScope()
	_ = s.Set("obs", map[string]any{
		"water": map[string]any{"count": float64(5)},
	})
	cases := []struct {
		path string
		want any
		ok   bool
	}{
		{"$obs.water.count", float64(5), true},
		{"obs.water.count", float64(5), true},
		{"$obs.water.missing", nil, false},
		{"$nope", nil, false},
		{"", nil, false},
	}
	for _, c := range cases {
		got, ok := s.Resolve(c.path)
		if ok != c.ok || got != c.want {
			t.Errorf("Resolve(%q) = %v, %v; want %v, %v", c.path, got, ok, c.want, c.ok)
		}
	}
}

func TestEvalBool_AllForms(t *testing.T) {
	scope := NewScope()
	_ = scope.Set("obs", map[string]any{
		"water":   map[string]any{"count": float64(3)},
		"harvest": map[string]any{"count": float64(0)},
	})
	_ = scope.Set("season", "fall")
	_ = scope.Set("flag", true)

	cases := []struct {
		expr string
		want bool
	}{
		{"$obs.water.count > 0", true},
		{"$obs.harvest.count > 0", false},
		{"$obs.water.count >= 3", true},
		{"$obs.water.count < 3", false},
		{"$season == \"fall\"", true},
		{"$season == 'fall'", true},
		{"$season != \"summer\"", true},
		{"$flag", true},
		{"!$flag", false},
		{"!$missing", true},
		{"$obs.water.count > 0 && $season == \"fall\"", true},
		{"$obs.water.count > 5 || $season == \"fall\"", true},
		{"$obs.water.count > 5 && $season == \"fall\"", false},
		{"($obs.water.count > 0)", true},
		// Mixed-type / missing variable: defensive false.
		{"$missing > 0", false},
	}
	for _, c := range cases {
		got, err := EvalBool(c.expr, scope)
		if err != nil {
			t.Errorf("EvalBool(%q) error: %v", c.expr, err)
			continue
		}
		if got != c.want {
			t.Errorf("EvalBool(%q) = %v; want %v", c.expr, got, c.want)
		}
	}
}

func TestEval_LiteralsAndArithSubstitution(t *testing.T) {
	scope := NewScope()
	_ = scope.Set("n", float64(7))
	tests := []struct {
		expr string
		want any
	}{
		{"42", float64(42)},
		{"-3.5", -3.5},
		{`"hello"`, "hello"},
		{"true", true},
		{"false", false},
		{"nil", nil},
		{"$n", float64(7)},
	}
	for _, tc := range tests {
		got, err := Eval(tc.expr, scope)
		if err != nil {
			t.Errorf("Eval(%q): %v", tc.expr, err)
			continue
		}
		if got != tc.want {
			t.Errorf("Eval(%q) = %v; want %v", tc.expr, got, tc.want)
		}
	}
}

// ── engine: run integration ─────────────────────────────────────────────

func TestRun_LinearTools(t *testing.T) {
	def := &Definition{
		ID: "linear",
		Steps: []Step{
			{Kind: StepKindTool, Tool: &ToolStep{Name: "npc_inspect_object", SaveAs: "obs"}},
			{Kind: StepKindTool, Tool: &ToolStep{Name: "npc_water_crops"}},
		},
	}
	runner := &stubRunner{
		toolOut: map[string]map[string]any{
			"npc_inspect_object": {"ok": true, "water": map[string]any{"count": float64(2)}},
			"npc_water_crops":    {"ok": true, "watered": float64(2)},
		},
	}
	res, err := Run(context.Background(), runner, "Abigail", def, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.ToolCalls != 2 {
		t.Errorf("expected 2 tool calls; got %d", res.ToolCalls)
	}
	if res.Stopped {
		t.Errorf("expected not stopped")
	}
}

func TestRun_BranchSkipsTrueElse(t *testing.T) {
	def := &Definition{
		ID: "branch",
		Steps: []Step{
			{Kind: StepKindTool, Tool: &ToolStep{Name: "npc_inspect_object", SaveAs: "obs"}},
			{Kind: StepKindBranch, Branch: &BranchStep{
				When: "$obs.water.count > 0",
				Then: []Step{{Kind: StepKindTool, Tool: &ToolStep{Name: "npc_water_crops"}}},
				Else: []Step{{Kind: StepKindTool, Tool: &ToolStep{Name: "npc_show_text_bubble"}}},
			}},
		},
	}
	// Case A: water.count > 0 → THEN runs.
	runner := &stubRunner{
		toolOut: map[string]map[string]any{
			"npc_inspect_object": {"water": map[string]any{"count": float64(3)}},
		},
	}
	res, err := Run(context.Background(), runner, "Abigail", def, nil)
	if err != nil {
		t.Fatalf("Run A: %v", err)
	}
	gotNames := toolNames(runner.toolCalls)
	wantNames := []string{"npc_inspect_object", "npc_water_crops"}
	if !equalStrSlice(gotNames, wantNames) {
		t.Errorf("case A tool sequence = %v; want %v", gotNames, wantNames)
	}
	if res.ToolCalls != 2 {
		t.Errorf("case A tools=%d; want 2", res.ToolCalls)
	}

	// Case B: water.count == 0 → ELSE runs.
	runnerB := &stubRunner{
		toolOut: map[string]map[string]any{
			"npc_inspect_object": {"water": map[string]any{"count": float64(0)}},
		},
	}
	if _, err := Run(context.Background(), runnerB, "Abigail", def, nil); err != nil {
		t.Fatalf("Run B: %v", err)
	}
	gotB := toolNames(runnerB.toolCalls)
	wantB := []string{"npc_inspect_object", "npc_show_text_bubble"}
	if !equalStrSlice(gotB, wantB) {
		t.Errorf("case B tool sequence = %v; want %v", gotB, wantB)
	}
}

func TestRun_NothingToDoSkipDefault(t *testing.T) {
	def := &Definition{
		ID: "noop",
		Steps: []Step{
			{Kind: StepKindTool, Tool: &ToolStep{Name: "npc_water_crops"}},
			{Kind: StepKindTool, Tool: &ToolStep{Name: "npc_clear_debris"}},
		},
	}
	runner := &stubRunner{
		toolOut: map[string]map[string]any{
			"npc_water_crops": {"ok": true, "nothing_to_do": true, "reason": "no unwatered crops"},
		},
	}
	res, err := Run(context.Background(), runner, "Abigail", def, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.NothingToDoCt != 1 {
		t.Errorf("nothing_to_do count = %d; want 1", res.NothingToDoCt)
	}
	if res.ToolCalls != 2 {
		t.Errorf("expected both tools to fire (skip policy); got %d", res.ToolCalls)
	}
	if res.Stopped {
		t.Errorf("default policy should not stop")
	}
}

func TestRun_NothingToDoStopPolicy(t *testing.T) {
	def := &Definition{
		ID: "noop-stop",
		Steps: []Step{
			{Kind: StepKindTool, Tool: &ToolStep{Name: "npc_water_crops", OnNothingToDo: "stop"}},
			{Kind: StepKindTool, Tool: &ToolStep{Name: "npc_clear_debris"}},
		},
	}
	runner := &stubRunner{
		toolOut: map[string]map[string]any{
			"npc_water_crops": {"nothing_to_do": true, "reason": "all watered"},
		},
	}
	res, err := Run(context.Background(), runner, "Abigail", def, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Stopped || res.StopReason != "all watered" {
		t.Errorf("expected stop with reason 'all watered'; got %v / %q", res.Stopped, res.StopReason)
	}
	if res.ToolCalls != 1 {
		t.Errorf("second tool should NOT have fired; got %d tools", res.ToolCalls)
	}
}

func TestRun_ForEach(t *testing.T) {
	def := &Definition{
		ID: "loop",
		Steps: []Step{
			{Kind: StepKindTool, Tool: &ToolStep{Name: "inspect", SaveAs: "obs"}},
			{Kind: StepKindForEach, ForEach: &ForEachStep{
				Over: "$obs.list",
				As:   "x",
				Do:   []Step{{Kind: StepKindTool, Tool: &ToolStep{Name: "act"}}},
			}},
		},
	}
	runner := &stubRunner{
		toolOut: map[string]map[string]any{
			"inspect": {"list": []any{
				map[string]any{"id": "a"},
				map[string]any{"id": "b"},
				map[string]any{"id": "c"},
			}},
		},
	}
	res, err := Run(context.Background(), runner, "Abigail", def, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// 1 inspect + 3 acts.
	if res.ToolCalls != 4 {
		t.Errorf("ToolCalls = %d; want 4", res.ToolCalls)
	}
}

func TestRun_RandomDeterministicWithSingleBranch(t *testing.T) {
	// Only one branch with non-zero weight → deterministically picked.
	def := &Definition{
		ID: "random",
		Steps: []Step{
			{Kind: StepKindRandom, Random: &RandomStep{
				Weighted: []RandomBranch{
					{Weight: 0, Do: []Step{{Kind: StepKindTool, Tool: &ToolStep{Name: "skip_me"}}}},
					{Weight: 1, Do: []Step{{Kind: StepKindTool, Tool: &ToolStep{Name: "always_me"}}}},
				},
			}},
		},
	}
	runner := &stubRunner{}
	if _, err := Run(context.Background(), runner, "Abigail", def, nil); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(runner.toolCalls) != 1 || runner.toolCalls[0].Name != "always_me" {
		t.Errorf("expected single call to 'always_me'; got %v", toolNames(runner.toolCalls))
	}
}

func TestRun_StopStep(t *testing.T) {
	def := &Definition{
		ID: "stop",
		Steps: []Step{
			{Kind: StepKindTool, Tool: &ToolStep{Name: "first"}},
			{Kind: StepKindStop, Stop: &StopStep{Reason: "not today"}},
			{Kind: StepKindTool, Tool: &ToolStep{Name: "never"}},
		},
	}
	runner := &stubRunner{}
	res, err := Run(context.Background(), runner, "X", def, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Stopped || res.StopReason != "not today" {
		t.Errorf("expected stop with reason; got %+v", res)
	}
	if len(runner.toolCalls) != 1 || runner.toolCalls[0].Name != "first" {
		t.Errorf("only 'first' should have fired; got %v", toolNames(runner.toolCalls))
	}
}

func TestRun_ToolErrorAborts(t *testing.T) {
	def := &Definition{
		ID: "boom",
		Steps: []Step{
			{Kind: StepKindTool, Tool: &ToolStep{Name: "fail"}},
			{Kind: StepKindTool, Tool: &ToolStep{Name: "skip"}},
		},
	}
	runner := &stubRunner{
		toolErr: map[string]error{"fail": errors.New("ws timeout")},
	}
	_, err := Run(context.Background(), runner, "X", def, nil)
	if err == nil || !strings.Contains(err.Error(), "ws timeout") {
		t.Errorf("expected ws timeout error; got %v", err)
	}
	if len(runner.toolCalls) != 1 {
		t.Errorf("subsequent step should NOT have fired; got %d calls", len(runner.toolCalls))
	}
}

func TestRun_ArgResolution(t *testing.T) {
	def := &Definition{
		ID: "args",
		Steps: []Step{
			{Kind: StepKindTool, Tool: &ToolStep{Name: "inspect", SaveAs: "obs"}},
			{Kind: StepKindTool, Tool: &ToolStep{
				Name: "act",
				Args: map[string]any{
					"x1":  "$obs.water.bbox.x1",
					"y1":  "$obs.water.bbox.y1",
					"raw": "literal-value",
					"missing_ref": "$nonexistent.path",
					"nested": map[string]any{
						"deep": "$obs.water.bbox.x1",
					},
					"list": []any{"$obs.water.bbox.x1", "literal"},
				},
			}},
		},
	}
	runner := &stubRunner{
		toolOut: map[string]map[string]any{
			"inspect": {"water": map[string]any{
				"bbox": map[string]any{"x1": float64(60), "y1": float64(14)},
			}},
		},
	}
	if _, err := Run(context.Background(), runner, "X", def, nil); err != nil {
		t.Fatalf("Run: %v", err)
	}
	args := runner.toolCalls[1].Args
	if args["x1"].(float64) != 60 {
		t.Errorf("x1 = %v; want 60", args["x1"])
	}
	if args["y1"].(float64) != 14 {
		t.Errorf("y1 = %v; want 14", args["y1"])
	}
	if args["raw"].(string) != "literal-value" {
		t.Errorf("raw = %v; want literal-value", args["raw"])
	}
	if args["missing_ref"] != nil {
		t.Errorf("missing ref should resolve to nil; got %v", args["missing_ref"])
	}
	nested, _ := args["nested"].(map[string]any)
	if nested["deep"].(float64) != 60 {
		t.Errorf("nested.deep = %v; want 60", nested["deep"])
	}
	list, _ := args["list"].([]any)
	if list[0].(float64) != 60 || list[1].(string) != "literal" {
		t.Errorf("list = %v; want [60 literal]", list)
	}
}

func TestRun_InputsAndDefaults(t *testing.T) {
	def := &Definition{
		ID: "inputs",
		Inputs: []InputSpec{
			{Name: "patch_w", Default: float64(10)},
			{Name: "seed_id", Default: "(O)472"},
		},
		Steps: []Step{
			{Kind: StepKindTool, Tool: &ToolStep{
				Name: "till",
				Args: map[string]any{
					"patch_w": "$patch_w",
					"seed_id": "$seed_id",
				},
			}},
		},
	}
	runner := &stubRunner{}
	// Caller overrides patch_w but not seed_id.
	_, err := Run(context.Background(), runner, "X", def, map[string]any{
		"patch_w": float64(8),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	args := runner.toolCalls[0].Args
	if args["patch_w"].(float64) != 8 {
		t.Errorf("patch_w override = %v; want 8", args["patch_w"])
	}
	if args["seed_id"].(string) != "(O)472" {
		t.Errorf("seed_id default = %v; want (O)472", args["seed_id"])
	}
}

// ── helpers ─────────────────────────────────────────────────────────────

func toolNames(calls []toolCallRecord) []string {
	out := make([]string, len(calls))
	for i, c := range calls {
		out[i] = c.Name
	}
	return out
}

func equalStrSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
