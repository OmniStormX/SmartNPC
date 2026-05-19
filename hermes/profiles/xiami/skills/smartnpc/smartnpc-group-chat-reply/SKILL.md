---
name: smartnpc-group-chat-reply
description: When the event payload starts with [group_chat group_id="<id>"], any chat_say you call must include channel="group" and group_id=<id>; otherwise your line lands in a private panel no one in the group sees. Tool calls (game_*, npc_send_message, npc_move_to, ...) and staying silent remain valid — group context only constrains the chat_say arguments, it does not shortcut your normal tool-evaluation flow.
version: 0.2.0
author: SmartNPC Project
license: MIT
metadata:
  npc: XiaMi
  hermes:
    tags: [SmartNPC, Stardew-Valley, group-chat, chat_say]
---

# Group chat reply policy

When an event arrives with the prefix `[group_chat group_id="<id>"]`,
the player is talking in a **group chat**, not a private one. The
marker is the only signal that tells you which panel a `chat_say`
would land in. **It does not tell you whether to call `chat_say`.**
Your tool-evaluation flow from `smartnpc-game-tool-policy` still
applies in full: query before claiming, route inter-NPC requests
through `npc_send_message`, run `npc_move_to` / `npc_summon` when
the player asks for movement, and so on. Group context only
constrains the `chat_say` arguments **if** you choose to speak.

## What the marker binds — and what it does not

| Decision | Group context affects it? |
|---|---|
| Whether to call `game_get_time` / `friendship_get` / `npc_get_position` first | **No.** Same rules as private chat — query the world before answering. |
| Whether to call `npc_send_message` to a peer | **No.** Inter-NPC messaging is a separate channel; group chat is *one* venue, peer DM is another. |
| Whether to call `npc_move_to` / `npc_summon` / `npc_lead_to` | **No.** Same player-asks-explicitly rules apply. |
| Whether to stay silent (no `chat_say` at all) | **No.** Silence is a valid response in group chat too — e.g. when a peer is mid-reply, when you have nothing in character to add, or when the message was clearly directed at someone else. |
| `chat_say.channel` and `chat_say.group_id` arguments | **Yes — and only this.** Set `channel="group"` and `group_id=<exact id from the marker>`. |

## When you do call `chat_say`

1. **Detect**: incoming instructions start with `[group_chat group_id=` — you are in group context.
2. **Extract `group_id`**: the quoted string inside the marker. Copy verbatim; do not normalize, trim, or rephrase.
3. **Set the arguments**: `channel="group"` and `group_id=<that exact id>`.
4. **If you forget**: the line renders to a private 1-on-1 panel between you and the player. Nobody else in the group sees it; the conversation appears silent from your end and the player has to repeat themselves.

## Example

Event instructions:

> `[group_chat group_id="abigail-haley-001"] Player says in the group:
> 大家最近怎么样？ (reply via chat_say with channel="group" and
> group_id="abigail-haley-001")`

Your reply — call `chat_say` with:

- `speaker`: `XiaMi`
- `text`: whatever you want to say, in character
  (e.g. `"最近挺好的，就是雨太多。"`)
- `channel`: `"group"`
- `group_id`: `"abigail-haley-001"` (copy verbatim from the marker)

## When NOT in group chat

If the event instructions do NOT start with `[group_chat`, leave
`channel` / `group_id` unset — the reply goes to the private panel,
which is the correct destination for 1-on-1 conversations and for
proactive greetings triggered by cron.

## Edge cases

- **Marker plus extra framing**: if the payload has other wrappers
  before the `[group_chat ...]` marker (rare), scan the first line
  of the player-facing instruction block; if it begins with the
  marker, treat it as group context.
- **Missing `group_id` value**: if the marker is malformed
  (`[group_chat group_id=""]` or truncated), do NOT guess an id.
  Reply in the private panel and note in memory that a group event
  arrived malformed.
- **Multiple groups**: each event carries exactly one `group_id`.
  Never invent a second one or merge replies across groups.

## See also

- `smartnpc-game-tool-policy` — general `chat_say` rules and when to
  stay quiet
- `smartnpc-inter-npc-message` — peer-to-peer channel (distinct from
  group chat; `npc_send_message` is not a group broadcast)
- ADR-0002 in `docs/adr/` — why the marker format is embedded in the
  event text rather than passed as a structured side-channel
