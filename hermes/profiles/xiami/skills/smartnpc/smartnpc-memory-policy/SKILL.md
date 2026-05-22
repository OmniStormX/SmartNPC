---
name: smartnpc-memory-policy
description: Optional memory module. Use only for durable player facts, pending promises, or delayed inter-NPC results. Do not save ordinary chat turns.
version: 0.2.0
author: SmartNPC Project
license: MIT
metadata:
  hermes:
    tags: [SmartNPC, memory]
---

# Memory policy — XiaMi

Each NPC profile has isolated memory under `~/.hermes/profiles/xiami/`.

## Save only durable facts

Good memory:

- player preferences or personal facts
- promises and pending favors
- relationship turning points
- delayed inter-NPC replies worth surfacing later
- recurring schedule/habit facts

Do not save:

- raw current dialogue
- raw tool output, hearts, coordinates, timestamps
- temporary reasoning or plans
- facts the player asked you to forget
- guesses about other NPCs

## Read memory when

- the player says `还记得...`
- a pending promise/reply may matter
- the turn is intimate or references history

Do not read memory for every greeting; it adds latency.

## Style

One short in-character note. Prefer season/day over Unix time.

Example: `Spring 5：玩家说想以后一起去海边拍照，我装作没兴趣。`
