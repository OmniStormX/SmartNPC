package agentbridge

import (
	"context"

	"github.com/OmniStormX/SmartNPC/pkg/eventbus"
)

// Sink is how an EventSource hands events to the agent-bridge core. It is
// a thin callback rather than a channel so adapters with existing
// callback-driven plumbing (e.g. ws_client.SetEventHandler) can plug in
// without rewriting their internals.
//
// Sink is safe to call from any goroutine and is non-blocking from the
// adapter's point of view (the implementation Server provides may itself
// fan out asynchronously). The provided ctx is the context Server.Run was
// called with; cancellation propagates here.
type Sink func(ctx context.Context, ev eventbus.Event)

// EventSource is anything that produces eventbus.Events for the bridge to
// dispatch. Adapters (e.g. adapters/stardew) implement this by translating
// their wire-format events (ws frames, polled state, ...) into the generic
// envelope.
//
// Lifecycle:
//
//   - Server.AttachEventSource registers the source.
//   - Server.Run calls Start(ctx, sink) once, in its own goroutine.
//   - Start SHOULD return when ctx is canceled. Long-lived sources block
//     until then; one-shot sources MAY return immediately after emitting.
//   - Returning an error from Start is logged but does not abort the
//     server — other sources keep running.
type EventSource interface {
	// Name is a short stable identifier used in logs.
	Name() string

	// Start runs the source. It MUST honor ctx cancellation. Sink is
	// goroutine-safe; calling it after ctx is canceled is harmless.
	Start(ctx context.Context, sink Sink) error
}
