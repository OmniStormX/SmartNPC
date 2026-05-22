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

1. Usually call `friendship_get` with your own internal NPC name to choose tone.
2. Optionally call `game_get_time` if the greeting depends on time.
3. Call exactly one `chat_say` with `speaker` set to your own internal NPC name.

Skip weather/status unless the event or context makes it relevant.

## Do not

- do not move, follow, lead, or summon
- do not message other NPCs
- do not output plain text after `chat_say`

## Fallback

If tools fail, still give one generic in-character opener, or stay silent if the player is busy.
