package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/OmniStormX/SmartNPC/adapters/stardew/bridge"
	"github.com/OmniStormX/SmartNPC/adapters/stardew/scheduler"
	"github.com/OmniStormX/SmartNPC/pkg/workflow"
)

// ── npc_plan_day ──────────────────────────────────────────────────

// NpcPlanDayInputEntry is one slot in the day plan.
//
// Three mutually-exclusive forms are supported (priority order):
//
//  1. workflow_id — reference a built-in / overridden workflow (recommended).
//     Pass optional args to supply input values (e.g. target_seed).
//  2. workflow   — an inline workflow definition (ad-hoc, LLM-authored).
//  3. action     — legacy single-tool entry; auto-wrapped as a 1-step workflow.
//
// Time precision: 10 minutes. game_minute must be one of {0,10,20,30,40,50};
// invalid values are rounded down to the nearest 10. Defaults to 0 (the hour mark).
type NpcPlanDayInputEntry struct {
	GameHour   int    `json:"game_hour"             jsonschema:"6-25 (SDV 6am to 2am displayed as 26:00)"`
	GameMinute int    `json:"game_minute,omitempty" jsonschema:"minute within the hour, in 10-min steps: 0/10/20/30/40/50. Defaults to 0."`
	Reason     string `json:"reason,omitempty"      jsonschema:"brief reason for this activity (for debugging)"`

	// ── P3: three forms, pick one ─────────────────────────────────────
	// Form 1: built-in workflow reference (recommended).
	WorkflowID string         `json:"workflow_id,omitempty" jsonschema:"id of a built-in workflow from workflow_list (e.g. \"farm_morning_round\")"`
	Args       map[string]any `json:"args,omitempty"        jsonschema:"optional input values for the workflow (e.g. {\"target_seed\": \"(O)472\"})"`

	// Form 2: inline workflow definition (for ad-hoc / personalised use).
	// Accepted as any to avoid MCP jsonschema recursion on the workflow DSL types.
	Workflow any `json:"workflow,omitempty" jsonschema:"inline workflow definition — a JSON object with id + steps fields"`

	// Form 3: legacy single-tool entry.
	Action string `json:"action,omitempty" jsonschema:"[deprecated] single MCP tool name — use workflow_id instead for multi-step reliability"`
}

// NpcPlanDayInput is the input to npc_plan_day.
type NpcPlanDayInput struct {
	NPC     string                 `json:"npc"              jsonschema:"NPC internal name, or \"*\" to apply the same schedule to ALL agent NPCs"`
	Day     int                    `json:"day"              jsonschema:"game day 1-28"`
	Season  string                 `json:"season"           jsonschema:"spring/summer/fall/winter"`
	Year    int                    `json:"year,omitempty"   jsonschema:"game year (default 1)"`
	Entries []NpcPlanDayInputEntry `json:"entries"          jsonschema:"~30 scheduled activities for today (min 8, max 20), 10-minute precision via game_minute"`
}

// NpcPlanDayOutput acknowledges the schedule was stored.
type NpcPlanDayOutput struct {
	OK       bool   `json:"ok"                jsonschema:"true if schedule was stored"`
	NPC      string `json:"npc"               jsonschema:"echo"`
	Accepted int    `json:"accepted"          jsonschema:"number of entries accepted"`
	Message  string `json:"message,omitempty" jsonschema:"status"`
}

// ── npc_get_schedule ──────────────────────────────────────────────

// NpcGetScheduleInput queries an NPC's remaining schedule.
type NpcGetScheduleInput struct {
	NPC string `json:"npc" jsonschema:"NPC internal name"`
}

// NpcGetScheduleOutputEntry is one pending entry.
type NpcGetScheduleOutputEntry struct {
	GameHour   int    `json:"game_hour"             jsonschema:"scheduled hour"`
	GameMinute int    `json:"game_minute,omitempty" jsonschema:"scheduled minute within the hour (0/10/20/30/40/50)"`
	Action     string `json:"action,omitempty"      jsonschema:"legacy tool name (empty when workflow_id is set)"`
	Reason     string `json:"reason,omitempty"      jsonschema:"reasoning"`
	// ── P3 workflow fields ────────────────────────────────────────────
	WorkflowID string `json:"workflow_id,omitempty" jsonschema:"built-in workflow id, if set"`
}

// NpcGetScheduleOutput returns the pending (unfired) entries.
type NpcGetScheduleOutput struct {
	OK      bool                        `json:"ok"               jsonschema:"true if found"`
	NPC     string                      `json:"npc"              jsonschema:"echo"`
	Entries []NpcGetScheduleOutputEntry `json:"entries"          jsonschema:"pending schedule entries (not yet fired)"`
	Message string                      `json:"message,omitempty" jsonschema:"status"`
}

// ── registration ──────────────────────────────────────────────────

// registerNpcSchedule mounts schedule tools on the MCP server.
// sched is the shared Scheduler instance; if nil, a new one is created
// (useful for tests). The same scheduler must be wired into the event
// router so game_time_tick can call sched.Tick().
//
// workflowReg is the workflow registry for validating workflow_id references.
// When nil, workflow_id entries are rejected (registry not available in tests).
func registerNpcSchedule(s *mcp.Server, sched *scheduler.Scheduler, br *bridge.WSClient, workflowReg *workflow.Registry, debug bool) {
	if sched == nil {
		sched = scheduler.New()
	}

	mcp.AddTool(s, &mcp.Tool{
		Name: "npc_plan_day",
		Description: "Submit a daily schedule for this NPC. Called once per game day " +
			"(typically on day_started) after reviewing weather, season, memory, and " +
			"recent events.\n\n" +
				"**EVERY entry MUST use `workflow_id`.** The `action` field is REJECTED. " +
					"FIRST call `workflow_list` to discover available workflows, then reference them " +
				"by id. Pass optional `args` for inputs like target_seed.\n" +
			"\n" +
			"When to call: at the START of each new game day — before doing anything " +
			"else. This is your plan for the day. Aim for 10-16 workflow_id entries spread across " +
			"hours 6-25 (min 8, max 20).\n\n" +
			"Time precision: each entry has a `game_hour` (6-25) and an optional " +
			"`game_minute` (0/10/20/30/40/50, default 0). The mod fires due entries " +
			"on a 20-minute tick, so an entry at 06:30 may run any time between " +
			"06:30 and 06:40 — that's fine and intentional.\n\n" +
			"Execution: when the scheduled time arrives, the schedule fires. " +
			"For workflow_id entries, the workflow engine runs the steps automatically — inspect→execute→bubble, no per-step LLM calls.\n\n" +

			"Constraints:\n" +
			"- game_hour range: 6 (6am) to 25 (1am next day, SDV convention)\n" +
			"- game_minute must be a multiple of 10 (rounded down if not)\n" +
			"- max 20 entries per day (workflows are long-running)\n" +
			"- calling again replaces the previous schedule entirely\n" +
			"- `action` field is DISABLED and will return an error — use workflow_id ALWAYS\n" +
			"IMPORTANT: Call this EXACTLY ONCE per game day — when you receive a " +
			"day_started event. If you already planned today, do NOT call again. " +
			"Use npc_get_schedule to check your existing plan.\n\n" +
			"Tips for good schedules:\n" +
			"- Aim for 10-16 workflow_id entries: space them 40-90 min apart\n" +
			"- Quota: 3-4 farm_care, 1-2 farm_extension, 2-3 farm_cleanup, 2-3 resource_gather, 1-2 social_interact\n" +
			"- Focus on productive work (maintenance, harvest, clear debris, forage, deliver items). Minimize idle/wander.\n" +
			"- Adapt to weather: skip outdoor-only actions on rainy days, replace with indoor productive alternatives\n" +
			"- Count your entries before submitting. Each workflow bundles 3-6 tool calls. If fewer than 8, add more. If more than 20, reduce.\n\n" +
			"Side-effect: WRITE (stores schedule in memory, cleared daily).",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in NpcPlanDayInput) (*mcp.CallToolResult, NpcPlanDayOutput, error) {
		slog.Info("npc_plan_day called",
			"npc", in.NPC, "day", in.Day, "season", in.Season, "year", in.Year,
			"entry_count", len(in.Entries))

		if in.NPC == "" {
			return nil, NpcPlanDayOutput{}, errNpcRequired
		}
		if len(in.Entries) == 0 {
			return nil, NpcPlanDayOutput{}, fmt.Errorf("entries is required (at least 1 scheduled activity)")
		}
		if len(in.Entries) > 40 {
			in.Entries = in.Entries[:40]
		}
		if in.Year <= 0 {
			in.Year = 1
		}

		// Convert input entries to scheduler entries via normalizeEntry.
		entries := make([]scheduler.Entry, 0, len(in.Entries))
		var deprecatedActions []string
		var workflowIDCount, inlineWfCount int
		for _, e := range in.Entries {
			if e.GameHour < 6 || e.GameHour > 25 {
				continue // skip invalid hours
			}
			// Round game_minute down to a 10-minute boundary; clamp to [0,50].
			minute := e.GameMinute
			if minute < 0 {
				minute = 0
			}
			if minute > 50 {
				minute = 50
			}
			minute = (minute / 10) * 10

			se, err := normalizeEntry(workflowReg, e)
			if err != nil {
				slog.Warn("npc_plan_day: skipping invalid entry",
					"game_hour", e.GameHour, "game_minute", minute, "err", err)
				continue
			}
			se.GameHour = e.GameHour
			se.GameMinute = minute
			se.Reason = e.Reason
			entries = append(entries, se)
			if e.WorkflowID != "" { workflowIDCount++ }
			if e.Workflow != nil { inlineWfCount++ }

			if e.Action != "" && e.WorkflowID == "" && e.Workflow == nil {
				deprecatedActions = append(deprecatedActions, e.Action)
			}
		}

		slog.Debug("npc_plan_day entry breakdown",
			"total", len(entries),
			"workflow_id", workflowIDCount,
			"inline", inlineWfCount,
			"legacy_action", len(deprecatedActions))

		// Determine target NPCs: "*" means all agent-managed NPCs.
		var targets []string
		if in.NPC == "*" {
			targets = sched.AgentNPCs()
			if len(targets) == 0 {
				return nil, NpcPlanDayOutput{}, fmt.Errorf("npc=\"*\" but no agent NPCs registered")
			}
		} else {
			targets = []string{in.NPC}
		}

		duplicates := make([]string, 0, len(targets))
		for _, npc := range targets {
			if sched.AlreadyPlannedToday(npc, in.Day, in.Season) {
				duplicates = append(duplicates, npc)
				slog.Warn("npc_plan_day: duplicate call for same day",
					"npc", npc, "day", in.Day, "season", in.Season)
			}
			sched.SetSchedule(scheduler.DaySchedule{
				NPC:     npc,
				Day:     in.Day,
				Season:  in.Season,
				Year:    in.Year,
				Entries: entries,
			})
			logSchedule(npc, in.Day, in.Season, in.Year, entries)
		}

		logToolCall("npc_plan_day", in)

		msg := fmt.Sprintf("schedule stored: %d entries for %d NPC(s), day %d %s", len(entries), len(targets), in.Day, in.Season)
		if len(entries) < 24 {
			msg += fmt.Sprintf("\n\n⚠️ WARNING: Only %d entries submitted. The schedule should have ~30 entries (min 24). "+
				"Consider calling npc_plan_day again with a fuller schedule.", len(entries))
		}
		if len(duplicates) > 0 {
			msg += fmt.Sprintf("\n\n⚠️ WARNING: You already submitted a schedule today for: %s. "+
				"The old plan has been replaced with this new one. "+
				"Do NOT call npc_plan_day again today — use npc_get_schedule to check your existing plan.",
				strings.Join(duplicates, ", "))
		}
		if len(deprecatedActions) > 0 {
			msg += fmt.Sprintf("\n\n💡 Tip: %d entries used the legacy `action` field (%s). "+
				"Consider using `workflow_id` instead — call workflow_list to see available workflows.",
				len(deprecatedActions), strings.Join(uniqueStr(deprecatedActions, 3), ", "))
		}
		out := NpcPlanDayOutput{
			OK:       true,
			NPC:      in.NPC,
			Accepted: len(entries),
			Message:  msg,
		}
		slog.Info("npc_plan_day result",
			"npc", out.NPC, "accepted", out.Accepted, "targets", len(targets), "message", out.Message)

		// Debug mode: push concise summary to each target NPC's chat panel + head bubble.
		if debug && br != nil {
			for _, npc := range targets {
				msg := fmt.Sprintf("[plan] stored %d entries, day %d %s", len(entries), in.Day, in.Season)
				go br.CallAs(context.Background(), npc, bridge.ActionChatSay, map[string]any{
					"npc":  npc,
					"text": msg,
				})
				go br.CallAs(context.Background(), npc, bridge.ActionNpcShowTextBubble, map[string]any{
					"npc":  npc,
					"text": msg,
				})
			}
		}

		return nil, out, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "npc_get_schedule",
		Description: "Query your remaining schedule for today — returns entries that " +
			"have NOT yet been triggered. Use to recall what you planned to do later.\n\n" +
			"When to call: when you need to reference your day plan — e.g. player asks " +
			"\"what are you doing today?\" or you want to check if something is coming up.\n\n" +
			"Side-effect: READ.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in NpcGetScheduleInput) (*mcp.CallToolResult, NpcGetScheduleOutput, error) {
		if in.NPC == "" {
			return nil, NpcGetScheduleOutput{}, errNpcRequired
		}
		logToolCall("npc_get_schedule", in)

		pending := sched.PendingEntries(in.NPC)
		outEntries := make([]NpcGetScheduleOutputEntry, 0, len(pending))
		for _, e := range pending {
			outEntries = append(outEntries, NpcGetScheduleOutputEntry{
				GameHour:   e.GameHour,
				GameMinute: e.GameMinute,
				Action:     e.Action,
				Reason:     e.Reason,
				WorkflowID: e.WorkflowID,
			})
		}

		msg := fmt.Sprintf("%d pending entries", len(outEntries))
		if len(outEntries) == 0 {
			msg = "no schedule set or all entries already fired"
		}

		return nil, NpcGetScheduleOutput{
			OK:      true,
			NPC:     in.NPC,
			Entries: outEntries,
			Message: msg,
		}, nil
	})
}

// ── normalizeEntry ────────────────────────────────────────────────────────

// normalizeEntry converts an NpcPlanDayInputEntry into a scheduler.Entry,
// resolving the three mutually-exclusive forms (workflow_id / workflow / action)
// into a consistent internal shape.
//
// Priority: workflow_id > workflow > action. When action is the only field
// set, it is auto-wrapped as a 1-step __legacy_action_wrapper__ workflow.
func normalizeEntry(reg *workflow.Registry, in NpcPlanDayInputEntry) (scheduler.Entry, error) {
	e := scheduler.Entry{
		Args: in.Args,
	}

	switch {
	case in.WorkflowID != "":
		if reg == nil {
			return e, errors.New("workflow_id requires a workflow registry")
		}
		if reg.Get(in.WorkflowID) == nil {
			return e, fmt.Errorf("unknown workflow_id %q — use workflow_list to see available IDs", in.WorkflowID)
		}
		e.WorkflowID = in.WorkflowID

	case in.Workflow != nil:
		// JSON-round-trip the any value into a workflow.Definition.
		raw, err := json.Marshal(in.Workflow)
		if err != nil {
			return e, fmt.Errorf("inline workflow: marshal: %w", err)
		}
		var def workflow.Definition
		if err := json.Unmarshal(raw, &def); err != nil {
			return e, fmt.Errorf("inline workflow: invalid JSON: %w", err)
		}
		if err := workflow.Validate(&def); err != nil {
			return e, fmt.Errorf("inline workflow: %w", err)
		}
		e.Workflow = &def

	case in.Action != "":
		// Legacy single-tool entries are REJECTED. The LLM MUST use
		// workflow_id from workflow_list instead. Each workflow bundles
		// 3-8 tool calls with inspect→execute→bubble chains.
		return e, fmt.Errorf(
			"`action` field is DISABLED — use `workflow_id` instead. "+
				"Call workflow_list to see available workflows, then reference them by id. "+
				"Legacy action %q is not accepted", in.Action)

	default:
		return e, errors.New("entry must specify workflow_id or workflow (inline definition). Call workflow_list to discover available workflows")
	}

	return e, nil
}

// uniqueStr returns up to n unique strings from ss.
func uniqueStr(ss []string, n int) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range ss {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
		if len(out) >= n {
			break
		}
	}
	return out
}
