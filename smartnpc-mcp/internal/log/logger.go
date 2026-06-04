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
	// AddSource: true makes slog attach a "source" group with file/line/function
	// to every record, e.g. "source":{"function":"main.run","file":".../main.go","line":120}.
	// Cost is one runtime.Callers per log call — negligible for our volume,
	// and invaluable when grepping logs to locate where a message came from.
	h := slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level:     lvl,
		AddSource: true,
	})
	return slog.New(h)
}
