package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"strings"
	"time"
)

// Runner is the engine's outbound interface. The schedule integration
// (P4) provides a concrete implementation that talks to the ws bridge,
// hermesrelay, and FollowSystem; tests and the standalone P1 build can
// pass a stub.
//
// All methods receive a context for cancellation (engine-level deadline
// or NPC offline detection) and return raw map[string]any payloads to
// keep the engine free of MCP-tool-specific schemas.
type Runner interface {
	// CallTool dispatches the named MCP tool with `args` resolved to
	// concrete values (no $ refs). Returns the tool's structured output
	// as a JSON-shaped map plus an `ok` flag. The engine inspects
	// `nothing_to_do` itself; runners should NOT swallow it.
	CallTool(ctx context.Context, npc, name string, args map[string]any) (map[string]any, error)

	// CallSkill triggers a Hermes SKILL run for the given NPC. Args may
	// be empty. The runner is free to dispatch synchronously or fire-
	// and-forget; the engine waits before continuing in either case.
	CallSkill(ctx context.Context, npc, skill string, args map[string]any) error

	// LLMChoice asks the NPC's LLM to pick one of the given options.
	// Returns the chosen string. The runner should constrain the model
	// to ONLY emit one of the options verbatim; if it fails, fall back
	// to the first option.
	LLMChoice(ctx context.Context, npc, prompt string, options []string) (string, error)

	// WaitIdle blocks until the NPC's FollowSystem is back to Idle (no
	// long-running mode) or the timeout elapses. The bool indicates
	// whether the wait condition succeeded (true) or timed out (false).
	WaitIdle(ctx context.Context, npc string, timeout time.Duration) (bool, error)
}

// Run drives a workflow definition to completion against the given runner.
// `npc` is passed through to every Runner call. `inputs` populates the
// initial scope (after applying defaults from the definition).
//
// Run is the single public entrypoint; engine methods are unexported so
// test code uses the same code path as production.
func Run(ctx context.Context, runner Runner, npc string, def *Definition, inputs map[string]any) (*RunResult, error) {
	if def == nil {
		return nil, errors.New("workflow.Run: nil definition")
	}
	if runner == nil {
		return nil, errors.New("workflow.Run: nil runner")
	}
	scope := NewScope()
	// Bind declared inputs. Caller-supplied values win; defaults fill
	// the rest. Inputs not declared in the spec are still bound (caller
	// may pass extras for ad-hoc workflows).
	for _, in := range def.Inputs {
		var value any = in.Default
		if v, ok := inputs[in.Name]; ok {
			value = v
		}
		if err := scope.Set(in.Name, value); err != nil {
			return nil, fmt.Errorf("input %q: %w", in.Name, err)
		}
	}
	for k, v := range inputs {
		if _, exists := scope.Get(k); exists {
			continue
		}
		_ = scope.Set(k, v)
	}

	eng := &engine{
		runner: runner,
		npc:    npc,
		def:    def,
		rng:    rand.New(rand.NewSource(time.Now().UnixNano())),
		result: &RunResult{WorkflowID: def.ID, NPC: npc},
	}
	stopped, err := eng.runSteps(ctx, def.Steps, scope)
	eng.result.Stopped = stopped
	return eng.result, err
}

// RunResult summarises a completed (or aborted) workflow run for log /
// debug surfaces.
type RunResult struct {
	WorkflowID string
	NPC        string
	// StepCount counts every dispatched step (tool/branch/random/foreach
	// iterations all count as 1 each).
	StepCount int
	// ToolCalls is the count of Tool steps that actually fired (excludes
	// short-circuited branches).
	ToolCalls int
	// Stopped is true when an early Stop step or on_nothing_to_do=stop
	// halted the run. Differentiates "ran to completion" from "deliberate
	// stop" for the log.
	Stopped       bool
	StopReason    string
	NothingToDoCt int
	// FailedStep is set when a step errored out hard. Engine returns the
	// error as the second return value of Run; this gives observers a
	// pointer to the step in question without re-walking the def.
	FailedStep int
}

// StopReasonAllZero is recorded when a looping workflow auto-stops after
// observing that every actionable group in the farm_actions output has zero
// work (the NPC has nothing left to do this iteration).
const StopReasonAllZero = "workflow.auto_stop.all_zero"

// SetSeed sets the RNG seed for deterministic tests. Production callers
// should not need this.
func (r *RunResult) String() string {
	return fmt.Sprintf("workflow %s on %s: steps=%d tools=%d nothing=%d stopped=%v reason=%q",
		r.WorkflowID, r.NPC, r.StepCount, r.ToolCalls, r.NothingToDoCt, r.Stopped, r.StopReason)
}

// ── engine ──────────────────────────────────────────────────────────────

type engine struct {
	runner Runner
	npc    string
	def    *Definition
	rng    *rand.Rand
	result *RunResult
}

// runSteps executes a slice of steps in order. Returns true when an
// inner step requested early termination (Stop or on_nothing_to_do=stop),
// in which case the caller (potentially nested) propagates upward.
func (e *engine) runSteps(ctx context.Context, steps []Step, scope *Scope) (bool, error) {
	for i := range steps {
		select {
		case <-ctx.Done():
			e.result.StopReason = "workflow.cancelled"
			return true, nil
		default:
		}
		stop, err := e.runStep(ctx, &steps[i], scope)
		if err != nil {
			return true, err
		}
		if stop {
			return true, nil
		}
	}
	return false, nil
}

func (e *engine) runStep(ctx context.Context, s *Step, scope *Scope) (bool, error) {
	e.result.StepCount++
	switch s.Kind {
	case StepKindTool:
		return e.runTool(ctx, s.Tool, scope)
	case StepKindBranch:
		return e.runBranch(ctx, s.Branch, scope)
	case StepKindRandom:
		return e.runRandom(ctx, s.Random, scope)
	case StepKindForEach:
		return e.runForEach(ctx, s.ForEach, scope)
	case StepKindSkillCall:
		return e.runSkill(ctx, s.SkillCall, scope)
	case StepKindLLMChoice:
		return e.runLLMChoice(ctx, s.LLMChoice, scope)
	case StepKindWait:
		return e.runWait(ctx, s.Wait, scope)
	case StepKindStop:
		e.result.StopReason = s.Stop.reasonOr("workflow.stop")
		return true, nil
	default:
		return true, fmt.Errorf("unknown step kind %q", s.Kind)
	}
}

// reasonOr is a tiny helper so StopStep can carry an empty reason.
func (s *StopStep) reasonOr(fallback string) string {
	if s == nil || s.Reason == "" {
		return fallback
	}
	return s.Reason
}

// ── tool ────────────────────────────────────────────────────────────────

// defaultToolTimeout is the per-tool-call real-time deadline applied when
// a ToolStep does not set an explicit timeout_seconds. Heavy actions
// (clear_debris, break_resource, etc.) should get an explicit value in the
// YAML so the intent is visible; this is the safety net.
const defaultToolTimeout = 60 * time.Second

func (e *engine) runTool(ctx context.Context, t *ToolStep, scope *Scope) (bool, error) {
	if t == nil || t.Name == "" {
		return true, errors.New("tool step missing name")
	}
	args, err := resolveArgs(t.Args, scope)
	if err != nil {
		return true, fmt.Errorf("tool %s args: %w", t.Name, err)
	}
		slog.Info("workflow: runTool", "npc", e.npc, "tool", t.Name, "args", args)


	// Per-tool timeout: explicit value wins, otherwise 60 s default.
	timeout := time.Duration(t.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = defaultToolTimeout
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	out, err := e.runner.CallTool(callCtx, e.npc, t.Name, args)
	if err != nil {
		return true, fmt.Errorf("tool %s: %w", t.Name, err)
	}
	e.result.ToolCalls++

	// Check for the structured "no work was found" ack the mod returns
	// from MarkNothingToDo. This is success at the protocol layer but a
	// signal to the workflow that the area was empty.
	if v, ok := out["nothing_to_do"]; ok && truthy(v) {
		e.result.NothingToDoCt++
		policy := strings.ToLower(strings.TrimSpace(t.OnNothingToDo))
		switch policy {
		case "", "skip":
			// Default: record it but continue.
		case "stop":
			reason, _ := out["reason"].(string)
			if reason == "" {
				reason = fmt.Sprintf("%s returned nothing_to_do", t.Name)
			}
			e.result.StopReason = reason
			return true, nil
		case "fail":
			reason, _ := out["reason"].(string)
			return true, fmt.Errorf("tool %s nothing_to_do: %s", t.Name, reason)
		default:
			return true, fmt.Errorf("tool %s: unknown on_nothing_to_do %q", t.Name, t.OnNothingToDo)
		}
	}

	if t.SaveAs != "" {
		if err := scope.Set(t.SaveAs, anyOrMap(out)); err != nil {
			return true, fmt.Errorf("tool %s save_as: %w", t.Name, err)
		}
	}
	return false, nil
}

// anyOrMap normalises tool outputs to map[string]any so dotted paths
// work uniformly. JSON unmarshaling already produces map[string]any in
// our pipelines, so this is a no-op fast path most of the time.
func anyOrMap(out map[string]any) any {
	if out == nil {
		return map[string]any{}
	}
	return out
}

// ── branch ──────────────────────────────────────────────────────────────

func (e *engine) runBranch(ctx context.Context, b *BranchStep, scope *Scope) (bool, error) {
	if b == nil {
		return true, errors.New("branch step missing payload")
	}
	cond, err := EvalBool(b.When, scope)
	if err != nil {
		return true, fmt.Errorf("branch when=%q: %w", b.When, err)
	}
	body := b.Then
	if !cond {
		body = b.Else
	}
	return e.runSteps(ctx, body, scope)
}

// ── random ──────────────────────────────────────────────────────────────

func (e *engine) runRandom(ctx context.Context, r *RandomStep, scope *Scope) (bool, error) {
	if r == nil || len(r.Weighted) == 0 {
		return false, nil // no-op
	}
	var total float64
	for _, w := range r.Weighted {
		if w.Weight > 0 {
			total += w.Weight
		}
	}
	if total <= 0 {
		return false, nil // all branches disabled — silently skip
	}
	pick := e.rng.Float64() * total
	var acc float64
	for _, w := range r.Weighted {
		if w.Weight <= 0 {
			continue
		}
		acc += w.Weight
		if pick <= acc {
			return e.runSteps(ctx, w.Do, scope)
		}
	}
	// Float rounding can fall through; run the last enabled branch.
	for i := len(r.Weighted) - 1; i >= 0; i-- {
		if r.Weighted[i].Weight > 0 {
			return e.runSteps(ctx, r.Weighted[i].Do, scope)
		}
	}
	return false, nil
}

// ── foreach ─────────────────────────────────────────────────────────────

func (e *engine) runForEach(ctx context.Context, f *ForEachStep, scope *Scope) (bool, error) {
	if f == nil || f.Over == "" || f.As == "" {
		return true, errors.New("foreach step missing over/as")
	}
	listVal, _ := scope.Resolve(f.Over)
	list, ok := listVal.([]any)
	if !ok {
		// Missing or wrong-typed value: treat as empty list (no error,
		// caller's data shape may legitimately omit the field).
		return false, nil
	}
	max := f.MaxIter
	if max <= 0 {
		max = 50
	}
	for i, item := range list {
		if i >= max {
			break
		}
		child := scope.Child()
		if err := child.Set(f.As, item); err != nil {
			return true, err
		}
		stop, err := e.runSteps(ctx, f.Do, child)
		if err != nil {
			return true, err
		}
		if stop {
			return true, nil
		}
	}
	return false, nil
}

// ── skill_call ──────────────────────────────────────────────────────────

func (e *engine) runSkill(ctx context.Context, s *SkillCallStep, scope *Scope) (bool, error) {
	select {
	case <-ctx.Done():
		return true, nil // graceful stop on cancel
	default:
	}
	if s == nil || s.Skill == "" {
		return true, errors.New("skill_call missing skill")
	}
	args, err := resolveArgs(s.Args, scope)
	if err != nil {
		return true, fmt.Errorf("skill_call %s args: %w", s.Skill, err)
	}
	if err := e.runner.CallSkill(ctx, e.npc, s.Skill, args); err != nil {
		return true, fmt.Errorf("skill_call %s: %w", s.Skill, err)
	}
	// For looping workflows: check if the observed state is all-zero
	// (NPC has nothing left to do).
	if e.def.Loop.IsLooping() && e.checkAutoStop(scope) {
		e.result.StopReason = StopReasonAllZero
		return true, nil // signal stop
	}
	return false, nil
}

// checkAutoStop returns true when the observed farm_actions output
// has zero work across all actionable groups — meaning the NPC
// has nothing left to do this iteration.
func (e *engine) checkAutoStop(scope *Scope) bool {
	if scope == nil {
		return false
	}
	raw, ok := scope.Get("obs")
	if !ok || raw == nil {
		return false
	}
	// Try to read the structured content. The tool output's first
	// content block is typically the JSON result.
	type actions struct {
		ActionsAvailable map[string]struct {
			Count int `json:"count"`
		} `json:"actions_available"`
	}
	var out actions
	b, err := json.Marshal(raw)
	if err != nil {
		return false
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return false
	}
	// Check the actionable groups — if all are zero, auto-stop.
	groups := []string{"harvest", "water", "clear", "till", "forage", "plant", "fill", "fill_blocked"}
	for _, g := range groups {
		if a, ok := out.ActionsAvailable[g]; ok && a.Count > 0 {
			return false
		}
	}
	return len(out.ActionsAvailable) > 0
}

// ── llm_choice ──────────────────────────────────────────────────────────

func (e *engine) runLLMChoice(ctx context.Context, l *LLMChoiceStep, scope *Scope) (bool, error) {
	if l == nil || l.SaveAs == "" || len(l.Options) == 0 {
		return true, errors.New("llm_choice missing save_as / options")
	}
	choice, err := e.runner.LLMChoice(ctx, e.npc, l.Prompt, l.Options)
	if err != nil {
		return true, fmt.Errorf("llm_choice: %w", err)
	}
	// Validate against the option list — defensive against models that
	// drift off-menu. Fall back to options[0] silently rather than
	// blowing up the run; the bound variable will still be a valid choice.
	valid := false
	for _, o := range l.Options {
		if o == choice {
			valid = true
			break
		}
	}
	if !valid {
		choice = l.Options[0]
	}
	if err := scope.Set(l.SaveAs, choice); err != nil {
		return true, err
	}
	return false, nil
}

// ── wait ────────────────────────────────────────────────────────────────

func (e *engine) runWait(ctx context.Context, w *WaitStep, scope *Scope) (bool, error) {
	if w == nil {
		return true, errors.New("wait step missing payload")
	}
	cond := strings.ToLower(strings.TrimSpace(w.Condition))
	timeout := time.Duration(w.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	switch cond {
	case "", "idle":
		ok, err := e.runner.WaitIdle(ctx, e.npc, timeout)
		if err != nil {
			return true, err
		}
		// Timeout is not a hard error — workflows may legitimately move
		// on if the previous step is still running. Engine records it
		// in the result so log can flag it.
		_ = ok
		return false, nil
	default:
		return true, fmt.Errorf("wait: unknown condition %q", cond)
	}
}

// ── argument resolution ─────────────────────────────────────────────────
//
// Args may contain $-prefixed strings or plain literals. We walk the map
// and replace any string value that starts with "$" by the resolved
// scope value; everything else passes through unchanged. Nested maps
// and lists are walked recursively.

func resolveArgs(args map[string]any, scope *Scope) (map[string]any, error) {
	if args == nil {
		return nil, nil
	}
	out := make(map[string]any, len(args))
	for k, v := range args {
		out[k] = resolveValue(v, scope)
	}
	return out, nil
}

func resolveValue(v any, scope *Scope) any {
	switch x := v.(type) {
	case string:
		// Only treat strings starting with `$` as references. This
		// lets workflows still pass literal IDs like "(O)472" without
		// escaping, since they don't begin with $.
		if strings.HasPrefix(x, "$") {
			if resolved, ok := scope.Resolve(x); ok {
				return resolved
			}
			// Unresolved refs become nil so downstream tools can decide;
			// arg validation is the tool's responsibility, not ours.
			return nil
		}
		return x
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, sub := range x {
			out[k] = resolveValue(sub, scope)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, sub := range x {
			out[i] = resolveValue(sub, scope)
		}
		return out
	default:
		return v
	}
}
