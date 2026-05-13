# Doc patches — synthetic events → hermesrelay wiring fix

> **Author**: docs-coord
> **Reviewers**: team-lead (please approve before I apply)
> **Status**: Drafts — staged for apply. Verified against `rebuild` working tree at 2026-05-12 (mcp-engineer's wiring landed, uncommitted). Anchors are stable, absolute line numbers will shift on lint passes — patches use relative anchors (function names, surrounding text) so they survive minor formatter churn.
> **Companion**: [`docs/adr/0001-synthetic-events-go-through-hermesrelay.md`](./adr/0001-synthetic-events-go-through-hermesrelay.md)

These edits keep the doc surface consistent with the wiring change
(synthetic events fanned out by `npc_send_message` / `npc_broadcast_event`
now pass through the same `bridge.EventHandler` chain mod events use, so
the hermesrelay sees them and POSTs the recipient NPC's Hermes Gateway).

Each patch is shown as a unified diff fragment for clarity. None of these
flip an existing claim — Plan A's exclusion in `hermes-event-trigger.md`
stays intact (per team-lead direction).

**Apply order (settled with team-lead 2026-05-12):**

1. wait for mcp-engineer commit hash + qa-tester `task ci-fast` green
2. fill `<!-- TODO: fill from commit <hash> -->` line refs in ADR-0001
3. apply patches 1–10
4. run `task ci-fast` (catches link-checker / pre-commit hook noise)
5. SendMessage diff summary to team-lead for review
6. commit on green light, **do not push** — bundles into delegate-fix PR
7. (separate, post-E2E commit) flip `docs/roadmap.md` 5.6 / 5.7 / 5.13 from 🧪 → ✅ + ADR-0001 status Proposed → Accepted

**Internal link path conventions (settled with team-lead 2026-05-12):**

- repo-root files (`CLAUDE.md`, `REFACTOR.md`, `README.md`) → docs:
  `docs/adr/0001-...md` (no leading slash; works in GitHub + local editors)
- inside `docs/`, sibling links: `./0001-...md`, `../events.md`
  (relative paths survive directory moves)
- never `/docs/...` with leading slash — some renderers treat it as
  absolute URL and 404 on `https://github.com/docs/...`

---

## Patch 1 — `docs/events.md`

**Why**: today the doc frames mod events vs synthetic events as having
the **same envelope** but stops short of saying they share the same
**downstream routing**. After the fix, synthetic events also flow
through hermesrelay, so the framing needs to make that explicit.

```diff
@@ d:\SmartNPC\docs\events.md @@
 1. **Mod events** — originated by the SMAPI mod, arriving over the `:18745`
    WebSocket and forwarded into MCP as logging notifications by
    [`tools.MakeEventForwarder`](../smartnpc-mcp/internal/tools/events.go).
 2. **Synthetic events** — emitted by `smartnpc-mcp` itself (e.g. inter-NPC
    messaging via `npc_send_message`). Uses the same envelope shape so
-   consumers see one uniform stream.
+   consumers see one uniform stream, and (since 2026-05-12) flows
+   through the same `bridge.EventHandler` chain mod events traverse —
+   so audible-routing, hermesrelay POST, and group-dispatch fire
+   identically regardless of source. See
+   [ADR-0001](./adr/0001-synthetic-events-go-through-hermesrelay.md).
```

And in the `## Synthetic events` section:

```diff
@@ d:\SmartNPC\docs\events.md ## Synthetic events @@
 ### `npc_message`

 Fanout of a `npc_send_message` tool call — one NPC talking privately to
 another. Each message is also persisted in an in-memory mailbox accessible
 via `npc_inbox_get` / `npc_inbox_ack`.
+
+Like mod events, this is dispatched to MCP notification subscribers AND
+POSTed to the Hermes Gateway via `hermesrelay` so the recipient NPC's
+profile wakes for a turn. The recipient is matched against
+`hermes/runtime-config.yaml` `match.npc` rules using the `to` field
+(see [`events.RecipientNPC`](../smartnpc-mcp/internal/events/events.go)).
```

---

## Patch 2 — `docs/hermes-event-trigger.md`

**Why**: the locked Plan B link diagram (lines 103–129) shows mod-side
events flowing to the relay; need to note that mcp-side synthetic events
join the same outbound path. **Plan A's exclusion is unchanged** — A is
about Hermes consuming MCP notifications inbound; the fix is still
strictly outbound HTTP POST per Plan B.

```diff
@@ d:\SmartNPC\docs\hermes-event-trigger.md ### **方案 B — 锁定** @@
 **链路**：

 ```
 SMAPI Mod
   ├──ws──> smartnpc-mcp（MCP server，连游戏）
   └──ws──> smartnpc-mcp（同一进程的事件转发器）
                 │
                 │ HTTP POST
                 v
        Hermes Gateway /v1/responses
 ```
+
+**对合成事件的扩展（2026-05-12）**：`smartnpc-mcp` 内部工具
+(`npc_send_message` / `npc_broadcast_event`) 产生的 synthetic event 走
+**同一条 outbound 路径**——经由共享的 `bridge.EventHandler` 注入
+`hermesrelay`，最终 POST 到 Hermes Gateway。这不改变 Plan B 的论断
+(Hermes 仍不消费 inbound MCP notification)；只是 outbound 入口从"仅
+ws 来源"扩展到"ws 来源 + tool-handler 来源"。Plan A 的排除依旧成立。
+详见 [ADR-0001](./adr/0001-synthetic-events-go-through-hermesrelay.md)。
```

(No edit to "方案 A — 排除" section. Confirmed per team-lead.)

---

## Patch 3 — `docs/protocol.md`

**Why**: the `npc_send_message` row in the MCP-only tools table (line
~537) currently doesn't tell a profile author that calling this tool
**will trigger another NPC's profile**. That's a load-bearing semantic
property and belongs in the description.

```diff
@@ d:\SmartNPC\docs\protocol.md ## MCP-only tools @@
-| `npc_send_message`      | WRITE       | NPC-to-NPC private message; buffered in an in-memory FIFO inbox          |
-| `npc_broadcast_event`   | WRITE       | NPC-to-all fire-and-forget event; no inbox                               |
+| `npc_send_message`      | WRITE       | NPC-to-NPC private message; buffered in an in-memory FIFO inbox AND triggers the recipient's Hermes profile via hermesrelay |
+| `npc_broadcast_event`   | WRITE       | NPC-to-all fire-and-forget event; no inbox; fans out to every routed Hermes profile via hermesrelay                          |
```

---

## Patch 4 — `REFACTOR.md`

**Why**: the M5 5.3 task line claims `npc_send_message` etc. are done,
but doesn't mention the integration with hermesrelay. After the fix
that's the more important property than the in-memory mailbox.

```diff
@@ d:\SmartNPC\REFACTOR.md M5 落地清单 @@
-| 5.3 | inter-NPC 工具 `npc_send_message` / `_broadcast_event` / `_inbox_*` | `internal/tools/npc_message.go` |
+| 5.3 | inter-NPC 工具 `npc_send_message` / `_broadcast_event` / `_inbox_*`（合成事件复用 hermesrelay outbound 路径，触发 recipient profile） | `internal/tools/npc_message.go` + ADR-0001 |
```

**Per team-lead Q2 ruling** (2026-05-12): also add a one-line navigation
pointer at the end of REFACTOR.md's M5 status narrative section (no
content duplication, just a link so readers don't lose the trail):

```diff
@@ d:\SmartNPC\REFACTOR.md M5 status narrative @@
 ... <existing M5 description ends> ...
+
+> 详见 [ADR-0001](docs/adr/0001-synthetic-events-go-through-hermesrelay.md) — synthetic events 为何复用 hermesrelay outbound 路径。
```

(Exact insertion line gets pinned during apply pass — REFACTOR.md is
narrative-style, no clean section anchor today.)

---

## Patch 5 — `smartnpc-mcp/internal/tools/npc_message.go` header comment

**Why**: the current 19–33 line comment lists **two** consumer pickup
mechanisms (push notification + pull inbox). After the fix there are
**three**, and Hermes profile gateway POST is the primary one.

```diff
@@ d:\SmartNPC\smartnpc-mcp\internal\tools\npc_message.go @@
 // Inter-NPC messaging. These tools let one NPC's Hermes profile (or the
 // legacy smartnpc-agent dev harness) send messages to other NPCs through
-// smartnpc-mcp. The recipient can pick them up in two ways:
+// smartnpc-mcp. The recipient is reached three ways, all driven from
+// the same emit site:
 //
-//  1. Push: by subscribing to MCP logging notifications with name
+//  1. Hermes profile wake (primary): the synthetic event is fed
+//     through the shared bridge.EventHandler so hermesrelay POSTs the
+//     recipient's Hermes Gateway, identical to a mod-sourced event.
+//     See ADR-0001 (docs/adr/0001-synthetic-events-go-through-hermesrelay.md).
+//
+//  2. MCP push notification: by subscribing to MCP logging notifications with name
 //     bridge.EventNpcMessage (targeted) or bridge.EventNpcBroadcast (fanout).
 //     The forwarder (internal/tools/events.go) already delivers these.
 //
-//  2. Pull: by calling npc_inbox_get. This is useful for consumers that
+//  3. Inbox pull: by calling npc_inbox_get. This is useful for consumers that
 //     can't subscribe to notifications (e.g. an agent loop driven by
 //     /v1/responses on a per-turn basis) or as a catch-up mechanism.
 //
 // The mailbox is purely in-memory and bounded per recipient; it is not
 // persisted across restarts. That's intentional — durable state lives in
 // Hermes memory, not here. See docs/events.md for the wire protocol.
```

---

## Patch 6 — `docs/architecture.md` (event-flow diagram)

**Why**: lines 95–123 show a `bridge.EventHandler chain` driven by
"smartnpc-mcp ws receive loop" with mod events as the only inputs. After
the fix, tool-handler-originated synthetic events join the same chain.
Diagram needs a second input arm so readers don't read it as
"hermesrelay only sees ws-sourced events".

```diff
@@ d:\SmartNPC\docs\architecture.md L96-L104 @@
 ```
 [player chat panel]            chat_message {npc=XiaMi, text="..."}
 [player clicks XiaMi]          npc_interact {npc=XiaMi}
 [day rolls over]               day_started {day, season, year}
                                        │
                                        ▼  (smartnpc-mcp ws receive loop)
                                bridge.EventHandler chain:
                                  1. MCP notification fan-out  (legacy MCP subscribers)
                                  2. hermesrelay.HandleEvent   ← Plan B
+
+[NPC A tool call: npc_send_message]   npc_message {from=A, to=B, text}
+[NPC A tool call: npc_broadcast]      npc_broadcast {from=A, kind, data}
+                                       │
+                                       ▼  (registerNpcMessage → emitSyntheticEvent
+                                            feeds the SAME bridge.EventHandler;
+                                            ctx is detached — see ADR-0001)
+                                  → joins step 1 + step 2 above
```

## Patch 7 — `docs/architecture.md` filtering & routing table

**Why**: line 130's `--hermes-npc` row says it routes by
`npc / to / target` fields — that's already correct since
`events.RecipientNPC` probes all three — but a reader can't tell whether
it applies to synthetic events too. One word fixes it.

```diff
@@ d:\SmartNPC\docs\architecture.md L130 @@
-| `smartnpc-mcp` hermesrelay `--hermes-npc` | Events whose `npc` / `to` / `target` matches this profile's NPC name; broadcast events (no NPC field) pass through |
+| `smartnpc-mcp` hermesrelay `--hermes-npc` | Events (mod-sourced **and** synthetic) whose `npc` / `to` / `target` matches this profile's NPC name; broadcast events (no NPC field) pass through |
```

## Patch 8 — `docs/architecture.md` process layout caption

**Why**: line 157 captions the hermesrelay outbound arrow as
"routed by event.npc" — fine for mod events, but synthetic events
typically use `event.to` (recipient field). One-word generalization
points readers at the actual probe order.

```diff
@@ d:\SmartNPC\docs\architecture.md L157 @@
-   └── hermesrelay outbound  ── routed by event.npc → matching gateway
+   └── hermesrelay outbound  ── routed by event.npc/to/target → matching gateway
```

## Patch 9 — `docs/mcp-tools.md` `npc_send_message` description

**Why**: line 181–182 says "Recipient picks it up either via MCP
notification (`npc_message`) or by polling `npc_inbox_get`" — same
"two ways" framing as `npc_message.go`'s header (Patch 5). Need the
same "three ways with Hermes profile wake as primary" rewrite.

```diff
@@ d:\SmartNPC\docs\mcp-tools.md L179-L196 @@
 ### `npc_send_message`

-Send a private message from one NPC to another. Recipient picks it up
-either via MCP notification (`npc_message`) or by polling `npc_inbox_get`.
+Send a private message from one NPC to another. Recipient is reached
+three ways from a single emit site:
+
+1. **Hermes profile wake** (primary): the synthetic event is fed
+   through the shared `bridge.EventHandler` so hermesrelay POSTs the
+   recipient's Hermes Gateway, identical to a mod-sourced event. See
+   [ADR-0001](./adr/0001-synthetic-events-go-through-hermesrelay.md).
+2. MCP push notification (`npc_message`).
+3. Inbox pull via `npc_inbox_get`.

 Inputs: `from`, `to`, `text`, optional `kind`. Errors: `invalid_params`
 when from/to/text are empty or from == to.

 ### `npc_broadcast_event`

-Fire-and-forget broadcast to every subscribed NPC. Not queued in any
-inbox. Use for world-wide signals.
+Fire-and-forget broadcast to every subscribed NPC. Not queued in any
+inbox. Use for world-wide signals. Like `npc_send_message`, fans out
+through the shared `bridge.EventHandler` — every routed Hermes profile
+receives a Gateway POST.

 Inputs: `from`, `kind`, optional `data` (JSON).
```

## Patch 10 — `npc_send_message` LLM-facing tool description in `npc_message.go`

**Why**: the tool **Description** string (the one MCP serves to the
LLM via `tools/list`) currently tells the model the recipient picks up
messages "by subscribing to MCP notifications ... or by calling
`npc_inbox_get`" — Hermes profile wake is **not** mentioned, even though
that is now the primary delivery channel. This is the description an
NPC profile reads when deciding *whether* to use the tool, so it must
reflect reality. Distinct from Patch 5 (in-file header comment for
code readers); this fixes the tool-discoverable description for the
LLM, which is the load-bearing surface for M5 task 5.2.

```diff
@@ smartnpc-mcp/internal/tools/npc_message.go registerNpcMessage / npc_send_message Description @@
-		Description: "Send a private message from one NPC to another. The recipient's " +
-			"Hermes profile (or other MCP client) can pick it up either by subscribing " +
-			"to MCP notifications (event name \"npc_message\") or by calling " +
-			"`npc_inbox_get`. Messages are buffered in-memory per recipient with a FIFO " +
-			"queue; oldest is dropped when the queue exceeds 64 entries.\n\n" +
+		Description: "Send a private message from one NPC to another. The synthetic " +
+			"event is dispatched THREE ways from a single emit site: (1) the " +
+			"recipient's Hermes profile is woken via Gateway POST through hermesrelay " +
+			"(primary — recipient takes a turn), (2) any MCP notification subscriber " +
+			"sees an `npc_message` event, (3) the message is buffered in an in-memory " +
+			"FIFO inbox readable via `npc_inbox_get`. The mailbox is bounded at 64 " +
+			"entries per recipient; oldest is dropped on overflow.\n\n" +
```

(The existing "When to call", "Constraints", ping-pong loop warning,
and side-effect lines are preserved verbatim — only the lead paragraph
changes.)

Apply the same lead-paragraph refresh to the `npc_broadcast_event`
description so the "every routed Hermes profile gets a POST" property
is in the lead sentence rather than buried in synth-event language:

```diff
@@ smartnpc-mcp/internal/tools/npc_message.go registerNpcMessage / npc_broadcast_event Description @@
-		Description: "Broadcast an event from one NPC to every other subscribed NPC " +
-			"(no explicit recipient). Fire-and-forget — the event is emitted as an " +
-			"MCP notification (name \"npc_broadcast\") but is NOT queued in any inbox. " +
-			"Consumers that are offline miss it.\n\n" +
+		Description: "Broadcast an event from one NPC to every other subscribed NPC " +
+			"(no explicit recipient). The synthetic event is dispatched two ways from " +
+			"a single emit site: (1) every routed Hermes profile receives a Gateway " +
+			"POST through hermesrelay (primary — each subscribed NPC may take a turn), " +
+			"and (2) any MCP notification subscriber sees an `npc_broadcast` event. " +
+			"Fire-and-forget — NOT queued in any inbox; offline consumers miss it.\n\n" +
```

(The "When to call", "Constraints", and "Side-effect" lines are
preserved verbatim — only the lead paragraph changes. Apply both
hunks together when E2E unblocks Patch 10.)

---

## Apply order (after mcp-engineer's diff lands)

1. Verify ctx-detachment is in place (`go hermes(context.Background(), ...)`) — block apply if not. **Verified 2026-05-12 in working tree at `internal/tools/npc_message.go::emitSyntheticEvent`.**
2. Resolve `<!-- TODO -->` line refs in ADR-0001. **Done 2026-05-12 — switched to relative anchors per team-lead's direction.**
3. Apply patches 1–10 above.
4. Run `task ci-fast` (no-op for docs but catches any frontmatter
   regressions in adjacent files).
5. Wait for qa-tester E2E pass on M5 5.6/5.7/5.13.
6. Flip `docs/roadmap.md` 5.6 / 5.7 / 5.13(delegate) from 🧪 to ✅ and
   ADR-0001 status from **Proposed** to **Accepted**.

## Open questions for team-lead

- **Q1**: ADR-0001 implementation-notes section currently has placeholder
  line refs. OK to leave them as `<!-- TODO -->` until mcp-engineer's
  diff is final, then I fill in?
- **Q2**: Patch 4 (REFACTOR.md) — there's a second M5 status table in
  the file body if I'm reading right. Should I duplicate the
  hermesrelay-integration note there, or is the落地清单 row enough?
- **Q3**: Patch 1's link `[ADR-0001](./adr/0001-...)` — preferred path
  style? `./adr/...` (relative) or `/docs/adr/...` (repo-rooted)? I see
  both styles in the repo.

---

# Follow-up patches — NOT IN THIS PR

> The patches below address a **separate, pre-existing bug** discovered while
> auditing the M5 profile fan-out. Tracked here for visibility and to keep the
> follow-up paper trail attached to the M5 doc work, but they are **scope
> outside the synthetic-events / delegate-fix PR** and apply independently.

## Patch F1 — `REFACTOR.md` (append new section, end of file)

**Why**: 5 of the 6 NPC profiles (`abigail`, `haley`, `harvey`, `penny`,
`sebastian`) have `skills/smartnpc/memory-policy/SKILL.md` files that are
verbatim copies of the xiami version, with `XiaMi` / `xiami` /
`memories/xiami/` / `conversation:xiami` left as hard-coded literals. The
heart-tier-7+ examples even carry XiaMi-specific phrasing/口癖. Net effect: the
non-xiami profiles, when their memory-policy skill activates, are instructed to
read/write under xiami's memory namespace and self-narrate as XiaMi.

This is a real bug but doesn't block the delegate-fix happy path (delegate
routing only depends on hermesrelay POST + recipient `match.npc`). The
silent-ack write path **does** exercise memory-policy SKILL guidance, so any
"NPC silently remembers" content from non-xiami profiles risks polluting
xiami's memory store. Severity: medium.

```diff
@@ d:\SmartNPC\REFACTOR.md (append at end of file) @@
+
+---
+
+## M5 follow-up — NPC name placeholder-ization in shared SKILL templates
+
+**Status**: open, post-delegate-fix
+**Severity**: medium — does not block delegate happy-path; **does** corrupt
+memory data via silent-ack write path on non-xiami profiles.
+
+### Current state
+
+Multiple shared SKILL files under `hermes/profiles/{abigail,haley,harvey,penny,sebastian}/skills/smartnpc/`
+were created by `cp` from `hermes/profiles/xiami/...` and retain hard-coded
+NPC literals. **The delegate-fix PR deliberately keeps these byte-identical
+across all 6 profiles** — the placeholder-ization work is intentionally
+out-of-scope and tracked here.
+
+Affected files (hard-coded NPC literals to be placeholder-ized) — full
+audit by `hermes-profile` 2026-05-12, grep `XiaMi|xiami|夏弥` per non-xiami profile:
+
+| File | hits/profile | Notes |
+|---|---|---|
+| `skills/smartnpc/memory-policy/SKILL.md` | 9 | `(XiaMi)` 标题、`memories/xiami/state.db` 路径、`conversation 'xiami'`、口癖示例。Frontmatter `description:` 也写 XiaMi。 |
+| `skills/smartnpc/inter-npc-message/SKILL.md` | 9 (becomes ~17 after delegate-fix rewrite) | Example A/B 段 "Player → XiaMi"、`to="Penny"` 字面例子，含中文角色名 `潘妮`/`阿比盖尔`。 |
+| `skills/smartnpc/game-tool-policy/SKILL.md` | 1 | 一句 `speaker = "XiaMi"` 当例子 (line 53)。 |
+| `config-overlay.yaml` | 1 | 第 1 行注释 `see xiami for the full template`。**仅注释，无功能影响**——可顺手清，但不阻塞。 |
+| `skills/smartnpc/proactive-greeting/SKILL.md` | 0 | 已用 hearts-tier 表写得通用，**无需改动**。 |
+| `cron-recipes.md` | n/a | **此文件仅 xiami 有**，其他 5 profile 不持有。是否分发到 6 profile 是另一个 follow-up（"分发还是保留为 xiami-only 参考食谱"），不在 F1 scope。 |
+| `SOUL.md` | n/a | 各 profile 自己的人格本，本就不共享，不在 follow-up scope。 |
+
+### Impact
+
+**memory-policy/SKILL.md**: any non-xiami profile that activates this
+SKILL is instructed to read/write the **xiami** memory namespace and to
+self-narrate in XiaMi's voice. The silent-ack path (NPC remembers
+without speaking) writes to disk, so this silently pollutes xiami's
+`memories/` directory with foreign data attributed to XiaMi.
+
+The most direct evidence (visible to anyone opening the file): the YAML
+frontmatter `description:` on `abigail/skills/smartnpc/memory-policy/SKILL.md`
+still reads "How XiaMi uses Hermes's built-in per-profile memory…" —
+i.e. Abigail's profile literally describes itself as XiaMi to its own
+SKILL loader. Equivalent text appears in haley/harvey/penny/sebastian.
+The silent-ack data-corruption path is the worst-case symptom; the
+frontmatter mismatch is the obvious-on-sight tell.
+
+**inter-npc-message/SKILL.md**: each cloned profile's delegate-flow
+examples name `XiaMi` / `Penny` / `Abigail` (and CN aliases) as the
+canonical actors. A profile reading its own SKILL sees delegate
+guidance modeled on someone else's name and may anchor on the literal
+example pair rather than abstracting "self → another NPC". Lower-
+severity than memory-policy (no data-corruption path), but degrades
+delegate routing accuracy.
+
+**Why the delegate-fix PR doesn't fix this**: the delegate-fix wiring
+(synthetic events → hermesrelay) is a `smartnpc-mcp` Go-side change
+that doesn't touch SKILL content. We could have done both at once but
+chose to keep PR scope tight: this follow-up cross-cuts 5 profiles ×
+N SKILL files plus an install-time substitution mechanism, which has a
+much broader review surface than the wiring fix.
+
+### Proposed fix
+
+Extend `hermes/install.sh`'s existing `sed`-based `__HOST_IP__` substitution
+pattern to cover SKILL files. The script already does this kind of
+install-time templating; add NPC-name placeholders and run the same `sed`
+over each SKILL on copy.
+
+Placeholder spec (xiami profile is the source-of-truth template AND a
+runnable profile — no separate `_templates/` directory):
+
+| Placeholder | Value example (abigail) | Use site |
+|---|---|---|
+| `{{NPC_NAME}}` | `Abigail` | profile's internal/PascalCase name; valid `speaker:` arg |
+| `{{NPC_DISPLAY}}` | `阿比盖尔` | Chinese display name for prose |
+| `{{NPC_DIR}}` | `abigail` | lowercase profile-dir name; for `memories/<dir>/`, `conversation:` |
+| `{{PEER_NAME}}` | `Penny` | example "other NPC" in delegate flows |
+| `{{PEER_DISPLAY}}` | `潘妮` | Chinese display for the peer |
+
+`{{NPC_DISPLAY}}` / `{{PEER_DISPLAY}}` come from a per-profile field —
+either a new `profile.yaml` next to `config-overlay.yaml`, or a `display:`
+entry pulled from the existing `runtime-config.yaml`. `hermes-profile`
+to pick the source on refactor day.
+
+Implementation sketch (`install.sh`, replacing the current `cp -r skills/`):
+
+```bash
+for src in $(find "$profile_dir/skills" -type f); do
+    rel="${src#$profile_dir/skills/}"
+    dst="$target/skills/$rel"
+    mkdir -p "$(dirname "$dst")"
+    sed -e "s|{{NPC_NAME}}|$profile|g" \
+        -e "s|{{NPC_DISPLAY}}|$display|g" \
+        -e "s|{{NPC_DIR}}|$profile|g" \
+        "$src" > "$dst"
+done
+```
+
+(Per-example `{{PEER_*}}` is per-example, not per-profile — replace at
+SKILL-author time with a deliberate peer pick, not at install time.)
+
+**Why not a `_templates/` source-of-truth tree** (rejected option):
+
+- xiami is already a runnable profile; making it also serve as the
+  template avoids a parallel directory readers must keep in sync
+- `install.sh` would need a special "ignore `_templates/`" rule
+- Local edit/Read workflows would need to track two locations
+
+### Why not in the delegate-fix PR
+
+- Different fault domain (template hygiene vs. event routing).
+- Cross-cuts 5 profiles × multiple SKILL files + an install script —
+  broader review surface.
+- Delegate-fix happy path doesn't depend on SKILL content correctness;
+  the wiring fix is independent and verifiable on its own.
+- Decision (team-lead, 2026-05-12): keep the 5 cloned profiles
+  byte-identical to xiami in this PR; placeholder-ize as a single
+  follow-up commit so the diff is auditable as one batch.
+
+### Owner
+
+`hermes-profile` (template authoring) + `docs-coord` (install.sh README and
+roadmap update).
```

## Patch F2 — `docs/roadmap.md` (insert after M5 acceptance checklist, before the `---`/M6 section)

```diff
@@ d:\SmartNPC\docs\roadmap.md M5 acceptance checklist @@
 - [ ] **M5.G**：smartnpc-agent 从启动文档下线，README 仅展示 Hermes 路径

+### M5 follow-up（不阻塞 M5 验收）
+
+| # | 内容 | 严重度 | 拥有者 |
+|---|------|--------|--------|
+| F-1 | NPC 名占位符化：`abigail` / `haley` / `harvey` / `penny` / `sebastian` 的 shared SKILL 含硬编码 NPC 字面量。`hermes-profile` 2026-05-12 完整 audit：`memory-policy/SKILL.md` (9 命中，污染 xiami memory store) + `inter-npc-message/SKILL.md` (9→17 命中，delegate 例子用错名) + `game-tool-policy/SKILL.md` (1 命中，speaker 例子) + `config-overlay.yaml` (1 命中，仅注释)。`proactive-greeting/SKILL.md` 已通用无需改。`cron-recipes.md` 仅 xiami 有，分发与否是另一个 follow-up。建议扩展 `hermes/install.sh` 现有 `sed` 替换流程，新增 `{{NPC_NAME}}` / `{{NPC_DISPLAY}}` / `{{NPC_DIR}}` + 例子 `{{PEER_NAME}}` / `{{PEER_DISPLAY}}` 五个占位符，xiami 同时作为 template + runnable profile。详见 [REFACTOR.md](../REFACTOR.md) 同名段落。 | 中 | hermes-profile + docs-coord |
+| F-2 | 占位符替换前后行为等价性回归：6 profile 替换后的 SKILL.md 在 hermes 解析（YAML frontmatter + markdown）层面无 lint 警告。最低用 `hermes profile lint <name>`，或起一个 6-profile diff-after-render 测试。F-1 落地时一并跑。 | 低 | hermes-profile |
+| F-3 | 群聊 channel 协议端到端打通：协议字段 + mod 分流均已实现，但 `chat_received` 事件渲染丢 `source=player_group`/`group_id`，且 6 份 profile 永不传 `chat_say` 的 `channel="group"`。详见 [REFACTOR.md](../REFACTOR.md) F3 段落。 | 中 | hermes-profile + mcp-engineer |
+
 ---

 ## M6 — Hermes-first 完工与归档
```

## Patch F3 — `REFACTOR.md` (append new section, after F1 placeholder-ization section)

**Why**: separate fault domain from F1/F2 (template hygiene). F3 is a
**protocol-completeness** issue — the `channel="group"` / `group_id` plumbing
exists end-to-end at the protocol + mod layer, but the bridge between mod
event and Hermes profile drops the group context, so NPC replies always land
in the private channel. Independent follow-up; no PR scope conflict with F1.

```diff
@@ d:\SmartNPC\REFACTOR.md (append after F1 section, before EOF) @@
+
+---
+
+## M5 follow-up — Group chat channel routing end-to-end
+
+**Status**: open, post-delegate-fix
+**Severity**: medium — does not block delegate happy-path; group chat core
+UX is degraded (NPC replies leak from group panel into private panel).
+
+### Current state
+
+The `channel` / `group_id` plumbing is partially wired but the bridge
+layer drops the group context, so NPCs are unaware they're in a group:
+
+| Layer | State |
+|---|---|
+| `chat_say` Input schema | ✅ already has `Channel` and `GroupID` fields ([smartnpc-mcp/internal/tools/chat.go:22-23](smartnpc-mcp/internal/tools/chat.go#L22-L23)) |
+| mod-side fan-out | ✅ `OnIncomingChatMessage` branches on `channel == "group"` and dispatches to `_groupMgr.OnNpcReply` vs the private toast/panel ([smapi-mod/ModEntry.cs:291-312](smapi-mod/ModEntry.cs#L291-L312)) + DTO declares `channel` / `group_id` ([smapi-mod/Chat/ChatHandler.cs:111-112](smapi-mod/Chat/ChatHandler.cs#L111-L112)) |
+| `chat_received` event renderer | ❌ [smartnpc-mcp/internal/events/format.go:53-57](smartnpc-mcp/internal/events/format.go#L53-L57) renders only `"Someone in the chat says: %s"` — drops `source=player_group` and `group_id` so the profile cannot tell it's in group context |
+| Profile guidance (6× SOUL.md / SKILL.md) | ❌ no skill teaches "if you received a group_id, reply with `channel=\"group\"` + that `group_id`" — so NPC always omits the channel arg and the mod treats the reply as private |
+| `docs/protocol.md` `chat_say` params table (L76-99) | ❌ `channel` / `group_id` not listed; profile authors have no spec to reference |
+| `docs/protocol.md` `chat_received` event (L564-595) | ❌ `source` field not enumerated for `player_group`; `group_id` not listed |
+
+### Impact
+
+Group-chat user flow: player opens group chat panel → types a line addressed to multiple NPCs.
+
+- mod emits `chat_received` with `source=player_group` + `group_id`
+- `events.FormatForHermes` strips both, hands the NPC a generic "Someone says…"
+- NPC's profile decides it's a private one-on-one and calls `chat_say` without `channel`
+- mod's `OnIncomingChatMessage` sees `channel == ""`, dispatches to private toast/panel
+- Player's group chat panel stays empty; the reply leaks into the private NPC panel
+
+The protocol layer + mod layer are correct; the regression is **purely in
+the mcp ↔ profile boundary's loss of group metadata**.
+
+### Proposed fix
+
+1. **`smartnpc-mcp/internal/events/format.go::formatChatReceived`** — when
+   `source == "player_group"`, render as `"You are in group chat <group_id>. Player says: <text>"`
+   so the LLM sees the group context inline. Keep the private `"Someone in the chat says..."`
+   branch for the no-source / `source=player` case.
+2. **`smartnpc-mcp/internal/events/events.go::ChatReceived`** struct — confirm
+   `Source` and `GroupID` fields exist; add if missing. Bump `docs/events.md`
+   `chat_received` schema to enumerate `source` values
+   (`player` / `player_group`) and the `group_id` field.
+3. **6× Hermes profile** — add a small `group-chat-policy/SKILL.md` (or fold
+   into an existing skill) teaching:
+   *"If the inbound event mentions `group chat <group_id>`, your `chat_say`
+   call MUST include `channel=\"group\"` and `group_id=<that id>`. Otherwise,
+   omit `channel` (defaults to private)."* This SKILL is generic across all
+   6 profiles and benefits from the F1 placeholder-ization mechanism — no
+   NPC-specific literals required.
+4. **`docs/protocol.md`** — extend `chat_say` params table (after `color`
+   row at L88) with `channel` and `group_id` rows; extend `chat_received`
+   data table (around L585) with the `source=player_group` value and the
+   `group_id` field.
+5. **Verification**: load a save, open group chat panel, type a line
+   addressed to 2 audible NPCs; confirm both NPCs' replies land in the
+   group panel (not the private toast). Depends on the mcp-engineer's
+   Bug 1 + Bug 2-b fixes from the delegate-fix PR being on `rebuild`.
+
+### Why not in the delegate-fix PR
+
+- Different fault domain (group routing vs synthetic event wiring).
+- Cross-cuts events/format.go + 6 profiles + protocol.md — broader review
+  surface than the wiring fix.
+- Delegate-fix happy path is private 1:1 chat; group chat is a separate
+  scenario with its own E2E checklist.
+- Decision (team-lead, 2026-05-12): track as F3 follow-up; do not bundle
+  into the wiring fix PR.
+
+### Owner
+
+`hermes-profile` (group-chat-policy SKILL across 6 profiles) +
+`mcp-engineer` (events/format.go + events.go) + `docs-coord` (protocol.md
++ events.md + roadmap entry).
```

(F2-roadmap also gets a corresponding F-3 row alongside F-1 / F-2 — see the F2 patch above.)


## Resolved with hermes-profile (2026-05-12)

Both open questions answered in her audit reply:

1. **Other affected SKILL files** — full inventory now in F1 "Current state"
   table: `memory-policy` (9 hits), `inter-npc-message` (9→17 hits after
   delegate-fix), `game-tool-policy` (1 hit), `config-overlay.yaml` (1 hit
   comment). `proactive-greeting` is clean. `cron-recipes.md` is xiami-only,
   separate question.
2. **Substitution mechanism** — extend `hermes/install.sh`'s existing
   `__HOST_IP__` `sed` pattern (option a). Five placeholders:
   `{{NPC_NAME}}` / `{{NPC_DISPLAY}}` / `{{NPC_DIR}}` / `{{PEER_NAME}}` /
   `{{PEER_DISPLAY}}`. Rejected `_templates/` separate tree because xiami
   needs to remain a runnable profile, and the indirection has no upside
   here. F1 "Proposed fix" now reflects this — locked.
