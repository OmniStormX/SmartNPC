# npc_fill_gaps: 农田形状修正 — 间隙填充

日期：2026-06-17 | 状态：已确认

## 问题

多次耕种、部分收获后，农田 bbox 内部形成不规则空隙（空地、杂物、树桩、树）。`FarmlandExtensionPlanner` 只向外扩展新 patch，不向内填充。agent 看不到这些 gap，农田逐渐碎片化。

## 设计

### 1. `farm_actions` 新增 `fill` + `fill_blocked` 组

在 `InspectObjectHandler.BuildFarmActionsResult` 中，扫描循环对 farmland bbox 内的 tile 判定：

| tile 状态 | 归类 |
|----------|------|
| 已有 HoeDirt | 跳过 |
| 空地（无 Object 无 terrainFeature）、passable、Diggable=T | `fill` |
| 有可清杂物 Object（`IsDebris`=true）或可清 terrainFeature（`IsTerrainDebris`=true）或成熟非 tapped Tree | `fill_blocked` |
| 永久障碍（建筑/栅栏/箱子/灌木/tapped tree/水/非 diggable） | 不纳入任何组 |

每个组输出 `count` + `bbox`（gap tile 的包围盒）。

### 2. `npc_fill_gaps` 工具

新 MCP 工具，只翻耕空地。不清杂物不砍树——agent 必须先用 `clear_debris` / `break_resource` 清掉 `fill_blocked` 覆盖的区域。

**Go 侧**（`npc_world_action.go`）：`NpcFillGapsInput`（npc + bbox）+ `NpcFillGapsOutput`（ok + filled + skipped + message）

**C# 侧**（`WorldActionHandlers.cs`）：`FillGapsHandler : NpcActionHandlerBase`
- `ActionName = "npc_fill_gaps"`
- Execute：扫描 bbox → 过滤出空地（无 HoeDirt、无杂物、无树、passable、Diggable=T）→ 无目标时 `MarkNothingToDo` → 有目标时调 `StartFillGaps`

**FollowSystem 侧**（`FollowSystem.cs`）：
- `NpcBehaviorState` 新增 `FillGapsQueue` / `FillGapsTarget` / `FillGapsPathed` / `FillGapsCount`
- `StartFillGaps(npcName, targets)` — 队列化填充目标
- `TickFillGaps` — 走到 tile → 检查仍为空 → 创建 HoeDirt（不清杂物不砍树，agent 已预处理）
- `NpcBehaviorMode.FillGaps` 枚举值

### 3. `farm_maintenance` 新增示例 C — 农田形状修正

```
inspect(what="farm_actions")
  → fill_blocked.count > 0 或 fill.count > 0

[如果 fill_blocked.count > 0]
  clear_debris(fill_blocked.bbox)       → 清杂物
  break_resource(fill_blocked.bbox)     → 砍树（如有）

[然后]
  fill_gaps(fill.bbox)                  → 填平空隙
  water_crops(fill.bbox)                → 新土浇水
  [可选] plant_seeds(fill.bbox)         → 补种
```

### 非目标

- `npc_fill_gaps` 不清杂物不砍树
- 不扩展农田（那是 `npc_till_soil` 的职责）
- 不处理永久障碍物

## 影响文件

| 文件 | 改动 |
|------|------|
| `smapi-mod/Behavior/WorldActionHandlers.cs` | `InspectObjectHandler`: `fill`/`fill_blocked` 组；`FillGapsHandler`: 新 Handler |
| `smapi-mod/Movement/FollowSystem.cs` | `NpcBehaviorState` 加 FillGaps 字段；`StartFillGaps`/`TickFillGaps`；新的 `NpcBehaviorMode.FillGaps` |
| `smapi-mod/ModEntry.cs` | 注册 `FillGapsHandler` |
| `smartnpc-mcp/adapters/stardew/tools/npc_world_action.go` | Go Input/Output + 注册 `npc_fill_gaps` |
| `smartnpc-mcp/adapters/stardew/bridge/protocol.go` | 新增 `ActionNpcFillGaps` 常量 |
| `hermes/profiles/_master/skills/smartnpc/smartnpc-farm-maintenance/SKILL.md` | 工具箱表 + 示例 C |
