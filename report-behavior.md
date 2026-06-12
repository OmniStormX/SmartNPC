# SmartNPC NPC 行为可实现性报告

基于 `docs/rich-npc-behaviors.md`，当前规划的 20 个行为整体**可实现**。实现方式上建议分为两层：

1. **C# SMAPI Mod 侧**：负责实际操作游戏世界，例如寻路、移除物体、浇水、收获、放入箱子、播放动画。
2. **Go MCP / Hermes 侧**：将每个行为暴露为 MCP tool，由 NPC 的 LLM 人格决定何时调用。

核心实现模式是：

```text
Hermes LLM 决策
  → 调用 MCP tool
  → smartnpc-mcp 转发 ws action
  → SMAPI Mod 在游戏主线程执行行为协程
```

---

## 一、总体可实现性结论

| 类型 | 数量 | 可实现性 | 说明 |
|---|---:|---|---|
| 世界交互类 | 12 个 | 高 | 基于 `GameLocation.Objects`、`terrainFeatures`、`Chest`、`FarmAnimal` 等 API |
| 社交表演类 | 8 个 | 很高 | 基于已有 NPC 移动、动画帧、表情气泡、tick 状态机 |
| 总计 | 20 个 | 高 | 不需要新美术，主要工作量在 C# 行为协程与 Go tool 注册 |

需要注意的是，**世界交互类行为会真实改变游戏状态**，例如清理杂物、浇水、收获、放入箱子等，因此实现时要控制范围、数量、失败回滚和防止误操作。

---

## 二、世界交互类行为

### 1. `npc_wander` — 自主漫步
> 已完成

| 项目 | 内容 |
|---|---|
| 可实现性 | 高 |
| 实现方式 | 随机选择当前地图可通行 tile，使用 `PathFindController` 寻路；到达后停留数秒，再选择下一个点 |
| 主要 API | `PathFindController`、`GameLocation.isTilePassable()`、`NPC.faceDirection()` |
| 难度 | ★★★☆☆ |
| 预期效果 | NPC 能在小镇、农场或指定地图里自然闲逛，增强“活着”的感觉 |

这是最适合作为基础行为的能力。实现后很多行为都可以复用“寻路到目标点”的逻辑。

---

### 2. `npc_clear_debris` — 清理农场杂物
> 已完成
| 项目 | 内容 |
|---|---|
| 可实现性 | 高 |
| 实现方式 | 扫描 `location.Objects`，筛选杂草、树枝、石头；NPC 走到目标旁，播放锄地动画，然后移除物体 |
| 主要 API | `location.Objects.Pairs`、`obj.IsWeeds()`、`obj.IsTwig()`、`obj.IsBreakableStone()`、`location.Objects.Remove(tile)` |
| 难度 | ★★★★☆ |
| 预期效果 | NPC 真的能帮玩家清理农场障碍物 |

这是非常有价值的行为，但要限制 `radius`、`max_count`，避免 NPC 一次清掉整个农场。

---

### 3. `npc_water_crops` — 浇灌作物

| 项目 | 内容 |
|---|---|
| 可实现性 | 高 |
| 实现方式 | 遍历 `terrainFeatures`，找到有作物但未浇水的 `HoeDirt`；NPC 走到旁边播放浇水动画，然后设置为已浇水 |
| 主要 API | `HoeDirt`、`dirt.crop`、`dirt.state.Value = 1`、`Sprite.Animate()` |
| 难度 | ★★★★☆ |
| 预期效果 | NPC 可以实际帮玩家浇水，玩家能看到地块变湿 |

这是最适合“农场助手型 NPC”的核心行为之一。

---

### 4. `npc_harvest_crops` — 收获成熟作物

| 项目 | 内容 |
|---|---|
| 可实现性 | 中高 |
| 实现方式 | 找到成熟作物，NPC 走到旁边，播放采摘/锄地动画，调用作物收获逻辑，将产物暂存到 NPC 内部背包 |
| 主要 API | `HoeDirt.crop`、`crop.harvest(...)`、`NpcInventory` |
| 难度 | ★★★★☆ |
| 预期效果 | NPC 可以收获成熟作物，并后续交给玩家或放进箱子 |

这个行为实现价值高，但需要仔细验证 `crop.harvest()` 的调用方式，避免重复产出或绕过原版机制。

---

### 5. `npc_deposit_items` — 将物品放入箱子
> 已完成
| 项目 | 内容 |
|---|---|
| 可实现性 | 高 |
| 实现方式 | NPC 携带内部背包中的物品，寻路到指定箱子旁，然后逐个调用箱子放入逻辑 |
| 主要 API | `location.Objects[tile]`、`Chest`、`chest.addItem(item)` |
| 难度 | ★★★☆☆ |
| 预期效果 | NPC 能把收集/收获的物品放进玩家指定箱子 |

这是物品流转链路里的关键行为，建议和 `npc_harvest_crops`、`npc_forage_collect` 配套实现。

---

### 6. `npc_deliver_items` — 将物品交给玩家
> 已完成
| 项目 | 内容 |
|---|---| 
| 可实现性 | 高 |
| 实现方式 | NPC 寻路到玩家附近，播放捧物动画，然后把内部背包物品逐个放入玩家背包 |
| 主要 API | `PathFindController`、`Game1.player.addItemToInventoryBool(item)` |
| 难度 | ★★★☆☆ |
| 预期效果 | NPC 能主动把收集到的东西交给玩家 |

实现时要处理背包满的情况：成功交付的移除，失败的继续保留在 NPC 内部背包。


---

### 7. `npc_forage_collect` — 采集野生物品

| 项目 | 内容 |
|---|---|
| 可实现性 | 高 |
| 实现方式 | 扫描地图上 `IsSpawnedObject` 的野生采集物；NPC 走过去，播放拾取动作，移除地图物体并暂存 |
| 主要 API | `location.Objects.Pairs`、`obj.IsSpawnedObject`、`location.Objects.Remove(tile)` |
| 难度 | ★★★☆☆ |
| 预期效果 | NPC 可以在森林、海滩等区域采蘑菇、贝壳、野果 |

适合和 `npc_wander` 组合，让 NPC 边闲逛边顺手采集。

---

### 8. `npc_pet_animal` — 抚摸农场动物

| 项目 | 内容 |
|---|---|
| 可实现性 | 中高 |
| 实现方式 | 查找最近未被抚摸的动物，NPC 走到旁边，播放抱鸡/抚摸动作，调用动物互动逻辑 |
| 主要 API | `Farm.getAllFarmAnimals()`、`animal.wasPet`、`animal.pet(Game1.player)` |
| 难度 | ★★★☆☆ |
| 预期效果 | NPC 可以帮玩家照顾动物，动物冒爱心并增加好感 |

需要确认 `animal.pet(Game1.player)` 在 NPC 代劳场景下是否符合预期；不行则直接调整动物好感和 `wasPet` 状态。

---

### 9. `npc_plant_seeds` — 播种

| 项目 | 内容 |
|---|---|
| 可实现性 | 中高 |
| 实现方式 | 从 NPC 内部背包取种子，扫描空 `HoeDirt`，NPC 走过去播放播种动作，然后创建作物 |
| 主要 API | `HoeDirt`、`dirt.crop == null`、`dirt.plant(...)` 或 `new Crop(...)` |
| 难度 | ★★★★☆ |
| 预期效果 | NPC 能在已翻好的土地上自动播种 |

这个行为很有价值，但要验证种子 ID、季节合法性、是否能种在当前地图/地块。

---

### 10. `npc_till_soil` — 翻地
> 已完成

| 项目 | 内容 |
|---|---|
| 可实现性 | 中高 |
| 实现方式 | 在指定区域内找空地，NPC 走过去播放锄地动画，在该 tile 创建 `HoeDirt` |
| 主要 API | `location.terrainFeatures.Add(tile, new HoeDirt())`、`PathFindController` |
| 难度 | ★★★☆☆ |
| 预期效果 | NPC 能帮玩家开垦一片新耕地 |

需要限制仅在农场或可耕地地图执行，避免在小镇路面、室内等位置创建异常地块。

---

### 11. `npc_inspect_object` — 查看/检查物体

| 项目 | 内容 |
|---|---|
| 可实现性 | 高 |
| 实现方式 | NPC 走到目标 tile 旁，面对目标，播放观察动作，并读取该 tile 上的对象/作物/地形状态返回给 LLM |
| 主要 API | `location.Objects[tile]`、`terrainFeatures[tile]`、`PathFindController`、`showTextAboveHead()` |
| 难度 | ★★★☆☆ |
| 预期效果 | NPC 不只是“看”，还能把物体状态反馈给 LLM 作为后续决策依据 |

这是非常重要的感知行为，适合做成 LLM 的“观察环境”工具。

---

### 12. `npc_place_object` — 放置物品到地面

| 项目 | 内容 |
|---|---|
| 可实现性 | 中 |
| 实现方式 | 从 NPC 内部背包取出物品，寻路到目标 tile，验证 tile 为空，再放置到 `location.Objects` |
| 主要 API | `location.Objects.Add(tile, obj)`、`PathFindController` |
| 难度 | ★★★☆☆ |
| 预期效果 | NPC 能把物品放到地面，例如摆放装饰、机器或设施 |

这个行为需要更谨慎：不同物品能否被放置、是否是 `bigCraftable`、是否允许当前地图放置，都要做边界检查。

---

## 三、社交表演类行为

### 13. `npc_approach_and_speak` — 走过来搭话
> 已完成

| 项目 | 内容 |
|---|---|
| 可实现性 | 高 |
| 实现方式 | NPC 寻路到玩家附近，面对玩家，显示表情气泡；LLM 后续调用 `chat_say` |
| 主要 API | `PathFindController`、`faceDirection()`、`doEmote()` |
| 难度 | ★★★☆☆ |
| 预期效果 | NPC 能主动走向玩家说话，而不是原地发送消息 |

这是社交类行为里优先级最高的一个。

---

### 14. `npc_express_emotion` — 复合情绪表达



| 项目 | 内容 |
|---|---|
| 可实现性 | 很高 |
| 实现方式 | 根据情绪类型组合动画帧、气泡、跳跃、颤抖、转向等动作 |
| 主要 API | `Sprite.CurrentFrame`、`doEmote()`、`jump()`、`shake()`、`faceDirection()` |
| 难度 | ★★★☆☆ |
| 预期效果 | NPC 能表现开心、害羞、思考、生气、低落等复合情绪 |

实现成本不高，但对人设表现提升很明显。

---

### 15. `npc_shy_retreat` — 害羞后退

| 项目 | 内容 |
|---|---|
| 可实现性 | 高 |
| 实现方式 | 计算玩家方向，NPC 向反方向移动一格，背对玩家，播放害羞姿态和心形气泡 |
| 主要 API | `Position`、`faceDirection()`、`Sprite.CurrentFrame=46`、`shake()`、`doEmote(20)` |
| 难度 | ★★★☆☆ |
| 预期效果 | 适合傲娇、害羞型 NPC 的强人设动作 |

实现时要检查后退目标 tile 是否可通行，避免卡进障碍物。

---

### 16. `npc_show_text_bubble` — 头顶浮字

| 项目 | 内容 |
|---|---|
| 可实现性 | 很高 |
| 实现方式 | 直接调用 NPC 头顶浮字 API |
| 主要 API | `npc.showTextAboveHead(text)` |
| 难度 | ★☆☆☆☆ |
| 预期效果 | NPC 能轻量表达碎碎念、短反应、内心活动 |

这是最简单但很实用的行为，建议第一批实现。

---

### 17. `npc_idle_activity` — 空闲活动/播放动画

| 项目 | 内容 |
|---|---|
| 可实现性 | 高 |
| 实现方式 | 合并单次动画与循环动画。`mode=once` 播一次；`mode=loop` 持续播放农活/休息/四处看等动作 |
| 主要 API | `Sprite.Animate()`、`Sprite.CurrentFrame`、`faceDirection()`、`Halt()` |
| 难度 | ★★★☆☆ |
| 预期效果 | NPC 空闲时不会傻站，可以做出忙碌、休息、观察等动作 |

这个行为不改变世界状态，只提升表现力，风险较低。

---

### 18. `npc_dance_happy` — 庆祝舞蹈

| 项目 | 内容 |
|---|---|
| 可实现性 | 高 |
| 实现方式 | 通过跳跃、欢呼帧、快速转向、心形气泡组成庆祝动作 |
| 主要 API | `jump()`、`Sprite.CurrentFrame=45`、`faceDirection()`、`doEmote(20)` |
| 难度 | ★★★☆☆ |
| 预期效果 | NPC 在好感提升、任务完成、收获成功时表现开心 |

实现简单，表现效果明显。

---

### 19. `npc_react_surprise` — 惊吓反应

| 项目 | 内容 |
|---|---|
| 可实现性 | 很高 |
| 实现方式 | 跳起、切换惊讶帧、显示 `!` 气泡，短时间后恢复 |
| 主要 API | `jump()`、`Sprite.CurrentFrame=47`、`doEmote(16)` |
| 难度 | ★★☆☆☆ |
| 预期效果 | NPC 对突发情况有自然反应 |

适合在玩家突然靠近、送礼、发现怪事时触发。

---

### 20. `npc_pace_anxiously` — 焦虑踱步

| 项目 | 内容 |
|---|---|
| 可实现性 | 高 |
| 实现方式 | NPC 在当前位置左右来回移动，端点停顿，并偶尔播放问号或暂停气泡 |
| 主要 API | `Position`、`faceDirection()`、`Sprite.Animate()`、tick 计时 |
| 难度 | ★★★☆☆ |
| 预期效果 | NPC 能表现等待、焦虑、犹豫、思考等状态 |

实现时要检查左右移动路径是否可通行，避免穿墙或卡住。

---

## 四、推荐实现优先级

### 第一阶段：基础协程与低风险行为

建议先实现：

| 行为 | 原因 |
|---|---|
| `npc_show_text_bubble` | API 简单，验证 ws action → C# handler 链路 |
| `npc_idle_activity` | 验证动画帧与 tick 协程 |
| `npc_wander` | 验证通用寻路行为 |
| `npc_approach_and_speak` | 验证 NPC 主动走向玩家 |
| `npc_express_emotion` | 快速提升表现力 |

这阶段主要验证基础架构：`RichBehaviorHandler`、`IBehaviorCoroutine`、tick pump、Go MCP tool 注册。

---

### 第二阶段：农场助手核心能力

建议实现：

| 行为 | 原因 |
|---|---|
| `npc_clear_debris` | 有明确游戏效果，玩家感知强 |
| `npc_water_crops` | 农场助手核心能力 |
| `npc_harvest_crops` | 与物品收集系统绑定 |
| `npc_deposit_items` | 形成“收获 → 存箱子”闭环 |
| `npc_deliver_items` | 形成“收获 → 交给玩家”闭环 |

这阶段会引入 `NpcInventory`，让 NPC 有“临时携带物品”的能力。

---

### 第三阶段：扩展农事与世界操作

建议实现：

| 行为 | 原因 |
|---|---|
| `npc_forage_collect` | 扩展到农场以外地图 |
| `npc_pet_animal` | 增加动物照料能力 |
| `npc_till_soil` | 支持开垦 |
| `npc_plant_seeds` | 支持完整种植链 |
| `npc_place_object` | 功能强但需要更严格边界检查 |
| `npc_inspect_object` | 让 LLM 能基于观察做决策 |

---

## 五、实现风险与注意点

| 风险点 | 说明 | 建议 |
|---|---|---|
| 世界状态修改 | 清理、收获、播种、放置物体会真实改变存档 | 所有行为限制 `radius`、`max_count`、地图类型 |
| 作物 API 细节 | `crop.harvest()`、`dirt.plant()` 需实际编译验证 | 先做小范围测试 |
| 箱子容量 | 箱子满时可能无法全部放入 | 保留剩余物品在 `NpcInventory` |
| NPC 穿墙/卡住 | 直接改 `Position` 的行为有风险 | 优先使用 `PathFindController`，直接位移前检查 tile |
| 多行为冲突 | NPC 同时执行多个行为会互相覆盖 | 单 NPC 单协程，新行为取消旧行为 |
| LLM 滥用工具 | 可能频繁清理/收获导致体验异常 | tool description 明确约束调用频率和场景 |

---

## 六、结论

这 20 个行为中，**全部具备实现基础**。其中：

- **最容易优先落地**：`npc_show_text_bubble`、`npc_idle_activity`、`npc_react_surprise`、`npc_express_emotion`
- **最有玩家价值**：`npc_clear_debris`、`npc_water_crops`、`npc_harvest_crops`、`npc_deposit_items`
- **最能体现 AI NPC 智能感**：`npc_wander`、`npc_inspect_object`、`npc_forage_collect`、`npc_deliver_items`
- **需要最谨慎实现**：`npc_plant_seeds`、`npc_place_object`、`npc_harvest_crops`

推荐从**基础协程 + 低风险表现行为**开始，再逐步实现**农场助手闭环**，最后扩展到播种、放置物品、动物互动等更复杂行为。
