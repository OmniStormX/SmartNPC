package tools

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/OmniStormX/SmartNPC/adapters/stardew/scheduler"
)

// logDir resolves the directory for npc_actions.log and schedule.log.
// Priority: $SMARTNPC_LOG_DIR > ./logs/ (relative to exe) > CWD.
func logDir() string {
	if d := os.Getenv("SMARTNPC_LOG_DIR"); d != "" {
		return d
	}
	// Try to use the executable's parent directory + logs/
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Join(filepath.Dir(exe), "..", "logs")
		if abs, err := filepath.Abs(dir); err == nil {
			if err := os.MkdirAll(abs, 0755); err != nil {
				slog.Default().Warn("failed to create log dir", "path", abs, "err", err)
			} else {
				slog.Default().Info("log directory created", "path", abs)
			}
			return abs
		}
	}
	return "."
}

// actionLogger is a dedicated logger for NPC tool calls.
var (
	actionLogOnce sync.Once
	actionLog     *slog.Logger
	resolvedDir   string
)

func getActionLogger() *slog.Logger {
	actionLogOnce.Do(func() {
		resolvedDir = logDir()
		path := filepath.Join(resolvedDir, "npc_actions.log")
		f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			actionLog = slog.Default()
			actionLog.Warn("failed to open npc_actions.log, falling back to default logger", "path", path, "err", err)
			return
		}
		actionLog = slog.New(slog.NewJSONHandler(f, &slog.HandlerOptions{Level: slog.LevelInfo}))
		slog.Default().Info("npc_actions.log opened", "path", path)
	})
	return actionLog
}

// logToolCall logs a tool invocation to npc_actions.log with the tool name
// and its raw input parameters (JSON-serialized).
func logToolCall(tool string, params any) {
	log := getActionLogger()
	raw, _ := json.Marshal(params)
	log.Info("tool_call", "tool", tool, "params", string(raw))
}

// ── schedule.log ──────────────────────────────────────────────────

var (
	schedLogOnce sync.Once
	schedLogFile *os.File

	schedTriggerLogOnce sync.Once
	schedTriggerLogFile *os.File
)

func getScheduleLogFile() *os.File {
	schedLogOnce.Do(func() {
		dir := logDir()
		path := filepath.Join(dir, "schedule.log")
		f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			slog.Default().Warn("failed to open schedule.log", "path", path, "err", err)
			return
		}
		schedLogFile = f
		slog.Default().Info("schedule.log opened", "path", path)
	})
	return schedLogFile
}

// getScheduleTriggerLogFile returns the file handle for
// <logDir>/mcp/schedule_triggers.log, used to record which schedule
// entries fire on each game_time_tick. Separate from schedule.log
// (which records plan_day output) so they can be reasoned about
// independently. Lazily opened on first use.
func getScheduleTriggerLogFile() *os.File {
	schedTriggerLogOnce.Do(func() {
		dir := filepath.Join(logDir(), "mcp")
		if err := os.MkdirAll(dir, 0755); err != nil {
			slog.Default().Warn("failed to mkdir logs/mcp", "dir", dir, "err", err)
			return
		}
		path := filepath.Join(dir, "schedule_triggers.log")
		f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			slog.Default().Warn("failed to open schedule_triggers.log", "path", path, "err", err)
			return
		}
		schedTriggerLogFile = f
		slog.Default().Info("schedule_triggers.log opened", "path", path)
	})
	return schedTriggerLogFile
}

// LogScheduleTriggers appends one record per scheduler tick to
// <logDir>/mcp/schedule_triggers.log. The format is a header line plus
// one tab-separated row per fired entry, e.g.:
//
//	[2026-06-03 14:05:12] hour=10:00 fired=2
//	  1  Harvey      10:00  npc_idle_activity              Morning clinic hours
//	  2  Abigail     10:00  npc_idle_activity              Practicing flute by the river
//
// No-op when there are no fired entries. Safe to call from the event
// hot path; uses a single lazy file handle and best-effort fsync.
func LogScheduleTriggers(hour int, fired []scheduler.FiredEntry) {
	if len(fired) == 0 {
		return
	}
	f := getScheduleTriggerLogFile()
	if f == nil {
		return
	}
	var b []byte
	b = fmt.Appendf(b, "[Schedule] [%s] hour=%02d:00 fired=%d\n",
		time.Now().Format("2006-01-02 15:04:05"), hour, len(fired))
	for i, e := range fired {
		action := e.Action
		if e.WorkflowID != "" {
			action = "W:" + e.WorkflowID
		}
		b = fmt.Appendf(b, "  %2d  %-12s %02d:%02d  %-30s  %s\n",
			i+1, e.NPC, e.GameHour, e.GameMinute, action, e.Reason)
	}
	if _, err := f.Write(b); err != nil {
		slog.Default().Warn("schedule_triggers.log write failed", "err", err)
		return
	}
	_ = f.Sync()
}

// logSchedule writes a human-readable schedule summary to schedule.log
// and also mirrors it to stderr in green for at-a-glance console feedback.
func logSchedule(npc string, day int, season string, year int, entries []scheduler.Entry) {
	header := fmt.Sprintf("\n==============================================\n"+
		"NPC: %s | Day %d %s Year %d | Entries: %d\n"+
		"==============================================\n",
		npc, day, season, year, len(entries))

	lines := make([]string, 0, len(entries))
	for i, e := range entries {
		action := e.Action
		if e.WorkflowID != "" {
			action = "W:" + e.WorkflowID
		}
		lines = append(lines, fmt.Sprintf("  [%2d] %02d:%02d  %-28s  %s\n", i+1, e.GameHour, e.GameMinute, action, e.Reason))
	}
	footer := "----------------------------------------------\n"

	// File output (plain).
	if f := getScheduleLogFile(); f != nil {
		f.WriteString(header)
		for _, l := range lines {
			f.WriteString(l)
		}
		f.WriteString(footer)
		f.Sync()
	}

	// Console mirror (green, stderr to keep MCP stdout clean).
	const green, reset = "\033[32m", "\033[0m"
	fmt.Fprint(os.Stderr, green, header)
	for _, l := range lines {
		fmt.Fprint(os.Stderr, l)
	}
	fmt.Fprint(os.Stderr, footer, reset)
}

// ── events.log (logs/mcp/events.log) ────────────────────────────────

var (
	eventLogOnce sync.Once
	eventLogFile *os.File
)

func getEventLogFile() *os.File {
	eventLogOnce.Do(func() {
		dir := filepath.Join(logDir(), "mcp")
		if err := os.MkdirAll(dir, 0755); err != nil {
			slog.Default().Warn("failed to mkdir logs/mcp", "dir", dir, "err", err)
			return
		}
		path := filepath.Join(dir, "events.log")
		f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			slog.Default().Warn("failed to open events.log", "path", path, "err", err)
			return
		}
		eventLogFile = f
		slog.Default().Info("events.log opened", "path", path)
	})
	return eventLogFile
}

// LogEvent appends a single line to <logDir>/mcp/events.log each time the
// bridge receives an event from the SMAPI mod. Format:
//
//	[Event] 2026-06-03 10:05:12 game_time_tick {"hour":10}
//
// Safe for concurrent use (single lazy file handle, atomic write per call).
func LogEvent(name string, data json.RawMessage) {
	f := getEventLogFile()
	if f == nil {
		return
	}
	line := fmt.Appendf(nil, "[Event] %s %s %s\n",
		time.Now().Format("2006-01-02 15:04:05"), name, string(data))
	_, _ = f.Write(line)
}

// ── session.log (logs/mcp/session.log) ────────────────────────────────

var (
	sessionLogOnce sync.Once
	sessionLogFile *os.File
)

func getSessionLogFile() *os.File {
	sessionLogOnce.Do(func() {
		dir := filepath.Join(logDir(), "mcp")
		if err := os.MkdirAll(dir, 0755); err != nil {
			slog.Default().Warn("failed to mkdir logs/mcp", "dir", dir, "err", err)
			return
		}
		path := filepath.Join(dir, "session.log")
		f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			slog.Default().Warn("failed to open session.log", "path", path, "err", err)
			return
		}
		sessionLogFile = f
		slog.Default().Info("session.log opened", "path", path)
	})
	return sessionLogFile
}

// LogSessionForward appends a single line to <logDir>/mcp/session.log each
// time an event is forwarded to a connected MCP session. Format:
//
//	[Session] 2026-06-03 10:05:12 chat_message → {"npc":"XiaMi","text":"Hi"}
//
// Safe for concurrent use (single lazy file handle, atomic write per call).
// If the payload is not valid JSON it is written as a raw string.
func LogSessionForward(name string, payload json.RawMessage) {
	f := getSessionLogFile()
	if f == nil {
		return
	}
	data := string(payload)
	if !json.Valid(payload) {
		data = fmt.Sprintf("%q", data)
	}
	line := fmt.Appendf(nil, "[Session] %s %s → %s\n",
		time.Now().Format("2006-01-02 15:04:05"), name, data)
	_, _ = f.Write(line)
}

