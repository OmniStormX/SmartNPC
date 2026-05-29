package tools

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/OmniStormX/SmartNPC/adapters/stardew/scheduler"
)

// ── npc_plan_day ──────────────────────────────────────────────────

// NpcPlanDayInputEntry is one slot in the day plan.
type NpcPlanDayInputEntry struct {
	GameHour int            `json:"game_hour"         jsonschema:"6-25 (SDV 6am to 2am displayed as 26:00)"`
	Action   string         `json:"action"            jsonschema:"MCP tool name to invoke at this hour, e.g. npc_water_crops"`
	Params   map[string]any `json:"params,omitempty"  jsonschema:"action-specific parameters object"`
	Reason   string         `json:"reason,omitempty"  jsonschema:"brief reason for this activity (for debugging)"`
}

// NpcPlanDayInput is the input to npc_plan_day.
type NpcPlanDayInput struct {
	NPC     string                 `json:"npc"              jsonschema:"NPC internal name, or \"*\" to apply the same schedule to ALL agent NPCs"`
	Day     int                    `json:"day"              jsonschema:"game day 1-28"`
	Season  string                 `json:"season"           jsonschema:"spring/summer/fall/winter"`
	Year    int                    `json:"year,omitempty"   jsonschema:"game year (default 1)"`
	Entries []NpcPlanDayInputEntry `json:"entries"          jsonschema:"list of scheduled activities for today (max 20 entries)"`
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
	GameHour int            `json:"game_hour"        jsonschema:"scheduled hour"`
	Action   string         `json:"action"           jsonschema:"tool to invoke"`
	Params   map[string]any `json:"params,omitempty" jsonschema:"parameters"`
	Reason   string         `json:"reason,omitempty" jsonschema:"reasoning"`
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
func registerNpcSchedule(s *mcp.Server, sched *scheduler.Scheduler) {
	if sched == nil {
		sched = scheduler.New()
	}

	mcp.AddTool(s, &mcp.Tool{
		Name: "npc_plan_day",
		Description: "Submit a daily schedule for this NPC. Called once per game day " +
			"(typically on day_started) after reviewing weather, season, memory, and " +
			"recent events. The schedule is a list of (game_hour, action) entries.\n\n" +
			"When to call: at the START of each new game day — before doing anything " +
			"else. This is your plan for the day. You can have 1-20 entries spread " +
			"across hours 6-25.\n\n" +
			"Execution: when the scheduled hour arrives, the system dispatches the " +
			"action automatically — it will NOT wake you again to confirm. In debug " +
			"mode (--log-level debug), actions are displayed in the game chat panel " +
			"as '[schedule] action params' instead of being executed.\n\n" +
			"Constraints:\n" +
			"- game_hour range: 6 (6am) to 25 (1am next day, SDV convention)\n" +
			"- action must be a valid MCP tool name from your available tools\n" +
			"- max 20 entries per day; duplicate hours are allowed (both fire)\n" +
			"- params is the exact JSON you'd pass to that tool (minus the `npc` field)\n" +
			"- calling again replaces the previous schedule entirely\n\n" +
			"Tips for good schedules:\n" +
			"- Space entries 2-3 hours apart to feel natural\n" +
			"- Include at least one social action (approach_and_speak, express_emotion)\n" +
			"- Adapt to weather: skip water_crops on rainy days\n" +
			"- Leave gaps — you'll react to events in real-time too\n\n" +
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
		if len(in.Entries) > 20 {
			in.Entries = in.Entries[:20]
		}
		if in.Year <= 0 {
			in.Year = 1
		}

		// Convert input entries to scheduler entries.
		entries := make([]scheduler.Entry, 0, len(in.Entries))
		for _, e := range in.Entries {
			if e.GameHour < 6 || e.GameHour > 25 {
				continue // skip invalid hours
			}
			if e.Action == "" {
				continue // skip empty actions
			}
			entries = append(entries, scheduler.Entry{
				GameHour: e.GameHour,
				Action:   e.Action,
				Params:   e.Params,
				Reason:   e.Reason,
			})
		}

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

		for _, npc := range targets {
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

		out := NpcPlanDayOutput{
			OK:       true,
			NPC:      in.NPC,
			Accepted: len(entries),
			Message:  fmt.Sprintf("schedule stored: %d entries for %d NPC(s), day %d %s", len(entries), len(targets), in.Day, in.Season),
		}
		slog.Info("npc_plan_day result",
			"npc", out.NPC, "accepted", out.Accepted, "targets", len(targets), "message", out.Message)

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
				GameHour: e.GameHour,
				Action:   e.Action,
				Params:   e.Params,
				Reason:   e.Reason,
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
