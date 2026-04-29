// Command smartnpc-agent is the NPC orchestrator.
//
// It spawns smartnpc-mcp as a subprocess (stdio) and acts as an MCP client.
// In later milestones it loads NPC personas, runs per-NPC agent loops powered
// by OpenAI, and persists memories.
//
// M1 subcommands:
//
//	ping       - call the MCP `ping` tool, print result
//	tools      - list tools exposed by the MCP server
//
// Example:
//
//	smartnpc-agent --mcp-bin path\to\smartnpc-mcp.exe ping --message hello
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/smartnpc/smartnpc-agent/internal/log"
	"github.com/smartnpc/smartnpc-agent/internal/mcpclient"
)

var version = "0.1.0-dev"

func main() {
	var (
		mcpBin   = flag.String("mcp-bin", "smartnpc-mcp", "path to smartnpc-mcp executable")
		mcpArgs  = flag.String("mcp-args", "", "extra args passed to smartnpc-mcp (space-separated)")
		logLevel = flag.String("log-level", "info", "log level: debug|info|warn|error")
		showVer  = flag.Bool("version", false, "print version and exit")
	)
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [flags] <subcommand> [subcommand-flags]\n", os.Args[0])
		fmt.Fprintln(os.Stderr, "\nFlags:")
		flag.PrintDefaults()
		fmt.Fprintln(os.Stderr, "\nSubcommands:")
		fmt.Fprintln(os.Stderr, "  ping       call the MCP `ping` tool")
		fmt.Fprintln(os.Stderr, "  tools      list MCP tools exposed by the server")
	}
	flag.Parse()

	if *showVer {
		fmt.Println(version)
		return
	}

	logger := log.New(*logLevel)
	slog.SetDefault(logger)

	args := flag.Args()
	if len(args) == 0 {
		flag.Usage()
		os.Exit(2)
	}
	sub, subArgs := args[0], args[1:]

	ctx, cancel := signal.NotifyContext(context.Background(),
		os.Interrupt, syscall.SIGTERM)
	defer cancel()

	cli, err := mcpclient.Spawn(ctx, mcpclient.Options{
		Binary: *mcpBin,
		Args:   splitArgs(*mcpArgs),
		Logger: logger,
	})
	if err != nil {
		logger.Error("failed to spawn smartnpc-mcp", "err", err)
		os.Exit(1)
	}
	defer cli.Close()

	switch sub {
	case "ping":
		if err := runPing(ctx, cli, subArgs); err != nil {
			logger.Error("ping failed", "err", err)
			os.Exit(1)
		}
	case "tools":
		if err := runListTools(ctx, cli); err != nil {
			logger.Error("listing tools failed", "err", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand: %s\n", sub)
		flag.Usage()
		os.Exit(2)
	}
}

func splitArgs(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	cur := ""
	for _, r := range s {
		if r == ' ' {
			if cur != "" {
				out = append(out, cur)
				cur = ""
			}
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}
