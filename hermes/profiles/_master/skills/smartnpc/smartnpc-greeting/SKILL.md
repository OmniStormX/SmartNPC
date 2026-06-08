---
name: smartnpc-greeting
description: Handle npc_interact events — the player clicked this NPC to start a conversation. Produce a short visible opener with context-appropriate tone.
version: 0.3.0
author: SmartNPC Project
license: MIT
metadata:
  hermes:
    tags: [SmartNPC, npc-interact, greeting]
---

# NPC interact greeting

Use when the event says the player walked up and opened a conversation.

## Flow

1. Call `friendship_get` for yourself to pick the right tone — warm for high
   hearts, neutral for mid, cool for low. This is the one case where reading a
   stat without the player asking is correct: the greeting's emotional color
   depends on the relationship level.
2. Optionally call `game_get_time` if time-of-day matters for the greeting.
3. Call exactly one `chat_say` as the turn terminator.

Skip weather or player status unless the event or context makes it relevant.

## Fallback

If tools fail, still give one generic in-character opener. Stay silent if the
player is busy.
