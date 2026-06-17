package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/OmniStormX/SmartNPC/pkg/workflow"
)

// ── workflow_list ─────────────────────────────────────────────────────

// WorkflowListInput is the (empty) input for workflow_list.
type WorkflowListInput struct{}

// WorkflowListEntry is a single workflow in the list.
type WorkflowListEntry struct {
	ID          string              `json:"id"`
	Description string              `json:"description"`
	Inputs      []workflow.InputSpec `json:"inputs,omitempty"`
}

// WorkflowListOutput returns every registered workflow.
type WorkflowListOutput struct {
	OK        bool                `json:"ok"`
	Workflows []WorkflowListEntry `json:"workflows"`
}

// ── workflow_get ──────────────────────────────────────────────────────

// WorkflowGetInput selects a single workflow.
type WorkflowGetInput struct {
	ID string `json:"id" jsonschema:"workflow id from workflow_list"`
}

// WorkflowGetOutput returns the full definition as a JSON object.
type WorkflowGetOutput struct {
	OK         bool   `json:"ok"`
	Definition any    `json:"definition,omitempty"`
	Message    string `json:"message,omitempty"`
}

// ── workflow_run_inline ───────────────────────────────────────────────

// WorkflowRunInlineInput runs a workflow against a no-op runner.
type WorkflowRunInlineInput struct {
	NPC        string `json:"npc"                    jsonschema:"NPC internal name to 'run as'"`
	WorkflowID string `json:"workflow_id,omitempty"  jsonschema:"registered workflow id to run"`
	Inline     any    `json:"inline,omitempty"       jsonschema:"ad-hoc workflow definition (JSON object with id, steps, and optional inputs)"`
	Args       map[string]any `json:"args,omitempty" jsonschema:"input values for the workflow"`
}

// WorkflowRunInlineOutput summarises the run.
type WorkflowRunInlineOutput struct {
	OK            bool   `json:"ok"`
	StepCount     int    `json:"step_count"`
	ToolCalls     int    `json:"tool_calls"`
	NothingToDoCt int    `json:"nothing_to_do_ct"`
	Stopped       bool   `json:"stopped,omitempty"`
	StopReason    string `json:"stop_reason,omitempty"`
	Message       string `json:"message"`
}

// ── workflow_choice_reply ─────────────────────────────────────────────

// WorkflowChoiceReplyInput carries the agent's answer.
type WorkflowChoiceReplyInput struct {
	RequestID string `json:"request_id" jsonschema:"the request_id from the workflow_llm_choice event"`
	Choice    string `json:"choice"     jsonschema:"the chosen option (exact string from the options list)"`
}

// WorkflowChoiceReplyOutput acknowledges receipt.
type WorkflowChoiceReplyOutput struct {
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
}

// ── noopRunner ────────────────────────────────────────────────────────

type noopRunner struct{}

func (r *noopRunner) CallTool(_ context.Context, npc, name string, args map[string]any) (map[string]any, error) {
	return map[string]any{"ok": true}, nil
}

func (r *noopRunner) CallSkill(_ context.Context, npc, skill string, args map[string]any) error {
	return nil
}

func (r *noopRunner) LLMChoice(_ context.Context, npc, prompt string, options []string) (string, error) {
	if len(options) > 0 {
		return options[0], nil
	}
	return "", nil
}

func (r *noopRunner) WaitIdle(_ context.Context, npc string, timeout time.Duration) (bool, error) {
	return true, nil
}

// ── pending choices registry (P4 LLMChoice protocol) ──────────────────

var (
	pendingChoicesMu sync.Mutex
	pendingChoices   = map[string]chan string{}
)

// RegisterPendingChoice creates a channel for the given requestID.
func RegisterPendingChoice(requestID string) chan string {
	ch := make(chan string, 1)
	pendingChoicesMu.Lock()
	pendingChoices[requestID] = ch
	pendingChoicesMu.Unlock()
	return ch
}

// CompletePendingChoice delivers the choice to the waiting channel.
func CompletePendingChoice(requestID, choice string) bool {
	pendingChoicesMu.Lock()
	ch, ok := pendingChoices[requestID]
	if ok {
		delete(pendingChoices, requestID)
	}
	pendingChoicesMu.Unlock()
	if !ok {
		return false
	}
	select {
	case ch <- choice:
	default:
	}
	return true
}

// ── registration ──────────────────────────────────────────────────────

// RegisterWorkflow mounts the workflow discovery and debug tools on the MCP
// server. `reg` must already be initialised (Init called). When debug is
// false, workflow_run_inline is skipped.
func RegisterWorkflow(s *mcp.Server, reg *workflow.Registry, debug bool) {
	if reg == nil {
		return
	}

	mcp.AddTool(s, &mcp.Tool{
		Name: "workflow_list",
		Description: "List all available built-in and overridden workflows. " +
			"Use this at the start of npc_plan_day to discover what workflows you can " +
			"schedule — each workflow bundles several farm-action tools into a reliable " +
			"multi-step routine that runs without further LLM involvement.\n\n" +
			"When to call: before calling npc_plan_day (once per game day), to pick " +
			"workflow_ids for your schedule entries. Also call whenever you want to " +
			"explore what workflows exist.\n\n" +
			"Side-effect: READ.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in WorkflowListInput) (*mcp.CallToolResult, WorkflowListOutput, error) {
		logToolCall("workflow_list", in)
		defs := reg.List()
		entries := make([]WorkflowListEntry, 0, len(defs))
		for _, d := range defs {
			entries = append(entries, WorkflowListEntry{
				ID:          d.ID,
				Description: d.Description,
				Inputs:      d.Inputs,
			})
		}
		return nil, WorkflowListOutput{
			OK:        true,
			Workflows: entries,
		}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "workflow_get",
		Description: "Return the full definition (steps, inputs, branching logic) of a " +
			"single named workflow. Use this to inspect a workflow you're considering for " +
			"your schedule — helps you understand what it will do step-by-step.\n\n" +
			"When to call: when you want to read a specific workflow's structure before " +
			"scheduling it. Call workflow_list first to discover IDs, then workflow_get " +
			"to zoom into one.\n\n" +
			"Side-effect: READ.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in WorkflowGetInput) (*mcp.CallToolResult, WorkflowGetOutput, error) {
		logToolCall("workflow_get", in)
		if in.ID == "" {
			return nil, WorkflowGetOutput{}, fmt.Errorf("id is required")
		}
		def := reg.Get(in.ID)
		if def == nil {
			return nil, WorkflowGetOutput{OK: false, Message: fmt.Sprintf("unknown workflow %q — use workflow_list to see available IDs", in.ID)}, nil
		}
		raw, _ := json.Marshal(def)
		var defAny any
		_ = json.Unmarshal(raw, &defAny)
		return nil, WorkflowGetOutput{
			OK:         true,
			Definition: defAny,
			Message:    fmt.Sprintf("workflow %q (%d steps)", def.ID, len(def.Steps)),
		}, nil
	})

	if debug {
		mcp.AddTool(s, &mcp.Tool{
			Name: "workflow_run_inline",
			Description: "[DEBUG ONLY] Run a workflow definition against a no-op runner to " +
				"validate its structure and flow. Tool calls are logged but not actually " +
				"executed — use this to test a new inline workflow before scheduling it.\n\n" +
				"Provide either `workflow_id` (to run a registered workflow) or `inline` " +
				"(to test an ad-hoc definition). Pass `args` to supply input values.\n\n" +
				"When to call: debugging only. Usable when mcp was started with --log-level debug.\n\n" +
				"Side-effect: READ (no world mutation — runs against a no-op runner).",
		}, func(ctx context.Context, _ *mcp.CallToolRequest, in WorkflowRunInlineInput) (*mcp.CallToolResult, WorkflowRunInlineOutput, error) {
			logToolCall("workflow_run_inline", in)
			if in.NPC == "" {
				in.NPC = "debug"
			}

			var def *workflow.Definition
			switch {
			case in.WorkflowID != "":
				def = reg.Get(in.WorkflowID)
				if def == nil {
					return nil, WorkflowRunInlineOutput{}, fmt.Errorf("unknown workflow %q", in.WorkflowID)
				}
			case in.Inline != nil:
				raw, err := json.Marshal(in.Inline)
				if err != nil {
					return nil, WorkflowRunInlineOutput{}, fmt.Errorf("inline workflow: marshal: %w", err)
				}
				var inline workflow.Definition
				if err := json.Unmarshal(raw, &inline); err != nil {
					return nil, WorkflowRunInlineOutput{}, fmt.Errorf("inline workflow invalid JSON: %w", err)
				}
				if err := workflow.Validate(&inline); err != nil {
					return nil, WorkflowRunInlineOutput{}, fmt.Errorf("inline workflow invalid: %w", err)
				}
				def = &inline
			default:
				return nil, WorkflowRunInlineOutput{}, fmt.Errorf("workflow_id or inline is required")
			}

			runCtx := ctx // no extra timeout — parent ctx provides guardrail

			runner := &noopRunner{}
			res, err := workflow.Run(runCtx, runner, in.NPC, def, in.Args)
			if err != nil {
				return nil, WorkflowRunInlineOutput{}, fmt.Errorf("workflow run failed: %w", err)
			}

			msg := fmt.Sprintf("%s: %d steps, %d tool calls, %d nothing-to-do, stopped=%v",
				res.WorkflowID, res.StepCount, res.ToolCalls, res.NothingToDoCt, res.Stopped)
			if res.Stopped {
				msg += fmt.Sprintf(" (%s)", res.StopReason)
			}
			slog.Info("workflow_run_inline", "npc", in.NPC, "result", msg)

			return nil, WorkflowRunInlineOutput{
				OK:            true,
				StepCount:     res.StepCount,
				ToolCalls:     res.ToolCalls,
				NothingToDoCt: res.NothingToDoCt,
				Stopped:       res.Stopped,
				StopReason:    res.StopReason,
				Message:       msg,
			}, nil
		})
	}

	// workflow_choice_reply is always registered.
	registerChoiceReplyTool(s)

	// workflow_run_history is always registered — reads from jsonl files.
	registerRunHistoryTool(s)
}

// registerChoiceReplyTool mounts workflow_choice_reply on the MCP server.
func registerChoiceReplyTool(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "workflow_choice_reply",
		Description: "Respond to a workflow llm_choice request. When the " +
			"workflow engine asks you to pick from a list of options, call " +
			"this tool with the exact request_id from the prompt and your " +
			"chosen option string.\n\n" +
			"IMPORTANT: ONLY call this when you receive a workflow_llm_choice " +
			"event. Copy the choice string EXACTLY from the options list — " +
			"do not rephrase or summarize.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in WorkflowChoiceReplyInput) (*mcp.CallToolResult, WorkflowChoiceReplyOutput, error) {
		if in.RequestID == "" || in.Choice == "" {
			return nil, WorkflowChoiceReplyOutput{OK: false, Message: "request_id and choice are required"}, nil
		}
		ok := CompletePendingChoice(in.RequestID, in.Choice)
		if ok {
			logToolCall("workflow_choice_reply", in)
			return nil, WorkflowChoiceReplyOutput{OK: true, Message: fmt.Sprintf("choice %q recorded", in.Choice)}, nil
		}
		return nil, WorkflowChoiceReplyOutput{OK: false, Message: fmt.Sprintf("unknown or expired request_id %q", in.RequestID)}, nil
	})
}

// ── workflow_run_history ─────────────────────────────────────────────────

// WorkflowRunHistoryInput queries past workflow runs.
type WorkflowRunHistoryInput struct {
	NPC     string `json:"npc"               jsonschema:"NPC internal name"`
	Season  string `json:"season,omitempty"  jsonschema:"spring/summer/fall/winter — defaults to current season if empty"`
	Day     int    `json:"day,omitempty"     jsonschema:"game day 1-28"`
	Year    int    `json:"year,omitempty"    jsonschema:"game year"`
	Limit   int    `json:"limit,omitempty"   jsonschema:"max records to return (default 20)"`
}

// WorkflowRunHistoryOutput returns past run records.
type WorkflowRunHistoryOutput struct {
	OK      bool                 `json:"ok"`
	Records []WorkflowRunRecord  `json:"records"`
	Message string               `json:"message,omitempty"`
}

// registerRunHistoryTool mounts workflow_run_history on the MCP server.
func registerRunHistoryTool(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "workflow_run_history",
		Description: "Query past workflow runs for an NPC on a given day. " +
			"Returns step counts, tool calls, nothing-to-do counts, " +
			"stop reasons, and errors. Use this to review what workflows " +
			"ran today (or yesterday) and how they performed.\n\n" +
			"When to call: during npc_plan_day to check what ran yesterday; " +
			"after a workflow run to verify its result; or when the player " +
			"asks \"what did you do today?\".\n\n" +
			"Side-effect: READ.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in WorkflowRunHistoryInput) (*mcp.CallToolResult, WorkflowRunHistoryOutput, error) {
		if in.NPC == "" {
			return nil, WorkflowRunHistoryOutput{OK: false, Message: "npc is required"}, nil
		}
		limit := in.Limit
		if limit <= 0 {
			limit = 20
		}

		records := ReadWorkflowRunHistory(in.NPC, in.Season, in.Day, in.Year, limit)

		msg := fmt.Sprintf("%d workflow runs found", len(records))
		if len(records) == 0 {
			if in.Season == "" && in.Day == 0 {
				msg = fmt.Sprintf("no workflow run history for %q — specify season/day/year or check logs/mcp/workflow_runs/", in.NPC)
			} else {
				msg = fmt.Sprintf("no workflow run history for %q on %s d%d y%d", in.NPC, in.Season, in.Day, in.Year)
			}
		}

		logToolCall("workflow_run_history", in)
		return nil, WorkflowRunHistoryOutput{
			OK:      true,
			Records: records,
			Message: msg,
		}, nil
	})
}
