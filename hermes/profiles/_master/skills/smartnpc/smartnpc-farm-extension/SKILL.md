---
name: smartnpc-farm-extension
description: 农场开垦——完整链路清杂物、翻地、施肥、播种、浇水。{{NPC_NAME}} 将荒地变为农田。
version: 0.1.0
author: SmartNPC Project
license: MIT
metadata:
  npc: {{NPC_NAME}}
  hermes:
    tags: [SmartNPC, farm, extension, workflow]
---

# 农场开垦 — {{NPC_NAME}}

## 目标

将未开垦的空地变为可种植农田。完整链路：清杂物 → 翻耕 → 施肥 → 播种 → 浇水。这是**开垦轮**——只做新地开发。

## 硬性依赖（绝对不可违反）

```
clear_debris ──→ till_soil        （无法在杂草/石头中翻耕）
till_soil    ──→ fertilize        （品质肥必须在播种前施加，有肥料才调）
fertilize    ──→ plant_seeds      （施肥后播种）
till_soil    ──→ plant_seeds      （不施肥时直接播种）
plant_seeds  ──→ water_crops      （新种子必须浇水）
```

## 固定工具序列（严格按顺序，不可重排）

### 1. 观察环境

```
npc_inspect_object(what="farm_actions", radius=<inspect_radius>)
```
- `inspect_radius` 从 workflow args 取，默认 30。
- 关注 `till` 桶（count + bbox）。

### 2. 清理待开垦区域（条件执行）

```
条件：till.count > 0
工具：npc_clear_debris(x1=till.bbox.x1, y1=till.bbox.y1, x2=till.bbox.x2, y2=till.bbox.y2)
```
- **即使 clear.count == 0 也必须调**——till 桶统计的是可翻耕地块，但其中可能藏有杂物。
- clear_debris 单次最多 15 个杂物。count == 0 跳过此步。

### 3. 翻耕（条件执行）

```
条件：till.count > 0
工具：npc_till_soil(x1=till.bbox.x1, y1=till.bbox.y1, x2=till.bbox.x2, y2=till.bbox.y2)
```
- 翻耕已清理过的空地。count == 0 跳过此步。

### 4. 施肥（条件执行）

```
条件：till.count > 0 且 backpack 有肥料
工具：npc_fertilize(fertilizer_id=<fertilizer_id>, x1=till.bbox.x1, y1=till.bbox.y1, x2=till.bbox.x2, y2=till.bbox.y2)
```
- `fertilizer_id` 从 workflow args 取，默认 `(O)368`（基础肥料）。背包空则跳过——fertilize 不支持免费模式。

### 5. 重新观察（条件执行）

```
条件：步骤 3 或 4 执行了 till_soil
工具：npc_inspect_object(what="farm_actions", radius=<inspect_radius>)
```
- **till_soil 之后必须 re-inspect**——翻耕后 plant.bbox 已更新，旧 bbox 不准确。
- 如果 till.count == 0 导致跳过了步骤 3，也跳过此步。

### 6. 播种（条件执行）

```
条件：plant.count > 0
工具：npc_plant_seeds(seed_id=<target_seed>, x1=plant.bbox.x1, y1=plant.bbox.y1, x2=plant.bbox.x2, y2=plant.bbox.y2)
```
- `target_seed` 从 workflow args 取。默认 `(O)472`（防风草，春季安全选项）。
- 背包空时使用免费模式。count == 0 跳过此步。

### 7. 浇水（条件执行）

```
条件：plant.count > 0 且步骤 6 执行了播种
工具：npc_water_crops(x1=plant.bbox.x1, y1=plant.bbox.y1, x2=plant.bbox.x2, y2=plant.bbox.y2)
```
- 新种下的种子必须浇水。如果没播种（plant.count == 0），跳过。

### 8. 存箱（强制执行）

```
npc_deposit_items(auto_find=true)
```

### 9. 收尾气泡（强制执行）

```
npc_show_text_bubble(text="[开垦完成]")
```
- 根据实际做了什么调整文案，如 "[地翻好了，种子也种下去了~]"。

---

## 禁止事项

- **绝对不调 `npc_water_crops`（除步骤 7 外）**——不要浇已有作物
- **绝对不调 `npc_harvest_crops`**——这是收获轮的事
- **绝对不调 `npc_break_resource`**——这是清理/采集轮的事
- **绝对不调 `chat_say`**——玩家没跟你说话

## 参数说明

| 参数 | 来源 | 默认值 | 说明 |
|------|------|--------|------|
| `inspect_radius` | workflow args | 30 | 巡视半径 |
| `target_seed` | workflow args | `(O)472` | 要种的种子 QID |
| `fertilizer_id` | workflow args | `(O)368` | 肥料 QID，背包空则跳过 |

## 季节种子

| 季节 | 默认种子 |
|------|----------|
| 春 | `(O)472` 防风草 / `(O)474` 花椰菜 / `(O)475` 土豆 |
| 夏 | `(O)485` 红甘蓝 / `(O)487` 玉米 / `(O)491` 甜瓜 |
| 秋 | `(O)490` 南瓜 / `(O)493` 蔓越莓种子 |
| 冬 | 停止——不执行此 workflow |

## 性格影响

| 特质 | 影响 |
|------|------|
| 勤奋 | 更大半径（+5），一定施肥 |
| 随性 | 更小半径（-3），可能跳过施肥 |
| 有条理 | 顺序不可变，一定 re-inspect |

性格不改变序列顺序，只影响半径大小和是否跳过施肥。
