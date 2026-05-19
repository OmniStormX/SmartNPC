# Hermes-first Migration — Design Spec

**Date:** 2026-05-12
**Status:** Design approved, pending implementation plan
**Scope:** Cut over the 6 SmartNPC characters from the frozen `smartnpc-agent` runtime to per-NPC Hermes Agent profiles, with `smartnpc-mcp` as the single ws-side bridge and multi-profile fan-out.

---

## 1. Background & motivation

The repo already declared `smartnpc-agent` frozen on 2026-05-11 ([REFACTOR.md](../../../REFACTOR.md), [docs/migration-smartnpc-agent.md](../../migration-smartnpc-agent.md)). Migration artifacts for **only XiaMi** ship today (`hermes/profiles/xiami/`); the other 5 NPCs (Abigail, Haley, Harvey, Penny, Sebastian) still run through `smartnpc-agent` in `run.bat`. This design covers the remaining work to make the Hermes-first path actually the runtime path.

**Non-goals (deliberately deferred):**

- Group chat orchestration on Hermes side — left at M6 TBD; the QQ-style UI on the mod side keeps rendering but no NPC will reply in group mode after this migration.
- Archiving `smartnpc-agent/` — stays on disk for regression compare, just stops being launched. M5.G handles archival.
- Hermes side cron / proactive recipes for the 5 new NPCs — they get the same baked-in skills as XiaMi but no per-NPC cron recipes yet.

---

## 2. Target architecture

```
SMAPI Mod (C#)
   │
   │ ws :18745 (single client)
   ▼
smartnpc-mcp (Go, single process)
   ├── :3000/mcp  (Streamable HTTP) ◀── Hermes profile (MCP client)
   │                                      ├── xiami      (gateway :8642)
   │                                      └── abigail    (gateway :8643)
   │                                  (haley:8644, harvey:8645, penny:8646,
   │                                   sebastian:8647 — files ready, not launched)
   │
   └── hermesrelay outbound
         POST {gateway}/v1/responses
         ▲
         │ routed by event.npc field via --hermes-config YAML
```

**Roles** (unchanged from migration doc, restated for clarity):

| Layer | Owns |
|---|---|
| SMAPI mod | Game-thread APIs, spatial NPC routing (`AudibleNPCResolver`), turn queue, UI |
| smartnpc-mcp | MCP tool schema, ws bridge, event protocol, **multi-profile fan-out** |
| Hermes profile | Decision-making, persona (SOUL.md), skills, memory (state.db), reflection |

Key invariants:

- **Exactly one `smartnpc-mcp` process.** The mod's ws server only accepts one client.
- **Per-NPC Hermes Gateway processes.** Each has its own `state.db`, its own port, its own conversation id.
- **Conversation id = profile name = `API_SERVER_MODEL_NAME`.** Lowercase (`xiami`). The internal NPC name in game/event payloads is **PascalCase** (`XiaMi`). This split is preserved.
- **`smartnpc-agent` is not launched.** Stays on disk (frozen) until M6 archive.

---

## 3. Components

### 3.1 New: `hermes/runtime-config.yaml`

Single source of truth for fan-out routing. Lives in repo, mounted into WSL via `/mnt/d/SmartNPC/`.

```yaml
# hermes/runtime-config.yaml — consumed by smartnpc-mcp --hermes-config
profiles:
  - name: xiami
    npc_filter: XiaMi          # event.npc must match (PascalCase)
    gateway_url: http://127.0.0.1:8642
    conversation: xiami
    model: hermes-agent
    api_key_env: SMARTNPC_HERMES_KEY   # mcp reads from this env var
  - name: abigail
    npc_filter: Abigail
    gateway_url: http://127.0.0.1:8643
    conversation: abigail
    model: hermes-agent
    api_key_env: SMARTNPC_HERMES_KEY
  - name: haley
    npc_filter: Haley
    gateway_url: http://127.0.0.1:8644
    conversation: haley
    model: hermes-agent
    api_key_env: SMARTNPC_HERMES_KEY
  - name: harvey
    npc_filter: Harvey
    gateway_url: http://127.0.0.1:8645
    conversation: harvey
    model: hermes-agent
    api_key_env: SMARTNPC_HERMES_KEY
  - name: penny
    npc_filter: Penny
    gateway_url: http://127.0.0.1:8646
    conversation: penny
    model: hermes-agent
    api_key_env: SMARTNPC_HERMES_KEY
  - name: sebastian
    npc_filter: Sebastian
    gateway_url: http://127.0.0.1:8647
    conversation: sebastian
    model: hermes-agent
    api_key_env: SMARTNPC_HERMES_KEY
```

Notes:

- `api_key_env` keeps the secret out of the file. mcp reads `os.Getenv($value)` at startup.
- Loopback `127.0.0.1` works because mcp runs on Windows host and Gateway binds `0.0.0.0` in WSL — but `mcp` runs on Windows, so it needs the WSL gateway IP (typically `192.168.59.118`). **Design picks: explicit IP in yaml**, manually set per machine. `hermes/install.sh` already detects `HOST_IP` via `ip route` for the **inbound** direction; we document the same command for the **outbound** direction so the user fills `runtime-config.yaml` once. Mirror-mode WSL networking (which would let `127.0.0.1` work transparently) was considered and rejected: introduces an environmental dependency on a Windows feature toggle, hard to debug when it silently fails.

### 3.2 Extended: `smartnpc-mcp/internal/hermesrelay/`

Current `Relay` is single-target. Extend to multi-target with a routing function:

```go
type Config struct {
    Profiles []ProfileTarget
    Logger   *slog.Logger
}

type ProfileTarget struct {
    Name         string  // for logs / metrics
    NPCFilter    string  // event.npc must equal this (PascalCase, case-sensitive)
    GatewayURL   string  // base url, /v1/responses appended
    Conversation string  // forwarded as conversation_id
    Model        string  // forwarded as model
    APIKey       string  // bearer token; resolved from APIKeyEnv at config-load time
}

// Route returns the target for an event, or nil when no profile matches.
// When event.npc is empty (e.g. chat_received with no audible NPCs), Route
// returns nil and the caller drops the event.
func (r *Relay) Route(ev *Event) *ProfileTarget { ... }
```

- Routing key is **exclusively `event.npc`**. No fallback to "default profile" — drops are logged but not retried.
- Existing `--hermes-url / --hermes-npc / --hermes-conversation / --hermes-model` flags become **deprecated aliases**: when present they synthesize a single-entry `Profiles` slice at config-load time. Removed in a follow-up PR.

### 3.3 Extended: `smartnpc-mcp/cmd/smartnpc-mcp/main.go`

Add:

```go
hermesConfig = flag.String("hermes-config", "",
    "path to YAML config with multi-profile fan-out routing. "+
    "When set, replaces --hermes-url / --hermes-npc / --hermes-conversation / --hermes-model.")
```

Precedence at startup:

1. `--hermes-config` set → load YAML, validate, build `[]ProfileTarget`.
2. Else if `--hermes-url` set → build single-entry slice (back-compat).
3. Else → no relay (mod-only mode for `--echo-mode` / tests).

### 3.4 New: 5 Hermes profiles

`hermes/profiles/{abigail,haley,harvey,penny,sebastian}/` each ships:

- `SOUL.md` — **handwritten** (per user decision), patterned after `xiami/SOUL.md` (8 sections: 身份 / 性格 / 说话风格 / 灵魂层次 / 好感度对应语气 / 绝对禁止 / 工具使用原则 / 人设背景). Source material: `smartnpc-agent/personas/<name>.json` (`personality`, `speaking_style`, `background`, `soul_notes`, `friendship_behaviors`) gives raw text; rewrite in xiami's voice/style.
- `config-overlay.yaml` — copy of xiami's, with `API_SERVER_PORT` bumped (xiami=8642, abigail=8643, haley=8644, harvey=8645, penny=8646, sebastian=8647) and `API_SERVER_MODEL_NAME` swapped.
- `skills/smartnpc/` — symlinks (or repo copies — see §6.1) to xiami's three skills: `game-tool-policy`, `proactive-greeting`, `memory-policy`. **Plus** the new `inter-npc-message` (§3.5).

### 3.5 New skill: `inter-npc-message/SKILL.md`

Goes into every profile (including a backport to xiami). Replaces the `consult_npc` + `[Delegation rule]` prompt logic from `smartnpc-agent/internal/agent/chat/`.

The skill has two roles in one document:

**Asker role** (when player talks to me about another NPC):

| Pattern | Action |
|---------|--------|
| Player asks about X's thoughts / plans / feelings / schedule | `npc_send_message(to=X, kind="query", text="<玩家的问题>", reply_expected=true)` then wait for X's inbox reply |
| Player asks me to get X to do something (come, go, deliver, perform) | `npc_send_message(to=X, kind="behavioral", text="<玩家想请你的事>", reply_expected=true)` |
| Phrase triggers (Chinese / English) | "帮我问 / 去问问 / ask X", "叫/让/请 X 过来 / 过去 / 做…", "告诉 X …", "把 X 喊来" |
| Forbidden | Fabricating X's voice. Pretending I did the task. |

**Receiver role** (when an `event_npc_message` arrives via MCP notification for me):

| Kind | Behavior |
|------|----------|
| `query` | Read `from` + `text`, treat as a question from another NPC, respond normally via `chat_say`. Also call `npc_send_message(to=from, kind="reply", text=<我的答案>)` to close the loop so the asker sees my response in their inbox. |
| `behavioral` | Run the request through `applyBehaviorIntent`-equivalent reasoning: keyword-match → call the right game tool (`npc_move_to`, `npc_summon`, etc.) → then `chat_say` a confirmation in character. |
| `reply` | A peer answered my earlier `query`. Pull from inbox, fold into the next persona reply. |

This skill replaces the entire Go-side delegation pipeline. No `consult_npc` synthetic tool, no router back-reference, no scratch history swapping.

### 3.6 Extended: `hermes/profiles/xiami/skills/smartnpc/smartnpc-game-tool-policy/SKILL.md`

Add a "Delegation" section that points to `inter-npc-message`, so the existing global skill keeps holistic discoverability. No behavior change beyond a cross-reference.

### 3.7 Rewritten: `run.bat`

Goals:

1. No `smartnpc-agent` invocation.
2. Build mcp + mod (Go agent build still ok — keeps regression artifact).
3. Run `hermes/install.sh` to sync profile files into WSL.
4. Start mcp `--http :3000 --ws-url ws://127.0.0.1:18745/ws --hermes-config /mnt/d/SmartNPC/hermes/runtime-config.yaml`.
5. Start 2 Hermes Gateways: xiami (8642) + abigail (8643). Each in its own WSL window. Health-check each before launching the game.
6. `ensure_hermes_aux.sh` step preserved (session_search → gpt-4o-mini fix).

The 4 unused profiles (haley/harvey/penny/sebastian) stay un-launched; documented in `run.bat` and `hermes/README.md` how to enable them (uncomment 4 lines).

### 3.8 New: `scripts/start_hermes_profiles.sh`

Helper invoked from `run.bat`. Takes a comma-separated profile list and starts each gateway in the background, captures PID, polls health endpoint until alive, logs PID + port mapping.

```bash
# Usage:
#   bash scripts/start_hermes_profiles.sh xiami,abigail
# Effect:
#   - launches `hermes -p $name gateway run --accept-hooks` per profile
#   - writes pids to /tmp/smartnpc-hermes-pids.txt
#   - waits up to 90s per profile for /health to return 200
#   - exits non-zero if any profile fails to become healthy
```

`run.bat` cleans up on next run by killing PIDs listed in the file (best-effort).

---

## 4. Data flow

### 4.1 Player → NPC reply (e.g. `chat_message` to XiaMi)

1. Player types in mod ChatPanel → mod emits `chat_message {npc:"XiaMi", text:"hi", source:"player"}` over ws.
2. mcp's bridge handler decodes; hermesrelay sees `event.npc=XiaMi` → routes to xiami target (`http://127.0.0.1:8642/v1/responses`).
3. POST body = `{model:"hermes-agent", conversation:"xiami", input:<rendered event>, api_key:SMARTNPC_HERMES_KEY}`.
4. Hermes Gateway runs the xiami profile: SOUL.md + skill catalog + state.db conversation history.
5. Hermes decision calls `chat_say(speaker="XiaMi", text="...")` via the MCP HTTP transport at `:3000/mcp`.
6. mcp's `chat_say` handler forwards to mod via ws → mod renders bubble.

### 4.2 NPC → NPC delegation (XiaMi asks Penny to come)

1. Player says to XiaMi "让潘妮过来".
2. XiaMi's Hermes skill `inter-npc-message` (asker role, behavioral pattern) fires → calls `npc_send_message(to="Penny", kind="behavioral", text="玩家想请你过来", reply_expected=true)`.
3. mcp's `npc_message` tool enqueues in Penny's mailbox AND emits MCP notification `event_npc_message`.
4. Penny's profile is subscribed to mcp notifications. Receives the event, triggers receiver-role skill.
5. Penny's `inter-npc-message` (receiver, behavioral) → calls `npc_summon(npc="Penny")` (or `npc_move_to` to player tile) → then `chat_say(speaker="Penny", text="好，我这就过去")`.
6. Penny also calls `npc_send_message(to="XiaMi", kind="reply", text="OK I'm on my way")`.
7. XiaMi's profile pulls from inbox on its **next inbound event** (player message, npc_interact, or the `event_npc_message` reply notification itself if Hermes triggers on it), folds the answer into the persona reply: "潘妮说她马上过来".

**Timing caveat:** Steps 5-7 are asynchronous. By the time Penny's `reply` reaches XiaMi's inbox, XiaMi may have already finished her persona reply for the current player turn. In that case the paraphrase happens on the **next** player turn (XiaMi's `inter-npc-message` receiver-role for `kind=reply` is also a valid trigger). The player therefore sees Penny's `chat_say` immediately (step 5), and XiaMi's paraphrase one turn later. This is acceptable — the inter-NPC channel is store-and-forward by design ([npc_message.go](../../../smartnpc-mcp/internal/tools/npc_message.go) mailbox), not request/response.

This is **end-to-end through Hermes profiles + MCP tools** — no Go-side delegation code path.

### 4.3 Group chat (deferred)

`group_create` / `group_message` events still emit from mod. mcp forwards them as MCP notifications but no profile has a skill that handles them yet. Net effect: player sees the group panel work locally, but no NPCs reply. **Documented in run.bat and hermes/README.md as a known limitation pending M6.**

---

## 5. Files & changes

### 5.1 Added

| Path | Purpose |
|------|---------|
| `hermes/runtime-config.yaml` | mcp fan-out config |
| `hermes/profiles/abigail/SOUL.md` | + `config-overlay.yaml` + skills/ |
| `hermes/profiles/haley/{SOUL.md, config-overlay.yaml, skills/}` | ditto, port 8644 |
| `hermes/profiles/harvey/{SOUL.md, config-overlay.yaml, skills/}` | ditto, port 8645 |
| `hermes/profiles/penny/{SOUL.md, config-overlay.yaml, skills/}` | ditto, port 8646 |
| `hermes/profiles/sebastian/{SOUL.md, config-overlay.yaml, skills/}` | ditto, port 8647 |
| `hermes/profiles/*/skills/smartnpc/smartnpc-inter-npc-message/SKILL.md` | new shared skill (6 copies / symlinks) |
| `scripts/start_hermes_profiles.sh` | WSL gateway launcher with health check |
| `docs/superpowers/specs/2026-05-12-hermes-first-migration-design.md` | this file |

### 5.2 Modified

| Path | Change |
|------|--------|
| `smartnpc-mcp/cmd/smartnpc-mcp/main.go` | `--hermes-config` flag; precedence over legacy single-target flags |
| `smartnpc-mcp/internal/hermesrelay/relay.go` | multi-target routing; YAML config loader; `Route(event) *ProfileTarget` |
| `smartnpc-mcp/internal/hermesrelay/*_test.go` | new tests: yaml load, routing by npc, drop unknown |
| `hermes/profiles/xiami/skills/smartnpc/smartnpc-game-tool-policy/SKILL.md` | cross-reference to inter-npc-message |
| `run.bat` | rewrite (§3.7) |
| `docs/architecture.md` | add multi-profile diagram + fan-out config description |
| `docs/hermes-profiles.md` | document `runtime-config.yaml`, link in "Multi-NPC checklist" |
| `docs/migration-smartnpc-agent.md` | flip "Group chat: TBD" remains; mark behavior parity ✅ for the 5 new NPCs |
| `docs/roadmap.md` | M5 (Hermes-first) → ✅ end-to-end verified after this PR; M6 still has group chat |
| `CLAUDE.md` | reflect Hermes-first as the run.bat default |

### 5.3 Untouched (frozen, but kept for regression)

`smartnpc-agent/` — code, tests, personas stay. `go.work` still includes it. `Taskfile.yml` agent:* targets still functional for ad-hoc dev harness use.

---

## 6. Open design questions resolved during brainstorm

### 6.1 Skill directory copy vs symlink

The shared `game-tool-policy / proactive-greeting / memory-policy / inter-npc-message` skills are identical across 6 profiles. Options:

- **Symlinks** — DRY, edit-once. But Windows + WSL symlinks are fragile; `install.sh` may dereference inconsistently.
- **Direct copies** — duplication, but `install.sh` already does the copy and a profile-specific override is sometimes useful (e.g. per-NPC tool policy tweaks).

**Pick: direct copies.** `install.sh` reads from `hermes/profiles/<name>/skills/` so a copy in each is the path of least resistance. If we need DRY later, refactor `install.sh` to splice a `common/skills/` directory.

### 6.2 npc_send_message inbox polling

`inter-npc-message` receiver role assumes Hermes profile gets MCP notifications. The mcp side already emits `event_npc_message` notifications ([smartnpc-mcp/internal/tools/npc_message.go](../../../smartnpc-mcp/internal/tools/npc_message.go)). What's unverified is whether Hermes Agent triggers a skill turn on receipt of a notification.

**Risk:** If Hermes only triggers on explicit input (via `/v1/responses`), the receiver won't fire. Mitigation:
- Verify with `hermes -p xiami mcp test smartnpc_game` after install.
- Fallback path: have `hermesrelay` also POST a "you have new mail" envelope to the recipient gateway when an inbox enqueue happens, treating it like a normal event. Cost: one extra HTTP per delegation hop.

**Decision:** Build the happy path (MCP notification). Add the fallback only if testing reveals notification-driven triggers don't work.

### 6.3 Hermes Gateway port consistency

If a user manually edits `~/.hermes/profiles/abigail/config.yaml` and changes the port, fan-out yaml is out of sync. Mitigation:
- `install.sh` always writes the canonical port from `config-overlay.yaml`. Manual edits in the live config will get re-merged on next install.
- `start_hermes_profiles.sh` health check uses the port from `runtime-config.yaml`. If mismatch, fail loudly.

---

## 7. Risks & mitigations (recap)

| Risk | Mitigation |
|------|------------|
| Hermes Gateway × 2 cold-start time (target < 90s combined) | Parallel launch; health-check loop; user override via `SMARTNPC_HERMES_BOOT_TIMEOUT` env |
| WSL gateway IP brittle | Document explicit IP in `runtime-config.yaml`; provide `ip route` snippet in `hermes/README.md` |
| MCP-notification-triggered skill doesn't fire (§6.2) | Fallback POST as a normal event; tested in integration phase |
| 5 new SOUL.md authoring quality | Use xiami's structure exactly; lift raw character data from `personas/*.json`; iterate per NPC after first end-to-end run |
| Profile name case mismatch (xiami vs XiaMi) | Existing convention preserved; `runtime-config.yaml` explicit `npc_filter` field with case-sensitive match; documented in `hermes-profiles.md` |
| Group chat regression | Documented as known M6-gated; users get a warning banner in mod when entering group mode |
| Behavior parity gaps after switch | Frozen `smartnpc-agent` stays bootable for A/B compare; `--persona-only` mode of smartnpc-agent kept for skill-bypass testing |

---

## 8. Testing strategy

**Unit:**

- `smartnpc-mcp/internal/hermesrelay/`: yaml load, multi-target routing, unknown-npc drop, env-var resolution.
- mcp `main_test.go`: `--hermes-config` flag precedence vs legacy flags.

**Integration (existing pipeline test extended):**

- `smartnpc-mcp/cmd/smartnpc-mcp/pipeline_test.go`: simulate `chat_message` event with `npc=Abigail`, assert POST goes to port 8643.

**Manual E2E (after merge):**

1. `run.bat` → wait for both gateways healthy.
2. Talk to XiaMi: expect chat_say reply within 10s.
3. Talk to Abigail: expect chat_say reply within 10s, no cross-pollination with XiaMi history.
4. "让阿比盖尔过来" to XiaMi: expect Abigail to `chat_say` + actually move (via inter-npc-message receiver skill).
5. Switch to group chat: expect mod-side UI works, no NPC reply, documented warning banner.

---

## 9. Out of scope (M6 follow-ups)

- Group chat orchestration on Hermes side
- Cron / proactive recipes for 5 new NPCs (only xiami has them today)
- `smartnpc-agent/` archival (move to `archive/`, drop from go.work)
- `--hermes-url / --hermes-npc` legacy flag removal
- Per-NPC LLM model selection in `runtime-config.yaml`

---

## 10. Acceptance criteria

- `task ci` green (lint + tests + build, all 3 modules).
- `run.bat` boots without launching `smartnpc-agent`. Both gateways healthy.
- Player chat to XiaMi and Abigail produces in-character replies via Hermes profile.
- NPC delegation: XiaMi → Penny via `npc_send_message`, Penny replies via chat_say + actually executes a game tool. (Penny profile must be launched manually for this test, or the test uses Abigail.)
- No regression in single-NPC chat path vs frozen Go agent (manual A/B over a 5-turn dialogue).
- All 6 SOUL.md files present, lint-clean (UTF-8 no BOM), peer-reviewed by the user for character voice.
