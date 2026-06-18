// Command workflow-lint validates every YAML workflow definition in
// pkg/workflow/builtin/ (and an optional overlay directory). It checks:
//
//   - Schema validity (ID, steps structure, save_as uniqueness, etc.)
//   - Tool name references against the known set of MCP tool names
//   - skill_call references against the known set of Hermes SKILL names
//   - Input variable references (no dangling $vars)
//
// Usage:
//
//	go run ./cmd/workflow-lint
//	go run ./cmd/workflow-lint --dir /path/to/overrides
//
// Exit 0 on clean; exit 1 with details on any finding.
package main

import (
	"flag"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/OmniStormX/SmartNPC/pkg/workflow"
)

// Known MCP tool names — the set of tools the mod actually handles.
// Keep in sync with adapters/stardew/tools/registry.go.
var knownTools = map[string]bool{
	// Chat
	"chat_say": true,
	// Mail
	"mail_send": true,
	// Game query
	"game_get_time":     true,
	"game_get_weather":  true,
	"game_get_season":   true,
	"game_get_location": true,
	// NPC perception
	"npc_get_nearby":      true,
	"npc_get_environment": true,
	// NPC movement
	"npc_move_to":        true,
	"npc_face_direction": true,
	"npc_get_position":   true,
	// NPC behavior
	"npc_summon":       true,
	"npc_emote":        true,
	"npc_give_item":    true,
	"npc_follow_start": true,
	"npc_follow_stop":  true,
	"npc_lead_to":      true,
	"npc_get_behavior": true,
	// NPC world actions
	"npc_wander":           true,
	"npc_water_crops":      true,
	"npc_plant_seeds":      true,
	"npc_fertilize":        true,
	"npc_harvest_crops":    true,
	"npc_till_soil":        true,
	"npc_clear_debris":     true,
	"npc_forage_collect":   true,
	"npc_break_resource":   true,
	"npc_deposit_items":    true,
	"npc_deliver_items":    true,
	"npc_inspect_object":   true,
	"npc_show_text_bubble": true,
	"npc_express_emotion":  true,
	"npc_dance_happy":      true,
	"npc_idle_activity":    true,
	"npc_pet_animal":       true,
	// NPC social
	"npc_approach_and_speak": true,
	// NPC inventory
	"npc_inventory_get":  true,
	"npc_inventory_put":  true,
	"npc_inventory_take": true,
	// Schedule
	"npc_plan_day":     true,
	"npc_get_schedule": true,
	// Workflow
	"workflow_list":         true,
	"workflow_get":          true,
	"workflow_run_inline":   true,
	"workflow_choice_reply": true,
	"workflow_run_history":  true,
	// Agent
	"agent_register_self": true,
}

// Known Hermes SKILL names (from hermes/profiles/_master/skills/).
var knownSkills = map[string]bool{
	"smartnpc-core":             true,
	"smartnpc-farm":             true,
	"smartnpc-farm-harvest":     true,
	"smartnpc-farm-maintenance": true,
	"smartnpc-farm-manager":     true,
	"smartnpc-farm-worker":      true,
	"smartnpc-gift":             true,
	"smartnpc-greeting":         true,
	"smartnpc-group-chat":       true,
	"smartnpc-inter-npc":        true,
	"smartnpc-memory":           true,
	"smartnpc-schedule":         true,
	"smartnpc-visit":            true,
}

// varRefRe matches $variable.path references.
var varRefRe = regexp.MustCompile(`\$[a-zA-Z_][a-zA-Z0-9_.]*`)

func main() {
	dir := flag.String("dir", "", "optional override directory (same as SMARTNPC_WORKFLOW_DIR)")
	flag.Parse()

	exitCode := 0

	// 1. Load all built-in workflows.
	reg := workflow.NewRegistry()
	if err := reg.Init(*dir); err != nil {
		fmt.Fprintf(os.Stderr, "FAIL: registry init: %v\n", err)
		os.Exit(1)
	}

	defs := reg.List()
	fmt.Printf("Loaded %d workflow(s)\n", len(defs))

	for _, def := range defs {
		errs := lintDef(def)
		if len(errs) > 0 {
			exitCode = 1
			fmt.Printf("\n%s:\n", def.ID)
			for _, e := range errs {
				fmt.Printf("  ✗ %s\n", e)
			}
		}
	}

	if exitCode == 0 {
		fmt.Println("All workflows pass lint.")
	}
	os.Exit(exitCode)
}

func lintDef(def *workflow.Definition) []string {
	var errs []string

	// 2. Schema validation (Validate already ran during Init, but double-check).
	if err := workflow.Validate(def); err != nil {
		errs = append(errs, fmt.Sprintf("validate: %v", err))
	}

	// 3. Check steps recursively.
	collectInputs := map[string]bool{}
	for _, in := range def.Inputs {
		collectInputs[in.Name] = true
	}
	errs = append(errs, lintSteps(def.Steps, collectInputs)...)

	return errs
}

func lintSteps(steps []workflow.Step, inputs map[string]bool) []string {
	var errs []string
	saveAsInScope := map[string]bool{}
	for k := range inputs {
		saveAsInScope[k] = true // inputs are already in scope
	}

	for i, s := range steps {
		prefix := fmt.Sprintf("step %d", i)
		errs = append(errs, lintStep(&s, prefix, saveAsInScope, inputs)...)
	}
	return errs
}

func lintStep(s *workflow.Step, prefix string, saveAsInScope, inputs map[string]bool) []string {
	var errs []string

	switch s.Kind {
	case workflow.StepKindTool:
		t := s.Tool
		if t == nil {
			return errs
		}
		// Check tool name.
		if !knownTools[t.Name] {
			errs = append(errs, fmt.Sprintf("%s: unknown tool %q", prefix, t.Name))
		}
		// Check arg references.
		for _, v := range t.Args {
			errs = append(errs, checkVarRefs(v, prefix, saveAsInScope)...)
		}
		// Track save_as.
		if t.SaveAs != "" {
			if saveAsInScope[t.SaveAs] {
				errs = append(errs, fmt.Sprintf("%s: duplicate save_as %q in scope", prefix, t.SaveAs))
			}
			saveAsInScope[t.SaveAs] = true
		}

	case workflow.StepKindBranch:
		b := s.Branch
		if b == nil {
			return errs
		}
		errs = append(errs, checkVarRefs(b.When, prefix, saveAsInScope)...)
		// Clone scope for each branch.
		thenScope := cloneMap(saveAsInScope)
		elseScope := cloneMap(saveAsInScope)
		if len(b.Then) > 0 {
			errs = append(errs, lintSteps(b.Then, thenScope)...)
		}
		if len(b.Else) > 0 {
			errs = append(errs, lintSteps(b.Else, elseScope)...)
		}

	case workflow.StepKindRandom:
		r := s.Random
		if r == nil {
			return errs
		}
		for j, w := range r.Weighted {
			childScope := cloneMap(saveAsInScope)
			errs = append(errs, lintSteps(w.Do, childScope)...)
			_ = j
		}

	case workflow.StepKindForEach:
		f := s.ForEach
		if f == nil {
			return errs
		}
		errs = append(errs, checkVarRefs(f.Over, prefix, saveAsInScope)...)
		childScope := cloneMap(saveAsInScope)
		if f.As != "" {
			childScope[f.As] = true
		}
		errs = append(errs, lintSteps(f.Do, childScope)...)

	case workflow.StepKindSkillCall:
		sc := s.SkillCall
		if sc == nil {
			return errs
		}
		if !knownSkills[sc.Skill] {
			errs = append(errs, fmt.Sprintf("%s: unknown skill %q", prefix, sc.Skill))
		}

	case workflow.StepKindLLMChoice:
		l := s.LLMChoice
		if l == nil {
			return errs
		}
		if l.SaveAs != "" {
			if saveAsInScope[l.SaveAs] {
				errs = append(errs, fmt.Sprintf("%s: duplicate save_as %q in scope", prefix, l.SaveAs))
			}
			saveAsInScope[l.SaveAs] = true
		}

	case workflow.StepKindWait, workflow.StepKindStop:
		// No additional checks.
	}
	return errs
}

// checkVarRefs scans a value (string, map, slice) for $variable references
// that are not defined in the current scope or inputs.
func checkVarRefs(v any, prefix string, scope map[string]bool) []string {
	switch x := v.(type) {
	case string:
		if strings.HasPrefix(x, "$") {
			refs := varRefRe.FindAllString(x, -1)
			var errs []string
			for _, ref := range refs {
				name := strings.TrimPrefix(ref, "$")
				// Take the first segment (before any dot) as the variable name.
				parts := strings.SplitN(name, ".", 2)
				varName := parts[0]
				if !scope[varName] {
					errs = append(errs, fmt.Sprintf("%s: variable %q not in scope (ref=%q)", prefix, varName, ref))
				}
			}
			return errs
		}
	case map[string]any:
		var errs []string
		for _, sub := range x {
			errs = append(errs, checkVarRefs(sub, prefix, scope)...)
		}
		return errs
	case []any:
		var errs []string
		for _, sub := range x {
			errs = append(errs, checkVarRefs(sub, prefix, scope)...)
		}
		return errs
	}
	return nil
}

func cloneMap(m map[string]bool) map[string]bool {
	out := make(map[string]bool, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
