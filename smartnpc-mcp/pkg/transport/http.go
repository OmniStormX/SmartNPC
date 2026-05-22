// Package transport hosts MCP transports for the agent-bridge.
//
// Today only Streamable HTTP lives here. Stdio is a one-liner against
// mcp.StdioTransport that callers handle inline.
package transport

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// HTTPOptions configures RunHTTP.
//
// Mux is the extension point: callers that need extra endpoints (e.g. an
// adapter-specific /status) pre-register them on a *http.ServeMux and
// pass it in. RunHTTP additionally registers /mcp + /mcp/ + /healthz on
// the same mux. When Mux is nil, RunHTTP allocates one.
type HTTPOptions struct {
	// Addr is the listen address, e.g. ":3000". Required.
	Addr string

	// AllowAnyOrigin disables DNS-rebinding / origin checks in the
	// underlying mcp Streamable HTTP handler. Required for cross-host
	// clients (e.g. Hermes inside WSL hitting the Windows host IP).
	AllowAnyOrigin bool

	// Mux is the http.ServeMux RunHTTP attaches to. Optional; nil means
	// "allocate a fresh one". Callers wanting custom endpoints should
	// pre-register them and pass in.
	Mux *http.ServeMux

	// ReadHeaderTimeout caps how long the server waits for the request
	// headers. Defaults to 5s when zero.
	ReadHeaderTimeout time.Duration

	// ShutdownTimeout caps the graceful shutdown when ctx is canceled.
	// Defaults to 5s when zero.
	ShutdownTimeout time.Duration
}

// RunHTTP serves the given mcp.Server over Streamable HTTP. Blocks until
// ctx is canceled or the listener errors. On ctx cancel, performs a
// graceful shutdown bounded by ShutdownTimeout. Returns nil on clean
// shutdown (ctx canceled or http.ErrServerClosed).
func RunHTTP(ctx context.Context, logger *slog.Logger, server *mcp.Server, opts HTTPOptions) error {
	if logger == nil {
		logger = slog.Default()
	}
	if opts.ReadHeaderTimeout == 0 {
		opts.ReadHeaderTimeout = 5 * time.Second
	}
	if opts.ShutdownTimeout == 0 {
		opts.ShutdownTimeout = 5 * time.Second
	}
	mux := opts.Mux
	if mux == nil {
		mux = http.NewServeMux()
	}

	mcpHandler := mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return server },
		&mcp.StreamableHTTPOptions{
			// Cross-host MCP clients (Hermes in WSL → Windows host) need
			// localhost-protection off because the listener is :PORT, not
			// 127.0.0.1:PORT, and the Origin header carries the host IP.
			DisableLocalhostProtection: opts.AllowAnyOrigin,
		},
	)
	mux.Handle("/mcp", mcpHandler)
	mux.Handle("/mcp/", mcpHandler)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	httpServer := &http.Server{
		Addr:              opts.Addr,
		Handler:           mux,
		ReadHeaderTimeout: opts.ReadHeaderTimeout,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), opts.ShutdownTimeout)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()

	logger.Info("listening on streamable HTTP", "addr", opts.Addr, "endpoint", "/mcp")
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
