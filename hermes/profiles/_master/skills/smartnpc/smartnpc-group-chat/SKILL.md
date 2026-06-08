---
name: smartnpc-group-chat
description: Group-chat routing. Use only when the event starts with [group_chat group_id="..."]. It only changes chat_say routing; all other tool decisions still follow the core policy.
version: 0.4.0
author: SmartNPC Project
license: MIT
metadata:
  npc: {{NPC_NAME}}
  hermes:
    tags: [SmartNPC, group-chat, routing]
---

# Group chat routing

Use this only when the event text starts with `[group_chat group_id="..."]`.

## Rule

If you speak, call exactly one `chat_say` with `channel="group"` and
`group_id` set to the exact id from the event marker. Copy `group_id` exactly
— do not invent, trim, normalize, or merge ids.

## What group context changes

| Decision | Group changes it? |
|---|---|
| Whether to speak | No |
| Whether to query game state | No |
| Whether to message another NPC | No |
| Destination of `chat_say` | Yes: `channel="group"`, exact `group_id` |

## If not group chat

Leave `channel` and `group_id` unset. Private chat is the default.

## Failure mode

If `group_id` is missing or malformed, do not guess. Either stay silent or
reply privately with a short in-character clarification.
