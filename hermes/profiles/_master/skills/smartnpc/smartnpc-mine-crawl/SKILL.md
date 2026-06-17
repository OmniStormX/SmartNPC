---
name: smartnpc-mine-crawl
description: 矿洞探索——观察周围资源，破除障碍，捡拾采集物，存箱入库。{{NPC_NAME}} 在矿洞中进行一轮探索。
version: 0.1.0
author: SmartNPC Project
license: MIT
metadata:
  npc: {{NPC_NAME}}
  hermes:
    tags: [SmartNPC, mine, exploration, workflow]
---

# 矿洞探索 — {{NPC_NAME}}

## 目标

在矿洞中进行一轮探索：观察周围可破坏资源和采集物 → 破资源 → 捡采集物 → 存箱。这是**单轮探索**——不深入多层。

## 固定工具序列（严格按顺序，不可重排）

### 1. 观察环境

```
npc_inspect_object(what="resources", radius=<inspect_radius>)
```
- `inspect_radius` 从 workflow args 取，默认 8。
- 获得 2 个桶：`break` / `forage`，每个有 `count` + `bbox`。

### 2. 破资源（条件执行）

```
条件：break.count > 0
工具：npc_break_resource(what="all", x1=break.bbox.x1, y1=break.bbox.y1, x2=break.bbox.x2, y2=break.bbox.y2)
```
- 先破后捡——破资源可能掉落新采集物。count == 0 跳过此步。

### 3. 捡采集物（条件执行）

```
条件：forage.count > 0
工具：npc_forage_collect(x1=forage.bbox.x1, y1=forage.bbox.y1, x2=forage.bbox.x2, y2=forage.bbox.y2)
```
- 捡地面物品。count == 0 跳过此步。

### 4. 存箱（强制执行）

```
npc_deposit_items(auto_find=true)
```

### 5. 收尾气泡（强制执行）

```
npc_show_text_bubble(text="[矿洞收获不错~]")
```
- 根据实际收获调整文案。

---

## 禁止事项

- **绝对不调任何 farm 工具**（water/till/plant/harvest/clear/fertilize/fill_gaps）
- **绝对不调 `npc_approach_and_speak`**——社交是另一轮的事
- **绝对不调 `chat_say`**——玩家没跟你说话
- **不要反复 inspect**——一次够了

## 参数说明

| 参数 | 来源 | 默认值 | 说明 |
|------|------|--------|------|
| `inspect_radius` | workflow args | 8 | 探测半径 |

## 性格影响

| 特质 | 影响 |
|------|------|
| 勇敢/勤奋 | 更大半径（+3），两个动作都做 |
| 谨慎/胆小 | 更小半径（-3），可能只做 forage_collect |

性格不改变序列顺序，只影响半径大小和是否跳过 break_resource。
