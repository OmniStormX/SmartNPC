package tools

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/OmniStormX/SmartNPC/adapters/stardew/bridge"
	"github.com/OmniStormX/SmartNPC/adapters/stardew/scheduler"
)

// ── npc_plan_day ──────────────────────────────────────────────────

// NpcPlanDayInputEntry is one slot in the day plan.
//
// Intentionally NO params field: the schedule only commits to the time
// and which tool to invoke. Concrete tool parameters are decided when
// the entry fires (see schedule_trigger handling), so the LLM can react
// to live game state — weather, player location, inventory, etc. — that
// would not be known at plan time.
//
// Time precision: 10 minutes. game_minute must be one of {0,10,20,30,40,50};
// invalid values are rounded down to the nearest 10. Defaults to 0 (the hour mark).
type NpcPlanDayInputEntry struct {
	GameHour   int    `json:"game_hour"             jsonschema:"6-25 (SDV 6am to 2am displayed as 26:00)"`
	GameMinute int    `json:"game_minute,omitempty" jsonschema:"minute within the hour, in 10-min steps: 0/10/20/30/40/50. Defaults to 0."`
	Action     string `json:"action"                jsonschema:"MCP tool name to invoke at this time, e.g. npc_water_crops. Do NOT include parameters here; choose them when the entry fires."`
	Reason     string `json:"reason,omitempty"      jsonschema:"brief reason for this activity (for debugging)"`
}

// NpcPlanDayInput is the input to npc_plan_day.
type NpcPlanDayInput struct {
	NPC     string                 `json:"npc"              jsonschema:"NPC internal name, or \"*\" to apply the same schedule to ALL agent NPCs"`
	Day     int                    `json:"day"              jsonschema:"game day 1-28"`
	Season  string                 `json:"season"           jsonschema:"spring/summer/fall/winter"`
	Year    int                    `json:"year,omitempty"   jsonschema:"game year (default 1)"`
	Entries []NpcPlanDayInputEntry `json:"entries"          jsonschema:"~30 scheduled activities for today (min 24, max 40), 10-minute precision via game_minute"`
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
	Action     string `json:"action"                jsonschema:"tool to invoke"`
	Reason     string `json:"reason,omitempty"      jsonschema:"reasoning"`
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
func registerNpcSchedule(s *mcp.Server, sched *scheduler.Scheduler, br *bridge.WSClient, debug bool) {
	if sched == nil {
		sched = scheduler.New()
	}

	mcp.AddTool(s, &mcp.Tool{
		Name: "npc_plan_day",
		Description: "Submit a daily schedule for this NPC. Called once per game day " +
			"(typically on day_started) after reviewing weather, season, memory, and " +
			"recent events. The schedule is a list of (game_hour, game_minute, action) " +
			"entries — only the TIME and the TOOL NAME are committed; parameters are " +
			"decided later when the action fires.\n\n" +
			"When to call: at the START of each new game day — before doing anything " +
			"else. This is your plan for the day. Aim for ~30 entries spread across " +
			"hours 6-25 (min 24, max 40). Never submit fewer than 24.\n\n" +
			"Time precision: each entry has a `game_hour` (6-25) and an optional " +
			"`game_minute` (0/10/20/30/40/50, default 0). The mod fires due entries " +
			"on a 20-minute tick, so an entry at 06:30 may run any time between " +
			"06:30 and 06:40 — that's fine and intentional.\n\n" +
			"Execution: when the scheduled time arrives, you (the NPC's LLM) will be " +
			"woken with a `schedule_trigger` event carrying the action name and your " +
			"original reason. You then call the tool with concrete parameters chosen " +
			"based on live game state (location, inventory, weather, who's nearby, " +
			"etc.). In debug mode (--log-level debug), the action name is just shown " +
			"in the game chat panel as '[schedule] action — reason'.\n\n" +
			"Constraints:\n" +
			"- game_hour range: 6 (6am) to 25 (1am next day, SDV convention)\n" +
			"- game_minute must be a multiple of 10 (rounded down if not)\n" +
			"- action must be a valid MCP tool name from your available tools\n" +
			"- max 40 entries per day; duplicate times are allowed (both fire)\n" +
			"- DO NOT pass tool parameters here — choose them at fire time\n" +
			"- calling again replaces the previous schedule entirely\n\n" +
			"IMPORTANT: Call this EXACTLY ONCE per game day — when you receive a " +
			"day_started event. If you already planned today, do NOT call again. " +
			"Use npc_get_schedule to check your existing plan.\n\n" +
			"Tips for good schedules:\n" +
			"- Aim for ~30 entries: roughly one every 20-40 minutes through the active day\n" +
			"- Distribution: 14+ farm work, 5-7 resource gathering, 4-5 inventory/delivery, 3-4 social\n" +
			"- Focus on productive work (maintenance, harvest, clear debris, forage, deliver items). Minimize idle/wander.\n" +
			"- Adapt to weather: skip outdoor-only actions on rainy days, replace with indoor productive alternatives\n" +
			"- Count your entries before submitting. If fewer than 24, add more work.\n\n" +
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

		// Convert input entries to scheduler entries.
		entries := make([]scheduler.Entry, 0, len(in.Entries))
		for _, e := range in.Entries {
			if e.GameHour < 6 || e.GameHour > 25 {
				continue // skip invalid hours
			}
			if e.Action == "" {
				continue // skip empty actions
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
			entries = append(entries, scheduler.Entry{
				GameHour:   e.GameHour,
				GameMinute: minute,
				Action:     e.Action,
				Reason:     e.Reason,
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
