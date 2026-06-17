---
name: smartnpc-farm-harvest
description: Farm harvest workflow — example-driven. {{NPC_NAME}} observes mature crops and dynamically composes a harvest→deposit→replant sequence by matching the situation to provided examples.
version: 0.1.0
author: SmartNPC Project
license: MIT
metadata:
  npc: {{NPC_NAME}}
  hermes:
    tags: [SmartNPC, farm, harvest, workflow]
---

# 农场收获 — {{NPC_NAME}}

你不是在执行固定脚本。本技能是**示例驱动**的：观察成熟作物，将当前情况匹配到下方最接近的示例，然后**动态组合**收获 → 存入 →（可选补种）序列。每轮收获都可能不同。

触发条件：
- 由工作流引擎通过 workflow YAML（如 `farm_care`、`farm_extension`）中的 `kind: skill_call` 步骤调用。引擎将 `inspect_radius` 等工作流输入作为上下文传入。
- 若维护类工作流执行后发现意外成熟作物，可切换或混合本技能

---

## 工具箱

| 工具 | 作用 | 前置条件 |
|------|-------------|--------------|
| `npc_inspect_object` | 查看周围作物 | —（始终可用） |
| `npc_harvest_crops` | 收获成熟作物，物品放入 NPC 背包 | 半径内有成熟作物 |
| `npc_deposit_items` | 将背包物品存入最近的箱子 | 背包中有物品，存在箱子 |
| `npc_deliver_items` | 将背包物品交给玩家 | 背包中有物品，玩家在附近且可交互 |
| `npc_plant_seeds` | 在新空出的耕地上补种 | 存在空耕地。**不需要种子** — 免种子模式即使背包为空也能运行。 |
| `npc_water_crops` | 给补种的种子浇水 | 补种后有干燥地块 |
| `npc_show_text_bubble` | 显示一句符合人设的简短内心话 | —（始终可用） |

## 硬性依赖

```
harvest_crops ──→ deposit_items    （背包满 → 必须存放）
harvest_crops ──→ deliver_items    （存入的替代方案，交给玩家）
harvest_crops ──→ plant_seeds      （推荐：收获后在空出的耕地上补种 — 免种子模式意味着不需要种子）
plant_seeds   ──→ water_crops      （新种子需要浇水）
```

收获 + 存入算作一次逻辑操作（它们是配对的）。收获之后，你必须立即存入或交付。不要把物品留在背包里——下次收获时可能会丢失。

---

## 示例工作流

### 示例 A — 大丰收

**场景：** 许多成熟作物（6+）。农场丰收满满。你专门来收获。这是重头戏。

**典型序列：**
```
npc_inspect_object(radius=12, what="crops")
  → 确认：8 个成熟作物分布在区域内

npc_harvest_crops(radius=12, max_count=8)
  → 收获所有成熟的

npc_deposit_items(auto_find=true)
  → 将背包清空到最近的箱子

[如果收获过程中发现更多成熟作物] npc_harvest_crops(radius=12, max_count=5)
  → 第二轮，收割遗漏的

npc_deposit_items(auto_find=true)
  → 存入第二批

[如果有种子] npc_plant_seeds(radius=12, max_count=min(empty_tilled, seeds))
  → 在已收获的地块上补种

[如果补种了] npc_water_crops(radius=12, max_count=10)
  → 给新种子浇水
```

**关键决策：**
- 如果区域较大（8+ 作物分布广），分 2 轮收获
- 每次收获之间务必存入（背包可能会满）
- 仅当你有当季合适的种子时才补种
- 如果一次装不下所有收获：收获→存入→收获→存入

---

### 示例 B — 少量收获

**场景：** 只有少量成熟作物（1-4 个）。速战速决。

**典型序列：**
```
npc_inspect_object(radius=8, what="crops")
  → 确认：3 个成熟作物

npc_harvest_crops(radius=8, max_count=4)
  → 收获那几个成熟的

npc_deposit_items(auto_find=true)
  → 存放起来

[如果玩家在附近且可交互] npc_deliver_items
  → 或者交给玩家而不是放箱子

[如果有种子] npc_plant_seeds(radius=8, max_count=3)
  → 快速补种
```

**关键决策：**
- 一次收获调用就够了
- 如果玩家在附近，考虑交付给玩家——更有人情味
- 地块很少，补种可选

---

### 示例 C — 路过顺手收

**场景：** 你不是来做农活的，但注意到有几株成熟作物。路过顺手收一下。

**典型序列：**
```
npc_inspect_object(radius=6, what="crops")
  → 确认：2 个成熟作物就在附近

npc_harvest_crops(radius=6, max_count=2)
  → 收获够得着的

npc_deposit_items(auto_find=true)
  → 立即存入

npc_show_text_bubble "顺手收了一下~"
```

**关键决策：**
- 小半径——只收触手可及的
- 不补种（这是顺手收的，不是计划中的）
- 不用特意去找箱子——用 auto_find
- 如果什么都没有成熟：直接跳过

---

### 示例 D — 收+补循环

**场景：** 一轮作物周期结束。大量成熟作物，你想立即在同一区域补种以保持生产力。

**典型序列：**
```
npc_inspect_object(radius=12, what="crops")
  → 确认：大量成熟作物，充足耕地

npc_harvest_crops(radius=12, max_count=10)
  → 收获成熟作物

npc_deposit_items(auto_find=true)
  → 存放收获物

npc_plant_seeds(radius=12, max_count=10)
  → 在新空出的耕地上补种

npc_water_crops(radius=12, max_count=12)
  → 给所有新种植的浇水

[如果有肥料] npc_fertilize(radius=10, max_count=8)
  → 施肥以提高产量
```

**关键决策：**
- 这是完整循环——收获 → 存入 → 种植 → 浇水 → 施肥
- 仅当你种子充足且季节不会太晚时执行
- 季节最后 3 天：不要补种（作物来不及成熟）
- 如果不确定，用 `game_get_time` 查看季节/日期

---

## 决策流程

### 1. 观察
调用 `npc_inspect_object(radius=10, what="crops")`。读取结果。关键数据：
- `mature_crops[]` — 数量、位置、作物类型
- `empty_hoedirt` — 数量（用于潜在的补种）
- `growing_crops[]` — 有没有快成熟的？（记下来下次用）

不要输出原始 JSON。做总结。

### 2. 匹配
哪个示例最匹配？

| 观察到的情况 | 最接近的示例 |
|---|---|
| 6+ 成熟作物，专门的收获时段 | A（大丰收） |
| 1-4 成熟作物，专门的收获时段 | B（少量收获） |
| 少量成熟，路过时 | C（路过顺手收） |
| 大量成熟 + 收获后想补种 | D（收+补循环） |

### 3. 组合
从工具箱中构建你的序列。遵守硬性依赖。
你可以混合示例——例如"像 B 那样少量收获，但我有种子，所以加上 D 的补种。"

### 4. 执行
一次一个工具。等待完成。每次之后：
- 检查：收获找到的作物是否少于预期？（工具数据可能比 inspect 旧）——如果是，跳过剩余的收获步骤
- 存入之后：背包空了吗？好的，继续

### 5. 收尾
- 用一次 `npc_show_text_bubble` 以角色口吻总结收获
  （例如"[收了6个蓝莓！放进箱子了~]"，"[今天收获不错]"）
- 写入记忆：`farm_harvest: last_date=<季节><日> crops=<数量>`
- 停止。不要调用 `chat_say`。

---

## 存入 vs. 交付 — 如何选择

| 条件 | 选择 |
|---|---|
| 玩家在附近（同地图，15 格内）且不忙 | 交付（`npc_deliver_items`） |
| 玩家较远 / 在不同地图 / 在过场动画 / 在睡觉 | 存入（`npc_deposit_items auto_find=true`） |
| 背包有稀有/珍贵物品（上古水果、杨桃等） | 尽可能交付给玩家 |
| 背包有大量普通物品（防风草、小麦等） | 存入箱子 |
| 你想和玩家说话 | 以交付为借口接近 |
| 你害羞或想避开玩家 | 悄悄存入 |

---

## 性格影响

| 性格特质 | 效果 |
|---|---|
| 勤奋/努力 | 更大收获半径，始终补种，偏好示例 D |
| 随性/放松 | 更小半径，只收不种，跳过补种 |
| 慷慨/乐于给予 | 偏好交付而非存入，把全部物品给玩家 |
| 独立自主 | 偏好存入，让玩家自己从箱子取 |
| 有条理 | 始终存入箱子，按类型分类 |
| 话多 | 好收成时兴奋的气泡，注意稀有作物 |
| 安静 | 最简气泡，只报数量 |

---

## 防护规则

- **每轮最多 7 次工具调用**（检查 + 最多 2 轮收获 + 存入 + 可选补种 + 浇水 + 气泡）。
- **每次收获调用后必须存入。** 不要在背包中堆积物品。
- **不要收获未成熟作物** — 只收 inspect 返回的 `mature_crops[]`。
- **季节最后 3 天不要补种** — 如果季节快结束了用 `game_get_time` 确认。
- **下雨/暴风雨 → 收获没问题**（下雨也可以收获），但如果补种了则跳过浇水。
- **冬天 → 不收获**（什么都不长）。直接跳过。
- **不要碰其他 NPC 的作物或箱子**，除非在共享农场上。
- **不要调用 `chat_say`** — 玩家没有对你说话。
- 如果 `npc_harvest_crops` 一无所获（工具数据可能比 inspect 旧），直接收尾——不要重试。

---

## 观察触发

当工作流引擎以机会性模式（如路过顺手收）调用本技能时：

1. 调用 `npc_inspect_object(radius=8, what="crops")`。
2. 判断：
   - **3+ 成熟作物可见** → 按示例 B 或 C 执行。
   - **1-2 成熟作物** → 仅按示例 C 执行（快速抓取）。
   - **零成熟作物** → 静默跳过。写入 `opportunistic_work: <日期> no mature crops`。
3. 你的性格驱动决策。没有随机数——你根据自己的性格做决定。勤奋的 NPC 即使只有 1 个成熟作物也会收；懒散的 NPC 需要 5+ 才肯动。
