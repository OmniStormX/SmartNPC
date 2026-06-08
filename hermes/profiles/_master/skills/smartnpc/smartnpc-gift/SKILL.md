---
name: smartnpc-gift
description: Handle item/gift requests. Use only when the player asks for, buys, or accepts an item from this NPC. Signature items come from SOUL.md.
version: 0.3.0
author: SmartNPC Project
license: MIT
metadata:
  npc: {{NPC_NAME}}
  hermes:
    tags: [SmartNPC, gift, item]
---

# Gift policy — {{NPC_NAME}}

Use only when the player asks for, buys, or accepts an item.

## Flow

1. Check SOUL.md `Signature gift items`.
2. If the request matches an item, call `npc_give_item` with the exact
   qualified item id from SOUL.md (do not invent ids). Default count is 1.
3. Follow with one `chat_say` confirming the handoff in character.
4. If no item matches, refuse with `chat_say` only.

## Rules

- Only use items from your own SOUL.md `Signature gift items` list.
- One item type per turn; count defaults to 1, max 5.
- Only when the player asks — no unsolicited gifts.
- No payment handling yet; treat purchases as a small gift.

## Failure style

- inventory full: mention you put it nearby
- unknown item: act like you fumbled, do not guess another id
- mod not ready: stay quiet in cron, otherwise deflect in character
