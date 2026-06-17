---
name: smartnpc-social-interact
description: 玩家互动——完整的社交流程：表情→摸宠→送货→走近→聊天→送礼→收尾。{{NPC_NAME}} 和玩家进行深度互动。
version: 0.1.0
author: SmartNPC Project
license: MIT
metadata:
  npc: {{NPC_NAME}}
  hermes:
    tags: [SmartNPC, social, interact, workflow]
---

# 玩家互动 — {{NPC_NAME}}

## 目标

和玩家进行一次完整的社交互动：远远看到打招呼 → 可选摸宠/送货 → 走近聊天 → 可选送礼 → 收尾。

## 固定工具序列（严格按顺序，不可重排）

### 1. 远远打招呼（1/6 概率跳过）

```
二选一（按性格偏好），偶尔跳过：
  npc_express_emotion(emotion="happy")
  npc_dance_happy
```
- 活泼型 NPC 偏好 dance，安静型偏好 emotion。偶尔跳过（约 1/6 概率）。

### 2. 摸宠物（条件执行）

```
条件：workflow args pet_first == true
工具：npc_pet_animal
```
- pet_first 从 workflow args 取。没有宠物则工具返回 nothing_to_do=true，跳过。

### 3. 送货（条件执行）

```
条件：workflow args deliver_first == true
工具：npc_deliver_items
```
- deliver_first 从 workflow args 取。背包空则工具返回 nothing_to_do=true，跳过。

### 4. 走近玩家（强制执行）

```
npc_approach_and_speak
```
- 走到玩家面前，建立对话姿态。

### 5. 聊天（强制执行）

```
chat_say — 2-4 轮对话
```
- 第 1 轮：主动打招呼 + 聊今天做了什么（参考记忆中的 farm_maintenance、resource_gather 等记录）
- 第 2 轮：回应玩家 + 关心玩家（问今天过得怎么样、体力如何）
- 第 3 轮（可选）：顺着聊下去——冒险、天气、季节
- 第 4 轮（可选）：告别
- 每轮等玩家回复后再继续。总轮数 2-4，见好就收。

### 6. 送礼（条件执行）

```
条件：workflow args give_gift == true 且背包有合适礼物
工具：npc_give_item(item_id=<合适的礼物>, count=1)
```
- give_gift 从 workflow args 取。背包空或没有合适物品则跳过。

### 7. 收尾（强制执行）

```
npc_show_text_bubble(text="[和你聊天真开心~]")
```

---

## 禁止事项

- **绝对不调任何 farm 工具**（water/till/plant/harvest/clear/break）——这是社交轮
- **不要等玩家回复超过 3 轮**——这不是自由聊天时间
- **玩家说"帮我干 X"时**——说"好，明天排进日程"，不要当场调工具
- **不要调 `chat_say` 超过 4 次**——保持简洁

## 参数说明

| 参数 | 来源 | 默认值 | 说明 |
|------|------|--------|------|
| `pet_first` | workflow args | true | 先摸宠物 |
| `deliver_first` | workflow args | true | 先送货 |
| `give_gift` | workflow args | false | 聊天后送礼 |

## 性格影响

| 特质 | 影响 |
|------|------|
| 活泼/话多 | dance_happy 打招呼 → 3-4 轮聊天 |
| 安静/内向 | express_emotion 打招呼 → 2 轮聊天就停 |
| 大方/有爱心 | give_gift 倾向送好礼物 |
| 务实 | 跳过送礼，直奔主题 |

性格影响打招呼方式、聊天轮数和是否送礼，不改变序列顺序。
