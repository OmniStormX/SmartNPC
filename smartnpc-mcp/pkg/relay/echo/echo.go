// Package echo provides a trivial agentbridge.Backend that logs every
// received event. Intended for dev / smoke-testing the dispatch fan-out
// without spinning up a real LLM agent.
package echo

import (
	"context"
	"log/slog"

	"github.com/OmniStormX/SmartNPC/pkg/eventbus"
)

// Backend is the no-op echo backend. The zero value is usable; Logger
// defaults to slog.Default().
type Backend struct {
	Logger *slog.Logger
}

// Name implements agentbridge.Backend.
func (b *Backend) Name() string { return "echo" }

// Forward implements agentbridge.Backend by logging the event at INFO.
// Always returns nil — the echo backend has nothing to fail at.
func (b *Backend) Forward(_ context.Context, ev eventbus.Event) error {
	log := b.Logger
	if log == nil {
		log = slog.Default()
	}
	log.Info("echo backend received event",
		"kind", ev.Kind,
		"source", ev.Source,
		"subject", ev.Subject,
		"payload_bytes", len(ev.Payload),
	)
	return nil
}
