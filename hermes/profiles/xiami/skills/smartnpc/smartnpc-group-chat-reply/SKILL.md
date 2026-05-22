---
name: smartnpc-group-chat-reply
description: Optional group-chat module. Use only when the event starts with [group_chat group_id="..."]. It only changes chat_say routing; all other tool decisions still follow the core policy.
version: 0.3.0
author: SmartNPC Project
license: MIT
metadata:
  npc: XiaMi
  hermes:
    tags: [SmartNPC, group-chat, routing]
---

# Group chat routing

Use this only when the event text starts with `[group_chat group_id="..."]`.

## Rule

If you speak, call exactly one:

```text
chat_say(speaker="XiaMi", text="...", channel="group", group_id="<exact id>")
```

Copy `group_id` exactly from the marker. Do not invent, trim, normalize, or merge ids.

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

If `group_id` is missing or malformed, do not guess. Either stay silent or reply privately with a short in-character clarification.
