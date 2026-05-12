# Authoring a Hermes Profile for a SmartNPC

A Hermes profile is one NPC's brain: persona, skills, memory,
gateway. Profiles live at `~/.hermes/profiles/<name>/`. The repo
ships a versioned source at `hermes/profiles/<name>/` plus
[`hermes/install.sh`](../hermes/install.sh) to sync.

This doc walks the **anatomy** of a profile, the **install loop**,
and the **template** for adding a new NPC.

## Anatomy

```
hermes/profiles/<npc>/
├── SOUL.md                                  identity, speaking, tools
├── config-overlay.yaml                      mcp_servers + API_SERVER_*
└── skills/smartnpc/
    ├── game-tool-policy/SKILL.md            when to call which tool
    ├── proactive-greeting/SKILL.md          how to react to npc_interact
    └── memory-policy/SKILL.md               what to commit to memory
```

After `install.sh`, the live profile additionally contains:

```
~/.hermes/profiles/<npc>/
├── SOUL.md                ← copied from repo
├── config.yaml            ← Hermes-generated + our overlay appended
├── skills/smartnpc/...    ← copied from repo
├── state.db               ← conversation history + memory (auto)
├── memories/              ← markdown note files (auto)
├── sessions/              ← per-session logs (auto)
└── logs/                  ← daemon logs (auto)
```

## File-by-file

### `SOUL.md` — identity layer (timeless)

What the NPC **is**: identity, voice, taboos, friendship-tier mood
ladder, the absolute "never do X" list. Owned by you, the author.

| Section | Purpose |
|---|---|
| 身份 / Identity | One-paragraph who-they-are |
| 性格 / Personality | Voice description, not a script |
| 说话风格 / Speaking style | Length, tone, do/don't |
| 灵魂层次 / Soul layers | Surface persona vs hidden depth — drives reaction shape |
| 好感度对应语气 | Per-heart-tier example openers (parsed by `friendship-behavior`) |
| 绝对禁止 / Taboos | Never break character, never reveal LLM-ness, ... |
| 工具使用原则 | Soft index into `skills/smartnpc/game-tool-policy` |
| 人设背景 | Internal lore the NPC holds but doesn't volunteer |

**Do not** put time-varying state in SOUL.md. The day's events,
relationship changes, recent conversation — all of that belongs in
memory or conversation history.

### `config-overlay.yaml` — runtime wiring

Two YAML blocks the `install.sh` injects into the live
`config.yaml`:

```yaml
mcp_servers:                   # how Hermes reaches smartnpc-mcp
  smartnpc_game:
    url: http://__HOST_IP__:3000/mcp   # filled in by install.sh
    tools:
      exclude: []
      resources: false
      prompts: false

API_SERVER_ENABLED: true       # how smartnpc-mcp (relay) reaches Hermes
API_SERVER_KEY: smartnpc-test-key
API_SERVER_HOST: 0.0.0.0
API_SERVER_PORT: 8642          # MUST be unique per concurrent profile
API_SERVER_MODEL_NAME: <npc>   # forwarded as `model` field
```

> **Port collision warning**: every profile that wants its own
> gateway needs its own `API_SERVER_PORT`. Two profiles can't share
> 8642. Bump per NPC.

### `skills/smartnpc/*/SKILL.md`

Each subdirectory is one Hermes skill. The frontmatter must contain:

```yaml
---
name: smartnpc-<slug>
description: One-line trigger description used by Hermes to decide relevance.
version: 0.1.0
author: SmartNPC Project
license: MIT
metadata:
  hermes:
    tags: [SmartNPC, Stardew-Valley, ...]
---
```

The body is markdown — sections, tables, examples. Hermes loads the
description into its skill catalog and the body into context when
the skill is invoked.

The three baked-in skills:

| Skill | Triggers on |
|---|---|
| `game-tool-policy` | Always relevant during any game-NPC turn |
| `proactive-greeting` | `npc_interact` events (player clicked NPC) |
| `memory-policy` | Memory commits / reads |

## Install loop

```bash
# 1. Bootstrap (only first time per profile)
hermes -p xiami help            # any hermes -p <name> command makes the dir

# 2. Sync from repo
wsl -d Ubuntu-22.04 bash /mnt/d/SmartNPC/hermes/install.sh

# 3. Verify Hermes can see smartnpc-mcp
hermes -p xiami mcp test smartnpc_game
# Expect: ✓ Connected + Tools discovered: ≥ 5

# 4. Run the gateway
hermes -p xiami gateway run --accept-hooks
```

After (3), open `~/.hermes/profiles/xiami/config.yaml` and double-check:

- Top-level `mcp_servers:` with `url: http://<wsl-gw-ip>:3000/mcp`
- Top-level `API_SERVER_ENABLED: true` + the rest

If you change `SOUL.md` or a skill, re-run `install.sh` — restart the
gateway to reload skills.

## Multi-NPC checklist

Adding a second NPC (e.g. Abigail):

1. Create `hermes/profiles/abigail/` with `SOUL.md` +
   `config-overlay.yaml` (bump `API_SERVER_PORT` to 8643,
   `API_SERVER_MODEL_NAME: abigail`).
2. Copy or fork `skills/smartnpc/` from xiami.
3. Run `hermes -p abigail help` to bootstrap.
4. Run `install.sh` again — it'll sync both.
5. Add an Agent-managed NPC entry in
   [`smapi-mod/NPC/AgentNpcRegistry`](../smapi-mod/NPC/AgentNpcRegistry.cs)
   if the NPC isn't already registered.
6. Run smartnpc-mcp **with a second `--hermes-*` flag set**, or run
   a second mcp instance pointed at the Abigail gateway (future
   work: a `--hermes-config` file for multi-profile fan-out).

## Conventions

- **Files are UTF-8 no BOM.** Markdown only — no YAML in body text.
- **Conversation id = profile name = `API_SERVER_MODEL_NAME`.** Don't
  rename without updating all three.
- **NPC internal name (in code/events) is case-sensitive.** Stardew
  uses `XiaMi` not `xiami` for the in-game NPC; the Hermes profile
  name is the lowercase `xiami`. Match the event filter accordingly:
  `--hermes-npc XiaMi` while `--hermes-conversation xiami`.
- **Don't check in `state.db` or `memories/`.** They contain
  per-run state and grow without bound.

## See also

- [`hermes/README.md`](../hermes/README.md) — install steps and
  prerequisites (Hermes mcp pkg, WSL networking)
- [`mcp-tools.md`](./mcp-tools.md) — what's in the MCP catalog
- [`events.md`](./events.md) — what event payloads look like
- [`hermes/profiles/xiami/cron-recipes.md`](../hermes/profiles/xiami/cron-recipes.md)
  — proactive behavior templates
