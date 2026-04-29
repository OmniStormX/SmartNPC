// Package log centralizes structured logging for the agent.
package log

import (
	"log/slog"
	"os"
	"strings"
)

// New returns a slog.Logger that writes JSON to stderr.
//
// The agent itself doesn't speak MCP on stdout, so it could log to stdout in
// principle — but using stderr keeps logs clearly separated from any future
// human-readable output.
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
	return slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: lvl}))
}
