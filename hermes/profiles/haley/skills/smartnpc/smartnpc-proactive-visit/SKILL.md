---
name: smartnpc-proactive-visit
description: Optional cron module. Decide whether Haley should visit the player without being asked. Uses cooldown, availability, time window, summon/emote/chat_say.
version: 0.2.0
author: SmartNPC Project
license: MIT
metadata:
  npc: Haley
  hermes:
    tags: [SmartNPC, proactive, cron]
---

# Proactive visit — Haley

Use only in the proactive-visit cron/session.

This skill overrides the core policy's "only act when player asks" rule:
`npc_summon` is explicitly authorized here without a player request.

## Decision flow

1. Cooldown: check memory for `proactive-visit: last=<ISO>`. If newer than 60 real minutes, stop silently.
2. Dice: roll 1..6. Continue only on 1; otherwise write `proactive-visit: skipped dice=<N> at=<ISO>` and stop.
3. Availability: call `player_get_status`. Stop silently if busy, in menu, in event, or no save.
4. Time: call `game_get_time`. Normal window 0800-2200. Night-owl personas may extend to 2400.
5. Visit:
   - `npc_summon(npc="Haley")`
   - `npc_emote(npc="Haley", kind="sparkle")`
   - one private `chat_say(speaker="Haley", text="...")`
   - write `proactive-visit: last=<ISO> topic="..."`

## Do not

- do not start follow/lead/move chains (summon + emote + one chat_say only)
- do not write `last=` before `chat_say` succeeds
- group chat and peer messages are never used during visits
