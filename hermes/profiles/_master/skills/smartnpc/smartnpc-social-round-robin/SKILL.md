---
name: smartnpc-social-round-robin
description: 轻量社交回合——闲逛/跳舞→走近→单向聊天→收尾。{{NPC_NAME}} 进行一次简短的社交，不等玩家回复。
version: 0.1.0
author: SmartNPC Project
license: MIT
metadata:
  npc: {{NPC_NAME}}
  hermes:
    tags: [SmartNPC, social, round-robin, workflow]
---

# 轻量社交 — {{NPC_NAME}}

## 目标

进行一次简短的社交回合：走动/跳舞 → 走近 → 单向聊天 → 收尾。不等玩家回复——这是单向回合。

## 固定工具序列（严格按顺序，不可重排）

### 1. 先动起来（强制执行）

```
二选一（按性格偏好）：
  npc_wander(duration_ticks=200, radius=8)
  npc_dance_happy
```
- 活泼型 NPC 偏好 dance，安静的偏好 wander。绝不呆站。

### 2. 走近玩家（强制执行）

```
npc_approach_and_speak
```

### 3. 单向聊天（强制执行）

```
chat_say × 1-2 句
```
- 打招呼 + 简短分享今天做了什么（参考记忆）。不等回复——这是单向回合。
- 例："嘿！今天翻了三块地，手都起茧了！" 或 "今天天气真好，出来走走~"

### 4. 收尾（强制执行）

```
npc_show_text_bubble(text="[社交真开心~]")
```

---

## 禁止事项

- **绝对不送礼物**（`npc_give_item`）
- **绝对不送货**（`npc_deliver_items`）
- **绝对不摸宠物**（`npc_pet_animal`）
- **绝对不等玩家回复**
- **绝对不调任何 farm 工具**
- **不要超过 2 句 chat_say**

## 参数说明

无额外参数。mode 固定为 `casual_round`。

## 性格影响

| 特质 | 影响 |
|------|------|
| 活泼 | dance_happy → 兴奋的打招呼 |
| 安静 | wander → 简短一句 |
| 社交型 | 2 句 chat_say，带表情 |
| 内向 | 1 句 chat_say，wander 更久 |

性格影响动作选择和聊天长度，不改变序列顺序。
