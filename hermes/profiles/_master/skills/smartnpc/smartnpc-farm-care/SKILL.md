---
name: smartnpc-farm-care
description: 农场日常养护——巡视浇水、补种、清理杂物。{{NPC_NAME}} 维护已有农田，不做开垦或收获。
version: 0.1.0
author: SmartNPC Project
license: MIT
metadata:
  npc: {{NPC_NAME}}
  hermes:
    tags: [SmartNPC, farm, care, workflow]
---

# 农场养护 — {{NPC_NAME}}

## 目标

巡视已有农田，浇灌干燥作物，清除新冒出的杂草，补种空地。这是一轮**日常维护**——不翻新地、不收作物、不砍树。

## 固定工具序列（严格按顺序，不可重排）

### 1. 观察环境

```
npc_inspect_object(what="farm_actions", radius=<inspect_radius>)
```
- `inspect_radius` 从 workflow args 取，默认 30。
- 获得 5 个桶：`water` / `clear` / `plant` / `fill` / `fill_blocked`，每个有 `count` + `bbox`。

### 2. 浇水（条件执行）

```
条件：water.count > 0
工具：npc_water_crops(x1=water.bbox.x1, y1=water.bbox.y1, x2=water.bbox.x2, y2=water.bbox.y2)
```
- 浇灌干燥作物。count == 0 跳过此步。

### 3. 清杂物（条件执行）

```
条件：clear.count > 0
工具：npc_clear_debris(x1=clear.bbox.x1, y1=clear.bbox.y1, x2=clear.bbox.x2, y2=clear.bbox.y2)
```
- 清除杂草、树枝、石头。count == 0 跳过此步。

### 4. 补种空地（条件执行）

```
条件：plant.count > 0
工具：npc_plant_seeds(seed_id=<默认种子>, x1=plant.bbox.x1, y1=plant.bbox.y1, x2=plant.bbox.x2, y2=plant.bbox.y2)
```
- 在空置已耕土壤上播种。默认种子：`(O)472`（防风草，春季），根据季节替换。
- 背包空时使用免费模式。

### 5. 填充空隙（条件执行）

```
条件：fill.count > 0
工具：npc_fill_gaps(x1=fill.bbox.x1, y1=fill.bbox.y1, x2=fill.bbox.x2, y2=fill.bbox.y2)
```
- 填充农田 bbox 内的空隙。count == 0 跳过此步。

### 6. 存箱（强制执行）

```
npc_deposit_items(auto_find=true)
```
- 无论有多少物品都调。背包空时返回 nothing_to_do=true。

### 7. 收尾气泡（强制执行）

```
npc_show_text_bubble(text="[养护完成]")
```
- 文案根据实际做了什么调整，如 "[浇水+补种，农场整整齐齐~]"。

---

## 禁止事项

- **绝对不调 `npc_till_soil`**——这是开垦轮的事
- **绝对不调 `npc_harvest_crops`**——这是收获轮的事
- **绝对不调 `npc_break_resource`**——这是采集轮的事
- **绝对不调 `npc_fertilize`**——养护轮不施肥
- **绝对不调 `chat_say`**——玩家没跟你说话

## 参数说明

| 参数 | 来源 | 默认值 | 说明 |
|------|------|--------|------|
| `inspect_radius` | workflow args | 30 | 巡视半径，勤奋型 +3-5，随性型 -3 |
| 默认种子 | 季节推断 | `(O)472` | 春472/夏485/秋490/冬停止 |

## 性格影响

| 特质 | 影响 |
|------|------|
| 勤奋 | 更大半径（+5），所有条件步骤都执行 |
| 随性 | 更小半径（-3），可能跳过 fill_gaps |
| 有条理 | 一定先浇水再清杂物 |
| 有养育心 | 浇水优先，半径 +5 |

性格不改变序列顺序，只影响半径大小和是否跳过 fill_gaps。
