---
name: smartnpc-farm-evening-close
description: 傍晚收尾——最后一轮收割成熟作物、补种空地、存箱入库。{{NPC_NAME}} 为当天农场工作画上句号。
version: 0.1.0
author: SmartNPC Project
license: MIT
metadata:
  npc: {{NPC_NAME}}
  hermes:
    tags: [SmartNPC, farm, evening, workflow]
---

# 傍晚收尾 — {{NPC_NAME}}

## 目标

傍晚前的最后一轮：收割所有成熟作物 → 入库 → 补种空地。这是**收尾轮**——不翻新地、不清杂物、不浇水。

## 固定工具序列（严格按顺序，不可重排）

### 1. 观察环境

```
npc_inspect_object(what="farm_actions", radius=<inspect_radius>)
```
- `inspect_radius` 从 workflow args 取，默认 30。
- 关注 `harvest` 和 `plant` 桶。

### 2. 收割成熟作物（条件执行）

```
条件：harvest.count > 0
工具：npc_harvest_crops(x1=harvest.bbox.x1, y1=harvest.bbox.y1, x2=harvest.bbox.x2, y2=harvest.bbox.y2)
```
- 收所有成熟作物。count == 0 跳过此步。

### 3. 入库（条件执行）

```
条件：步骤 2 执行了 harvest
工具：npc_deposit_items(auto_find=true)
```
- 收获的作物立即存箱。如果没收获，跳过。

### 4. 补种空地（条件执行）

```
条件：plant.count > 0
工具：npc_plant_seeds(seed_id=<target_seed>, x1=plant.bbox.x1, y1=plant.bbox.y1, x2=plant.bbox.x2, y2=plant.bbox.y2)
```
- `target_seed` 从 workflow args 取。默认 `(O)472`（防风草）。
- 背包空时使用免费模式。count == 0 跳过此步。

### 5. 存箱（强制执行）

```
npc_deposit_items(auto_find=true)
```

### 6. 收尾（强制执行，二选一）

```
npc_show_text_bubble(text="[今天干得不错，收工了]")
或
npc_express_emotion(emotion="happy")
```
- 勤奋/有成就感的 NPC 用 emotion happy，其他用 bubble。

---

## 禁止事项

- **绝对不调 `npc_water_crops`**——新种子明天浇
- **绝对不调 `npc_till_soil`**——这是开垦轮的事
- **绝对不调 `npc_clear_debris`**——这是清理轮的事
- **绝对不调 `npc_break_resource`**——这是采集轮的事
- **绝对不调 `chat_say`**——玩家没跟你说话

## 参数说明

| 参数 | 来源 | 默认值 | 说明 |
|------|------|--------|------|
| `inspect_radius` | workflow args | 30 | 巡视半径 |
| `target_seed` | workflow args | `(O)472` | 补种种子 QID |

## 性格影响

| 特质 | 影响 |
|------|------|
| 勤奋 | 一定补种（不空着地过夜） |
| 随性 | 可能跳过补种（"明天再说"） |
| 有条理 | harvest → deposit → plant 顺序不可变 |

性格不改变序列顺序，只影响是否跳过补种和收尾用 bubble 还是 emotion。
