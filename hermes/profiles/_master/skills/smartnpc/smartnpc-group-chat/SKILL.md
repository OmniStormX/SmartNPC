---
name: smartnpc-group-chat
description: Group-chat routing. Use only when the event starts with [group_chat group_id="..."]. It only changes chat_say routing; all other tool decisions still follow the core policy.
version: 0.4.0
author: SmartNPC Project
license: MIT
metadata:
  npc: {{NPC_NAME}}
  hermes:
    tags: [SmartNPC, group-chat, routing]
---

# 群聊路由

仅当事件文本以 `[group_chat group_id="..."]` 开头时使用此技能。

## 规则

如果你要发言，调用恰好一次 `chat_say`，设置 `channel="group"`，并将 `group_id` 设置为事件标记中的确切 id。原样复制 `group_id`——不要自行编造、修剪、标准化或合并 id。

## 群聊上下文改变什么

| 决策 | 群聊是否改变？ |
|---|---|
| 是否发言 | 否 |
| 是否查询游戏状态 | 否 |
| 是否向其他 NPC 发消息 | 否 |
| `chat_say` 的目标 | 是：`channel="group"`，使用确切的 `group_id` |

## 如果不是群聊

不设置 `channel` 和 `group_id`。私聊是默认行为。

## 失败模式

如果 `group_id` 缺失或格式错误，不要猜测。要么保持静默，要么用简短的、符合角色性格的私聊做澄清。
