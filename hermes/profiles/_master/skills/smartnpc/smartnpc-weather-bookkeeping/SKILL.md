---
name: smartnpc-weather-bookkeeping
description: 雨天室内——整理库存、室内休息。{{NPC_NAME}} 下雨天在室内度过。
version: 0.1.0
author: SmartNPC Project
license: MIT
metadata:
  npc: {{NPC_NAME}}
  hermes:
    tags: [SmartNPC, weather, indoor, workflow]
---

# 雨天室内 — {{NPC_NAME}}

## 目标

下雨天不用出门。在室内查看背包 → 整理入库或休息 → 享受雨天。

## 固定工具序列（严格按顺序，不可重排）

### 1. 查看背包

```
npc_inventory_get
```
- 查看背包是否有东西。有物品则下一步 deposit，空则 idle_activity。

### 2. 整理或休息（强制执行，二选一）

```
如果有物品（inventory 非空）：
  npc_deposit_items(auto_find=true)

如果背包空：
  npc_idle_activity(duration_ticks=300)
```
- **确定性规则**：有物品 → 整理入库；背包空 → 室内休息。不随机。

### 3. 收尾（强制执行）

```
npc_show_text_bubble(text="[下雨天，在家待着也不错]")
```
- 根据性格调整文案，如 "[难得清闲~]" / "[听听雨声……]"。

---

## 禁止事项

- **绝对不调任何户外工具**（water/till/plant/harvest/clear/break/forage_collect/wander）
- **绝对不调 `chat_say`**——这是 solo 回合
- **绝对不调 `npc_wander`**——室内走动没意义

## 参数说明

无额外参数。

## 性格影响

| 特质 | 影响 |
|------|------|
| 勤奋/有条理 | 背包空时也调 deposit（检查箱子整理） |
| 随性/慵懒 | 背包非空也可能选 idle（"明天再说"） |
| 安静 | 简单 bubble，安静享受雨天 |

性格影响 deposit/idle 决策倾向和 bubble 风格，不改变序列顺序。
