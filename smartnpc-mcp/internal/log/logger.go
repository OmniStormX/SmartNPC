// Package log centralizes structured logging configuration.
//
// All logs MUST go to stderr. The MCP stdio transport reserves stdout for the
// JSON-RPC stream; writing logs there will corrupt the protocol.
package log

import (
	"log/slog"
	"os"
	"strings"
)

// New returns a slog.Logger that writes JSON to stderr.
func New(level string) *slog.Logger {
	var lvl slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn", "warning":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	h := slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: lvl})
	return slog.New(h)
}
