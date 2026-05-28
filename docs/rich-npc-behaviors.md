# Rich NPC Behavior System — 策划文档

> 为 SmartNPC 的 AI NPC 设计 20 个**有实际游戏功能意义**的高层行为。
> 行为分为两大类：**世界交互类**（操作游戏世界对象）和**社交表演类**（肢体语言+情绪表达）。
> 全部由 Hermes LLM 人格自主决策触发。

## 设计原则

1. **有实质意义** — 行为要产生真实的游戏效果（清理杂物、存放物品、种植浇水），不只是播动画
2. **纯 LLM 决策** — 行为作为 MCP tool 暴露，C# 侧不做自动触发规则
3. **零新美术** — 全部使用现有 49 帧精灵 + SDV 原生气泡
4. **单 NPC 单协程** — 每个 NPC 同一时刻只运行一个行为，新行为自动取消旧行为

---

## 现有精灵帧参考

| 帧索引 | 内容 |
|--------|------|
| 0-7 | 正面行走 (8帧) |
| 8-15 | 背面行走 (8帧) |
| 16-23 | 侧面行走 (8帧) |
| 24-32 | 锄地动作 (9帧) |
| 33-39 | 浇水动作 (7帧) |
| 40 | 捧南瓜 |
| 41 | 捧作物 |
| 42 | 捧花 |
| 43 | 站立待机 |
| 44 | 抱鸡 |
| 45 | 欢呼姿态 |
| 46 | 害羞/心动姿态 |
| 47 | 惊讶姿态 |
| 48 | 犯困姿态 |

另有 SDV 原生：10 种气泡表情、`showTextAboveHead()`、`jump()`、`shake()`。

---

## 行为清单

### 一、世界交互类（有实际游戏效果）

#### 1. `npc_wander` — 自主漫步

| 项目 | 内容 |
|------|------|
| **效果** | NPC 在当前地图（或指定地图）随机选择可达 tile，寻路走过去；到达后停留几秒，再选下一个点。持续 `duration` 秒后自动结束。给玩家"NPC 在小镇里闲逛"的真实感 |
| **API** | `PathFindController`(随机 endPoint) + endBehavior callback 重新选点 + `faceDirection()` 到达后随机朝向 + 可选 `showTextAboveHead()` 偶尔碎碎念 |
| **实现** | tick 驱动协程：选点 → 寻路 → 等 controller 完成 → 停留 2-4s → 循环。选点逻辑：在地图 `isTilePassable()` 范围内随机挑可达 tile（距 NPC 5-15 tile），寻路失败则重选 |
| **难度** | ★★★☆☆ |
| **参数** | `npc`, 可选 `map`(默认当前地图), 可选 `duration_seconds`(默认60, 上限300) |
| **预期效果** | NPC 在镇上自然地东走西逛，偶尔停下来看看四周，像一个真正生活在这里的人 |

#### 2. `npc_clear_debris` — 清理农场杂物

| 项目 | 内容 |
|------|------|
| **效果** | NPC 扫描农场上的杂草(Weeds)、树枝(Twig)、碎石(Stone)，寻路走到最近的一个 → 播放锄地动画 → 从 `location.Objects` 中移除该物体 → 寻找下一个 → 循环直到清完或超时 |
| **API** | 遍历 `location.Objects.Pairs` 用 `obj.IsWeeds()` / `obj.IsTwig()` / `obj.IsBreakableStone()` 筛选 → `PathFindController` 走到旁边 → `Sprite.Animate()`(锄地帧 24-32) → `location.Objects.Remove(tile)` 移除 |
| **实现** | 协程循环：scan → pick nearest → pathfind → arrive → animate hoe (9帧 ~0.75s) → remove object → scan next。同时从碎片中随机产出少量资源（石头→stone, 树枝→wood, 杂草→fiber）放入 NPC 内部背包 |
| **难度** | ★★★★☆ |
| **参数** | `npc`, 可选 `radius`(默认20 tile), 可选 `max_count`(默认10), 可选 `duration_seconds`(默认60) |
| **预期效果** | NPC 拿着锄头在农场上一个一个清理杂草和石头，动作自然，像真的在帮玩家干活 |

#### 3. `npc_water_crops` — 浇灌作物

| 项目 | 内容 |
|------|------|
| **效果** | NPC 扫描附近的 `HoeDirt`（已耕地），找出有作物但未浇水的地块 → 寻路走到旁边 → 播放浇水动画 → 将 `HoeDirt.state` 设为已浇水 → 下一块 |
| **API** | 遍历 `location.terrainFeatures.Pairs`，筛选 `tf is HoeDirt dirt && dirt.crop != null && dirt.state.Value != 1` → `PathFindController` → `Sprite.Animate()`(浇水帧 33-39) → `dirt.state.Value = 1` |
| **实现** | 与 clear_debris 类似的 scan-walk-act 循环。浇水动画 7 帧约 0.6s |
| **难度** | ★★★★☆ |
| **参数** | `npc`, 可选 `radius`(默认15), 可选 `max_count`(默认20), 可选 `duration_seconds`(默认90) |
| **预期效果** | NPC 拿着水壶在菜地里一块一块浇水，能直接看到地块变色（浇水后颜色变深） |

#### 4. `npc_harvest_crops` — 收获成熟作物

| 项目 | 内容 |
|------|------|
| **效果** | NPC 扫描附近的 `HoeDirt`，找出已成熟的作物 → 寻路到旁边 → 播放锄地动画 → 调用 `crop.harvest()` 收获 → 产出物品暂存到 NPC 的"收集物"列表，后续可交给玩家或放入箱子 |
| **API** | 遍历 `terrainFeatures` 筛选成熟作物 → `PathFindController` → `Sprite.Animate()` → `dirt.crop.harvest(tileX, tileY, dirt)` → 收集产出的 `Item` |
| **实现** | harvest() 调用 SDV 原生收获逻辑（含经验、品质计算）。收获物暂存在 handler 维护的 `List<Item>` 中，后续通过 `npc_deposit_items` 或 `npc_deliver_items` 交付 |
| **难度** | ★★★★☆ |
| **参数** | `npc`, 可选 `radius`(默认15), 可选 `max_count`(默认10) |
| **预期效果** | NPC 走到成熟的作物旁，弯腰采摘，作物消失，NPC 手上"拿着"收获的东西 |

#### 5. `npc_deposit_items` — 将物品放入箱子

| 项目 | 内容 |
|------|------|
| **效果** | NPC 携带之前收集的物品 → 寻路到指定位置的箱子(Chest) → 播放放置动画 → 将物品逐个放入箱子 |
| **API** | 通过 tile 坐标在 `location.Objects` 中找到 `Chest` 对象 → `PathFindController` 走到旁边 → `Sprite.CurrentFrame`(捧物帧 40-41) → `chest.addItem(item)` 逐个放入 |
| **实现** | 先验证目标 tile 是 Chest 且箱子有空位。寻路到达后，每放一个物品播一个短动画。箱子满了提前结束并报告 |
| **难度** | ★★★☆☆ |
| **参数** | `npc`, `chest_x`, `chest_y`, 可选 `chest_map`(默认当前地图) |
| **预期效果** | NPC 捧着东西走到箱子旁，一件件放进去。玩家打开箱子能看到 NPC 存放的物品 |

#### 6. `npc_deliver_items` — 将物品交给玩家

| 项目 | 内容 |
|------|------|
| **效果** | NPC 携带收集的物品 → 寻路走向玩家 → 面对玩家 → 播放捧物帧 → 将物品逐个放入玩家背包。背包满则提示并保留剩余 |
| **API** | `PathFindController`(玩家前方 tile) → `Sprite.CurrentFrame`(捧物帧) → `Game1.player.addItemToInventoryBool(item)` → `showTextAboveHead()` |
| **实现** | 交付前显示捧物姿态，每个物品间隔 0.3s |
| **难度** | ★★★☆☆ |
| **参数** | `npc` |
| **预期效果** | NPC 捧着一堆东西走过来递给玩家，浮字显示"这是今天收获的东西" |

#### 7. `npc_forage_collect` — 采集野生物品

| 项目 | 内容 |
|------|------|
| **效果** | NPC 扫描当前地图上的可采集物品（`obj.IsSpawnedObject` 为 true 的地面物品，即觅食物/贝壳/水果等）→ 寻路走到旁边 → 弯腰拾取动画 → 从 `location.Objects` 中移除并收入 NPC 收集列表 |
| **API** | 遍历 `location.Objects.Pairs` 筛选 `obj.IsSpawnedObject` → `PathFindController` → `Sprite.Animate()` → `location.Objects.Remove(tile)` → 暂存 item |
| **实现** | 与 clear_debris 的 scan-walk-act 模式相同，区别是保留拾取的物品而非丢弃 |
| **难度** | ★★★☆☆ |
| **参数** | `npc`, 可选 `radius`(默认20), 可选 `max_count`(默认5) |
| **预期效果** | NPC 在海边捡贝壳、在森林里采蘑菇，然后可以通过 deliver/deposit 交给玩家或放进箱子 |

#### 8. `npc_pet_animal` — 抚摸农场动物

| 项目 | 内容 |
|------|------|
| **效果** | NPC 寻路到指定（或最近的）农场动物旁 → 面对动物 → 播放抱鸡帧(44) → 心形气泡 → 增加动物好感度 |
| **API** | 遍历 `farm.getAllFarmAnimals()` → `PathFindController` 走到旁边 → `Sprite.CurrentFrame=44` → `animal.pet(Game1.player)` → `doEmote(20)` |
| **实现** | 找到最近的未被今日抚摸的动物（`animal.wasPet.Value == false`），寻路过去，播放动画，增加好感 |
| **难度** | ★★★☆☆ |
| **参数** | `npc`, 可选 `animal_name`(不指定则找最近未摸的) |
| **预期效果** | NPC 走到鸡/牛/羊旁边蹲下抱抱，动物头上冒爱心。实际增加动物好感度 |

#### 9. `npc_plant_seeds` — 播种

| 项目 | 内容 |
|------|------|
| **效果** | NPC 从 NpcInventory 中取出种子类物品 → 扫描附近已耕未种的 `HoeDirt`（`dirt.crop == null`）→ 寻路走到旁边 → 播放锄地动画 → 在该地块种下种子 |
| **API** | 遍历 `terrainFeatures` 筛选空 HoeDirt → `PathFindController` → `Sprite.Animate()`(锄地帧) → `dirt.plant(seedItem.ItemId, farmer)` 或 `dirt.crop = new Crop(seedId, tileX, tileY, location)` |
| **实现** | 从 NpcInventory 中查找 Category=-74（种子类）的物品。每种一颗消耗一个种子。种完或种子用完结束 |
| **难度** | ★★★★☆ |
| **参数** | `npc`, 可选 `seed_id`(不指定则用背包中第一个种子), 可选 `max_count`(默认10) |
| **预期效果** | NPC 拿着种子在已翻好的地里一颗颗种下去，玩家过会儿能看到嫩芽冒出来 |

#### 10. `npc_till_soil` — 翻地

| 项目 | 内容 |
|------|------|
| **效果** | NPC 在农场指定区域内，将普通地块变为耕地 → 寻路到目标 tile → 播放锄地动画 → 在该 tile 创建 `HoeDirt` |
| **API** | 检查 `location.isTilePassable()` 且无现有 terrainFeature → `PathFindController` → `Sprite.Animate()`(锄地帧 24-32) → `location.terrainFeatures.Add(tile, new HoeDirt())` |
| **实现** | 扫描 NPC 附近 radius 内的空地 tile，逐个翻地。每翻一块播一次锄地动画 |
| **难度** | ★★★☆☆ |
| **参数** | `npc`, `center_x`, `center_y`, 可选 `radius`(默认3), 可选 `max_count`(默认9) |
| **预期效果** | NPC 挥锄头在空地上翻出一片耕地，后续可以播种 |

#### 11. `npc_inspect_object` — 查看/检查物体

| 项目 | 内容 |
|------|------|
| **效果** | NPC 寻路到目标 tile 旁的物体/作物/地形 → 面对目标 → 问号/开心气泡 → 可选头顶评论文字。表现出 NPC 在观察和关注农场环境，同时返回该 tile 的物体信息给 LLM |
| **API** | `PathFindController` 到相邻 tile → `faceDirection()` 朝向目标 → `doEmote()` → `showTextAboveHead()` → 读取 `location.Objects[tile]` 或 `terrainFeatures[tile]` 的信息并返回 |
| **实现** | 寻路到达后，回报目标 tile 上物体的类型、名称、状态（如作物生长阶段、机器加工进度等），供 LLM 做后续决策 |
| **难度** | ★★★☆☆ |
| **参数** | `npc`, `target_x`, `target_y`, 可选 `emote_kind`, 可选 `text` |
| **预期效果** | NPC 好奇地走过去看看某个东西，同时 LLM 获得该物体的详细信息来决定下一步 |

#### 12. `npc_place_object` — 放置物品到地面

| 项目 | 内容 |
|------|------|
| **效果** | NPC 从 NpcInventory 中取出指定物品 → 寻路到目标 tile → 播放放置动画 → 将物品放置在地面上（如放置加工机、装饰物等） |
| **API** | `PathFindController` → `Sprite.Animate()` → `location.Objects.Add(tile, obj)` 放置 `StardewValley.Object` → 消耗 NpcInventory 中对应物品 |
| **实现** | 验证目标 tile 为空（无现有 Object），从 NpcInventory 找到对应物品，放置后标记为 `bigCraftable` 或普通 Object |
| **难度** | ★★★☆☆ |
| **参数** | `npc`, `item_id`, `target_x`, `target_y` |
| **预期效果** | NPC 把手里的东西摆在地上——比如放个洒水器、摆个装饰品 |

---

### 二、社交表演类（情绪 + 肢体语言）

#### 13. `npc_approach_and_speak` — 走过来搭话

| 项目 | 内容 |
|------|------|
| **效果** | NPC 寻路到玩家身旁（1-2 格）→ 面对玩家 → 显示到达气泡。LLM 可紧接 `chat_say` 说话 |
| **API** | `PathFindController`(endPoint=玩家前方) + endBehavior callback + `faceDirection()` + `doEmote()` |
| **实现** | 创建寻路，注册 arrival callback。callback 中面对玩家 + 气泡 + 标记完成 |
| **难度** | ★★★☆☆ |
| **参数** | `npc`, 可选 `emote_kind` |
| **预期效果** | NPC 主动走过来找你说话，而不是站在原地等你去找他 |

#### 14. `npc_express_emotion` — 复合情绪表达

| 项目 | 内容 |
|------|------|
| **效果** | 根据情绪组合多信号：`happy`(欢呼+跳+心) / `embarrassed`(害羞帧+背对+抖) / `thinking`(问号+左右看) / `angry`(抖+怒气泡+背对) / `melancholy`(犯困帧+悲伤气泡) |
| **API** | `Sprite.CurrentFrame` + `doEmote()` + `jump()` + `shake()` + `faceDirection()` |
| **实现** | 每种情绪是预定义步骤序列，共用协程框架 |
| **难度** | ★★★☆☆ |
| **参数** | `npc`, `emotion`(happy/embarrassed/thinking/angry/melancholy), 可选 `text` |
| **预期效果** | NPC 用丰富的肢体语言表达情绪，不只是冒个气泡 |

#### 15. `npc_shy_retreat` — 害羞后退

| 项目 | 内容 |
|------|------|
| **效果** | NPC 后退 1 格 → 背对玩家 → 害羞帧(46) → 颤抖 → 心形气泡 → 浮字。傲娇名场面 |
| **API** | `Position` 偏移 + `faceDirection()` + `Sprite.CurrentFrame=46` + `shake()` + `doEmote(20)` + `showTextAboveHead()` |
| **实现** | 计算玩家方向，反向位移 64px，播放情绪序列 |
| **难度** | ★★★☆☆ |
| **参数** | `npc`, 可选 `text` |
| **预期效果** | 说了暖心的话之后害羞地退一步别过头 |

#### 16. `npc_show_text_bubble` — 头顶浮字

| 项目 | 内容 |
|------|------|
| **效果** | NPC 头顶出现短文字，3s 后消失。比 `chat_say` 更轻量，适合碎碎念 |
| **API** | `npc.showTextAboveHead(text)` |
| **实现** | 单次调用，无状态机 |
| **难度** | ★☆☆☆☆ |
| **参数** | `npc`, `text`(≤40字) |
| **预期效果** | NPC 自言自语，不进入正式对话流 |

#### 17. `npc_idle_activity` — 空闲活动/播放动画

| 项目 | 内容 |
|------|------|
| **效果** | NPC 在原地执行一段持续活动。支持两种模式：<br>• **单次动画**(mode=`once`)：播放指定帧序列 1 次后恢复待机（锄地/浇水/各种 pose）<br>• **循环活动**(mode=`loop`)：持续循环动画+偶尔切方向/冒气泡，超时后结束（做农活/四处看/休息）<br>**纯表演，不影响世界状态** |
| **API** | `Sprite.Animate()` + `Sprite.CurrentFrame` + `faceDirection()` + `Halt()` + `doEmote()` + tick 驱动 |
| **实现** | mode=once：根据 `animation` 参数选帧范围，逐帧推进，播完复原。mode=loop：子状态机循环（动画→间隔→重复），超时结束 |
| **难度** | ★★★☆☆ |
| **参数** | `npc`, `animation`(hoe/water/hold_pumpkin/hold_crop/hold_flower/hold_chicken/cheer/heart/surprised/sleepy/look_around/rest), 可选 `mode`(once/loop, 默认once), 可选 `duration_seconds`(loop模式默认10, 上限60) |
| **预期效果** | once: LLM 根据情境触发合适的肢体语言。loop: 没有具体任务时让 NPC 看起来在"忙着"而不是傻站着 |

#### 18. `npc_dance_happy` — 庆祝舞蹈

| 项目 | 内容 |
|------|------|
| **效果** | 跳起 → 欢呼帧 → 快速转圈 → 再跳 → 心形气泡。约 3 秒 |
| **API** | `jump()` × 2 + `Sprite.CurrentFrame=45` + `faceDirection()` × 4 + `doEmote(20)` |
| **实现** | ~180 tick 步骤序列 |
| **难度** | ★★★☆☆ |
| **参数** | `npc` |
| **预期效果** | 得到好消息或完成任务后的庆祝 |

#### 19. `npc_react_surprise` — 惊吓反应

| 项目 | 内容 |
|------|------|
| **效果** | NPC 跳起 → 惊讶帧(47) → `!` 气泡 → 800ms 后恢复。用于被突然搭话、看到意外事物 |
| **API** | `jump()` + `Sprite.CurrentFrame=47` + `doEmote(16)` + tick 计时 |
| **实现** | 4 步序列：jump@tick0 → frame@tick5 → emote@tick10 → restore@tick50 |
| **难度** | ★★☆☆☆ |
| **参数** | `npc`, 可选 `face_direction` |
| **预期效果** | NPC 被吓了一跳的自然反应 |

#### 20. `npc_pace_anxiously` — 焦虑踱步

| 项目 | 内容 |
|------|------|
| **效果** | NPC 原地左右走 3 格，每到端点停 500ms，循环到时间结束。偶尔显示问号/暂停气泡。表达等待、焦虑、思考 |
| **API** | `Position` 直接偏移 + `faceDirection(3/1)` 交替 + `Sprite.Animate()`(步行帧) + tick 计时 |
| **实现** | 状态机：WalkLeft → PauseLeft → WalkRight → PauseRight → 循环。每 tick 偏移 2px，3 tile = 192px |
| **难度** | ★★★☆☆ |
| **参数** | `npc`, 可选 `duration_seconds`(默认8, 上限20), 可选 `tiles`(默认3) |
| **预期效果** | NPC 来回踱步，适合等人、犹豫不决、焦急等情境 |

---

## 总览对比表

| # | 行为 | 分类 | 难度 | 游戏效果 | 核心机制 |
|---|------|------|------|---------|----------|
| 1 | `npc_wander` | 世界 | ★★★ | 真实游走 | PathFind 循环 + 随机选点 |
| 2 | `npc_clear_debris` | 世界 | ★★★★ | 移除杂物 | Objects.Remove + 锄地动画 |
| 3 | `npc_water_crops` | 世界 | ★★★★ | 浇灌作物 | HoeDirt.state=1 + 浇水动画 |
| 4 | `npc_harvest_crops` | 世界 | ★★★★ | 收获作物 | crop.harvest() + 物品暂存 |
| 5 | `npc_deposit_items` | 世界 | ★★★ | 存入箱子 | Chest.addItem() |
| 6 | `npc_deliver_items` | 世界 | ★★★ | 交给玩家 | addItemToInventoryBool() |
| 7 | `npc_forage_collect` | 世界 | ★★★ | 野外采集 | Objects.Remove + 保留物品 |
| 8 | `npc_pet_animal` | 世界 | ★★★ | 抚摸动物 | animal.pet() / 好感 +15 |
| 9 | `npc_plant_seeds` | 世界 | ★★★★ | 播种 | HoeDirt.plant() / new Crop() |
| 10 | `npc_till_soil` | 世界 | ★★★ | 翻地 | terrainFeatures.Add(HoeDirt) |
| 11 | `npc_inspect_object` | 世界 | ★★★ | 查看物体+回报信息 | PathFind + 读取 Object 状态 |
| 12 | `npc_place_object` | 世界 | ★★★ | 放置物品到地面 | Objects.Add() |
| 13 | `npc_approach_and_speak` | 社交 | ★★★ | 主动搭话 | PathFind + callback |
| 14 | `npc_express_emotion` | 社交 | ★★★ | 复合情绪 | frame + emote + shake + jump |
| 15 | `npc_shy_retreat` | 社交 | ★★★ | 傲娇后退 | position + face + shake |
| 16 | `npc_show_text_bubble` | 社交 | ★ | 头顶浮字 | showTextAboveHead |
| 17 | `npc_idle_activity` | 社交 | ★★★ | 空闲活动/肢体语言 | Animate + 状态机 |
| 18 | `npc_dance_happy` | 社交 | ★★★ | 庆祝舞蹈 | jump + spin + emote |
| 19 | `npc_react_surprise` | 社交 | ★★ | 惊吓反应 | jump + frame + emote |
| 20 | `npc_pace_anxiously` | 社交 | ★★★ | 焦虑踱步 | position tick loop |

---

## 关键 SDV API 用法速查

```csharp
// ── 遍历地面物体 ──
foreach (var kv in location.Objects.Pairs)  // key=Vector2(tile), value=StardewValley.Object
{
    var tile = kv.Key;
    var obj = kv.Value;
    if (obj.IsWeeds())         { /* 杂草 */ }
    if (obj.IsTwig())          { /* 树枝 */ }
    if (obj.IsBreakableStone()){ /* 碎石 */ }
    if (obj.IsSpawnedObject)   { /* 可采集觅食物 */ }
}
location.Objects.Remove(tile); // 移除物体
location.Objects.Add(tile, newObj); // 放置物体

// ── 遍历地形特征（耕地/树/草） ──
foreach (var kv in location.terrainFeatures.Pairs) // key=Vector2, value=TerrainFeature
{
    if (kv.Value is HoeDirt dirt)
    {
        if (dirt.crop == null) { /* 空耕地，可播种 */ }
        if (dirt.crop != null && dirt.state.Value != 1) { /* 有作物未浇水 */ }
        dirt.state.Value = 1; // 浇水
        dirt.crop.harvest(tileX, tileY, dirt); // 收获
    }
}
// 翻新地
location.terrainFeatures.Add(tile, new HoeDirt());
// 播种
dirt.crop = new Crop(seedId, (int)tile.X, (int)tile.Y, location);

// ── 箱子操作 ──
if (location.Objects.TryGetValue(tile, out var chestObj) && chestObj is Chest chest)
{
    chest.addItem(item);         // 放入
    chest.Items.Remove(item);    // 取出
}

// ── 动物操作 ──
if (location is Farm farm)
{
    foreach (var animal in farm.getAllFarmAnimals())
    {
        if (!animal.wasPet.Value) { /* 今日未摸 */ }
        animal.pet(Game1.player);  // 抚摸（增加好感+设 wasPet）
    }
}

// ── NPC 内部收集物 ──
// handler 维护 Dictionary<npcName, List<Item>> 暂存
// deliver/deposit 时消费这个列表
```

---

## 行为链示例

LLM 可以将多个行为串联成有意义的"一天的工作"：

```
早晨  → npc_wander (走到农场)
      → npc_water_crops (浇灌所有作物)
      → npc_show_text_bubble ("今天的作物看起来不错")

上午  → npc_till_soil (翻几块新地)
      → npc_plant_seeds (种下昨天买的种子)
      → npc_express_emotion (happy)

午后  → npc_harvest_crops (收获成熟的)
      → npc_deposit_items (放进农场箱子)
      → npc_approach_and_speak + chat_say ("收获了好多东西！")

傍晚  → npc_clear_debris (清理一圈杂草)
      → npc_pet_animal (去鸡舍摸摸鸡)
      → npc_forage_collect (顺路捡蘑菇)
      → npc_deliver_items (把采集的东西交给玩家)

闲暇  → npc_idle_activity (loop: look_around)
      → npc_wander (在小镇溜达)
      → npc_inspect_object (看看路边的花)
      → npc_show_text_bubble ("这朵花好漂亮")
```

---

## 架构概要

```
┌─────────────────┐       ws        ┌──────────────────┐      MCP HTTP      ┌───────────────┐
│  C# SMAPI Mod   │ ◄────────────── │  smartnpc-mcp    │ ◄───────────────── │ Hermes Gateway│
│                 │                  │  (Go)            │                     │ (LLM 决策)    │
│ RichBehaviorHdl │  20 个 action   │ npc_rich_behavior│  tool call          │ SOUL.md 人格  │
│ IBehaviorCorout │  world+social   │ 20 个 MCP tool   │                     │ 决定何时调用  │
│ tick pump 60fps │                  │ Input/Output DTO │                     │               │
│ NpcInventory    │                  │                  │                     │               │
│ (收集物暂存)     │                  │                  │                     │               │
└─────────────────┘                  └──────────────────┘                     └───────────────┘
```

新增组件：
- `NpcInventory` — 每个 NPC 的临时物品收集列表（内存中，不持久化到存档）
- `IBehaviorCoroutine` — tick 驱动协程接口
- `RichBehaviorHandler` — 持有活跃协程 + NpcInventory + ws handler 入口

---

## 实现顺序

| 阶段 | 内容 | 预计工作量 |
|------|------|-----------|
| 0 | 协程骨架 + NpcInventory + ModEntry 注册 | 0.5 天 |
| 1 | 世界交互基础: wander + clear_debris + water_crops | 2-3 天 |
| 2 | 农事完整链: harvest + plant_seeds + till_soil + inspect_object | 2-3 天 |
| 3 | 物品流转: deposit + deliver + forage + pet_animal + place_object | 2 天 |
| 4 | 社交表演: approach + emotion + shy + text + idle_activity + dance + surprise + pace | 1.5-2 天 |
| 5 | Go MCP tools + tests + protocol.md | 2 天 |

**总计 ~10-12 天**
