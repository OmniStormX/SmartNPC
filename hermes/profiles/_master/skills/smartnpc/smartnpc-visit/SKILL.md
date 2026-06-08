---
name: smartnpc-visit
description: Proactive visit (cron). Decide whether {{NPC_NAME}} should visit the player without being asked. Uses cooldown, dice roll, availability check, time window, then summon + emote + chat_say.
version: 0.3.0
author: SmartNPC Project
license: MIT
metadata:
  npc: {{NPC_NAME}}
  hermes:
    tags: [SmartNPC, proactive, cron]
---

# Proactive visit — {{NPC_NAME}}

Use only in the proactive-visit cron/session.

This skill overrides the core policy's "only act when player asks" rule:
`npc_summon` is explicitly authorized here without a player request.

## Decision flow

1. **Cooldown**: check memory for `proactive-visit: last=<ISO>`. If newer than
   60 real minutes, stop silently.
2. **Dice**: roll 1..6. Continue only on 1; otherwise write
   `proactive-visit: skipped dice=<N> at=<ISO>` and stop.
3. **Availability**: call `player_get_status`. Stop silently if busy, in menu,
   in event, or no save.
4. **Time**: call `game_get_time`. Normal window 0800-2200. Night-owl personas
   may extend to 2400.
5. **Visit**:
   - `npc_summon` — warp to player
   - `npc_emote` with kind="sparkle"
   - one private `chat_say`
   - write `proactive-visit: last=<ISO> topic="..."`

## Guardrails

- Summon + emote + one chat_say only — no follow, lead, or move chains.
- Do not write `last=` before `chat_say` succeeds.
- Group chat and peer messages are never used during visits.
