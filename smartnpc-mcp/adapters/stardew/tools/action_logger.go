package tools

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

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
			os.MkdirAll(abs, 0755)
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

// logSchedule writes a human-readable schedule summary to schedule.log.
func logSchedule(npc string, day int, season string, year int, entries []scheduler.Entry) {
	f := getScheduleLogFile()
	if f == nil {
		return
	}

	header := fmt.Sprintf("\n══════════════════════════════════════════════\n"+
		"NPC: %s | Day %d %s Year %d | Entries: %d\n"+
		"══════════════════════════════════════════════\n",
		npc, day, season, year, len(entries))
	f.WriteString(header)

	for i, e := range entries {
		line := fmt.Sprintf("  [%2d] %02d:00  %-28s  %s\n", i+1, e.GameHour, e.Action, e.Reason)
		f.WriteString(line)
		if len(e.Params) > 0 {
			if raw, err := json.Marshal(e.Params); err == nil {
				fmt.Fprintf(f, "       params: %s\n", string(raw))
			}
		}
	}
	f.WriteString("──────────────────────────────────────────────\n")
	f.Sync()
}
