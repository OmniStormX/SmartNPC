# ADR-0004: agent-bridge framework extraction

Date: 2026-05-22
Status: Accepted (PR 0–5 landed; PR 6 = this doc)

## Context

`smartnpc-mcp` started life as a Stardew-Valley-only MCP server: tool
schemas, ws bridge, event types, and Hermes relay all sat in
`internal/`, all hard-coded around SDV concepts (NPC names, seasons,
tile coordinates). The package import path read
`github.com/smartnpc/smartnpc-mcp` — a hostname that never resolved to a
real GitHub repo.

The user wants to template the runtime so future projects (other games,
non-game agent-driven applications) can reuse the
"WebSocket↔MCP↔LLM-agent" plumbing without forking SDV-specific code.
Goal: **A. generic agent-bridge framework** — core stops mentioning
"NPC" / "game"; SDV becomes the first reference adapter.

## Decision

Refactor in-place, no repo split. Final layout:

```
smartnpc-mcp/                     ← Go module github.com/OmniStormX/SmartNPC
├── pkg/                          ← public framework API
│   ├── agentbridge/              ← Server, EventSource, Backend, ToolGroup, registry
│   ├── eventbus/                 ← domain-agnostic Event{Kind, Source, Subject, Payload, Timestamp}
│   ├── transport/                ← MCP transport plumbing (HTTP today)
│   └── relay/
│       ├── hermes/               ← Hermes Gateway Backend
│       └── echo/                 ← trivial logging Backend (dev / smoke)
├── adapters/                     ← domain-specific
│   └── stardew/
│       ├── adapter.go            ← New / Register / EventSource implementation
│       ├── bridge/               ← ws protocol DTOs + WSClient
│       ├── events/               ← SDV typed payload structs + format
│       └── tools/                ← chat/game_query/npc_*/mail/player_query
├── cmd/
│   ├── smartnpc-mcp/             ← legacy SDV-specific binary (preserved)
│   └── agent-bridge/             ← generic CLI; bridge.yaml driven
└── internal/
    └── log/                      ← framework-private logger
```

The legacy `cmd/smartnpc-mcp/main.go` is kept untouched as the daily
SDV launcher; the new `cmd/agent-bridge` is the composition-driven
template for future non-SDV deployments.

## Refactor PRs

| PR | Title | Effect |
|----|-------|--------|
| 0  | rename module path | `github.com/smartnpc/smartnpc-mcp` → `github.com/OmniStormX/SmartNPC` |
| 1  | extract pkg/eventbus | domain-agnostic Event type |
| 2  | move hermesrelay to pkg/relay/hermes | physical lift; behavior unchanged |
| 3a | extract pkg/transport | RunHTTP encapsulates /mcp + /healthz + graceful shutdown |
| 3b | agentbridge Server / EventSource / Backend + echo | core abstractions; not yet consumed by main.go |
| 4  | move SDV code to adapters/stardew | `internal/{bridge,events,tools}` → `adapters/stardew/{bridge,events,tools}`; `meta.go` (ping) lifted to pkg/agentbridge |
| 5  | generic agent-bridge CLI | yaml-driven registry; stardew + hermes + echo all factory-registered |
| 6  | this ADR + adapter-guide | docs |

## Consequences

### Wins

- Framework code (`pkg/`) is importable from outside. Non-SDV adapters
  can consume `agentbridge.Server`, `eventbus.Event`, `relay.Backend`
  without dragging in SDV concepts.
- New deployments declare topology in `bridge.yaml`:
  ```yaml
  adapters:
    - kind: stardew
      config:
        ws_url: ws://127.0.0.1:18745/ws
  relays:
    - kind: hermes
      config:
        runtime_config: ./hermes/runtime-config.yaml
  transport:
    kind: http
    addr: ":3000"
  ```
- `cmd/smartnpc-mcp` zero-regression: SDV daily flow (`run.bat`,
  game-mode launches) untouched.
- Test coverage preserved: every renamed `*_test.go` moved with its
  source; new abstractions have their own unit tests.

### Spec deltas accepted during execution

- **`Kind` naming**: PR 1 documented dotted (`chat.message`,
  cloudevents-style). Reality: stardew adapter emits SDV-shaped
  `Kind` (`chat_message`) verbatim so the hermes Backend can route
  with a 1:1 mapping. Dotted style applies only to framework-internal
  events. Future adapters MAY pick either convention; the framework
  treats `Kind` as opaque.
- **Inter-NPC wake-up via generic CLI**: `npc_send_message` /
  `npc_broadcast_event` rely on `bridge.EventHandler` (legacy
  `(ctx, name, data)` signature) to wake the recipient's Hermes
  profile. The generic CLI's relay path uses
  `Backend.Forward(eventbus.Event)`, which is incompatible. As a
  result, the generic CLI cannot trigger inter-NPC wake-up today.
  cmd/smartnpc-mcp continues to support it. Unifying both routes
  onto `eventbus.Event` is a follow-up (PR 7).
- **PR 5 scope was b-route (yaml + registry + factory)** instead of
  the simpler "just rewrite main.go to use agentbridge.New". Risk
  noted: ~500 LoC of registry/factory code carries no second-adapter
  validation today.

### Costs / debts

- The `internal/events.FormatForHermes` function (and per-event
  Hermes prompt rendering) still lives under
  `adapters/stardew/events/format.go` with hermes-specific knowledge.
  The hermes Backend depends on it; a true Hermes ↔ SDV decoupling
  would require moving that translation to a separate
  `pkg/relay/hermes/translators/stardew/` package — not done.
- The HTTP transport's `/status` endpoint in `cmd/smartnpc-mcp/main.go`
  is SDV-shaped (probes per-Hermes-profile gateways). Generic
  agent-bridge deployments will get a different status surface in
  the future.
- The dispatched-event routing in legacy `makeRouter`
  (audible-NPC translation, chat-guard reset) lives in `main.go`
  and is SDV-specific. Generic CLI deployments lose those features
  today.
- `bridge.yaml` is hand-written. There's no schema validator;
  unknown kinds error at assembly time, but config typos within a
  factory's subtree only show up when that factory's `Decode` runs.

## Alternatives considered

### A. Multi-game adapter framework (B-route from chat history)

Hardcode "game NPC" as the central concept; expose
`adapter.NPCAdapter` with `MoveTo / Speak / Listen` interfaces.

Rejected: forces every future use case into the NPC vocabulary.
A non-game agent (e.g. desktop assistant) would have to fake an NPC
to plug in.

### B. Repo split (agent-bridge core in its own repo)

Rejected: maintenance cost doubles immediately, no second consumer to
justify it. Single-repo monorepo with `pkg/` as the public surface
gives the same import experience while staying easy to refactor.

### C. Vanity import path (`go.omnistorm.dev/agent-bridge`)

Rejected: requires running a meta-tag server. Module path now matches
the GitHub URL (`github.com/OmniStormX/SmartNPC`) — can revisit if
the framework outgrows the SmartNPC repo name.

## Follow-ups (not scheduled)

- PR 7: unify inter-NPC routing onto `eventbus.Event` so the generic
  CLI can wake recipient Hermes profiles.
- PR 8: extract Hermes prompt rendering from `adapters/stardew/events`
  into a stardew-aware translator under `pkg/relay/hermes`.
- PR 9: split `docs/protocol.md` into `protocol-core.md` (eventbus +
  Backend) and `adapters/stardew-protocol.md` (SDV ws actions).
- PR 10: optional — move `smapi-mod/` and `hermes/` under
  `examples/smartnpc/` to reposition the repo as
  "agent-bridge framework + SmartNPC reference".
