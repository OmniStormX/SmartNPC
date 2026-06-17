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
        protected override bool RefuseWhileBusy => true;
        protected override bool IsPreemptable  => true;
        public WanderHandler(IMonitor log, Func<bool> showBubble, FollowSystem follow)
            : base(log, showBubble)
        {
            _follow = follow;
            SetBusyGate(follow);
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
        protected override bool RefuseWhileBusy => true;

        public ClearDebrisHandler(IMonitor log, Func<bool> showBubble, NpcInventory inventory, FollowSystem follow)
            : base(log, showBubble)
        {
            _inventory = inventory;
            _follow    = follow;
            SetBusyGate(follow);
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
            int maxCount = ParseInt(@params, "max_count", 10, 1, 9999);
            int extend   = ParseInt(@params, "extend",    0, 0, 10);
            var (bboxOn, x1, y1, x2, y2) = ParseBBox(@params);
            // Hard cap at 15 debris per call (~10-15s real time). bbox mode also respects it.
            const int CLEAR_HARD_CAP = 15;
            int effectiveCap = Math.Min(maxCount, CLEAR_HARD_CAP);

            var location = npc.currentLocation;
            if (location is null) return;

            // 1. First pass: walk the user-supplied region (bbox OR radius
            //    around NPC) and accumulate the bounding box of any HoeDirt
            //    tiles found. The "clear debris" intent on a farm map is
            //    actually "tidy the cropland", not "scrub every weed in
            //    range" — without this constraint a radius=30 sweep on Farm
            //    drags the NPC into trees/brush far from the planted area.
            //
            //    farmlandFound stays false on non-farm maps (forest, mine,
            //    etc.) — we keep the original sweep semantics there because
            //    the agent legitimately uses clear_debris to clean trails
            //    in those locations.
            var npcTile = npc.Tile;
            int sx1, sy1, sx2, sy2;
            if (bboxOn)
            {
                sx1 = x1; sy1 = y1; sx2 = x2; sy2 = y2;
            }
            else
            {
                int sr = Math.Max(radius, 1);
                sx1 = (int)npcTile.X - sr;
                sy1 = (int)npcTile.Y - sr;
                sx2 = (int)npcTile.X + sr;
                sy2 = (int)npcTile.Y + sr;
            }

            bool farmlandFound = false;
            int fx1 = int.MaxValue, fy1 = int.MaxValue, fx2 = int.MinValue, fy2 = int.MinValue;
            for (int tx = sx1; tx <= sx2; tx++)
            {
                for (int ty = sy1; ty <= sy2; ty++)
                {
                    var tv = new Microsoft.Xna.Framework.Vector2(tx, ty);
                    if (!bboxOn)
                    {
                        float dd = Microsoft.Xna.Framework.Vector2.Distance(npcTile, tv);
                        if (dd > radius) continue;
                    }
                    if (!location.terrainFeatures.TryGetValue(tv, out var tf)) continue;
                    if (tf is not StardewValley.TerrainFeatures.HoeDirt) continue;
                    farmlandFound = true;
                    if (tx < fx1) fx1 = tx;
                    if (ty < fy1) fy1 = ty;
                    if (tx > fx2) fx2 = tx;
                    if (ty > fy2) fy2 = ty;
                }
            }

            Log.Log(
                $"[npc_clear_debris] {npcName}: farmland bbox BEFORE extend: " +
                $"({fx1},{fy1})-({fx2},{fy2}) farmlandFound={farmlandFound}",
                LogLevel.Info);

            // Extend the farmland bbox outward so the NPC clears the perimeter
            // around the crop area, not just the dirt itself.
            if (farmlandFound && extend > 0)
            {
                fx1 = Math.Max(0, fx1 - extend);
                fy1 = Math.Max(0, fy1 - extend);
                fx2 = fx2 + extend;
                fy2 = fy2 + extend;

                Log.Log(
                    $"[npc_clear_debris] {npcName}: farmland bbox AFTER extend({extend}): " +
                    $"({fx1},{fy1})-({fx2},{fy2})",
                    LogLevel.Info);
            }

            // 2. Scan for clearable objects, restricted to the farmland
            //    bbox when one was found; otherwise the original user
            //    region (radius/bbox).
            var targets = new List<Microsoft.Xna.Framework.Vector2>();

            foreach (var kv in location.Objects.Pairs)
            {
                var tile = kv.Key;
                var obj  = kv.Value;
                if (!IsDebris(obj)) continue;
                if (bboxOn)
                {
                    // Agent explicitly chose this bbox — respect it.
                    if (tile.X < x1 || tile.X > x2 || tile.Y < y1 || tile.Y > y2) continue;
                }
                else if (farmlandFound)
                {
                    // Radius mode with farmland nearby — restrict to cropland.
                    if (tile.X < fx1 || tile.X > fx2 || tile.Y < fy1 || tile.Y > fy2) continue;
                }
                else
                {
                    float dist = Microsoft.Xna.Framework.Vector2.Distance(npcTile, tile);
                    if (dist > radius) continue;
                }
                targets.Add(tile);
            }

            // 2b. Scan terrainFeatures for tree stumps and saplings.
            foreach (var kv in location.terrainFeatures.Pairs)
            {
                var tile = kv.Key;
                var tf   = kv.Value;
                if (!IsTerrainDebris(tf)) continue;
                if (bboxOn)
                {
                    if (tile.X < x1 || tile.X > x2 || tile.Y < y1 || tile.Y > y2) continue;
                }
                else if (farmlandFound)
                {
                    if (tile.X < fx1 || tile.X > fx2 || tile.Y < fy1 || tile.Y > fy2) continue;
                }
                else
                {
                    float dist = Microsoft.Xna.Framework.Vector2.Distance(npcTile, tile);
                    if (dist > radius) continue;
                }
                targets.Add(tile);
            }

            // 2c. Scan resourceClumps for large stumps and hollow logs.
            if (location.resourceClumps != null)
            {
                for (int ci = location.resourceClumps.Count - 1; ci >= 0; ci--)
                {
                    var clump = location.resourceClumps[ci];
                    if (clump is null || !IsResourceClumpDebris(clump)) continue;
                    var ct = clump.Tile;
                    // Check if the clump's origin tile is within the search area.
                    bool inScope = false;
                    if (bboxOn)
                        inScope = ct.X >= x1 && ct.X <= x2 && ct.Y >= y1 && ct.Y <= y2;
                    else if (farmlandFound)
                        inScope = ct.X >= fx1 && ct.X <= fx2 && ct.Y >= fy1 && ct.Y <= fy2;
                    else
                        inScope = Microsoft.Xna.Framework.Vector2.Distance(npcTile,
                            new Microsoft.Xna.Framework.Vector2(ct.X, ct.Y)) <= radius;
                    if (inScope)
                        targets.Add(new Microsoft.Xna.Framework.Vector2(ct.X, ct.Y));
                }
            }

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

            // Visualise the actual area the NPC will work in: farmland bbox
            // when we narrowed; otherwise the agent-provided region.
            int ox1 = farmlandFound ? fx1 : (bboxOn ? x1 : (int)npcTile.X - radius);
            int oy1 = farmlandFound ? fy1 : (bboxOn ? y1 : (int)npcTile.Y - radius);
            int ox2 = farmlandFound ? fx2 : (bboxOn ? x2 : (int)npcTile.X + radius);
            int oy2 = farmlandFound ? fy2 : (bboxOn ? y2 : (int)npcTile.Y + radius);
            BBoxOverlay.Instance.MarkAction(npcName, location.Name ?? "", ox1, oy1, ox2, oy2);

            Log.Log(
                $"[npc_clear_debris] {npcName}: queued {targets.Count} debris targets " +
                (farmlandFound
                    ? $"(narrowed to farmland bbox ({fx1},{fy1})-({fx2},{fy2}))"
                    : "(no farmland nearby; using full input region)"),
                LogLevel.Info);
        }

        // ── helpers ───────────────────────────────────────────────────

        internal static bool IsDebris(StardewValley.Object obj)
        {
            if (obj is null) return false;
            return obj.IsWeeds() || obj.IsTwig()
                || (obj.Category == StardewValley.Object.litterCategory)
                || (obj.Name == "Stone" && obj.ParentSheetIndex >= 0);
        }

        internal static string DebrisDropId(StardewValley.Object obj)
        {
            if (obj.IsTwig())  return "(O)388"; // Wood
            if (obj.IsWeeds()) return "(O)771"; // Mixed Seeds
            return "(O)390";  // Stone
        }

        /// <summary>
        /// TerrainFeature counterpart of IsDebris: tree stumps, saplings,
        /// and tall grass are clearable; mature trees are not (they belong
        /// to npc_break_resource).
        /// </summary>
        internal static bool IsTerrainDebris(StardewValley.TerrainFeatures.TerrainFeature tf)
        {
            if (tf is StardewValley.TerrainFeatures.Tree tree)
            {
                if (tree.stump.Value) return true;
                if (tree.growthStage.Value < 5) return true;
            }
            if (tf is StardewValley.TerrainFeatures.Grass)
                return true;
            return false;
        }

        /// <summary>Drop id for terrain debris: stump→Hardwood, sapling→Wood, grass→Fiber.</summary>
        internal static string TerrainDebrisDropId(StardewValley.TerrainFeatures.TerrainFeature tf)
        {
            if (tf is StardewValley.TerrainFeatures.Tree tree && tree.stump.Value)
                return "(O)709"; // Hardwood
            if (tf is StardewValley.TerrainFeatures.Grass)
                return "(O)771"; // Fiber (Mixed Seeds equivalent)
            return "(O)388"; // Wood
        }

        /// <summary>
        /// Check if a ResourceClump is clearable debris (stump, hollow log).
        /// parentSheetIndex 600 = stump, 602 = hollow log.
        /// Boulders (622, 672, 752) and meteorites are break_resource targets, not debris.
        /// </summary>
        internal static bool IsResourceClumpDebris(StardewValley.TerrainFeatures.ResourceClump clump)
        {
            if (clump is null) return false;
            return clump.parentSheetIndex.Value == 600   // stump
                || clump.parentSheetIndex.Value == 602;  // hollow log
        }

        /// <summary>Drop id for resource clump debris.</summary>
        internal static string ResourceClumpDebrisDropId(StardewValley.TerrainFeatures.ResourceClump clump)
        {
            return "(O)709"; // Hardwood
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
        protected override bool RefuseWhileBusy => true;

        public WaterCropsHandler(IMonitor log, Func<bool> showBubble, FollowSystem follow)
            : base(log, showBubble)
        {
            _follow = follow;
            SetBusyGate(follow);
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
            int maxCount = ParseInt(@params, "max_count", 5, 1, 9999);
            var (bboxOn, x1, y1, x2, y2) = ClearDebrisHandler.ParseBBox(@params);
            int effectiveCap = bboxOn ? int.MaxValue : maxCount;

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
                // Authoritative "needs watering" check — state.Value != 1
                // (HoeDirt.watered constant). HoeDirt.needsWatering() in
                // SDV adds extra path-of-failure semantics that diverge
                // from what we want here (we treat dead crops as no-op
                // separately) so we read state directly to keep the
                // observe / select / execute layers consistent.
                if (dirt.crop.dead.Value) continue;
                if (dirt.state.Value == StardewValley.TerrainFeatures.HoeDirt.watered) continue;

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
            if (targets.Count > effectiveCap) targets = targets.GetRange(0, effectiveCap);

            if (targets.Count == 0)
            {
                string scope = bboxOn ? $"bbox=({x1},{y1})-({x2},{y2})" : $"radius={radius}";
                Log.Log($"[npc_water_crops] {npcName}: no unwatered crops in {scope}", LogLevel.Info);
                MarkNothingToDo($"no unwatered crops in {scope}");
                return;
            }

            // TSP-order the visit so a wide bbox doesn't zig-zag.
            var startPt = new Microsoft.Xna.Framework.Point((int)npcTile.X, (int)npcTile.Y);
            var tilePoints = PathPlanner.PlanBy(
                startPt, targets, t => new Microsoft.Xna.Framework.Point((int)t.tile.X, (int)t.tile.Y))
                .Select(t => new Microsoft.Xna.Framework.Point((int)t.tile.X, (int)t.tile.Y));
            _follow.StartWaterCrops(npcName, tilePoints);

            BBoxOverlay.Instance.MarkAction(npcName, location.Name ?? "",
                bboxOn ? x1 : (int)npcTile.X - radius,
                bboxOn ? y1 : (int)npcTile.Y - radius,
                bboxOn ? x2 : (int)npcTile.X + radius,
                bboxOn ? y2 : (int)npcTile.Y + radius);

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
        protected override bool RefuseWhileBusy => true;

        public HarvestCropsHandler(IMonitor log, Func<bool> showBubble, NpcInventory inventory, FollowSystem follow)
            : base(log, showBubble)
        {
            _inventory = inventory;
            _follow    = follow;
            SetBusyGate(follow);
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
            int maxCount = ParseInt(@params, "max_count", 5, 1, 9999);
            var (bboxOn, x1, y1, x2, y2) = ClearDebrisHandler.ParseBBox(@params);
            int effectiveCap = bboxOn ? int.MaxValue : maxCount;

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
                // Harvestable: final growth phase AND fullyGrown is true.
                //   - Single-harvest crops (parsnip, pumpkin): once mature, both
                //     conditions hold until harvested.
                //   - Multi-harvest crops (cranberry, blueberry, coffee): after
                //     a successful harvest SDV keeps currentPhase at final but
                //     resets fullyGrown=false during the regrow countdown.
                //     Skipping !fullyGrown stops the NPC from walking up to a
                //     not-yet-ripe-again berry bush only to bounce off.
                if (!IsCropHarvestable(dirt.crop)) continue;

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
            if (targets.Count > effectiveCap) targets = targets.GetRange(0, effectiveCap);

            if (targets.Count == 0)
            {
                string scope = bboxOn ? $"bbox=({x1},{y1})-({x2},{y2})" : $"radius={radius}";
                Log.Log($"[npc_harvest_crops] {npcName}: no mature crops in {scope}", LogLevel.Info);
                MarkNothingToDo($"no mature/ripe crops in {scope}");
                return;
            }

            var startPt = new Microsoft.Xna.Framework.Point((int)npcTile.X, (int)npcTile.Y);
            var tilePoints = PathPlanner.PlanBy(
                startPt, targets, t => new Microsoft.Xna.Framework.Point((int)t.tile.X, (int)t.tile.Y))
                .Select(t => new Microsoft.Xna.Framework.Point((int)t.tile.X, (int)t.tile.Y));
            _follow.StartHarvestCrops(npcName, tilePoints, _inventory);

            BBoxOverlay.Instance.MarkAction(npcName, location.Name ?? "",
                bboxOn ? x1 : (int)npcTile.X - radius,
                bboxOn ? y1 : (int)npcTile.Y - radius,
                bboxOn ? x2 : (int)npcTile.X + radius,
                bboxOn ? y2 : (int)npcTile.Y + radius);

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

        /// <summary>
        /// Single source of truth for "is this crop ready to pick right now?".
        /// Mirrors the precondition inside SDV's <c>Crop.harvest()</c>:
        ///
        ///   <c>!dead && currentPhase >= phaseDays.Count - 1
        ///      && (!fullyGrown || dayOfCurrentPhase &lt;= 0)</c>
        ///
        /// `fullyGrown` is NOT "ripe right now" — it's a sticky flag that
        /// flips to true the first time a crop reaches the final phase, and
        /// stays true forever after for multi-harvest crops. The actual
        /// gate is the disjunction:
        ///
        ///   - Single-harvest crops (parsnip, pumpkin, ...): once they
        ///     reach final phase, `fullyGrown` is still FALSE (SDV only
        ///     flips it to true after the first "ripen" tick). They pass
        ///     via the !fullyGrown branch.
        ///   - Multi-harvest crops first ripening (cranberry, blueberry):
        ///     fullyGrown=true, dayOfCurrentPhase=0 → pass via the
        ///     dayOfCurrentPhase&lt;=0 branch.
        ///   - Multi-harvest crops mid-regrow: fullyGrown=true,
        ///     dayOfCurrentPhase>0 → BOTH branches false → not ripe yet.
        ///     This is the case the agent kept walking up to before; the
        ///     bbox bbox-aware sweep now correctly skips them.
        ///
        /// Used by HarvestCropsHandler.Execute (target selection),
        /// InspectObjectHandler's farm_actions sweep (the harvest bucket
        /// + per-crop tally), and FollowSystem.TickHarvestCrops (per-tile
        /// retry gate) so all three layers agree.
        /// </summary>
        internal static bool IsCropHarvestable(StardewValley.Crop? crop)
        {
            if (crop is null) return false;
            if (crop.dead.Value) return false;
            if (crop.currentPhase.Value < crop.phaseDays.Count - 1) return false;
            return !crop.fullyGrown.Value || crop.dayOfCurrentPhase.Value <= 0;
        }
    }

    internal sealed class DepositItemsHandler : NpcActionHandlerBase
    {
        private readonly NpcInventory _inventory;
        private readonly FollowSystem _follow;

        protected override string ActionName => "npc_deposit_items";
        protected override bool RefuseWhileBusy => true;

        public DepositItemsHandler(IMonitor log, Func<bool> showBubble, NpcInventory inventory, FollowSystem follow)
            : base(log, showBubble)
        {
            _inventory = inventory;
            _follow    = follow;
            SetBusyGate(follow);
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
        protected override bool RefuseWhileBusy => true;

        public DeliverItemsHandler(IMonitor log, Func<bool> showBubble, NpcInventory inventory, FollowSystem follow)
            : base(log, showBubble)
        {
            _inventory = inventory;
            _follow    = follow;
            SetBusyGate(follow);
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
        protected override bool RefuseWhileBusy => true;

        public ForageCollectHandler(IMonitor log, Func<bool> showBubble, NpcInventory inventory, FollowSystem follow)
            : base(log, showBubble)
        {
            _inventory = inventory;
            _follow    = follow;
            SetBusyGate(follow);
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
            int maxCount = ParseInt(@params, "max_count", 3, 1, 9999);
            int extend   = ParseInt(@params, "extend",    0, 0, 10);
            var (bboxOn, x1, y1, x2, y2) = ClearDebrisHandler.ParseBBox(@params);
            int effectiveCap = bboxOn ? int.MaxValue : maxCount;

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

            if (targets.Count > effectiveCap) targets = targets.GetRange(0, effectiveCap);

            if (targets.Count == 0)
            {
                string scope = bboxOn ? $"bbox=({x1},{y1})-({x2},{y2})" : $"radius={radius}";
                Log.Log($"[npc_forage_collect] {npcName}: no forage in {scope}", LogLevel.Info);
                MarkNothingToDo($"no spawned forage in {scope}");
                return;
            }

            var startPt = new Microsoft.Xna.Framework.Point((int)npcTile.X, (int)npcTile.Y);
            var ordered = PathPlanner.PlanBy(
                startPt, targets, t => new Microsoft.Xna.Framework.Point((int)t.tile.X, (int)t.tile.Y));
            var forageTargets = ordered.Select(t =>
                (new Microsoft.Xna.Framework.Point((int)t.tile.X, (int)t.tile.Y), t.itemId, t.itemName))
                .ToList();

            _follow.StartForageCollect(npcName, forageTargets, _inventory);

            BBoxOverlay.Instance.MarkAction(npcName, location.Name ?? "",
                bboxOn ? x1 : (int)npcTile.X - radius,
                bboxOn ? y1 : (int)npcTile.Y - radius,
                bboxOn ? x2 : (int)npcTile.X + radius,
                bboxOn ? y2 : (int)npcTile.Y + radius);

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
        private readonly FollowSystem _follow;

        protected override string ActionName => "npc_pet_animal";
        protected override bool RefuseWhileBusy => true;

        public PetAnimalHandler(IMonitor log, Func<bool> showBubble, FollowSystem follow)
            : base(log, showBubble)
        {
            _follow = follow;
            SetBusyGate(follow);
        }

        protected override string ResolveBubble(JsonElement @params)
        {
            var pet = Game1.player?.getPet();
            return pet is not null ? $"[摸摸 {pet.Name}]" : "[摸摸宠物]";
        }

        protected override void Execute(NPC npc, string npcName, JsonElement @params)
        {
            // The schedule asks to pet the current player pet. We resolve
            // it via Game1.player.getPet() rather than taking an
            // animal_name param — there's only one player pet at a time
            // and the agent should not need to know its name. Anything
            // else (cows, chickens — `FarmAnimal`) is intentionally out
            // of scope; ranch animals have their own petting hooks the
            // engine handles when the player walks past them.
            var pet = Game1.player?.getPet();
            if (pet is null)
            {
                Log.Log($"[npc_pet_animal] {npcName}: player has no pet", LogLevel.Info);
                MarkNothingToDo("the player has no farm pet (cat/dog/turtle) to pet");
                return;
            }
            if (pet.currentLocation is null)
            {
                Log.Log($"[npc_pet_animal] {npcName}: pet has no currentLocation", LogLevel.Info);
                MarkNothingToDo($"pet {pet.Name} has no current location");
                return;
            }
            if (npc.currentLocation != pet.currentLocation)
            {
                string petMap = pet.currentLocation.Name ?? "<null>";
                string npcMap = npc.currentLocation?.Name ?? "<null>";
                Log.Log(
                    $"[npc_pet_animal] {npcName}: pet {pet.Name} on '{petMap}' but NPC on '{npcMap}'",
                    LogLevel.Info);
                MarkNothingToDo(
                    $"pet {pet.Name} is on map '{petMap}', not on this NPC's map '{npcMap}'. " +
                    "Skip this slot or run npc_summon for the NPC first.");
                return;
            }

            bool started = _follow.StartPetAnimal(npcName, npc);
            if (!started)
            {
                MarkNothingToDo("could not start pet-animal walk (pet unavailable)");
                return;
            }
            Log.Log(
                $"[npc_pet_animal] {npcName}: walking to pet {pet.Name} at {pet.Tile}",
                LogLevel.Info);
        }
    }

    internal sealed class PlantSeedsHandler : NpcActionHandlerBase
    {
        private readonly NpcInventory _inventory;
        private readonly FollowSystem _follow;

        protected override string ActionName => "npc_plant_seeds";
        protected override bool RefuseWhileBusy => true;

        public PlantSeedsHandler(IMonitor log, Func<bool> showBubble, NpcInventory inventory, FollowSystem follow)
            : base(log, showBubble)
        {
            _inventory = inventory;
            _follow    = follow;
            SetBusyGate(follow);
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

            int maxCount = ParseInt(@params, "max_count", 5, 1, 9999);
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

            int radius   = ParseInt(@params, "radius",    5, 1, 30);
            int maxCount = ParseInt(@params, "max_count", 5, 1, 9999);
            var (bboxOn, bx1, by1, bx2, by2) = ClearDebrisHandler.ParseBBox(@params);

            var location = npc.currentLocation;
            if (location is null) return;

            // Map check: only farm-type maps.
            if (!IsPlantableMap(location))
            {
                Log.Log($"[npc_plant_seeds] {npcName}: map '{location.Name}' is not plantable (farm/greenhouse only)", LogLevel.Warn);
                return;
            }

            // Check inventory: does NPC have enough seeds?
            // bbox mode ignores max_count (the rectangle is the cap), but the
            // backpack count still bounds work — can't plant more seeds than
            // we carry. Free-plant mode (no seeds in backpack) lifts that
            // bound; bbox just plants every empty tile.
            var items = _inventory.GetItems(npcName);
            var seedSlot = items.FirstOrDefault(s => s.ItemId == seedId);
            int available = seedSlot?.Count ?? 0;
            int effectiveCap;
            if (available <= 0)
            {
                Log.Log($"[npc_plant_seeds] {npcName}: no {seedId} in backpack, planting without consuming seeds", LogLevel.Info);
                effectiveCap = bboxOn ? int.MaxValue : maxCount;
            }
            else
            {
                effectiveCap = bboxOn ? available : Math.Min(maxCount, available);
            }

            // Scan for empty HoeDirt tiles within bbox or radius.
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

                    // Check HoeDirt with no crop.
                    if (!location.terrainFeatures.TryGetValue(tileV2, out var tf)) continue;
                    if (tf is not StardewValley.TerrainFeatures.HoeDirt dirt) continue;
                    if (dirt.crop != null) continue;

                    targets.Add((tileV2, d));
                }
            }

            // Sort by distance and take top effectiveCap.
            targets.Sort((a, b) => a.dist.CompareTo(b.dist));
            if (targets.Count > effectiveCap) targets = targets.GetRange(0, effectiveCap);

            if (targets.Count == 0)
            {
                string scope = bboxOn ? $"bbox=({bx1},{by1})-({bx2},{by2})" : $"radius={radius}";
                Log.Log($"[npc_plant_seeds] {npcName}: no empty HoeDirt in {scope}", LogLevel.Info);
                MarkNothingToDo($"no empty tilled soil in {scope}");
                return;
            }

            var startPt = new Microsoft.Xna.Framework.Point((int)npcTile.X, (int)npcTile.Y);
            var tilePoints = PathPlanner.PlanBy(
                startPt, targets, t => new Microsoft.Xna.Framework.Point((int)t.tile.X, (int)t.tile.Y))
                .Select(t => new Microsoft.Xna.Framework.Point((int)t.tile.X, (int)t.tile.Y));
            _follow.StartPlantSeeds(npcName, tilePoints, seedId, _inventory);

            BBoxOverlay.Instance.MarkAction(npcName, location.Name ?? "",
                bboxOn ? bx1 : (int)npcTile.X - radius,
                bboxOn ? by1 : (int)npcTile.Y - radius,
                bboxOn ? bx2 : (int)npcTile.X + radius,
                bboxOn ? by2 : (int)npcTile.Y + radius);

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
        protected override bool RefuseWhileBusy => true;

        public TillSoilHandler(IMonitor log, Func<bool> showBubble, FollowSystem follow)
            : base(log, showBubble)
        {
            _follow = follow;
            SetBusyGate(follow);
        }

        /// <summary>Public entry for debug commands running on the game thread.</summary>
        public void ExecuteDebug(NPC npc, string npcName, JsonElement @params)
            => Execute(npc, npcName, @params);

        protected override string ResolveBubble(JsonElement @params)
        {
            int patchW = ParseInt(@params, "patch_w", 10, 1, 12);
            int patchH = ParseInt(@params, "patch_h",  6, 1, 12);
            return $"[翻地] patch={patchW}x{patchH}";
        }

        // Per-thunk handoff to GetResult: the rectangle the planner chose.
        // Thunks run serially on the game thread, so a plain field is fine.
        // Reset at the start of every Execute.
        private FarmlandExtensionPlanner.Plan? _lastPlan;

        protected override void Execute(NPC npc, string npcName, JsonElement @params)
        {
            _lastPlan = null;

            int radius = ParseInt(@params, "radius",  3, 1, 30);
            int patchW = ParseInt(@params, "patch_w", 10, 1, 12);
            int patchH = ParseInt(@params, "patch_h",  6, 1, 12);
            var (bboxOn, bx1, by1, bx2, by2) = ClearDebrisHandler.ParseBBox(@params);

            var location = npc.currentLocation;
            if (location is null) return;

            // Only allow on farm-type maps to avoid creating HoeDirt on paths / indoors.
            if (!IsTillableMap(location))
            {
                Log.Log($"[npc_till_soil] {npcName}: map '{location.Name}' is not tillable (farm/greenhouse only)", LogLevel.Warn);
                MarkNothingToDo($"map '{location.Name}' is not a farm/greenhouse");
                return;
            }

            // Compute the search bbox: use agent-supplied rectangle when set,
            // otherwise fall back to a square window around the NPC sized by
            // radius. The planner sees this as a hard outer bound.
            var npcTile = npc.Tile;
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

            var bbox = new Microsoft.Xna.Framework.Rectangle(
                sx1, sy1, sx2 - sx1 + 1, sy2 - sy1 + 1);
            var npcPt = new Microsoft.Xna.Framework.Point((int)npcTile.X, (int)npcTile.Y);

            // Single source of truth for "where to till": the planner picks
            // a regular patchW × patchH rectangle that hugs existing
            // HoeDirt when possible. Returns null if no such rectangle fits
            // anywhere in the bbox — could be because the bbox is full of
            // tiles, too small to fit the patch, or every candidate spot
            // is blocked by Objects / non-Diggable terrain.
            var plan = FarmlandExtensionPlanner.Plan_(location, bbox, npcPt, patchW, patchH);
            if (!plan.HasValue)
            {
                string scope = bboxOn ? $"bbox=({bx1},{by1})-({bx2},{by2})" : $"radius={radius}";
                Log.Log(
                    $"[npc_till_soil] {npcName}: no {patchW}x{patchH} patch fits in {scope}",
                    LogLevel.Info);
                MarkNothingToDo($"no {patchW}x{patchH} farmland-adjacent patch fits in {scope}");
                return;
            }

            _lastPlan = plan;
            var rect = plan.Value.Rect;

            // ── Pre-clear phase: remove ALL debris within the patch rect ─
            // before the NPC starts walking. This guarantees the till area
            // is clean regardless of what farm_actions reported. Runs
            // synchronously on the game thread — safe for world mutation.
            var preClearCount = 0;
            for (int dx = 0; dx < rect.Width; dx++)
            {
                for (int dy = 0; dy < rect.Height; dy++)
                {
                    var tv = new Microsoft.Xna.Framework.Vector2(rect.X + dx, rect.Y + dy);
                    if (location.Objects.TryGetValue(tv, out var dobj) && dobj != null
                        && ClearDebrisHandler.IsDebris(dobj))
                    {
                        location.Objects.Remove(tv);
                        preClearCount++;
                    }
                    if (location.terrainFeatures.TryGetValue(tv, out var dtf) && dtf != null
                        && ClearDebrisHandler.IsTerrainDebris(dtf))
                    {
                        location.terrainFeatures.Remove(tv);
                        preClearCount++;
                    }
                }
            }
            // ResourceClumps that overlap the patch rect.
            if (location.resourceClumps != null)
            {
                for (int ri = location.resourceClumps.Count - 1; ri >= 0; ri--)
                {
                    var rc = location.resourceClumps[ri];
                    if (rc is null || !ClearDebrisHandler.IsResourceClumpDebris(rc)) continue;
                    var rct = rc.Tile;
                    // Check if the clump bounding box intersects the patch rect.
                    if (rct.X + rc.width.Value > rect.X && rct.X < rect.X + rect.Width
                        && rct.Y + rc.height.Value > rect.Y && rct.Y < rect.Y + rect.Height)
                    {
                        location.resourceClumps.RemoveAt(ri);
                        preClearCount++;
                    }
                }
            }
            if (preClearCount > 0)
            {
                Log.Log(
                    $"[npc_till_soil] {npcName}: pre-cleared {preClearCount} debris in patch " +
                    $"({rect.X},{rect.Y})-({rect.X + rect.Width - 1},{rect.Y + rect.Height - 1})",
                    LogLevel.Info);
            }

            // Materialise patch tiles and TSP-order them so the NPC walks
            // a clean serpentine through the rectangle instead of zigzagging.
            var patchTiles = new System.Collections.Generic.List<Microsoft.Xna.Framework.Point>(rect.Width * rect.Height);
            for (int dx = 0; dx < rect.Width; dx++)
            {
                for (int dy = 0; dy < rect.Height; dy++)
                    patchTiles.Add(new Microsoft.Xna.Framework.Point(rect.X + dx, rect.Y + dy));
            }
            var tilePoints = PathPlanner.PlanBy(npcPt, patchTiles, p => p);
            _follow.StartTillSoil(npcName, tilePoints,
                preClearTiles: plan.Value.TilesToClear,
                preBreakTiles: plan.Value.TilesToBreak);

            BBoxOverlay.Instance.MarkAction(npcName, location.Name ?? "",
                rect.X, rect.Y, rect.X + rect.Width - 1, rect.Y + rect.Height - 1);

            Log.Log(
                $"[npc_till_soil] {npcName}: tilling {patchW}x{patchH} patch at " +
                $"({rect.X},{rect.Y})-({rect.X + rect.Width - 1},{rect.Y + rect.Height - 1}) " +
                $"adjacent={plan.Value.AdjacentToExisting} edge={plan.Value.AdjacencyLen}",
                LogLevel.Info);
        }

        protected override object? GetResult(NPC npc, string npcName, JsonElement @params)
        {
            if (!_lastPlan.HasValue) return null;
            var p = _lastPlan.Value;
            var r = p.Rect;
            return new
            {
                ok      = true,
                npc     = npcName,
                action  = ActionName,
                tilled  = r.Width * r.Height,
                patch   = new
                {
                    x1 = r.X,
                    y1 = r.Y,
                    x2 = r.X + r.Width  - 1,
                    y2 = r.Y + r.Height - 1,
                },
                adjacent_to_existing = p.AdjacentToExisting,
                adjacency_edge       = p.AdjacencyLen,
                message = p.AdjacentToExisting
                    ? $"tilled {r.Width}x{r.Height} patch adjacent to existing farmland"
                    : $"tilled {r.Width}x{r.Height} patch (no existing farmland nearby — seeded)",
            };
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

                            // mature = "ripe right now" (HarvestCropsHandler will pick it
                            // this tick). growing = anything else, including a multi-harvest
                            // crop in its regrow window (phase=final, fullyGrown=true,
                            // dayOfCurrentPhase>0). Same gate used by the farm_actions sweep.
                            if (HarvestCropsHandler.IsCropHarvestable(dirt.crop))
                            {
                                matureCrops.Add(new { x = tx, y = ty, crop = cn, id = cid });
                            }
                            else
                            {
                                growingCrops.Add(new { x = tx, y = ty, crop = cn, id = cid, phase, phases });
                            }

                            if (dirt.state.Value != StardewValley.TerrainFeatures.HoeDirt.watered)
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
        // every tile into one of 7 buckets:
        //
        //   harvest — HoeDirt with crop in its final growth phase
        //   water   — HoeDirt with crop that needsWatering()
        //   clear   — Object on tile that is debris (weeds/twig/litter/small stone)
        //   till    — passable, Diggable=T, no Object, no terrain feature
        //   forage  — Object with IsSpawnedObject=true
        //   plant   — HoeDirt without a crop
        //   break   — mature non-tapped Tree, or large Stone (PSI >= 44)
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

            // Debris candidates are buffered, not Hit() directly, because
            // we want to restrict the `clear` group to the farmland bbox
            // (whatever rectangle covers all HoeDirt tiles seen this sweep).
            // Without this, a wide-radius observe on Farm picks up brush
            // far from the planted area and the agent then issues a giant
            // clear_debris bbox covering the whole region.
            var debrisCandidates = new List<(int x, int y)>();
            bool farmlandFound = false;
            int fx1 = int.MaxValue, fy1 = int.MaxValue, fx2 = int.MinValue, fy2 = int.MinValue;

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
                        // Track farmland bbox for the post-pass clear filter.
                        farmlandFound = true;
                        if (tx < fx1) fx1 = tx;
                        if (ty < fy1) fy1 = ty;
                        if (tx > fx2) fx2 = tx;
                        if (ty > fy2) fy2 = ty;

                        if (dirt.crop != null && !dirt.crop.dead.Value)
                        {
                            // Harvestable right now — same gate as
                            // HarvestCropsHandler so the bbox the agent
                            // feeds back never contains regrowing tiles.
                            if (HarvestCropsHandler.IsCropHarvestable(dirt.crop))
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

                            // Authoritative gate: state.Value != watered.
                            // We do NOT call HoeDirt.needsWatering() here —
                            // observation has shown it can return true even
                            // when state.Value == watered (paddy-crop or
                            // auto-water side effects) and that desync makes
                            // the agent see a constant `water.count` after
                            // the NPC has actually watered the tiles. Reading
                            // state directly keeps observe / select / execute
                            // aligned and lets the count drop as expected.
                            if (dirt.state.Value != StardewValley.TerrainFeatures.HoeDirt.watered)
                                Hit("water", tx, ty);
                        }
                        else if (dirt.crop == null)
                        {
                            Hit("plant", tx, ty);
                        }
                    }

                    // ── Trees: break (mature, not tapped) ───────────
                    // Mirrors BreakResourceHandler tree filter so the
                    // bbox the agent feeds back actually has work.
                    if (tf is StardewValley.TerrainFeatures.Tree tree
                        && tree.growthStage.Value >= 5
                        && !tree.tapped.Value)
                    {
                        Hit("break", tx, ty);
                    }

                    // ── Objects: clear (debris) / forage (spawn) / break (large stone) ─
                    if (location.Objects.TryGetValue(tileV2, out var obj) && obj != null)
                    {
                        if (IsDebrisObj(obj))
                            debrisCandidates.Add((tx, ty));
                        if (obj.IsSpawnedObject)
                            Hit("forage", tx, ty);
                        // Large stones (PSI >= 44) are break targets; smaller
                        // ones already counted as `clear`.
                        if (obj.Name == "Stone" && obj.ParentSheetIndex >= 44)
                            Hit("break", tx, ty);
                    }

                    // ── TerrainFeature debris: clear group ────────
                    // Tree stumps and saplings are clearable; contribute
                    // to the `clear` group same as small Objects.
                    // Reuse `tf` from the HoeDirt TryGetValue above —
                    // it is non-null iff there is any terrainFeature at this tile.
                    if (tf != null && IsDebrisTerrainFeature(tf))
                        debrisCandidates.Add((tx, ty));

                    // ── till: empty or clearable-debris, passable, Diggable=T ─
                    // Tiles with clearable debris (weeds, twigs, saplings,
                    // stumps) are counted as tillable — the debris will be
                    // removed before tilling. Non-debris objects/terrain still
                    // block.
                    bool blockedByObject = location.Objects.TryGetValue(tileV2, out var tillObj)
                        && tillObj != null && !IsDebrisObj(tillObj);
                    bool blockedByTerrain = location.terrainFeatures.TryGetValue(tileV2, out var tillTf)
                        && tillTf != null
                        && !IsDebrisTerrainFeature(tillTf)
                        && !(tillTf is StardewValley.TerrainFeatures.Tree tillTree
                             && tillTree.growthStage.Value >= 5
                             && !tillTree.tapped.Value);
                    if (!blockedByObject && !blockedByTerrain
                        && location.isTilePassable(new xTile.Dimensions.Location(tx, ty), Game1.viewport)
                        && location.doesTileHaveProperty(tx, ty, "Diggable", "Back") == "T")
                    {
                        Hit("till", tx, ty);
                    }

                    // ── fill / fill_blocked: gaps inside farmland bbox ──
                    // After all other hits, check if this tile is a gap
                    // inside the farmland bbox. Only fires when farmlandFound
                    // and the tile is inside (fx1..fx2, fy1..fy2).
                    if (farmlandFound
                        && tx >= fx1 && tx <= fx2 && ty >= fy1 && ty <= fy2)
                    {
                        // Already HoeDirt -> not a gap.
                        if (tf is StardewValley.TerrainFeatures.HoeDirt)
                        {
                            // nothing — already farmland
                        }
                        else if (location.Objects.TryGetValue(tileV2, out var gapObj) && gapObj != null
                            && IsDebrisObj(gapObj))
                        {
                            // Debris object in the gap — needs clearing first.
                            Hit("fill_blocked", tx, ty);
                        }
                        else if (tf != null && IsDebrisTerrainFeature(tf))
                        {
                            // Terrain debris in the gap — needs clearing first.
                            Hit("fill_blocked", tx, ty);
                        }
                        else if (tf is StardewValley.TerrainFeatures.Tree gapTree
                            && gapTree.growthStage.Value >= 5
                            && !gapTree.tapped.Value)
                        {
                            // Mature non-tapped tree in the gap — needs breaking.
                            Hit("fill_blocked", tx, ty);
                        }
                        else if (!location.Objects.ContainsKey(tileV2)
                            && !location.terrainFeatures.ContainsKey(tileV2)
                            && location.isTilePassable(new xTile.Dimensions.Location(tx, ty), Game1.viewport)
                            && location.doesTileHaveProperty(tx, ty, "Diggable", "Back") == "T")
                        {
                            // Empty tillable gap — ready to fill directly.
                            Hit("fill", tx, ty);
                        }
                        // Permanent obstacles (buildings, fences, bushes, water,
                        // non-diggable) are not added to either group.
                    }
                }
            }

            // Post-pass: commit debris candidates into the `clear` bucket,
            // restricted to an extended farmland bbox (farmland + margin).
            // Without the margin, `clear.bbox` is exactly the HoeDirt envelope
            // and the agent clears only debris ON the crops — missing debris
            // in the adjacent tillable area where the NPC will extend the field.
            const int clearExtend = 6;
            int cfx1 = farmlandFound ? Math.Max(0, fx1 - clearExtend) : 0;
            int cfy1 = farmlandFound ? Math.Max(0, fy1 - clearExtend) : 0;
            int cfx2 = farmlandFound ? fx2 + clearExtend : 0;
            int cfy2 = farmlandFound ? fy2 + clearExtend : 0;

            foreach (var (tx, ty) in debrisCandidates)
            {
                if (farmlandFound)
                {
                    if (tx < cfx1 || tx > cfx2 || ty < cfy1 || ty > cfy2) continue;
                }
                Hit("clear", tx, ty);
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
            AddActionGroup("break");
            AddActionGroup("fill");
            AddActionGroup("fill_blocked");

            // ── Summary ────────────────────────────────────────────────
            var parts = new List<string>();
            foreach (var key in new[] { "harvest", "water", "clear", "till", "forage", "plant", "break", "fill", "fill_blocked" })
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

            // Debug overlay: paint the observed square red briefly so the
            // operator can see exactly what the agent just looked at.
            // No-op when ModConfig.DebugShowBBoxOverlay is false.
            int r = Math.Max(radius, 0);
            BBoxOverlay.Instance.MarkObserve(
                location.Name ?? "",
                cx - r, cy - r, cx + r, cy + r);

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
            if (tf is StardewValley.TerrainFeatures.Grass)
                return true;
            return false;
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
        protected override bool RefuseWhileBusy => true;

        public WithdrawFromChestHandler(IMonitor log, Func<bool> showBubble, NpcInventory inventory, FollowSystem follow)
            : base(log, showBubble)
        {
            _inventory = inventory;
            _follow    = follow;
            SetBusyGate(follow);
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
        protected override bool RefuseWhileBusy => true;

        public BreakResourceHandler(IMonitor log, Func<bool> showBubble, NpcInventory inventory, FollowSystem follow)
            : base(log, showBubble)
        {
            _inventory = inventory;
            _follow    = follow;
            SetBusyGate(follow);
        }

        protected override string ResolveBubble(JsonElement @params)
        {
            int radius   = ParseInt(@params, "radius",    6, 1, 30);
            int maxCount = ParseInt(@params, "max_count", 3, 1, 9999);
            int extend   = ParseInt(@params, "extend",    0, 0, 10);
            string what  = ParseWhat(@params);
            return $"[伐木] r={radius} max={maxCount} {what}";
        }

        protected override void Execute(NPC npc, string npcName, JsonElement @params)
        {
            int radius   = ParseInt(@params, "radius",    6, 1, 30);
            int maxCount = ParseInt(@params, "max_count", 3, 1, 9999);
            int extend   = ParseInt(@params, "extend",    0, 0, 10);
            string what  = ParseWhat(@params);
            var (bboxOn, bx1, by1, bx2, by2) = ClearDebrisHandler.ParseBBox(@params);
            // Hard cap at 10 resources per call to prevent excessive gathering.
            // max_count is configurable; bbox mode also respects it (no longer defaults to unlimited).
            const int HARD_CAP = 10;
            int effectiveCap = Math.Min(maxCount, HARD_CAP);
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
            // Scan range comes from bbox when set; otherwise radius around NPC.
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

            if (wantTrees)
            {
                for (int tx = sx1; tx <= sx2; tx++)
                {
                    for (int ty = sy1; ty <= sy2; ty++)
                    {
                        var tileV2 = new Microsoft.Xna.Framework.Vector2(tx, ty);
                        float d = Microsoft.Xna.Framework.Vector2.Distance(npcTile, tileV2);
                        if (!bboxOn && d > radius) continue;

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

                    if (bboxOn)
                    {
                        if (tileV2.X < bx1 || tileV2.X > bx2 || tileV2.Y < by1 || tileV2.Y > by2) continue;
                    }
                    else
                    {
                        float dist = Microsoft.Xna.Framework.Vector2.Distance(npcTile, tileV2);
                        if (dist > radius) continue;
                    }
                    float d = Microsoft.Xna.Framework.Vector2.Distance(npcTile, tileV2);
                    targets.Add((new Microsoft.Xna.Framework.Point((int)tileV2.X, (int)tileV2.Y),
                        false, false, "", obj.ParentSheetIndex, d));
                }
            }

            // ── Sort & cap ──────────────────────────────────────────
            targets.Sort((a, b) => a.dist.CompareTo(b.dist));
            if (targets.Count > effectiveCap) targets = targets.GetRange(0, effectiveCap);

            if (targets.Count == 0)
            {
                string scope = bboxOn ? $"bbox=({bx1},{by1})-({bx2},{by2})" : $"radius={radius}";
                Log.Log($"[npc_break_resource] {npcName}: no {what} resources in {scope}", LogLevel.Info);
                MarkNothingToDo($"no {what} resources in {scope}");
                return;
            }

            // ── Start FollowSystem (TSP-ordered) ────────────────────
            var startPt = new Microsoft.Xna.Framework.Point((int)npcTile.X, (int)npcTile.Y);
            var ordered = PathPlanner.PlanBy(startPt, targets, t => t.tile);
            var resourceTargets = ordered.Select(t =>
                new ResourceTarget(t.tile, t.isTree, t.isStump, t.treeType, t.stoneIndex));
            _follow.StartBreakResource(npcName, resourceTargets, _inventory);

            BBoxOverlay.Instance.MarkAction(npcName, location.Name ?? "",
                bboxOn ? bx1 : (int)npcTile.X - radius,
                bboxOn ? by1 : (int)npcTile.Y - radius,
                bboxOn ? bx2 : (int)npcTile.X + radius,
                bboxOn ? by2 : (int)npcTile.Y + radius);

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
        protected override bool RefuseWhileBusy => true;

        public FertilizeHandler(IMonitor log, Func<bool> showBubble, NpcInventory inventory, FollowSystem follow)
            : base(log, showBubble)
        {
            _inventory = inventory;
            _follow    = follow;
            SetBusyGate(follow);
        }

        protected override string ResolveBubble(JsonElement @params)
        {
            string? fertId = null;
            if (@params.ValueKind == JsonValueKind.Object &&
                @params.TryGetProperty("fertilizer_id", out JsonElement s) &&
                s.ValueKind == JsonValueKind.String)
                fertId = s.GetString();
            int maxCount = ParseInt(@params, "max_count", 5, 1, 9999);
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

            int radius   = ParseInt(@params, "radius",    5, 1, 30);
            int maxCount = ParseInt(@params, "max_count", 5, 1, 9999);
            var (bboxOn, bx1, by1, bx2, by2) = ClearDebrisHandler.ParseBBox(@params);
            // bbox itself is the area cap; max_count is a radius-mode safety knob only.
            int effectiveCap = bboxOn ? int.MaxValue : maxCount;

            var location = npc.currentLocation;
            if (location is null) return;

            if (!IsFarmMap(location))
            {
                Log.Log($"[npc_fertilize] {npcName}: map '{location.Name}' is not a farm", LogLevel.Warn);
                return;
            }

            // Scan for empty, unfertilized HoeDirt tiles within bbox or radius.
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

                    if (!location.terrainFeatures.TryGetValue(tileV2, out var tf)) continue;
                    if (tf is not StardewValley.TerrainFeatures.HoeDirt dirt) continue;
                    if (dirt.crop != null) continue; // only empty tiles
                    if (!string.IsNullOrEmpty(dirt.fertilizer.Value)) continue; // already fertilized

                    targets.Add((tileV2, d));
                }
            }

            // Sort & cap (bbox mode keeps everything inside the rectangle).
            targets.Sort((a, b) => a.dist.CompareTo(b.dist));
            if (targets.Count > effectiveCap) targets = targets.GetRange(0, effectiveCap);

            if (targets.Count == 0)
            {
                string scope = bboxOn ? $"bbox=({bx1},{by1})-({bx2},{by2})" : $"radius={radius}";
                Log.Log($"[npc_fertilize] {npcName}: no empty unfertilized HoeDirt in {scope}", LogLevel.Info);
                MarkNothingToDo($"no empty unfertilized tilled soil in {scope}");
                return;
            }

            // Inventory check (non-blocking, like plant_seeds).
            var items = _inventory.GetItems(npcName);
            var slot = items.FirstOrDefault(s => s.ItemId == fertId);
            int available = slot?.Count ?? 0;
            if (available <= 0)
                Log.Log($"[npc_fertilize] {npcName}: no {fertId} in backpack, fertilizing without consuming", LogLevel.Info);

            var startPt = new Microsoft.Xna.Framework.Point((int)npcTile.X, (int)npcTile.Y);
            var tilePoints = PathPlanner.PlanBy(
                startPt, targets, t => new Microsoft.Xna.Framework.Point((int)t.tile.X, (int)t.tile.Y))
                .Select(t => new Microsoft.Xna.Framework.Point((int)t.tile.X, (int)t.tile.Y));
            _follow.StartFertilize(npcName, tilePoints, fertId, _inventory);

            BBoxOverlay.Instance.MarkAction(npcName, location.Name ?? "",
                bboxOn ? bx1 : (int)npcTile.X - radius,
                bboxOn ? by1 : (int)npcTile.Y - radius,
                bboxOn ? bx2 : (int)npcTile.X + radius,
                bboxOn ? by2 : (int)npcTile.Y + radius);

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

    internal sealed class FillGapsHandler : NpcActionHandlerBase
    {
        private readonly FollowSystem _follow;

        protected override string ActionName => "npc_fill_gaps";
        protected override bool RefuseWhileBusy => true;

        public FillGapsHandler(IMonitor log, Func<bool> showBubble, FollowSystem follow)
            : base(log, showBubble)
        {
            _follow = follow;
            SetBusyGate(follow);
        }

        protected override string ResolveBubble(JsonElement @params)
        {
            return "[填充空隙]";
        }

        protected override void Execute(NPC npc, string npcName, JsonElement @params)
        {
            var (bboxOn, x1, y1, x2, y2) = ClearDebrisHandler.ParseBBox(@params);
            if (!bboxOn)
            {
                MarkNothingToDo("fill_gaps requires x1/y1/x2/y2 bbox");
                return;
            }

            var location = npc.currentLocation;
            if (location is null) return;

            // Scan bbox for empty gap tiles: no HoeDirt, no Object, no terrainFeature,
            // passable, Diggable=T. Does NOT clear debris or chop trees — agent must
            // have already handled fill_blocked.
            var npcTile = npc.Tile;
            var targets = new System.Collections.Generic.List<Microsoft.Xna.Framework.Vector2>();
            int skipped = 0;

            for (int tx = x1; tx <= x2; tx++)
            {
                for (int ty = y1; ty <= y2; ty++)
                {
                    var tv = new Microsoft.Xna.Framework.Vector2(tx, ty);

                    // Skip if already HoeDirt.
                    if (location.terrainFeatures.TryGetValue(tv, out var tf)
                        && tf is StardewValley.TerrainFeatures.HoeDirt)
                        continue;

                    // Skip if occupied by Object or non-HoeDirt terrainFeature.
                    if (location.Objects.ContainsKey(tv))
                    {
                        skipped++;
                        continue;
                    }
                    if (location.terrainFeatures.ContainsKey(tv))
                    {
                        skipped++;
                        continue;
                    }

                    if (!location.isTilePassable(new xTile.Dimensions.Location(tx, ty), Game1.viewport))
                    {
                        skipped++;
                        continue;
                    }
                    if (location.doesTileHaveProperty(tx, ty, "Diggable", "Back") != "T")
                    {
                        skipped++;
                        continue;
                    }

                    targets.Add(tv);
                }
            }

            if (targets.Count == 0)
            {
                Log.Log(
                    $"[npc_fill_gaps] {npcName}: no empty gaps in bbox ({x1},{y1})-({x2},{y2}), skipped={skipped}",
                    LogLevel.Info);
                MarkNothingToDo($"no empty gaps in bbox ({x1},{y1})-({x2},{y2}), {skipped} tiles occupied/blocked");
                return;
            }

            // Sort by distance and TSP-order.
            targets.Sort((a, b) =>
                Microsoft.Xna.Framework.Vector2.Distance(npcTile, a)
                    .CompareTo(Microsoft.Xna.Framework.Vector2.Distance(npcTile, b)));

            var startPt = new Microsoft.Xna.Framework.Point((int)npcTile.X, (int)npcTile.Y);
            var tilePoints = targets
                .Select(t => new Microsoft.Xna.Framework.Point((int)t.X, (int)t.Y))
                .ToList();
            var ordered = PathPlanner.PlanBy(startPt, tilePoints, p => p);
            _follow.StartFillGaps(npcName, ordered);

            Log.Log(
                $"[npc_fill_gaps] {npcName}: queued {targets.Count} gap tiles in bbox ({x1},{y1})-({x2},{y2}), skipped={skipped}",
                LogLevel.Info);
        }
    }
}
