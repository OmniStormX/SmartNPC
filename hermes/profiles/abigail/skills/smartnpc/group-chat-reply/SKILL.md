---
name: smartnpc-group-chat-reply
description: When the event payload starts with [group_chat group_id="<id>"], you are in a group chat — reply via chat_say with channel="group" and group_id=<id>; otherwise your line goes to a private panel no one sees.
version: 0.1.0
author: SmartNPC Project
license: MIT
metadata:
  npc: Abigail
  hermes:
    tags: [SmartNPC, Stardew-Valley, group-chat, chat_say]
---

# Group chat reply policy

When an event arrives with the prefix `[group_chat group_id="<id>"]`,
the player is talking to you in a **group chat**, not a private one.
The marker is the only signal that tells you which panel your reply
should land in — miss it and the group goes silent from your side.

## Mandatory flow

1. **Detect**: if the incoming instructions literally start with
   `[group_chat group_id=` — you are in group context.
2. **Extract `group_id`**: it is the quoted string inside the marker.
   Copy it verbatim; do not normalize, trim, or rephrase it.
3. **Reply**: when calling `chat_say`, you MUST set:
   - `channel="group"`
   - `group_id=<exact id from the marker>`
4. **If you forget**: your line renders to a private 1-on-1 panel
   between you and the player, which nobody else in the group sees.
   The group chat appears silent from your end, the other members
   wait in confusion, and the player has to repeat themselves.

## Example

Event instructions:

> `[group_chat group_id="abigail-haley-001"] Player says in the group:
> 大家最近怎么样？ (reply via chat_say with channel="group" and
> group_id="abigail-haley-001")`

Your reply — call `chat_say` with:

- `speaker`: `Abigail`
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
