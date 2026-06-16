// Package workflow defines the schedule workflow DSL and the engine that
// drives it. A workflow is an ordered list of steps that the scheduler
// executes when an entry fires; steps can be plain tool calls, branches
// on observed game state, weighted-random pickers, foreach loops, and a
// few specialised forms (skill_call, llm_choice, wait, stop).
//
// The engine itself lives in engine.go. This file declares the data
// model only — it has no dependencies on the MCP server, the websocket
// bridge, or the Hermes relay so it can be unit-tested in isolation.
//
// JSON / YAML wire shape is described inline on each struct via the
// `json` tags. YAML loading reuses the same shape via gopkg.in/yaml.v3
// (see registry.go in P2). All step types share a tagged union via the
// `Kind` field so the input is self-describing:
//
//   - { kind: "tool",        name: "...", args: {...}, save_as: "obs" }
//   - { kind: "branch",      when: "$obs.water.count > 0", then: [...], else: [...] }
//   - { kind: "random",      weighted: [{ weight: 3, do: [...] }, ...] }
//   - { kind: "foreach",     over: "$obs.harvest.crops", as: "c", do: [...] }
//   - { kind: "skill_call",  skill: "smartnpc-greeting" }
//   - { kind: "llm_choice",  prompt: "...", options: ["a","b"], save_as: "pick" }
//   - { kind: "wait",        condition: "idle", timeout_seconds: 30 }
//   - { kind: "stop",        reason: "..." }
//
// Inputs surface to expressions as `$<name>` (e.g. `$obs.water.count`).
// Tool outputs go into the same namespace via `save_as`. Engine variables
// are *immutable per write* — a step writes once via save_as and later
// steps read; foreach binds a fresh scope per iteration.
package workflow

// Definition is one named workflow. Either built-in (loaded from
// pkg/workflow/builtin) or inline (declared by the LLM in npc_plan_day).
type Definition struct {
	ID          string         `json:"id"          yaml:"id"`
	Description string         `json:"description" yaml:"description,omitempty"`
	Version     string         `json:"version"     yaml:"version,omitempty"`
	// Inputs declares the optional named arguments the workflow accepts.
	// Each input may declare a default; if absent and the call did not
	// supply a value, the variable resolves to nil (expression-friendly).
	Inputs []InputSpec `json:"inputs,omitempty" yaml:"inputs,omitempty"`
	// Steps is the ordered body of the workflow.
	Steps []Step `json:"steps" yaml:"steps"`
}

// InputSpec describes one named input slot.
type InputSpec struct {
	Name        string `json:"name"                  yaml:"name"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
	// Default is the value to bind when the caller did not supply this
	// input. Stored as `any` to keep YAML loading trivial.
	Default any `json:"default,omitempty" yaml:"default,omitempty"`
}

// Step is the tagged-union container. Exactly one of the optional sub-fields
// is populated, selected by Kind. Using a single struct (rather than an
// interface) keeps YAML/JSON unmarshaling cheap and avoids reflection
// gymnastics — the cost is a few unused pointers per step, which is fine
// at the scales we run (≤ 50 steps per workflow).
type Step struct {
	Kind StepKind `json:"kind" yaml:"kind"`

	// Optional human-readable label, surfaced in trace logs / debug UIs.
	// Engine ignores it functionally.
	Label string `json:"label,omitempty" yaml:"label,omitempty"`

	Tool      *ToolStep      `json:",inline,omitempty" yaml:",inline,omitempty"`
	Branch    *BranchStep    `json:",inline,omitempty" yaml:",inline,omitempty"`
	Random    *RandomStep    `json:",inline,omitempty" yaml:",inline,omitempty"`
	ForEach   *ForEachStep   `json:",inline,omitempty" yaml:",inline,omitempty"`
	SkillCall *SkillCallStep `json:",inline,omitempty" yaml:",inline,omitempty"`
	LLMChoice *LLMChoiceStep `json:",inline,omitempty" yaml:",inline,omitempty"`
	Wait      *WaitStep      `json:",inline,omitempty" yaml:",inline,omitempty"`
	Stop      *StopStep      `json:",inline,omitempty" yaml:",inline,omitempty"`
}

// StepKind tags the active sub-payload of a Step.
type StepKind string

const (
	StepKindTool      StepKind = "tool"
	StepKindBranch    StepKind = "branch"
	StepKindRandom    StepKind = "random"
	StepKindForEach   StepKind = "foreach"
	StepKindSkillCall StepKind = "skill_call"
	StepKindLLMChoice StepKind = "llm_choice"
	StepKindWait      StepKind = "wait"
	StepKindStop      StepKind = "stop"
)

// ToolStep invokes a single MCP tool (typically a mod handler like
// npc_water_crops). Args may reference variables via $name.path syntax.
type ToolStep struct {
	Name string         `json:"name"             yaml:"name"`
	Args map[string]any `json:"args,omitempty"   yaml:"args,omitempty"`
	// SaveAs binds the tool's output map under this variable name in the
	// surrounding scope; later steps can read e.g. `$obs.water.count`.
	SaveAs string `json:"save_as,omitempty" yaml:"save_as,omitempty"`
	// OnNothingToDo controls what to do when the tool returned a
	// nothing_to_do=true ack. "skip" continues to the next step (default);
	// "stop" ends the workflow with the tool's reason; "fail" propagates
	// up as a workflow error.
	OnNothingToDo string `json:"on_nothing_to_do,omitempty" yaml:"on_nothing_to_do,omitempty"`
}

// BranchStep is an if/else. `When` is evaluated against the current
// variable scope; truthy → Then, falsy → Else.
type BranchStep struct {
	When string `json:"when"          yaml:"when"`
	Then []Step `json:"then,omitempty" yaml:"then,omitempty"`
	Else []Step `json:"else,omitempty" yaml:"else,omitempty"`
}

// RandomStep runs exactly ONE of its branches, sampled according to the
// per-branch weight. Weights need not sum to 1 — they're normalised at
// runtime. A weight of 0 disables a branch (handy for personality gating
// via inputs).
type RandomStep struct {
	Weighted []RandomBranch `json:"weighted" yaml:"weighted"`
}

// RandomBranch is one option of a RandomStep.
type RandomBranch struct {
	Weight float64 `json:"weight" yaml:"weight"`
	Do     []Step  `json:"do"     yaml:"do"`
}

// ForEachStep iterates a list-valued variable. Each iteration binds a
// fresh scope where `$<As>` refers to the current item; outer-scope vars
// are still visible. MaxIter caps iterations to defend against runaway
// data — defaults to 50 if unset.
type ForEachStep struct {
	Over    string `json:"over"               yaml:"over"`
	As      string `json:"as"                 yaml:"as"`
	Do      []Step `json:"do"                 yaml:"do"`
	MaxIter int    `json:"max_iter,omitempty" yaml:"max_iter,omitempty"`
}

// SkillCallStep delegates to an existing Hermes SKILL by triggering a
// pseudo-event through hermesrelay (the runner's job to wire). Use this
// to keep complex prompted flows like smartnpc-greeting reachable from a
// workflow without re-implementing them.
type SkillCallStep struct {
	Skill string         `json:"skill"          yaml:"skill"`
	Args  map[string]any `json:"args,omitempty" yaml:"args,omitempty"`
}

// LLMChoiceStep asks the NPC's LLM to pick from a small fixed list.
// The chosen string is bound under SaveAs; later steps branch on it.
// Use sparingly — most decisions should be deterministic branches.
type LLMChoiceStep struct {
	Prompt  string   `json:"prompt"  yaml:"prompt"`
	Options []string `json:"options" yaml:"options"`
	SaveAs  string   `json:"save_as" yaml:"save_as"`
}

// WaitStep blocks until a runtime condition holds. "idle" is the typical
// case: wait for FollowSystem.GetMode(npc) to return Idle before the
// next tool step.
type WaitStep struct {
	Condition      string `json:"condition"               yaml:"condition"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty" yaml:"timeout_seconds,omitempty"`
}

// StopStep ends the workflow early. Reason is recorded in the run log.
type StopStep struct {
	Reason string `json:"reason,omitempty" yaml:"reason,omitempty"`
}
