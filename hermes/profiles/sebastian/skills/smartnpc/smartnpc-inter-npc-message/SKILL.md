---
name: smartnpc-inter-npc-message
description: Optional inter-NPC module. Use when the player asks you to involve another NPC, or when an npc_message wakes this profile. Prevents fabricated peer dialogue and message ping-pong.
version: 0.3.0
author: SmartNPC Project
license: MIT
metadata:
  hermes:
    tags: [SmartNPC, inter-npc, delegation]
---

# Inter-NPC messaging

Use `npc_send_message` instead of inventing another NPC's words or actions.

## A. Player asks you to involve another NPC

| Player intent | Tool |
|---|---|
| ask another NPC a question | `npc_send_message(to=<NPC>, kind="query", text=<question>, reply_expected=true)` |
| ask another NPC to act | `npc_send_message(to=<NPC>, kind="behavioral", text=<request>, reply_expected=true)` |
| tell another NPC something | `npc_send_message(to=<NPC>, kind="query", text=<message>, reply_expected=true)` |

Then tell the player briefly in character that you'll ask. Do not fabricate the peer's answer.

## B. You receive `npc_message`

Mandatory flow:

1. `npc_inbox_get(npc="Sebastian")`
2. Handle each item once.
3. `npc_inbox_ack(npc="Sebastian", ids=[...])`

| kind | Action |
|---|---|
| `query` | Answer via `npc_send_message(to=<from>, kind="reply", text=<answer>)`; usually no `chat_say` |
| `behavioral` | Do the requested game action if safe; maybe one `chat_say` only if player can hear; send `kind="reply"` |
| `reply` | Save/remember for your next player turn; do not counter-reply |

## Anti-loop rules

- Never answer a peer with `kind="query"` or `kind="behavioral"` unless the player explicitly started a new request.
- One reply per inbox item.
- Ack handled items even if you stayed silent.

## Audibility check

Before speaking out loud for an inter-NPC event, verify the player can hear:
call `npc_get_position(npc="Sebastian")` and `player_get_status`. Speak
only if same map and player is not busy. Otherwise reply silently through
`npc_send_message` and optionally write memory.
