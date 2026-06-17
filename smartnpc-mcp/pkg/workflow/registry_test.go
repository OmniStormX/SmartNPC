package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadAllBuiltin(t *testing.T) {
	r := NewRegistry()
	if err := r.Init(""); err != nil {
		t.Fatalf("Init: %v", err)
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

	for _, d := range r.List() {
		if _, ok := wantIDs[d.ID]; ok {
			wantIDs[d.ID] = true
		} else {
			t.Errorf("unexpected built-in workflow: %s", d.ID)
		}
		if d.ID == "" {
			t.Error("workflow with empty ID")
		}
		if d.Description == "" {
			t.Errorf("workflow %s has empty description", d.ID)
		}
		if len(d.Steps) == 0 {
			t.Errorf("workflow %s has no steps", d.ID)
		}
	}

	for id, found := range wantIDs {
		if !found {
			t.Errorf("missing built-in workflow: %s", id)
		}
	}

	// Also test Get.
	for id := range wantIDs {
		d := r.Get(id)
		if d == nil || d.ID != id {
			t.Errorf("Get(%s) returned nil or mismatched ID", id)
		}
	}
	if r.Get("nonexistent") != nil {
		t.Error("Get(nonexistent) should return nil")
	}
}

func TestValidate_RejectsBadSteps(t *testing.T) {
	cases := []struct {
		name string
		def  *Definition
		err  string
	}{
		{
			name: "nil definition",
			def:  nil,
			err:  "nil definition",
		},
		{
			name: "missing id",
			def:  &Definition{Steps: []Step{{Kind: StepKindStop, Stop: &StopStep{}}}},
			err:  "missing id",
		},
		{
			name: "empty steps",
			def:  &Definition{ID: "x"},
			err:  "steps must not be empty",
		},
		{
			name: "unknown step kind",
			def: &Definition{ID: "x", Steps: []Step{
				{Kind: StepKind("bogus")},
			}},
			err: "unknown step kind",
		},
		{
			name: "tool step missing name",
			def: &Definition{ID: "x", Steps: []Step{
				{Kind: StepKindTool},
			}},
			err: "tool step missing name",
		},
		{
			name: "branch step missing when",
			def: &Definition{ID: "x", Steps: []Step{
				{Kind: StepKindBranch, Branch: &BranchStep{Then: []Step{{Kind: StepKindStop, Stop: &StopStep{}}}}},
			}},
			err: "branch step missing when",
		},
		{
			name: "branch step empty body",
			def: &Definition{ID: "x", Steps: []Step{
				{Kind: StepKindBranch, Branch: &BranchStep{When: "true"}},
			}},
			err: "branch step has empty then and else",
		},
		{
			name: "random step no branches",
			def: &Definition{ID: "x", Steps: []Step{
				{Kind: StepKindRandom},
			}},
			err: "random step has no weighted branches",
		},
		{
			name: "random step negative weight",
			def: &Definition{ID: "x", Steps: []Step{
				{Kind: StepKindRandom, Random: &RandomStep{Weighted: []RandomBranch{
					{Weight: -1, Do: []Step{{Kind: StepKindStop, Stop: &StopStep{}}}},
				}}},
			}},
			err: "negative weight",
		},
		{
			name: "random step all zero weights",
			def: &Definition{ID: "x", Steps: []Step{
				{Kind: StepKindRandom, Random: &RandomStep{Weighted: []RandomBranch{
					{Weight: 0, Do: []Step{{Kind: StepKindStop, Stop: &StopStep{}}}},
				}}},
			}},
			err: "no branch with weight > 0",
		},
		{
			name: "foreach missing over",
			def: &Definition{ID: "x", Steps: []Step{
				{Kind: StepKindForEach, ForEach: &ForEachStep{As: "x", Do: []Step{{Kind: StepKindStop, Stop: &StopStep{}}}}},
			}},
			err: "foreach step missing over",
		},
		{
			name: "foreach missing as",
			def: &Definition{ID: "x", Steps: []Step{
				{Kind: StepKindForEach, ForEach: &ForEachStep{Over: "$list", Do: []Step{{Kind: StepKindStop, Stop: &StopStep{}}}}},
			}},
			err: "foreach step missing as",
		},
		{
			name: "foreach empty body",
			def: &Definition{ID: "x", Steps: []Step{
				{Kind: StepKindForEach, ForEach: &ForEachStep{Over: "$list", As: "x"}},
			}},
			err: "foreach step body is empty",
		},
		{
			name: "skill_call missing skill",
			def: &Definition{ID: "x", Steps: []Step{
				{Kind: StepKindSkillCall},
			}},
			err: "skill_call step missing skill",
		},
		{
			name: "llm_choice missing save_as",
			def: &Definition{ID: "x", Steps: []Step{
				{Kind: StepKindLLMChoice, LLMChoice: &LLMChoiceStep{Prompt: "pick", Options: []string{"a"}}},
			}},
			err: "llm_choice step missing save_as",
		},
		{
			name: "llm_choice no options",
			def: &Definition{ID: "x", Steps: []Step{
				{Kind: StepKindLLMChoice, LLMChoice: &LLMChoiceStep{SaveAs: "x"}},
			}},
			err: "llm_choice step has no options",
		},
		{
			name: "duplicate save_as",
			def: &Definition{ID: "x", Steps: []Step{
				{Kind: StepKindTool, Tool: &ToolStep{Name: "a", SaveAs: "z"}},
				{Kind: StepKindTool, Tool: &ToolStep{Name: "b", SaveAs: "z"}},
			}},
			err: "duplicate save_as",
		},
		{
			name: "bad on_nothing_to_do",
			def: &Definition{ID: "x", Steps: []Step{
				{Kind: StepKindTool, Tool: &ToolStep{Name: "a", OnNothingToDo: "panic"}},
			}},
			err: "unknown on_nothing_to_do",
		},
		{
			name: "wait unknown condition",
			def: &Definition{ID: "x", Steps: []Step{
				{Kind: StepKindWait, Wait: &WaitStep{Condition: "win"}},
			}},
			err: "unknown condition",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := Validate(c.def)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", c.err)
			}
			if !strings.Contains(err.Error(), c.err) {
				t.Errorf("error %q does not contain %q", err.Error(), c.err)
			}
		})
	}
}

func TestRegistry_DuplicateIDError(t *testing.T) {
	// Load builtins once, then try to store a def with same ID manually.
	r := NewRegistry()
	if err := r.Init(""); err != nil {
		t.Fatalf("Init: %v", err)
	}
	// store() is unexported — verify that Init with a dir containing a
	// duplicate-ID file overlays (doesn't error). We test overlay in
	// TestRegistry_OverrideDir below. For the error path, test LoadDef
	// of a YAML whose ID matches a built-in — Init does the store check.
	// We can't call r.store directly, but we can verify Get works:
	if r.Get("farm_extension") == nil {
		t.Fatal("farm_extension not loaded")
	}
	// A second Init() call with the same embedded defs would error on
	// duplicates; that's fine — Init is called once at startup.
	// Verify the duplicate is already protected: create a fresh registry
	// and feed it two same-ID defs.
	r2 := NewRegistry()
	// Use store via loadDir — write two same-ID files to a temp dir.
	dir := t.TempDir()
	f1 := filepath.Join(dir, "a.yaml")
	f2 := filepath.Join(dir, "b.yaml")
	yaml1 := "id: dup\nsteps:\n  - kind: stop\n    reason: ok\n"
	yaml2 := "id: dup\nsteps:\n  - kind: stop\n    reason: also ok\n"
	if err := os.WriteFile(f1, []byte(yaml1), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f2, []byte(yaml2), 0644); err != nil {
		t.Fatal(err)
	}
	// First load succeeds.
	if err := r2.Init(dir); err == nil {
		t.Error("expected duplicate ID error from Init with two same-ID files")
	} else if !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("expected duplicate error, got: %v", err)
	}
}

func TestRegistry_OverrideDir(t *testing.T) {
	// Create a fresh registry, load builtins, then overlay one via extraDir.
	r := NewRegistry()
	if err := r.Init(""); err != nil {
		t.Fatalf("Init: %v", err)
	}

	original := r.Get("farm_cleanup")
	if original == nil {
		t.Fatal("farm_cleanup not loaded")
	}

	// Write an override to a temp dir — same ID, different description.
	dir := t.TempDir()
	override := `id: farm_cleanup
description: 清理杂草和石头（override 版）
version: "2"
steps:
  - kind: tool
    name: npc_clear_debris
    args: {}
`
	if err := os.WriteFile(filepath.Join(dir, "cleanup_override.yaml"), []byte(override), 0644); err != nil {
		t.Fatal(err)
	}

	// Re-init with extraDir — should overlay.
	r2 := NewRegistry()
	if err := r2.Init(dir); err != nil {
		t.Fatalf("Init with extraDir: %v", err)
	}

	overridden := r2.Get("farm_cleanup")
	if overridden == nil {
		t.Fatal("farm_cleanup lost after overlay")
	}
	if overridden.Description != "清理杂草和石头（override 版）" {
		t.Errorf("description = %q, want override value", overridden.Description)
	}
	if overridden.Version != "2" {
		t.Errorf("version = %q, want 2", overridden.Version)
	}
	if len(overridden.Steps) != 1 || overridden.Steps[0].Tool == nil || overridden.Steps[0].Tool.Name != "npc_pet_animal" {
		t.Error("steps mismatch in overridden definition")
	}

	// Other builtins still load fine.
	if r2.Get("farm_extension") == nil {
		t.Error("other builtins should still be available after overlay")
	}
}

func TestLoadDef_RoundTrip(t *testing.T) {
	// Parse YAML and verify struct fields are populated correctly.
	yaml := `id: test_workflow
description: A test workflow
version: "1"
inputs:
  - name: radius
    description: scan radius
    default: 10
steps:
  - kind: tool
    name: npc_inspect_object
    args:
      what: farm_actions
      radius: "$radius"
    save_as: obs
  - kind: branch
    when: "$obs.water.count > 0"
    then:
      - kind: tool
        name: npc_water_crops
        args:
          x1: "$obs.water.bbox.x1"
    else:
      - kind: tool
        name: npc_show_text_bubble
        args:
          text: "nothing to water"
`

	def, err := LoadDef([]byte(yaml))
	if err != nil {
		t.Fatalf("LoadDef: %v", err)
	}
	if def.ID != "test_workflow" {
		t.Errorf("ID = %q", def.ID)
	}
	if def.Description != "A test workflow" {
		t.Errorf("description = %q", def.Description)
	}
	if len(def.Inputs) != 1 || def.Inputs[0].Name != "radius" {
		t.Errorf("inputs: %+v", def.Inputs)
	}
	if len(def.Steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(def.Steps))
	}

	// Step 0: tool
	s0 := def.Steps[0]
	if s0.Kind != StepKindTool || s0.Tool.Name != "npc_inspect_object" {
		t.Errorf("step 0: %+v", s0)
	}
	if s0.Tool.SaveAs != "obs" {
		t.Errorf("step 0 save_as = %q, want obs", s0.Tool.SaveAs)
	}
	if s0.Tool.Args["what"] != "farm_actions" {
		t.Errorf("step 0 arg what = %v", s0.Tool.Args["what"])
	}
	// $radius should survive as literal string (engine resolves at runtime).
	if s0.Tool.Args["radius"] != "$radius" {
		t.Errorf("step 0 arg radius = %v, want $radius literal", s0.Tool.Args["radius"])
	}

	// Step 1: branch
	s1 := def.Steps[1]
	if s1.Kind != StepKindBranch || s1.Branch.When != "$obs.water.count > 0" {
		t.Errorf("step 1: kind=%s when=%q", s1.Kind, s1.Branch.When)
	}
	if len(s1.Branch.Then) != 1 || s1.Branch.Then[0].Tool == nil || s1.Branch.Then[0].Tool.Name != "npc_water_crops" {
		t.Errorf("step 1 then: %+v", s1.Branch.Then)
	}
	if len(s1.Branch.Else) != 1 || s1.Branch.Else[0].Tool == nil || s1.Branch.Else[0].Tool.Name != "npc_show_text_bubble" {
		t.Errorf("step 1 else: %+v", s1.Branch.Else)
	}
}

func TestLoadDef_MinimalValid(t *testing.T) {
	yaml := `id: min
steps:
  - kind: stop
    reason: done
`
	def, err := LoadDef([]byte(yaml))
	if err != nil {
		t.Fatalf("LoadDef: %v", err)
	}
	if len(def.Steps) != 1 || def.Steps[0].Stop == nil || def.Steps[0].Stop.Reason != "done" {
		t.Errorf("minimal def mismatch: %+v", def)
	}
}

func TestLint_AllBuiltinsPass(t *testing.T) {
	r := NewRegistry()
	if err := r.Init(""); err != nil {
		t.Fatalf("Init: %v", err)
	}
	for _, def := range r.List() {
		if err := Validate(def); err != nil {
			t.Errorf("%s: Validate failed: %v", def.ID, err)
		}
		// Check every tool step references a known tool.
		// (Basic sanity — the full lint runs in cmd/workflow-lint.)
		for i, s := range def.Steps {
			if s.Kind == StepKindTool && s.Tool != nil && s.Tool.Name == "" {
				t.Errorf("%s step %d: tool step with empty name", def.ID, i)
			}
			if s.Kind == StepKindSkillCall && s.SkillCall != nil && s.SkillCall.Skill == "" {
				t.Errorf("%s step %d: skill_call with empty skill", def.ID, i)
			}
			if s.Kind == StepKindBranch && s.Branch != nil && s.Branch.When == "" {
				t.Errorf("%s step %d: branch with empty when", def.ID, i)
			}
		}
	}
}
