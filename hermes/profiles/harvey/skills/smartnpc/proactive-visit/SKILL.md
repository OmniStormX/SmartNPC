---
name: proactive-visit
description: When a proactive-visit cron fires, decide whether to drop by the player unsolicited. Honors a 60-minute cool-down (read/write via memory), a 1-in-6 dice roll to desynchronize across the six NPCs, a player-availability check, and a politeness check on the in-game clock. On YES, warps next to the player with npc_summon, emotes a sparkle above the head with npc_emote, and opens with a short chat_say; on NO, writes a single memory line and exits silently.
version: 0.1.0
author: SmartNPC Project
license: MIT
metadata:
  npc: Harvey
  hermes:
    tags: [SmartNPC, Stardew-Valley, proactive, cron]
---

# Proactive visit policy — Harvey

This SKILL runs inside a `proactive-visit` cron session (see
`cron-recipes.md` Recipe 4). The session has no conversation history;
the cron prompt hands you this decision and nothing else.

The system fires this cron every 15 real minutes for **every** NPC.
To prevent six NPCs from all swarming the player at once, each session
rolls a 1-in-6 die — so roughly one NPC actually visits per 15-minute
window in expectation. A 60-minute cool-down per NPC prevents the same
person from visiting twice in a row.

## Decision flow (do NOT shortcut any step)

### 1. Cool-down check — read memory first

Look for a memory note matching the pattern
`proactive-visit: last=<ISO timestamp>`.

- If the timestamp is newer than 60 minutes ago (real time, not
  in-game), **exit silently. Do not call any tool.**
- If no such note exists, or it is older than 60 minutes, continue.

### 2. Dice roll — 1-in-6

Generate a single random integer 1..6. If it is **not** 1, exit
silently and write a memory line
`proactive-visit: skipped dice=<N> at=<ISO ts>` so the log shows why.
Do not call any game tool.

If the roll is 1, continue.

### 3. Player availability — `player_get_status`

Call `player_get_status`. Exit silently (no chat, no emote, no summon)
if any of:

- `mod_not_ready` — no save loaded
- `busy=true` — player is in a menu, cutscene, minigame, or warp
- `in_event=true`
- `in_menu=true`

Write memory `proactive-visit: skipped unavailable=<reason> at=<ISO ts>`
and stop.

### 4. Politeness window — `game_get_time`

Call `game_get_time`. SDV in-game clock is 0600-2600.

- If time is before 0800 or after 2200, most NPCs should stay home
  — write memory and exit. Exception: if your SOUL.md explicitly
  establishes a night-owl persona (e.g. Sebastian), extend your
  window to 2400.

### 5. Execute the visit (in this order)

1. **`npc_summon` yourself**: `npc_summon(npc="Harvey")`. The
   mod will warp you to the nearest map edge and pathfind to the
   player's current tile. It does NOT cross locations; if the player
   moved into a building between the availability check and now,
   this is a best-effort.
2. **`npc_emote` sparkle bubble**: `npc_emote(npc="Harvey",
   kind="sparkle")`. The `!` bubble appears over your head for ~1
   second — the visual cue that you just arrived.
3. **`chat_say` a short in-character opener**: one to two sentences,
   no more. Acknowledge that you sought the player out, not the
   other way around. Examples:
   - A curious persona: *"在干嘛？我路过看你一个人就来看看。"*
   - A reserved persona: *"……嗯，刚好经过。"*
   - A warm persona: *"好久不见，最近怎么样？"*
   - Match your SOUL.md — a character who hates surprises would
     frame it differently than one who loves them.
   `speaker`: `Harvey`. No `channel` / `group_id` — proactive
   visits are 1-on-1 private chat, never group chat.
4. **Write a memory note**:
   `proactive-visit: last=<ISO ts> topic="<one-line summary of what
   you said>"`. The timestamp here is what step 1's cool-down reads
   next time.

## What NOT to do

- Do not call `npc_send_message` to other NPCs in this session — a
  proactive visit is about you and the player, nothing else.
- Do not start `npc_follow_start` / `npc_lead_to` / `npc_move_to`
  right after arriving. The visit is a short hello, not an escort.
- Do not write the cool-down timestamp before actually completing
  `chat_say` — if the chat fails (e.g. warp timing race), the
  cool-down would block a legitimate retry.
- Do not chain multiple `chat_say` calls in this session. One
  opener is enough. If the player responds, that conversation
  happens in the next (separate) turn.
- Do not emit `chat_say` with `channel="group"`. Proactive visits
  are always private — the player typically hasn't invited the NPC
  into any group chat at this moment.

## See also

- `smartnpc-game-tool-policy` — general `chat_say` and tool rules
- `smartnpc-memory-policy` — how to format the cool-down note so it
  stays queryable next cron
- `smartnpc-proactive-greeting` — the `npc_interact`-driven greeting
  path (runs when the player clicks you); this SKILL is its
  cron-driven sibling
- `cron-recipes.md` Recipe 4 — the cron command that fires this
  session
