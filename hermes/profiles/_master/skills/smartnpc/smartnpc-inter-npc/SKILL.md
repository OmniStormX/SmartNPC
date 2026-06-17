---
name: smartnpc-inter-npc
description: Inter-NPC messaging. Use when the player asks you to involve another NPC, or when an npc_message wakes this profile. Prevents fabricated peer dialogue and message ping-pong.
version: 0.4.0
author: SmartNPC Project
license: MIT
metadata:
  hermes:
    tags: [SmartNPC, inter-npc, delegation]
---

# NPC 间消息

使用 `npc_send_message` 而非自行编造其他 NPC 的对话或行为。

## A. 玩家要求你让另一个 NPC 参与

| 玩家意图 | 工具 |
|---|---|
| 向另一个 NPC 提问 | `npc_send_message`，kind="query"，reply_expected=true |
| 让另一个 NPC 执行动作 | `npc_send_message`，kind="behavioral"，reply_expected=true |
| 告诉另一个 NPC 某件事 | `npc_send_message`，kind="query"，reply_expected=true |

然后用符合角色性格的方式简短告诉玩家你会去问。不要编造对方的回答。

## B. 你收到了 `npc_message`

强制流程：

1. `npc_inbox_get` — 读取自己的待处理消息。
2. 每项只处理一次。
3. `npc_inbox_ack` — 按 id 移除已处理的项目。

| kind | 动作 |
|---|---|
| `query` | 通过 `npc_send_message` 以 kind="reply" 回答；通常不需要 `chat_say` |
| `behavioral` | 如果安全则执行请求的游戏动作；只有在玩家可听见时可选一条 `chat_say`；发送 kind="reply" |
| `behavioral`（来自管理者的农场任务） | 通过 `skill_view` 加载 `smartnpc-farm-worker`，然后遵循该技能的 §A 或 §B。不要用通用行为流程处理农场任务——农场工人有专门的工作流。 |
| `reply` | 保存或记住，在下次玩家回合时使用；不要再次回复 |

## 防循环规则

- 除非玩家明确发起新的请求，否则绝不要用 kind="query" 或 kind="behavioral" 回复同伴。
- 每个收件箱条目一条回复。
- 即使你保持静默，也要确认已处理的项目。

## 可听性检查

在因 NPC 间事件而开口说话之前，确认玩家能听到：调用 `npc_get_position` 查询自己位置，以及 `player_get_status`。仅在同一地图且玩家不忙时才发言。否则通过 `npc_send_message` 静默回复，并可选择写入记忆。
