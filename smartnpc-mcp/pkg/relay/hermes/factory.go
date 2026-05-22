package hermesrelay

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"gopkg.in/yaml.v3"

	"github.com/OmniStormX/SmartNPC/pkg/agentbridge"
	"github.com/OmniStormX/SmartNPC/pkg/eventbus"
)

// factoryConfig is the yaml shape under `relays[i].config:` for the
// hermes relay.
//
// runtime_config points at the multi-profile YAML file (the same one
// previously consumed by the legacy --hermes-config flag). All other
// per-profile knobs (gateway_url, conversation, model, npc_filter, ...)
// live in that file, not here.
type factoryConfig struct {
	RuntimeConfig string `yaml:"runtime_config"`
}

// init registers the hermes relay factory under kind "hermes". The
// returned Backend wraps a *Group so the existing multi-profile fan-out
// (per-NPC filtering, conversation routing) is reused as-is.
func init() {
	agentbridge.RegisterRelay("hermes", func(node yaml.Node, logger *slog.Logger) (agentbridge.Backend, error) {
		var fc factoryConfig
		if err := node.Decode(&fc); err != nil {
			return nil, fmt.Errorf("hermes relay: decode config: %w", err)
		}
		if fc.RuntimeConfig == "" {
			return nil, fmt.Errorf("hermes relay: config.runtime_config is required")
		}
		cfgs, err := LoadConfigFile(fc.RuntimeConfig)
		if err != nil {
			return nil, err
		}
		group, err := NewGroup(cfgs, logger)
		if err != nil {
			return nil, err
		}
		return &groupBackend{group: group}, nil
	})
}

// groupBackend adapts a *Group (which speaks the legacy
// EventHandler signature: ctx, name string, data json.RawMessage) to
// the agent-bridge Backend interface.
//
// The translation is intentionally trivial: ev.Kind is used as the
// SDV event name verbatim, and ev.Payload is the raw data. Adapters
// (e.g. adapters/stardew) are expected to produce events with Kind
// values matching the SDV protocol's event-name strings ("chat_message",
// "npc_interact", ...) so this 1-to-1 mapping holds.
type groupBackend struct {
	group *Group
}

func (g *groupBackend) Name() string { return "hermes" }

func (g *groupBackend) Forward(ctx context.Context, ev eventbus.Event) error {
	g.group.HandleEvent(ctx, ev.Kind, json.RawMessage(ev.Payload))
	return nil
}
