// Command smartnpc-mcp is the MCP server bridging the Stardew Valley SMAPI mod
// to MCP clients (Claude Desktop, Hermes, ...).
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
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/OmniStormX/SmartNPC/internal/bridge"
	"github.com/OmniStormX/SmartNPC/internal/hermesrelay"
	"github.com/OmniStormX/SmartNPC/internal/log"
	"github.com/OmniStormX/SmartNPC/internal/tools"
)

var version = "0.1.0-dev"

func main() {
	startTime := time.Now()

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
		hermesURL = flag.String("hermes-url", "",
			"if set, forward bridge events to this Hermes Gateway base URL "+
				"(e.g. http://127.0.0.1:8642). Empty disables the relay.")
		hermesAPIKey = flag.String("hermes-api-key", "",
			"bearer token sent to Hermes Gateway; match the API_SERVER_KEY "+
				"of the target profile")
		hermesConversation = flag.String("hermes-conversation", "",
			"Hermes conversation id, conventionally the profile name (e.g. \"xiami\")")
		hermesModel = flag.String("hermes-model", "",
			"Hermes profile model name (API_SERVER_MODEL_NAME of the target profile)")
		hermesNPC = flag.String("hermes-npc", "",
			"forward only events whose npc/to/target matches this NPC name; "+
				"empty forwards every event")
		hermesPersonaFile = flag.String("hermes-persona-file", "",
			"optional path to a markdown file whose contents are sent as the "+
				"Hermes `instructions` field on every event POST")
		hermesConfig = flag.String("hermes-config", "",
			"path to a YAML file with a `profiles:` array (one entry per NPC) "+
				"for multi-profile fan-out. When set, takes precedence over "+
				"--hermes-url / --hermes-npc / --hermes-conversation / --hermes-model.")
	)
	flag.Parse()

	if *showVersion {
		fmt.Fprintln(os.Stderr, version)
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

	// Build the hermes event handler. Precedence:
	//   1. --hermes-config (multi-profile) — preferred for production
	//   2. --hermes-url + sibling flags     — legacy single-target
	//   3. neither                           — relay disabled
	var hermesHandler bridge.EventHandler
	var hermesRelays []*hermesrelay.Relay // collected for /status reporting
	switch {
	case *hermesConfig != "":
		cfgs, err := hermesrelay.LoadConfigFile(*hermesConfig)
		if err != nil {
			logger.Error("hermes-config load failed", "path", *hermesConfig, "err", err)
			os.Exit(1)
		}
		group, err := hermesrelay.NewGroup(cfgs, logger)
		if err != nil {
			logger.Error("hermesrelay group init failed", "err", err)
			os.Exit(1)
		}
		hermesHandler = group.HandleEvent
		hermesRelays = group.Relays()
		logger.Info("hermes relay enabled (multi-profile)",
			"config", *hermesConfig, "profiles", len(group.Relays()))
	case *hermesURL != "":
		payloadLogger, payloadEnabled, err := hermesrelay.PayloadLoggerFromEnv()
		if err != nil {
			logger.Error("hermesrelay payload log open failed", "err", err)
			os.Exit(1)
		}
		single, err := hermesrelay.New(hermesrelay.Config{
			URL:           *hermesURL,
			APIKey:        *hermesAPIKey,
			Conversation:  *hermesConversation,
			Model:         *hermesModel,
			NPCName:       *hermesNPC,
			PersonaFile:   *hermesPersonaFile,
			DebugPayload:  hermesrelay.DebugPayloadEnabled() || payloadEnabled,
			PayloadLogger: payloadLogger,
			Store:         hermesrelay.StoreFromEnv(),
			Timeout:       hermesrelay.TimeoutFromEnv(),
		}, logger)
		if err != nil {
			logger.Error("hermesrelay init failed", "err", err)
			os.Exit(1)
		}
		hermesHandler = single.HandleEvent
		hermesRelays = []*hermesrelay.Relay{single}
		logger.Info("hermes relay enabled (single-profile, legacy flags)",
			"url", *hermesURL, "conversation", *hermesConversation,
			"npc_filter", *hermesNPC)
	default:
		hermesHandler = nil
	}

	// Group-chat speak budget: one chat_say per (group, speaker) per player
	// turn; reset by player input into that group. Lives at process scope so
	// the same instance is observed by both the chat_say tool handler and
	// the bridge router that resets it on player_group events.
	chatGuard := tools.NewChatSayGuard()

	// Wire the ws bridge first so we can attach event forwarders during
	// tool registration.
	var br *bridge.WSClient
	if *wsURL != "" {
		// Construct first, then bind the handler — the handler needs to
		// reference the client to issue chat_say in echo mode.
		br = bridge.NewWSClient(bridge.WSClientOptions{URL: *wsURL, Logger: logger})
		br.SetEventHandler(makeRouter(server, logger, br, *echoMode, *echoSpeaker, hermesHandler, chatGuard))
		if err := br.Connect(ctx); err != nil {
			// Mod may not be running yet. The ws client retries in the
			// background; meanwhile non-mod tools (ping) still work.
			logger.Warn("initial ws connect failed; will retry in background", "err", err)
		}
	}

	tools.RegisterAll(server, br, hermesHandler, chatGuard, logger)

	if *httpAddr != "" {
		runHTTP(ctx, logger, server, *httpAddr, *httpAllowAnyOrigin,
			startTime, *wsURL, br, hermesRelays)
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
//
// Also exposes /healthz (liveness, no dependencies) and /status (operator
// dashboard: ws connection state + per-profile gateway health probe). The
// status endpoint is read-only and probes each Hermes Gateway's /health URL
// in parallel with a short per-call timeout so a single dead gateway can't
// stall the response.
func runHTTP(
	ctx context.Context,
	logger *slog.Logger,
	server *mcp.Server,
	addr string,
	allowAnyOrigin bool,
	startTime time.Time,
	wsURL string,
	br *bridge.WSClient,
	hermesRelays []*hermesrelay.Relay,
) {
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
	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		snap := buildStatusSnapshot(r.Context(), startTime, wsURL, br, hermesRelays)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(snap)
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
//   - audible-routing policy: a chat_received event with non-empty
//     `audible_npcs` is converted into an additional synthesized
//     chat_message targeting the closest audible NPC. The original
//     chat_received is still forwarded to MCP clients (ambient observers),
//     but the Hermes relay only sees the synthesized chat_message to
//     avoid double-delivering the same line as both broadcast and DM.
//   - optional --echo-mode: when a chat_received event arrives, immediately
//     issue a chat_say back through the same bridge.
//   - optional Hermes relay: POST the event to a running Hermes Gateway
//     so an NPC profile can drive its turn.
//   - group-chat speak-budget reset: every chat_received with
//     source="player_group" calls ResetGroup on chatGuard so each NPC in
//     that group gets a fresh chat_say budget for the new player turn.
//
// br may be nil during initial wiring; in that case echo-mode is a no-op.
// relay may be nil; in that case the Hermes forwarding is skipped.
// chatGuard may be nil; the reset hook becomes a no-op.
func makeRouter(server *mcp.Server, logger *slog.Logger, br *bridge.WSClient, echo bool, speaker string, relay bridge.EventHandler, chatGuard *tools.ChatSayGuard) bridge.EventHandler {
	forward := tools.MakeEventForwarder(server, logger)

	return func(ctx context.Context, name string, data json.RawMessage) {
		// Refresh chat_say budgets before the relay fires so the recipient
		// NPC's wake-up starts clean. MaybeResetGuard handles both the
		// group reset (on player_group input) and the private reset (any
		// event addressed to a specific NPC).
		tools.MaybeResetGuard(chatGuard, name, data)

		// MCP clients always see the raw event stream.
		forward(ctx, name, data)

		// Audible routing: when chat_received carries audible_npcs, the
		// synthesized chat_message is the authoritative event for the
		// relay. The original chat_received is suppressed from the relay
		// to avoid double-delivery.
		var synthData json.RawMessage
		var synthOK bool
		if name == bridge.EventChatReceived {
			synthData, synthOK = synthChatMessageFromAudible(data)
		}

		switch {
		case synthOK:
			// The synth carries the recipient NPC; refresh that NPC's
			// private budget before the relay wakes their Hermes profile.
			tools.MaybeResetGuard(chatGuard, bridge.EventChatMessage, synthData)
			forward(ctx, bridge.EventChatMessage, synthData)
			if relay != nil {
				relay(ctx, bridge.EventChatMessage, synthData)
			}
		case relay != nil:
			relay(ctx, name, data)
		}

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

// synthChatMessageFromAudible inspects a chat_received payload and, when
// it carries a non-empty `audible_npcs` array, returns a synthesized
// chat_message payload (npc + target + text + source) for the closest
// audible NPC. The C# mod sorts the list by distance ascending, so the
// first entry is the recipient. Returns (nil, false) when the payload
// has no audible NPCs or the JSON is malformed.
func synthChatMessageFromAudible(data json.RawMessage) (json.RawMessage, bool) {
	var p struct {
		Text    string `json:"text"`
		Source  string `json:"source"`
		Audible []struct {
			Name string `json:"name"`
		} `json:"audible_npcs"`
	}
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, false
	}
	if len(p.Audible) == 0 || p.Audible[0].Name == "" || p.Text == "" {
		return nil, false
	}
	out, err := json.Marshal(map[string]any{
		"npc":    p.Audible[0].Name,
		"target": p.Audible[0].Name,
		"text":   p.Text,
		"source": p.Source,
	})
	if err != nil {
		return nil, false
	}
	return out, true
}

// statusProfile is the per-Hermes-profile slice of the /status response.
// Each entry corresponds to one entry in --hermes-config.yaml (or the single
// --hermes-url profile in legacy mode).
type statusProfile struct {
	NPCFilter    string `json:"npc_filter,omitempty"`
	GatewayURL   string `json:"gateway_url"`
	Conversation string `json:"conversation,omitempty"`
	Model        string `json:"model,omitempty"`
	Healthy      bool   `json:"healthy"`
	LatencyMS    int64  `json:"latency_ms,omitempty"`
	Error        string `json:"error,omitempty"`
}

// statusSnapshot is the body of the /status response — a single read-only
// view of mcp's runtime state. Generated on demand; not cached.
type statusSnapshot struct {
	Version        string          `json:"version"`
	UptimeSeconds  int64           `json:"uptime_seconds"`
	StartedAt      string          `json:"started_at"`
	GeneratedAt    string          `json:"generated_at"`
	ModWSURL       string          `json:"mod_ws_url,omitempty"`
	ModWSConnected bool            `json:"mod_ws_connected"`
	Profiles       []statusProfile `json:"profiles"`
}

// buildStatusSnapshot collects the live state into a statusSnapshot. Each
// profile gateway is probed in parallel with a 2-second timeout so a single
// dead gateway can't stall the response. The probe is HTTP GET on the URL
// derived from cfg.URL by stripping the /v1/responses suffix and appending
// /health (the convention Hermes Gateway uses).
func buildStatusSnapshot(
	ctx context.Context,
	startTime time.Time,
	wsURL string,
	br *bridge.WSClient,
	relays []*hermesrelay.Relay,
) statusSnapshot {
	now := time.Now()
	snap := statusSnapshot{
		Version:       version,
		UptimeSeconds: int64(now.Sub(startTime).Seconds()),
		StartedAt:     startTime.UTC().Format(time.RFC3339),
		GeneratedAt:   now.UTC().Format(time.RFC3339),
		ModWSURL:      wsURL,
		Profiles:      make([]statusProfile, len(relays)),
	}
	if br != nil {
		snap.ModWSConnected = br.Connected()
	}

	if len(relays) == 0 {
		return snap
	}

	// Per-profile health probe in parallel.
	const probeTimeout = 2 * time.Second
	httpClient := &http.Client{Timeout: probeTimeout}
	var wg sync.WaitGroup
	for i, r := range relays {
		wg.Add(1)
		go func(idx int, rr *hermesrelay.Relay) {
			defer wg.Done()
			cfg := rr.Cfg()
			ps := statusProfile{
				GatewayURL:   cfg.URL,
				Conversation: cfg.Conversation,
				Model:        cfg.Model,
				NPCFilter:    cfg.NPCName,
			}
			// Hermes Gateway exposes /health on the same host:port as
			// /v1/responses. Strip the responses suffix so we hit the
			// liveness endpoint instead of the agent endpoint.
			base := strings.TrimSuffix(cfg.URL, "/v1/responses")
			base = strings.TrimSuffix(base, "/")
			healthURL := base + "/health"

			pctx, cancel := context.WithTimeout(ctx, probeTimeout)
			defer cancel()
			req, err := http.NewRequestWithContext(pctx, http.MethodGet, healthURL, nil)
			if err != nil {
				ps.Error = "build request: " + err.Error()
				snap.Profiles[idx] = ps
				return
			}
			start := time.Now()
			resp, err := httpClient.Do(req)
			ps.LatencyMS = time.Since(start).Milliseconds()
			if err != nil {
				ps.Error = err.Error()
				snap.Profiles[idx] = ps
				return
			}
			_ = resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				ps.Healthy = true
			} else {
				ps.Error = fmt.Sprintf("status %d", resp.StatusCode)
			}
			snap.Profiles[idx] = ps
		}(i, r)
	}
	wg.Wait()
	return snap
}
