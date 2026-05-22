// Command agent-bridge is the generic MCP bridge entrypoint. It loads a
// bridge.yaml file, dereferences each adapter / relay against the global
// registry (populated by adapter / relay package init() functions), and
// runs the assembled Server over the configured MCP transport.
//
// Compared to cmd/smartnpc-mcp (which hardcodes the SDV adapter and
// Hermes relay), this binary is purely composition-driven:
//
//	agent-bridge --config bridge.yaml
//
// Adapters and relays are linked at build time. Today the binary
// imports adapters/stardew, pkg/relay/hermes, and pkg/relay/echo so
// those kinds are available; future binaries may swap or extend them.
//
// In stdio mode, all logging goes to stderr. The MCP protocol stream
// owns stdout exclusively.
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

	"github.com/OmniStormX/SmartNPC/internal/log"
	"github.com/OmniStormX/SmartNPC/pkg/agentbridge"
	"github.com/OmniStormX/SmartNPC/pkg/transport"

	// Side-effect imports populate the global registry.
	_ "github.com/OmniStormX/SmartNPC/adapters/stardew"
	_ "github.com/OmniStormX/SmartNPC/pkg/relay/echo"
	_ "github.com/OmniStormX/SmartNPC/pkg/relay/hermes"
)

var version = "0.1.0-dev"

func main() {
	var (
		showVersion        = flag.Bool("version", false, "print version and exit")
		configPath         = flag.String("config", "", "path to bridge.yaml (required)")
		logLevel           = flag.String("log-level", "info", "debug | info | warn | error")
		httpAllowAnyOrigin = flag.Bool("http-allow-any-origin", true,
			"in transport.kind=http, disable origin / localhost protection so cross-host clients can connect")
	)
	flag.Parse()

	if *showVersion {
		fmt.Fprintln(os.Stderr, version)
		return
	}
	if *configPath == "" {
		fmt.Fprintln(os.Stderr, "agent-bridge: --config is required")
		os.Exit(2)
	}

	logger := log.New(*logLevel)
	slog.SetDefault(logger)

	cfg, err := agentbridge.LoadConfig(*configPath)
	if err != nil {
		logger.Error("agent-bridge: load config failed", "err", err)
		os.Exit(1)
	}

	mcpServer := mcp.NewServer(&mcp.Implementation{
		Name:    "agent-bridge",
		Version: version,
	}, &mcp.ServerOptions{Logger: logger})

	srv, err := cfg.Assemble(mcpServer, logger)
	if err != nil {
		logger.Error("agent-bridge: assemble failed", "err", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// EventSources run in their own goroutine via srv.Run; meanwhile the
	// MCP transport blocks in the foreground. When transport returns
	// (clean shutdown or fatal error), we cancel ctx so srv.Run exits.
	runErrCh := make(chan error, 1)
	go func() {
		runErrCh <- srv.Run(ctx)
	}()

	logger.Info("agent-bridge starting",
		"version", version,
		"config", *configPath,
		"transport", cfg.Transport.Kind,
		"adapters", len(cfg.Adapters),
		"relays", len(cfg.Relays),
	)

	switch cfg.Transport.Kind {
	case "stdio":
		if err := mcpServer.Run(ctx, &mcp.StdioTransport{}); err != nil {
			logger.Error("stdio transport failed", "err", err)
			cancel()
			<-runErrCh
			os.Exit(1)
		}
	case "http":
		if err := transport.RunHTTP(ctx, logger, mcpServer, transport.HTTPOptions{
			Addr:           cfg.Transport.Addr,
			AllowAnyOrigin: *httpAllowAnyOrigin,
		}); err != nil {
			logger.Error("http transport failed", "err", err)
			cancel()
			<-runErrCh
			os.Exit(1)
		}
	}

	cancel()
	<-runErrCh
	logger.Info("agent-bridge: shut down cleanly")
}
