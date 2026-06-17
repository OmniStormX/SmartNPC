package tools

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/OmniStormX/SmartNPC/pkg/workflow"
)

// LogWorkflowRun writes one JSONL record to
// <logDir>/mcp/workflow_runs/<npc>-<season>-d<day>-y<year>.jsonl.
//
// Safe for concurrent use (per-file mutex). When the file is not yet open,
// creates the directory and file. No-op when the result is nil.
func LogWorkflowRun(
	npc string,
	season string,
	day, year int,
	res *workflow.RunResult,
	err error,
	duration time.Duration,
	args map[string]any,
) {
	if res == nil {
		return
	}

	dir := filepath.Join(logDir(), "mcp", "workflow_runs")
	if mkdirErr := os.MkdirAll(dir, 0755); mkdirErr != nil {
		slog.Default().Warn("workflow_runs: mkdir failed", "dir", dir, "err", mkdirErr)
		return
	}

	filename := fmt.Sprintf("%s-%s-d%d-y%d.jsonl", npc, season, day, year)
	path := filepath.Join(dir, filename)

	// Per-file mutex so concurrent runs for different NPCs or days don't block.
	fileMu := getWorkflowRunFileMu(path)
	fileMu.Lock()
	defer fileMu.Unlock()

	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		slog.Default().Warn("workflow_runs: open failed", "path", path, "err", err)
		return
	}
	defer f.Close()

	record := map[string]any{
		"ts":             time.Now().UTC().Format(time.RFC3339),
		"npc":            npc,
		"workflow_id":    res.WorkflowID,
		"step_count":     res.StepCount,
		"tool_calls":     res.ToolCalls,
		"nothing_to_do":  res.NothingToDoCt,
		"stopped":        res.Stopped,
		"stop_reason":    res.StopReason,
		"failed_step":    res.FailedStep,
		"duration_ms":    duration.Milliseconds(),
	}
	if err != nil {
		record["error"] = err.Error()
	}
	if args != nil {
		record["args"] = args
	}

	b, marshalErr := json.Marshal(record)
	if marshalErr != nil {
		slog.Default().Warn("workflow_runs: marshal failed", "err", marshalErr)
		return
	}
	b = append(b, '\n')
	if _, writeErr := f.Write(b); writeErr != nil {
		slog.Default().Warn("workflow_runs: write failed", "path", path, "err", writeErr)
	}
	_ = f.Sync()
}

// ── per-file mutex registry ──────────────────────────────────────────────

var (
	workflowRunMu   sync.Mutex
	workflowRunFiles = map[string]*sync.Mutex{}
)

func getWorkflowRunFileMu(path string) *sync.Mutex {
	workflowRunMu.Lock()
	defer workflowRunMu.Unlock()
	if mu, ok := workflowRunFiles[path]; ok {
		return mu
	}
	mu := &sync.Mutex{}
	workflowRunFiles[path] = mu
	return mu
}

// ── read helpers for the workflow_run_history tool ────────────────────────

// WorkflowRunRecord is one line from the jsonl file.
type WorkflowRunRecord struct {
	TS          string         `json:"ts"`
	NPC         string         `json:"npc"`
	WorkflowID  string         `json:"workflow_id"`
	StepCount   int            `json:"step_count"`
	ToolCalls   int            `json:"tool_calls"`
	NothingToDo int            `json:"nothing_to_do"`
	Stopped     bool           `json:"stopped"`
	StopReason  string         `json:"stop_reason,omitempty"`
	FailedStep  int            `json:"failed_step,omitempty"`
	DurationMS  int64          `json:"duration_ms"`
	Error       string         `json:"error,omitempty"`
	Args        map[string]any `json:"args,omitempty"`
}

// ReadWorkflowRunHistory reads up to `limit` most recent records for the
// given NPC on a specific game day. When limit <= 0, defaults to 20.
// Returns records newest-first. Returns empty slice on any IO error.
func ReadWorkflowRunHistory(npc, season string, day, year, limit int) []WorkflowRunRecord {
	if limit <= 0 {
		limit = 20
	}
	dir := filepath.Join(logDir(), "mcp", "workflow_runs")
	filename := fmt.Sprintf("%s-%s-d%d-y%d.jsonl", npc, season, day, year)
	path := filepath.Join(dir, filename)

	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	dec := json.NewDecoder(f)
	var all []WorkflowRunRecord
	for dec.More() {
		var r WorkflowRunRecord
		if err := dec.Decode(&r); err != nil {
			break
		}
		all = append(all, r)
	}

	// Reverse so newest-first.
	for i, j := 0, len(all)-1; i < j; i, j = i+1, j-1 {
		all[i], all[j] = all[j], all[i]
	}

	if len(all) > limit {
		all = all[:limit]
	}
	return all
}

// ListWorkflowRunDays returns the available (season, day, year) tuples
// for which workflow run records exist for the given NPC.
func ListWorkflowRunDays(npc string) [][3]any {
	dir := filepath.Join(logDir(), "mcp", "workflow_runs")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var prefix = npc + "-"
	var out [][3]any
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if len(name) > len(prefix) && name[:len(prefix)] == prefix && filepath.Ext(name) == ".jsonl" {
			// Parse "<npc>-<season>-d<day>-y<year>.jsonl"
			core := name[len(prefix) : len(name)-6] // trim prefix and .jsonl
			var season string
			var day, year int
			if n, _ := fmt.Sscanf(core, "%s-d%d-y%d", &season, &day, &year); n == 3 {
				out = append(out, [3]any{season, day, year})
			}
		}
	}
	return out
}
