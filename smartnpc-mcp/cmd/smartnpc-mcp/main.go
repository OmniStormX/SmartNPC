// Command smartnpc-mcp is the MCP server bridging the Stardew Valley SMAPI mod
// to MCP clients (e.g., the smartnpc-agent or Claude Desktop).
//
// Transport: stdio (newline-delimited JSON-RPC over stdin/stdout).
// IMPORTANT: never write logs to stdout — it would corrupt the MCP stream.
// All logging must go through stderr (slog handler is wired accordingly).
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/smartnpc/smartnpc-mcp/internal/log"
	"github.com/smartnpc/smartnpc-mcp/internal/tools"
)

// Build-time variables (override via -ldflags).
var (
	version = "0.1.0-dev"
)

func main() {
	var (
		showVersion = flag.Bool("version", false, "print version and exit")
		logLevel    = flag.String("log-level", "info", "log level: debug|info|warn|error")
		// wsURL kept for future use; currently only the ping tool is wired up.
		wsURL = flag.String("ws-url", "ws://localhost:8765/game", "SMAPI mod WebSocket URL (M2+)")
	)
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}

	logger := log.New(*logLevel)
	slog.SetDefault(logger)

	logger.Info("smartnpc-mcp starting",
		"version", version,
		"ws_url", *wsURL,
	)

	ctx, cancel := signal.NotifyContext(context.Background(),
		os.Interrupt, syscall.SIGTERM)
	defer cancel()

	server := mcp.NewServer(&mcp.Implementation{
		Name:    "smartnpc-mcp",
		Title:   "Stardew Valley NPC Bridge",
		Version: version,
	}, &mcp.ServerOptions{
		Logger: logger,
	})

	// Register all tool sets. M1 only ships the meta `ping` tool.
	tools.RegisterAll(server)

	logger.Info("listening on stdio")
	if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil {
		logger.Error("server terminated with error", "err", err)
		os.Exit(1)
	}
	logger.Info("smartnpc-mcp shut down cleanly")
}
