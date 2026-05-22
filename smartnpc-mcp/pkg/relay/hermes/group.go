package hermesrelay

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/OmniStormX/SmartNPC/internal/events"
)

// Group fans an event out to every Relay whose NPC filter matches. Use this
// when smartnpc-mcp is wired against multiple Hermes profiles (multi-NPC
// runtime). A Group with a single Relay degenerates to the single-target
// behavior of the legacy --hermes-url path.
type Group struct {
	relays []*Relay
	logger *slog.Logger
}

// NewGroup constructs one Relay per Config and wraps them in a Group. Returns
// an error when configs is empty or any constituent Relay fails to construct.
func NewGroup(configs []Config, logger *slog.Logger) (*Group, error) {
	if len(configs) == 0 {
		return nil, fmt.Errorf("hermesrelay: NewGroup requires at least one config")
	}
	if logger == nil {
		logger = slog.Default()
	}
	relays := make([]*Relay, 0, len(configs))
	for i, cfg := range configs {
		r, err := New(cfg, logger)
		if err != nil {
			return nil, fmt.Errorf("hermesrelay: profile %d: %w", i, err)
		}
		relays = append(relays, r)
	}
	return &Group{relays: relays, logger: logger}, nil
}

// HandleEvent implements bridge.EventHandler. It asks each Relay whether the
// event matches its filter (via Relay.ShouldRoute) and dispatches to all that
// match. When no relay matches, the event is dropped with a debug log so an
// unrouteable NPC name surfaces during diagnosis.
func (g *Group) HandleEvent(ctx context.Context, name string, data json.RawMessage) {
	matched := 0
	for _, r := range g.relays {
		if !r.ShouldRoute(name, data) {
			continue
		}
		r.HandleEvent(ctx, name, data)
		matched++
	}
	if matched == 0 {
		recipient, _, _ := events.RecipientNPC(name, data)
		g.logger.Debug("hermesrelay group: no profile matched event, dropping",
			"event", name, "recipient", recipient)
	}
}

// Relays exposes the underlying slice for diagnostics (e.g. main.go logging).
// Callers MUST NOT mutate the slice.
func (g *Group) Relays() []*Relay {
	return g.relays
}
