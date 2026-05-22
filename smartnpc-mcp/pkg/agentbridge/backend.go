// Package agentbridge is the core of the agent-bridge framework: a thin
// composition layer over the MCP go-sdk that wires together event sources
// (adapters) and relay backends (LLM agent runtimes).
//
// Domain knowledge lives in adapters (e.g. adapters/stardew). The core is
// game-agnostic and never interprets eventbus.Event payloads.
package agentbridge

import (
	"context"

	"github.com/OmniStormX/SmartNPC/pkg/eventbus"
)

// Backend is a sink for events. The most common implementation is the
// Hermes Gateway relay (pkg/relay/hermes), which translates each Event
// into a Hermes /v1/responses POST. Test code may use pkg/relay/echo.
//
// Forward is invoked synchronously by Server.dispatch but each backend is
// expected to NOT block on slow downstream work — the canonical pattern
// is to enqueue / fire a goroutine internally. A blocking Forward stalls
// every other backend on the same Server.
//
// Errors are logged but do not abort the dispatch fan-out: a misbehaving
// backend MUST NOT take out its peers. Backends should also recover from
// panics internally; the framework does not wrap them in recover().
type Backend interface {
	// Name is a short stable identifier used in logs and registry lookups.
	// Lowercase ASCII, e.g. "hermes", "echo".
	Name() string

	// Forward delivers ev to the backend. Returning an error is purely
	// informational — Server logs it and moves on. Backends that need
	// retry / persistence semantics own that internally.
	Forward(ctx context.Context, ev eventbus.Event) error
}
