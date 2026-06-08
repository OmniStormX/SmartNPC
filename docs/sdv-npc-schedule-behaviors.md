# SDV NPC Schedule 行为系统（反编译分析）

> 来源：`Stardew Valley.dll` — `StardewValley.NPC` 类，ilspycmd 8.2 反编译  
> 版本：Stardew Valley 1.6（DLL 于 2026-06 分析）  
> 关联设计：`docs/superpowers/specs/` 切断游戏 NPC 控制方案

---

## 1. 架构概览

`npc.Schedule` 是 `Dictionary<int, SchedulePathDescription>`，键为游戏内时间（如 `610` = 早 6:10），每天由 `TryLoadSchedule()` 从 `Data/Characters/Schedules/<NpcName>.xnb` 加载。

驱动链路：

```
Game1.UpdateTick
  → NPC.update()
    → checkSchedule(timeOfDay)          ← 每 tick 检查是否到点
      → PathFindController 开始寻路
        → 到达目的地时触发 endBehaviorFunction
          → getRouteEndBehaviorFunction(endOfRouteBehavior, endOfRouteMessage)
            → walkInSquareAtEndOfRoute  (square_ 系)
            → doAnimationAtEndOfScheduleRoute  (AnimationDescriptions 系)
            → startRouteBehavior()      (hardcoded 特例)
```

`checkSchedule` 的早退条件（代码直接证据）：

```csharp
if (ignoreScheduleToday || Schedule == null)
    return;
```

即：**将 `ignoreScheduleToday` 设为 `true` 或清空 `Schedule` 字典，即可完全阻止 schedule 驱动**。

---

## 2. SchedulePathDescription 字段

每条 schedule 记录包含：

| 字段 | 类型 | 说明 |
|------|------|------|
| `route` | `Stack<Point>` | 预计算路径点列表 |
| `facingDirection` | `int` | 到达后朝向（0=上 1=右 2=下 3=左） |
| `endOfRouteBehavior` | `string` | 到达后执行的命名行为（见下表） |
| `endOfRouteMessage` | `string` | 到达后说的台词（`"..."` 格式） |
| `time` | `int` | 触发时刻（游戏内时间，如 `1000`） |

---

## 3. endOfRouteBehavior 完整分类

### 3.1 硬编码特例（`startRouteBehavior` switch + 额外逻辑）

这些行为在 `NPC.cs` 中有专用 C# 代码，**不依赖** `AnimationDescriptions.xnb`：

| 行为名 | 适用 NPC | 触发效果 | 清理（finishRouteBehavior） |
|--------|----------|----------|-----------------------------|
| `abigail_videogames` | Abigail | 广播游戏机屏幕精灵（id=688）到当前 location；播放 emote 52（游戏图标） | 移除精灵 id=688 |
| `clint_hammer` | Clint | 扩展 sprite 宽 +16px（32px 宽）；切换到帧 8；在第 14 帧插入锤击音效回调 `clintHammerSound` | `reloadSprite()` 恢复；`Halt()` |
| `birdie_fish` | Birdie（海滩老人） | 扩展 sprite 宽 +16px（32px 宽）；切换到帧 8 | `reloadSprite()` 恢复；`Halt()` |
| `dick_fish` | Willy（内部名 Dick） | 扩展 sprite 高 +32px（64px 高）；`drawOffset=(0,96)`；播放 `slosh` 音效 | `reloadSprite()` 恢复；`Halt()` |

### 3.2 通用 pattern（所有 NPC，代码逻辑匹配字符串 pattern）

| 模式 | 匹配条件 | 效果 |
|------|----------|------|
| `square_W_H` 或 `square_W_H_F` | `behaviorName.Contains("square_")` | 以当前 tile 为中心，在 W×H 方格内循环走动（`walkInSquare`）；`F` 为 facing preference（可选） |
| `*_sleep`（任意 `_sleep` 结尾） | `behaviorName.Contains("sleep")` | 调用 `playSleepingAnimation()`：设 `isSleeping=true`、`layingDown=true`、`HideShadow=true`；从 `AnimationDescriptions["npcname_sleep"]` 读取睡眠帧 |
| `change_beach` | 精确匹配 | 换成沙滩服装（outfit overlay），走 `doAnimationAtEndOfScheduleRoute` 路径但跳过 AnimationDescriptions 查表 |
| `change_normal` | 精确匹配 | 换回普通服装，同上 |
| `"..."`（引号包裹） | `behaviorName[0] == '"'` | 将引号内文本设为 `endOfRouteMessage`，直接作为台词显示，**不触发任何动画** |

### 3.3 metadata 修饰符（附加在 AnimationDescriptions 数据的第 5 字段起）

这些不是独立行为名，而是 `routeAnimationMetadata` 数组中的指令：

| 指令 | 格式 | 效果 |
|------|------|------|
| `laying_down` | 单词 | 设 `layingDown=true`、`HideShadow=true`（躺下姿态，隐藏阴影） |
| `offset X Y` | `offset <int> <int>` | 设置 `drawOffset`（像素偏移），用于对齐特定动画帧 |
| `penny_dishes` | 精确字符串 | 额外设 `drawOffset.Y += 16`（洗碗动画对齐） |

### 3.4 数据驱动行为（`Data/animationDescriptions.xnb`，LZX 压缩，完整 key 列表无法直接解包）

格式：`introFrames / loopFrames / outroFrames [/ message [/ metadata...]]`

触发流程：
1. `getRouteEndBehaviorFunction` 查 `AnimationDescriptions` 字典，找到则返回 `doAnimationAtEndOfScheduleRoute`
2. `loadEndOfRouteBehavior` 解析 intro/loop/outro 帧数组
3. 到达目的地时：播放 intro 帧 → loop 帧（循环）→ 离开时播放 outro 帧
4. 同时调用 `startRouteBehavior(behaviorName)` 处理硬编码特例

已从代码中确认的 **data-driven behavior key 规律**：
- `<npcname>_sleep`（小写）：每个 NPC 各自的睡眠帧（`playSleepingAnimation` 读取）
- `penny_dishes`：Penny 洗碗动画（有 metadata 修饰）
- 其余 key 全部存于 xnb 数据文件，需游戏运行时通过 `DataLoader.AnimationDescriptions` 读取

---

## 4. 睡觉系统单独入口（独立于 Schedule）

`startSleeping()` / `playSleepingAnimation()` 可由游戏在 Schedule 之外触发：

```csharp
// playSleepingAnimation 实现摘要
isSleeping.Value = true;
drawOffset = new Vector2(0f, name == "Sebastian" ? 12 : -4);
if (isMarried()) drawOffset.X = -12f;
// 从 AnimationDescriptions["npcname_sleep"] 读帧
DataLoader.AnimationDescriptions(Game1.content).TryGetValue(name.ToLower() + "_sleep", out var frame);
```

特殊说明：
- Sebastian 睡觉 `drawOffset.Y = 12`（其他 NPC 为 `-4`）
- 已婚配偶 `drawOffset.X = -12`（双人床对齐）

**这是 Schedule 切断后需要单独 Harmony patch 的入口**。

---

## 5. 每日重置机制（`dayupdate`）

每天开始时游戏执行：

```csharp
ignoreScheduleToday = false;   // 重置标志
controller = null;
temporaryController = null;
directionsToNewLocation = null;
Schedule = null;               // 随后由 TryLoadSchedule() 重新填充
isSleeping.Value = false;
isPlayingSleepingAnimation = false;
_startedEndOfRouteBehavior = null;
_finishingEndOfRouteBehavior = null;
```

然后调用 `TryLoadSchedule()` 重新从数据文件加载 Schedule。

**设计影响：永久切断游戏控制必须在 `DayStarted` 事件中重设 `ignoreScheduleToday = true` 并清空 Schedule，因为 `dayupdate` 会把 `ignoreScheduleToday` 重置为 `false`。**

---

## 6. `performTenMinuteUpdate` 附带行为

每 10 游戏分钟调用，与 Schedule 无关，但会触发两类行为需注意：

1. **Ambient 台词**：以 10% 概率显示 `<LocationName>_Ambient` 对话（头顶气泡），与 Agent 聊天无冲突
2. **sayHiTo**：NPC 移动时若附近有其他角色且满足社交条件，调用 `sayHiTo()` 显示打招呼气泡

后者与 Agent 对话机制存在潜在干扰，需评估是否 patch `sayHiTo`。

---

## 7. 对"切断游戏控制"方案的影响摘要

| 控制维度 | 游戏入口 | 切断方式 |
|----------|----------|----------|
| Schedule 自动寻路 | `checkSchedule` → `ignoreScheduleToday` 检查 | `OnDayStarted` 设 `ignoreScheduleToday=true` + 清空 `Schedule` |
| Schedule endOfRoute 动画 | 同上，随 Schedule 一并切断 | 随 Schedule 切断自动生效 |
| 睡觉传送/动画 | `playSleepingAnimation`、`isSleeping` 赋值 | Harmony prefix `NPC.startSleeping` / `NPC.playSleepingAnimation` |
| PathFindController 被游戏覆盖 | `checkSchedule`、`dayupdate`、`returnHomeFromFarmPosition` | `FollowSystem.PumpOnGameTick` 守卫：Idle 时检测 `controller != null` 则 `Halt()` |
| 原版对话框 | `NpcDialoguePatch`（已有）+ `sayHiTo` | 现有 patch 已覆盖点击对话；`sayHiTo` 仅头顶气泡，暂不 patch |
