// Package stardew is the Stardew Valley adapter for the agent-bridge
// framework. It wires:
//
//   - a WebSocket bridge to the SMAPI mod (adapters/stardew/bridge)
//   - the SDV-specific tool registry (adapters/stardew/tools)
//   - an EventSource that translates ws events into eventbus.Event
//
// Used both by the legacy cmd/smartnpc-mcp/main.go (direct construction)
// and by the generic cmd/agent-bridge CLI via yaml-driven registration.
package stardew

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gopkg.in/yaml.v3"

	"github.com/OmniStormX/SmartNPC/adapters/stardew/bridge"
	"github.com/OmniStormX/SmartNPC/adapters/stardew/tools"
	"github.com/OmniStormX/SmartNPC/pkg/agentbridge"
	"github.com/OmniStormX/SmartNPC/pkg/eventbus"
)

// Config is the stardew adapter's slice of bridge.yaml.
//
// Example:
//
//	adapters:
//	  - kind: stardew
//	    config:
//	      ws_url: ws://127.0.0.1:18745/ws
type Config struct {
	// WSURL is the SMAPI mod's WebSocket endpoint. Empty means use the
	// default (ws://127.0.0.1:18745/ws).
	WSURL string `yaml:"ws_url"`
}

// Adapter is the live runtime for the stardew adapter — a WSClient plus
// metadata. Adapter implements agentbridge.EventSource by translating
// ws events into eventbus.Events for the bridge to fan out.
type Adapter struct {
	cfg    Config
	client *bridge.WSClient
	logger *slog.Logger
}

// New constructs an Adapter. The WSClient is created but NOT connected
// yet — Connect happens in Start so the lifecycle is bound to the
// caller's context.
func New(cfg Config, logger *slog.Logger) *Adapter {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.WSURL == "" {
		cfg.WSURL = bridge.DefaultWSURL
	}
	return &Adapter{
		cfg:    cfg,
		client: bridge.NewWSClient(bridge.WSClientOptions{URL: cfg.WSURL, Logger: logger}),
		logger: logger,
	}
}

// Name implements agentbridge.EventSource.
func (a *Adapter) Name() string { return "stardew" }

// Start implements agentbridge.EventSource. Connects the ws client,
// translates every incoming ws event into an eventbus.Event, and
// invokes sink. Blocks until ctx is canceled.
//
// The ws client retries internally on disconnect; Start does not return
// on transient connection failures — only on ctx cancellation.
func (a *Adapter) Start(ctx context.Context, sink agentbridge.Sink) error {
	a.client.SetEventHandler(func(_ context.Context, name string, data json.RawMessage) {
		ev := eventbus.Event{
			Kind:    name, // SDV event names ("chat_message", ...) are used verbatim as Kind
			Source:  "sdv",
			Payload: json.RawMessage(data),
		}
		// Best-effort recipient extraction so Backends can route by NPC
		// without re-parsing the payload. Failures here just leave Subject
		// empty; downstream filters fall through.
		ev.Subject = extractSubject(name, data)
		sink(ctx, ev)
	})
	if err := a.client.Connect(ctx); err != nil {
		// Mirror legacy main.go behavior: warn, do not abort — ws_client
		// retries in the background.
		a.logger.Warn("stardew: initial ws connect failed; retrying in background",
			"url", a.cfg.WSURL, "err", err)
	}
	<-ctx.Done()
	_ = a.client.Close()
	return nil
}

// Register installs the stardew adapter onto the given agent-bridge
// Server: attaches the EventSource and mounts the SDV tool registry.
//
// NOTE: inter-NPC messaging (npc_send_message / npc_broadcast_event)
// requires a Hermes EventHandler to wake the recipient's profile.
// When invoked from the generic agent-bridge CLI, that handler is
// not available (Backends speak eventbus.Event, not the legacy
// EventHandler signature). Inter-NPC tools still register, but
// synthesized events are emitted to an in-process logger only —
// recipient profiles will not be woken until the routing path is
// unified onto eventbus.Event in a follow-up.
func (a *Adapter) Register(srv *agentbridge.Server) error {
	srv.AttachEventSource(a)
	chatGuard := tools.NewChatSayGuard()
	if err := srv.Mount("stardew/tools", func(s *mcp.Server) error {
		tools.RegisterAll(s, a.client, nil /* hermes EventHandler */, chatGuard, a.logger)
		return nil
	}); err != nil {
		return err
	}
	a.logger.Info("stardew: adapter registered",
		"ws_url", a.cfg.WSURL,
		"inter_npc_wake", "disabled — generic CLI does not wire Hermes EventHandler")
	return nil
}

// extractSubject returns the NPC name an event is "about", best-effort.
// Empty string when the event has no clear subject (or parsing fails).
func extractSubject(name string, data json.RawMessage) string {
	var p struct {
		NPC    string `json:"npc"`
		Target string `json:"target"`
		To     string `json:"to"`
	}
	if err := json.Unmarshal(data, &p); err != nil {
		return ""
	}
	switch {
	case p.NPC != "":
		return p.NPC
	case p.Target != "":
		return p.Target
	case p.To != "":
		return p.To
	}
	_ = name
	return ""
}

// init registers the stardew adapter factory under kind "stardew".
func init() {
	agentbridge.RegisterAdapter("stardew", func(node yaml.Node, srv *agentbridge.Server) error {
		var cfg Config
		if err := node.Decode(&cfg); err != nil {
			return fmt.Errorf("stardew adapter: decode config: %w", err)
		}
		// We synthesize a logger here because RegisterAdapter signatures
		// don't pass one through; future spec change could thread it.
		// Falling back to slog.Default is acceptable — the caller
		// (LoadAndAssemble) sets slog.Default before calling.
		a := New(cfg, slog.Default())
		return a.Register(srv)
	})
}
