# npc_clear_debris: 地形杂物清理 + 可开垦检测放宽

日期：2026-06-17 | 状态：已确认

## 问题

`npc_clear_debris` 只扫描 `location.Objects`，树桩和树苗（在 `location.terrainFeatures` 中）永远不会被清理。同时 `FarmlandExtensionPlanner.IsTillable()` 和 `farm_actions` 的 `till` 组无条件排除所有含 Objects/terrainFeatures 的 tile，导致有杂物的地块在「检测可开垦」阶段就被排除。

## 设计

### 1. 杂物分类扩展

新增 `IsTerrainDebris`（与现有 `IsDebris(Object)` 平行），定义 terrainFeature 中的可清理物：

- `Tree` + `stump.Value == true`（树桩） → 可清理
- `Tree` + `growthStage.Value < 5`（树苗） → 可清理
- 其他 terrainFeature（成熟树、HoeDirt、Bush 等） → 不可清理

将 `IsDebris(Object)` 和 `IsTerrainDebris(TerrainFeature)` 提取为可以被多个类共用的静态方法（放 `ClearDebrisHandler` 作为 internal static）。

### 2. ClearDebrisHandler.Execute 目标扫描

在现有 `location.Objects.Pairs` 循环后追加 `location.terrainFeatures.Pairs` 循环：

- 按 `IsTerrainDebris` 过滤
- 受同样约束：bbox / radius / farmland-bbox
- 与 Object 目标合并到统一的 `targets` 列表
- 目标只记录 tile 坐标；`TickClearDebris` 侧自行分辨类型

### 3. TickClearDebris 执行清理

到达目标 tile 后：

1. 先检查 `location.Objects` — 有则 Remove + 掉落物（现有逻辑）
2. 若无 Object，检查 `location.terrainFeatures` — 是 Tree stump/sapling 则 Remove + 掉落物
3. 两者都无 → 已被清理，跳过

掉落物映射：树桩 → `(O)709` Hardwood；树苗 → `(O)388` Wood。

### 4. farm_actions 数据修正

`InspectObjectHandler.BuildFarmActionsResult`：

- **`clear` 组** — 追加 terrainFeature 扫描，树桩/树苗计入 `clear` 组（使用与 ClearDebrisHandler 相同的分类器）
- **`till` 组** — 放宽条件：有杂物 Object 的 tile 不再排除（只排除非杂物如 chest/machine/fence）；有可清理 terrainFeature 的 tile 不再排除。与 `FarmlandExtensionPlanner.IsTillable` 使用相同放宽逻辑

### 5. FarmlandExtensionPlanner.IsTillable 放宽

`IsTillable` 不再无条件排除所有 Objects 和 terrainFeatures：

- Objects：仅排除非可清理杂物（chest、machine、fence、crafted items 等）
- terrainFeatures：仅排除非可清理项（成熟树、HoeDirt、Bush、Flooring 等）

杂物 Object 和 tree stump/sapling → `IsTillable` 返回 `true`。

### 6. TickTillSoil 翻地前清理

到达目标 tile 后、创建 HoeDirt 之前：

1. 检查 tile 上是否有杂物 Object → 有则 Remove + 掉落物
2. 检查 tile 上是否有 terrainFeature 杂物（树桩/树苗） → 有则 Remove + 掉落物
3. 然后正常创建 HoeDirt

### 7. farm_actions `break` 组不变

`break` 组继续只包含成熟树（growthStage >= 5, not tapped）+ 大石头（PSI >= 44）。树苗不在 break 组但已在 clear 组，不再有空隙。

## 影响文件

| 文件 | 改动 |
|------|------|
| `smapi-mod/Behavior/WorldActionHandlers.cs` | `ClearDebrisHandler`: +`IsTerrainDebris`, +terrainFeature 扫描；`InspectObjectHandler`: `clear` 组追加 terrainFeature，`till` 组放宽 |
| `smapi-mod/Movement/FollowSystem.cs` | `TickClearDebris`: Object/TerrainFeature 分叉清理；`TickTillSoil`: 翻地前清杂物 |
| `smapi-mod/Behavior/FarmlandExtensionPlanner.cs` | `IsTillable`: 不排除杂物 Object 和可清理 terrainFeature |

## 非目标

- `npc_break_resource` 行为不变（仍然处理成熟树 + 大石头）
- `npc_wander` / `npc_water_crops` / `npc_harvest_crops` 不变
- Go 侧 MCP 工具定义不变（Input/Output schema 不变）
- ws protocol 不变
