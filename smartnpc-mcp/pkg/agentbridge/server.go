package agentbridge

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/OmniStormX/SmartNPC/pkg/eventbus"
)

// ToolGroup registers a coherent set of MCP tools on the underlying
// mcp.Server. Adapters typically expose one ToolGroup per domain
// concern (chat, perception, movement, ...).
//
// Implemented as a function rather than an interface because tool
// registration is stateless from the framework's point of view: the
// adapter has already captured whatever state (ws clients, config) it
// needs in the closure.
type ToolGroup func(*mcp.Server) error

// Server is the agent-bridge composition root.
//
// It wraps a single *mcp.Server and tracks the EventSources and Backends
// the runtime should drive when Run is called. Compose by chaining
// AttachEventSource / AttachBackend / Mount calls before Run.
type Server struct {
	mcp     *mcp.Server
	logger  *slog.Logger
	mu      sync.Mutex
	sources []namedSource
	backends []Backend
	groups   []namedGroup // diagnostics; ToolGroups run synchronously at Mount time
}

type namedSource struct {
	name string
	src  EventSource
}

type namedGroup struct {
	name string
}

// Options control New. All fields are optional.
type Options struct {
	// Logger is used for framework-level info / warn output. Defaults to
	// slog.Default() when nil.
	Logger *slog.Logger
}

// New constructs a Server wrapping the given mcp.Server. The mcp.Server
// is owned by the caller (so callers can pass through ServerOptions
// freely); Server only borrows it for tool registration and notification
// fan-out.
func New(m *mcp.Server, opts Options) *Server {
	if m == nil {
		panic("agentbridge.New: mcp.Server is required")
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	return &Server{
		mcp:    m,
		logger: opts.Logger,
	}
}

// MCP returns the underlying mcp.Server. Adapters that need to register
// tools without going through Mount (e.g. one-off bespoke tools) can
// reach in directly. Most adapters should prefer Mount.
func (s *Server) MCP() *mcp.Server {
	return s.mcp
}

// Mount registers a ToolGroup on the underlying mcp.Server. The name is
// purely for diagnostics. Returns the registration error from fn unchanged.
//
// Mount is synchronous: tools are visible to MCP clients immediately
// after this call returns.
func (s *Server) Mount(name string, fn ToolGroup) error {
	if fn == nil {
		return fmt.Errorf("agentbridge.Mount: %q has nil ToolGroup", name)
	}
	if err := fn(s.mcp); err != nil {
		return fmt.Errorf("agentbridge.Mount(%q): %w", name, err)
	}
	s.mu.Lock()
	s.groups = append(s.groups, namedGroup{name: name})
	s.mu.Unlock()
	s.logger.Info("agentbridge: mounted tool group", "name", name)
	return nil
}

// AttachEventSource registers an EventSource to be started by Run.
// Sources are NOT started immediately — Run is the launcher.
func (s *Server) AttachEventSource(src EventSource) {
	if src == nil {
		return
	}
	s.mu.Lock()
	s.sources = append(s.sources, namedSource{name: src.Name(), src: src})
	s.mu.Unlock()
	s.logger.Info("agentbridge: attached event source", "name", src.Name())
}

// AttachBackend registers a Backend that will receive every dispatched
// event. Order of attachment is preserved; Forward is invoked
// sequentially in attach order. A backend that takes its time delays the
// next backend's Forward — backends are expected to internalize any
// blocking work (the Hermes implementation, for example, fires a
// goroutine per POST).
func (s *Server) AttachBackend(b Backend) {
	if b == nil {
		return
	}
	s.mu.Lock()
	s.backends = append(s.backends, b)
	s.mu.Unlock()
	s.logger.Info("agentbridge: attached backend", "name", b.Name())
}

// Run starts every attached EventSource in its own goroutine, then blocks
// until ctx is canceled. Each source runs concurrently; sink callbacks
// fan out to all attached Backends sequentially in attach order. Errors
// from individual sources are logged but never abort the others.
//
// This method does NOT itself host an MCP transport — callers are
// expected to drive mcp.Server.Run separately (e.g. via pkg/transport
// for HTTP, or directly for stdio). Run focuses on the adapter / relay
// dispatch loop.
func (s *Server) Run(ctx context.Context) error {
	s.mu.Lock()
	sources := append([]namedSource(nil), s.sources...)
	s.mu.Unlock()

	if len(sources) == 0 {
		s.logger.Warn("agentbridge.Run: no event sources attached; only mcp tools are reachable")
		<-ctx.Done()
		return ctx.Err()
	}

	sink := s.dispatch
	var wg sync.WaitGroup
	for _, ns := range sources {
		wg.Add(1)
		go func(ns namedSource) {
			defer wg.Done()
			s.logger.Info("agentbridge: starting event source", "name", ns.name)
			if err := ns.src.Start(ctx, sink); err != nil && !errors.Is(err, context.Canceled) {
				s.logger.Warn("agentbridge: event source ended with error",
					"name", ns.name, "err", err)
			}
		}(ns)
	}

	<-ctx.Done()
	wg.Wait()
	return ctx.Err()
}

// dispatch is the Sink implementation Run hands to every EventSource.
// Iterates Backends in attach order; logs errors but does not stop.
func (s *Server) dispatch(ctx context.Context, ev eventbus.Event) {
	s.mu.Lock()
	backends := append([]Backend(nil), s.backends...)
	s.mu.Unlock()
	for _, b := range backends {
		if err := b.Forward(ctx, ev); err != nil {
			s.logger.Warn("agentbridge: backend forward failed",
				"backend", b.Name(), "kind", ev.Kind, "source", ev.Source, "err", err)
		}
	}
}
