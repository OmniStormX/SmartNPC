# Adapter Guide

How to plug a new domain into the agent-bridge framework. Companion to
[ADR-0004](adr/0004-agent-bridge-extraction.md).

## What an adapter is

An adapter is the glue between **a specific application's wire
protocol** (a game's mod API, a desktop service, an IoT bus, ...) and
the framework-shaped abstractions in `pkg/`. It owns:

1. **Transport** — how it talks to the application (e.g. WebSocket).
2. **Tools** — the MCP tools the LLM agent can call to query / act on
   the application.
3. **Events** — translating application-specific events into
   `eventbus.Event` for relay backends to consume.

The reference adapter is `adapters/stardew/`. Skim its layout before
starting a new one.

## Minimum viable adapter

A new adapter `adapters/myadapter/` needs four things:

### 1. Config struct

```go
package myadapter

type Config struct {
    Endpoint string `yaml:"endpoint"`
    APIKey   string `yaml:"api_key"`
}
```

This is the slice of `bridge.yaml` your adapter owns:

```yaml
adapters:
  - kind: myadapter
    config:
      endpoint: https://example.com/api
      api_key: ${MY_API_KEY}
```

### 2. Adapter struct + EventSource impl

```go
type Adapter struct {
    cfg    Config
    client *yourTransport
    logger *slog.Logger
}

func New(cfg Config, logger *slog.Logger) *Adapter { ... }

// Name returns the adapter's identifier in logs.
func (a *Adapter) Name() string { return "myadapter" }

// Start blocks until ctx is canceled. Translate every incoming
// application event into eventbus.Event and pass to sink.
func (a *Adapter) Start(ctx context.Context, sink agentbridge.Sink) error {
    a.client.OnEvent(func(name string, payload []byte) {
        ev := eventbus.Event{
            Kind:    name,        // your domain's event name verbatim
            Source:  "myadapter",  // matches Name()
            Subject: extractSubject(name, payload),
            Payload: payload,
        }
        sink(ctx, ev)
    })
    if err := a.client.Connect(ctx); err != nil {
        a.logger.Warn("connect failed; retrying in background", "err", err)
    }
    <-ctx.Done()
    return a.client.Close()
}
```

### 3. Register tools + EventSource

```go
func (a *Adapter) Register(srv *agentbridge.Server) error {
    srv.AttachEventSource(a)
    return srv.Mount("myadapter/tools", func(s *mcp.Server) error {
        // Use mcp.AddTool to register one tool per domain action.
        registerMyTools(s, a.client)
        return nil
    })
}
```

### 4. Factory registration

```go
import "gopkg.in/yaml.v3"

func init() {
    agentbridge.RegisterAdapter("myadapter", func(node yaml.Node, srv *agentbridge.Server) error {
        var cfg Config
        if err := node.Decode(&cfg); err != nil {
            return fmt.Errorf("myadapter: decode config: %w", err)
        }
        return New(cfg, slog.Default()).Register(srv)
    })
}
```

To make `agent-bridge` find your adapter at runtime, add the side-effect
import to `cmd/agent-bridge/main.go`:

```go
import _ "github.com/OmniStormX/SmartNPC/adapters/myadapter"
```

## Tool design rules

See [`.codebuddy/rules/mcp-tool-design.mdc`](../.codebuddy/rules/mcp-tool-design.mdc)
for the full ruleset. Highlights:

- Tool name format: `<domain>_<verb>` lowercase snake_case
  (`myadapter_query`, `myadapter_act`).
- Input/Output structs MUST have `json` + `jsonschema` tags. First
  Output field is `OK bool`.
- Handler returns `(nil, output, err)` — let the SDK fill content.
- Every tool gets an end-to-end test using `InMemoryTransport`. See
  `pkg/agentbridge/meta_test.go` (ping) for the minimal template.

## Event design rules

- `Kind` is opaque to the framework. Pick a convention that matches
  your application's existing event names. The stardew adapter emits
  `chat_message`, `npc_interact`, etc. directly.
- `Source` should be a short ASCII identifier matching `Adapter.Name()`.
- `Subject` is best-effort — extract a principal (NPC name, user id,
  channel id) so backends can route without re-parsing payload.
- `Payload` is your domain-typed struct, JSON-encoded. Document the
  shape under `docs/adapters/<myadapter>-events.md` so backend
  authors know what to expect.

## Reusing relay backends

Backends consume `eventbus.Event`. The bundled backends:

| Kind | Package | What it does |
|------|---------|--------------|
| `hermes` | `pkg/relay/hermes` | POST `/v1/responses` to Hermes Gateway profiles, fanned out by per-profile NPC filter |
| `echo` | `pkg/relay/echo` | INFO-log every received event (dev / smoke) |

`hermes` interprets `Kind` as a Hermes "event name" verbatim. Your
adapter's events flow through it unchanged. The downside: Hermes
profiles' SKILL.md files are written against SDV vocabulary. Pointing a
non-SDV adapter at Hermes will produce events Hermes doesn't know how
to react to until you write a corresponding profile + skills.

## Testing

For each new adapter:

1. **Unit test the EventSource** — mock the transport, sink-side
   should receive `eventbus.Event` values matching expectations.
2. **End-to-end test the tools** — use `mcp.NewInMemoryTransports`
   (see `adapters/stardew/tools/meta_test.go`-equivalent for the
   pattern).
3. **Smoke test the registry wiring** — load a tiny `bridge.yaml`
   declaring your adapter and call `cfg.Assemble`. Confirms
   `init()` registration ran. See
   `cmd/agent-bridge/main_test.go::TestAssemble_StardewEcho` for
   the template.

## Anti-patterns

- **Don't import another adapter's package.** Adapters are siblings;
  they communicate via `eventbus.Event` through the framework.
- **Don't put domain logic in `pkg/`.** That layer must remain
  game-agnostic. SDV-specific helpers stay in `adapters/stardew/`.
- **Don't bypass the transport abstraction.** If you need stdio + HTTP
  + something new, propose a new transport in `pkg/transport/`
  instead of inlining a `net.Listen` in your adapter.
- **Don't use the global registry from inside tests.** The registry is
  process-global mutable state. Tests share-state across packages
  unless they explicitly install factories under
  `withFreshRegistry` or equivalent (see
  `pkg/agentbridge/config_test.go`).

## Concrete examples to read

- `adapters/stardew/adapter.go` — full reference implementation.
- `adapters/stardew/tools/registry.go` — grouped tool registration.
- `adapters/stardew/events/events.go` — typed event payloads.
- `pkg/relay/echo/echo.go` — minimal Backend (50 lines).
- `pkg/relay/hermes/factory.go` — non-trivial Backend with yaml config.
