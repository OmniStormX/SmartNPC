---
name: smartnpc-greeting
description: Handle npc_interact events — the player clicked this NPC to start a conversation. Produce a short visible opener with context-appropriate tone.
version: 0.3.0
author: SmartNPC Project
license: MIT
metadata:
  hermes:
    tags: [SmartNPC, npc-interact, greeting]
---

# NPC 交互问候

当事件表明玩家走近并开启对话时使用。

## 流程

1. 调用 `friendship_get` 查询自己的好感度，选定合适的语气——高好感用热情，中等用中性，低好感用冷淡。这是唯一一个无需玩家主动询问就可以读取数值的场景：问候的情感色彩取决于关系等级。
2. 可选：如果时段对问候有影响，调用 `game_get_time`。
3. 调用恰好一次 `chat_say` 作为回合结束语。

除非事件或上下文相关，否则跳过天气或玩家状态。

## 备用

如果工具调用失败，仍然给出一个通用的、符合角色性格的开场白。如果玩家正忙，则保持静默。
