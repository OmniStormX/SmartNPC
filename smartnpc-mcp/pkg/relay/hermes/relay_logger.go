package hermesrelay

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ── relay.log (logs/mcp/relay.log) ──────────────────────────────────

var (
	relayLogOnce sync.Once
	relayLogFile *os.File
)

// relayLogDir resolves the log directory. Uses the same convention as
// adapters/stardew/tools: $SMARTNPC_LOG_DIR > ./logs/ (relative to exe).
func relayLogDir() string {
	if d := os.Getenv("SMARTNPC_LOG_DIR"); d != "" {
		return d
	}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Join(filepath.Dir(exe), "..", "logs")
		if abs, err := filepath.Abs(dir); err == nil {
			_ = os.MkdirAll(abs, 0755)
			return abs
		}
	}
	return "."
}

func getRelayLogFile() *os.File {
	relayLogOnce.Do(func() {
		dir := filepath.Join(relayLogDir(), "mcp")
		if err := os.MkdirAll(dir, 0755); err != nil {
			slog.Default().Warn("failed to mkdir logs/mcp", "dir", dir, "err", err)
			return
		}
		path := filepath.Join(dir, "relay.log")
		f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			slog.Default().Warn("failed to open relay.log", "path", path, "err", err)
			return
		}
		relayLogFile = f
		slog.Default().Info("relay.log opened", "path", path)
	})
	return relayLogFile
}

// LogRelayRequest records the formatted prompt sent to an NPC's Hermes Gateway.
// Format:
//
//	[RELAY → XiaMi] 2026-06-03 06:00:01 day_started input_len=1534 persona_len=8921
//
// Safe for concurrent use.
func LogRelayRequest(npc, eventName string, inputLen, personaLen int) {
	f := getRelayLogFile()
	if f == nil {
		return
	}
	line := fmt.Appendf(nil, "[RELAY → %s] %s %s input_len=%d persona_len=%d\n",
		npc, time.Now().Format("2006-01-02 15:04:05"), eventName, inputLen, personaLen)
	_, _ = f.Write(line)
}

// LogRelayResponse records the response received from an NPC's Hermes Gateway.
// Format:
//
//	[RELAY ← XiaMi] 2026-06-03 06:00:05 day_started status=200 elapsed=3.2s tokens(in=500,cached=480,out=120)
//
// Safe for concurrent use.
func LogRelayResponse(npc, eventName string, status int, elapsed time.Duration, inputTokens, cachedTokens, outputTokens int) {
	f := getRelayLogFile()
	if f == nil {
		return
	}
	cacheRatio := 0.0
	if inputTokens > 0 {
		cacheRatio = float64(cachedTokens) / float64(inputTokens)
	}
	line := fmt.Appendf(nil, "[RELAY ← %s] %s %s status=%d elapsed=%.1fs tokens(in=%d,cached=%d,out=%d,cache=%.0f%%)\n",
		npc, time.Now().Format("2006-01-02 15:04:05"), eventName,
		status, elapsed.Seconds(), inputTokens, cachedTokens, outputTokens, cacheRatio*100)
	_, _ = f.Write(line)
}

// LogRelayError records a failed relay attempt.
// Format:
//
//	[RELAY ✗ XiaMi] 2026-06-03 06:00:01 day_started status=502 elapsed=5.0s error="bad gateway"
//
// Safe for concurrent use.
func LogRelayError(npc, eventName string, status int, elapsed time.Duration, errMsg string) {
	f := getRelayLogFile()
	if f == nil {
		return
	}
	line := fmt.Appendf(nil, "[RELAY ✗ %s] %s %s status=%d elapsed=%.1fs error=%s\n",
		npc, time.Now().Format("2006-01-02 15:04:05"), eventName,
		status, elapsed.Seconds(), errMsg)
	_, _ = f.Write(line)
}
