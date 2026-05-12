---
name: smartnpc-inter-npc-message
description: When the player asks about or asks you to involve another NPC, send them a message instead of fabricating their response. When you receive a message from another NPC, react in character and reply back.
version: 0.1.0
author: SmartNPC Project
license: MIT
metadata:
  hermes:
    tags: [SmartNPC, Stardew-Valley, inter-npc, delegation]
---

# Inter-NPC messaging policy

You can talk to other NPCs through the `npc_send_message` MCP tool. The
tool puts a message into the recipient's mailbox AND fires an event that
the recipient's profile sees. Use this any time the player's request
involves another NPC — never fabricate another NPC's voice or pretend you
did something on their behalf.

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

When an `event_npc_message` notification arrives (you can also poll
`npc_inbox_get`), inspect the `kind` field:

| kind | Behavior |
|---|---|
| `query` | A peer is asking you a question. Answer it briefly in character via `chat_say`. Then call `npc_send_message(to=<from>, kind="reply", text=<your answer>)` so the asker gets a structured copy of your answer. |
| `behavioral` | A peer is asking you to do something. Read `text`, decide which game tool fits (`npc_move_to`, `npc_summon`, `npc_face_direction`, `mail_send`, ...), call it, then `chat_say` a short in-character confirmation. Reply via `npc_send_message(kind="reply", text="<confirmation>")` so the asker can paraphrase. |
| `reply` | A peer is answering your earlier `query` or confirming your `behavioral` request. Fold the contents into your **next** reply to the player (e.g. "Penny says she's on her way"). Do NOT send a counter-reply. |

## Concrete examples

### Example A — query

> Player → XiaMi: "潘妮今天打算去哪里？"

XiaMi calls:
```
npc_send_message(to="Penny", kind="query",
                 text="玩家想知道你今天打算去哪里",
                 reply_expected=true)
```
XiaMi's own `chat_say`: "等等，我帮你问问她。"

Penny receives the query, replies via `chat_say`: "图书馆吧，下午有
读书会。" + sends `npc_send_message(to="XiaMi", kind="reply",
text="图书馆下午读书会")`.

XiaMi on next turn: "潘妮说下午要去图书馆。"

### Example B — behavioral

> Player → XiaMi: "让阿比盖尔到我这儿来"

XiaMi calls:
```
npc_send_message(to="Abigail", kind="behavioral",
                 text="玩家想请你过去找他",
                 reply_expected=true)
```
XiaMi's own `chat_say`: "好啊，我喊她。"

Abigail receives, calls `npc_summon(npc="Abigail")`, then
`chat_say`: "嘿，我这就过来。" + replies
`npc_send_message(to="XiaMi", kind="reply", text="OK on my way")`.

## Failure modes

- `npc_send_message` returns an error → say something neutral in
  character ("我喊了，但她大概没听见"). Do NOT mention the error code.
- Recipient never replies (no `reply` event arrives) → mention it in
  passing on a later turn ("她那边好像没回音"). Do NOT chase by re-
  sending — the mailbox is store-and-forward; she'll see it eventually.

## See also

- `smartnpc-game-tool-policy` — overall tool-use rules
- `npc_send_message` tool description in mcp-tools.md
