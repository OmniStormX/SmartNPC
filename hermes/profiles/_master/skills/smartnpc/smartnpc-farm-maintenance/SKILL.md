---
name: smartnpc-farm-maintenance
description: Farm maintenance workflow — example-driven. {{NPC_NAME}} observes the land and dynamically composes a sequence of maintenance actions (clear debris, till, plant, water, fertilize) by matching the situation to provided examples.
version: 0.1.0
author: SmartNPC Project
license: MIT
metadata:
  npc: {{NPC_NAME}}
  hermes:
    tags: [SmartNPC, farm, maintenance, workflow]
---

# 农场维护 — {{NPC_NAME}}

你不是在执行固定脚本。此技能是**示例驱动**的：观察土地，将当前情况匹配到下方最接近的示例，然后**动态编排**适合你所见的动作序列。每轮维护都可能不同——根据天气、季节、背包、土壤状态和你自身性格进行调整。

触发条件：
- 由工作流引擎通过 workflow YAML（如 `farm_care`、`farm_extension`）中的 `kind: skill_call` 步骤调用。引擎将 `inspect_radius` 等工作流输入作为上下文传入。

---

## 工具箱

这些是构建块。你自行选择使用哪些以及使用顺序（受限于下方的硬性依赖）。

| 工具 | 功能 | 前置条件 |
|------|-------------|--------------|
| `npc_clear_debris` | 清除地面的杂草、树枝、石头 | 边界框内存在物体 |
| `npc_till_soil` | 将空地变为可耕种土壤 | 未耕种的空地砖格，可挖掘，非冬季 |
| `npc_water_crops` | 浇灌干燥的作物 / 干燥的已耕土壤 | 边界框内存在干燥的 HoeDirt |
| `npc_fertilize` | 对已耕土壤施肥。| 空置已耕土壤（dirt.crop == null），未施过肥。**播种前调用——品质肥必须在种子发芽前施加。** |
| `npc_inspect_object` | 调查周围的土地状态 | —（始终可用） |
| `npc_show_text_bubble` | 显示一句符合角色性格的简短想法 | —（始终可用） |

> **关键 — 耕种和种植是一等动作，不是可选的后续操作。**
>
> 当 inspect 的 `farm_actions.till.count > 0` 时，必须执行 `npc_till_soil`。
> 当 `farm_actions.plant.count > 0` 时，必须执行 `npc_plant_seeds`（如果没有种子，免费模式会生效——该砖格仍然会变成已种植的作物）。
> 仅仅因为"你没有种子"或"这是一轮快速维护"就跳过耕种/种植，是最常见的失败模式，会导致农场永远停滞。
> 不知道选什么季节合适的种子时，默认种植 `(O)472`（防风草）。

## 硬性依赖

这些是物理约束。不要违反——游戏会拒绝动作或产生无意义的结果。

```
clear_debris ──→ till_soil       （无法在杂草/石头中耕种）
till_soil    ──→ fertilize       （品质肥必须在播种前施加）
fertilize    ──→ plant_seeds     （施肥后播种）
till_soil    ──→ plant_seeds     （不施肥时直接播种）
plant_seeds  ──→ water_crops     （新种下的种子应该浇水）
water_crops  可随时对干燥砖格进行
```

不确定时：**清除 → 耕种 → 施肥 → 种植 → 浇水** 是完整链条。
当 inspect 显示这些类别计数不为零时，执行完整链条；只有当某类计数确实为 0 时才跳过该环节。

---

## 示例工作流

研究这些示例来理解编排动作的模式。将你的当前情况匹配到最接近的示例，然后进行调整。

所有示例的参数均来自 `npc_inspect_object(what="farm_actions")` 的输出，它提供 7 个桶（`harvest/water/clear/till/forage/plant/break`），每个桶都有一个 `count` 和一个 `bbox`。将 bbox 直接传给对应行为工具的 `x1/y1/x2/y2`——该矩形就是工作区域。

### 示例 A — 开垦新地

**情况：** Inspect 显示 `till.count > 0`（大量可挖掘空地），可能还有 `clear.count > 0`（上方有杂物）。尚无已耕土壤，或很少。你想把这片区域变成农田。

**典型序列：**
```
npc_inspect_object(radius=15, what="farm_actions")
  → 查看 till.count 高，clear.count 可能有，plant.count 低

npc_clear_debris(x1,y1,x2,y2 = clear.bbox)
  → 清除农田及周边区域内的杂物（杂草、树枝、石头、树桩、树苗）
  → 使用 clear.bbox —— 它已裁剪到农田范围，避免清到远处无关区域。
  → 如果 clear.count == 0 但 till.count > 0，跳过此步（TickTillSoil 内部逐格清理）。

npc_till_soil(x1,y1,x2,y2 = till.bbox)
  → 翻耕待开垦区域。**内部 TickTillSoil 会在翻耕前逐格清理杂物、树桩、成熟树。**
  → 不要在 till_soil 前用 till.bbox 调 clear_debris —— till.bbox 跨度过大。
  → 如果返回 nothing_to_do=true，用更小的 patch_w/patch_h 或更大的 radius 重试。

npc_fertilize(fertilizer_id = 当季肥料, x1,y1,x2,y2 = till.bbox)
  → **在播种前施肥。** 品质肥料必须在种子发芽前施加——翻耕后立即施肥是最稳妥的做法。
  → 常用肥料：(O)368 基础肥料 / (O)369 优质肥料 / (O)465 生长激素 / (O)370 保湿土
  → 没有肥料时跳过此步（免费模式不适用施肥）。

npc_plant_seeds(seed_id = 当季种子, x1,y1,x2,y2 = till.bbox 或 plant.bbox)
  → 施肥后播种。背包为空则使用免费种植模式。

npc_water_crops(x1,y1,x2,y2 = plant.bbox)
  → 浇灌新种子（覆盖你刚刚种植的砖格）
```

**关键决策：**
- **先施肥再播种。** 品质肥料（基础/优质/豪华）必须在种子发芽前施加。翻耕后立即施肥是最稳妥的做法。
- 常用 `fertilizer_id`：(O)368 基础肥料 / (O)369 优质肥料 / (O)465 生长激素 / (O)370 保湿土
- 没有肥料时跳过 npc_fertilize——不要用免费模式（施肥不支持免费模式，会报错）。
- 选择匹配季节的 `seed_id`：
  - 春：`(O)472` 防风草 / `(O)474` 花椰菜 / `(O)475` 土豆
  - 夏：`(O)485` 红甘蓝 / `(O)487` 玉米 / `(O)491` 甜瓜
  - 秋：`(O)490` 南瓜 / `(O)493` 蔓越莓种子 / `(O)499` 上古种子
  - 冬：停止——不要执行此示例
- 即使背包为空也要执行 plant_seeds。`(O)472` 是安全的回退选项。
- 翻耕后，该区域是新鲜的空置 HoeDirt → 不需要重新 inspect；将同一个 bbox 传给 plant_seeds 和 water_crops。

---

### 示例 B — 日常养护

**情况：** Inspect 显示 `harvest.count` 不多或为零，`water.count > 0`，`clear.count > 0` 少量，可能 `plant.count > 0`（一些砖格仍空置）。农场已成型——保持运转即可。

**典型序列：**
```
npc_inspect_object(radius=12, what="farm_actions")

[如果 water.count > 0]
  npc_water_crops(x1,y1,x2,y2 = water.bbox)
  → 浇灌干燥作物

[如果 clear.count > 0]
  npc_clear_debris(x1,y1,x2,y2 = clear.bbox)
  → 清除新冒出的杂草

[如果 plant.count > 0]   ← 即使是"轻量轮次"也不要跳过此步
  npc_plant_seeds(seed_id = 当季种子, x1,y1,x2,y2 = plant.bbox)
  → 填充空置的已耕土壤，避免农场荒废

[如果 plant.count > 0 且你不担心肥料问题]
  npc_fertilize(fertilizer_id="(O)368", x1,y1,x2,y2 = plant.bbox)
  → 对你刚刚种植的空置已耕砖格进行定点施肥
```

**关键决策：**
- 浇水是第一优先级——干燥作物会枯萎。
- 当 `plant.count > 0` 时不要跳过 plant_seeds。"这是一轮轻量维护"是把荒置砖格留到明天的错误理由。
- 这是最常见的轮次形态；在没有大规模收获的日子里，大多数时候以此为目标。

---

### 示例 C — 补种轮作

**情况：** 作物刚刚被收获（由你或他人完成）。Inspect 显示 `plant.count` 高，`harvest.count` ≈ 0，可能有一些 `clear.count`。你正在重新填充空置的已耕土壤。

**典型序列：**
```
npc_inspect_object(radius=12, what="farm_actions")
  → 确认：plant.count 高，harvest.count ≈ 0

[如果 clear.count > 0]
  npc_clear_debris(x1,y1,x2,y2 = clear.bbox)
  → 先清理

npc_plant_seeds(seed_id = 当季种子,
                x1,y1,x2,y2 = plant.bbox)
  → 在所有可用的空置已耕土壤上种植。无种子则使用免费模式。

npc_water_crops(x1,y1,x2,y2 = plant.bbox)
  → 浇灌新种下的种子

[如果你有肥料或想使用免费施肥]
  npc_fertilize(fertilizer_id="(O)368", x1,y1,x2,y2 = plant.bbox)
  → 对新种植区域施肥（背包为空则使用免费模式）
```

**关键决策：**
- 此示例必须执行 plant_seeds——这是本轮的全部意义。
- 先种植再浇水（新种子需要水）。
- 空背包不会阻塞此轮；plant_seeds 以免费模式运行，土壤仍然会变成已种植的作物。

---

### 示例 D — 全量整备

**情况：** 新季节刚开始（第 1-2 天），或农场被忽视了一段时间。Inspect 显示多个桶计数不为零：`clear.count` 高，`till.count` 高，`plant.count` 高。

**典型序列：**
```
npc_inspect_object(radius=20, what="farm_actions")
  → 广域扫描——查看全部 7 个桶

npc_clear_debris(x1,y1,x2,y2 = clear.bbox)
  → 全面清除农田杂草

npc_till_soil(x1,y1,x2,y2 = till.bbox)
  → 翻耕所有空置未耕种的可挖掘砖格

npc_plant_seeds(seed_id = 当季种子,
                x1,y1,x2,y2 = unionBBox(till.bbox, plant.bbox))
  → 在新耕区域 + 已有空置 HoeDirt 上种植

npc_water_crops(x1,y1,x2,y2 = unionBBox(till.bbox, plant.bbox))
  → 浇灌所有刚种下的作物

npc_fertilize(fertilizer_id="(O)368",
              x1,y1,x2,y2 = unionBBox(till.bbox, plant.bbox))
  → 对新种植区施肥（免费模式也可以）
```

**关键决策：**
- 使用 `radius=20` 或更大的 inspect——这是个大工程。
- 仅当多个桶的 farm_actions 计数同时很高时才触发此示例。
- 冬季：不要使用此示例（只有 clear_debris 有效）。
- "unionBBox" 的意思是：选取能覆盖两个 bbox 的最小矩形。大多数情况下 till.bbox 和 plant.bbox 大量重叠。

---

### 示例 E — 轻量路过

**情况：** 你正经过农场附近的区域，并非在专门的维护触发下。做个一件小事就够了。

**典型序列：**
```
npc_inspect_object(radius=8, what="farm_actions")
  → 快速扫描

恰好选择以下之一：
  [如果 water.count >= 1]  npc_water_crops(x1,y1,x2,y2 = water.bbox)
  [如果 clear.count >= 1]  npc_clear_debris(x1,y1,x2,y2 = clear.bbox)
  [如果 plant.count >= 1]  npc_plant_seeds(seed_id="(O)472",
                                           x1,y1,x2,y2 = plant.bbox)

npc_show_text_bubble "顺手弄了一下~"
```

**关键决策：**
- 最多一个动作（不计算 inspect + bubble）。
- bbox 已经限制了区域——不需要额外的 max_count。
- 如果三个计数全部为 0：完全跳过，不要强求。
- 这是机会性的，不是计划内的。

---

## 决策流程（严格按顺序执行，不可跳过）

### 1. 观察
调用 `npc_inspect_object(radius=12, what="farm_actions")`。读取结果。
响应会给出 7 个桶（每个都有 `count` + `bbox`）：

- `till` — 可变为农田的空置可挖掘地面（**最高优先级！**）
- `clear` — 农田上/附近的杂物
- `plant` — 等待种子的空置已耕土壤
- `water` — 需要浇水的干燥作物（**已有作物才浇！**）
- `harvest` — 成熟作物（交给 `smartnpc-farm-harvest` 处理）
- `forage` — 已生成的采集物（单独处理，不在本技能中）
- `break` — 树木 / 大石头（单独处理）

### 2. ⚠️ 硬性决策门（必须先过这关）

看完 inspect 结果后，按以下顺序判断——**匹配到第一条就停，不要往下看**：

| 优先级 | 条件 | 你必须做的事 | 你禁止做的事 |
|--------|------|-------------|-------------|
| **P0** | `till.count > 0` | 执行示例 A（开垦新地）完整链路：clear → till → re-inspect → plant → water → fertilize | **禁止**调用 npc_water_crops（无作物可浇）、**禁止**调用 npc_break_resource（不是采集轮）、**禁止**调用 npc_harvest_crops |
| P1 | `harvest.count >= 3` | 切换到 `smartnpc-farm-harvest` skill |
| P2 | `water.count > 0` **且** `till.count == 0` | 执行示例 B（日常养护）：water → clear → plant（如有空地） | 禁止 break_resource，这不是采集轮 |
| P3 | `plant.count > 0` **且** `till.count == 0` | 执行示例 C（补种轮作）：plant → water | |
| P4 | 各项计数都很低（< 3）| 执行示例 E（轻量路过）：最多 1 个动作 | |
| P5 | 所有计数为 0 | 气泡 "[今天没什么要弄的]"，写入记忆，停止 | |

**⚠️ 核心原则：till.count > 0 时，你只做开垦。浇水、砍树、采集全部禁止。till→plant→water 中的 water 是浇你刚种下的种子，不是浇已有的作物。**

### 3. 匹配示例
按 P0-P5 找到唯一匹配项后，严格按该示例的步骤顺序执行。

### 4. 执行
每次调用一个工具。每个行为工具从 inspect 的对应桶获取 bbox——无需逐砖格坐标，bbox 调用不需要 max_count。在同一轮中不需要在关联步骤之间重新 inspect（但 till 之后必须 re-inspect 获取更新后的 plant.bbox）。

### 5. 收尾
- 一句 `npc_show_text_bubble`，概括你实际做了什么
- 写入记忆：`farm_maintenance: last_date=<季节><日> actions=<摘要>`
- 停止。不要调用 `chat_say`。

---

## 性格影响

你的 SOUL.md 定义了你是谁。让它塑造你的维护风格：

| 特质 | 影响 |
|---|---|
| 勤奋 / 努力 | 更大半径（+3-5），每轮更多动作，偏好示例 A/D |
| 随意 / 悠闲 | 更小半径（-3），更少动作，偏好示例 B/E |
| 有条不紊 / 爱整洁 | 总是先清除杂物再干别的，偏好施肥 |
| 大大咧咧 / 不拘小节 | 可能跳过杂物清理，专注于种植 |
| 话多 | 每个主要动作后弹出气泡 |
| 安静 | 只在收尾时弹出气泡 |
| 有养育心 | 将浇水置于一切之上 |
| 务实 | 优先做投入产出比最高的事 |

这些是指导原则，不是规则。让你的角色自然地影响决策——勤奋的 NPC 就是做得多，随意的 NPC 就是做得少。

---

## 护栏

- **每轮最多 6 次工具调用**（inspect + 最多 4 个动作 + bubble）。
- **下雨/暴风雨 → 立即停止。** 写入 `farm_maintenance: skipped rain`。
- **冬季 → 只有 clear_debris 有效。** 耕种/种植/浇水/施肥不可用。
- **仅在农场类地图上操作**（Farm、FarmHouse、FarmCave 等）。如果你在城镇道路上或在森林中，最多限于示例 E。
- **不要碰正在生长的作物。** 此技能用于土壤维护——收获属于 `smartnpc-farm-harvest`。
- **不要调用 `chat_say`** — 玩家没跟你说话。
- **不要调用 `npc_plan_day`** — 那是日程技能的工作。
- 如果观察后没有可用的工具，立即用简短气泡和记忆写入收尾。不要强行动作。
- 如果你今天已经完成了 2 轮维护，偏好示例 E（轻量）。

---

## 观察触发

当工作流引擎以机会性模式（如路过顺手做）调用本技能时：

1. 调用 `npc_inspect_object(radius=10, what="farm_actions")`。
2. 判断：有什么值得做的吗？
   - **`till.count`、`plant.count`、`clear.count`、`water.count` 任一 ≥ 3**
     → 按上方完整决策流程继续。
   - **所有计数在 1-2 范围** → 只用示例 E。
   - **所有计数为 0** → 完全跳过。写入
     `opportunistic_work: <日期> 无事可做`。不需要气泡。
3. 决定权在你——这正是你的性格发挥作用的地方。勤奋的 NPC 更常答应；懒惰的 NPC 会拒绝。不需要掷骰子——你就是这个角色。

概率推理存在于你的 SOUL 中，不在代码里。如果当下不适合你的角色，放心跳过。
