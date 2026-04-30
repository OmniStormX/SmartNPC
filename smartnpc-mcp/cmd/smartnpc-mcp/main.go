// Command smartnpc-mcp is the MCP server bridging the Stardew Valley SMAPI mod
// to MCP clients (smartnpc-agent, Claude Desktop, Hermes, ...).
//
// Two transports are supported:
//
//   - stdio (default): newline-delimited JSON-RPC over stdin/stdout. The
//     usual case for desktop MCP clients that spawn the server as a child
//     process.
//   - streamable HTTP (--http :PORT): exposes the same MCP server over HTTP
//     so a client on a different host (e.g. Hermes inside WSL while SDV +
//     mcp run on the Windows host) can connect remotely.
//
// IMPORTANT: in stdio mode, never write logs to stdout — it would corrupt
// the MCP stream. All logging goes through stderr.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/smartnpc/smartnpc-mcp/internal/bridge"
	"github.com/smartnpc/smartnpc-mcp/internal/log"
	"github.com/smartnpc/smartnpc-mcp/internal/tools"
)

var version = "0.1.0-dev"

func main() {
	var (
		showVersion = flag.Bool("version", false, "print version and exit")
		logLevel    = flag.String("log-level", "info", "log level: debug|info|warn|error")
		wsURL       = flag.String("ws-url", bridge.DefaultWSURL,
			"SMAPI mod WebSocket URL; empty disables mod-backed tools")
		echoMode = flag.Bool("echo-mode", false,
			"forward chat_received events back as chat_say (built-in echo NPC, no LLM). "+
				"Useful for verifying the round trip without an LLM agent.")
		echoSpeaker = flag.String("echo-speaker", "SmartNPC",
			"speaker name used when --echo-mode is on")
		httpAddr = flag.String("http", "",
			"if set (e.g. ':3000'), expose the MCP server over Streamable HTTP "+
				"on this address instead of stdio. Use for cross-host clients "+
				"like Hermes in WSL.")
		httpAllowAnyOrigin = flag.Bool("http-allow-any-origin", true,
			"in --http mode, disable origin / localhost restrictions so cross-host "+
				"clients can connect (set false if exposing to a hostile network)")
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
		"echo_mode", *echoMode,
		"http_addr", *httpAddr,
	)

	ctx, cancel := signal.NotifyContext(context.Background(),
		os.Interrupt, syscall.SIGTERM)
	defer cancel()

	server := mcp.NewServer(&mcp.Implementation{
		Name:    "smartnpc-mcp",
		Title:   "Stardew Valley NPC Bridge",
		Version: version,
	}, &mcp.ServerOptions{Logger: logger})

	// Wire the ws bridge first so we can attach event forwarders during
	// tool registration.
	var br *bridge.WSClient
	if *wsURL != "" {
		// Construct first, then bind the handler — the handler needs to
		// reference the client to issue chat_say in echo mode.
		br = bridge.NewWSClient(bridge.WSClientOptions{URL: *wsURL, Logger: logger})
		br.SetEventHandler(makeRouter(server, logger, br, *echoMode, *echoSpeaker))
		if err := br.Connect(ctx); err != nil {
			// Mod may not be running yet. The ws client retries in the
			// background; meanwhile non-mod tools (ping) still work.
			logger.Warn("initial ws connect failed; will retry in background", "err", err)
		}
	}

	tools.RegisterAll(server, br)

	if *httpAddr != "" {
		runHTTP(ctx, logger, server, *httpAddr, *httpAllowAnyOrigin)
	} else {
		runStdio(ctx, logger, server)
	}
}

// runStdio is the default mode: serve MCP over stdin/stdout.
func runStdio(ctx context.Context, logger *slog.Logger, server *mcp.Server) {
	logger.Info("listening on stdio")
	if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil {
		logger.Error("server terminated with error", "err", err)
		os.Exit(1)
	}
	logger.Info("smartnpc-mcp shut down cleanly")
}

// runHTTP serves the MCP server over Streamable HTTP at /mcp. Suitable for
// remote MCP clients (e.g. Hermes inside WSL connecting to the Windows host).
func runHTTP(ctx context.Context, logger *slog.Logger, server *mcp.Server, addr string, allowAnyOrigin bool) {
	mcpHandler := mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return server },
		&mcp.StreamableHTTPOptions{
			// Make cross-host access work out of the box. We listen on
			// :PORT (all interfaces) so DNS-rebinding protection only
			// triggers when the listener resolves to a loopback address;
			// passing this flag also keeps Hermes-from-WSL hitting the
			// Windows host IP from being rejected.
			DisableLocalhostProtection: allowAnyOrigin,
			// CrossOriginProtection: leave nil so the SDK's default
			// (zero-value http.CrossOriginProtection) is used. If a remote
			// MCP client gets blocked by Origin checks, set the env var
			// GODEBUG=disablecrossoriginprotection=1 when launching mcp.
		},
	)

	mux := http.NewServeMux()
	mux.Handle("/mcp", mcpHandler)
	mux.Handle("/mcp/", mcpHandler)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	httpServer := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()

	logger.Info("listening on streamable HTTP", "addr", addr, "endpoint", "/mcp")
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("http server terminated with error", "err", err)
		os.Exit(1)
	}
	logger.Info("smartnpc-mcp shut down cleanly")
}

// makeRouter constructs the bridge.EventHandler that combines:
//   - forwarding every event to MCP clients as a logging notification
//   - optional --echo-mode: when a chat_received event arrives, immediately
//     issue a chat_say back through the same bridge.
//
// br may be nil during initial wiring; in that case echo-mode is a no-op.
func makeRouter(server *mcp.Server, logger *slog.Logger, br *bridge.WSClient, echo bool, speaker string) bridge.EventHandler {
	forward := tools.MakeEventForwarder(server, logger)

	return func(ctx context.Context, name string, data json.RawMessage) {
		forward(ctx, name, data)

		if !echo || br == nil || name != bridge.EventChatReceived {
			return
		}
		var p struct {
			Text   string `json:"text"`
			Source string `json:"source"`
		}
		if err := json.Unmarshal(data, &p); err != nil || p.Text == "" {
			return
		}
		// Don't echo our own messages back (defense in depth — the mod
		// already filters info messages, but be safe).
		if p.Source == speaker {
			return
		}
		go func() {
			_, err := br.Call(ctx, bridge.ActionChatSay, map[string]any{
				"speaker": speaker,
				"text":    "You said: " + p.Text,
				"color":   "yellow",
			})
			if err != nil {
				logger.Warn("echo chat_say failed", "err", err)
			}
		}()
	}
}
