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
// IMPORTANT: in stdio mode, never write logs to stdout 鈥?it would corrupt
// the MCP stream. All logging goes through stderr.
package main

import (
	"context"
	"encoding/json"
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

	"github.com/OmniStormX/SmartNPC/adapters/stardew/bridge"
	"github.com/OmniStormX/SmartNPC/adapters/stardew/scheduler"
	"github.com/OmniStormX/SmartNPC/adapters/stardew/tools"
	"github.com/OmniStormX/SmartNPC/internal/log"
	"github.com/OmniStormX/SmartNPC/pkg/agentbridge"
	hermesrelay "github.com/OmniStormX/SmartNPC/pkg/relay/hermes"
	"github.com/OmniStormX/SmartNPC/pkg/transport"
	"github.com/OmniStormX/SmartNPC/pkg/workflow"
)

var version = "0.1.0-dev"

func main() {
	startTime := time.Now()

	// On Windows, switch the console output codepage to UTF-8 so non-ASCII
	// bytes (em-dash, CJK from LLM-produced text) render correctly on stderr
	// instead of mojibake under the default GBK / cp936. No-op elsewhere.
	log.EnableUTF8Console()

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
		mcpAPIKey = flag.String("mcp-api-key", "",
			"if set, require this Bearer token on /mcp requests. "+
				"Use when exposing --http to a public network.")
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
	//   1. --hermes-config (multi-profile) 鈥?preferred for production
	//   2. --hermes-url + sibling flags     鈥?legacy single-target
	//   3. neither                           鈥?relay disabled
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
			if len(hermesRelays) == 0 {
				logger.Warn("hermes relay loaded but no profiles enabled — events will not be forwarded")
			} else {
				logger.Info("hermes relay enabled (multi-profile)",
					"config", *hermesConfig, "profiles", len(hermesRelays))
			}
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

	// Wire the ws bridge. Construction order:
	//   1. Create WSClient (no handler yet)
	//   2. RegisterAll 鈫?returns dayScheduler (needs br for tool handlers)
	//   3. makeRouter (needs br, dayScheduler, hermesHandler)
	//   4. SetEventHandler + Connect
	var br *bridge.WSClient
	if *wsURL != "" {
		br = bridge.NewWSClient(bridge.WSClientOptions{URL: *wsURL, Logger: logger})
	}

	// Initialise the workflow registry from embedded builtins. Allow an
	// external directory (SMARTNPC_WORKFLOW_DIR) to overlay custom definitions
	// without rebuilding the binary.
	workflowReg := workflow.NewRegistry()
	if err := workflowReg.Init(os.Getenv("SMARTNPC_WORKFLOW_DIR")); err != nil {
		logger.Error("workflow registry init failed", "err", err)
		os.Exit(1)
	}
	logger.Info("workflow registry initialised", "count", len(workflowReg.List()))

	// ── Workflow pump (P4): when enabled, schedule entries with workflow
	// defs are run locally by the workflow engine rather than forwarded
	// to Hermes as schedule_trigger events. This eliminates one LLM call
	// per tool step. Set SMARTNPC_WORKFLOW_PUMP=1 to enable.
	workflowPump := os.Getenv("SMARTNPC_WORKFLOW_PUMP") != "0"
	if workflowPump {
		logger.Info("workflow pump ENABLED — schedule entries will run through local workflow engine")
	}

	dayScheduler := tools.RegisterAll(server, br, hermesHandler, chatGuard, logger, workflowReg, *logLevel == "debug")
	// Populate agent NPC list from relay configs so npc_plan_day supports "*".
	if len(hermesRelays) > 0 {
		names := make([]string, 0, len(hermesRelays))
		for _, r := range hermesRelays {
			if n := r.Cfg().NPCName; n != "" {
				names = append(names, n)
			}
		}
		dayScheduler.SetAgentNPCs(names)
		logger.Info("scheduler: agent NPCs registered", "npcs", names)
	}
	// Framework-level tools (ping) live in agentbridge so they remain
	// available even if the SDV adapter is detached.
	agentbridge.RegisterMeta(server)

	if br != nil {
		br.SetEventHandler(makeRouter(server, logger, br, *echoMode, *echoSpeaker, hermesHandler, hermesRelays, chatGuard, dayScheduler, workflowReg, workflowPump, *logLevel == "debug"))
		if err := br.Connect(ctx); err != nil {
			// Mod may not be running yet. The ws client retries in the
			// background; meanwhile non-mod tools (ping) still work.
			logger.Warn("initial ws connect failed; will retry in background", "err", err)
		}
	}

	if *httpAddr != "" {
		runHTTP(ctx, logger, server, *httpAddr, *httpAllowAnyOrigin, *mcpAPIKey,
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

// runHTTP serves the MCP server over Streamable HTTP via pkg/transport.
//
// Also exposes /status (SDV-specific operator dashboard: ws connection
// state + per-Hermes-profile gateway health probe). /mcp + /healthz are
// owned by transport.RunHTTP. The status endpoint is read-only and
// probes each Hermes Gateway's /health URL in parallel with a short
// per-call timeout so a single dead gateway can't stall the response.
func runHTTP(
	ctx context.Context,
	logger *slog.Logger,
	server *mcp.Server,
	addr string,
	allowAnyOrigin bool,
	mcpAPIKey string,
	startTime time.Time,
	wsURL string,
	br *bridge.WSClient,
	hermesRelays []*hermesrelay.Relay,
) {
	mux := http.NewServeMux()
	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		snap := buildStatusSnapshot(r.Context(), startTime, wsURL, br, hermesRelays)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(snap)
	})

	if err := transport.RunHTTP(ctx, logger, server, transport.HTTPOptions{
		Addr:           addr,
		AllowAnyOrigin: allowAnyOrigin,
		MCPAPIKey:      mcpAPIKey,
		Mux:            mux,
	}); err != nil {
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
//   - game_time_tick handling: checks the daily scheduler and fires
//     schedule_trigger events for each NPC with a due entry.
//   - day_started handling: clears all NPC schedules for the new day.
//
// br may be nil during initial wiring; in that case echo-mode is a no-op.
// relay may be nil; in that case the Hermes forwarding is skipped.
// chatGuard may be nil; the reset hook becomes a no-op.
// sched may be nil; in that case schedule dispatch is skipped.
// schedTriggerMsg carries everything a per-NPC worker needs to process one
// schedule_trigger event.
type schedTriggerMsg struct {
	ctx         context.Context
	npc         string
	triggerData json.RawMessage
	action      string
	reason      string
	// ── P4 workflow fields ────────────────────────────────────────────
	workflowID string
	workflow   *workflow.Definition
	args       map[string]any
}

// npcWorkerQueueSize is the buffer depth of each per-NPC trigger channel.
// Generous enough to absorb a burst of triggers from a single tick without
// blocking the event loop.
const npcWorkerQueueSize = 16

func makeRouter(
	server *mcp.Server,
	logger *slog.Logger,
	br *bridge.WSClient,
	echo bool,
	speaker string,
	relay bridge.EventHandler,
	relays []*hermesrelay.Relay,
	chatGuard *tools.ChatSayGuard,
	sched *scheduler.Scheduler,
	workflowReg *workflow.Registry,
	workflowPump bool,
	schedDebug bool,
) bridge.EventHandler {
	forward := tools.MakeEventForwarder(server, logger)

	// When day_started fires, the mod also emits a game_time_tick for
	// the same hour (typically 6). Suppress the relay for that tick so
	// the LLM receives a single day_started turn and calls npc_plan_day
	// before being interrupted by a concurrent tick turn.
	var suppressTickRelayUntil time.Time

	// 鈹€鈹€ Per-NPC persistent worker goroutines 鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€
	// One goroutine per known agent NPC, each consuming from its own
	// buffered channel. This guarantees:
	//   - Bounded goroutine count (no fire-and-forget explosion)
	//   - Per-NPC serial execution (no race on ws calls for same NPC)
	//   - Backpressure via channel buffer (drops with warning on overflow)
	npcQueues := make(map[string]chan schedTriggerMsg)
	if sched != nil {
		for _, npc := range sched.AgentNPCs() {
			ch := make(chan schedTriggerMsg, npcWorkerQueueSize)
			npcQueues[npc] = ch
			if workflowPump {
				go npcWorkflowWorker(ch, logger, br, relay, workflowReg, schedDebug)
			} else {
				go npcTriggerWorker(ch, logger, br, relay, schedDebug)
			}
		}
		if len(npcQueues) > 0 {
			mode := "schedule_trigger"
			if workflowPump {
				mode = "workflow engine"
			}
			logger.Info("scheduler: per-NPC workers started", "count", len(npcQueues), "mode", mode)
		}
	}

	return func(ctx context.Context, name string, data json.RawMessage) {
		// Persistent event trace 鈫?logs/mcp/events.log
		tools.LogEvent(name, data)

		// Refresh chat_say budgets before the relay fires so the recipient
		// NPC's wake-up starts clean. MaybeResetGuard handles both the
		// group reset (on player_group input) and the private reset (any
		// event addressed to a specific NPC).
		tools.MaybeResetGuard(chatGuard, name, data)

		// MCP clients always see the raw event stream.
		forward(ctx, name, data)

		// 鈹€鈹€ day_started: clear all NPC schedules + rotate session 鈹€鈹€鈹€鈹€鈹€鈹€
		if name == bridge.EventDayStarted {
			// Rotate Hermes conversation so each game day starts a fresh
			// session. Parse the event to build a meaningful suffix.
			if len(relays) > 0 {
				var ds struct {
					Day    int    `json:"day"`
					Season string `json:"season"`
					Year   int    `json:"year"`
				}
				if err := json.Unmarshal(data, &ds); err == nil && ds.Season != "" {
					suffix := fmt.Sprintf("%s-d%d-y%d", ds.Season, ds.Day, ds.Year)
					for _, r := range relays {
						r.RotateSession(suffix)
					}
				}
			}
			if sched != nil {
				sched.ClearAll()
				logger.Info("scheduler: cleared all NPC schedules for new day")
			}
			suppressTickRelayUntil = time.Now().Add(5 * time.Second)
		}

		// ── game_time_tick: check scheduler and fire triggers ─────────
		if name == bridge.EventGameTimeTick && sched != nil {

			var tick struct {
				Hour    int `json:"hour"`
				Minute  int `json:"minute"`
				Minutes int `json:"minutes"`
			}
			if err := json.Unmarshal(data, &tick); err == nil && tick.Hour >= 6 {
				// Newer mod payloads send `minutes` (hour*60+minute, total
				// minutes from midnight). Older callers / tests may only
				// send `hour`, so fall back to hour*60 to keep them working.
				gameMinutes := tick.Minutes
				if gameMinutes == 0 {
					gameMinutes = tick.Hour*60 + tick.Minute
				}
				fired := sched.Tick(gameMinutes)
				logger.Debug("scheduler: tick",
				"game_minutes", gameMinutes,
				"fired", len(fired))
				// When schedule entries fire, suppress the game_time_tick
				// relay so the LLM doesn't receive two concurrent turns
				// (schedule_trigger + tick) for the same instant.
				if len(fired) > 0 {
					suppressTickRelayUntil = time.Now().Add(5 * time.Second)
				}
				// Persist a human-readable record of what fired this tick to
				// <logDir>/mcp/schedule_triggers.log. No-op when nothing
				// fired. Console output stays out of the way — slog already
				// emits a structured "scheduler: firing schedule_trigger"
				// line below for each entry.
				tools.LogScheduleTriggers(tick.Hour, fired)
				for _, entry := range fired {
					triggerData, err := json.Marshal(map[string]any{
						"npc":          entry.NPC,
						"game_hour":    entry.GameHour,
						"game_minute":  entry.GameMinute,
						"game_minutes": entry.GameMinutes,
						"action":       entry.Action,
						"workflow_id":  entry.WorkflowID,
						"args":         entry.Args,
						"reason":       entry.Reason,
					})
					if err != nil {
						logger.Warn("scheduler: marshal trigger failed", "npc", entry.NPC, "err", err)
						continue
					}
					logger.Info("scheduler: firing schedule_trigger",
						"npc", entry.NPC, "hour", entry.GameHour, "minute", entry.GameMinute,
						"action", entry.Action, "workflow_id", entry.WorkflowID)
					// Forward to MCP clients for observability.
					forward(ctx, bridge.EventScheduleTrigger, triggerData)

					// Dispatch to per-NPC worker queue for serial execution.
					msg := schedTriggerMsg{
						ctx:         ctx,
						npc:         entry.NPC,
						triggerData: triggerData,
						action:      entry.Action,
						reason:      entry.Reason,
						workflowID:  entry.WorkflowID,
						workflow:    entry.Workflow,
						args:        entry.Args,
					}
					if ch, ok := npcQueues[entry.NPC]; ok {
						select {
						case ch <- msg:
						default:
							logger.Warn("scheduler: NPC trigger queue full, dropping",
								"npc", entry.NPC, "action", entry.Action)
						}
					} else {
						// NPC not in pre-registered list 鈥?fall back to inline.
						logger.Warn("scheduler: no worker for NPC, inline dispatch",
							"npc", entry.NPC, "action", entry.Action)
						if schedDebug && br != nil {
							dispatchSchedDebug(ctx, logger, br, entry.NPC, entry.Action, entry.Reason)
						} else if relay != nil {
							relay(ctx, bridge.EventScheduleTrigger, triggerData)
						}
					}
				}
			}
		}

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
		case relay != nil && !(name == bridge.EventGameTimeTick && (workflowPump || time.Now().Before(suppressTickRelayUntil))):
			relay(ctx, name, data)
		case relay != nil && name == bridge.EventGameTimeTick:
			reason := "after day_started"
			if workflowPump {
				reason = "workflow pump enabled (ticks handled locally)"
			}
			logger.Debug("relay: suppressing game_time_tick",
				"reason", reason, "suppress_until", suppressTickRelayUntil.Format("15:04:05.000"))
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
		// Don't echo our own messages back (defense in depth 鈥?the mod
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

// npcTriggerWorker is the persistent goroutine that consumes schedule_trigger
// messages for a single NPC. It serializes execution so ws calls for the same
// NPC never race. (Legacy path — replaced by npcWorkflowWorker when the
// workflow pump is enabled.)
func npcTriggerWorker(
	ch <-chan schedTriggerMsg,
	logger *slog.Logger,
	br *bridge.WSClient,
	relay bridge.EventHandler,
	schedDebug bool,
) {
	for msg := range ch {
		if schedDebug && br != nil {
			dispatchSchedDebug(msg.ctx, logger, br, msg.npc, msg.action, msg.reason)
		}
		if relay != nil {
			relay(msg.ctx, bridge.EventScheduleTrigger, msg.triggerData)
		}
	}
}

// npcWorkflowWorker is the P4 persistent goroutine that consumes schedule
// messages for a single NPC. When the entry carries a workflow definition,
// it runs the workflow engine locally (no per-step LLM call). Falls back to
// schedule_trigger relay when the entry has no workflow definition.
func npcWorkflowWorker(
	ch <-chan schedTriggerMsg,
	logger *slog.Logger,
	br *bridge.WSClient,
	relay bridge.EventHandler,
	workflowReg *workflow.Registry,
	schedDebug bool,
) {
	for msg := range ch {
		logger.Debug("workflow worker: received trigger",
			"npc", msg.npc,
			"workflow_id", msg.workflowID,
			"action", msg.action,
			"has_inline", msg.workflow != nil,
		)

		// Resolve the workflow definition.
		def := resolveDefinition(workflowReg, msg.workflowID, msg.workflow)
		if def == nil {
			// No workflow — fall back to old schedule_trigger path.
			logger.Warn("workflow worker: no workflow definition, dispatching as schedule_trigger",
				"npc", msg.npc, "workflow_id", msg.workflowID, "action", msg.action)
			if schedDebug && br != nil {
				dispatchSchedDebug(msg.ctx, logger, br, msg.npc, msg.action, msg.reason)
			}
			if relay != nil {
				relay(msg.ctx, bridge.EventScheduleTrigger, msg.triggerData)
			}
			continue
		}

		logger.Info("workflow worker: starting workflow",
			"npc", msg.npc, "workflow_id", def.ID, "steps", len(def.Steps))

		// Run the workflow engine.
		runner := workflow.NewMCPRunner(workflow.MCPRunnerOptions{
			Bridge: br,
			Logger: logger.With("npc", msg.npc, "workflow", def.ID),
			Relay:  relay,
		})

		runCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		started := time.Now()
		res, err := workflow.Run(runCtx, runner, msg.npc, def, msg.args)
		elapsed := time.Since(started)
		cancel()

		// Persist the run result to logs/mcp/workflow_runs/.
		// Season/day/year default to 0/"unknown" since the schedule entry
		// doesn't carry game date info — the player can filter later.
		tools.LogWorkflowRun(msg.npc, "unknown", 0, 0, res, err, elapsed, msg.args)

		if err != nil {
			logger.Warn("workflow run failed",
				"npc", msg.npc, "workflow", def.ID, "elapsed_ms", elapsed.Milliseconds(), "err", err)
		} else {
			logger.Info("workflow run completed",
				"npc", msg.npc, "workflow", def.ID,
				"steps", res.StepCount, "tools", res.ToolCalls,
				"nothing", res.NothingToDoCt, "stopped", res.Stopped,
				"elapsed_ms", elapsed.Milliseconds())
		}
	}
}

// resolveDefinition returns the workflow.Definition for the given entry,
// resolving a built-in ID from the registry or using an inline definition.
// Returns nil when neither is set.
func resolveDefinition(reg *workflow.Registry, workflowID string, inline *workflow.Definition) *workflow.Definition {
	if inline != nil {
		return inline
	}
	if workflowID != "" && reg != nil {
		return reg.Get(workflowID)
	}
	return nil
}

// dispatchSchedDebug sends the schedule action as game chat + text bubble for
// visual debugging without waking the LLM.
func dispatchSchedDebug(
	ctx context.Context,
	logger *slog.Logger,
	br *bridge.WSClient,
	npc, action, reason string,
) {
	msg := fmt.Sprintf("[schedule] %s", action)
	if reason != "" {
		msg = fmt.Sprintf("%s 鈥?%s", msg, reason)
	}
	logger.Debug("schedDebug: dispatching",
		"npc", npc, "msg", msg)
	_, _ = br.CallAs(ctx, npc, bridge.ActionChatSay, map[string]any{
		"npc":  npc,
		"text": msg,
	})
	_, _ = br.CallAs(ctx, npc, bridge.ActionNpcShowTextBubble, map[string]any{
		"npc":  npc,
		"text": msg,
	})
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

// statusSnapshot is the body of the /status response 鈥?a single read-only
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
