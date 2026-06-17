# NPC 背包 + ClearDebris 真实行为 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为所有 Agent-managed NPC 添加持久化背包（per-save SMAPI 存储），实现 `npc_clear_debris` 真实清理行为（清杂物 → 掉落物写入背包），并在 NPC 头顶显示背包图标，点击可弹出格子视图面板。

**Architecture:** C# SMAPI mod 侧新增 `NpcInventory`（数据层）+ `ClearDebrisHandler` 真实逻辑 + `NpcInventoryPanel`（4列格子 UI）+ HUD 头顶图标；Go MCP 侧新增 `npc_inventory_get` / `npc_inventory_put` / `npc_inventory_take` 三个工具。`NpcInventory` 通过 SMAPI `Helper.Data.WriteSaveData` 持久化，`ModEntry` 串联 OnSaveLoaded / OnSaving。

**Tech Stack:** C# .NET 6 / SMAPI 4 / MonoGame / Go 1.25 / MCP SDK

---

## 文件清单

### 新建文件
- `smapi-mod/Data/NpcInventory.cs` — 背包数据层（ItemSlot、NpcInventory 类、Snapshot/Restore）
- `smapi-mod/UI/NpcInventoryPanel.cs` — 格子背包面板（IClickableMenu 子类）
- `smapi-mod/UI/NpcInventoryHud.cs` — HUD 头顶图标绘制 + 点击检测
- `smartnpc-mcp/adapters/stardew/tools/npc_inventory.go` — Go MCP 工具（inventory_get/put/take）
- `smartnpc-mcp/adapters/stardew/tools/npc_inventory_test.go` — Go 工具端到端测试

### 修改文件
- `smapi-mod/Behavior/WorldActionHandlers.cs` — `ClearDebrisHandler.Execute` 真实实现
- `smapi-mod/ModEntry.cs` — 注入 NpcInventory；OnSaveLoaded/OnSaving；OnRenderedHud；OnButtonPressed
- `smapi-mod/Bridge/protocol.go`（Go）— 新增 `ActionNpcInventoryGet/Put/Take` 常量
- `smartnpc-mcp/adapters/stardew/tools/registry.go` — 注册 `registerNpcInventory`

---

## Task 1: 数据层 `NpcInventory`

**Files:**
- Create: `smapi-mod/Data/NpcInventory.cs`

- [ ] **Step 1: 创建 `NpcInventory.cs`**

```csharp
// smapi-mod/Data/NpcInventory.cs
// Per-NPC persistent backpack. Serialized with SMAPI's WriteSaveData.

using System.Collections.Generic;
using System.Linq;

namespace SmartNPC.Bridge
{
    /// <summary>One stack of items in an NPC's backpack.</summary>
    internal sealed class ItemSlot
    {
        public string ItemId  { get; set; } = "";  // SDV qualified id, e.g. "(O)390"
        public int    Count   { get; set; } = 1;
        public int    Quality { get; set; } = 0;   // 0=normal 1=silver 2=gold 4=iridium
    }

    /// <summary>
    /// In-memory backpack store for all Agent-managed NPCs.
    /// One instance lives in ModEntry; persisted per save via SMAPI.
    /// </summary>
    internal sealed class NpcInventory
    {
        private readonly Dictionary<string, List<ItemSlot>> _bags = new();

        // ── read ──────────────────────────────────────────────────

        public IReadOnlyList<ItemSlot> GetItems(string npcName)
        {
            if (string.IsNullOrEmpty(npcName)) return System.Array.Empty<ItemSlot>();
            return _bags.TryGetValue(npcName, out var bag)
                ? bag.AsReadOnly()
                : System.Array.Empty<ItemSlot>();
        }

        public bool HasItems(string npcName) =>
            _bags.TryGetValue(npcName, out var b) && b.Count > 0;

        // ── write ─────────────────────────────────────────────────

        /// <summary>Add (or stack) items. Returns the new total count for that stack.</summary>
        public int Add(string npcName, string itemId, int count, int quality = 0)
        {
            if (string.IsNullOrEmpty(npcName) || string.IsNullOrEmpty(itemId) || count <= 0)
                return 0;

            var bag = GetOrCreate(npcName);
            var slot = bag.FirstOrDefault(s =>
                s.ItemId == itemId && s.Quality == quality);

            if (slot is null)
            {
                slot = new ItemSlot { ItemId = itemId, Count = 0, Quality = quality };
                bag.Add(slot);
            }
            slot.Count += count;
            return slot.Count;
        }

        /// <summary>
        /// Remove up to <paramref name="count"/> of <paramref name="itemId"/>.
        /// Returns how many were actually removed.
        /// </summary>
        public int Take(string npcName, string itemId, int count)
        {
            if (string.IsNullOrEmpty(npcName) || string.IsNullOrEmpty(itemId) || count <= 0)
                return 0;

            if (!_bags.TryGetValue(npcName, out var bag)) return 0;

            int taken = 0;
            foreach (var slot in bag.Where(s => s.ItemId == itemId).ToList())
            {
                int canTake = System.Math.Min(slot.Count, count - taken);
                slot.Count -= canTake;
                taken      += canTake;
                if (slot.Count <= 0) bag.Remove(slot);
                if (taken >= count) break;
            }
            return taken;
        }

        public void Clear(string npcName)
        {
            _bags.Remove(npcName);
        }

        // ── persistence ───────────────────────────────────────────

        /// <summary>Snapshot for SMAPI WriteSaveData.</summary>
        public Dictionary<string, List<ItemSlot>> Snapshot() =>
            _bags.ToDictionary(kv => kv.Key, kv => kv.Value.ToList());

        /// <summary>Restore from a snapshot. Replaces all in-memory state.</summary>
        public void Restore(Dictionary<string, List<ItemSlot>>? snapshot)
        {
            _bags.Clear();
            if (snapshot is null) return;
            foreach (var kv in snapshot)
                if (kv.Value?.Count > 0)
                    _bags[kv.Key] = kv.Value;
        }

        // ── helpers ───────────────────────────────────────────────

        private List<ItemSlot> GetOrCreate(string npcName)
        {
            if (!_bags.TryGetValue(npcName, out var bag))
            {
                bag = new List<ItemSlot>();
                _bags[npcName] = bag;
            }
            return bag;
        }
    }
}
```

- [ ] **Step 2: 编译验证**

```cmd
cd D:\SmartNPC && C:\Users\synchen\go\bin\task.exe mod:build
```
期望：0 errors 0 warnings

- [ ] **Step 3: commit**

```
git add smapi-mod/Data/NpcInventory.cs
git commit -m "feat(mod): add NpcInventory data layer with per-npc ItemSlot storage"
```

---

## Task 2: 串联 NpcInventory 到 ModEntry

**Files:**
- Modify: `smapi-mod/ModEntry.cs`

- [ ] **Step 1: 在 ModEntry 顶部声明字段并更新 save key**

在 `private const string SaveKey_Unread` 行之后加：

```csharp
private const string SaveKey_Inventories = "smartnpc.inventories";
```

在 `private readonly UnreadTracker _unread = new();` 之后加：

```csharp
private readonly NpcInventory _npcInventory = new();
```

- [ ] **Step 2: `OnSaveLoaded` 恢复背包**

在 `_unread.Restore(un);` 之后加：

```csharp
var inv = this.Helper.Data.ReadSaveData<Dictionary<string, List<ItemSlot>>>(SaveKey_Inventories);
_npcInventory.Restore(inv);
this.Monitor.Log($"NPC inventories restored ({AgentNpcRegistry.GetAll().Count} NPCs)", LogLevel.Debug);
```

- [ ] **Step 3: `OnSaving` 持久化背包**

在 `this.Helper.Data.WriteSaveData(SaveKey_Unread, _unread.Snapshot());` 之后加：

```csharp
this.Helper.Data.WriteSaveData(SaveKey_Inventories, _npcInventory.Snapshot());
```

- [ ] **Step 4: 把 `_npcInventory` 传给 `ClearDebrisHandler`**

找到：

```csharp
new ClearDebrisHandler(this.Monitor, showBubble),
```

改为：

```csharp
new ClearDebrisHandler(this.Monitor, showBubble, _npcInventory),
```

- [ ] **Step 5: 编译验证**

```cmd
C:\Users\synchen\go\bin\task.exe mod:build
```
期望：0 errors

- [ ] **Step 6: commit**

```
git add smapi-mod/ModEntry.cs
git commit -m "feat(mod): wire NpcInventory into ModEntry save/load lifecycle"
```

---

## Task 3: ClearDebrisHandler 真实实现

**Files:**
- Modify: `smapi-mod/Behavior/WorldActionHandlers.cs`

- [ ] **Step 1: 替换 `ClearDebrisHandler`**

找到并替换整个 `ClearDebrisHandler` 类：

```csharp
internal sealed class ClearDebrisHandler : NpcActionHandlerBase
{
    private readonly NpcInventory _inventory;

    protected override string ActionName => "npc_clear_debris";

    public ClearDebrisHandler(IMonitor log, Func<bool> showBubble, NpcInventory inventory)
        : base(log, showBubble)
    {
        _inventory = inventory;
    }

    protected override string ResolveBubble(JsonElement @params)
    {
        int radius   = ParseInt(@params, "radius",    5, 1, 10);
        int maxCount = ParseInt(@params, "max_count", 3, 1, 10);
        return $"[清理] r={radius} max={maxCount}";
    }

    protected override void Execute(NPC npc, string npcName, JsonElement @params)
    {
        int radius   = ParseInt(@params, "radius",    5, 1, 10);
        int maxCount = ParseInt(@params, "max_count", 3, 1, 10);

        var location = npc.currentLocation;
        if (location is null) return;

        // 1. Scan for clearable objects within radius.
        var npcTile = npc.Tile;
        var targets = new List<(Microsoft.Xna.Framework.Vector2 tile, StardewValley.Object obj)>();

        foreach (var kv in location.Objects.Pairs)
        {
            var tile = kv.Key;
            var obj  = kv.Value;

            if (!IsDebris(obj)) continue;

            float dist = Microsoft.Xna.Framework.Vector2.Distance(npcTile, tile);
            if (dist > radius) continue;

            targets.Add((tile, obj));
        }

        // Sort by distance, take max_count.
        targets.Sort((a, b) =>
            Microsoft.Xna.Framework.Vector2.Distance(npcTile, a.tile)
                .CompareTo(Microsoft.Xna.Framework.Vector2.Distance(npcTile, b.tile)));

        if (targets.Count > maxCount) targets = targets.GetRange(0, maxCount);

        int cleared = 0;
        foreach (var (tile, obj) in targets)
        {
            // Warp NPC to adjacent tile (best-effort; no async await here).
            var adjacent = new Microsoft.Xna.Framework.Vector2(tile.X, tile.Y + 1);
            try { npc.controller = null; npc.Halt(); } catch { }
            try
            {
                npc.controller = new StardewValley.Pathfinding.PathFindController(
                    c: npc,
                    location: location,
                    endPoint: new Microsoft.Xna.Framework.Point((int)adjacent.X, (int)adjacent.Y),
                    finalFacingDirection: 0,
                    endBehaviorFunction: null);
            }
            catch { /* non-fatal, NPC stays put but still clears */ }

            // Play emote and remove object.
            npc.doEmote(16); // exclamation "!"

            if (location.Objects.ContainsKey(tile))
            {
                location.Objects.Remove(tile);

                // Add drop to inventory.
                string dropId = DebrisDropId(obj);
                _inventory.Add(npcName, dropId, 1);
                cleared++;

                Log.Log(
                    $"[npc_clear_debris] {npcName} cleared {obj.Name} at ({(int)tile.X},{(int)tile.Y})" +
                    $" → added {dropId} to inventory",
                    LogLevel.Debug);
            }
        }

        Log.Log($"[npc_clear_debris] {npcName} cleared {cleared} debris, inventory now has {_inventory.GetItems(npcName).Count} stacks", LogLevel.Info);
    }

    // ── helpers ───────────────────────────────────────────────────

    private static bool IsDebris(StardewValley.Object obj)
    {
        if (obj is null) return false;
        return obj.IsWeeds() || obj.IsTwig()
            || (obj.Category == StardewValley.Object.litterCategory)
            || (obj.Name == "Stone" && obj.ParentSheetIndex >= 0);
    }

    /// <summary>Standard drop item id for each debris type.</summary>
    private static string DebrisDropId(StardewValley.Object obj)
    {
        if (obj.IsTwig())  return "(O)388"; // Wood
        if (obj.IsWeeds()) return "(O)771"; // Mixed Seeds
        // Stone / everything else
        return "(O)390";  // Stone
    }

    private static int ParseInt(JsonElement p, string key, int def, int min, int max)
    {
        if (p.ValueKind == JsonValueKind.Object &&
            p.TryGetProperty(key, out JsonElement el) &&
            el.TryGetInt32(out int v))
            return System.Math.Clamp(v, min, max);
        return def;
    }
}
```

- [ ] **Step 2: 在文件顶部确认 `using System.Collections.Generic;` 已存在**

如果没有，在其他 using 之后加：

```csharp
using System.Collections.Generic;
```

- [ ] **Step 3: 编译验证**

```cmd
C:\Users\synchen\go\bin\task.exe mod:build
```
期望：0 errors

- [ ] **Step 4: commit**

```
git add smapi-mod/Behavior/WorldActionHandlers.cs
git commit -m "feat(mod): implement ClearDebrisHandler with real debris removal and inventory"
```

---

## Task 4: HUD 头顶图标（`NpcInventoryHud`）

**Files:**
- Create: `smapi-mod/UI/NpcInventoryHud.cs`

- [ ] **Step 1: 创建 `NpcInventoryHud.cs`**

```csharp
// NpcInventoryHud — draws a small backpack icon above Agent-managed NPCs
// when their inventory is non-empty. Click the icon to open NpcInventoryPanel.
//
// Drawn in OnRenderedHud (above world, below menus).
// Click detection in OnButtonPressed (MouseLeft only).

using System;
using System.Collections.Generic;
using Microsoft.Xna.Framework;
using Microsoft.Xna.Framework.Graphics;
using StardewValley;
using StardewValley.Menus;

namespace SmartNPC.Bridge
{
    internal sealed class NpcInventoryHud
    {
        // Icon dimensions in screen pixels (pre-scale).
        private const int IconW = 18;
        private const int IconH = 18;
        // Pixel offset above the NPC's name tag.
        private const int OffsetYAboveHead = 80;

        // SDV Cursors sheet coords for the small backpack icon:
        // row=3, col=6 in the 16×16 grid → source rect (96, 48, 16, 16)
        // (This is the "inventory bag" mini-icon used in tooltips.)
        private static readonly Rectangle BackpackSrc = new(96, 48, 16, 16);

        private readonly NpcInventory       _inventory;
        private readonly Action<string>     _openPanel;
        private readonly List<(string Name, Rectangle Bounds)> _hotspots = new();

        public NpcInventoryHud(NpcInventory inventory, Action<string> openPanel)
        {
            _inventory = inventory;
            _openPanel = openPanel;
        }

        // ── draw ──────────────────────────────────────────────────

        public void Draw(SpriteBatch sb)
        {
            _hotspots.Clear();
            if (!Context.IsWorldReady) return;
            if (Game1.activeClickableMenu != null) return;

            var player   = Game1.player;
            var location = player?.currentLocation;
            if (location is null) return;

            foreach (var npcName in AgentNpcRegistry.GetAll())
            {
                if (!_inventory.HasItems(npcName)) continue;

                NPC? npc = Game1.getCharacterFromName(npcName);
                if (npc is null || npc.currentLocation != location) continue;

                // Convert world position → screen position.
                Vector2 npcWorldPos = npc.Position;
                float screenX = (npcWorldPos.X - Game1.viewport.X) * Game1.options.zoomLevel;
                float screenY = (npcWorldPos.Y - Game1.viewport.Y - OffsetYAboveHead) * Game1.options.zoomLevel;

                // Skip if off screen.
                if (screenX < -IconW * 2 || screenX > Game1.graphics.GraphicsDevice.Viewport.Width + IconW) continue;
                if (screenY < -IconH * 2 || screenY > Game1.graphics.GraphicsDevice.Viewport.Height) continue;

                float scale = Game1.options.zoomLevel * 2f;
                int drawW = (int)(IconW * scale);
                int drawH = (int)(IconH * scale);
                int drawX = (int)(screenX - drawW / 2f);
                int drawY = (int)(screenY - drawH);

                sb.Draw(
                    texture: Game1.mouseCursors,
                    destinationRectangle: new Rectangle(drawX, drawY, drawW, drawH),
                    sourceRectangle: BackpackSrc,
                    color: Color.White * 0.92f);

                _hotspots.Add((npcName, new Rectangle(drawX, drawY, drawW, drawH)));
            }
        }

        // ── click ─────────────────────────────────────────────────

        /// <summary>
        /// Call from OnButtonPressed for MouseLeft.
        /// Returns true if the click was consumed by a backpack icon.
        /// </summary>
        public bool TryClick(int screenX, int screenY)
        {
            foreach (var (name, bounds) in _hotspots)
            {
                if (bounds.Contains(screenX, screenY))
                {
                    _openPanel(name);
                    return true;
                }
            }
            return false;
        }
    }
}
```

- [ ] **Step 2: 编译验证**

```cmd
C:\Users\synchen\go\bin\task.exe mod:build
```
期望：0 errors

- [ ] **Step 3: commit**

```
git add smapi-mod/UI/NpcInventoryHud.cs
git commit -m "feat(mod): add NpcInventoryHud head-icon overlay for non-empty NPC backpacks"
```

---

## Task 5: 背包格子面板（`NpcInventoryPanel`）

**Files:**
- Create: `smapi-mod/UI/NpcInventoryPanel.cs`

- [ ] **Step 1: 创建 `NpcInventoryPanel.cs`**

```csharp
// NpcInventoryPanel — modal grid view of one NPC's backpack.
// 4 columns, rows grow with item count (min 2 rows = 8 slots).
// Hover a slot to see item name + count + quality in the bottom bar.
// Read-only; close with Esc or the X button.

using System;
using System.Collections.Generic;
using Microsoft.Xna.Framework;
using Microsoft.Xna.Framework.Graphics;
using StardewValley;
using StardewValley.Menus;

namespace SmartNPC.Bridge
{
    internal sealed class NpcInventoryPanel : IClickableMenu
    {
        private const int Cols        = 4;
        private const int CellSize    = 64;
        private const int Padding     = 16;
        private const int TitleHeight = 48;
        private const int FooterH     = 40;
        private const int MinRows     = 2;

        private readonly string          _npcName;
        private readonly NpcInventory    _inventory;
        private readonly List<ItemSlot>  _items;

        private int    _hoveredIndex = -1;

        public NpcInventoryPanel(string npcName, NpcInventory inventory)
            : base(0, 0, 0, 0, showUpperRightCloseButton: true)
        {
            _npcName   = npcName;
            _inventory = inventory;
            _items     = new List<ItemSlot>(inventory.GetItems(npcName));

            // Compute panel size.
            int rows    = Math.Max(MinRows, (int)Math.Ceiling(_items.Count / (double)Cols));
            int panelW  = Cols * CellSize + Padding * 2;
            int panelH  = TitleHeight + rows * CellSize + Padding * 2 + FooterH;

            // Centre on screen.
            int x = (Game1.uiViewport.Width  - panelW) / 2;
            int y = (Game1.uiViewport.Height - panelH) / 2;

            initialize(x, y, panelW, panelH, showUpperRightCloseButton: true);
        }

        public override void draw(SpriteBatch b)
        {
            // Dim background.
            b.Draw(Game1.fadeToBlackRect,
                new Rectangle(0, 0, Game1.uiViewport.Width, Game1.uiViewport.Height),
                Color.Black * 0.5f);

            // Panel background.
            IClickableMenu.drawTextureBox(b, xPositionOnScreen, yPositionOnScreen, width, height, Color.White);

            // Title.
            string title = $"{_npcName} 的背包";
            b.DrawString(Game1.dialogueFont, title,
                new Vector2(xPositionOnScreen + Padding, yPositionOnScreen + Padding),
                Game1.textColor);

            // Grid cells.
            int gridTop = yPositionOnScreen + TitleHeight + Padding;
            int gridLeft = xPositionOnScreen + Padding;

            for (int i = 0; i < Math.Max(MinRows * Cols, _items.Count + 1); i++)
            {
                int col = i % Cols;
                int row = i / Cols;
                int cx  = gridLeft + col * CellSize;
                int cy  = gridTop  + row * CellSize;
                var cellRect = new Rectangle(cx, cy, CellSize, CellSize);

                // Cell background / highlight.
                Color bg = (i == _hoveredIndex) ? Color.LightYellow : Color.White * 0.3f;
                b.Draw(Game1.fadeToBlackRect, cellRect, bg);
                IClickableMenu.drawTextureBox(b, Game1.menuTexture,
                    new Rectangle(0, 256, 60, 60), cx, cy, CellSize, CellSize,
                    Color.White, drawShadow: false);

                if (i >= _items.Count) continue;
                var slot = _items[i];

                // Draw item icon.
                try
                {
                    var item = StardewValley.ItemRegistry.Create(slot.ItemId, slot.Count, slot.Quality);
                    item?.drawInMenu(b, new Vector2(cx + 4, cy + 4), 0.85f,
                        transparency: 1f, layerDepth: 0.9f,
                        drawStackNumber: StackDrawType.Draw,
                        color: Color.White, drawShadow: true);
                }
                catch
                {
                    // Unknown item id — draw placeholder text.
                    b.DrawString(Game1.smallFont, "?",
                        new Vector2(cx + CellSize / 2f - 6, cy + CellSize / 2f - 8),
                        Color.DarkRed);
                }
            }

            // Footer: hovered item detail.
            int footerY = yPositionOnScreen + height - FooterH - Padding / 2;
            if (_hoveredIndex >= 0 && _hoveredIndex < _items.Count)
            {
                var slot = _items[_hoveredIndex];
                string qualityLabel = slot.Quality switch
                {
                    1 => "银质",
                    2 => "金质",
                    4 => "铱质",
                    _ => "普通",
                };
                string detail = $"{slot.ItemId}  ×{slot.Count}  品质：{qualityLabel}";
                try
                {
                    var item = StardewValley.ItemRegistry.Create(slot.ItemId);
                    if (item is not null)
                        detail = $"{item.DisplayName}  ×{slot.Count}  品质：{qualityLabel}";
                }
                catch { }
                b.DrawString(Game1.smallFont, detail,
                    new Vector2(xPositionOnScreen + Padding, footerY),
                    Game1.textColor);
            }

            // Close button.
            base.draw(b);
            drawMouse(b);
        }

        public override void performHoverAction(int x, int y)
        {
            base.performHoverAction(x, y);
            _hoveredIndex = HitTestSlot(x, y);
        }

        public override void receiveRightClick(int x, int y, bool playSound = true) =>
            exitThisMenu();

        public override void receiveKeyPress(Microsoft.Xna.Framework.Input.Keys key)
        {
            if (key == Microsoft.Xna.Framework.Input.Keys.Escape)
                exitThisMenu();
            base.receiveKeyPress(key);
        }

        // ── helpers ───────────────────────────────────────────────

        private int HitTestSlot(int sx, int sy)
        {
            int gridTop  = yPositionOnScreen + TitleHeight + Padding;
            int gridLeft = xPositionOnScreen + Padding;
            int rows     = Math.Max(MinRows, (int)Math.Ceiling(_items.Count / (double)Cols));

            for (int i = 0; i < rows * Cols; i++)
            {
                int col = i % Cols;
                int row = i / Cols;
                var r   = new Rectangle(gridLeft + col * CellSize, gridTop + row * CellSize,
                                        CellSize, CellSize);
                if (r.Contains(sx, sy)) return i;
            }
            return -1;
        }
    }
}
```

- [ ] **Step 2: 编译验证**

```cmd
C:\Users\synchen\go\bin\task.exe mod:build
```
期望：0 errors

- [ ] **Step 3: commit**

```
git add smapi-mod/UI/NpcInventoryPanel.cs
git commit -m "feat(mod): add NpcInventoryPanel 4-col grid menu"
```

---

## Task 6: 串联 HUD + Panel 到 ModEntry

**Files:**
- Modify: `smapi-mod/ModEntry.cs`

- [ ] **Step 1: 添加字段**

在 `private NotificationToast? _toast;` 之后加：

```csharp
private NpcInventoryHud?   _inventoryHud;
```

- [ ] **Step 2: `OnGameLaunched` 末尾初始化 HUD**

在 `this.Monitor.Log($"StardewMCPBridge ready...` 之前加：

```csharp
_inventoryHud = new NpcInventoryHud(_npcInventory, OpenInventoryPanel);
```

- [ ] **Step 3: 添加 `OpenInventoryPanel` 辅助方法**

在 `OpenChatPanelForNpc` 方法之后加：

```csharp
private void OpenInventoryPanel(string npcName)
{
    Game1.activeClickableMenu = new NpcInventoryPanel(npcName, _npcInventory);
}
```

- [ ] **Step 4: `OnRenderedHud` 绘制 HUD 图标**

找到：

```csharp
private void OnRenderedHud(object? sender, RenderedHudEventArgs e)
{
    // Toasts are HUD-level; draw above world but below menus.
    if (Game1.activeClickableMenu is ChatPanel) return;
    _toast?.Draw(e.SpriteBatch);
}
```

替换为：

```csharp
private void OnRenderedHud(object? sender, RenderedHudEventArgs e)
{
    // Toasts are HUD-level; draw above world but below menus.
    if (Game1.activeClickableMenu is ChatPanel) return;
    _toast?.Draw(e.SpriteBatch);
    _inventoryHud?.Draw(e.SpriteBatch);
}
```

- [ ] **Step 5: `OnButtonPressed` 加背包图标点击检测**

找到：

```csharp
private void OnButtonPressed(object? sender, ButtonPressedEventArgs e)
{
    if (e.Button != SButton.MouseLeft) return;
    if (Game1.activeClickableMenu != null) return;
    if (_toast == null) return;

    int mx = (int)e.Cursor.ScreenPixels.X;
    int my = (int)e.Cursor.ScreenPixels.Y;
    _toast.TryClick(mx, my);
}
```

替换为：

```csharp
private void OnButtonPressed(object? sender, ButtonPressedEventArgs e)
{
    if (e.Button != SButton.MouseLeft) return;
    if (Game1.activeClickableMenu != null) return;

    int mx = (int)e.Cursor.ScreenPixels.X;
    int my = (int)e.Cursor.ScreenPixels.Y;

    // Backpack icon takes priority over toast (icon is smaller / more precise).
    if (_inventoryHud != null && _inventoryHud.TryClick(mx, my)) return;
    _toast?.TryClick(mx, my);
}
```

- [ ] **Step 6: 编译验证**

```cmd
C:\Users\synchen\go\bin\task.exe mod:build
```
期望：0 errors

- [ ] **Step 7: commit**

```
git add smapi-mod/ModEntry.cs
git commit -m "feat(mod): wire NpcInventoryHud draw and click into ModEntry"
```

---

## Task 7: Go MCP 工具（inventory_get/put/take）

**Files:**
- Create: `smartnpc-mcp/adapters/stardew/tools/npc_inventory.go`
- Modify: `smartnpc-mcp/adapters/stardew/bridge/protocol.go`
- Modify: `smartnpc-mcp/adapters/stardew/tools/registry.go`

- [ ] **Step 1: 新增 action 常量到 `protocol.go`**

在 `ActionNpcPlaceObject = "npc_place_object"` 之后加：

```go
// ── NPC inventory ────────────────────────────────────────────
ActionNpcInventoryGet  = "npc_inventory_get"
ActionNpcInventoryPut  = "npc_inventory_put"
ActionNpcInventoryTake = "npc_inventory_take"
```

- [ ] **Step 2: 创建 `npc_inventory.go`**

```go
package tools

import (
    "context"
    "fmt"

    "github.com/modelcontextprotocol/go-sdk/mcp"

    "github.com/OmniStormX/SmartNPC/adapters/stardew/bridge"
)

// ── npc_inventory_get ─────────────────────────────────────────

type NpcInventoryGetInput struct {
    NPC string `json:"npc" jsonschema:"NPC internal name"`
}

type ItemSlotOutput struct {
    ItemId  string `json:"item_id"           jsonschema:"SDV qualified item id"`
    Count   int    `json:"count"             jsonschema:"stack size"`
    Quality int    `json:"quality"           jsonschema:"0=normal 1=silver 2=gold 4=iridium"`
}

type NpcInventoryGetOutput struct {
    OK    bool             `json:"ok"`
    NPC   string           `json:"npc"`
    Items []ItemSlotOutput `json:"items"`
}

// ── npc_inventory_put ─────────────────────────────────────────

type NpcInventoryPutInput struct {
    NPC     string `json:"npc"               jsonschema:"NPC internal name"`
    ItemId  string `json:"item_id"           jsonschema:"SDV qualified item id, e.g. \"(O)390\""`
    Count   int    `json:"count"             jsonschema:"amount to add (default 1)"`
    Quality int    `json:"quality,omitempty" jsonschema:"0=normal 1=silver 2=gold 4=iridium"`
}

type NpcInventoryPutOutput struct {
    OK         bool   `json:"ok"`
    NPC        string `json:"npc"`
    NewTotal   int    `json:"new_total"          jsonschema:"new count for this stack after adding"`
    Message    string `json:"message,omitempty"`
}

// ── npc_inventory_take ────────────────────────────────────────

type NpcInventoryTakeInput struct {
    NPC    string `json:"npc"    jsonschema:"NPC internal name"`
    ItemId string `json:"item_id" jsonschema:"SDV qualified item id to remove"`
    Count  int    `json:"count"   jsonschema:"amount to remove"`
}

type NpcInventoryTakeOutput struct {
    OK      bool   `json:"ok"`
    NPC     string `json:"npc"`
    Taken   int    `json:"taken"            jsonschema:"how many were actually removed"`
    Message string `json:"message,omitempty"`
}

// ── registration ──────────────────────────────────────────────

func registerNpcInventory(s *mcp.Server, br *bridge.WSClient) {
    mcp.AddTool(s, &mcp.Tool{
        Name: "npc_inventory_get",
        Description: "Return the current contents of an NPC's backpack.\n\n" +
            "When to call: before deciding whether to deliver items, after clear_debris " +
            "or forage_collect, or when the player asks what the NPC is carrying.\n\n" +
            "Side-effect: READ (no world mutation).",
    }, func(ctx context.Context, req *mcp.CallToolRequest, in NpcInventoryGetInput) (*mcp.CallToolResult, NpcInventoryGetOutput, error) {
        if in.NPC == "" {
            return nil, NpcInventoryGetOutput{}, fmt.Errorf("npc is required")
        }
        logToolCall("npc_inventory_get", in)
        out, err := callBridge[NpcInventoryGetOutput](ctx, req, br, bridge.ActionNpcInventoryGet, in, "npc_inventory_get")
        return nil, out, err
    })

    mcp.AddTool(s, &mcp.Tool{
        Name: "npc_inventory_put",
        Description: "Add an item to an NPC's backpack (for scripted gift-giving or testing).\n\n" +
            "When to call: manually seeding an NPC's inventory in scenarios where the NPC " +
            "should be carrying something specific.\n\n" +
            "Side-effect: WRITE (modifies NPC inventory, no world object removed).",
    }, func(ctx context.Context, req *mcp.CallToolRequest, in NpcInventoryPutInput) (*mcp.CallToolResult, NpcInventoryPutOutput, error) {
        if in.NPC == "" {
            return nil, NpcInventoryPutOutput{}, fmt.Errorf("npc is required")
        }
        if in.Count <= 0 {
            in.Count = 1
        }
        logToolCall("npc_inventory_put", in)
        out, err := callBridge[NpcInventoryPutOutput](ctx, req, br, bridge.ActionNpcInventoryPut, in, "npc_inventory_put")
        return nil, out, err
    })

    mcp.AddTool(s, &mcp.Tool{
        Name: "npc_inventory_take",
        Description: "Remove items from an NPC's backpack (does NOT give them to the player — " +
            "use npc_deliver_items for that).\n\n" +
            "When to call: discarding unwanted collected items, or adjusting inventory state.\n\n" +
            "Side-effect: WRITE (removes from NPC inventory only).",
    }, func(ctx context.Context, req *mcp.CallToolRequest, in NpcInventoryTakeInput) (*mcp.CallToolResult, NpcInventoryTakeOutput, error) {
        if in.NPC == "" {
            return nil, NpcInventoryTakeOutput{}, fmt.Errorf("npc is required")
        }
        logToolCall("npc_inventory_take", in)
        out, err := callBridge[NpcInventoryTakeOutput](ctx, req, br, bridge.ActionNpcInventoryTake, in, "npc_inventory_take")
        return nil, out, err
    })
}
```

- [ ] **Step 3: 注册到 `registry.go`**

在 `registerNpcWorldAction(s, br)` 之后加：

```go
registerNpcInventory(s, br)
```

- [ ] **Step 4: 编译 Go**

```cmd
cd D:\SmartNPC && C:\Users\synchen\go\bin\task.exe mcp:build
```
期望：0 errors

- [ ] **Step 5: commit**

```
git add smartnpc-mcp/adapters/stardew/bridge/protocol.go
git add smartnpc-mcp/adapters/stardew/tools/npc_inventory.go
git add smartnpc-mcp/adapters/stardew/tools/registry.go
git commit -m "feat(mcp): add npc_inventory_get/put/take MCP tools"
```

---

## Task 8: C# mod 侧处理 inventory ws actions

**Files:**
- Modify: `smapi-mod/ModEntry.cs`

- [ ] **Step 1: 注册 3 个 ws action handler**

在 `_router.Register("npc_get_behavior", _behavior.HandleGetBehavior);` 之后加：

```csharp
// NPC inventory ws actions — read/write the in-memory NpcInventory.
_router.Register("npc_inventory_get",  HandleInventoryGet);
_router.Register("npc_inventory_put",  HandleInventoryPut);
_router.Register("npc_inventory_take", HandleInventoryTake);
```

- [ ] **Step 2: 添加 3 个 handler 方法到 ModEntry**（放在 `SuppressScheduleForAgentNpcs` 之后）

```csharp
// ── NPC inventory ws handlers ───────────────────────────────────────

private System.Threading.Tasks.Task<Response> HandleInventoryGet(string id, System.Text.Json.JsonElement p)
{
    string? npc = p.ValueKind == System.Text.Json.JsonValueKind.Object &&
                  p.TryGetProperty("npc", out var el) ? el.GetString() : null;
    if (string.IsNullOrWhiteSpace(npc))
        return System.Threading.Tasks.Task.FromResult(Response.Failure(id, "invalid_params", "npc is required"));

    var items = _npcInventory.GetItems(npc!)
        .Select(s => new { item_id = s.ItemId, count = s.Count, quality = s.Quality })
        .ToArray();

    return System.Threading.Tasks.Task.FromResult(Response.Success(id, new
    {
        ok    = true,
        npc,
        items,
    }));
}

private System.Threading.Tasks.Task<Response> HandleInventoryPut(string id, System.Text.Json.JsonElement p)
{
    if (p.ValueKind != System.Text.Json.JsonValueKind.Object)
        return System.Threading.Tasks.Task.FromResult(Response.Failure(id, "invalid_params", "object params required"));

    string? npc    = p.TryGetProperty("npc",     out var e1) ? e1.GetString()    : null;
    string? itemId = p.TryGetProperty("item_id", out var e2) ? e2.GetString()    : null;
    int count      = p.TryGetProperty("count",   out var e3) && e3.TryGetInt32(out int c) ? c : 1;
    int quality    = p.TryGetProperty("quality", out var e4) && e4.TryGetInt32(out int q) ? q : 0;

    if (string.IsNullOrWhiteSpace(npc) || string.IsNullOrWhiteSpace(itemId))
        return System.Threading.Tasks.Task.FromResult(Response.Failure(id, "invalid_params", "npc and item_id are required"));

    int newTotal = _npcInventory.Add(npc!, itemId!, count, quality);
    return System.Threading.Tasks.Task.FromResult(Response.Success(id, new
    {
        ok        = true,
        npc,
        new_total = newTotal,
        message   = $"added {count}× {itemId} to {npc}",
    }));
}

private System.Threading.Tasks.Task<Response> HandleInventoryTake(string id, System.Text.Json.JsonElement p)
{
    if (p.ValueKind != System.Text.Json.JsonValueKind.Object)
        return System.Threading.Tasks.Task.FromResult(Response.Failure(id, "invalid_params", "object params required"));

    string? npc    = p.TryGetProperty("npc",     out var e1) ? e1.GetString() : null;
    string? itemId = p.TryGetProperty("item_id", out var e2) ? e2.GetString() : null;
    int count      = p.TryGetProperty("count",   out var e3) && e3.TryGetInt32(out int c) ? c : 1;

    if (string.IsNullOrWhiteSpace(npc) || string.IsNullOrWhiteSpace(itemId))
        return System.Threading.Tasks.Task.FromResult(Response.Failure(id, "invalid_params", "npc and item_id are required"));

    int taken = _npcInventory.Take(npc!, itemId!, count);
    return System.Threading.Tasks.Task.FromResult(Response.Success(id, new
    {
        ok      = true,
        npc,
        taken,
        message = taken > 0 ? $"removed {taken}× {itemId} from {npc}" : "item not found in inventory",
    }));
}
```

- [ ] **Step 3: 编译验证**

```cmd
C:\Users\synchen\go\bin\task.exe mod:build
```
期望：0 errors

- [ ] **Step 4: commit**

```
git add smapi-mod/ModEntry.cs
git commit -m "feat(mod): wire npc_inventory_get/put/take ws action handlers"
```

---

## Task 9: Go 端到端测试

**Files:**
- Create: `smartnpc-mcp/adapters/stardew/tools/npc_inventory_test.go`

- [ ] **Step 1: 创建 `npc_inventory_test.go`**

完全仿照 `npc_world_action_test.go` 的模式（`package tools`，`bridge.NewTestServer` echo stub，`mcp.NewInMemoryTransports`）：

```go
package tools

import (
    "context"
    "encoding/json"
    "testing"
    "time"

    "github.com/modelcontextprotocol/go-sdk/mcp"

    "github.com/OmniStormX/SmartNPC/adapters/stardew/bridge"
)

func newInventoryClientServer(t *testing.T) (*mcp.ClientSession, context.Context, func()) {
    t.Helper()

    ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)

    srv := bridge.NewTestServer(func(_ context.Context, action string, params json.RawMessage) (any, error) {
        var p struct {
            NPC    string `json:"npc"`
            ItemId string `json:"item_id"`
            Count  int    `json:"count"`
        }
        _ = json.Unmarshal(params, &p)
        switch action {
        case bridge.ActionNpcInventoryGet:
            return map[string]any{
                "ok":  true,
                "npc": p.NPC,
                "items": []map[string]any{
                    {"item_id": "(O)390", "count": 3, "quality": 0},
                },
            }, nil
        case bridge.ActionNpcInventoryPut:
            cnt := p.Count
            if cnt <= 0 {
                cnt = 1
            }
            return map[string]any{"ok": true, "npc": p.NPC, "new_total": cnt}, nil
        case bridge.ActionNpcInventoryTake:
            return map[string]any{"ok": true, "npc": p.NPC, "taken": 0}, nil
        }
        return map[string]any{"ok": true, "npc": p.NPC}, nil
    })

    br := bridge.NewWSClient(bridge.WSClientOptions{URL: srv.URL_WS()})
    if err := br.Connect(ctx); err != nil {
        cancel(); srv.Close()
        t.Fatalf("ws connect: %v", err)
    }

    server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "test"}, nil)
    registerNpcInventory(server, br)

    t1, t2 := mcp.NewInMemoryTransports()
    if _, err := server.Connect(ctx, t1, nil); err != nil {
        cancel(); br.Close(); srv.Close()
        t.Fatalf("server connect: %v", err)
    }
    client := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "t"}, nil)
    cs, err := client.Connect(ctx, t2, nil)
    if err != nil {
        cancel(); br.Close(); srv.Close()
        t.Fatalf("client connect: %v", err)
    }
    cleanup := func() { cs.Close(); br.Close(); srv.Close(); cancel() }
    return cs, ctx, cleanup
}

func TestNpcInventoryGet_ReturnsItems(t *testing.T) {
    cs, ctx, cleanup := newInventoryClientServer(t)
    defer cleanup()

    res, err := cs.CallTool(ctx, &mcp.CallToolParams{
        Name:      "npc_inventory_get",
        Arguments: map[string]any{"npc": "Abigail"},
    })
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if len(res.Content) == 0 {
        t.Fatal("empty content")
    }
    text := res.Content[0].(*mcp.TextContent).Text
    var out struct {
        OK    bool `json:"ok"`
        Items []struct {
            ItemId string `json:"item_id"`
            Count  int    `json:"count"`
        } `json:"items"`
    }
    if err := json.Unmarshal([]byte(text), &out); err != nil {
        t.Fatalf("unmarshal: %v", err)
    }
    if !out.OK {
        t.Fatal("expected ok=true")
    }
    if len(out.Items) != 1 || out.Items[0].ItemId != "(O)390" || out.Items[0].Count != 3 {
        t.Fatalf("unexpected items: %+v", out.Items)
    }
}

func TestNpcInventoryPut_ReturnsNewTotal(t *testing.T) {
    cs, ctx, cleanup := newInventoryClientServer(t)
    defer cleanup()

    res, err := cs.CallTool(ctx, &mcp.CallToolParams{
        Name:      "npc_inventory_put",
        Arguments: map[string]any{"npc": "Abigail", "item_id": "(O)390", "count": 1},
    })
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    text := res.Content[0].(*mcp.TextContent).Text
    var out struct {
        OK       bool `json:"ok"`
        NewTotal int  `json:"new_total"`
    }
    _ = json.Unmarshal([]byte(text), &out)
    if !out.OK || out.NewTotal != 1 {
        t.Fatalf("unexpected: %+v", out)
    }
}

func TestNpcInventoryTake_ReturnsZeroWhenEmpty(t *testing.T) {
    cs, ctx, cleanup := newInventoryClientServer(t)
    defer cleanup()

    res, err := cs.CallTool(ctx, &mcp.CallToolParams{
        Name:      "npc_inventory_take",
        Arguments: map[string]any{"npc": "Abigail", "item_id": "(O)999", "count": 5},
    })
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    text := res.Content[0].(*mcp.TextContent).Text
    var out struct {
        Taken int `json:"taken"`
    }
    _ = json.Unmarshal([]byte(text), &out)
    if out.Taken != 0 {
        t.Fatalf("expected taken=0, got %d", out.Taken)
    }
}
```

- [ ] **Step 2: 运行测试**

```cmd
cd D:\SmartNPC\smartnpc-mcp && go test -run TestNpcInventory ./adapters/stardew/tools/... -v
```
期望：3 tests PASS

- [ ] **Step 3: commit**

```
git add smartnpc-mcp/adapters/stardew/tools/npc_inventory_test.go
git commit -m "test(mcp): add npc_inventory_get/put/take end-to-end tests"
```

---

## Task 10: 全量 CI 验证

- [ ] **Step 1: 跑完整 CI**

```cmd
cd D:\SmartNPC && C:\Users\synchen\go\bin\task.exe ci
```
期望：profiles:verify + lint + test + build 全部 PASS，0 errors

- [ ] **Step 2: 安装到游戏并手动验证**

```cmd
C:\Users\synchen\go\bin\task.exe mod:install
```

验证清单：
1. 加载存档 → SMAPI 日志出现 `NPC inventories restored`
2. SMAPI 控制台执行 `smartnpc_gather` 把 NPC 集中到农场
3. 打开 SMAPI 控制台验证 `npc_inventory_get` ws 请求能正常转发（可用 echo mode）
4. 农场有杂草/石头时触发 `npc_clear_debris` → SMAPI 日志出现 `cleared N debris`
5. 清理后 NPC 头顶出现背包图标（非空背包）
6. 点击图标 → `NpcInventoryPanel` 打开，格子中可见石头/木头图标 + 数量
7. 退出存档再进入 → 背包内容持久化

- [ ] **Step 3: 最终 commit**

```
git add -A
git commit -m "feat: NPC inventory system - data layer, ClearDebris, HUD icon, grid panel, MCP tools"
```
