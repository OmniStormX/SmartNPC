# npc_clear_debris 地形杂物清理 + 可开垦检测放宽 — 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 扩大 `npc_clear_debris` 清理范围到 terrainFeatures（树桩、树苗），并放宽 `IsTillable` / `farm_actions` 的可开垦检测使其包含杂物覆盖的 tile。

**Architecture:** 在 `ClearDebrisHandler` 中新增 `IsTerrainDebris` 静态方法并在 Execute 中追加 terrainFeatures 扫描；`TickClearDebris` 分叉处理 Object / TerrainFeature 移除；`FarmlandExtensionPlanner.IsTillable` 和 `farm_actions` 的 `till`/`clear` 组同步放宽；`TickTillSoil` 翻地前自动清理杂物。

**Tech Stack:** C# / .NET 6, Stardew Valley 1.6 SMAPI mod

---

### Task 1: 提取共享杂物分类器，新增 `IsTerrainDebris`

**Files:**
- Modify: `smapi-mod/Behavior/WorldActionHandlers.cs` — `ClearDebrisHandler` 类

- [ ] **Step 1: 将 `IsDebris` 从 private static 改为 internal static，新增 `IsTerrainDebris`**

定位到 `ClearDebrisHandler` 类的 `IsDebris` 方法（约 Line 281），将 `private` 改为 `internal`，并在其后追加 `IsTerrainDebris`：

```csharp
internal static bool IsDebris(StardewValley.Object obj)
{
    if (obj is null) return false;
    return obj.IsWeeds() || obj.IsTwig()
        || (obj.Category == StardewValley.Object.litterCategory)
        || (obj.Name == "Stone" && obj.ParentSheetIndex >= 0);
}

/// <summary>
/// TerrainFeature counterpart of IsDebris: tree stumps (chopped-down trees)
/// and saplings (growthStage &lt; 5) are clearable; mature trees are not
/// (they belong to npc_break_resource).
/// </summary>
internal static bool IsTerrainDebris(StardewValley.TerrainFeatures.TerrainFeature tf)
{
    if (tf is StardewValley.TerrainFeatures.Tree tree)
    {
        if (tree.stump.Value) return true;
        if (tree.growthStage.Value < 5) return true;
    }
    return false;
}
```

同时将 `DebrisDropId` 从 `private static` 改为 `internal static`，并在其后追加 terrain 版本：

```csharp
internal static string DebrisDropId(StardewValley.Object obj)
{
    if (obj.IsTwig())  return "(O)388"; // Wood
    if (obj.IsWeeds()) return "(O)771"; // Mixed Seeds
    return "(O)390";  // Stone
}

/// <summary>Drop id for terrain debris: stump→Hardwood, sapling→Wood.</summary>
internal static string TerrainDebrisDropId(StardewValley.TerrainFeatures.TerrainFeature tf)
{
    if (tf is StardewValley.TerrainFeatures.Tree tree && tree.stump.Value)
        return "(O)709"; // Hardwood
    return "(O)388"; // Wood
}
```

- [ ] **Step 2: 构建验证编译**

```powershell
cd D:\SmartNPC\smapi-mod; dotnet build
```

Expected: 编译通过（`IsDebris` 改为 internal 不影响已有调用者，都在同一 assembly 内）。

---

### Task 2: ClearDebrisHandler.Execute — 追加 terrainFeatures 扫描

**Files:**
- Modify: `smapi-mod/Behavior/WorldActionHandlers.cs` — `ClearDebrisHandler.Execute` 方法

- [ ] **Step 1: 简化 targets 列表类型，移除未使用的 Object 字段**

当前 `targets` 类型为 `List<(Vector2 tile, Object obj)>`，但 `obj` 从未在下游消费（只用于 `IsDebris` 过滤）。改为 `List<Vector2>`，同时更新 sort/lambda：

定位到约 Line 212-233，替换 Object 扫描循环：

```csharp
// 2. Scan for clearable objects, restricted to the farmland
//    bbox when one was found; otherwise the original user
//    region (radius/bbox).
var targets = new List<Microsoft.Xna.Framework.Vector2>();

foreach (var kv in location.Objects.Pairs)
{
    var tile = kv.Key;
    var obj  = kv.Value;
    if (!IsDebris(obj)) continue;
    if (farmlandFound)
    {
        if (tile.X < fx1 || tile.X > fx2 || tile.Y < fy1 || tile.Y > fy2) continue;
    }
    else if (bboxOn)
    {
        if (tile.X < x1 || tile.X > x2 || tile.Y < y1 || tile.Y > y2) continue;
    }
    else
    {
        float dist = Microsoft.Xna.Framework.Vector2.Distance(npcTile, tile);
        if (dist > radius) continue;
    }
    targets.Add(tile);
}
```

- [ ] **Step 2: 追加 terrainFeatures 扫描循环**

在 Object 扫描循环的闭合大括号后、sort 前追加：

```csharp
// 2b. Scan terrainFeatures for tree stumps and saplings.
foreach (var kv in location.terrainFeatures.Pairs)
{
    var tile = kv.Key;
    var tf   = kv.Value;
    if (!IsTerrainDebris(tf)) continue;
    if (farmlandFound)
    {
        if (tile.X < fx1 || tile.X > fx2 || tile.Y < fy1 || tile.Y > fy2) continue;
    }
    else if (bboxOn)
    {
        if (tile.X < x1 || tile.X > x2 || tile.Y < y1 || tile.Y > y2) continue;
    }
    else
    {
        float dist = Microsoft.Xna.Framework.Vector2.Distance(npcTile, tile);
        if (dist > radius) continue;
    }
    targets.Add(tile);
}
```

- [ ] **Step 3: 更新 sort / PathPlanner lambda 适配 List<Vector2>**

替换约 Line 235-260 的 sort + PathPlanner 调用：

```csharp
targets.Sort((a, b) =>
    Microsoft.Xna.Framework.Vector2.Distance(npcTile, a)
        .CompareTo(Microsoft.Xna.Framework.Vector2.Distance(npcTile, b)));

if (targets.Count > effectiveCap) targets = targets.GetRange(0, effectiveCap);

if (targets.Count == 0)
{
    string scope = farmlandFound
        ? $"farmland=({fx1},{fy1})-({fx2},{fy2})"
        : (bboxOn ? $"bbox=({x1},{y1})-({x2},{y2})" : $"radius={radius}");
    Log.Log($"[npc_clear_debris] {npcName}: no debris in {scope}", LogLevel.Info);
    MarkNothingToDo($"no debris in {scope}");
    return;
}

// 3. Plan a near-optimal visit order over the capped target set
var startPt = new Microsoft.Xna.Framework.Point((int)npcTile.X, (int)npcTile.Y);
var tilePoints = targets
    .Select(t => new Microsoft.Xna.Framework.Point((int)t.X, (int)t.Y))
    .ToList();
var ordered = PathPlanner.PlanBy(startPt, tilePoints, p => p);
_follow.StartClearDebris(npcName, ordered, _inventory);
```

> 注意：`PathPlanner.PlanBy` 原来接受 `List<(Vector2 tile, Object obj)>` 和 lambda `t => new Point((int)t.tile.X, (int)t.tile.Y)`。现在改为 `List<Point>` 输入，lambda 简化为 `p => p`。

- [ ] **Step 4: 构建验证编译**

```powershell
cd D:\SmartNPC\smapi-mod; dotnet build
```

Expected: 编译通过。

---

### Task 3: TickClearDebris — 分叉处理 Object / TerrainFeature 清理

**Files:**
- Modify: `smapi-mod/Movement/FollowSystem.cs` — `TickClearDebris` 方法（约 Line 1280-1384）

- [ ] **Step 1: 重写到达后的清理逻辑**

定位到 `TickClearDebris` 中 `if (dist <= 1.5f && pathDone)` 块（约 Line 1301-1338）。将整个 Object 销毁 + 掉落物采集块替换为分叉逻辑：

```csharp
if (dist <= 1.5f && pathDone)
{
    // Destroy debris — check Objects first, then terrainFeatures.
    bool cleared = false;
    string dropId = "(O)390"; // default: Stone

    if (location.Objects.TryGetValue(targetV2, out var obj) && obj != null
        && ClearDebrisHandler.IsDebris(obj))
    {
        dropId = ClearDebrisHandler.DebrisDropId(obj);
        location.Objects.Remove(targetV2);
        cleared = true;
    }
    else if (location.terrainFeatures.TryGetValue(targetV2, out var tf) && tf != null
        && ClearDebrisHandler.IsTerrainDebris(tf))
    {
        dropId = ClearDebrisHandler.TerrainDebrisDropId(tf);
        location.terrainFeatures.Remove(targetV2);
        cleared = true;
    }

    if (cleared)
    {
        st.DebrisInventory.Add(npcName, dropId, 1);
        npc.doEmote(16); // "!"
        _log.Log(
            $"[FollowSystem/ClearDebris] {npcName}: cleared at ({target.X},{target.Y}) → {dropId}",
            LogLevel.Debug);
    }
    else
    {
        _log.Log(
            $"[FollowSystem/ClearDebris] {npcName}: tile ({target.X},{target.Y}) " +
            $"already cleared, skipping",
            LogLevel.Debug);
    }

    // Advance to next target.
    if (st.DebrisQueue.Count == 0)
    {
        _log.Log($"[FollowSystem/ClearDebris] {npcName}: all targets done → Idle", LogLevel.Debug);
        st.Mode = NpcBehaviorMode.Idle;
        return;
    }

    st.DebrisTarget  = st.DebrisQueue.Dequeue();
    st.DebrisPathed  = false;
    st.LastPathTick  = 0;
    return;
}
```

- [ ] **Step 2: 构建验证编译**

```powershell
cd D:\SmartNPC\smapi-mod; dotnet build
```

Expected: 编译通过。

---

### Task 4: FarmlandExtensionPlanner.IsTillable — 放宽候选条件

**Files:**
- Modify: `smapi-mod/Behavior/FarmlandExtensionPlanner.cs` — `IsTillable` 方法（Line 207-215）

- [ ] **Step 1: 重写 IsTillable，允许杂物 tile 通过**

```csharp
/// <summary>
/// Mirrors TillSoilHandler.Execute's per-tile precondition, with one
/// relaxation: tiles that hold clearable debris (weeds, twigs, small stones,
/// tree stumps, saplings) are allowed — the debris will be removed before
/// tilling. Non-debris objects (chests, machines, fences) and non-clearable
/// terrain features (mature trees, bushes, flooring) still block.
/// </summary>
private static bool IsTillable(GameLocation location, int tx, int ty)
{
    var v = new Microsoft.Xna.Framework.Vector2(tx, ty);

    // Objects: only block if NOT clearable debris.
    if (location.Objects.TryGetValue(v, out var obj) && obj != null)
    {
        if (!ClearDebrisHandler.IsDebris(obj)) return false;
    }

    // TerrainFeatures: only block if NOT clearable debris.
    if (location.terrainFeatures.TryGetValue(v, out var tf) && tf != null)
    {
        if (!ClearDebrisHandler.IsTerrainDebris(tf)) return false;
    }

    if (!location.isTilePassable(new xTile.Dimensions.Location(tx, ty), Game1.viewport)) return false;
    if (location.doesTileHaveProperty(tx, ty, "Diggable", "Back") != "T") return false;
    return true;
}
```

> 移除了未使用的 `using StardewValley.TerrainFeatures;`（若只在此方法用了 HoeDirt 检查则可以精简，但文件顶部已有 using 不碍事）。

- [ ] **Step 2: 构建验证编译**

```powershell
cd D:\SmartNPC\smapi-mod; dotnet build
```

Expected: 编译通过。

---

### Task 5: farm_actions — clear 组追加 terrainFeature，till 组放宽

**Files:**
- Modify: `smapi-mod/Behavior/WorldActionHandlers.cs` — `InspectObjectHandler.BuildFarmActionsResult` 方法

- [ ] **Step 1: 新增 `IsDebrisTerrainFeature` 本地辅助方法**

在 `InspectObjectHandler` 类中，`IsDebrisObj` 方法附近（约 Line 1572）追加：

```csharp
/// <summary>
/// TerrainFeature counterpart of IsDebrisObj: tree stumps and saplings
/// are clearable debris and belong in the `clear` action group.
/// </summary>
private static bool IsDebrisTerrainFeature(StardewValley.TerrainFeatures.TerrainFeature tf)
{
    if (tf is StardewValley.TerrainFeatures.Tree tree)
    {
        if (tree.stump.Value) return true;
        if (tree.growthStage.Value < 5) return true;
    }
    return false;
}
```

- [ ] **Step 2: 在 scan loop 中追加 terrainFeature debris → clear 组**

定位到 `BuildFarmActionsResult` 中处理 Objects 的 `if (location.Objects.TryGetValue(...))` 块（约 Line 1463-1473）。在 `// Objects: clear (debris) / forage (spawn) / break (large stone)` 注释块之后、闭合大括号之前，追加 terrainFeature 检查：

在 Objects 块结束后追加（约 Line 1473 之后）：

```csharp
                    // ── TerrainFeature debris: clear group ────────
                    // Tree stumps and saplings are clearable; contribute
                    // to the `clear` group same as small Objects.
                    if (location.terrainFeatures.TryGetValue(tileV2, out var tf2) && tf2 != null)
                    {
                        if (IsDebrisTerrainFeature(tf2))
                            debrisCandidates.Add((tx, ty));
                    }
```

- [ ] **Step 3: 放宽 till 组条件**

定位到 till 组检查（约 Line 1476-1482），替换为：

```csharp
                    // ── till: empty or clearable-debris, passable, Diggable=T ─
                    // Tiles with clearable debris (weeds, twigs, saplings,
                    // stumps) are counted as tillable — the debris will be
                    // removed before tilling. Non-debris objects/terrain still
                    // block.
                    bool blockedByObject = location.Objects.TryGetValue(tileV2, out var tillObj)
                        && tillObj != null && !IsDebrisObj(tillObj);
                    bool blockedByTerrain = location.terrainFeatures.TryGetValue(tileV2, out var tillTf)
                        && tillTf != null && !IsDebrisTerrainFeature(tillTf);
                    if (!blockedByObject && !blockedByTerrain
                        && location.isTilePassable(new xTile.Dimensions.Location(tx, ty), Game1.viewport)
                        && location.doesTileHaveProperty(tx, ty, "Diggable", "Back") == "T")
                    {
                        Hit("till", tx, ty);
                    }
```

- [ ] **Step 4: 构建验证编译**

```powershell
cd D:\SmartNPC\smapi-mod; dotnet build
```

Expected: 编译通过。

---

### Task 6: TickTillSoil — 翻地前清杂物

**Files:**
- Modify: `smapi-mod/Movement/FollowSystem.cs` — `TickTillSoil` 方法（约 Line 1752-1866）

- [ ] **Step 1: 在翻地前插入杂物清理逻辑**

定位到 `TickTillSoil` 中 `if (dist <= 1.5f && pathDone)` 块（约 Line 1773）。在当前的 `if (!occupied)` 检查之前插入杂物清理：

替换约 Line 1773-1801 的整段逻辑：

```csharp
            if (dist <= 1.5f && pathDone)
            {
                // ── Clear debris before tilling ──────────────────────
                // If the tile has clearable debris on it, remove it now
                // so the HoeDirt can be placed. Drops are collected into
                // the NPC's inventory if available; otherwise lost.
                bool debrisCleared = false;

                if (location.Objects.TryGetValue(targetV2, out var obj) && obj != null
                    && ClearDebrisHandler.IsDebris(obj))
                {
                    string dropId = ClearDebrisHandler.DebrisDropId(obj);
                    location.Objects.Remove(targetV2);
                    debrisCleared = true;
                    _log.Log(
                        $"[FollowSystem/TillSoil] {npcName}: cleared debris object {obj.Name} at ({target.X},{target.Y}) → {dropId}",
                        LogLevel.Debug);
                }
                else if (location.terrainFeatures.TryGetValue(targetV2, out var tf) && tf != null
                    && ClearDebrisHandler.IsTerrainDebris(tf))
                {
                    string dropId = ClearDebrisHandler.TerrainDebrisDropId(tf);
                    location.terrainFeatures.Remove(targetV2);
                    debrisCleared = true;
                    _log.Log(
                        $"[FollowSystem/TillSoil] {npcName}: cleared terrain debris at ({target.X},{target.Y}) → {dropId}",
                        LogLevel.Debug);
                }

                // Re-check occupancy after potential debris removal.
                bool occupied = location.Objects.ContainsKey(targetV2)
                             || location.terrainFeatures.ContainsKey(targetV2);

                // ── Till phase ──────────────────────────────────────────
                if (!occupied)
                {
                    try
                    {
                        int frame = npc.Sprite.currentFrame;
                        npc.faceDirection(2); // face down
                        location.terrainFeatures.Add(targetV2, new HoeDirt());
                        st.TillCount++;

                        _log.Log(
                            $"[FollowSystem/TillSoil] {npcName}: tilled ({target.X},{target.Y}) frame={frame}" +
                            (debrisCleared ? " (debris cleared first)" : ""),
                            LogLevel.Debug);
                    }
                    catch (Exception ex)
                    {
                        _log.Log(
                            $"[FollowSystem/TillSoil] {npcName}: till ({target.X},{target.Y}) failed: {ex.Message}",
                            LogLevel.Warn);
                    }
                }
                else
                {
                    _log.Log(
                        $"[FollowSystem/TillSoil] {npcName}: tile ({target.X},{target.Y}) occupied by non-debris, skipping",
                        LogLevel.Debug);
                }

                // Advance to next target.
                if (st.TillQueue.Count == 0)
                {
                    _log.Log(
                        $"[FollowSystem/TillSoil] {npcName}: done, tilled={st.TillCount} → Idle",
                        LogLevel.Info);
                    npc.doEmote(32); // happy
                    st.Mode = NpcBehaviorMode.Idle;
                    return;
                }

                st.TillTarget  = st.TillQueue.Dequeue();
                st.TillPathed  = false;
                st.LastPathTick = 0;
                return;
            }
```

- [ ] **Step 2: 构建验证编译**

```powershell
cd D:\SmartNPC\smapi-mod; dotnet build
```

Expected: 编译通过。

---

### Task 7: 编译 + 运行 CI 验证

**Files:** 无新建，验证所有改动

- [ ] **Step 1: 完整构建 smapi-mod**

```powershell
cd D:\SmartNPC\smapi-mod; dotnet build
```

Expected: 编译成功，无 error/warning。

- [ ] **Step 2: 运行 task ci-fast**

```powershell
C:\Users\synchen\go\bin\task.exe ci-fast
```

Expected: profiles:verify + lint + test 全部通过。

- [ ] **Step 3: 检查 PathPlanner.PlanBy 签名兼容性**

确认 `PathPlanner.PlanBy` 接受 `List<Point>` 作为输入。若当前签名为 `PlanBy<T>(Point start, List<T> items, Func<T, Point> selector)`，则 Task 2 Step 3 的调用 `PlanBy(startPt, tilePoints, p => p)` 兼容。

验证方式：
```powershell
cd D:\SmartNPC; grep -n "public.*PlanBy" smapi-mod/Behavior/PathPlanner.cs
```

- [ ] **Step 4: Commit**

```bash
git add smapi-mod/Behavior/WorldActionHandlers.cs smapi-mod/Movement/FollowSystem.cs smapi-mod/Behavior/FarmlandExtensionPlanner.cs
git commit -m "fix(mod): expand clear_debris to terrain features and relax tillable detection"
```
