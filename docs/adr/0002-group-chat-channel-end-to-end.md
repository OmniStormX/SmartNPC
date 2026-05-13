# ADR-0002: Group chat channel propagates from event to NPC reply

> **Status**: Accepted — implemented on `rebuild`, tests pinned at
> `TestFormatForHermes_ChatReceived{GroupSource,GroupEmptyID,PrivateLegacy}`,
> pending E2E verification
> **Date**: 2026-05-12
> **Context**: M5 follow-up F3 (group chat channel routing end-to-end)
> **Supersedes**: —
> **Related**: [ADR-0001](./0001-synthetic-events-go-through-hermesrelay.md),
> [`docs/adr/drafts/synthetic-events-doc-patches.md`](./drafts/synthetic-events-doc-patches.md)
> F3 section, [`docs/protocol.md`](../protocol.md), [`docs/events.md`](../events.md)

---

## Context

The delegate-fix PR (synthetic events → hermesrelay, ADR-0001, 5 commits
on `rebuild` 2026-05-12) closed the inter-NPC routing gap. While
auditing the M5 fan-out we found a **separate, pre-existing** group-chat
regression that the wiring fix does not touch.

The `channel` / `group_id` plumbing is partially wired:

- `chat_say` Input schema **already** carries `Channel` and `GroupID`
  fields ([`smartnpc-mcp/internal/tools/chat.go`](../../smartnpc-mcp/internal/tools/chat.go)
  Input struct).
- The SMAPI mod's `OnIncomingChatMessage` **already** branches on
  `channel == "group"` and dispatches to `_groupMgr.OnNpcReply` vs.
  the private toast/panel ([`smapi-mod/ModEntry.cs`](../../smapi-mod/ModEntry.cs)
  / [`smapi-mod/Chat/ChatHandler.cs`](../../smapi-mod/Chat/ChatHandler.cs)).
- [`smartnpc-mcp/internal/events/format.go::formatChatReceived`](../../smartnpc-mcp/internal/events/format.go)
  drops `source=player_group` and `group_id` when rendering, handing the
  profile a generic `"Someone in the chat says: <text>"`.
- The 6 Hermes profiles never see group context, so their `chat_say`
  call omits `channel` / `group_id`, and the mod treats the reply as
  private — replies leak from the group panel into the private NPC panel.

The protocol layer + mod layer are correct end-to-end; the regression
is **purely at the mcp ↔ profile boundary**, where the rendering layer
strips the group metadata before the LLM sees it.

## Decision

Inject group context at the **rendering layer**
(`events.FormatForHermes` / `formatChatReceived`) so the inbound
`instructions` string the profile receives explicitly tells the LLM
"you are in group chat `<group_id>`". The profile's reply policy then
mirrors the channel back through `chat_say(channel="group", group_id=...)`
and the mod's existing group-dispatch path takes over.

The rendered group-context line (pinned in
[`events/format.go::formatChatReceived`](../../smartnpc-mcp/internal/events/format.go)):

```
[group_chat group_id="<id>"] Player says in the group: <text> (any chat_say reply must include channel="group" and group_id="<id>"; tool calls and silence remain valid)
```

The prefix is a structured tag (`[group_chat group_id=...]`) followed by
the player line and a conditional parenthetical: the hint binds **only
the arguments of `chat_say` if the profile decides to speak**, not the
decision to speak. Earlier wording (`(reply via chat_say with ...)`)
was observed to short-circuit the profile's normal tool-evaluation
flow — the LLM read it as "next step is `chat_say`" and skipped
`game_*` / `npc_send_message` / movement tools that it would have
called in private chat. The reworded hint preserves the
`channel="group"` visual prominence that the original risk section
required, while explicitly carving out tool calls and silence as still
valid responses. The legacy private rendering
(`Someone in the chat says: <text>`) is retained for
`source=player` / empty / missing-group-id cases as a defensive
fallback.

A small, generic SKILL (`group-chat-reply/SKILL.md`) added to all 6
profiles teaches the same contract in long form: group context only
constrains `chat_say` arguments — `game-tool-policy`'s
query-before-claiming, `inter-npc-message`'s peer-DM routing, and
movement tools all still apply identically to private chat. This skill
carries no NPC-specific literals beyond `{{NPC_NAME}}` in metadata
and benefits from F1's placeholder mechanism without requiring any
per-profile customization (see ADR-0003).

## Alternatives considered

### A. New event type `group_chat_received`

Introduce a distinct `chat_received` sibling event that the mod emits
only when `source == player_group`.

Rejected because:

- The mod **already** carries `source` and `group_id` fields on the
  existing `chat_received` event ([`smapi-mod/Chat/ChatHandler.cs`](../../smapi-mod/Chat/ChatHandler.cs)).
  A second event type duplicates the envelope without adding new
  routing capability.
- Bloats the protocol surface (`docs/protocol.md`, `events.go` typed
  structs, `format.go` switch, profile SKILL guidance) for one bit of
  metadata that already fits in the existing event.
- Creates a "which event do I subscribe to?" question for every future
  consumer (mod-side, profile-side, dev harness) that didn't exist
  before.

### B. mcp backend intercepts `chat_say` and auto-fills `channel` / `group_id`

Have `smartnpc-mcp` track conversation state (which NPC is currently in
which group) and silently inject the channel/group fields into outgoing
`chat_say` calls when the last inbound was a group event.

Rejected because:

- Violates the **stateless bridge** principle — `smartnpc-mcp` does not
  persist conversation state today (CLAUDE.md "边界原则": *"smartnpc-mcp
  不持久化状态，只做协议桥"*); turning it into one for this single feature
  is a large architectural concession.
- Requires per-NPC turn-tracking + correlation logic the bridge layer
  has carefully avoided (Hermes is the source of truth for NPC state).
- The profile would still not "know" it's in group context, so any
  reasoning that depends on group awareness (addressing multiple
  participants, adjusting tone) silently fails. Auto-injection patches
  the symptom, not the cause.

### C. Mod-side echo with marker event

When the player sends to a group, have the mod emit both `chat_received`
AND a synthetic marker event (`group_context_set` or similar) the
profile correlates with the next chat by timestamp.

Rejected because:

- Adds a second event the profile must reason about, with no upside
  over rendering-layer injection.
- Introduces ordering/correlation fragility (what if `chat_received`
  arrives before the marker? what if marker is missed?).
- The mod-side fan-out already has all the metadata needed; pushing
  that metadata up through the existing event is strictly simpler than
  emitting an auxiliary event.

## Consequences

### Positive

- Group-chat user flow works end-to-end with the existing mod fan-out
  + protocol fields; no protocol-surface changes beyond doc fixes.
- Fix lives in one rendering function plus a generic SKILL — small
  review surface, single fault domain.
- Profile gains explicit group awareness (can reason about
  participants), which is a prerequisite for any future
  multi-addressee / heart-tier-conditional group behavior.
- Reuses the F1 placeholder mechanism for free — no NPC-specific
  literals in `group-chat-policy/SKILL.md`.

### Negative / risks

- 6 profiles must all carry the new SKILL; drift between profiles is
  the same risk F1/F2 address structurally (placeholder + render).
- Profile must be taught to **always** check group context on inbound
  chat — a profile that forgets this regresses to the current symptom
  silently. Mitigated by the rendering layer making the group-id
  visible inline in the inbound `instructions` (i.e. the LLM sees a
  prompt that mentions `group_id`, hard for it to ignore).
- A profile that hallucinates a `group_id` for a non-group inbound
  would mis-route a reply into a non-existent group. The mod-side
  `OnIncomingChatMessage` should validate that `group_id` exists
  before dispatching, falling back to private if not — to be confirmed
  during F3 implementation.

## Pinned tests

Pinned in
[`smartnpc-mcp/internal/events/events_test.go`](../../smartnpc-mcp/internal/events/events_test.go):

- `TestFormatForHermes_ChatReceivedGroupSource` — `source=player_group`
  with non-empty `group_id` renders the structured prefix and the
  inline `channel="group"` / `group_id="<id>"` hint.
- `TestFormatForHermes_ChatReceivedGroupEmptyID` — `source=player_group`
  with empty `group_id` falls back to the legacy `"Someone in the chat
  says: ..."` rendering and emits no `group_id` / `channel="group"`
  substring (defensive fallback).
- `TestFormatForHermes_ChatReceivedPrivateLegacy` — `source=player` (and
  empty `source`) preserve the exact legacy `"Someone in the chat says:
  <text>"` string with no group leakage (regression guard).

Still TODO (manual / E2E):

- `pipeline_test.go` fixture asserting the group-rendered `instructions`
  reaches the hermesrelay outbound POST body intact.
- End-to-end profile reply mirrors `channel="group"` + matching
  `group_id` back through `chat_say` (qa-tester live scenario, not a
  unit test).

## Implementation notes

Landed on `rebuild`:

- [`internal/events/events.go::ChatReceived`](../../smartnpc-mcp/internal/events/events.go)
  carries `Text` (string), `Source` (string enum: `"player"` |
  `"player_group"`), and `GroupID` (string, `omitempty`, present iff
  `source == "player_group"`). The doc-comment on the struct enumerates
  the source enum so future schema additions stay typechecked.
- [`internal/events/format.go::formatChatReceived`](../../smartnpc-mcp/internal/events/format.go)
  branches on `source == "player_group" && group_id != ""` to emit the
  structured group-context prefix; everything else (private legacy +
  defensive empty-group-id path) keeps the original
  `"Someone in the chat says: ..."` rendering.
- [`internal/events/events_test.go`](../../smartnpc-mcp/internal/events/events_test.go)
  pins all three branches (see "Pinned tests" above).
- Doc surface synced via doc-coord patches:
  [`docs/protocol.md`](../protocol.md) `chat_say` + `chat_received`
  tables (P-F3-1 / P-F3-2),
  [`docs/events.md`](../events.md) `chat_received` group-source
  rendering note (P-F3-3),
  [`docs/architecture.md`](../architecture.md) diagram caption
  parenthetical (P-F3-4),
  [`docs/mcp-tools.md`](../mcp-tools.md) `chat_say` description
  (P-F3-5),
  [`smartnpc-mcp/internal/tools/chat.go`](../../smartnpc-mcp/internal/tools/chat.go)
  `Description` MUST-language for `channel="group"` (P-F3-6).

Profile-side rollout note: the rendered prompt itself names the exact
`chat_say` arguments inline, so a per-profile `group-chat-policy/SKILL.md`
is **not** required for the happy path. A SKILL would only be needed if
profiles start ignoring the inline hint — to be revisited after E2E
verification.

## Verification

End-to-end: load a save with group chat enabled, open the group panel,
type one line addressed to 2+ audible NPCs. Both NPCs reply in the
group panel (not the private toast). The mod's `_groupMgr.OnNpcReply`
sees `channel="group"` + correct `group_id` for each reply.

Depends on: delegate-fix PR (`rebuild`) merged + mcp-engineer's
events/format.go diff landed + 6× profile SKILL added.

When this passes on real hardware, flip this ADR's status from
**Proposed** to **Accepted** and update [`docs/roadmap.md`](../roadmap.md)
M5 follow-up F-3 row.
