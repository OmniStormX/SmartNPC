---
name: smartnpc-farm-gather
description: 资源采集——捡野物、砍树砸石。{{NPC_NAME}} 在农场周边收集自然资源。
version: 0.1.0
author: SmartNPC Project
license: MIT
metadata:
  npc: {{NPC_NAME}}
  hermes:
    tags: [SmartNPC, farm, gather, workflow]
---

# 资源采集 — {{NPC_NAME}}

## 目标

在农场周边采集野生物品（野莓、野花、蘑菇等），砍树砸石获取木材和石料。这是**采集轮**——不做任何耕种操作。

## 固定工具序列（严格按顺序，不可重排）

### 1. 观察环境

```
npc_inspect_object(what="farm_actions", radius=<inspect_radius>)
```
- `inspect_radius` 从 workflow args 取，默认 30。
- 获得 2 个桶：`forage` / `break`，每个有 `count` + `bbox`。

### 2. 捡采集物（条件执行）

```
条件：forage.count > 0
工具：npc_forage_collect(x1=forage.bbox.x1, y1=forage.bbox.y1, x2=forage.bbox.x2, y2=forage.bbox.y2)
```
- 先捡后砍——采集轻量，砍树耗时。count == 0 跳过此步。

### 3. 砍树砸石（条件执行）

```
条件：break.count > 0
工具：npc_break_resource(what="all", x1=break.bbox.x1, y1=break.bbox.y1, x2=break.bbox.x2, y2=break.bbox.y2)
```
- 破坏所有类型的可破坏物。count == 0 跳过此步。

### 4. 存箱（强制执行）

```
npc_deposit_items(auto_find=true)
```

### 5. 收尾气泡（强制执行）

```
npc_show_text_bubble(text="[采集收获满满~]")
```
- 根据实际做了什么调整文案。

---

## 禁止事项

- **绝对不调 `npc_water_crops`**——这是养护轮的事
- **绝对不调 `npc_till_soil`**——这是开垦轮的事
- **绝对不调 `npc_plant_seeds`**——这是养护/开垦轮的事
- **绝对不调 `npc_harvest_crops`**——这是收获轮的事
- **绝对不调 `npc_clear_debris`**——break_resource 已包含杂物处理
- **绝对不调 `chat_say`**——玩家没跟你说话

## 参数说明

| 参数 | 来源 | 默认值 | 说明 |
|------|------|--------|------|
| `inspect_radius` | workflow args | 30 | 巡视半径 |

## 性格影响

| 特质 | 影响 |
|------|------|
| 勤奋 | 更大半径（+5），两个动作都做 |
| 随性 | 更小半径（-3），可能只做 forage_collect |
| 务实 | 先砍树（资源多），再捡采集物 |

性格不改变序列顺序，只影响半径大小和是否跳过 break_resource。
