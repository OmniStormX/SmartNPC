# npc_deposit_items Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** NPC 走到指定（或自动寻找的最近）箱子旁，把背包（`NpcInventory`）中指定（或全部）物品放入，背包中对应物品减少。

**Architecture:** 复用 `ClearDebris` 的模式——`DepositItemsHandler.Execute` 扫描参数、找箱子、委托给 `FollowSystem.StartDepositItems`；`FollowSystem.TickDepositItems` 每帧驱动寻路→到达→放入→Idle。Go 侧修改 `NpcDepositItemsInput/Output` 结构体，增加 `auto_find` 和 `item_ids` 字段。

**Tech Stack:** C# .NET 6 / SMAPI 4 / MonoGame / Go 1.25

---

## 文件清单

### 修改
- `smapi-mod/Movement/FollowSystem.cs` — 新增 `DepositItems` mode、state 字段、`StartDepositItems`、`TickDepositItems`
- `smapi-mod/Behavior/WorldActionHandlers.cs` — `DepositItemsHandler` 真实实现（注入 `NpcInventory` + `FollowSystem`）
- `smapi-mod/ModEntry.cs` — `DepositItemsHandler` 构造传参
- `smapi-mod/Debug/DebugCommands.cs` — 新增 `smartnpc_deposit_items` debug 命令
- `smartnpc-mcp/adapters/stardew/tools/npc_world_action.go` — 修改 `NpcDepositItemsInput/Output`

---

## Task 1: FollowSystem — DepositItems 模式状态字段

**Files:**
- Modify: `smapi-mod/Movement/FollowSystem.cs`

- [ ] **Step 1: 在 `NpcBehaviorMode` 枚举中新增 `DepositItems`**

找到：
```csharp
    internal enum NpcBehaviorMode
    {
        Idle,
        Summoning,
        Following,
        Leading,
        Wander,
        ClearDebris,
    }
```
替换为：
```csharp
    internal enum NpcBehaviorMode
    {
        Idle,
        Summoning,
        Following,
        Leading,
        Wander,
        ClearDebris,
        DepositItems,
    }
```

- [ ] **Step 2: 在 `NpcBehaviorState` 中新增 `DepositItems` 字段**

找到：
```csharp
        // ClearDebris: ordered queue of debris tiles to visit and destroy.
        public Queue<Point>?  DebrisQueue     { get; set; }
        public NpcInventory?  DebrisInventory { get; set; }
        public Point          DebrisTarget    { get; set; }
        public bool           DebrisPathed    { get; set; }
```
在它之后插入：
```csharp
        // DepositItems: walk to a chest and deposit carried items.
        public Point               DepositChestTile { get; set; }
        public string?             DepositChestMap  { get; set; }
        public HashSet<string>?    DepositItemIds   { get; set; }  // null = all
        public NpcInventory?       DepositInventory { get; set; }
        public bool                DepositPathed    { get; set; }
        public int                 DepositedCount   { get; set; }
```

- [ ] **Step 3: 编译验证**

```cmd
cd D:/SmartNPC && "/c/Users/synchen/go/bin/task.exe" mod:build
```
期望：0 errors

- [ ] **Step 4: commit**

```bash
cd D:/SmartNPC && git add smapi-mod/Movement/FollowSystem.cs && git commit -m "feat(mod): add DepositItems mode and state fields to FollowSystem"
```

---

## Task 2: FollowSystem — StartDepositItems + TickDepositItems

**Files:**
- Modify: `smapi-mod/Movement/FollowSystem.cs`

- [ ] **Step 1: 在 switch 中添加 `DepositItems` case**

找到：
```csharp
                        case NpcBehaviorMode.ClearDebris:
                            this.TickClearDebris(npc, name, st);
                            break;
```
在它之后插入：
```csharp
                        case NpcBehaviorMode.DepositItems:
                            this.TickDepositItems(npc, name, st);
                            break;
```

- [ ] **Step 2: 在 `StartClearDebris` 方法之后添加 `StartDepositItems`**

找到 `StartClearDebris` 末尾的 `}`，在其后插入：

```csharp
        // ── StartDepositItems ─────────────────────────────────────────────

        /// <summary>
        /// Walk NPC to the specified chest (or nearest chest if autoFind=true),
        /// then deposit items from <paramref name="inventory"/> filtered by
        /// <paramref name="itemIds"/> (null = all items).
        /// Returns false immediately if no chest is found.
        /// </summary>
        public bool StartDepositItems(
            string npcName,
            NPC npc,
            Point chestTile,
            bool autoFind,
            string? chestMap,
            IEnumerable<string>? itemIds,
            NpcInventory inventory)
        {
            var location = string.IsNullOrEmpty(chestMap)
                ? npc.currentLocation
                : Game1.getLocationFromName(chestMap);

            if (location is null)
            {
                _log.Log($"[FollowSystem/DepositItems] {npcName}: map '{chestMap}' not found", LogLevel.Warn);
                return false;
            }

            // Auto-find nearest chest.
            if (autoFind)
            {
                Point? nearest = FindNearestChest(npc, location);
                if (nearest is null)
                {
                    _log.Log($"[FollowSystem/DepositItems] {npcName}: no chest found in {location.Name}", LogLevel.Warn);
                    return false;
                }
                chestTile = nearest.Value;
            }
            else
            {
                // Validate that the specified tile contains a Chest.
                if (!location.Objects.TryGetValue(new Vector2(chestTile.X, chestTile.Y), out var obj)
                    || obj is not StardewValley.Objects.Chest)
                {
                    _log.Log($"[FollowSystem/DepositItems] {npcName}: no chest at ({chestTile.X},{chestTile.Y})", LogLevel.Warn);
                    return false;
                }
            }

            var st = this.GetOrCreate(npcName);
            st.DepositChestTile = chestTile;
            st.DepositChestMap  = location.NameOrUniqueName ?? location.Name;
            st.DepositItemIds   = itemIds is null ? null : new HashSet<string>(itemIds, StringComparer.OrdinalIgnoreCase);
            st.DepositInventory = inventory;
            st.DepositPathed    = false;
            st.DepositedCount   = 0;
            st.Mode             = NpcBehaviorMode.DepositItems;
            st.LastPathTick     = 0;

            _log.Log(
                $"[FollowSystem/DepositItems] {npcName}: started → chest=({chestTile.X},{chestTile.Y}) " +
                $"map={st.DepositChestMap} filter={st.DepositItemIds?.Count.ToString() ?? "all"}",
                LogLevel.Debug);
            return true;
        }

        /// <summary>Scan <paramref name="location"/> for the Chest nearest to <paramref name="npc"/>.</summary>
        private static Point? FindNearestChest(NPC npc, GameLocation location)
        {
            Point? best = null;
            float bestDist = float.MaxValue;
            foreach (var kv in location.Objects.Pairs)
            {
                if (kv.Value is not StardewValley.Objects.Chest) continue;
                float d = Vector2.Distance(npc.Tile, kv.Key);
                if (d < bestDist) { bestDist = d; best = new Point((int)kv.Key.X, (int)kv.Key.Y); }
            }
            return best;
        }
```

- [ ] **Step 3: 添加 `TickDepositItems` 方法**

在 `TickClearDebris` 方法之后、`// ── helpers` 注释之前插入：

```csharp
        private void TickDepositItems(NPC npc, string npcName, NpcBehaviorState st)
        {
            if (st.DepositInventory is null)
            {
                st.Mode = NpcBehaviorMode.Idle;
                return;
            }

            // Resolve the target location.
            var location = string.IsNullOrEmpty(st.DepositChestMap)
                ? npc.currentLocation
                : Game1.getLocationFromName(st.DepositChestMap);

            if (location is null) { st.Mode = NpcBehaviorMode.Idle; return; }

            var chestV2 = new Vector2(st.DepositChestTile.X, st.DepositChestTile.Y);

            // Check chest still exists.
            if (!location.Objects.TryGetValue(chestV2, out var chestObj)
                || chestObj is not StardewValley.Objects.Chest chest)
            {
                _log.Log(
                    $"[FollowSystem/DepositItems] {npcName}: chest at ({st.DepositChestTile.X}," +
                    $"{st.DepositChestTile.Y}) gone → Idle (deposited={st.DepositedCount})",
                    LogLevel.Debug);
                st.Mode = NpcBehaviorMode.Idle;
                return;
            }

            float dist = Vector2.Distance(npc.Tile,
                new Vector2(st.DepositChestTile.X, st.DepositChestTile.Y));
            bool pathDone = npc.controller == null
                            || npc.controller.pathToEndPoint == null
                            || npc.controller.pathToEndPoint.Count == 0;

            if (dist <= 1.5f && pathDone)
            {
                // ── Deposit phase ──────────────────────────────────────
                var items = st.DepositInventory.GetItems(npcName).ToList();
                if (st.DepositItemIds is not null)
                    items = items.Where(s => st.DepositItemIds.Contains(s.ItemId)).ToList();

                if (items.Count == 0)
                {
                    _log.Log($"[FollowSystem/DepositItems] {npcName}: nothing to deposit → Idle", LogLevel.Debug);
                    npc.doEmote(32); // happy
                    st.Mode = NpcBehaviorMode.Idle;
                    return;
                }

                foreach (var slot in items)
                {
                    StardewValley.Item? item;
                    try { item = StardewValley.ItemRegistry.Create(slot.ItemId, slot.Count, slot.Quality); }
                    catch (Exception ex)
                    {
                        _log.Log($"[FollowSystem/DepositItems] ItemRegistry.Create({slot.ItemId}) failed: {ex.Message}", LogLevel.Warn);
                        continue;
                    }

                    var leftover = chest.addItem(item);
                    int placed = slot.Count - (leftover?.Stack ?? 0);
                    if (placed > 0)
                    {
                        st.DepositInventory.Take(npcName, slot.ItemId, placed);
                        st.DepositedCount += placed;
                    }
                    _log.Log(
                        $"[FollowSystem/DepositItems] {npcName}: deposited {placed}/{slot.Count}× {slot.ItemId}",
                        LogLevel.Debug);
                }

                npc.doEmote(32); // happy
                _log.Log(
                    $"[FollowSystem/DepositItems] {npcName}: done, total deposited={st.DepositedCount} → Idle",
                    LogLevel.Info);
                st.Mode = NpcBehaviorMode.Idle;
                return;
            }

            // ── Pathing phase ──────────────────────────────────────────
            if (!st.DepositPathed || npc.controller == null)
            {
                Point ct = st.DepositChestTile;
                // Try adjacent tiles: below, above, right, left.
                Point[] candidates = {
                    new(ct.X, ct.Y + 1),
                    new(ct.X, ct.Y - 1),
                    new(ct.X + 1, ct.Y),
                    new(ct.X - 1, ct.Y),
                };
                bool ok = false;
                foreach (var adj in candidates)
                {
                    if (this.TryStartPath(npc, location, adj)) { ok = true; break; }
                }
                st.DepositPathed = ok;
                st.LastPathTick  = _tickCounter > 0 ? _tickCounter : 1;
                _log.Log(
                    $"[FollowSystem/DepositItems] {npcName}: pathing to chest ({ct.X},{ct.Y}) ok={ok}",
                    LogLevel.Debug);
            }
        }
```

- [ ] **Step 4: 确认 `using System.Linq;` 已存在于文件顶部**

读取 `FollowSystem.cs` 前 15 行，如果没有 `using System.Linq;` 则在 `using System.Collections.Generic;` 之后加一行。

- [ ] **Step 5: 编译验证**

```cmd
cd D:/SmartNPC && "/c/Users/synchen/go/bin/task.exe" mod:build
```
期望：0 errors

- [ ] **Step 6: commit**

```bash
cd D:/SmartNPC && git add smapi-mod/Movement/FollowSystem.cs && git commit -m "feat(mod): add StartDepositItems and TickDepositItems to FollowSystem"
```

---

## Task 3: DepositItemsHandler 真实实现

**Files:**
- Modify: `smapi-mod/Behavior/WorldActionHandlers.cs`
- Modify: `smapi-mod/ModEntry.cs`

- [ ] **Step 1: 替换 `WorldActionHandlers.cs` 中的 `DepositItemsHandler` stub**

找到：
```csharp
    internal sealed class DepositItemsHandler : NpcActionHandlerBase
    {
        protected override string ActionName => "npc_deposit_items";
        public DepositItemsHandler(IMonitor log, Func<bool> showBubble) : base(log, showBubble) { }
        // TODO: find nearby chest and deposit held items
    }
```
替换为：
```csharp
    internal sealed class DepositItemsHandler : NpcActionHandlerBase
    {
        private readonly NpcInventory _inventory;
        private readonly FollowSystem _follow;

        protected override string ActionName => "npc_deposit_items";

        public DepositItemsHandler(IMonitor log, Func<bool> showBubble, NpcInventory inventory, FollowSystem follow)
            : base(log, showBubble)
        {
            _inventory = inventory;
            _follow    = follow;
        }

        /// <summary>Public entry for debug commands running on the game thread.</summary>
        public void ExecuteDebug(NPC npc, string npcName, JsonElement @params)
            => Execute(npc, npcName, @params);

        protected override string ResolveBubble(JsonElement @params) => "[deposit]";

        protected override void Execute(NPC npc, string npcName, JsonElement @params)
        {
            // Parse chest coordinates.
            bool autoFind = true;
            int  chestX   = 0;
            int  chestY   = 0;
            string? map   = null;
            List<string>? itemIds = null;

            if (@params.ValueKind == JsonValueKind.Object)
            {
                if (@params.TryGetProperty("auto_find", out var af) && af.ValueKind == JsonValueKind.False)
                    autoFind = false;
                if (@params.TryGetProperty("chest_x", out var cx)) cx.TryGetInt32(out chestX);
                if (@params.TryGetProperty("chest_y", out var cy)) cy.TryGetInt32(out chestY);
                if (@params.TryGetProperty("map", out var mp) && mp.ValueKind == JsonValueKind.String)
                    map = mp.GetString();
                if (@params.TryGetProperty("item_ids", out var ids) && ids.ValueKind == JsonValueKind.Array)
                {
                    itemIds = new List<string>();
                    foreach (var el in ids.EnumerateArray())
                        if (el.ValueKind == JsonValueKind.String && el.GetString() is string s)
                            itemIds.Add(s);
                }
            }

            // If chest_x and chest_y are both 0 (or unset), treat as auto_find.
            if (chestX == 0 && chestY == 0) autoFind = true;

            // Validate: background is empty?
            var items = _inventory.GetItems(npcName);
            if (itemIds != null)
                items = items.Where(s => itemIds.Contains(s.ItemId, StringComparer.OrdinalIgnoreCase)).ToArray();
            if (items.Count == 0)
            {
                Log.Log($"[npc_deposit_items] {npcName}: backpack empty (or filter matched nothing), nothing to deposit", LogLevel.Info);
                return;
            }

            bool started = _follow.StartDepositItems(
                npcName, npc,
                new Microsoft.Xna.Framework.Point(chestX, chestY),
                autoFind, map,
                itemIds,
                _inventory);

            if (!started)
                Log.Log($"[npc_deposit_items] {npcName}: could not start (no chest found?)", LogLevel.Warn);
            else
                Log.Log($"[npc_deposit_items] {npcName}: queued deposit, autoFind={autoFind} chest=({chestX},{chestY})", LogLevel.Info);
        }
    }
```

- [ ] **Step 2: 确认文件顶部有 `using System.Linq;`**

读取文件前 12 行，没有则在 `using System.Collections.Generic;` 后加。

- [ ] **Step 3: 修改 `ModEntry.cs` 中 `DepositItemsHandler` 的构造调用**

找到：
```csharp
                    new DepositItemsHandler(this.Monitor, showBubble),
```
替换为：
```csharp
                    new DepositItemsHandler(this.Monitor, showBubble, _npcInventory, _follow),
```

- [ ] **Step 4: 编译验证**

```cmd
cd D:/SmartNPC && "/c/Users/synchen/go/bin/task.exe" mod:build
```
期望：0 errors

- [ ] **Step 5: commit**

```bash
cd D:/SmartNPC && git add smapi-mod/Behavior/WorldActionHandlers.cs smapi-mod/ModEntry.cs && git commit -m "feat(mod): implement DepositItemsHandler with walk-to-chest and deposit logic"
```

---

## Task 4: Go MCP 工具更新

**Files:**
- Modify: `smartnpc-mcp/adapters/stardew/tools/npc_world_action.go`

- [ ] **Step 1: 修改 `NpcDepositItemsInput` 和 `NpcDepositItemsOutput`**

找到：
```go
type NpcDepositItemsInput struct {
	NPC    string `json:"npc"              jsonschema:"NPC internal name"`
	ChestX int    `json:"chest_x"          jsonschema:"chest tile X"`
	ChestY int    `json:"chest_y"          jsonschema:"chest tile Y"`
	Map    string `json:"map,omitempty"    jsonschema:"map (default: NPC's current map)"`
}

type NpcDepositItemsOutput struct {
	OK        bool   `json:"ok"                  jsonschema:"true if accepted"`
	NPC       string `json:"npc"                 jsonschema:"echo"`
	Deposited int    `json:"deposited,omitempty" jsonschema:"items deposited"`
	Message   string `json:"message,omitempty"   jsonschema:"status"`
}
```
替换为：
```go
type NpcDepositItemsInput struct {
	NPC      string   `json:"npc"                jsonschema:"NPC internal name"`
	ChestX   int      `json:"chest_x,omitempty"  jsonschema:"chest tile X; ignored when auto_find=true"`
	ChestY   int      `json:"chest_y,omitempty"  jsonschema:"chest tile Y; ignored when auto_find=true"`
	AutoFind bool     `json:"auto_find,omitempty" jsonschema:"true = ignore coordinates and walk to nearest chest"`
	Map      string   `json:"map,omitempty"      jsonschema:"map name (default: NPC current map)"`
	ItemIds  []string `json:"item_ids,omitempty" jsonschema:"qualified item ids to deposit; omit to deposit everything"`
}

type NpcDepositItemsOutput struct {
	OK        bool   `json:"ok"                  jsonschema:"true if accepted"`
	NPC       string `json:"npc"                 jsonschema:"echo"`
	Deposited int    `json:"deposited,omitempty" jsonschema:"total items actually deposited"`
	ChestX    int    `json:"chest_x,omitempty"   jsonschema:"actual chest tile X used"`
	ChestY    int    `json:"chest_y,omitempty"   jsonschema:"actual chest tile Y used"`
	Message   string `json:"message,omitempty"   jsonschema:"status"`
}
```

- [ ] **Step 2: 更新工具 Description**

找到：
```go
	mcp.AddTool(s, &mcp.Tool{
		Name: "npc_deposit_items",
		Description: "NPC walks to a chest and deposits all carried items.\n\n" +
			"When to call: after harvesting/foraging, NPC puts items away.\n\n" +
			"Side-effect: WRITE (modifies chest contents, clears NPC backpack).",
```
替换为：
```go
	mcp.AddTool(s, &mcp.Tool{
		Name: "npc_deposit_items",
		Description: "NPC walks to a chest and deposits carried items from their backpack.\n\n" +
			"When to call: after npc_clear_debris / npc_forage_collect / npc_harvest_crops, " +
			"to transfer collected items into storage.\n\n" +
			"Chest selection: set auto_find=true to automatically walk to the nearest chest " +
			"(ignores chest_x/chest_y). Or specify chest_x+chest_y for a specific chest.\n\n" +
			"Item filter: set item_ids to deposit only specific items (e.g. [\"(O)390\"]); " +
			"omit to deposit everything in backpack.\n\n" +
			"Side-effect: WRITE (adds items to chest, removes from NPC backpack). " +
			"If chest is full, remaining items stay in NPC backpack.",
```

- [ ] **Step 3: Go 构建验证**

```cmd
cd D:/SmartNPC && "/c/Users/synchen/go/bin/task.exe" mcp:build
```
期望：0 errors

- [ ] **Step 4: commit**

```bash
cd D:/SmartNPC && git add smartnpc-mcp/adapters/stardew/tools/npc_world_action.go && git commit -m "feat(mcp): update npc_deposit_items with auto_find and item_ids support"
```

---

## Task 5: Debug 命令 `smartnpc_deposit_items`

**Files:**
- Modify: `smapi-mod/Debug/DebugCommands.cs`

- [ ] **Step 1: 添加常量**

找到：
```csharp
        private const string CmdClearDebris = "smartnpc_clear_debris";
```
在其后插入：
```csharp
        private const string CmdDeposit     = "smartnpc_deposit_items";
```

- [ ] **Step 2: 修改 `Register` 方法签名，加 `NpcInventory` 参数（已有）并注册新命令**

`Register` 方法签名已有 `NpcInventory inventory` 和 `FollowSystem follow`。在 `CmdClearDebris` 注册之后加：

```csharp
            commands.Add(
                name: CmdDeposit,
                documentation:
                    "Force an NPC to walk to the nearest chest and deposit backpack items.\n" +
                    $"Usage:\n" +
                    $"  {CmdDeposit} <NpcName>                      auto-find nearest chest, deposit all\n" +
                    $"  {CmdDeposit} <NpcName> <chestX> <chestY>   deposit to specific chest\n" +
                    $"  {CmdDeposit} <NpcName> auto (O)390 (O)388  auto-find, filter by item ids",
                callback: (_, args) => HandleDeposit(args, log, inventory, follow));
```

- [ ] **Step 3: 更新 log.Log 注册列表**

找到：
```csharp
            log.Log($"[DebugCommands] registered: {CmdFriendship}, {CmdDebug}, {CmdTeleport}, {CmdProactive}, {CmdStatus}, {CmdTick}, {CmdGoto}, {CmdGather}, {CmdWander}, {CmdClearDebris}", LogLevel.Trace);
```
替换为：
```csharp
            log.Log($"[DebugCommands] registered: {CmdFriendship}, {CmdDebug}, {CmdTeleport}, {CmdProactive}, {CmdStatus}, {CmdTick}, {CmdGoto}, {CmdGather}, {CmdWander}, {CmdClearDebris}, {CmdDeposit}", LogLevel.Trace);
```

- [ ] **Step 4: 在文件末尾 `}` 之前添加 `HandleDeposit` 方法**

找到文件最后两行：
```csharp
    }
}
```
在 `}` 之前插入：
```csharp
        // ── smartnpc_deposit_items ─────────────────────────────────────────

        // Usage:
        //   smartnpc_deposit_items <NpcName>                      → auto-find, all items
        //   smartnpc_deposit_items <NpcName> <chestX> <chestY>   → specific chest, all items
        //   smartnpc_deposit_items <NpcName> auto (O)390 (O)388  → auto-find, filtered
        private static void HandleDeposit(string[] args, IMonitor log, NpcInventory inventory, FollowSystem follow)
        {
            if (!Context.IsWorldReady)
            {
                log.Log("no save loaded; start or load a save first.", LogLevel.Error);
                return;
            }
            if (args.Length < 1)
            {
                log.Log($"usage: {CmdDeposit} <NpcName> [chestX chestY | auto] [(O)itemId ...]", LogLevel.Error);
                return;
            }

            string name = args[0];
            NPC? npc = Game1.getCharacterFromName(name);
            if (npc is null)
            {
                log.Log($"NPC '{name}' not found.", LogLevel.Warn);
                return;
            }

            bool autoFind = true;
            int  chestX   = 0;
            int  chestY   = 0;
            List<string>? itemIds = null;
            int argIdx = 1;

            if (args.Length > 1)
            {
                if (string.Equals(args[1], "auto", StringComparison.OrdinalIgnoreCase))
                {
                    autoFind = true;
                    argIdx   = 2;
                }
                else if (args.Length >= 3 && int.TryParse(args[1], out int px) && int.TryParse(args[2], out int py))
                {
                    autoFind = false;
                    chestX   = px;
                    chestY   = py;
                    argIdx   = 3;
                }
            }

            // Remaining args are item_ids.
            if (argIdx < args.Length)
            {
                itemIds = new List<string>();
                for (int i = argIdx; i < args.Length; i++)
                    itemIds.Add(args[i]);
            }

            bool started = follow.StartDepositItems(
                name, npc,
                new Microsoft.Xna.Framework.Point(chestX, chestY),
                autoFind, null,
                itemIds,
                inventory);

            if (started)
                log.Log($"[smartnpc_deposit_items] triggered for {name} autoFind={autoFind} chest=({chestX},{chestY})", LogLevel.Info);
            else
                log.Log($"[smartnpc_deposit_items] could not start (no chest found?)", LogLevel.Warn);
        }
```

- [ ] **Step 5: 编译验证**

```cmd
cd D:/SmartNPC && "/c/Users/synchen/go/bin/task.exe" mod:build
```
期望：0 errors

- [ ] **Step 6: commit**

```bash
cd D:/SmartNPC && git add smapi-mod/Debug/DebugCommands.cs && git commit -m "feat(mod): add smartnpc_deposit_items debug command"
```

---

## Task 6: 全量 CI 验证

- [ ] **Step 1: 运行完整 CI**

```cmd
cd D:/SmartNPC && "/c/Users/synchen/go/bin/task.exe" ci
```
期望：profiles:verify + lint + test + build 全绿，0 errors

- [ ] **Step 2: 安装 mod**

```cmd
"/c/Users/synchen/go/bin/task.exe" mod:install
```

- [ ] **Step 3: 游戏内验证**

进游戏，在农场放一个箱子，SMAPI console：
```
smartnpc_clear_debris Abigail 8 3
```
等 Abigail 清完杂物（头顶出现背包图标），再执行：
```
smartnpc_deposit_items Abigail
```
期望：Abigail 走向最近箱子，到达后播放 happy 表情，背包图标消失，箱子里出现石头/木头/混合种子。

- [ ] **Step 4: 最终 commit**

```bash
cd D:/SmartNPC && git add -A && git commit -m "feat: npc_deposit_items — walk-to-chest deposit with auto_find and item_ids filter"
```
