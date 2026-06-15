// Per-action handler files for world actions.
// Each is a minimal subclass of NpcActionHandlerBase. Override Execute
// when ready to implement real game logic.

using System;
using System.Collections.Generic;
using System.Linq;
using System.Text.Json;
using Microsoft.Xna.Framework;
using StardewModdingAPI;
using StardewValley;

namespace SmartNPC.Bridge
{
    // ── World actions ────────────────────────────────────────────────

    internal sealed class WanderHandler : NpcActionHandlerBase
    {
        private readonly FollowSystem _follow;

        protected override string ActionName => "npc_wander";
        public WanderHandler(IMonitor log, Func<bool> showBubble, FollowSystem follow)
            : base(log, showBubble)
        {
            _follow = follow;
        }

        protected override string ResolveBubble(JsonElement @params)
        {
            int radius = ParseRadius(@params);
            var (cx, cy, md, x1, y1, x2, y2) = ParseConstraints(@params);
            if (md > 0) return $"[wander] r={radius} center=({cx},{cy}) maxD={md}";
            if (x1 < x2 && y1 < y2) return $"[wander] r={radius} zone=({x1},{y1})-({x2},{y2})";
            return $"[wander] r={radius}";
        }

        protected override void Execute(NPC npc, string npcName, JsonElement @params)
        {
            int radius = ParseRadius(@params);
            int cx, cy, md, x1, y1, x2, y2;
            (cx, cy, md, x1, y1, x2, y2) = ParseConstraints(@params);

            // If center not specified, use NPC's current position.
            if (md > 0 && cx == 0 && cy == 0)
            {
                cx = (int)npc.Tile.X;
                cy = (int)npc.Tile.Y;
            }

            DoWander(npc, npcName, radius, cx, cy, md, x1, y1, x2, y2, _follow, Log);
        }

        /// <summary>Legacy overload — unconstrained wander (debug commands).</summary>
        public static void DoWander(NPC npc, string npcName, int radius, FollowSystem follow, IMonitor log)
            => DoWander(npc, npcName, radius, 0, 0, 0, 0, 0, 0, 0, follow, log);

        /// <summary>
        /// Start continuous wander for <paramref name="npc"/>: walk to a random passable tile,
        /// arrive, pick another, repeat. Optional zone constraints limit where tiles are picked.
        /// </summary>
        public static void DoWander(NPC npc, string npcName, int radius,
            int centerX, int centerY, int maxDist,
            int x1, int y1, int x2, int y2,
            FollowSystem follow, IMonitor log)
        {
            follow.StartWander(npcName, npc, radius, centerX, centerY, maxDist, x1, y1, x2, y2);
            var parts = new System.Collections.Generic.List<string>();
            parts.Add($"radius={radius}");
            if (maxDist > 0) parts.Add($"center=({centerX},{centerY}) maxD={maxDist}");
            if (x1 < x2 && y1 < y2) parts.Add($"zone=({x1},{y1})-({x2},{y2})");
            log.Log($"[npc_wander] {npc.Name} continuous wander started {string.Join(" ", parts)}", LogLevel.Debug);
        }

        private static int ParseRadius(JsonElement @params)
        {
            if (@params.ValueKind == JsonValueKind.Object &&
                @params.TryGetProperty("radius", out JsonElement el) &&
                el.TryGetInt32(out int r) && r > 0)
                return Math.Clamp(r, 1, 24);
            return 8;
        }

        private static (int cx, int cy, int md, int x1, int y1, int x2, int y2) ParseConstraints(JsonElement @params)
        {
            int cx = 0, cy = 0, md = 0, x1 = 0, y1 = 0, x2 = 0, y2 = 0;
            if (@params.ValueKind != JsonValueKind.Object) return (cx, cy, md, x1, y1, x2, y2);

            if (@params.TryGetProperty("center_x", out var el) && el.TryGetInt32(out int v)) cx = v;
            if (@params.TryGetProperty("center_y", out el) && el.TryGetInt32(out v)) cy = v;
            if (@params.TryGetProperty("max_distance", out el) && el.TryGetInt32(out v) && v > 0) md = v;
            if (@params.TryGetProperty("x1", out el) && el.TryGetInt32(out v)) x1 = v;
            if (@params.TryGetProperty("y1", out el) && el.TryGetInt32(out v)) y1 = v;
            if (@params.TryGetProperty("x2", out el) && el.TryGetInt32(out v)) x2 = v;
            if (@params.TryGetProperty("y2", out el) && el.TryGetInt32(out v)) y2 = v;
            return (cx, cy, md, x1, y1, x2, y2);
        }
    }

    internal sealed class ClearDebrisHandler : NpcActionHandlerBase
    {
        private readonly NpcInventory _inventory;
        private readonly FollowSystem _follow;

        protected override string ActionName => "npc_clear_debris";

        public ClearDebrisHandler(IMonitor log, Func<bool> showBubble, NpcInventory inventory, FollowSystem follow)
            : base(log, showBubble)
        {
            _inventory = inventory;
            _follow    = follow;
        }

        /// <summary>Public entry for debug commands running on the game thread.</summary>
        public void ExecuteDebug(NPC npc, string npcName, JsonElement @params)
            => Execute(npc, npcName, @params);

        protected override string ResolveBubble(JsonElement @params)
        {
            int radius   = ParseInt(@params, "radius",    5, 1, 30);
            int maxCount = ParseInt(@params, "max_count", 3, 1, 10);
            return $"[清理] r={radius} max={maxCount}";
        }

        protected override void Execute(NPC npc, string npcName, JsonElement @params)
        {
            int radius   = ParseInt(@params, "radius",    5, 1, 30);
            int maxCount = ParseInt(@params, "max_count", 3, 1, 10);
            var (bboxOn, x1, y1, x2, y2) = ParseBBox(@params);

            var location = npc.currentLocation;
            if (location is null) return;

            // 1. Scan for clearable objects within bbox or radius, sort by distance.
            var npcTile = npc.Tile;
            var targets = new List<(Microsoft.Xna.Framework.Vector2 tile, StardewValley.Object obj)>();

            foreach (var kv in location.Objects.Pairs)
            {
                var tile = kv.Key;
                var obj  = kv.Value;
                if (!IsDebris(obj)) continue;
                if (bboxOn)
                {
                    if (tile.X < x1 || tile.X > x2 || tile.Y < y1 || tile.Y > y2) continue;
                }
                else
                {
                    float dist = Microsoft.Xna.Framework.Vector2.Distance(npcTile, tile);
                    if (dist > radius) continue;
                }
                targets.Add((tile, obj));
            }

            targets.Sort((a, b) =>
                Microsoft.Xna.Framework.Vector2.Distance(npcTile, a.tile)
                    .CompareTo(Microsoft.Xna.Framework.Vector2.Distance(npcTile, b.tile)));

            if (targets.Count > maxCount) targets = targets.GetRange(0, maxCount);

            if (targets.Count == 0)
            {
                string scope = bboxOn ? $"bbox=({x1},{y1})-({x2},{y2})" : $"radius={radius}";
                Log.Log($"[npc_clear_debris] {npcName}: no debris in {scope}", LogLevel.Info);
                return;
            }

            // 2. Hand the ordered tile list to FollowSystem — it will walk NPC to each
            //    tile, destroy the object on arrival, and collect drops into the backpack.
            var tilePoints = targets.Select(t => new Microsoft.Xna.Framework.Point(
                (int)t.tile.X, (int)t.tile.Y));
            _follow.StartClearDebris(npcName, tilePoints, _inventory);

            Log.Log($"[npc_clear_debris] {npcName}: queued {targets.Count} debris targets", LogLevel.Info);
        }

        // ── helpers ───────────────────────────────────────────────────

        private static bool IsDebris(StardewValley.Object obj)
        {
            if (obj is null) return false;
            return obj.IsWeeds() || obj.IsTwig()
                || (obj.Category == StardewValley.Object.litterCategory)
                || (obj.Name == "Stone" && obj.ParentSheetIndex >= 0);
        }

        private static string DebrisDropId(StardewValley.Object obj)
        {
            if (obj.IsTwig())  return "(O)388"; // Wood
            if (obj.IsWeeds()) return "(O)771"; // Mixed Seeds
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

        // Parse x1/y1/x2/y2 bbox from params. Returns (on, x1, y1, x2, y2).
        // bbox is "on" only when all 4 fields are non-zero AND form a valid
        // rectangle (x1 <= x2, y1 <= y2). Used by all behavior handlers to
        // accept a bbox from npc_inspect_object's farm_actions output.
        internal static (bool on, int x1, int y1, int x2, int y2) ParseBBox(JsonElement p)
        {
            int x1 = 0, y1 = 0, x2 = 0, y2 = 0;
            if (p.ValueKind != JsonValueKind.Object) return (false, 0, 0, 0, 0);
            if (p.TryGetProperty("x1", out var e) && e.TryGetInt32(out int v)) x1 = v;
            if (p.TryGetProperty("y1", out e) && e.TryGetInt32(out v)) y1 = v;
            if (p.TryGetProperty("x2", out e) && e.TryGetInt32(out v)) x2 = v;
            if (p.TryGetProperty("y2", out e) && e.TryGetInt32(out v)) y2 = v;
            bool on = x1 != 0 && y1 != 0 && x2 != 0 && y2 != 0 && x1 <= x2 && y1 <= y2;
            return (on, x1, y1, x2, y2);
        }
    }

    internal sealed class WaterCropsHandler : NpcActionHandlerBase
    {
        private readonly FollowSystem _follow;

        protected override string ActionName => "npc_water_crops";

        public WaterCropsHandler(IMonitor log, Func<bool> showBubble, FollowSystem follow)
            : base(log, showBubble)
        {
            _follow = follow;
        }

        /// <summary>Public entry for debug commands running on the game thread.</summary>
        public void ExecuteDebug(NPC npc, string npcName, JsonElement @params)
            => Execute(npc, npcName, @params);

        protected override string ResolveBubble(JsonElement @params)
        {
            int radius   = ParseInt(@params, "radius",    5, 1, 30);
            int maxCount = ParseInt(@params, "max_count", 5, 1, 20);
            return $"[浇水] r={radius} max={maxCount}";
        }

        protected override void Execute(NPC npc, string npcName, JsonElement @params)
        {
            int radius   = ParseInt(@params, "radius",    5, 1, 30);
            int maxCount = ParseInt(@params, "max_count", 5, 1, 20);
            var (bboxOn, x1, y1, x2, y2) = ClearDebrisHandler.ParseBBox(@params);

            var location = npc.currentLocation;
            if (location is null) return;

            // Scan for unwatered HoeDirt with crops within bbox or radius.
            var npcTile = npc.Tile;
            var targets = new System.Collections.Generic.List<(Microsoft.Xna.Framework.Vector2 tile, float dist)>();

            foreach (var kv in location.terrainFeatures.Pairs)
            {
                var tile = kv.Key;
                if (kv.Value is not StardewValley.TerrainFeatures.HoeDirt dirt) continue;
                if (dirt.crop == null) continue;
                if (!dirt.needsWatering()) continue;

                if (bboxOn)
                {
                    if (tile.X < x1 || tile.X > x2 || tile.Y < y1 || tile.Y > y2) continue;
                }
                else
                {
                    float dist = Microsoft.Xna.Framework.Vector2.Distance(npcTile, tile);
                    if (dist > radius) continue;
                }
                float d = Microsoft.Xna.Framework.Vector2.Distance(npcTile, tile);
                targets.Add((tile, d));
            }

            targets.Sort((a, b) => a.dist.CompareTo(b.dist));
            if (targets.Count > maxCount) targets = targets.GetRange(0, maxCount);

            if (targets.Count == 0)
            {
                string scope = bboxOn ? $"bbox=({x1},{y1})-({x2},{y2})" : $"radius={radius}";
                Log.Log($"[npc_water_crops] {npcName}: no unwatered crops in {scope}", LogLevel.Info);
                return;
            }

            var tilePoints = targets.Select(t => new Microsoft.Xna.Framework.Point((int)t.tile.X, (int)t.tile.Y));
            _follow.StartWaterCrops(npcName, tilePoints);

            Log.Log($"[npc_water_crops] {npcName}: queued {targets.Count} tiles to water", LogLevel.Info);
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

    internal sealed class HarvestCropsHandler : NpcActionHandlerBase
    {
        private readonly NpcInventory _inventory;
        private readonly FollowSystem _follow;

        protected override string ActionName => "npc_harvest_crops";

        public HarvestCropsHandler(IMonitor log, Func<bool> showBubble, NpcInventory inventory, FollowSystem follow)
            : base(log, showBubble)
        {
            _inventory = inventory;
            _follow    = follow;
        }

        /// <summary>Public entry for debug commands running on the game thread.</summary>
        public void ExecuteDebug(NPC npc, string npcName, JsonElement @params)
            => Execute(npc, npcName, @params);

        protected override string ResolveBubble(JsonElement @params)
        {
            int radius   = ParseInt(@params, "radius",    5, 1, 30);
            int maxCount = ParseInt(@params, "max_count", 5, 1, 10);
            return $"[收获] r={radius} max={maxCount}";
        }

        protected override void Execute(NPC npc, string npcName, JsonElement @params)
        {
            int radius   = ParseInt(@params, "radius",    5, 1, 30);
            int maxCount = ParseInt(@params, "max_count", 5, 1, 10);
            var (bboxOn, x1, y1, x2, y2) = ClearDebrisHandler.ParseBBox(@params);

            var location = npc.currentLocation;
            if (location is null) return;

            // Scan for mature crops on HoeDirt within bbox or radius.
            var npcTile = npc.Tile;
            var targets = new System.Collections.Generic.List<(Microsoft.Xna.Framework.Vector2 tile, float dist)>();

            foreach (var kv in location.terrainFeatures.Pairs)
            {
                var tile = kv.Key;
                if (kv.Value is not StardewValley.TerrainFeatures.HoeDirt dirt) continue;
                if (dirt.crop == null) continue;
                if (dirt.crop.dead.Value) continue;
                // Harvestable: crop must be in its final growth phase.
                // We don't check fullyGrown here because some crops (e.g. pumpkin)
                // spend multiple days in the final phase before fullyGrown flips.
                // crop.harvest() itself does the final fullyGrown gate.
                if (dirt.crop.currentPhase.Value < dirt.crop.phaseDays.Count - 1) continue;

                if (bboxOn)
                {
                    if (tile.X < x1 || tile.X > x2 || tile.Y < y1 || tile.Y > y2) continue;
                }
                else
                {
                    float dist = Microsoft.Xna.Framework.Vector2.Distance(npcTile, tile);
                    if (dist > radius) continue;
                }
                float d = Microsoft.Xna.Framework.Vector2.Distance(npcTile, tile);
                targets.Add((tile, d));
            }

            targets.Sort((a, b) => a.dist.CompareTo(b.dist));
            if (targets.Count > maxCount) targets = targets.GetRange(0, maxCount);

            if (targets.Count == 0)
            {
                string scope = bboxOn ? $"bbox=({x1},{y1})-({x2},{y2})" : $"radius={radius}";
                Log.Log($"[npc_harvest_crops] {npcName}: no mature crops in {scope}", LogLevel.Info);
                return;
            }

            var tilePoints = targets.Select(t => new Microsoft.Xna.Framework.Point((int)t.tile.X, (int)t.tile.Y));
            _follow.StartHarvestCrops(npcName, tilePoints, _inventory);

            Log.Log($"[npc_harvest_crops] {npcName}: queued {targets.Count} crops to harvest", LogLevel.Info);
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
            // Parse parameters.
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

            // Early exit: nothing to deposit?
            var items = _inventory.GetItems(npcName);
            IReadOnlyList<ItemSlot> filtered = itemIds != null
                ? items.Where(s => itemIds.Contains(s.ItemId, StringComparer.OrdinalIgnoreCase)).ToList()
                : items;

            if (filtered.Count == 0)
            {
                Log.Log($"[npc_deposit_items] {npcName}: backpack empty (or filter matched nothing)", LogLevel.Info);
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
                Log.Log($"[npc_deposit_items] {npcName}: queued deposit autoFind={autoFind} chest=({chestX},{chestY})", LogLevel.Info);
        }
    }

    internal sealed class DeliverItemsHandler : NpcActionHandlerBase
    {
        private readonly NpcInventory _inventory;
        private readonly FollowSystem _follow;

        protected override string ActionName => "npc_deliver_items";

        public DeliverItemsHandler(IMonitor log, Func<bool> showBubble, NpcInventory inventory, FollowSystem follow)
            : base(log, showBubble)
        {
            _inventory = inventory;
            _follow    = follow;
        }

        /// <summary>Public entry for debug commands running on the game thread.</summary>
        public void ExecuteDebug(NPC npc, string npcName, JsonElement @params)
            => Execute(npc, npcName, @params);

        protected override string ResolveBubble(JsonElement @params)
        {
            return "[送货]";
        }

        protected override void Execute(NPC npc, string npcName, JsonElement @params)
        {
            var items = _inventory.GetItems(npcName);
            if (items.Count == 0)
            {
                Log.Log($"[npc_deliver_items] {npcName}: backpack empty, nothing to deliver", LogLevel.Info);
                return;
            }

            _follow.StartDeliverItems(npcName, _inventory);
            Log.Log($"[npc_deliver_items] {npcName}: queued delivery of {items.Count} item types", LogLevel.Info);
        }
    }

    internal sealed class ForageCollectHandler : NpcActionHandlerBase
    {
        private readonly NpcInventory _inventory;
        private readonly FollowSystem _follow;

        protected override string ActionName => "npc_forage_collect";

        public ForageCollectHandler(IMonitor log, Func<bool> showBubble, NpcInventory inventory, FollowSystem follow)
            : base(log, showBubble)
        {
            _inventory = inventory;
            _follow    = follow;
        }

        protected override string ResolveBubble(JsonElement @params)
        {
            int radius   = ParseInt(@params, "radius",    8, 1, 30);
            int maxCount = ParseInt(@params, "max_count", 3, 1, 10);
            return $"[采集] r={radius} max={maxCount}";
        }

        protected override void Execute(NPC npc, string npcName, JsonElement @params)
        {
            int radius   = ParseInt(@params, "radius",    8, 1, 30);
            int maxCount = ParseInt(@params, "max_count", 3, 1, 10);
            var (bboxOn, x1, y1, x2, y2) = ClearDebrisHandler.ParseBBox(@params);

            var location = npc.currentLocation;
            if (location is null) return;

            var npcTile = npc.Tile;
            var targets = new List<(Microsoft.Xna.Framework.Vector2 tile, string itemId, string itemName)>();

            foreach (var kv in location.Objects.Pairs)
            {
                var tile = kv.Key;
                var obj  = kv.Value;
                if (!obj.IsSpawnedObject) continue;

                if (bboxOn)
                {
                    if (tile.X < x1 || tile.X > x2 || tile.Y < y1 || tile.Y > y2) continue;
                }
                else
                {
                    float dist = Microsoft.Xna.Framework.Vector2.Distance(npcTile, tile);
                    if (dist > radius) continue;
                }
                targets.Add((tile, obj.ItemId, obj.DisplayName));
            }

            targets.Sort((a, b) =>
                Microsoft.Xna.Framework.Vector2.Distance(npcTile, a.tile)
                    .CompareTo(Microsoft.Xna.Framework.Vector2.Distance(npcTile, b.tile)));

            if (targets.Count > maxCount) targets = targets.GetRange(0, maxCount);

            if (targets.Count == 0)
            {
                string scope = bboxOn ? $"bbox=({x1},{y1})-({x2},{y2})" : $"radius={radius}";
                Log.Log($"[npc_forage_collect] {npcName}: no forage in {scope}", LogLevel.Info);
                return;
            }

            var forageTargets = targets.Select(t =>
                (new Microsoft.Xna.Framework.Point((int)t.tile.X, (int)t.tile.Y), t.itemId, t.itemName))
                .ToList();

            _follow.StartForageCollect(npcName, forageTargets, _inventory);
            Log.Log($"[npc_forage_collect] {npcName}: queued {targets.Count} forage targets", LogLevel.Info);
        }

        /// <summary>Public entry for debug commands running on the game thread.</summary>
        public void ExecuteDebug(NPC npc, string npcName, JsonElement @params)
            => Execute(npc, npcName, @params);

        private static int ParseInt(JsonElement p, string key, int def, int min, int max)
        {
            if (p.ValueKind == JsonValueKind.Object &&
                p.TryGetProperty(key, out JsonElement el) &&
                el.TryGetInt32(out int v))
                return System.Math.Clamp(v, min, max);
            return def;
        }
    }

    internal sealed class PetAnimalHandler : NpcActionHandlerBase
    {
        protected override string ActionName => "npc_pet_animal";
        public PetAnimalHandler(IMonitor log, Func<bool> showBubble) : base(log, showBubble) { }
        // TODO: find nearby animal and pet it
    }

    internal sealed class PlantSeedsHandler : NpcActionHandlerBase
    {
        private readonly NpcInventory _inventory;
        private readonly FollowSystem _follow;

        protected override string ActionName => "npc_plant_seeds";

        public PlantSeedsHandler(IMonitor log, Func<bool> showBubble, NpcInventory inventory, FollowSystem follow)
            : base(log, showBubble)
        {
            _inventory = inventory;
            _follow    = follow;
        }

        /// <summary>Public entry for debug commands running on the game thread.</summary>
        public void ExecuteDebug(NPC npc, string npcName, JsonElement @params)
            => Execute(npc, npcName, @params);

        protected override string ResolveBubble(JsonElement @params)
        {
            string? seedId = null;
            if (@params.ValueKind == JsonValueKind.Object &&
                @params.TryGetProperty("seed_id", out JsonElement s) &&
                s.ValueKind == JsonValueKind.String)
                seedId = s.GetString();

            int maxCount = ParseInt(@params, "max_count", 5, 1, 10);
            return seedId is not null
                ? $"[播种] {seedId} x{maxCount}"
                : "[播种]";
        }

        protected override void Execute(NPC npc, string npcName, JsonElement @params)
        {
            // Parse seed_id (required).
            string? seedId = null;
            if (@params.ValueKind == JsonValueKind.Object &&
                @params.TryGetProperty("seed_id", out JsonElement s) &&
                s.ValueKind == JsonValueKind.String)
                seedId = s.GetString();

            if (string.IsNullOrWhiteSpace(seedId))
            {
                Log.Log($"[npc_plant_seeds] {npcName}: seed_id is required", LogLevel.Warn);
                return;
            }

            int radius   = ParseInt(@params, "radius",    5, 1, 10);
            int maxCount = ParseInt(@params, "max_count", 5, 1, 10);

            var location = npc.currentLocation;
            if (location is null) return;

            // Map check: only farm-type maps.
            if (!IsPlantableMap(location))
            {
                Log.Log($"[npc_plant_seeds] {npcName}: map '{location.Name}' is not plantable (farm/greenhouse only)", LogLevel.Warn);
                return;
            }

            // Check inventory: does NPC have enough seeds?
            var items = _inventory.GetItems(npcName);
            var seedSlot = items.FirstOrDefault(s => s.ItemId == seedId);
            int available = seedSlot?.Count ?? 0;
            int toPlant = maxCount;
            if (available <= 0)
            {
                Log.Log($"[npc_plant_seeds] {npcName}: no {seedId} in backpack, planting without consuming seeds", LogLevel.Info);
            }
            else
            {
                toPlant = Math.Min(maxCount, available);
            }

            // Scan for empty HoeDirt tiles within radius.
            var npcTile = npc.Tile;
            var targets = new System.Collections.Generic.List<(Microsoft.Xna.Framework.Vector2 tile, float dist)>();
            int scanRange = Math.Max(radius, 1);

            for (int dx = -scanRange; dx <= scanRange; dx++)
            {
                for (int dy = -scanRange; dy <= scanRange; dy++)
                {
                    int tx = (int)npcTile.X + dx;
                    int ty = (int)npcTile.Y + dy;
                    var tileV2 = new Microsoft.Xna.Framework.Vector2(tx, ty);
                    float d = Microsoft.Xna.Framework.Vector2.Distance(npcTile, tileV2);
                    if (d > radius) continue;

                    // Check HoeDirt with no crop.
                    if (!location.terrainFeatures.TryGetValue(tileV2, out var tf)) continue;
                    if (tf is not StardewValley.TerrainFeatures.HoeDirt dirt) continue;
                    if (dirt.crop != null) continue;

                    targets.Add((tileV2, d));
                }
            }

            // Sort by distance and take top toPlant.
            targets.Sort((a, b) => a.dist.CompareTo(b.dist));
            if (targets.Count > toPlant) targets = targets.GetRange(0, toPlant);

            if (targets.Count == 0)
            {
                Log.Log($"[npc_plant_seeds] {npcName}: no empty HoeDirt in radius={radius}", LogLevel.Info);
                return;
            }

            var tilePoints = targets.Select(t => new Microsoft.Xna.Framework.Point((int)t.tile.X, (int)t.tile.Y));
            _follow.StartPlantSeeds(npcName, tilePoints, seedId, _inventory);

            Log.Log($"[npc_plant_seeds] {npcName}: queued {targets.Count} tiles to plant with {seedId} (seeds_in_backpack={available})", LogLevel.Info);
        }

        // ── helpers ───────────────────────────────────────────────────

        private static bool IsPlantableMap(GameLocation location)
        {
            if (location.IsFarm) return true;
            if (location.IsGreenhouse) return true;
            string name = location.NameOrUniqueName ?? location.Name ?? "";
            return name.Contains("Island", StringComparison.OrdinalIgnoreCase)
                && name.Contains("Farm",  StringComparison.OrdinalIgnoreCase);
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

    internal sealed class TillSoilHandler : NpcActionHandlerBase
    {
        private readonly FollowSystem _follow;

        protected override string ActionName => "npc_till_soil";

        public TillSoilHandler(IMonitor log, Func<bool> showBubble, FollowSystem follow)
            : base(log, showBubble)
        {
            _follow = follow;
        }

        /// <summary>Public entry for debug commands running on the game thread.</summary>
        public void ExecuteDebug(NPC npc, string npcName, JsonElement @params)
            => Execute(npc, npcName, @params);

        protected override string ResolveBubble(JsonElement @params)
        {
            int radius   = ParseInt(@params, "radius",    3, 1, 30);
            int maxCount = ParseInt(@params, "max_count", 5, 1, 15);
            return $"[翻地] r={radius} max={maxCount}";
        }

        protected override void Execute(NPC npc, string npcName, JsonElement @params)
        {
            int radius   = ParseInt(@params, "radius",    3, 1, 30);
            int maxCount = ParseInt(@params, "max_count", 5, 1, 15);
            var (bboxOn, bx1, by1, bx2, by2) = ClearDebrisHandler.ParseBBox(@params);

            var location = npc.currentLocation;
            if (location is null) return;

            // Only allow on farm-type maps to avoid creating HoeDirt on paths / indoors.
            if (!IsTillableMap(location))
            {
                Log.Log($"[npc_till_soil] {npcName}: map '{location.Name}' is not tillable (farm/greenhouse only)", LogLevel.Warn);
                return;
            }

            // Scan for empty diggable tiles within bbox or radius.
            var npcTile = npc.Tile;
            var targets = new System.Collections.Generic.List<(Microsoft.Xna.Framework.Vector2 tile, float dist)>();

            int sx1, sy1, sx2, sy2;
            if (bboxOn)
            {
                sx1 = bx1; sy1 = by1; sx2 = bx2; sy2 = by2;
            }
            else
            {
                int scanRange = Math.Max(radius, 1);
                sx1 = (int)npcTile.X - scanRange;
                sy1 = (int)npcTile.Y - scanRange;
                sx2 = (int)npcTile.X + scanRange;
                sy2 = (int)npcTile.Y + scanRange;
            }

            for (int tx = sx1; tx <= sx2; tx++)
            {
                for (int ty = sy1; ty <= sy2; ty++)
                {
                    var tileV2 = new Microsoft.Xna.Framework.Vector2(tx, ty);
                    float d = Microsoft.Xna.Framework.Vector2.Distance(npcTile, tileV2);
                    if (!bboxOn && d > radius) continue;

                    // Skip occupied tiles.
                    if (location.Objects.ContainsKey(tileV2)) continue;
                    if (location.terrainFeatures.ContainsKey(tileV2)) continue;
                    // Must be passable (not water, cliff, building, etc.).
                    if (!location.isTilePassable(
                        new xTile.Dimensions.Location(tx, ty), Game1.viewport)) continue;
                    // Must be diggable per the map's Back layer property.
                    if (location.doesTileHaveProperty(tx, ty, "Diggable", "Back") != "T") continue;

                    targets.Add((tileV2, d));
                }
            }

            // Sort by distance and take top maxCount.
            targets.Sort((a, b) => a.dist.CompareTo(b.dist));
            if (targets.Count > maxCount) targets = targets.GetRange(0, maxCount);

            if (targets.Count == 0)
            {
                string scope = bboxOn ? $"bbox=({bx1},{by1})-({bx2},{by2})" : $"radius={radius}";
                Log.Log($"[npc_till_soil] {npcName}: no empty diggable tiles in {scope}", LogLevel.Info);
                return;
            }

            var tilePoints = targets.Select(t => new Microsoft.Xna.Framework.Point((int)t.tile.X, (int)t.tile.Y));
            _follow.StartTillSoil(npcName, tilePoints);

            Log.Log($"[npc_till_soil] {npcName}: queued {targets.Count} tiles to till", LogLevel.Info);
        }

        // ── helpers ───────────────────────────────────────────────────

        private static bool IsTillableMap(GameLocation location)
        {
            if (location.IsFarm) return true;
            if (location.IsGreenhouse) return true;
            // Also allow Island farm maps (e.g. IslandWest).
            string name = location.NameOrUniqueName ?? location.Name ?? "";
            return name.Contains("Island", StringComparison.OrdinalIgnoreCase)
                && name.Contains("Farm",  StringComparison.OrdinalIgnoreCase);
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

    internal sealed class InspectObjectHandler : NpcActionHandlerBase
    {
        protected override string ActionName => "npc_inspect_object";
        public InspectObjectHandler(IMonitor log, Func<bool> showBubble) : base(log, showBubble) { }

        /// <summary>Public entry for debug commands running on the game thread.</summary>
        public object ExecuteDebug(NPC npc, string npcName, JsonElement @params)
            => GetResult(npc, npcName, @params)!;

        protected override string ResolveBubble(JsonElement @params)
        {
            int radius = ParseInt(@params, "radius", 0, 0, 30);
            string what = ParseWhat(@params);
            return radius > 0 ? $"[观察] {what} r={radius}" : "[观察]";
        }

        protected override object? GetResult(NPC npc, string npcName, JsonElement @params)
        {
            var location = npc.currentLocation;
            if (location is null) return Error("no_location", "NPC has no current location");

            int cx = (int)npc.Tile.X;
            int cy = (int)npc.Tile.Y;
            if (@params.ValueKind == JsonValueKind.Object)
            {
                if (@params.TryGetProperty("x", out var ex) && ex.TryGetInt32(out int px)) cx = px;
                if (@params.TryGetProperty("y", out var ey) && ey.TryGetInt32(out int py)) cy = py;
            }

            int radius    = ParseInt(@params, "radius", 0, 0, 30);
            string what   = ParseWhat(@params);

            // Facade: NPC looks toward the center tile.
            npc.faceDirection(2);

            // ── farm_actions mode ────────────────────────────────────
            // Aggregated action-plan output: 6 groups, each with count + bbox.
            // Designed for the agent to pick a high-level action and feed the
            // bbox back to the matching behavior tool without needing per-tile
            // coordinates.
            if (what == "farm_actions")
            {
                return BuildFarmActionsResult(npc, npcName, location, cx, cy, radius);
            }

            bool wantCrops   = what == "crops"   || what == "all";
            bool wantObjects = what == "objects" || what == "all";
            bool wantTerrain = what == "terrain" || what == "all";

            // ── Scan ──────────────────────────────────────────────────
            int scanned = 0;
            var matureCrops    = new List<object>();
            var unwateredCrops = new List<object>();
            var growingCrops   = new List<object>();
            var emptyHoeDirt   = new List<object>();
            var objects        = new List<object>();
            var terrainItems   = new List<object>();
            int totalUnwatered = 0;
            int totalEmptyHoe  = 0;

            int scanRange = Math.Max(radius, 0);
            for (int dx = -scanRange; dx <= scanRange; dx++)
            {
                for (int dy = -scanRange; dy <= scanRange; dy++)
                {
                    int tx = cx + dx;
                    int ty = cy + dy;
                    float d = Microsoft.Xna.Framework.Vector2.Distance(
                        new Vector2(cx, cy), new Vector2(tx, ty));
                    if (radius > 0 && d > radius) continue;
                    scanned++;

                    var tileV2 = new Vector2(tx, ty);

                    // Crops / terrain features.
                    if (wantCrops && location.terrainFeatures.TryGetValue(tileV2, out var tf)
                        && tf is StardewValley.TerrainFeatures.HoeDirt dirt)
                    {
                        if (dirt.crop != null)
                        {
                            var cd = ItemRegistry.GetData(dirt.crop.indexOfHarvest.Value);
                            string cn = cd?.DisplayName ?? dirt.crop.indexOfHarvest.Value;
                            string cid = dirt.crop.indexOfHarvest.Value;
                            int phase = (int)dirt.crop.currentPhase.Value;
                            int phases = dirt.crop.phaseDays.Count;

                            if (dirt.crop.fullyGrown.Value)
                            {
                                matureCrops.Add(new { x = tx, y = ty, crop = cn, id = cid });
                            }
                            else
                            {
                                growingCrops.Add(new { x = tx, y = ty, crop = cn, id = cid, phase, phases });
                            }

                            if (dirt.needsWatering())
                            {
                                totalUnwatered++;
                                unwateredCrops.Add(new { x = tx, y = ty, crop = cn });
                            }
                        }
                        else
                        {
                            totalEmptyHoe++;
                            emptyHoeDirt.Add(new { x = tx, y = ty });
                        }
                    }

                    // Objects.
                    if (wantObjects && location.Objects.TryGetValue(tileV2, out var obj) && obj != null)
                    {
                        objects.Add(new { x = tx, y = ty, name = obj.DisplayName, id = obj.ItemId });
                    }

                    // Other terrain (non-HoeDirt).
                    if (wantTerrain && location.terrainFeatures.TryGetValue(tileV2, out var tf2)
                        && tf2 is not StardewValley.TerrainFeatures.HoeDirt)
                    {
                        string tn = tf2.GetType().Name;
                        terrainItems.Add(new { x = tx, y = ty, type = tn });
                    }
                }
            }

            // ── Build summary ─────────────────────────────────────────
            var parts = new System.Collections.Generic.List<string>();
            if (matureCrops.Count    > 0) parts.Add($"{matureCrops.Count} mature crops");
            if (growingCrops.Count   > 0) parts.Add($"{growingCrops.Count} growing");
            if (totalUnwatered       > 0) parts.Add($"{totalUnwatered} unwatered");
            if (totalEmptyHoe        > 0) parts.Add($"{totalEmptyHoe} empty HoeDirt");
            if (objects.Count        > 0) parts.Add($"{objects.Count} objects");
            if (terrainItems.Count   > 0) parts.Add($"{terrainItems.Count} terrain features");
            string summary = parts.Count > 0
                ? string.Join(", ", parts)
                : "nothing of interest";

            var result = new Dictionary<string, object>
            {
                ["ok"]           = true,
                ["npc"]          = npcName,
                ["center"]       = new { x = cx, y = cy },
                ["radius"]       = radius,
                ["tiles_scanned"] = scanned,
                ["summary"]      = summary,
                ["location"]     = location.Name ?? "",
                ["season"]       = Game1.currentSeason ?? "",
            };

            if (matureCrops.Count    > 0) result["mature_crops"]     = matureCrops;
            if (growingCrops.Count   > 0) result["growing_crops"]    = growingCrops;
            if (totalUnwatered       > 0) result["unwatered_crops"]  = totalUnwatered;
            if (totalEmptyHoe        > 0) result["empty_hoedirt"]    = totalEmptyHoe;
            if (objects.Count        > 0) result["objects"]          = objects;
            if (terrainItems.Count   > 0) result["terrain"]          = terrainItems;

            Log.Log(
                $"[npc_inspect_object] {npcName}: center=({cx},{cy}) radius={radius} what={what} scanned={scanned} → {summary}",
                LogLevel.Info);

            return result;
        }

        // ── farm_actions mode ────────────────────────────────────────
        //
        // Single sweep over the radius (or whole-tile rectangle) classifying
        // every tile into one of 6 buckets:
        //
        //   harvest — HoeDirt with crop in its final growth phase
        //   water   — HoeDirt with crop that needsWatering()
        //   clear   — Object on tile that is debris (weeds/twig/litter/small stone)
        //   till    — passable, Diggable=T, no Object, no terrain feature
        //   forage  — Object with IsSpawnedObject=true
        //   plant   — HoeDirt without a crop
        //
        // Each non-empty bucket gets a count + axis-aligned bbox the agent
        // can feed back as x1/y1/x2/y2 to the matching behavior tool.
        private object BuildFarmActionsResult(NPC npc, string npcName, GameLocation location,
            int cx, int cy, int radius)
        {
            // Per-bucket aggregators: count + bbox bounds + (harvest only) per-crop tally.
            var buckets = new Dictionary<string, (int count, int minX, int minY, int maxX, int maxY)>();
            void Hit(string key, int x, int y)
            {
                if (buckets.TryGetValue(key, out var v))
                {
                    buckets[key] = (v.count + 1,
                        Math.Min(v.minX, x), Math.Min(v.minY, y),
                        Math.Max(v.maxX, x), Math.Max(v.maxY, y));
                }
                else
                {
                    buckets[key] = (1, x, y, x, y);
                }
            }

            // Per-crop tally for the harvest bucket: id → (name, count).
            var harvestCrops = new Dictionary<string, (string name, int count)>();

            int scanned = 0;
            int scanRange = Math.Max(radius, 0);
            for (int dx = -scanRange; dx <= scanRange; dx++)
            {
                for (int dy = -scanRange; dy <= scanRange; dy++)
                {
                    int tx = cx + dx;
                    int ty = cy + dy;
                    float d = Microsoft.Xna.Framework.Vector2.Distance(
                        new Vector2(cx, cy), new Vector2(tx, ty));
                    if (radius > 0 && d > radius) continue;
                    scanned++;

                    var tileV2 = new Vector2(tx, ty);

                    // ── HoeDirt: harvest / water / plant ─────────────
                    if (location.terrainFeatures.TryGetValue(tileV2, out var tf)
                        && tf is StardewValley.TerrainFeatures.HoeDirt dirt)
                    {
                        if (dirt.crop != null && !dirt.crop.dead.Value)
                        {
                            // Final phase = harvestable. Matches HarvestCropsHandler.
                            if (dirt.crop.currentPhase.Value >= dirt.crop.phaseDays.Count - 1)
                            {
                                Hit("harvest", tx, ty);
                                string cid = dirt.crop.indexOfHarvest.Value;
                                var cd = ItemRegistry.GetData(cid);
                                string cn = cd?.DisplayName ?? cid;
                                if (harvestCrops.TryGetValue(cid, out var hv))
                                    harvestCrops[cid] = (hv.name, hv.count + 1);
                                else
                                    harvestCrops[cid] = (cn, 1);
                            }

                            if (dirt.needsWatering())
                                Hit("water", tx, ty);
                        }
                        else if (dirt.crop == null)
                        {
                            Hit("plant", tx, ty);
                        }
                    }

                    // ── Objects: clear (debris) / forage (spawn) ─────
                    if (location.Objects.TryGetValue(tileV2, out var obj) && obj != null)
                    {
                        if (IsDebrisObj(obj))
                            Hit("clear", tx, ty);
                        if (obj.IsSpawnedObject)
                            Hit("forage", tx, ty);
                    }

                    // ── till: empty, passable, Diggable=T ────────────
                    if (!location.Objects.ContainsKey(tileV2)
                        && !location.terrainFeatures.ContainsKey(tileV2)
                        && location.isTilePassable(new xTile.Dimensions.Location(tx, ty), Game1.viewport)
                        && location.doesTileHaveProperty(tx, ty, "Diggable", "Back") == "T")
                    {
                        Hit("till", tx, ty);
                    }
                }
            }

            // ── Assemble actions_available map ─────────────────────────
            var actions = new Dictionary<string, object>();
            void AddActionGroup(string key)
            {
                if (!buckets.TryGetValue(key, out var v) || v.count == 0)
                {
                    // Always emit the key so the agent can see "0".
                    actions[key] = new { count = 0 };
                    return;
                }
                var bbox = new { x1 = v.minX, y1 = v.minY, x2 = v.maxX, y2 = v.maxY };
                if (key == "harvest" && harvestCrops.Count > 0)
                {
                    var crops = harvestCrops
                        .Select(kv => new { id = kv.Key, name = kv.Value.name, count = kv.Value.count })
                        .OrderByDescending(c => c.count)
                        .ToList();
                    actions[key] = new { count = v.count, bbox, crops };
                }
                else
                {
                    actions[key] = new { count = v.count, bbox };
                }
            }

            AddActionGroup("harvest");
            AddActionGroup("water");
            AddActionGroup("clear");
            AddActionGroup("till");
            AddActionGroup("forage");
            AddActionGroup("plant");

            // ── Summary ────────────────────────────────────────────────
            var parts = new List<string>();
            foreach (var key in new[] { "harvest", "water", "clear", "till", "forage", "plant" })
            {
                if (buckets.TryGetValue(key, out var v) && v.count > 0)
                    parts.Add($"{key}={v.count}");
            }
            string summary = parts.Count > 0 ? string.Join(" ", parts) : "no farm work available";

            var result = new Dictionary<string, object>
            {
                ["ok"]                = true,
                ["npc"]               = npcName,
                ["center"]            = new { x = cx, y = cy },
                ["radius"]            = radius,
                ["tiles_scanned"]     = scanned,
                ["summary"]           = summary,
                ["location"]          = location.Name ?? "",
                ["season"]            = Game1.currentSeason ?? "",
                ["actions_available"] = actions,
            };

            Log.Log(
                $"[npc_inspect_object] {npcName}: farm_actions center=({cx},{cy}) r={radius} scanned={scanned} → {summary}",
                LogLevel.Info);

            return result;
        }

        // Local debris classifier — mirrors ClearDebrisHandler.IsDebris so
        // farm_actions counts match what npc_clear_debris would actually
        // act on.
        private static bool IsDebrisObj(StardewValley.Object obj)
        {
            if (obj is null) return false;
            return obj.IsWeeds() || obj.IsTwig()
                || (obj.Category == StardewValley.Object.litterCategory)
                || (obj.Name == "Stone" && obj.ParentSheetIndex >= 0 && obj.ParentSheetIndex < 44);
        }

        private static object Error(string code, string message)
            => new { ok = false, error_code = code, message };

        private static string ParseWhat(JsonElement p)
        {
            if (p.ValueKind == JsonValueKind.Object &&
                p.TryGetProperty("what", out var w) &&
                w.ValueKind == JsonValueKind.String)
            {
                string s = w.GetString() ?? "";
                if (s == "crops" || s == "objects" || s == "terrain" || s == "farm_actions") return s;
            }
            return "all";
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

    internal sealed class PlaceObjectHandler : NpcActionHandlerBase
    {
        protected override string ActionName => "npc_place_object";
        public PlaceObjectHandler(IMonitor log, Func<bool> showBubble) : base(log, showBubble) { }
        // TODO: place item from inventory onto target tile
    }

    internal sealed class WithdrawFromChestHandler : NpcActionHandlerBase
    {
        private readonly NpcInventory _inventory;
        private readonly FollowSystem _follow;

        protected override string ActionName => "npc_withdraw_from_chest";

        public WithdrawFromChestHandler(IMonitor log, Func<bool> showBubble, NpcInventory inventory, FollowSystem follow)
            : base(log, showBubble)
        {
            _inventory = inventory;
            _follow    = follow;
        }

        protected override string ResolveBubble(JsonElement @params)
        {
            string? itemId = null;
            if (@params.ValueKind == JsonValueKind.Object &&
                @params.TryGetProperty("item_id", out JsonElement s) &&
                s.ValueKind == JsonValueKind.String)
                itemId = s.GetString();
            int count = ParseInt(@params, "count", 1, 1, 999);
            return itemId is not null
                ? $"[取物] {itemId} x{count}"
                : "[取物]";
        }

        protected override void Execute(NPC npc, string npcName, JsonElement @params)
        {
            string? itemId = null;
            if (@params.ValueKind == JsonValueKind.Object &&
                @params.TryGetProperty("item_id", out JsonElement s) &&
                s.ValueKind == JsonValueKind.String)
                itemId = s.GetString();

            if (string.IsNullOrWhiteSpace(itemId))
            {
                Log.Log($"[npc_withdraw_from_chest] {npcName}: item_id is required", LogLevel.Warn);
                return;
            }

            int count = ParseInt(@params, "count", 1, 1, 999);

            bool autoFind = true;
            int  chestX   = 0;
            int  chestY   = 0;
            string? map   = null;

            if (@params.ValueKind == JsonValueKind.Object)
            {
                if (@params.TryGetProperty("auto_find", out var af) && af.ValueKind == JsonValueKind.False)
                    autoFind = false;
                if (@params.TryGetProperty("chest_x", out var cx)) cx.TryGetInt32(out chestX);
                if (@params.TryGetProperty("chest_y", out var cy)) cy.TryGetInt32(out chestY);
                if (@params.TryGetProperty("map", out var mp) && mp.ValueKind == JsonValueKind.String)
                    map = mp.GetString();
            }

            if (chestX == 0 && chestY == 0) autoFind = true;

            bool started = _follow.StartWithdrawItems(
                npcName, npc,
                new Microsoft.Xna.Framework.Point(chestX, chestY),
                autoFind, map,
                itemId, count,
                _inventory);

            if (!started)
                Log.Log($"[npc_withdraw_from_chest] {npcName}: could not start (no chest found?)", LogLevel.Warn);
            else
                Log.Log($"[npc_withdraw_from_chest] {npcName}: queued withdraw {itemId} x{count} autoFind={autoFind}", LogLevel.Info);
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

    internal sealed class TransferItemHandler : NpcActionHandlerBase
    {
        private readonly NpcInventory _inventory;

        protected override string ActionName => "npc_transfer_item";

        public TransferItemHandler(IMonitor log, Func<bool> showBubble, NpcInventory inventory)
            : base(log, showBubble)
        {
            _inventory = inventory;
        }

        protected override string ResolveBubble(JsonElement @params)
        {
            string? toNpc = null;
            if (@params.ValueKind == JsonValueKind.Object &&
                @params.TryGetProperty("to_npc", out JsonElement s) &&
                s.ValueKind == JsonValueKind.String)
                toNpc = s.GetString();
            return toNpc is not null
                ? $"[转交] -> {toNpc}"
                : "[转交]";
        }

        protected override object? GetResult(NPC npc, string npcName, JsonElement @params)
        {
            string? toNpc = null;
            string? itemId = null;
            int count = 0;

            if (@params.ValueKind == JsonValueKind.Object)
            {
                if (@params.TryGetProperty("to_npc", out JsonElement tn) &&
                    tn.ValueKind == JsonValueKind.String)
                    toNpc = tn.GetString();
                if (@params.TryGetProperty("item_id", out JsonElement ii) &&
                    ii.ValueKind == JsonValueKind.String)
                    itemId = ii.GetString();
                if (@params.TryGetProperty("count", out JsonElement c))
                    c.TryGetInt32(out count);
            }

            if (string.IsNullOrWhiteSpace(toNpc) || string.IsNullOrWhiteSpace(itemId) || count <= 0)
                return new { ok = false, error_code = "invalid_params", message = "to_npc, item_id, and positive count are required" };

            // Check sender has enough.
            var fromItems = _inventory.GetItems(npcName);
            var slot = fromItems.FirstOrDefault(s => s.ItemId == itemId);
            int available = slot?.Count ?? 0;
            int toTransfer = Math.Min(count, available);

            if (toTransfer <= 0)
            {
                Log.Log($"[npc_transfer_item] {npcName} -> {toNpc}: no {itemId} in backpack", LogLevel.Warn);
                return new { ok = false, error_code = "insufficient_items", message = $"no {itemId} in {npcName}'s backpack" };
            }

            _inventory.Take(npcName, itemId, toTransfer);
            _inventory.Add(toNpc, itemId, toTransfer);

            Log.Log($"[npc_transfer_item] {npcName} -> {toNpc}: transferred {toTransfer}x {itemId}", LogLevel.Info);

            return new
            {
                ok = true,
                from_npc = npcName,
                to_npc = toNpc,
                item_id = itemId,
                transferred = toTransfer,
                message = $"transferred {toTransfer}x {itemId} from {npcName} to {toNpc}",
            };
        }
    }

    internal sealed class BreakResourceHandler : NpcActionHandlerBase
    {
        private readonly NpcInventory _inventory;
        private readonly FollowSystem  _follow;

        protected override string ActionName => "npc_break_resource";

        public BreakResourceHandler(IMonitor log, Func<bool> showBubble, NpcInventory inventory, FollowSystem follow)
            : base(log, showBubble)
        {
            _inventory = inventory;
            _follow    = follow;
        }

        protected override string ResolveBubble(JsonElement @params)
        {
            int radius   = ParseInt(@params, "radius",    6, 1, 15);
            int maxCount = ParseInt(@params, "max_count", 3, 1, 10);
            string what  = ParseWhat(@params);
            return $"[伐木] r={radius} max={maxCount} {what}";
        }

        protected override void Execute(NPC npc, string npcName, JsonElement @params)
        {
            int radius   = ParseInt(@params, "radius",    6, 1, 15);
            int maxCount = ParseInt(@params, "max_count", 3, 1, 10);
            string what  = ParseWhat(@params);
            bool wantTrees  = what == "trees" || what == "all";
            bool wantStones = what == "stones" || what == "all";

            var location = npc.currentLocation;
            if (location is null) return;

            var npcTile = npc.Tile;
            var targets = new System.Collections.Generic.List<(
                Microsoft.Xna.Framework.Point tile,
                bool isTree, bool isStump, string treeType, int stoneIndex,
                float dist
            )>();

            // ── Scan terrainFeatures for trees ──────────────────────
            if (wantTrees)
            {
                int scanRange = Math.Max(radius, 1);
                for (int dx = -scanRange; dx <= scanRange; dx++)
                {
                    for (int dy = -scanRange; dy <= scanRange; dy++)
                    {
                        int tx = (int)npcTile.X + dx;
                        int ty = (int)npcTile.Y + dy;
                        var tileV2 = new Microsoft.Xna.Framework.Vector2(tx, ty);
                        float d = Microsoft.Xna.Framework.Vector2.Distance(npcTile, tileV2);
                        if (d > radius) continue;

                        if (!location.terrainFeatures.TryGetValue(tileV2, out var tf)) continue;
                        if (tf is not StardewValley.TerrainFeatures.Tree tree) continue;

                        // Skip saplings (growthStage < 5).
                        if (tree.growthStage.Value < 5) continue;
                        // Skip tapped trees.
                        if (tree.tapped.Value) continue;

                        bool isStump = tree.stump.Value;
                        string treeType = tree.treeType.Value;

                        targets.Add((new Microsoft.Xna.Framework.Point(tx, ty),
                            true, isStump, treeType, 0, d));
                    }
                }
            }

            // ── Scan Objects for large stones ──────────────────────
            if (wantStones)
            {
                foreach (var kv in location.Objects.Pairs)
                {
                    var tileV2 = kv.Key;
                    var obj    = kv.Value;
                    if (obj is null) continue;
                    // Large stones (PSI >= 44). Small stones handled by clear_debris.
                    if (!(obj.Name == "Stone" && obj.ParentSheetIndex >= 44)) continue;

                    float d = Microsoft.Xna.Framework.Vector2.Distance(npcTile, tileV2);
                    if (d > radius) continue;

                    targets.Add((new Microsoft.Xna.Framework.Point((int)tileV2.X, (int)tileV2.Y),
                        false, false, "", obj.ParentSheetIndex, d));
                }
            }

            // ── Sort & cap ──────────────────────────────────────────
            targets.Sort((a, b) => a.dist.CompareTo(b.dist));
            if (targets.Count > maxCount) targets = targets.GetRange(0, maxCount);

            if (targets.Count == 0)
            {
                Log.Log($"[npc_break_resource] {npcName}: no {what} resources in radius={radius}", LogLevel.Info);
                return;
            }

            // ── Start FollowSystem ──────────────────────────────────
            var resourceTargets = targets.Select(t =>
                new ResourceTarget(t.tile, t.isTree, t.isStump, t.treeType, t.stoneIndex));
            _follow.StartBreakResource(npcName, resourceTargets, _inventory);

            Log.Log($"[npc_break_resource] {npcName}: queued {targets.Count} resources (trees={targets.Count(t => t.isTree)} stones={targets.Count(t => !t.isTree)})", LogLevel.Info);
        }

        // ── helpers ────────────────────────────────────────────────

        private static string ParseWhat(JsonElement p)
        {
            if (p.ValueKind == JsonValueKind.Object &&
                p.TryGetProperty("what", out var w) &&
                w.ValueKind == JsonValueKind.String)
            {
                string s = w.GetString() ?? "";
                if (s == "trees" || s == "stones") return s;
            }
            return "all";
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

    internal sealed class FertilizeHandler : NpcActionHandlerBase
    {
        private readonly NpcInventory _inventory;
        private readonly FollowSystem  _follow;

        protected override string ActionName => "npc_fertilize";

        public FertilizeHandler(IMonitor log, Func<bool> showBubble, NpcInventory inventory, FollowSystem follow)
            : base(log, showBubble)
        {
            _inventory = inventory;
            _follow    = follow;
        }

        protected override string ResolveBubble(JsonElement @params)
        {
            string? fertId = null;
            if (@params.ValueKind == JsonValueKind.Object &&
                @params.TryGetProperty("fertilizer_id", out JsonElement s) &&
                s.ValueKind == JsonValueKind.String)
                fertId = s.GetString();
            int maxCount = ParseInt(@params, "max_count", 5, 1, 15);
            return fertId is not null
                ? $"[施肥] {fertId} x{maxCount}"
                : "[施肥]";
        }

        protected override void Execute(NPC npc, string npcName, JsonElement @params)
        {
            string? fertId = null;
            if (@params.ValueKind == JsonValueKind.Object &&
                @params.TryGetProperty("fertilizer_id", out JsonElement s) &&
                s.ValueKind == JsonValueKind.String)
                fertId = s.GetString();

            if (string.IsNullOrWhiteSpace(fertId))
            {
                Log.Log($"[npc_fertilize] {npcName}: fertilizer_id is required", LogLevel.Warn);
                return;
            }

            int radius   = ParseInt(@params, "radius",    5, 1, 10);
            int maxCount = ParseInt(@params, "max_count", 5, 1, 15);

            var location = npc.currentLocation;
            if (location is null) return;

            if (!IsFarmMap(location))
            {
                Log.Log($"[npc_fertilize] {npcName}: map '{location.Name}' is not a farm", LogLevel.Warn);
                return;
            }

            // Scan for empty, unfertilized HoeDirt tiles.
            var npcTile = npc.Tile;
            var targets = new System.Collections.Generic.List<(Microsoft.Xna.Framework.Vector2 tile, float dist)>();
            int scanRange = Math.Max(radius, 1);

            for (int dx = -scanRange; dx <= scanRange; dx++)
            {
                for (int dy = -scanRange; dy <= scanRange; dy++)
                {
                    int tx = (int)npcTile.X + dx;
                    int ty = (int)npcTile.Y + dy;
                    var tileV2 = new Microsoft.Xna.Framework.Vector2(tx, ty);
                    float d = Microsoft.Xna.Framework.Vector2.Distance(npcTile, tileV2);
                    if (d > radius) continue;

                    if (!location.terrainFeatures.TryGetValue(tileV2, out var tf)) continue;
                    if (tf is not StardewValley.TerrainFeatures.HoeDirt dirt) continue;
                    if (dirt.crop != null) continue; // only empty tiles
                    if (!string.IsNullOrEmpty(dirt.fertilizer.Value)) continue; // already fertilized

                    targets.Add((tileV2, d));
                }
            }

            // Sort & cap.
            targets.Sort((a, b) => a.dist.CompareTo(b.dist));
            if (targets.Count > maxCount) targets = targets.GetRange(0, maxCount);

            if (targets.Count == 0)
            {
                Log.Log($"[npc_fertilize] {npcName}: no empty unfertilized HoeDirt in radius={radius}", LogLevel.Info);
                return;
            }

            // Inventory check (non-blocking, like plant_seeds).
            var items = _inventory.GetItems(npcName);
            var slot = items.FirstOrDefault(s => s.ItemId == fertId);
            int available = slot?.Count ?? 0;
            if (available <= 0)
                Log.Log($"[npc_fertilize] {npcName}: no {fertId} in backpack, fertilizing without consuming", LogLevel.Info);

            var tilePoints = targets.Select(t => new Microsoft.Xna.Framework.Point((int)t.tile.X, (int)t.tile.Y));
            _follow.StartFertilize(npcName, tilePoints, fertId, _inventory);

            Log.Log($"[npc_fertilize] {npcName}: queued {tilePoints.Count()} tiles to fertilize with {fertId} (in_backpack={available})", LogLevel.Info);
        }

        private static bool IsFarmMap(GameLocation location)
        {
            if (location.IsFarm) return true;
            if (location.IsGreenhouse) return true;
            string name = location.NameOrUniqueName ?? location.Name ?? "";
            return name.Contains("Island", StringComparison.OrdinalIgnoreCase)
                && name.Contains("Farm",  StringComparison.OrdinalIgnoreCase);
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
}
