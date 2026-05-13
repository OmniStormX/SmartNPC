# Doc patches — follow-up PR (F1/F2 placeholder + F3 group chat)

> **Author**: docs-coord
> **Reviewers**: team-lead (please approve before I apply)
> **Status**: Drafts — staged, NOT applied. Awaiting design lock from
> hermes-profile (F1/F2 sed install.sh) + mcp-engineer
> (F3 events/format.go group-context injection).
> **Companions**:
> - [ADR-0002](../0002-group-chat-channel-end-to-end.md) — F3 design
> - [ADR-0003](../0003-npc-name-placeholder-cloning.md) — F1/F2 design
> - [synthetic-events-doc-patches.md](./synthetic-events-doc-patches.md)
>   — origin trail (F1/F2/F3 first surfaced as F-section in the
>   delegate-fix follow-up)

These edits keep the doc surface consistent with the F1/F2/F3 fixes
once they land. Each patch has an explicit blocker on the upstream
agent's design before its `<!-- TODO -->` slots can be filled. **Do not
apply** any patch until ADR-0002 / ADR-0003 are flipped from Proposed
to design-locked.

**Apply order (provisional, settled with team-lead pending):**

1. Wait for ADR-0002 design-lock from mcp-engineer (F3 section).
2. Wait for ADR-0003 design-lock from hermes-profile (F1/F2 section).
3. Fill `<!-- TODO -->` slots in patches below from final designs.
4. Apply F3 patches (`P-F3-1` … `P-F3-6`) once F3 implementation lands
   on `rebuild`.
5. Apply F1/F2 patches (`P-F1-1` … `P-F1-4`) once `install.sh`
   substitution mechanism lands.
6. Run `task ci-fast` after each batch.
7. SendMessage diff summary to team-lead before commit.

**Internal link conventions** — same as the delegate-fix drafts file:
relative links (`./0002-...md`, `../events.md`); never leading-slash
absolute paths.

---

# Part A — F3 patches (group chat channel end-to-end)

> Blocker: ADR-0002 design-lock + mcp-engineer's `events/format.go` +
> `events.go` diff merged on `rebuild`. All `<!-- TODO -->` slots below
> reference final wording from that diff.

## P-F3-1 — `docs/protocol.md` `chat_say` params table

**Why**: the `chat_say` Input struct already exposes `Channel` and
`GroupID` ([`smartnpc-mcp/internal/tools/chat.go`](../../smartnpc-mcp/internal/tools/chat.go)),
but the protocol params table doesn't list them — profile authors and
external integrators have no spec to reference, so they omit the
fields and replies leak to the private channel.

```diff
@@ d:\SmartNPC\docs\protocol.md ## chat_say params @@
 | `text`    | string | Yes      | Dialog text                                    |
 | `color`   | string | No       | Hex color override                             |
+| `channel` | string | No       | `"group"` to route into the group panel; omit / `""` for private (default). When set to `"group"`, `group_id` is required. |
+| `group_id`| string | Conditional | Group chat session id; required when `channel="group"`, ignored otherwise. Must match an active group_id from a `group_create` event or an inbound `chat_received` event with `source=player_group`. |
```

<!-- TODO: confirm exact column names ("Required" vs "Notes") and the
existing row immediately preceding `color` once mcp-engineer's diff
lands; the snippet above assumes the current 4-column layout
(Field / Type / Required / Description). Pin column header on apply. -->

## P-F3-2 — `docs/protocol.md` `chat_received` events table

**Why**: `chat_received` already carries `source` and `group_id` from
the mod, but `docs/protocol.md`'s event spec doesn't enumerate the
`source` values or document `group_id`, so profile authors can't
discover what to switch on.

```diff
@@ d:\SmartNPC\docs\protocol.md ## chat_received event @@
 | `text`         | string | Inbound chat text                                  |
 | `audible_npcs` | array  | Agent-managed NPC names within audible range       |
+| `source`       | string | One of `player` (or empty), `player_group`. Determines whether the event came from the legacy private channel or the group chat panel. |
+| `group_id`     | string | Group chat session id when `source=player_group`; empty otherwise. |
```

<!-- TODO: pin exact existing rows preceding the insertion; pull from
mcp-engineer's events.go ChatReceived struct json tags after their
diff lands. -->

## P-F3-3 — `docs/events.md` `chat_received` synthetic-events note

**Why**: the synthetic-events section currently doesn't mention that
group-source `chat_received` events carry an inline group-context line
in the rendered `instructions`. Profile authors writing
`group-chat-policy/SKILL.md` need to know what string they're matching
on.

```diff
@@ d:\SmartNPC\docs\events.md ### `chat_received` @@
 ... <existing description> ...
+
+**Group source rendering**: when `source=player_group` and `group_id`
+is set, [`events.FormatForHermes`](../smartnpc-mcp/internal/events/format.go)
+prefixes the inbound `instructions` with a group-context line so the
+receiving NPC's profile can decide to mirror `channel="group"` +
+`group_id=<id>` back through `chat_say`. The exact prefix is
+`<!-- TODO: paste mcp-engineer's final wording from formatChatReceived -->`.
+See [ADR-0002](./adr/0002-group-chat-channel-end-to-end.md).
```

<!-- TODO: replace the exact-prefix placeholder once mcp-engineer
locks the wording in formatChatReceived. -->

## P-F3-4 — `docs/architecture.md` filtering & routing table + outbound caption

**Why audit**: the L138 filter row (already updated by the delegate-fix
PR to mention "synthetic + mod") is **agnostic to channel**. The L165
outbound caption says "routed by event.npc/to/target → matching
gateway" — group_id is **not** a routing key (NPC name still is); the
gateway selection logic is unchanged. So the filter table needs **no**
edit for F3. The outbound caption needs **no** edit for F3.

What does need a small note: the diagram comment block (L96-L131) ends
with `chat_say(speaker, text)`. Group context flows through the same
arrow but with two extra fields (`channel`, `group_id`); a one-line
parenthetical keeps the diagram honest without redrawing it.

```diff
@@ d:\SmartNPC\docs\architecture.md L127-L131 @@
                                       │ chat_say(speaker, text)
+                                      │ (group chat: also channel="group" + group_id;
+                                      │  see ADR-0002)
                                       ▼
                               smartnpc-mcp ws action → smapi-mod →
                                       chat bubble in-game
```

<!-- Audit verdict: filter table (L138) + outbound caption (L165) are
already correct for F3 — no group-specific edit needed. Only the
diagram chat_say arrow gets the parenthetical above. -->

## P-F3-5 — `docs/mcp-tools.md` `chat_say` description

**Why**: the tool documentation should call out that group-chat
scenarios MUST set `channel="group"` and `group_id`, mirroring the
SKILL guidance ADR-0002 introduces.

```diff
@@ d:\SmartNPC\docs\mcp-tools.md ### `chat_say` @@
 ... <existing description> ...
+
+**Group chat**: when replying in response to a `chat_received` event
+with `source=player_group`, you MUST set `channel="group"` and
+`group_id=<the inbound group_id>`. Omitting either field causes the
+mod to dispatch the reply to the private toast/panel instead of the
+group panel — replies leak across channels. For private 1:1 chat,
+omit `channel` (defaults to private). See
+[ADR-0002](./adr/0002-group-chat-channel-end-to-end.md).
```

<!-- TODO: confirm the existing "When to call" / "Side-effect" block
shape so the insertion point is consistent with sibling tool docs. -->

## P-F3-6 — `smartnpc-mcp/internal/tools/chat.go` description / header

**Why**: the tool **Description** string served via MCP `tools/list` is
what the LLM reads when deciding how to call `chat_say`. It must
mention the group-channel contract or profiles will keep omitting
`channel` even after F3 lands.

```diff
@@ smartnpc-mcp/internal/tools/chat.go registerChatSay Description @@
-		Description: "Speak as the given NPC..." +
+		Description: "Speak as the given NPC. For private 1:1 chat, omit " +
+			"`channel` (defaults to private). For group chat replies " +
+			"(when the inbound event had `source=player_group`), you " +
+			"MUST set `channel=\"group\"` and `group_id=<inbound group_id>` " +
+			"or the reply leaks into the private toast/panel.\n\n" +
+			...rest of existing description...
```

<!-- TODO: pull current Description string verbatim and merge in
the group-channel paragraph as the second paragraph (after the
existing first sentence). Apply only after ADR-0002 design-lock and
the SKILL guidance is rolled out to all 6 profiles in lockstep —
otherwise profiles see the new description but lack the SKILL to
follow it. -->

---

# Part B — F1/F2 patches (NPC name placeholder substitution)

> Blocker: ADR-0003 design-lock + hermes-profile's final `install.sh`
> sed substitution mechanism merged on `rebuild`. All
> `<!-- TODO -->` slots below reference the final placeholder syntax
> and per-profile data source.

## P-F1-1 — `docs/architecture.md` "profile cloning mechanism" section

**Why**: today `docs/architecture.md` has no description of how the 6
profiles relate to each other or how shared SKILL content propagates.
Once F1/F2 land, the install-time render step is a load-bearing piece
of the architecture — readers must be able to find it.

```diff
@@ d:\SmartNPC\docs\architecture.md (insert as a new section, after
   "Filtering & routing", before "Process layout (production)") @@
+
+## Profile cloning mechanism
+
+The 6 NPC profiles under `hermes/profiles/` share most SKILL content
+but each must self-narrate in its own NPC's voice and write to its
+own memory namespace. xiami serves as the **source-of-truth template**
+AND a **runnable profile**; the other 5 profiles are derived from
+xiami at install time via placeholder substitution in
+[`hermes/install.sh`](../hermes/install.sh).
+
+Five placeholders (per-profile substituted at install time):
+
+| Placeholder | Example value (abigail) | Use site |
+|---|---|---|
+| `<!-- TODO: NPC_NAME placeholder syntax -->` | `Abigail` | PascalCase internal name; valid `speaker:` arg |
+| `<!-- TODO: NPC_DISPLAY placeholder syntax -->` | `阿比盖尔` | CN display name for prose |
+| `<!-- TODO: NPC_DIR placeholder syntax -->` | `abigail` | profile dir; `memories/<dir>/`, `conversation:` |
+| `<!-- TODO: PEER_NAME placeholder syntax -->` | `Penny` | example "other NPC" in delegate flows |
+| `<!-- TODO: PEER_DISPLAY placeholder syntax -->` | `潘妮` | CN display for the peer |
+
+`<!-- TODO: NPC_DISPLAY -->` source-of-truth: <!-- TODO: per-profile
+`profile.yaml` vs. `runtime-config.yaml` `display:` field — pick from
+hermes-profile's final design. -->
+
+See [ADR-0003](./adr/0003-npc-name-placeholder-cloning.md) for the
+rejected alternatives (runtime resolution, manual edit, separate
+`_templates/` tree).
```

<!-- TODO: fill the placeholder-syntax cells once hermes-profile
locks the spec. Insertion target is after the "Filtering & routing"
table and before "Process layout (production)" — confirm anchor on
apply. -->

## P-F1-2 — `docs/hermes-profiles.md` rendering flow

**Why**: `docs/hermes-profiles.md` exists (verified 2026-05-12) and
documents profile structure. The render flow is missing — once F1/F2
lands, profile authors need to know they're editing the xiami master
and the other 5 are generated.

```diff
@@ d:\SmartNPC\docs\hermes-profiles.md (insert as a new section, near
   the top after the existing introduction) @@
+
+## Profile rendering (install-time substitution)
+
+xiami's profile is the source of truth for shared SKILL content. The
+other 5 profiles (`abigail`, `haley`, `harvey`, `penny`, `sebastian`)
+are generated at install time by [`hermes/install.sh`](../hermes/install.sh)
+via placeholder substitution.
+
+**To edit shared SKILL content**: edit the xiami master only
+(`hermes/profiles/xiami/skills/...`). Run `hermes/install.sh` to
+propagate to the other 5. **Do not edit non-xiami profile SKILL files
+directly** — they are generated artifacts and your changes will be
+overwritten on next install.
+
+**To edit profile-specific content** (SOUL.md, profile.yaml,
+config-overlay.yaml fields specific to one NPC): edit the per-profile
+file. These are not templated.
+
+The five install-time placeholders and their substitution mechanism
+are documented in
+[`docs/architecture.md` § Profile cloning mechanism](./architecture.md#profile-cloning-mechanism)
+and [ADR-0003](./adr/0003-npc-name-placeholder-cloning.md).
```

<!-- TODO: confirm hermes-profiles.md current section structure on
apply; the insertion target ("after introduction") is the working
assumption. Audit this file once hermes-profile finalizes the
substitution mechanism — the doc may need additional updates beyond
this insertion. -->

## P-F1-3 — `hermes/install.sh` README / header comment block

**Why**: anyone running `install.sh` (or auditing it) should see the
substitution contract documented in the script itself, with pointers
to the source-of-truth ADR for the rationale.

```diff
@@ d:\SmartNPC\hermes\install.sh (top-of-file comment block) @@
+# Profile installer with NPC-name placeholder substitution.
+#
+# xiami is the source-of-truth template AND a runnable profile. The
+# other 5 profiles (abigail, haley, harvey, penny, sebastian) are
+# rendered from the xiami master via sed substitution of five
+# placeholders:
+#
+#   <!-- TODO: NPC_NAME -->     PascalCase internal name (e.g. Abigail)
+#   <!-- TODO: NPC_DISPLAY -->  CN display name (e.g. 阿比盖尔)
+#   <!-- TODO: NPC_DIR -->      lowercase dir name (e.g. abigail)
+#   <!-- TODO: PEER_NAME -->    example "other NPC" in delegate flows
+#   <!-- TODO: PEER_DISPLAY --> CN display for the peer
+#
+# Per-profile values come from <!-- TODO: source path -->.
+#
+# Do NOT edit non-xiami profile SKILL files directly — they are
+# regenerated on every install. Edit xiami's master, then re-run
+# this script.
+#
+# See ADR-0003 (docs/adr/0003-npc-name-placeholder-cloning.md) and
+# docs/architecture.md § Profile cloning mechanism for design.
```

<!-- TODO: replace the placeholder syntax + per-profile source path
with hermes-profile's final spec. Apply alongside the install.sh
implementation diff so the comment matches the code. -->

## P-F1-4 — `CLAUDE.md` "hermes profile 目录" description

**Why**: CLAUDE.md L113 currently lists
`hermes/profiles/xiami/` — `Hermes NPC profile（SOUL.md +
config-overlay.yaml + skills/`). Once F1/F2 lands, the description
should indicate xiami is the master and the other 5 are rendered.

```diff
@@ d:\SmartNPC\CLAUDE.md (关键目录 section, hermes/profiles row) @@
-- `hermes/profiles/xiami/` — Hermes NPC profile（`SOUL.md` + `config-overlay.yaml` + `skills/`）
+- `hermes/profiles/xiami/` — Hermes NPC profile **master template** (`SOUL.md` + `config-overlay.yaml` + `skills/`)；其他 5 profile (`abigail` / `haley` / `harvey` / `penny` / `sebastian`) 通过 `hermes/install.sh` 占位符替换从 xiami 渲染生成。详见 [ADR-0003](docs/adr/0003-npc-name-placeholder-cloning.md)。**不要直接编辑非 xiami profile 的 SKILL 文件——会被 install 覆盖。**
```

<!-- Note: this is a content-bearing edit to CLAUDE.md, not a stylistic
one. Confirm with team-lead that the 中文 wording matches the existing
CLAUDE.md tone before applying. Apply only after install.sh diff
lands so the mechanism actually exists when readers follow the
pointer. -->

---

## Open questions for team-lead

- **Q1**: Should the F3 patches and F1/F2 patches go in the **same**
  follow-up PR (single review, two fault domains) or **two** sequential
  PRs (cleaner per-PR scope)? My read: two PRs — F1/F2 cross-cuts 5
  profiles + an install script and is reviewable on its own; F3
  cross-cuts events/format.go + 6 profile SKILLs + protocol docs. But
  if team-lead prefers one-PR for velocity, I'll merge the patch sets.
- **Q2**: P-F3-6 (chat.go Description) and the 6× SKILL rollout for
  group-chat-policy must land **together** to avoid a "profiles see
  new description but lack SKILL guidance" window. Confirm: hermes-
  profile owns the SKILL rollout, mcp-engineer owns the chat.go diff,
  and we coordinate via a single F3 commit?
- **Q3**: P-F1-4 (CLAUDE.md edit) is the only patch with a tone
  question (existing CLAUDE.md is terse 中文). The proposed wording
  is longer than the current row — happy to tighten if preferred.
