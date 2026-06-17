package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/OmniStormX/SmartNPC/adapters/stardew/bridge"
)

// FollowSystemQuery answers whether an NPC's FollowSystem is idle.
// The production implementation calls npc_get_behavior through the ws bridge.
type FollowSystemQuery interface {
	GetMode(npc string) string // "Idle" or other mode tag
}

// MCPRunnerOptions configures the production Runner.
type MCPRunnerOptions struct {
	Bridge *bridge.WSClient
	Follow FollowSystemQuery
	Logger *slog.Logger
	// Relay sends synthetic events to Hermes to invoke skills.
	// When nil, CallSkill is a no-op (logs and returns).
	Relay bridge.EventHandler
	// ChoiceReply is called by the workflow_choice_reply MCP tool to
	// complete a pending LLMChoice request. The engine calls LLMChoice,
	// which registers a requestID and waits; the MCP tool calls
	// ChoiceReply with the agent's answer.
	ChoiceReply func(requestID, choice string)
}

// MCPRunner is the production Runner. Each Run() invocation should get a
// dedicated MCPRunner so per-run state (pending choices) is isolated.
//
// CallTool dispatches the named action to the mod via the ws bridge.
// CallSkill forwards to hermesrelay as a synthetic event (fire-and-forget).
// LLMChoice does a synchronous round-trip: sends a synthetic event to the
// agent, then waits for the agent to call workflow_choice_reply.
// WaitIdle polls FollowSystem until the NPC is idle or timeout.
type MCPRunner struct {
	bridge      *bridge.WSClient
	follow      FollowSystemQuery
	relay       bridge.EventHandler
	logger      *slog.Logger
	choiceReply func(requestID, choice string)

	// Pending LLMChoice requests keyed by requestID.
	choices map[string]chan string
}

// NewMCPRunner creates a production runner. opts.Bridge and opts.Logger are
// required; opts.Follow defaults to a no-op that always returns "Idle".
func NewMCPRunner(opts MCPRunnerOptions) *MCPRunner {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	return &MCPRunner{
		bridge:      opts.Bridge,
		follow:      opts.Follow,
		relay:       opts.Relay,
		logger:      opts.Logger,
		choiceReply: opts.ChoiceReply,
		choices:     make(map[string]chan string),
	}
}

// CallTool dispatches the named action to the mod via the ws bridge.
// Pre-pends "npc" to args if not already present so the mod routes correctly.
func (r *MCPRunner) CallTool(ctx context.Context, npc, name string, args map[string]any) (map[string]any, error) {
	if r.bridge == nil {
		return nil, fmt.Errorf("MCPRunner.CallTool: no bridge configured")
	}
	if args == nil {
		args = map[string]any{}
	}
	if _, ok := args["npc"]; !ok {
		args["npc"] = npc
	}
	raw, err := r.bridge.CallAs(ctx, npc, name, args)
	if err != nil {
		return nil, fmt.Errorf("tool %s: %w", name, err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		// Fall back to wrapping the raw bytes so the engine gets a usable map.
		return map[string]any{"ok": true, "_raw": string(raw)}, nil
	}
	return out, nil
}

// CallSkill sends a synthetic workflow_skill_call event to the Hermes agent
// via the relay, then waits for the agent to finish its turn. The agent loads
// the specified skill via skill_view and executes it dynamically (inspect →
// decide → call tools). This method blocks until the NPC's FollowSystem has
// been continuously idle for stableIdleDuration, or until ctx is cancelled.
func (r *MCPRunner) CallSkill(ctx context.Context, npc, skill string, args map[string]any) error {
	if r.relay == nil {
		r.logger.Warn("MCPRunner.CallSkill: no relay configured, skip", "npc", npc, "skill", skill)
		return nil
	}
	if args == nil {
		args = map[string]any{}
	}
	data, err := json.Marshal(map[string]any{
		"npc":   npc,
		"skill": skill,
		"args":  args,
	})
	if err != nil {
		return fmt.Errorf("skill_call marshal: %w", err)
	}
	r.logger.Info("MCPRunner.CallSkill: sending to Hermes", "npc", npc, "skill", skill)
	r.relay(ctx, "workflow_skill_call", data)

	// Wait for the LLM agent to complete its turn. The agent may call
	// multiple tools (inspect → clear → till → plant → water → bubble).
	// We poll npc_get_behavior until the NPC has been continuously idle
	// for stableIdleDuration, meaning the agent turn is over.
	if r.bridge == nil {
		r.logger.Warn("MCPRunner.CallSkill: no bridge, cannot wait for idle", "npc", npc)
		return nil
	}

	const (
		pollInterval       = 2 * time.Second
		stableIdleDuration = 5 * time.Second
		maxWait            = 120 * time.Second
	)

	deadline := time.Now().Add(maxWait)
	idleSince := time.Time{}
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("skill_call %s: context cancelled while waiting: %w", skill, ctx.Err())
		case <-ticker.C:
			if time.Now().After(deadline) {
				r.logger.Warn("MCPRunner.CallSkill: wait for idle timed out",
					"npc", npc, "skill", skill, "timeout", maxWait)
				return nil // soft timeout — don't fail the workflow
			}

			mode := r.getBehaviorMode(ctx, npc)
			r.logger.Debug("MCPRunner.CallSkill: polling behavior",
				"npc", npc, "mode", mode)

			if mode == "Idle" || mode == "" {
				if idleSince.IsZero() {
					idleSince = time.Now()
				} else if time.Since(idleSince) >= stableIdleDuration {
					r.logger.Info("MCPRunner.CallSkill: agent turn completed",
						"npc", npc, "skill", skill, "stable_idle", time.Since(idleSince).String())
					return nil
				}
			} else {
				idleSince = time.Time{} // reset — NPC is busy
			}
		}
	}
}

// getBehaviorMode queries the mod for the NPC's current FollowSystem mode.
// Returns "Idle" when the NPC is not executing any long-running action.
func (r *MCPRunner) getBehaviorMode(ctx context.Context, npc string) string {
	callCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	raw, err := r.bridge.CallAs(callCtx, npc, "npc_get_behavior", map[string]any{"npc": npc})
	if err != nil {
		r.logger.Warn("MCPRunner.getBehaviorMode: call failed", "npc", npc, "err", err)
		return ""
	}
	var out struct {
		Mode string `json:"mode"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		r.logger.Warn("MCPRunner.getBehaviorMode: unmarshal failed", "npc", npc, "err", err)
		return ""
	}
	return out.Mode
}

// LLMChoice asks the NPC's LLM to pick one of the given options.
// Sends a synthetic event through the relay, then waits for the agent to
// respond via workflow_choice_reply or until the context deadline.
func (r *MCPRunner) LLMChoice(ctx context.Context, npc, prompt string, options []string) (string, error) {
	if len(options) == 0 {
		return "", fmt.Errorf("LLMChoice: no options provided")
	}
	// In P4, LLMChoice returns options[0] as the default when there's no
	// relay wired. The full synchronous round-trip (via ChoiceReply) will
	// be activated in the scheduler_pump integration.
	r.logger.Info("MCPRunner.LLMChoice: using default (relay not wired)", "npc", npc, "prompt", prompt)
	return options[0], nil
}

// WaitIdle polls FollowSystem at 250ms intervals until the NPC is idle or
// the timeout expires. Returns true when idle was reached, false on timeout.
func (r *MCPRunner) WaitIdle(ctx context.Context, npc string, timeout time.Duration) (bool, error) {
	if r.follow == nil {
		// No follow system configured — assume idle immediately.
		return true, nil
	}
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-ticker.C:
			if time.Now().After(deadline) {
				return false, nil // timeout is not an error
			}
			mode := r.follow.GetMode(npc)
			if mode == "Idle" || mode == "" {
				return true, nil
			}
		}
	}
}
