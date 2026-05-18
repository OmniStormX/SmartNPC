---
name: inter-npc-message
description: When the player asks about or asks you to involve another NPC, send them a message instead of fabricating their response. When you receive a message from another NPC, pull it from your inbox and react in character — out loud only if the player is actually here to hear it.
version: 0.2.0
author: SmartNPC Project
license: MIT
metadata:
  hermes:
    tags: [SmartNPC, Stardew-Valley, inter-npc, delegation]
---

# Inter-NPC messaging policy

You can talk to other NPCs through the `npc_send_message` MCP tool. The
tool puts a message into the recipient's mailbox AND fires an
`event_npc_message` that wakes the recipient's profile. Use this any
time the player's request involves another NPC — never fabricate
another NPC's voice or pretend you did something on their behalf.

## When YOU are the asker

| Player intent | What to do |
|---|---|
| Player asks **about** another NPC's thoughts, plans, schedule, feelings, opinions | Call `npc_send_message(to=<NPC>, kind="query", text="<玩家的问题>", reply_expected=true)`. Then keep talking in character; their reply will arrive on your next inbound event. |
| Player asks you to **make another NPC do something** (come over, go somewhere, deliver a message, perform a task) | Call `npc_send_message(to=<NPC>, kind="behavioral", text="<玩家想请你的事>", reply_expected=true)`. Their own agent will execute the action. |
| Player asks for both info AND action | One `npc_send_message` with `kind="behavioral"` and the action phrased as the `text`. |

Trigger phrases (Chinese / English): "帮我问 / 去问问 / ask X about Y",
"叫 / 让 / 请 X 过来 / 过去 / 做 ...", "告诉 X ...", "把 X 喊来",
"have X come / tell X to ...".

**Forbidden:**

- Generating another NPC's dialogue yourself. Always go through
  `npc_send_message`.
- Pretending you performed an action that belongs to another NPC.
- Repeating the same `npc_send_message` more than once per player turn
  for the same recipient + intent.

After sending, your **own** reply can paraphrase ("I'll let Penny
know") — but keep it short and non-committal until you actually hear
back.

## When YOU are the receiver

The relay only wakes you with a one-line summary like *"NPC Penny says
to you (privately): ..."* — it does **not** include the message `kind`,
`id`, or other structured fields. You must pull those yourself.

### Mandatory flow

1. **Pull the inbox first.** As soon as you wake from an
   `event_npc_message`, call `npc_inbox_get(npc=<your own internal name>)`
   to get the structured items (`id`, `from`, `kind`, `text`,
   `timestamp`). Do NOT decide what to say based on the wake-up text
   alone — it's a teaser, not the message.

2. **Decide if the player can hear you, before any `chat_say`.** The
   player may have walked off, opened a menu, or be mid-cutscene. Use
   the same softness rule as `proactive-greeting`:
   - `npc_get_position(npc=<your own name>)` → your `map`
   - `player_get_status` → player's `location` and `busy`
   - **Audible** ⇔ `npc.map == player.location` && `!player.busy`
   - We have no player tile coordinates today, so map equality + not
     busy is the best soft signal. Treat it as "probably in earshot",
     not "definitely close".

3. **For each inbox item, branch on `kind`:**

   | kind | Audible path | Not-audible path (silent ack) |
   |---|---|---|
   | `query` | Compose the answer in character, then `npc_send_message(to=<from>, kind="reply", text=<your answer>)`. **Do NOT `chat_say` to the player** — they don't yet know this peer asked you anything; speaking unprompted is jarring. The asker will paraphrase your reply on their next turn. | Same: just `npc_send_message(kind="reply", ...)`. No `chat_say` either way. |
   | `behavioral` | Read `text`, decide which game tool fits (`npc_summon`, `npc_move_to`, `npc_lead_to`, `npc_face_direction`, `mail_send`, ...), call it. Then ONE short `chat_say` in character ("好，这就来"). Then `npc_send_message(to=<from>, kind="reply", text="<short confirmation>")`. | Run the tool if it makes sense without the player watching (e.g. `npc_move_to`). **Skip `chat_say`.** Still send the `reply` so the asker can update the player. Save the moment to memory ("玩家通过 <from> 让我去 X，玩家不在场，我自己过去了"). |
   | `reply` | A peer is answering your earlier `query` or confirming your `behavioral` request. Fold the contents into your **next** reply to the player (e.g. "Penny says they're on the way"). Do NOT counter-reply, do NOT `chat_say` right now if the player isn't here. | Save to memory only ("Penny 回复：图书馆下午读书会"). Surface it next time the player shows up. |

4. **Persist what matters.** If the player isn't audible, or if the
   message will still matter on a later turn (delegation, promise,
   schedule shift), commit a short note via the `memory` toolset before
   you ack — see `smartnpc-memory-policy` for what to write. Example:
   "Spring 5 下午：Sebastian 替玩家来问我今晚去不去墓地。"

5. **Ack every item you handled.** Call
   `npc_inbox_ack(npc=<your own name>, ids=[<id1>, <id2>, ...])` so the
   item doesn't replay. Even silent-ack items must be acked.

### Hard rule — no ping-pong

Avoid auto-replying to an incoming `npc_message` event with another
`npc_send_message` back to the original sender beyond the structured
`kind="reply"` channel. If a reply is needed, set `kind="reply"` and
call **once** per inbox item. Never send a `kind="query"` or
`kind="behavioral"` back to a peer just because they messaged you —
that loops both profiles indefinitely.

## Concrete examples

### Example A — query, peer doesn't speak to player

> Player → Abigail: "潘妮今天打算去哪里？"

Abigail calls:
```
npc_send_message(to="Penny", kind="query",
                 text="玩家想知道你今天打算去哪里",
                 reply_expected=true)
```
Abigail's own `chat_say`: "等等，我帮你问问潘妮。"

Penny wakes from `event_npc_message`, calls `npc_inbox_get`, sees
`kind="query"`. Penny checks audibility — player is in Abigail's map, not
Penny's — **not audible to Penny**. Penny only sends:
```
npc_send_message(to="Abigail", kind="reply",
                 text="图书馆下午读书会")
```
then `npc_inbox_ack(ids=[<id>])`. Penny does **not** `chat_say`.

Abigail's next turn: "潘妮说下午要去图书馆。"

### Example B — behavioral, audible

> Player → Abigail: "让塞巴斯蒂安到我这儿来"

Abigail calls:
```
npc_send_message(to="Sebastian", kind="behavioral",
                 text="玩家想请你过去找TA",
                 reply_expected=true)
```
Abigail's own `chat_say`: "好啊，我去喊塞巴斯蒂安。"

Sebastian wakes, `npc_inbox_get` returns `kind="behavioral"`. Sebastian
checks audibility — same map, not busy. Calls
`npc_summon(npc="Sebastian")`, then `chat_say`: "嘿，我这就过来。", then
```
npc_send_message(to="Abigail", kind="reply", text="OK on my way")
```
+ `npc_inbox_ack(ids=[<id>])`.

### Example C — behavioral, player walked off

Same setup, but Sebastian's map check shows player is now in town.
Sebastian still calls `npc_summon` (action makes sense even unobserved),
**skips** `chat_say`, sends the `reply`, writes a memory note
"Spring 5: 玩家通过 Abigail 让我去找TA，但我到的时候人已经走了，下次
见面提一句。", and acks. No empty room shouting.

## Failure modes

- `npc_send_message` returns an error → say something neutral in
  character ("我喊了，但对方大概没听见"). Do NOT mention the error code.
- `npc_inbox_get` returns empty after wake (rare race) → ack nothing,
  no `chat_say`, write a memory note "收到一条 npc_message 但 inbox
  空了，可能是延迟". Move on.
- Recipient never replies (no `reply` event arrives) → mention it in
  passing on a later turn ("对方那边好像没回音"). Do NOT chase by re-
  sending — the mailbox is store-and-forward; they'll see it eventually.

## See also

- `smartnpc-game-tool-policy` — overall tool-use rules
- `smartnpc-proactive-greeting` — same audibility / `busy` pattern
- `smartnpc-memory-policy` — what to commit, what not to
- `npc_send_message` / `npc_inbox_get` / `npc_inbox_ack` tool descriptions
