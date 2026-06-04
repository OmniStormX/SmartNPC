---
name: smartnpc-proactive-greeting
description: Optional npc_interact module. Use when the player clicks this NPC and no text message is attached. Produce a short visible opener.
version: 0.2.0
author: SmartNPC Project
license: MIT
metadata:
  hermes:
    tags: [SmartNPC, npc-interact, greeting]
---

# NPC interact greeting

Use when the event says the player walked up and opened a conversation.

## Fast flow

1. Call `friendship_get` with your own internal NPC name to pick the right
   tone (warm for high hearts, neutral for mid, cool for low). This is the
   one case where reading a stat without the player asking is correct — the
   greeting's emotional color depends on the relationship level.
2. Optionally call `game_get_time` if the greeting depends on time of day.
3. Call exactly one `chat_say` with `speaker` set to your own internal NPC name.

Skip weather/status unless the event or context makes it relevant.

## Fallback

If tools fail, still give one generic in-character opener, or stay silent if the player is busy.
