package workflow

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// ── precompile channel registry ───────────────────────────────────────
//
// PrecompileSkill sends a skill to Hermes with precompile=true and waits
// for the LLM to call workflow_precompile_result. This registry bridges
// the MCP tool handler (in adapters/stardew/tools) and the Runner.

var (
	precompileMu  sync.Mutex
	precompileChs = map[string]chan string{}
)

// RegisterPrecompile creates a channel for the given plan_id.
func RegisterPrecompile(planID string) chan string {
	ch := make(chan string, 1)
	precompileMu.Lock()
	precompileChs[planID] = ch
	precompileMu.Unlock()
	return ch
}

// CompletePrecompile delivers the plan JSON to the waiting channel.
func CompletePrecompile(planID, planJSON string) bool {
	precompileMu.Lock()
	ch, ok := precompileChs[planID]
	if ok {
		delete(precompileChs, planID)
	}
	precompileMu.Unlock()
	if !ok {
		return false
	}
	select {
	case ch <- planJSON:
	default:
	}
	return true
}

// UnregisterPrecompile removes a precompile channel without delivering.
func UnregisterPrecompile(planID string) {
	precompileMu.Lock()
	delete(precompileChs, planID)
	precompileMu.Unlock()
}

// ── RecordingRunner ───────────────────────────────────────────────────

// RecordingRunner wraps a real Runner, intercepting mutating tool calls and
// recording them as concrete ToolSteps. Read-only tools (inspect, find, get)
// are proxied to the real runner so the LLM gets accurate environment data.
//
// BuildDefinition returns a precompiled workflow.Definition that can be
// replayed at trigger time with zero LLM latency.
type RecordingRunner struct {
	real Runner

	mu    sync.Mutex
	steps []recordedStep
}

type recordedStep struct {
	ToolName string
	Args     map[string]any
}

// NewRecordingRunner creates a RecordingRunner wrapping the given real runner.
func NewRecordingRunner(real Runner) *RecordingRunner {
	return &RecordingRunner{real: real}
}

// RecordedSteps returns the number of mutating tool calls captured so far.
func (r *RecordingRunner) RecordedSteps() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.steps)
}

// BuildDefinition returns a precompiled workflow.Definition from the recorded
// steps. Returns nil if no mutating steps were captured (nothing to do).
func (r *RecordingRunner) BuildDefinition(skill string) *Definition {
	r.mu.Lock()
	defer r.mu.Unlock()

	if len(r.steps) == 0 {
		slog.Debug("precompile: BuildDefinition — no steps recorded")
		return nil
	}

	steps := make([]Step, 0, len(r.steps))
	for _, s := range r.steps {
		steps = append(steps, Step{
			Kind: StepKindTool,
			Tool: &ToolStep{
				Name: s.ToolName,
				Args: s.Args,
			},
		})
	}

	return &Definition{
		ID:          "precompiled_" + skill,
		Description: "Precompiled from skill_call " + skill + " — concrete tool steps, no LLM",
		Version:     "1",
		Steps:       steps,
	}
}

// CallTool proxies read-only tools to the real runner; mutating tools are
// recorded and a fake success is returned so the LLM's turn continues.
func (r *RecordingRunner) CallTool(ctx context.Context, npc, name string, args map[string]any) (map[string]any, error) {
	if isReadOnlyTool(name) {
		slog.Debug("precompile: proxying read-only tool", "tool", name)
		return r.real.CallTool(ctx, npc, name, args)
	}

	r.mu.Lock()
	r.steps = append(r.steps, recordedStep{ToolName: name, Args: args})
	n := len(r.steps)
	r.mu.Unlock()

	slog.Debug("precompile: recorded mutating tool",
		"tool", name, "step", n,
		"args", args,
	)
	return map[string]any{"ok": true, "nothing_to_do": false}, nil
}

// CallSkill forwards to the real runner.
func (r *RecordingRunner) CallSkill(ctx context.Context, npc, skill string, args map[string]any) error {
	return r.real.CallSkill(ctx, npc, skill, args)
}

// LLMChoice forwards to the real runner.
func (r *RecordingRunner) LLMChoice(ctx context.Context, npc, prompt string, options []string) (string, error) {
	return r.real.LLMChoice(ctx, npc, prompt, options)
}

// WaitIdle forwards to the real runner.
func (r *RecordingRunner) WaitIdle(ctx context.Context, npc string, timeout time.Duration) (bool, error) {
	return r.real.WaitIdle(ctx, npc, timeout)
}

// PrecompileSkill runs the skill in recording mode and returns the
// precompiled definition. This is the default implementation used when
// the real runner doesn't provide its own PrecompileSkill override.
func (r *RecordingRunner) PrecompileSkill(ctx context.Context, npc, skill string, args map[string]any) (*Definition, error) {
	def := &Definition{
		ID:      "precompile_" + skill,
		Version: "1",
		Steps: []Step{
			{
				Kind: StepKindSkillCall,
				SkillCall: &SkillCallStep{
					Skill: skill,
					Args:  args,
				},
			},
		},
	}

	_, err := Run(ctx, r, npc, def, nil)
	if err != nil {
		return nil, err
	}

	result := r.BuildDefinition(skill)
	if result == nil {
		slog.Warn("precompile: no steps recorded", "npc", npc, "skill", skill)
	}
	return result, nil
}

// ── read-only tool detection ──────────────────────────────────────────────

var readOnlyToolPrefixes = []string{
	"npc_inspect_",
	"npc_find_",
	"npc_get_",
	"game_get_",
	"player_get_",
	"player_query_",
	"workflow_list",
	"workflow_get",
	"npc_workflow_status",
	"workflow_run_history",
	"npc_perception_",
}

func isReadOnlyTool(name string) bool {
	for _, prefix := range readOnlyToolPrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

// ── JSON encoding helpers (used by MCPRunner) ─────────────────────────

// EncodePrecompilePayload builds the JSON payload for a precompile
// workflow_skill_call event.
func EncodePrecompilePayload(npc, skill, planID string, args map[string]any) ([]byte, error) {
	pcArgs := make(map[string]any, len(args)+2)
	for k, v := range args {
		pcArgs[k] = v
	}
	pcArgs["precompile"] = true
	pcArgs["plan_id"] = planID

	return json.Marshal(map[string]any{
		"npc":   npc,
		"skill": skill,
		"args":  pcArgs,
	})
}
