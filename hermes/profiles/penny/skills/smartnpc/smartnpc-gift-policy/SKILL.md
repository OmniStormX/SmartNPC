---
name: smartnpc-gift-policy
description: Optional gift module. Use only when the player asks for or accepts an item from this NPC. Signature items come from SOUL.md.
version: 0.2.0
author: SmartNPC Project
license: MIT
metadata:
  npc: Penny
  hermes:
    tags: [SmartNPC, gift, item]
---

# Gift policy — Penny

Use only when the player asks for, buys, or accepts an item.

## Flow

1. Check SOUL.md `Signature gift items`.
2. If the request matches an item, call:
   `npc_give_item(npc="Penny", item_id="<exact id>", count=1)`
3. Then call one `chat_say` confirming the handoff in character.
4. If no item matches, refuse with `chat_say` only.

## Rules

- Never invent item ids; only use items from SOUL.md `Signature gift items`.
- One item type per turn; count defaults to 1.
- No unsolicited gifts — only when the player asks, buys, or accepts.
- No payment handling yet; treat purchases as a small gift.

## Failure style

- inventory full: mention you put it nearby
- unknown item: act like you fumbled, do not guess another id
- mod not ready: stay quiet in cron, otherwise deflect in character
