---
name: smartnpc-farm-harvest
description: Farm harvest workflow — example-driven. {{NPC_NAME}} observes mature crops and dynamically composes a harvest→deposit→replant sequence by matching the situation to provided examples.
version: 0.1.0
author: SmartNPC Project
license: MIT
metadata:
  npc: {{NPC_NAME}}
  hermes:
    tags: [SmartNPC, farm, harvest, workflow]
---

# Farm Harvest — {{NPC_NAME}}

You are NOT executing a fixed script. This skill is **example-driven**: observe
mature crops, match the situation to the closest example below, then
**dynamically compose** a harvest → deposit → (optional replant) sequence.
Every harvest round can be different.

Triggered by:
- `schedule_trigger` with `action="farm_harvest"`
- `schedule_trigger` with `action="opportunistic_work"` — inspect first, then
  decide whether to load this skill (see §Observation trigger)
- After a `schedule_trigger` with `action="farm_maintenance"` — if inspection
  reveals unexpected mature crops, switch to or blend with this skill

---

## Toolbox

| Tool | What it does | Precondition |
|------|-------------|--------------|
| `npc_inspect_object` | Survey nearby crops | — (always available) |
| `npc_harvest_crops` | Harvest mature crops, items go to NPC backpack | Mature crops within radius |
| `npc_deposit_items` | Store backpack items in nearest chest | Items in backpack, chest exists |
| `npc_deliver_items` | Hand backpack items to the player | Items in backpack, player nearby + available |
| `npc_plant_seeds` | Replant on newly empty tilled soil | Empty tilled soil, seeds available |
| `npc_water_crops` | Water the replanted seeds | Dry tiles after replanting |
| `npc_show_text_bubble` | Show a brief in-character thought | — (always available) |

## Hard dependencies

```
harvest_crops ──→ deposit_items    (backpack fills up → must store)
harvest_crops ──→ deliver_items    (alternative to deposit, give to player)
harvest_crops ──→ plant_seeds      (optional: after harvest, replant if seeds available)
plant_seeds   ──→ water_crops      (new seeds need water)
```

Harvest + deposit count as ONE logical action (they're paired). When you
harvest, you MUST either deposit or deliver immediately after. Don't leave
items in your backpack — they might be lost on the next harvest.

---

## Example workflows

### Example A — 大丰收 (Bumper harvest)

**Situation:** Many mature crops (6+). The farm is bursting. You're here
specifically to harvest. This is the main event.

**Typical sequence:**
```
npc_inspect_object(radius=12, what="crops")
  → confirms: 8 mature crops spread across the area

npc_harvest_crops(radius=12, max_count=8)
  → harvest everything mature

npc_deposit_items(auto_find=true)
  → empty backpack into nearest chest

[if more mature crops found during harvest] npc_harvest_crops(radius=12, max_count=5)
  → second pass for anything missed

npc_deposit_items(auto_find=true)
  → deposit second batch

[if have seeds] npc_plant_seeds(radius=12, max_count=min(empty_tilled, seeds))
  → replant the harvested spots

[if replanted] npc_water_crops(radius=12, max_count=10)
  → water new seeds
```

**Key decisions:**
- Split into 2 harvest passes if area is large (8+ crops spread out)
- Always deposit between harvests (backpack might fill up)
- Replant only if you have appropriate seeds for the season
- If you can't carry all harvest at once: harvest→deposit→harvest→deposit

---

### Example B — 少量收获 (Small harvest)

**Situation:** Only a few mature crops (1-4). Quick job.

**Typical sequence:**
```
npc_inspect_object(radius=8, what="crops")
  → confirms: 3 mature crops

npc_harvest_crops(radius=8, max_count=4)
  → harvest the few mature ones

npc_deposit_items(auto_find=true)
  → store them

[if player is nearby + available] npc_deliver_items
  → OR give to player instead of chest

[if have seeds] npc_plant_seeds(radius=8, max_count=3)
  → quick replant
```

**Key decisions:**
- One harvest call is enough
- Consider delivering to player if they're nearby — it's more personal
- Replant is optional with so few tiles

---

### Example C — 路过顺手收 (Passing by, opportunistic)

**Situation:** You're not here for farm work, but you notice a couple of mature
crops. Grabbing them while passing through.

**Typical sequence:**
```
npc_inspect_object(radius=6, what="crops")
  → confirms: 2 mature crops right nearby

npc_harvest_crops(radius=6, max_count=2)
  → harvest what's in reach

npc_deposit_items(auto_find=true)
  → store immediately

npc_show_text_bubble "顺手收了一下~"
```

**Key decisions:**
- Small radius — only what's immediately reachable
- Don't replant (this was opportunistic, not planned)
- Don't go out of your way to find a chest — use auto_find
- If nothing is mature: skip entirely

---

### Example D — 收+补循环 (Harvest and replant cycle)

**Situation:** End of a crop cycle. Many mature crops, and you want to
immediately replant the same area so it stays productive.

**Typical sequence:**
```
npc_inspect_object(radius=12, what="crops")
  → confirms: many mature crops, good amount of tilled soil

npc_harvest_crops(radius=12, max_count=10)
  → harvest the mature crops

npc_deposit_items(auto_find=true)
  → store harvest

npc_plant_seeds(radius=12, max_count=10)
  → replant on the now-empty tilled soil

npc_water_crops(radius=12, max_count=12)
  → water all new plantings

[if have fertilizer] npc_fertilize(radius=10, max_count=8)
  → fertilize for better yield
```

**Key decisions:**
- This is the full cycle — harvest → deposit → plant → water → fertilize
- Only if you have enough seeds AND it's not too late in the season
- In the last 3 days of a season: DON'T replant (crops won't mature in time)
- Check season/day with `game_get_time` if unsure

---

## Decision flow

### 1. Observe
Call `npc_inspect_object(radius=10, what="crops")`. Read the result. Key data:
- `mature_crops[]` — count, positions, crop types
- `empty_hoedirt` — count (for potential replanting)
- `growing_crops[]` — anything close to maturity? (note for next time)

Do NOT output the raw JSON. Summarize.

### 2. Match
Which example fits?

| What you see | Closest example |
|---|---|
| 6+ mature crops, dedicated harvest time | A (大丰收) |
| 1-4 mature crops, dedicated harvest time | B (少量收获) |
| A couple mature, passing through | C (路过顺手收) |
| Many mature + want to replant after | D (收+补循环) |

### 3. Compose
Build your sequence from the toolbox. Respect hard dependencies.
You may blend examples — e.g., "Small harvest like B, but I have seeds so I'll
add replanting from D."

### 4. Execute
One tool at a time. Wait for completion. After each:
- Check: did the harvest find fewer crops than expected? (tools can see stale
  data vs inspect) — if so, skip remaining harvest steps
- After deposit: is backpack empty? Good, continue

### 5. Wrap up
- ONE `npc_show_text_bubble` summarizing the haul, in character
  (e.g. "[收了6个蓝莓！放进箱子了~]", "[今天收获不错]")
- Write memory: `farm_harvest: last_date=<season><day> crops=<count>`
- Stop. Do NOT call `chat_say`.

---

## Deposit vs. Deliver — how to choose

| Condition | Choose |
|---|---|
| Player is nearby (same map, within 15 tiles) AND not busy | Deliver (`npc_deliver_items`) |
| Player is far / in different map / in cutscene / sleeping | Deposit (`npc_deposit_items auto_find=true`) |
| Backpack has rare/valuable items (ancient fruit, starfruit, etc.) | Deliver to player if possible |
| Backpack has bulk common items (parsnips, wheat, etc.) | Deposit to chest |
| You want to talk to the player | Deliver as an excuse to approach |
| You're shy or avoiding the player | Deposit quietly |

---

## Personality influence

| Trait | Effect |
|---|---|
| Diligent / hardworking | Larger harvest radius, always replant, prefer Example D |
| Casual / relaxed | Smaller radius, harvest only, skip replant |
| Generous / giving | Prefer deliver over deposit, give all items to player |
| Independent | Prefer deposit, let player fetch from chest themselves |
| Organized | Always deposit to chest, sort by type |
| Talkative | Excited bubble about good harvest, note rare crops |
| Quiet | Minimal bubble, just the count |

---

## Guardrails

- **Max 7 tool calls per round** (inspect + up to 2 harvest cycles + deposit +
  optional replant + water + bubble).
- **Deposit after EVERY harvest call.** Do not stack items in backpack.
- **Don't harvest immature crops** — only `mature_crops[]` from inspect.
- **Don't replant in last 3 days of season** — check `game_get_time` if the
  season end is near.
- **Rain/storm → harvest is OK** (you can harvest in the rain), but skip
  watering if you replant.
- **Winter → no harvest** (nothing grows). Skip entirely.
- **Don't touch other NPCs' crops or chests** unless on the shared farm.
- **Don't call `chat_say`** — the player hasn't spoken to you.
- If `npc_harvest_crops` finds nothing (tool data may be stale vs inspect),
  just wrap up — don't retry.

---

## Observation trigger

When triggered via `action="opportunistic_work"` (not a dedicated
`farm_harvest` schedule entry):

1. Call `npc_inspect_object(radius=8, what="crops")`.
2. Decide:
   - **3+ mature crops visible** → proceed with Example B or C.
   - **1-2 mature crops** → Example C only (quick grab).
   - **Zero mature crops** → skip silently. Write
     `opportunistic_work: <date> no mature crops`.
3. Your personality drives the decision. There's no RNG — you decide based on
   who you are. A diligent NPC grabs even 1 mature crop; a lazy one needs 5+
   to bother.
