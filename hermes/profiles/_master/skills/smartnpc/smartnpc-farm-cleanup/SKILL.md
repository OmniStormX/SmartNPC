---
name: smartnpc-farm-cleanup
description: 农场周边清理——清除杂物、砍树碎石、捡采集物。{{NPC_NAME}} 打扫农场及周边区域。
version: 0.1.0
author: SmartNPC Project
license: MIT
metadata:
  npc: {{NPC_NAME}}
  hermes:
    tags: [SmartNPC, farm, cleanup, workflow]
---

# 农场清理 — {{NPC_NAME}}

## 目标

清除农场及周边的杂物（杂草、树枝、石头），砍树砸石，捡采集物。这是**清理轮**——不做任何耕种操作。

## 固定工具序列（严格按顺序，不可重排）

### 1. 观察环境

```
npc_inspect_object(what="farm_actions", radius=<inspect_radius>)
```
- `inspect_radius` 从 workflow args 取，默认 30。
- 获得 3 个桶：`clear` / `break` / `forage`，每个有 `count` + `bbox`。

### 2. 清杂物（条件执行）

```
条件：clear.count > 0
工具：npc_clear_debris(x1=clear.bbox.x1, y1=clear.bbox.y1, x2=clear.bbox.x2, y2=clear.bbox.y2)
```
- 清除杂草、树枝、小石头。count == 0 跳过此步。

### 3. 砍树砸石（条件执行）

```
条件：break.count > 0
工具：npc_break_resource(what="all", x1=break.bbox.x1, y1=break.bbox.y1, x2=break.bbox.x2, y2=break.bbox.y2)
```
- 破坏树木、树桩、大石头。`what="all"` 覆盖所有类型。count == 0 跳过此步。

### 4. 捡采集物（条件执行）

```
条件：forage.count > 0
工具：npc_forage_collect(x1=forage.bbox.x1, y1=forage.bbox.y1, x2=forage.bbox.x2, y2=forage.bbox.y2)
```
- 捡地面采集物，先清后捡（清理可能暴露出新采集物）。count == 0 跳过此步。

### 5. 存箱（强制执行）

```
npc_deposit_items(auto_find=true)
```

### 6. 收尾气泡（强制执行）

```
npc_show_text_bubble(text="[清理干净了]")
```
- 根据实际做了什么调整文案。

---

## 禁止事项

- **绝对不调 `npc_water_crops`**——这是养护轮的事
- **绝对不调 `npc_till_soil`**——这是开垦轮的事
- **绝对不调 `npc_plant_seeds`**——这是养护/开垦轮的事
- **绝对不调 `npc_harvest_crops`**——这是收获轮的事
- **绝对不调 `chat_say`**——玩家没跟你说话

## 参数说明

| 参数 | 来源 | 默认值 | 说明 |
|------|------|--------|------|
| `inspect_radius` | workflow args | 30 | 巡视半径 |

## 性格影响

| 特质 | 影响 |
|------|------|
| 勤奋 | 更大半径（+5），三个动作全做 |
| 随性 | 更小半径（-3），可能跳过 break_resource |
| 有条理 | 一定先 clear 再 break，次序不可乱 |

性格不改变序列顺序，只影响半径大小和是否跳过 break_resource。
