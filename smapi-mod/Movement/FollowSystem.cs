// NPC behavior state machine: Idle / Summoning / Following / Leading.
//
// Owns one NpcBehaviorState per managed NPC. All state transitions and
// game-thread work (PathFindController construction, warpCharacter, etc.)
// happen inside PumpOnGameTick — ws handlers only *set* the target mode and
// return. This keeps the ws receive loop free of Game1 access.
//
// Summoning : NPC travels to the player (cross-map warp if needed), then
//             paths to an adjacent tile in front of the player. Terminates
//             in Idle when adjacent.
// Following : every ~30 ticks, if the distance to the player exceeds the
//             follow radius, repath the NPC to a tile one behind the player.
// Leading   : segmented pathfinding toward a target tile. Every segment is
//             capped at 5 tiles; on segment completion, enqueue the next one.
//             Terminates in Idle when the final target is reached.
//
// OnPlayerWarped: if the NPC is Following/Leading the warping player, warp
// the NPC to the new location's arrival tile and resume.

using System;
using System.Collections.Generic;
using System.Linq;
using System.Threading.Tasks;
using Microsoft.Xna.Framework;
using StardewModdingAPI;
using StardewValley;
using StardewValley.Pathfinding;
using StardewValley.TerrainFeatures;

namespace SmartNPC.Bridge
{
    /// <summary>Current behavior mode for an Agent-managed NPC.</summary>
    internal enum NpcBehaviorMode
    {
        Idle,
        Summoning,
        Following,
        Leading,
        Wander,
        ClearDebris,
        ForageCollect,
        DepositItems,
        DeliverItems,
        TillSoil,
        ApproachAndSpeak,
        PlantSeeds,
        WaterCrops,
        HarvestCrops,
        WithdrawItems,
        BreakResource,
        Fertilize,
    }

    /// <summary>Describes a single resource target (tree, stump, or large stone).</summary>
    public readonly struct ResourceTarget
    {
        public readonly Point Tile;
        public readonly bool IsTree;
        public readonly bool IsStump;
        public readonly string TreeType;    // e.g. "oak", "maple", "pine"
        public readonly int StoneIndex;     // ParentSheetIndex for stones

        public ResourceTarget(Point tile, bool isTree, bool isStump, string treeType, int stoneIndex)
        {
            Tile = tile;
            IsTree = isTree;
            IsStump = isStump;
            TreeType = treeType;
            StoneIndex = stoneIndex;
        }
    }

    /// <summary>Per-NPC mutable state held by FollowSystem.</summary>
    internal sealed class NpcBehaviorState
    {
        public NpcBehaviorMode Mode { get; set; } = NpcBehaviorMode.Idle;

        // Leading target (map-local tile coordinates).
        public Point LeadTarget { get; set; }
        public string? LeadMap   { get; set; }

        // Wander: current destination + radius for continuous re-selection.
        public Point WanderTarget { get; set; }
        public int   WanderRadius { get; set; } = 8;

        // Wander constraints (optional).
        public int WanderCenterX     { get; set; }
        public int WanderCenterY     { get; set; }
        public int WanderMaxDistance { get; set; } // 0 = unlimited
        public int WanderX1          { get; set; }
        public int WanderY1          { get; set; }
        public int WanderX2          { get; set; }
        public int WanderY2          { get; set; }

        // ClearDebris: ordered queue of debris tiles to visit and destroy.
        public Queue<Point>?  DebrisQueue     { get; set; }
        public NpcInventory?  DebrisInventory { get; set; }
        public Point          DebrisTarget    { get; set; }
        public bool           DebrisPathed    { get; set; }

        // DepositItems: walk to a chest and deposit carried items.
        public Point               DepositChestTile { get; set; }
        public string?             DepositChestMap  { get; set; }
        public HashSet<string>?    DepositItemIds   { get; set; }  // null = all
        public NpcInventory?       DepositInventory { get; set; }
        public bool                DepositPathed    { get; set; }
        public int                 DepositedCount   { get; set; }

        // DeliverItems: walk to player and hand over items.
        public NpcInventory?  DeliverInventory { get; set; }
        public bool           DeliverPathed    { get; set; }
        public int            DeliveredCount   { get; set; }

        // TillSoil: walk to tiles and create HoeDirt.
        public Queue<Point>? TillQueue  { get; set; }
        public Point         TillTarget { get; set; }
        public bool          TillPathed { get; set; }
        public int           TillCount  { get; set; }

        // ForageCollect: walk to spawned objects and pick them up.
        public Queue<(Point Tile, string ItemId, string ItemName)>? ForageQueue    { get; set; }
        public (Point Tile, string ItemId, string ItemName)          ForageTarget  { get; set; }
        public NpcInventory?                                          ForageInventory { get; set; }
        public bool                                                   ForagePathed  { get; set; }

        // ApproachAndSpeak: walk to player and emote.
        public string? ApproachReason { get; set; }
        public bool    ApproachPathed { get; set; }

        // PlantSeeds: walk to empty HoeDirt tiles and plant seeds from inventory.
        public Queue<Point>? PlantSeedQueue     { get; set; }
        public Point         PlantSeedTarget    { get; set; }
        public NpcInventory? PlantSeedInventory { get; set; }
        public string?       PlantSeedID        { get; set; }  // qualified id, e.g. "(O)472"
        public bool          PlantSeedPathed    { get; set; }
        public int           PlantSeededCount   { get; set; }

        // WaterCrops: walk to unwatered crop tiles and water them.
        public Queue<Point>? WaterCropQueue  { get; set; }
        public Point         WaterCropTarget { get; set; }
        public bool          WaterCropPathed { get; set; }
        public int           WaterCropCount  { get; set; }

        // HarvestCrops: walk to mature crop tiles, harvest into inventory.
        public Queue<Point>? HarvestQueue     { get; set; }
        public Point         HarvestTarget    { get; set; }
        public NpcInventory? HarvestInventory { get; set; }
        public bool          HarvestPathed    { get; set; }
        public int           HarvestedCount   { get; set; }

        // WithdrawItems: walk to a chest and withdraw items into backpack.
        public Point               WithdrawChestTile { get; set; }
        public string?             WithdrawChestMap  { get; set; }
        public NpcInventory?       WithdrawInventory { get; set; }
        public string?             WithdrawItemID    { get; set; }
        public int                 WithdrawCount     { get; set; }
        public int                 WithdrawnTotal    { get; set; }
        public bool                WithdrawPathed    { get; set; }

        // BreakResource: walk to trees/stones, destroy them, collect drops.
        public Queue<ResourceTarget>? BreakResourceQueue     { get; set; }
        public ResourceTarget         BreakResourceTarget    { get; set; }
        public NpcInventory?          BreakResourceInventory { get; set; }
        public bool                   BreakResourcePathed    { get; set; }
        public int                    BreakResourceCount     { get; set; }

        // Fertilize: walk to empty HoeDirt tiles and apply fertilizer.
        public Queue<Point>? FertilizeQueue     { get; set; }
        public Point         FertilizeTarget    { get; set; }
        public NpcInventory? FertilizeInventory { get; set; }
        public string?       FertilizeID        { get; set; }
        public bool          FertilizePathed    { get; set; }
        public int           FertilizedCount    { get; set; }

        // Tick scheduler: only repath on these boundaries.
        public uint LastPathTick { get; set; }

        // Path failure guard: track consecutive TryStartPath failures for the
        // same target so we can skip unreachable tiles instead of retrying
        // every tick forever.
        public int PathFailCount { get; set; }

        // Summoning: true once we have issued the final same-map path.
        public bool SummonPathed { get; set; }
    }

    /// <summary>
    /// Drives Summon / Follow / Lead for all registered NPCs. One instance
    /// lives in ModEntry and is pumped from OnUpdateTicked.
    /// </summary>
    internal sealed class FollowSystem
    {
        // Tick cadence for Following / Leading re-evaluation.
        private const int ReplanIntervalTicks = 30;

        // Max consecutive TryStartPath failures before skipping the target.
        private const int MaxPathFailures = 5;

        // Follow radius: stay within this many tiles of the player. If farther,
        // the NPC is repathed behind the player.
        private const float FollowRadiusTiles = 3f;

        // Leading segment length: walk at most this many tiles per replan.
        private const int LeadSegmentTiles = 5;

        private static readonly Random s_rng = new();

        private readonly IMonitor _log;
        private Func<string, object?, Task>? _broadcastEvent;
        private readonly Dictionary<string, NpcBehaviorState> _states =
            new(StringComparer.OrdinalIgnoreCase);

        private uint _tickCounter;

        public FollowSystem(IMonitor log) { _log = log; }

        /// <summary>
        /// Wire in the ws broadcast function after WebSocketServer is created.
        /// Called from ModEntry once _ws is ready.
        /// </summary>
        public void SetBroadcast(Func<string, object?, Task> broadcast)
        {
            _broadcastEvent = broadcast;
        }

        // ── public API (called from ws handlers on the game thread via ModEntry) ──

        /// <summary>
        /// Pre-register an Agent-managed NPC so the Idle guard in PumpOnGameTick
        /// can cancel game-injected controllers even before the first Agent command.
        /// Safe to call multiple times for the same name.
        /// </summary>
        public void EnsureRegistered(string npcName) => this.GetOrCreate(npcName);

        public void Summon(string npcName)
        {
            var st = this.GetOrCreate(npcName);
            st.Mode = NpcBehaviorMode.Summoning;
            st.SummonPathed = false;
            st.LastPathTick = 0;
        }

        public void StartFollow(string npcName)
        {
            var st = this.GetOrCreate(npcName);
            st.Mode = NpcBehaviorMode.Following;
            st.LastPathTick = 0;
        }

        public void StopFollow(string npcName)
        {
            if (!_states.TryGetValue(npcName, out var st)) return;
            st.Mode = NpcBehaviorMode.Idle;

            // Cancel any in-flight pathing so the NPC actually stops.
            NPC? npc = Game1.getCharacterFromName(npcName);
            if (npc != null)
            {
                try { npc.Halt(); } catch { /* non-fatal */ }
                if (npc.controller != null) npc.controller = null;
            }
        }

        public void LeadTo(string npcName, int x, int y, string? map)
        {
            var st = this.GetOrCreate(npcName);
            st.Mode = NpcBehaviorMode.Leading;
            st.LeadTarget = new Point(x, y);
            st.LeadMap = string.IsNullOrWhiteSpace(map) ? null : map;
            st.LastPathTick = 0;
        }

        /// <summary>
        /// Start continuous wander for <paramref name="npcName"/>: pick a random passable tile,
        /// walk there, then repeat automatically until <see cref="StopFollow"/> is called.
        /// </summary>
        /// <param name="centerX">Center X for tether constraint (0 = use NPC position).</param>
        /// <param name="centerY">Center Y for tether constraint (0 = use NPC position).</param>
        /// <param name="maxDist">Max distance from center; 0 = unlimited.</param>
        /// <param name="x1,y1,x2,y2">Bounding box constraint; only active when x1&lt;x2 and y1&lt;y2.</param>
        public void StartWander(string npcName, NPC npc, int radius,
            int centerX = 0, int centerY = 0, int maxDist = 0,
            int x1 = 0, int y1 = 0, int x2 = 0, int y2 = 0)
        {
            var st = this.GetOrCreate(npcName);
            var prev = st.Mode;
            st.WanderRadius = Math.Clamp(radius, 1, 24);

            // Store constraints.
            st.WanderCenterX     = centerX;
            st.WanderCenterY     = centerY;
            st.WanderMaxDistance = maxDist;
            st.WanderX1 = x1; st.WanderY1 = y1;
            st.WanderX2 = x2; st.WanderY2 = y2;

            Point? dest = PickWanderDest(npc, st, s_rng);
            if (dest is null)
            {
                _log.Log($"[FollowSystem/StartWander] {npcName}: no passable tile in zone, aborting", LogLevel.Warn);
                return;
            }

            st.Mode = NpcBehaviorMode.Wander;
            st.WanderTarget = dest.Value;
            st.LastPathTick = 0;
            _log.Log(
                $"[FollowSystem/StartWander] {npcName}: mode {prev}→Wander " +
                $"dest=({dest.Value.X},{dest.Value.Y}) radius={radius}" +
                (maxDist > 0 ? $" center=({centerX},{centerY}) maxD={maxDist}" : "") +
                (x1 < x2 && y1 < y2 ? $" zone=({x1},{y1})-({x2},{y2})" : ""),
                LogLevel.Debug);
        }

        /// <summary>
        /// Pick a random passable tile within the wander zone. When constraints are set
        /// (center+max_distance, bounding box, or both), tiles outside the zone are rejected.
        /// Returns null if no candidate is found in 40 attempts.
        /// </summary>
        internal static Point? PickWanderDest(NPC npc, NpcBehaviorState st, Random rng)
        {
            var location = npc.currentLocation;
            if (location is null) return null;

            int ox = (int)(npc.Position.X / 64f);
            int oy = (int)(npc.Position.Y / 64f);
            int radius = st.WanderRadius;

            // Constraint flags.
            bool hasTether  = st.WanderMaxDistance > 0;
            bool hasRect    = st.WanderX1 < st.WanderX2 && st.WanderY1 < st.WanderY2;
            int  centerX    = hasTether && st.WanderCenterX == 0 && st.WanderCenterY == 0 ? ox : st.WanderCenterX;
            int  centerY    = hasTether && st.WanderCenterX == 0 && st.WanderCenterY == 0 ? oy : st.WanderCenterY;

            for (int attempt = 0; attempt < 40; attempt++)
            {
                int tx, ty;
                if (hasRect)
                {
                    // Pick random tile within the bounding box.
                    tx = rng.Next(st.WanderX1, st.WanderX2 + 1);
                    ty = rng.Next(st.WanderY1, st.WanderY2 + 1);
                }
                else
                {
                    tx = ox + rng.Next(-radius, radius + 1);
                    ty = oy + rng.Next(-radius, radius + 1);
                }
                if (tx == ox && ty == oy) continue;

                // Tether check.
                if (hasTether)
                {
                    int dx = tx - centerX;
                    int dy = ty - centerY;
                    if (Math.Abs(dx) + Math.Abs(dy) > st.WanderMaxDistance) continue;
                }

                if (location.isTilePassable(new xTile.Dimensions.Location(tx, ty), Game1.viewport)
                    && !location.isObjectAtTile(tx, ty))
                    return new Point(tx, ty);
            }
            return null;
        }

        // ── StartClearDebris ──────────────────────────────────────────────

        /// <summary>
        /// Queue up a list of debris tiles for the NPC to walk to, destroy, and
        /// collect into <paramref name="inventory"/> one by one.
        /// </summary>
        public void StartClearDebris(string npcName, IEnumerable<Point> targets, NpcInventory inventory)
        {
            var st = this.GetOrCreate(npcName);
            st.DebrisQueue     = new Queue<Point>(targets);
            st.DebrisInventory = inventory;
            st.DebrisPathed    = false;

            if (st.DebrisQueue.Count == 0)
            {
                _log.Log($"[FollowSystem/ClearDebris] {npcName}: no targets, nothing to do", LogLevel.Debug);
                return;
            }

            st.DebrisTarget = st.DebrisQueue.Dequeue();
            st.Mode         = NpcBehaviorMode.ClearDebris;
            st.LastPathTick = 0;
            _log.Log($"[FollowSystem/ClearDebris] {npcName}: started, {st.DebrisQueue.Count + 1} targets", LogLevel.Debug);
        }

        // ── StartForageCollect ────────────────────────────────────────────

        /// <summary>
        /// Queue up a list of forage targets for the NPC to walk to, pick up, and
        /// store in <paramref name="inventory"/>, emitting a ws event per item.
        /// </summary>
        public void StartForageCollect(
            string npcName,
            IEnumerable<(Point, string, string)> targets,
            NpcInventory inventory)
        {
            var st = this.GetOrCreate(npcName);
            st.ForageQueue    = new Queue<(Point, string, string)>(targets);
            st.ForageInventory = inventory;
            st.ForagePathed   = false;

            if (st.ForageQueue.Count == 0)
            {
                _log.Log($"[FollowSystem/ForageCollect] {npcName}: no targets, nothing to do", LogLevel.Debug);
                return;
            }

            st.ForageTarget = st.ForageQueue.Dequeue();
            st.Mode         = NpcBehaviorMode.ForageCollect;
            st.LastPathTick = 0;
            _log.Log($"[FollowSystem/ForageCollect] {npcName}: started, {st.ForageQueue.Count + 1} targets", LogLevel.Debug);
        }

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

        // ── StartDeliverItems ──────────────────────────────────────────────

        /// <summary>
        /// Walk NPC to the player and hand over all items from <paramref name="inventory"/>.
        /// Player must be on the same map. Items that can't fit stay in NPC backpack.
        /// </summary>
        public void StartDeliverItems(string npcName, NpcInventory inventory)
        {
            var st = this.GetOrCreate(npcName);
            st.DeliverInventory = inventory;
            st.DeliverPathed    = false;
            st.DeliveredCount   = 0;
            st.Mode             = NpcBehaviorMode.DeliverItems;
            st.LastPathTick     = 0;

            _log.Log(
                $"[FollowSystem/DeliverItems] {npcName}: started",
                LogLevel.Debug);
        }

        // ── StartTillSoil ─────────────────────────────────────────────────

        /// <summary>
        /// Queue up a list of empty diggable tiles for the NPC to walk to and
        /// till (create HoeDirt) one by one. Only valid on farm-type maps.
        /// </summary>
        public void StartTillSoil(string npcName, IEnumerable<Point> targets)
        {
            var st = this.GetOrCreate(npcName);
            st.TillQueue  = new Queue<Point>(targets);
            st.TillPathed = false;
            st.TillCount  = 0;

            if (st.TillQueue.Count == 0)
            {
                _log.Log($"[FollowSystem/TillSoil] {npcName}: no targets, nothing to do", LogLevel.Debug);
                return;
            }

            st.TillTarget   = st.TillQueue.Dequeue();
            st.Mode         = NpcBehaviorMode.TillSoil;
            st.LastPathTick = 0;
            _log.Log($"[FollowSystem/TillSoil] {npcName}: started, {st.TillQueue.Count + 1} targets", LogLevel.Debug);
        }

        // ── StartApproachAndSpeak ──────────────────────────────────────────

        /// <summary>
        /// Walk NPC to the player, face them, and show a heart emote.
        /// Intended as a precursor to LLM-initiated chat_say — after arrival,
        /// the NPC returns to Idle and the LLM can follow up with a message.
        /// </summary>
        public void StartApproachAndSpeak(string npcName, string? reason)
        {
            var st = this.GetOrCreate(npcName);
            st.ApproachReason = reason;
            st.ApproachPathed = false;
            st.Mode           = NpcBehaviorMode.ApproachAndSpeak;
            st.LastPathTick   = 0;

            _log.Log(
                $"[FollowSystem/ApproachAndSpeak] {npcName}: started{(reason is not null ? $" reason=\"{reason}\"" : "")}",
                LogLevel.Debug);
        }

        // ── StartPlantSeeds ─────────────────────────────────────────────

        /// <summary>
        /// Queue up a list of empty HoeDirt tiles for the NPC to walk to and
        /// plant with seeds from their backpack. Only valid on farm-type maps.
        /// </summary>
        public void StartPlantSeeds(
            string npcName,
            IEnumerable<Point> targets,
            string seedId,
            NpcInventory inventory)
        {
            var st = this.GetOrCreate(npcName);
            st.PlantSeedQueue     = new Queue<Point>(targets);
            st.PlantSeedInventory = inventory;
            st.PlantSeedID        = seedId;
            st.PlantSeedPathed    = false;
            st.PlantSeededCount   = 0;

            if (st.PlantSeedQueue.Count == 0)
            {
                _log.Log($"[FollowSystem/PlantSeeds] {npcName}: no targets, nothing to do", LogLevel.Debug);
                return;
            }

            st.PlantSeedTarget = st.PlantSeedQueue.Dequeue();
            st.Mode            = NpcBehaviorMode.PlantSeeds;
            st.LastPathTick    = 0;
            _log.Log($"[FollowSystem/PlantSeeds] {npcName}: started, seed={seedId}, {st.PlantSeedQueue.Count + 1} targets", LogLevel.Debug);
        }

        // ── StartWaterCrops ──────────────────────────────────────────────

        /// <summary>
        /// Queue up a list of unwatered crop tiles for the NPC to walk to and
        /// water one by one. Works on any map with HoeDirt.
        /// </summary>
        public void StartWaterCrops(string npcName, IEnumerable<Point> targets)
        {
            var st = this.GetOrCreate(npcName);
            st.WaterCropQueue  = new Queue<Point>(targets);
            st.WaterCropPathed = false;
            st.WaterCropCount  = 0;

            if (st.WaterCropQueue.Count == 0)
            {
                _log.Log($"[FollowSystem/WaterCrops] {npcName}: no targets, nothing to do", LogLevel.Debug);
                return;
            }

            st.WaterCropTarget = st.WaterCropQueue.Dequeue();
            st.Mode            = NpcBehaviorMode.WaterCrops;
            st.LastPathTick    = 0;
            _log.Log($"[FollowSystem/WaterCrops] {npcName}: started, {st.WaterCropQueue.Count + 1} targets", LogLevel.Debug);
        }

        // ── StartWithdrawItems ────────────────────────────────────────────

        /// <summary>
        /// Walk NPC to the specified chest (or nearest chest if autoFind=true),
        /// then withdraw items from the chest into <paramref name="inventory"/>.
        /// Returns false immediately if no chest is found.
        /// </summary>
        public bool StartWithdrawItems(
            string npcName,
            NPC npc,
            Point chestTile,
            bool autoFind,
            string? chestMap,
            string itemId,
            int count,
            NpcInventory inventory)
        {
            var location = string.IsNullOrEmpty(chestMap)
                ? npc.currentLocation
                : Game1.getLocationFromName(chestMap);

            if (location is null)
            {
                _log.Log($"[FollowSystem/WithdrawItems] {npcName}: map '{chestMap}' not found", LogLevel.Warn);
                return false;
            }

            // Auto-find nearest chest.
            if (autoFind)
            {
                Point? nearest = FindNearestChest(npc, location);
                if (nearest is null)
                {
                    _log.Log($"[FollowSystem/WithdrawItems] {npcName}: no chest found in {location.Name}", LogLevel.Warn);
                    return false;
                }
                chestTile = nearest.Value;
            }
            else
            {
                if (!location.Objects.TryGetValue(new Vector2(chestTile.X, chestTile.Y), out var obj)
                    || obj is not StardewValley.Objects.Chest)
                {
                    _log.Log($"[FollowSystem/WithdrawItems] {npcName}: no chest at ({chestTile.X},{chestTile.Y})", LogLevel.Warn);
                    return false;
                }
            }

            var st = this.GetOrCreate(npcName);
            st.WithdrawChestTile = chestTile;
            st.WithdrawChestMap  = location.NameOrUniqueName ?? location.Name;
            st.WithdrawInventory = inventory;
            st.WithdrawItemID    = itemId;
            st.WithdrawCount     = count;
            st.WithdrawnTotal    = 0;
            st.WithdrawPathed    = false;
            st.Mode              = NpcBehaviorMode.WithdrawItems;
            st.LastPathTick      = 0;

            _log.Log(
                $"[FollowSystem/WithdrawItems] {npcName}: started → chest=({chestTile.X},{chestTile.Y}) " +
                $"map={st.WithdrawChestMap} item={itemId} count={count}",
                LogLevel.Debug);
            return true;
        }

        // ── StartBreakResource ────────────────────────────────────────────

        /// <summary>
        /// Queue up a list of resource targets (trees, stumps, large stones) for
        /// the NPC to walk to, destroy, and collect drops into <paramref name="inventory"/>.
        /// </summary>
        public void StartBreakResource(string npcName, IEnumerable<ResourceTarget> targets, NpcInventory inventory)
        {
            var st = this.GetOrCreate(npcName);
            st.BreakResourceQueue     = new Queue<ResourceTarget>(targets);
            st.BreakResourceInventory = inventory;
            st.BreakResourcePathed    = false;
            st.BreakResourceCount     = 0;

            if (st.BreakResourceQueue.Count == 0)
            {
                _log.Log($"[FollowSystem/BreakResource] {npcName}: no targets, nothing to do", LogLevel.Debug);
                return;
            }

            st.BreakResourceTarget = st.BreakResourceQueue.Dequeue();
            st.Mode                = NpcBehaviorMode.BreakResource;
            st.LastPathTick        = 0;
            _log.Log($"[FollowSystem/BreakResource] {npcName}: started, {st.BreakResourceQueue.Count + 1} targets", LogLevel.Debug);
        }

        // ── StartFertilize ────────────────────────────────────────────────

        public void StartFertilize(string npcName, IEnumerable<Point> targets, string fertilizerId, NpcInventory inventory)
        {
            var st = this.GetOrCreate(npcName);
            st.FertilizeQueue     = new Queue<Point>(targets);
            st.FertilizeInventory = inventory;
            st.FertilizeID        = fertilizerId;
            st.FertilizePathed    = false;
            st.FertilizedCount    = 0;

            if (st.FertilizeQueue.Count == 0)
            {
                _log.Log($"[FollowSystem/Fertilize] {npcName}: no targets, nothing to do", LogLevel.Debug);
                return;
            }

            st.FertilizeTarget = st.FertilizeQueue.Dequeue();
            st.Mode            = NpcBehaviorMode.Fertilize;
            st.LastPathTick    = 0;
            _log.Log($"[FollowSystem/Fertilize] {npcName}: started, fert={fertilizerId}, {st.FertilizeQueue.Count + 1} targets", LogLevel.Debug);
        }

        // ── StartHarvestCrops ──────────────────────────────────────────────

        /// <summary>
        /// Queue up a list of mature crop tiles for the NPC to walk to, harvest via
        /// the game's native crop.harvest(), and store the produce in <paramref name="inventory"/>.
        /// </summary>
        public void StartHarvestCrops(string npcName, IEnumerable<Point> targets, NpcInventory inventory)
        {
            var st = this.GetOrCreate(npcName);
            st.HarvestQueue     = new Queue<Point>(targets);
            st.HarvestInventory = inventory;
            st.HarvestPathed    = false;
            st.HarvestedCount   = 0;

            if (st.HarvestQueue.Count == 0)
            {
                _log.Log($"[FollowSystem/HarvestCrops] {npcName}: no targets, nothing to do", LogLevel.Debug);
                return;
            }

            st.HarvestTarget = st.HarvestQueue.Dequeue();
            st.Mode          = NpcBehaviorMode.HarvestCrops;
            st.LastPathTick  = 0;
            _log.Log($"[FollowSystem/HarvestCrops] {npcName}: started, {st.HarvestQueue.Count + 1} targets", LogLevel.Debug);
        }

        public NpcBehaviorMode GetMode(string npcName)
        {
            return _states.TryGetValue(npcName, out var st) ? st.Mode : NpcBehaviorMode.Idle;
        }

        public IReadOnlyDictionary<string, NpcBehaviorMode> Snapshot()
        {
            var copy = new Dictionary<string, NpcBehaviorMode>(_states.Count);
            foreach (var kv in _states) copy[kv.Key] = kv.Value.Mode;
            return copy;
        }

        // ── tick pump ─────────────────────────────────────────────────────

        public void PumpOnGameTick()
        {
            if (!Context.IsWorldReady) return;
            _tickCounter++;

            foreach (var kv in _states)
            {
                string name = kv.Key;
                NpcBehaviorState st = kv.Value;

                NPC? npc = Game1.getCharacterFromName(name);
                if (npc == null) continue;

                // Idle guard: if the game has injected a PathFindController while
                // the Agent isn't driving any movement, cancel it immediately.
                // This covers schedule-driven pathing that survived ignoreScheduleToday,
                // warp-home logic from returnHomeFromFarmPosition, and any other
                // game-side controller assignment.
                if (st.Mode == NpcBehaviorMode.Idle && npc.controller != null)
                {
                    _log.Log(
                        $"[FollowSystem/IdleGuard] cancelled game-injected controller for {name} " +
                        $"tick={_tickCounter}",
                        LogLevel.Debug);
                    try { npc.Halt(); } catch { /* non-fatal */ }
                    npc.controller = null;
                    continue;
                }

                if (st.Mode == NpcBehaviorMode.Idle) continue;

                try
                {
                    switch (st.Mode)
                    {
                        case NpcBehaviorMode.Summoning:
                            this.TickSummoning(npc, st);
                            break;
                        case NpcBehaviorMode.Following:
                            this.TickFollowing(npc, st);
                            break;
                        case NpcBehaviorMode.Leading:
                            this.TickLeading(npc, st);
                            break;
                        case NpcBehaviorMode.Wander:
                            this.TickWander(npc, st);
                            break;
                        case NpcBehaviorMode.ClearDebris:
                            this.TickClearDebris(npc, name, st);
                            break;
                        case NpcBehaviorMode.ForageCollect:
                            this.TickForageCollect(npc, name, st);
                            break;
                        case NpcBehaviorMode.DepositItems:
                            this.TickDepositItems(npc, name, st);
                            break;
                        case NpcBehaviorMode.DeliverItems:
                            this.TickDeliverItems(npc, name, st);
                            break;
                        case NpcBehaviorMode.TillSoil:
                            this.TickTillSoil(npc, name, st);
                            break;
                        case NpcBehaviorMode.ApproachAndSpeak:
                            this.TickApproachAndSpeak(npc, name, st);
                            break;
                        case NpcBehaviorMode.PlantSeeds:
                            this.TickPlantSeeds(npc, name, st);
                            break;
                        case NpcBehaviorMode.WaterCrops:
                            this.TickWaterCrops(npc, name, st);
                            break;
                        case NpcBehaviorMode.HarvestCrops:
                            this.TickHarvestCrops(npc, name, st);
                            break;
                        case NpcBehaviorMode.WithdrawItems:
                            this.TickWithdrawItems(npc, name, st);
                            break;
                        case NpcBehaviorMode.BreakResource:
                            this.TickBreakResource(npc, name, st);
                            break;
                        case NpcBehaviorMode.Fertilize:
                            this.TickFertilize(npc, name, st);
                            break;
                    }
                }
                catch (Exception ex)
                {
                    _log.Log($"[FollowSystem] {name} {st.Mode} threw: {ex}", LogLevel.Warn);
                    st.Mode = NpcBehaviorMode.Idle;
                }
            }
        }

        public void OnPlayerWarped(GameLocation newLocation)
        {
            if (newLocation == null) return;

            Farmer player = Game1.player;
            foreach (var kv in _states)
            {
                string name = kv.Key;
                NpcBehaviorState st = kv.Value;
                if (st.Mode != NpcBehaviorMode.Following && st.Mode != NpcBehaviorMode.Leading)
                    continue;

                NPC? npc = Game1.getCharacterFromName(name);
                if (npc == null) continue;
                if (npc.currentLocation == newLocation) continue;

                try
                {
                    Vector2 arrival = player.Tile;
                    Game1.warpCharacter(npc, newLocation, arrival);
                    if (npc.controller != null) npc.controller = null;
                    st.LastPathTick = 0; // force immediate repath on next tick
                }
                catch (Exception ex)
                {
                    _log.Log($"[FollowSystem] warp-chase {name} failed: {ex}", LogLevel.Warn);
                }
            }
        }

        // ── per-mode tick handlers ────────────────────────────────────────

        private void TickSummoning(NPC npc, NpcBehaviorState st)
        {
            Farmer player = Game1.player;
            if (player?.currentLocation == null) return;

            // Not on the same map → cross-map warp to an adjacent tile.
            if (npc.currentLocation != player.currentLocation)
            {
                Vector2 arrival = this.TileBehind(player);
                try { Game1.warpCharacter(npc, player.currentLocation, arrival); }
                catch (Exception ex) { _log.Log($"[FollowSystem] summon warp failed: {ex}", LogLevel.Warn); st.Mode = NpcBehaviorMode.Idle; return; }
                if (npc.controller != null) npc.controller = null;
                st.SummonPathed = false;
                return;
            }

            // Same map: walk to a tile in front of the player.
            if (!st.SummonPathed || npc.controller == null)
            {
                Point end = this.TileInFront(player);
                if (this.TryStartPath(npc, npc.currentLocation, end))
                {
                    st.SummonPathed = true;
                }
            }

            // Arrived when close to the player (≤ 1.5 tiles) and no active pathing.
            float dist = Vector2.Distance(npc.Tile, player.Tile);
            bool arrived = dist <= 1.5f && (npc.controller == null || npc.controller.pathToEndPoint == null || npc.controller.pathToEndPoint.Count == 0);
            if (arrived)
            {
                // Face the player and go Idle.
                int facing = FacePlayerDir(npc, player);
                npc.faceDirection(facing);
                st.Mode = NpcBehaviorMode.Idle;
            }
        }

        private void TickFollowing(NPC npc, NpcBehaviorState st)
        {
            Farmer player = Game1.player;
            if (player?.currentLocation == null) return;

            // Cross-map (player already moved and Warped event not wired, or NPC lagged).
            if (npc.currentLocation != player.currentLocation)
            {
                try { Game1.warpCharacter(npc, player.currentLocation, this.TileBehind(player)); }
                catch (Exception ex) { _log.Log($"[FollowSystem] follow warp failed: {ex}", LogLevel.Warn); }
                if (npc.controller != null) npc.controller = null;
                st.LastPathTick = _tickCounter;
                return;
            }

            if (!this.ShouldReplan(st)) return;

            float dist = Vector2.Distance(npc.Tile, player.Tile);
            if (dist <= FollowRadiusTiles)
            {
                // Close enough — no new path needed.
                return;
            }

            Point end = this.TileBehind(player).ToPoint();
            this.TryStartPath(npc, npc.currentLocation, end);
            st.LastPathTick = _tickCounter;
        }

        private void TickLeading(NPC npc, NpcBehaviorState st)
        {
            // Resolve leading target map (default: NPC's current map).
            GameLocation? targetMap = string.IsNullOrWhiteSpace(st.LeadMap)
                ? npc.currentLocation
                : Game1.getLocationFromName(st.LeadMap);
            if (targetMap == null)
            {
                st.Mode = NpcBehaviorMode.Idle;
                return;
            }

            // If the NPC is not yet on the target map, warp there first.
            if (npc.currentLocation != targetMap)
            {
                try { Game1.warpCharacter(npc, targetMap, new Vector2(st.LeadTarget.X, st.LeadTarget.Y)); }
                catch (Exception ex) { _log.Log($"[FollowSystem] lead warp failed: {ex}", LogLevel.Warn); st.Mode = NpcBehaviorMode.Idle; return; }
                if (npc.controller != null) npc.controller = null;
                st.LastPathTick = _tickCounter;
                return;
            }

            // Arrived?
            Point npcTile = new((int)npc.Tile.X, (int)npc.Tile.Y);
            if (ManhattanDistance(npcTile, st.LeadTarget) <= 1)
            {
                int facing = DirectionTo(npcTile, st.LeadTarget);
                if (facing >= 0) npc.faceDirection(facing);
                st.Mode = NpcBehaviorMode.Idle;
                return;
            }

            if (!this.ShouldReplan(st)) return;

            // Segmented pathing: walk at most LeadSegmentTiles toward the target.
            Point next = SegmentTarget(npcTile, st.LeadTarget, LeadSegmentTiles);
            this.TryStartPath(npc, npc.currentLocation, next);
            st.LastPathTick = _tickCounter;
        }

        private void TickWander(NPC npc, NpcBehaviorState st)
        {
            // First tick (or re-kick after selecting a new dest): kick off the path.
            if (st.LastPathTick == 0)
            {
                var loc = npc.currentLocation;
                if (loc is null)
                {
                    _log.Log($"[FollowSystem/Wander] {npc.Name}: currentLocation null, aborting", LogLevel.Warn);
                    st.Mode = NpcBehaviorMode.Idle;
                    return;
                }

                _log.Log(
                    $"[FollowSystem/Wander] {npc.Name}: initiating path to " +
                    $"({st.WanderTarget.X},{st.WanderTarget.Y}) tick={_tickCounter} " +
                    $"loc={loc.Name} ctrlBefore={npc.controller != null}",
                    LogLevel.Debug);

                bool ok = this.TryStartPath(npc, loc, st.WanderTarget);
                int pathLen = npc.controller?.pathToEndPoint?.Count ?? -1;

                _log.Log(
                    $"[FollowSystem/Wander] {npc.Name}: TryStartPath ok={ok} " +
                    $"pathLen={pathLen} ctrlAfter={npc.controller != null}",
                    LogLevel.Debug);

                if (ok)
                {
                    st.PathFailCount = 0;
                    st.LastPathTick  = _tickCounter > 0 ? _tickCounter : 1;
                }
                else
                {
                    // Path failed — skip this destination and try another.
                    st.PathFailCount++;
                    if (st.PathFailCount >= MaxPathFailures)
                    {
                        _log.Log(
                            $"[FollowSystem/Wander] {npc.Name}: {st.PathFailCount} consecutive path failures, going Idle",
                            LogLevel.Warn);
                        st.Mode = NpcBehaviorMode.Idle;
                        return;
                    }

                    Point? next = PickWanderDest(npc, st, s_rng);
                    if (next is null)
                    {
                        _log.Log(
                            $"[FollowSystem/Wander] {npc.Name}: no passable tile after path fail, going Idle",
                            LogLevel.Warn);
                        st.Mode = NpcBehaviorMode.Idle;
                        return;
                    }

                    st.WanderTarget  = next.Value;
                    st.LastPathTick  = 0; // re-enter first-tick branch immediately
                    _log.Log(
                        $"[FollowSystem/Wander] {npc.Name}: path failed → trying dest=({next.Value.X},{next.Value.Y}) fails={st.PathFailCount}",
                        LogLevel.Debug);
                }
                return;
            }

            // Subsequent ticks: monitor for arrival.
            bool ctrlNull  = npc.controller == null;
            int  pathCount = npc.controller?.pathToEndPoint?.Count ?? 0;
            bool pathDone  = ctrlNull || pathCount == 0;

            _log.Log(
                $"[FollowSystem/Wander] {npc.Name}: tick={_tickCounter} " +
                $"ctrlNull={ctrlNull} pathCount={pathCount} done={pathDone}",
                LogLevel.Trace);

            if (!pathDone) return;

            // Arrived — pick the next random destination and loop.
            st.PathFailCount = 0; // reset on successful arrival
            Point? next2 = PickWanderDest(npc, st, s_rng);
            if (next2 is null)
            {
                _log.Log(
                    $"[FollowSystem/Wander] {npc.Name}: no passable tile found, going Idle",
                    LogLevel.Debug);
                st.Mode = NpcBehaviorMode.Idle;
                return;
            }

            st.WanderTarget = next2.Value;
            st.LastPathTick = 0;   // trigger path kick on the very next tick
            _log.Log(
                $"[FollowSystem/Wander] {npc.Name}: arrived → next dest=({next2.Value.X},{next2.Value.Y})",
                LogLevel.Debug);
        }

        private void TickClearDebris(NPC npc, string npcName, NpcBehaviorState st)
        {
            var location = npc.currentLocation;
            if (location is null || st.DebrisQueue is null || st.DebrisInventory is null)
            {
                st.Mode = NpcBehaviorMode.Idle;
                return;
            }

            Point target = st.DebrisTarget;

            // Check if target object still exists (may have been cleared by something else).
            var targetV2 = new Vector2(target.X, target.Y);
            bool objectPresent = location.Objects.ContainsKey(targetV2);

            // If arrived (≤ 1.5 tiles) and path finished → destroy + collect → advance queue.
            float dist = Vector2.Distance(npc.Tile, new Vector2(target.X, target.Y));
            bool pathDone = npc.controller == null
                            || npc.controller.pathToEndPoint == null
                            || npc.controller.pathToEndPoint.Count == 0;

            if (dist <= 1.5f && pathDone)
            {
                // Destroy object if still present.
                if (objectPresent && location.Objects.TryGetValue(targetV2, out var obj))
                {
                    string dropId = obj.IsTwig()  ? "(O)388"
                                  : obj.IsWeeds() ? "(O)771"
                                  : "(O)390";

                    location.Objects.Remove(targetV2);
                    st.DebrisInventory.Add(npcName, dropId, 1);
                    npc.doEmote(16); // "!"
                    _log.Log(
                        $"[FollowSystem/ClearDebris] {npcName}: cleared {obj.Name} " +
                        $"at ({target.X},{target.Y}) → {dropId}",
                        LogLevel.Debug);
                }
                else
                {
                    _log.Log(
                        $"[FollowSystem/ClearDebris] {npcName}: object at ({target.X},{target.Y}) " +
                        $"already gone, skipping",
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

            // Not yet arrived — start or continue pathing (rate-limited).
            if ((!st.DebrisPathed || npc.controller == null) && this.ShouldReplan(st))
            {
                // Walk to a tile adjacent to the debris (one tile below it).
                Point adjacent = new Point(target.X, target.Y + 1);
                bool ok = this.TryStartPath(npc, location, adjacent);
                if (!ok)
                {
                    // Try tile above if below is impassable.
                    adjacent = new Point(target.X, target.Y - 1);
                    ok = this.TryStartPath(npc, location, adjacent);
                }
                st.DebrisPathed = ok;
                st.LastPathTick = _tickCounter;
                if (ok)
                {
                    st.PathFailCount = 0;
                }
                else
                {
                    st.PathFailCount++;
                }

                _log.Log(
                    $"[FollowSystem/ClearDebris] {npcName}: pathing to ({target.X},{target.Y}) ok={ok} fails={st.PathFailCount}",
                    LogLevel.Debug);

                // Skip unreachable target after MaxPathFailures consecutive failures.
                if (!ok && st.PathFailCount >= MaxPathFailures)
                {
                    _log.Log(
                        $"[FollowSystem/ClearDebris] {npcName}: giving up on ({target.X},{target.Y}) after {st.PathFailCount} failures",
                        LogLevel.Warn);
                    st.PathFailCount = 0;
                    if (st.DebrisQueue.Count == 0)
                    {
                        st.Mode = NpcBehaviorMode.Idle;
                        return;
                    }
                    st.DebrisTarget = st.DebrisQueue.Dequeue();
                    st.DebrisPathed = false;
                    st.LastPathTick = 0;
                }
            }
        }

        private void TickForageCollect(NPC npc, string npcName, NpcBehaviorState st)
        {
            var location = npc.currentLocation;
            if (location is null || st.ForageQueue is null || st.ForageInventory is null)
            {
                st.Mode = NpcBehaviorMode.Idle;
                return;
            }

            (Point target, string itemId, string itemName) = st.ForageTarget;
            var targetV2 = new Vector2(target.X, target.Y);

            float dist = Vector2.Distance(npc.Tile, new Vector2(target.X, target.Y));
            bool pathDone = npc.controller == null
                            || npc.controller.pathToEndPoint == null
                            || npc.controller.pathToEndPoint.Count == 0;

            if (dist <= 1.5f && pathDone)
            {
                if (location.Objects.ContainsKey(targetV2))
                {
                    location.Objects.Remove(targetV2);
                    st.ForageInventory.Add(npcName, itemId, 1);
                    npc.doEmote(20);
                    _log.Log(
                        $"[FollowSystem/ForageCollect] {npcName}: collected {itemName} ({itemId}) " +
                        $"at ({target.X},{target.Y})",
                        LogLevel.Debug);

                    _ = _broadcastEvent?.Invoke("forage_collected", new
                    {
                        npc       = npcName,
                        item_id   = itemId,
                        item_name = itemName,
                        quantity  = 1,
                        tile_x    = target.X,
                        tile_y    = target.Y,
                        location  = location.Name ?? "",
                    });
                }
                else
                {
                    _log.Log(
                        $"[FollowSystem/ForageCollect] {npcName}: object at ({target.X},{target.Y}) " +
                        $"already gone, skipping",
                        LogLevel.Debug);
                }

                if (st.ForageQueue.Count == 0)
                {
                    _log.Log($"[FollowSystem/ForageCollect] {npcName}: all targets done → Idle", LogLevel.Debug);
                    st.Mode = NpcBehaviorMode.Idle;
                    return;
                }

                st.ForageTarget = st.ForageQueue.Dequeue();
                st.ForagePathed = false;
                st.LastPathTick = 0;
                return;
            }

            if (!st.ForagePathed || npc.controller == null || this.ShouldReplan(st))
            {
                Point[] candidates = {
                    new(target.X,     target.Y + 1),
                    new(target.X,     target.Y - 1),
                    new(target.X + 1, target.Y),
                    new(target.X - 1, target.Y),
                };
                bool ok = false;
                foreach (var adj in candidates)
                {
                    ok = this.TryStartPath(npc, location, adj);
                    if (ok) break;
                }
                st.ForagePathed = ok;
                st.LastPathTick = _tickCounter;
                if (ok)
                {
                    st.PathFailCount = 0;
                }
                else
                {
                    st.PathFailCount++;
                }

                _log.Log(
                    $"[FollowSystem/ForageCollect] {npcName}: pathing to ({target.X},{target.Y}) ok={ok} fails={st.PathFailCount}",
                    LogLevel.Debug);

                // Skip unreachable target after MaxPathFailures consecutive failures.
                if (!ok && st.PathFailCount >= MaxPathFailures)
                {
                    _log.Log(
                        $"[FollowSystem/ForageCollect] {npcName}: giving up on ({target.X},{target.Y}) after {st.PathFailCount} failures",
                        LogLevel.Warn);
                    st.PathFailCount = 0;
                    if (st.ForageQueue.Count == 0)
                    {
                        st.Mode = NpcBehaviorMode.Idle;
                        return;
                    }
                    st.ForageTarget = st.ForageQueue.Dequeue();
                    st.ForagePathed = false;
                    st.LastPathTick = 0;
                }
            }
        }

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
                    npc.doEmote(16); // "!" — nothing to do
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
                    if (placed > 0)
                    {
                        _log.Log(
                            $"[FollowSystem/DepositItems] {npcName}: deposited {placed}/{slot.Count}× {slot.ItemId}",
                            LogLevel.Debug);
                    }
                    else
                    {
                        _log.Log(
                            $"[FollowSystem/DepositItems] {npcName}: chest full, could not deposit {slot.ItemId}",
                            LogLevel.Warn);
                    }
                }

                npc.doEmote(32); // happy
                _log.Log(
                    $"[FollowSystem/DepositItems] {npcName}: done, total deposited={st.DepositedCount} → Idle",
                    LogLevel.Info);
                st.Mode = NpcBehaviorMode.Idle;
                return;
            }

            // ── Pathing phase ──────────────────────────────────────────
            if ((!st.DepositPathed || npc.controller == null) && this.ShouldReplan(st))
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
                st.LastPathTick  = _tickCounter;
                if (ok)
                {
                    st.PathFailCount = 0;
                }
                else
                {
                    st.PathFailCount++;
                }
                _log.Log(
                    $"[FollowSystem/DepositItems] {npcName}: pathing to chest ({ct.X},{ct.Y}) ok={ok} fails={st.PathFailCount}",
                    LogLevel.Debug);

                if (!ok && st.PathFailCount >= MaxPathFailures)
                {
                    _log.Log(
                        $"[FollowSystem/DepositItems] {npcName}: giving up on chest ({ct.X},{ct.Y}) after {st.PathFailCount} failures → Idle",
                        LogLevel.Warn);
                    st.PathFailCount = 0;
                    st.Mode = NpcBehaviorMode.Idle;
                }
            }
        }

        private void TickDeliverItems(NPC npc, string npcName, NpcBehaviorState st)
        {
            if (st.DeliverInventory is null)
            {
                st.Mode = NpcBehaviorMode.Idle;
                return;
            }

            // Check items still exist — early out if empty.
            var items = st.DeliverInventory.GetItems(npcName).ToList();
            if (items.Count == 0)
            {
                _log.Log(
                    $"[FollowSystem/DeliverItems] {npcName}: backpack empty → Idle",
                    LogLevel.Debug);
                st.Mode = NpcBehaviorMode.Idle;
                return;
            }

            Farmer player = Game1.player;
            if (player?.currentLocation is null)
            {
                st.Mode = NpcBehaviorMode.Idle;
                return;
            }

            // Must be on same map.
            if (npc.currentLocation != player.currentLocation)
            {
                _log.Log(
                    $"[FollowSystem/DeliverItems] {npcName}: player not on same map → Idle",
                    LogLevel.Debug);
                st.Mode = NpcBehaviorMode.Idle;
                return;
            }

            var location = npc.currentLocation;
            if (location is null) { st.Mode = NpcBehaviorMode.Idle; return; }

            Point playerTile = player.Tile.ToPoint();

            float dist = Vector2.Distance(npc.Tile,
                new Vector2(playerTile.X, playerTile.Y));
            bool pathDone = npc.controller == null
                            || npc.controller.pathToEndPoint == null
                            || npc.controller.pathToEndPoint.Count == 0;

            if (dist <= 1.5f && pathDone)
            {
                // ── Delivery phase ──────────────────────────────────────
                foreach (var slot in items)
                {
                    StardewValley.Item? item;
                    try { item = StardewValley.ItemRegistry.Create(slot.ItemId, slot.Count, slot.Quality); }
                    catch (Exception ex)
                    {
                        _log.Log(
                            $"[FollowSystem/DeliverItems] ItemRegistry.Create({slot.ItemId}) failed: {ex.Message}",
                            LogLevel.Warn);
                        continue;
                    }

                    bool added = Game1.player.addItemToInventoryBool(item);
                    if (added)
                    {
                        st.DeliverInventory.Take(npcName, slot.ItemId, slot.Count);
                        st.DeliveredCount += slot.Count;
                        _log.Log(
                            $"[FollowSystem/DeliverItems] {npcName}: delivered {slot.Count}× {slot.ItemId}",
                            LogLevel.Debug);
                    }
                    else
                    {
                        _log.Log(
                            $"[FollowSystem/DeliverItems] {npcName}: player inventory full, kept {slot.ItemId}",
                            LogLevel.Warn);
                    }
                }

                npc.doEmote(32); // happy
                _log.Log(
                    $"[FollowSystem/DeliverItems] {npcName}: done, total delivered={st.DeliveredCount} → Idle",
                    LogLevel.Info);
                st.Mode = NpcBehaviorMode.Idle;
                return;
            }

            // ── Pathing phase — walk to adjacent tile near player ──────
            if (!st.DeliverPathed || npc.controller == null || this.ShouldReplan(st))
            {
                // Try adjacent tiles: below, above, right, left of player.
                Point[] candidates = {
                    new(playerTile.X,     playerTile.Y + 1),
                    new(playerTile.X,     playerTile.Y - 1),
                    new(playerTile.X + 1, playerTile.Y),
                    new(playerTile.X - 1, playerTile.Y),
                };
                bool ok = false;
                foreach (var adj in candidates)
                {
                    if (this.TryStartPath(npc, location, adj)) { ok = true; break; }
                }
                st.DeliverPathed = ok;
                st.LastPathTick  = _tickCounter;
                if (ok)
                {
                    st.PathFailCount = 0;
                }
                else
                {
                    st.PathFailCount++;
                }
                _log.Log(
                    $"[FollowSystem/DeliverItems] {npcName}: pathing to player ({playerTile.X},{playerTile.Y}) ok={ok} fails={st.PathFailCount}",
                    LogLevel.Debug);

                if (!ok && st.PathFailCount >= MaxPathFailures)
                {
                    _log.Log(
                        $"[FollowSystem/DeliverItems] {npcName}: giving up after {st.PathFailCount} failures → Idle",
                        LogLevel.Warn);
                    st.PathFailCount = 0;
                    st.Mode = NpcBehaviorMode.Idle;
                }
            }
        }

        private void TickTillSoil(NPC npc, string npcName, NpcBehaviorState st)
        {
            var location = npc.currentLocation;
            if (location is null || st.TillQueue is null)
            {
                st.Mode = NpcBehaviorMode.Idle;
                return;
            }

            Point target = st.TillTarget;
            var targetV2 = new Vector2(target.X, target.Y);

            // Check if tile is still tillable (empty ground, no HoeDirt yet).
            bool occupied = location.Objects.ContainsKey(targetV2)
                         || location.terrainFeatures.ContainsKey(targetV2);

            float dist = Vector2.Distance(npc.Tile, new Vector2(target.X, target.Y));
            bool pathDone = npc.controller == null
                            || npc.controller.pathToEndPoint == null
                            || npc.controller.pathToEndPoint.Count == 0;

            if (dist <= 1.5f && pathDone)
            {
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
                            $"[FollowSystem/TillSoil] {npcName}: tilled ({target.X},{target.Y}) frame={frame}",
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
                        $"[FollowSystem/TillSoil] {npcName}: tile ({target.X},{target.Y}) occupied, skipping",
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

            // ── Pathing phase ──────────────────────────────────────────
            if ((!st.TillPathed || npc.controller == null) && this.ShouldReplan(st))
            {
                // Walk to a tile adjacent to the target (try below first).
                Point[] candidates = {
                    new(target.X,     target.Y + 1),
                    new(target.X,     target.Y - 1),
                    new(target.X + 1, target.Y),
                    new(target.X - 1, target.Y),
                };
                bool ok = false;
                foreach (var adj in candidates)
                {
                    if (this.TryStartPath(npc, location, adj)) { ok = true; break; }
                }
                st.TillPathed = ok;
                st.LastPathTick = _tickCounter;
                if (ok)
                {
                    st.PathFailCount = 0;
                }
                else
                {
                    st.PathFailCount++;
                }
                _log.Log(
                    $"[FollowSystem/TillSoil] {npcName}: pathing to ({target.X},{target.Y}) ok={ok} fails={st.PathFailCount}",
                    LogLevel.Debug);

                // Skip unreachable target after MaxPathFailures consecutive failures.
                if (!ok && st.PathFailCount >= MaxPathFailures)
                {
                    _log.Log(
                        $"[FollowSystem/TillSoil] {npcName}: giving up on ({target.X},{target.Y}) after {st.PathFailCount} failures",
                        LogLevel.Warn);
                    st.PathFailCount = 0;
                    if (st.TillQueue.Count == 0)
                    {
                        st.Mode = NpcBehaviorMode.Idle;
                        return;
                    }
                    st.TillTarget = st.TillQueue.Dequeue();
                    st.TillPathed = false;
                    st.LastPathTick = 0;
                }
            }
        }

        private void TickApproachAndSpeak(NPC npc, string npcName, NpcBehaviorState st)
        {
            Farmer player = Game1.player;
            if (player?.currentLocation is null)
            {
                st.Mode = NpcBehaviorMode.Idle;
                return;
            }

            // Must be on same map.
            if (npc.currentLocation != player.currentLocation)
            {
                _log.Log(
                    $"[FollowSystem/ApproachAndSpeak] {npcName}: player not on same map → Idle",
                    LogLevel.Debug);
                st.Mode = NpcBehaviorMode.Idle;
                return;
            }

            var location = npc.currentLocation;
            if (location is null) { st.Mode = NpcBehaviorMode.Idle; return; }

            Point playerTile = player.Tile.ToPoint();

            float dist = Vector2.Distance(npc.Tile,
                new Vector2(playerTile.X, playerTile.Y));
            bool pathDone = npc.controller == null
                            || npc.controller.pathToEndPoint == null
                            || npc.controller.pathToEndPoint.Count == 0;

            if (dist <= 1.5f && pathDone)
            {
                // ── Arrived ─────────────────────────────────────────────
                int facing = FacePlayerDir(npc, player);
                npc.faceDirection(facing);

                // Show a text bubble with the reason (if any).
                if (!string.IsNullOrWhiteSpace(st.ApproachReason))
                    npc.showTextAboveHead(st.ApproachReason);
                npc.doEmote(20); // heart — "I want to talk to you"

                _log.Log(
                    $"[FollowSystem/ApproachAndSpeak] {npcName}: arrived, facing={facing} → Idle",
                    LogLevel.Info);
                st.Mode = NpcBehaviorMode.Idle;
                return;
            }

            // ── Pathing phase ──────────────────────────────────────────
            if (!st.ApproachPathed || npc.controller == null || this.ShouldReplan(st))
            {
                Point[] candidates = {
                    new(playerTile.X,     playerTile.Y + 1),
                    new(playerTile.X,     playerTile.Y - 1),
                    new(playerTile.X + 1, playerTile.Y),
                    new(playerTile.X - 1, playerTile.Y),
                };
                bool ok = false;
                foreach (var adj in candidates)
                {
                    if (this.TryStartPath(npc, location, adj)) { ok = true; break; }
                }
                st.ApproachPathed = ok;
                st.LastPathTick   = _tickCounter;
                if (ok)
                {
                    st.PathFailCount = 0;
                }
                else
                {
                    st.PathFailCount++;
                }
                _log.Log(
                    $"[FollowSystem/ApproachAndSpeak] {npcName}: pathing to player ({playerTile.X},{playerTile.Y}) ok={ok} fails={st.PathFailCount}",
                    LogLevel.Debug);

                if (!ok && st.PathFailCount >= MaxPathFailures)
                {
                    _log.Log(
                        $"[FollowSystem/ApproachAndSpeak] {npcName}: giving up after {st.PathFailCount} failures → Idle",
                        LogLevel.Warn);
                    st.PathFailCount = 0;
                    st.Mode = NpcBehaviorMode.Idle;
                }
            }
        }

        private void TickPlantSeeds(NPC npc, string npcName, NpcBehaviorState st)
        {
            var location = npc.currentLocation;
            if (location is null || st.PlantSeedQueue is null || st.PlantSeedInventory is null || st.PlantSeedID is null)
            {
                st.Mode = NpcBehaviorMode.Idle;
                return;
            }

            Point target = st.PlantSeedTarget;
            var targetV2 = new Vector2(target.X, target.Y);

            // Check if HoeDirt still exists and is empty.
            location.terrainFeatures.TryGetValue(targetV2, out var tf);
            HoeDirt? dirt = tf as HoeDirt;
            bool hasHoeDirt = dirt != null && dirt.crop == null;

            float dist = Vector2.Distance(npc.Tile, new Vector2(target.X, target.Y));
            bool pathDone = npc.controller == null
                            || npc.controller.pathToEndPoint == null
                            || npc.controller.pathToEndPoint.Count == 0;

            if (dist <= 1.5f && pathDone)
            {
                // ── Plant phase ──────────────────────────────────────────
                if (hasHoeDirt)
                {
                    try
                    {
                        npc.faceDirection(2); // face down
                        // Crop constructor accepts either qualified "(O)490" or
                        // unqualified "490". HoeDirt.plant() internally calls
                        // TryGetData which is strict about prefix format — it failed
                        // with "(O)490". Direct crop assignment via ItemRegistry
                        // handles both forms.
                        var seedData = ItemRegistry.GetData(st.PlantSeedID);
                        string cropId = seedData?.ItemId ?? st.PlantSeedID;
                        // Strip the "(O)" prefix if present for Crop constructor.
                        if (cropId.StartsWith("(O)") || cropId.StartsWith("(o)"))
                            cropId = cropId.Substring(3);
                        dirt!.crop = new Crop(cropId, target.X, target.Y, location);
                        // Only consume from inventory if the NPC has the seed.
                        var items = st.PlantSeedInventory.GetItems(npcName);
                        bool hasSeed = items.Any(s => s.ItemId == st.PlantSeedID && s.Count > 0);
                        if (hasSeed)
                            st.PlantSeedInventory.Take(npcName, st.PlantSeedID, 1);
                        st.PlantSeededCount++;

                        _log.Log(
                            $"[FollowSystem/PlantSeeds] {npcName}: planted {st.PlantSeedID} at ({target.X},{target.Y})",
                            LogLevel.Debug);
                    }
                    catch (Exception ex)
                    {
                        _log.Log(
                            $"[FollowSystem/PlantSeeds] {npcName}: plant ({target.X},{target.Y}) failed: {ex.Message}",
                            LogLevel.Warn);
                    }
                }
                else
                {
                    _log.Log(
                        $"[FollowSystem/PlantSeeds] {npcName}: tile ({target.X},{target.Y}) no longer empty HoeDirt, skipping",
                        LogLevel.Debug);
                }

                // Advance to next target.
                if (st.PlantSeedQueue.Count == 0)
                {
                    _log.Log(
                        $"[FollowSystem/PlantSeeds] {npcName}: done, planted={st.PlantSeededCount} → Idle",
                        LogLevel.Info);
                    npc.doEmote(32); // happy
                    st.Mode = NpcBehaviorMode.Idle;
                    return;
                }

                st.PlantSeedTarget = st.PlantSeedQueue.Dequeue();
                st.PlantSeedPathed = false;
                st.LastPathTick    = 0;
                return;
            }

            // ── Pathing phase ──────────────────────────────────────────
            if ((!st.PlantSeedPathed || npc.controller == null) && this.ShouldReplan(st))
            {
                Point[] candidates = {
                    new(target.X,     target.Y + 1),
                    new(target.X,     target.Y - 1),
                    new(target.X + 1, target.Y),
                    new(target.X - 1, target.Y),
                };
                bool ok = false;
                foreach (var adj in candidates)
                {
                    if (this.TryStartPath(npc, location, adj)) { ok = true; break; }
                }
                st.PlantSeedPathed = ok;
                st.LastPathTick    = _tickCounter;
                if (ok)
                {
                    st.PathFailCount = 0;
                }
                else
                {
                    st.PathFailCount++;
                }
                _log.Log(
                    $"[FollowSystem/PlantSeeds] {npcName}: pathing to ({target.X},{target.Y}) ok={ok} fails={st.PathFailCount}",
                    LogLevel.Debug);

                // Skip unreachable target after MaxPathFailures consecutive failures.
                if (!ok && st.PathFailCount >= MaxPathFailures)
                {
                    _log.Log(
                        $"[FollowSystem/PlantSeeds] {npcName}: giving up on ({target.X},{target.Y}) after {st.PathFailCount} failures",
                        LogLevel.Warn);
                    st.PathFailCount = 0;
                    if (st.PlantSeedQueue.Count == 0)
                    {
                        st.Mode = NpcBehaviorMode.Idle;
                        return;
                    }
                    st.PlantSeedTarget = st.PlantSeedQueue.Dequeue();
                    st.PlantSeedPathed = false;
                    st.LastPathTick    = 0;
                }
            }
        }

        private void TickWaterCrops(NPC npc, string npcName, NpcBehaviorState st)
        {
            var location = npc.currentLocation;
            if (location is null || st.WaterCropQueue is null)
            {
                st.Mode = NpcBehaviorMode.Idle;
                return;
            }

            Point target = st.WaterCropTarget;
            var targetV2 = new Vector2(target.X, target.Y);

            // Check if HoeDirt still has a crop that needs watering.
            location.terrainFeatures.TryGetValue(targetV2, out var tf);
            HoeDirt? dirt = tf as HoeDirt;
            bool needsWater = dirt != null && dirt.crop != null && dirt.needsWatering();

            float dist = Vector2.Distance(npc.Tile, new Vector2(target.X, target.Y));
            bool pathDone = npc.controller == null
                            || npc.controller.pathToEndPoint == null
                            || npc.controller.pathToEndPoint.Count == 0;

            if (dist <= 1.5f && pathDone)
            {
                if (needsWater)
                {
                    try
                    {
                        npc.faceDirection(2); // face down
                        // No native watering animation on this NPC sprite —
                        // use a text bubble as visual feedback instead.
                        npc.showTextAboveHead("这是一个浇水动画");
                        dirt!.state.Value = 1; // 1 = watered
                        st.WaterCropCount++;

                        _log.Log(
                            $"[FollowSystem/WaterCrops] {npcName}: watered at ({target.X},{target.Y})",
                            LogLevel.Debug);
                    }
                    catch (Exception ex)
                    {
                        _log.Log(
                            $"[FollowSystem/WaterCrops] {npcName}: water ({target.X},{target.Y}) failed: {ex.Message}",
                            LogLevel.Warn);
                    }
                }
                else
                {
                    _log.Log(
                        $"[FollowSystem/WaterCrops] {npcName}: tile ({target.X},{target.Y}) no longer needs water, skipping",
                        LogLevel.Debug);
                }

                if (st.WaterCropQueue.Count == 0)
                {
                    _log.Log(
                        $"[FollowSystem/WaterCrops] {npcName}: done, watered={st.WaterCropCount} → Idle",
                        LogLevel.Info);
                    npc.doEmote(32);
                    st.Mode = NpcBehaviorMode.Idle;
                    return;
                }

                st.WaterCropTarget = st.WaterCropQueue.Dequeue();
                st.WaterCropPathed = false;
                st.LastPathTick    = 0;
                return;
            }

            if ((!st.WaterCropPathed || npc.controller == null) && this.ShouldReplan(st))
            {
                Point[] candidates = {
                    new(target.X,     target.Y + 1),
                    new(target.X,     target.Y - 1),
                    new(target.X + 1, target.Y),
                    new(target.X - 1, target.Y),
                };
                bool ok = false;
                foreach (var adj in candidates)
                {
                    if (this.TryStartPath(npc, location, adj)) { ok = true; break; }
                }
                st.WaterCropPathed = ok;
                st.LastPathTick    = _tickCounter;
                if (ok)
                {
                    st.PathFailCount = 0;
                }
                else
                {
                    st.PathFailCount++;
                }
                _log.Log(
                    $"[FollowSystem/WaterCrops] {npcName}: pathing to ({target.X},{target.Y}) ok={ok} fails={st.PathFailCount}",
                    LogLevel.Debug);

                // Skip unreachable target after MaxPathFailures consecutive failures.
                if (!ok && st.PathFailCount >= MaxPathFailures)
                {
                    _log.Log(
                        $"[FollowSystem/WaterCrops] {npcName}: giving up on ({target.X},{target.Y}) after {st.PathFailCount} failures",
                        LogLevel.Warn);
                    st.PathFailCount = 0;
                    if (st.WaterCropQueue.Count == 0)
                    {
                        st.Mode = NpcBehaviorMode.Idle;
                        return;
                    }
                    st.WaterCropTarget = st.WaterCropQueue.Dequeue();
                    st.WaterCropPathed = false;
                    st.LastPathTick    = 0;
                }
            }
        }

        private void TickHarvestCrops(NPC npc, string npcName, NpcBehaviorState st)
        {
            var location = npc.currentLocation;
            if (location is null || st.HarvestQueue is null || st.HarvestInventory is null)
            {
                st.Mode = NpcBehaviorMode.Idle;
                return;
            }

            Point target = st.HarvestTarget;
            var targetV2 = new Vector2(target.X, target.Y);

            // Verify the crop is still harvestable.
            location.terrainFeatures.TryGetValue(targetV2, out var tf);
            HoeDirt? dirt = tf as HoeDirt;
            bool canHarvest = dirt != null
                           && dirt.crop != null
                           && !dirt.crop.dead.Value
                           && dirt.crop.currentPhase.Value >= dirt.crop.phaseDays.Count - 1;

            float dist = Vector2.Distance(npc.Tile, new Vector2(target.X, target.Y));
            bool pathDone = npc.controller == null
                            || npc.controller.pathToEndPoint == null
                            || npc.controller.pathToEndPoint.Count == 0;

            if (dist <= 1.5f && pathDone)
            {
                if (canHarvest)
                {
                    try
                    {
                        npc.faceDirection(2); // face down
                        // crop.harvest() without a JunimoHarvester adds produce
                        // directly to the player's inventory. To put it in the NPC
                        // backpack instead, snapshot each slot BEFORE the call,
                        // then move only the newly added items per slot.
                        // Per-slot tracking avoids the key-collision bug of a
                        // Dictionary<string,int> when multiple stacks share the
                        // same ItemId+Quality.
                        int maxItems = Game1.player.MaxItems;
                        var slotBefore = new (string? itemId, int quality, int count)[maxItems];
                        for (int i = 0; i < maxItems; i++)
                        {
                            var item = Game1.player.Items[i];
                            if (item != null)
                                slotBefore[i] = (item.ItemId, item.Quality, item.Stack);
                        }

                        bool harvested = dirt!.crop.harvest((int)targetV2.X, (int)targetV2.Y, dirt);
                        if (harvested)
                        {
                            // Per-slot delta: compare each slot's before/after directly.
                            for (int i = 0; i < maxItems; i++)
                            {
                                var item = Game1.player.Items[i];
                                var before = slotBefore[i];

                                int delta = 0;
                                if (item != null && before.itemId != null
                                    && item.ItemId == before.itemId && item.Quality == before.quality)
                                {
                                    // Same item+quality in same slot — stack grew.
                                    delta = item.Stack - before.count;
                                }
                                else if (item != null && before.itemId == null)
                                {
                                    // Slot was empty before, now has an item — all from harvest.
                                    delta = item.Stack;
                                }
                                // else: slot changed to different item or emptied — skip.

                                if (delta > 0)
                                {
                                    int take = Math.Min(item!.Stack, delta);
                                    item.Stack -= take;
                                    if (item.Stack <= 0)
                                        Game1.player.Items[i] = null;
                                    st.HarvestInventory.Add(npcName, item.ItemId, take, item.Quality);
                                    st.HarvestedCount += take;
                                }
                            }
                            // Collect debris that harvest() spawned (e.g. when player
                            // inventory is full and items drop to the ground).
                            foreach (var d in location.debris.ToList())
                            {
                                if (d?.item is not null && d.Chunks.Count > 0)
                                {
                                    var chunkTile = new Vector2(
                                        (int)(d.Chunks[0].position.X / Game1.tileSize),
                                        (int)(d.Chunks[0].position.Y / Game1.tileSize));
                                    if (chunkTile == targetV2)
                                    {
                                        // Collect debris item into NPC inventory instead of deleting.
                                        var debrisItem = d.item;
                                        st.HarvestInventory.Add(npcName, debrisItem.ItemId, debrisItem.Stack, debrisItem.Quality);
                                        st.HarvestedCount += debrisItem.Stack;
                                        location.debris.Remove(d);
                                    }
                                }
                            }
                            // Clear dead crop from HoeDirt if harvest() left it behind.
                            // Single-harvest crops: harvest() sets dead=true + soil.crop=null.
                            // Multi-harvest crops: harvest() resets regrow state, crop alive.
                            // Only null out if the crop is actually dead.
                            if (dirt.crop != null && dirt.crop.dead.Value)
                                dirt.crop = null;
                            npc.doEmote(20);
                            _log.Log(
                                $"[FollowSystem/HarvestCrops] {npcName}: harvested at ({target.X},{target.Y}), " +
                                $"total={st.HarvestedCount}",
                                LogLevel.Debug);

                            _ = _broadcastEvent?.Invoke("crop_harvested", new
                            {
                                npc      = npcName,
                                tile_x   = target.X,
                                tile_y   = target.Y,
                                location = location.Name ?? "",
                            });
                        }
                        else
                        {
                            _log.Log(
                                $"[FollowSystem/HarvestCrops] {npcName}: harvest returned false at ({target.X},{target.Y})",
                                LogLevel.Debug);
                        }
                    }
                    catch (Exception ex)
                    {
                        _log.Log(
                            $"[FollowSystem/HarvestCrops] {npcName}: harvest ({target.X},{target.Y}) failed: {ex.Message}",
                            LogLevel.Warn);
                    }
                }
                else
                {
                    _log.Log(
                        $"[FollowSystem/HarvestCrops] {npcName}: tile ({target.X},{target.Y}) no longer harvestable, skipping",
                        LogLevel.Debug);
                }

                if (st.HarvestQueue.Count == 0)
                {
                    _log.Log(
                        $"[FollowSystem/HarvestCrops] {npcName}: done, harvested={st.HarvestedCount} → Idle",
                        LogLevel.Info);
                    npc.doEmote(32);
                    st.Mode = NpcBehaviorMode.Idle;
                    return;
                }

                st.HarvestTarget = st.HarvestQueue.Dequeue();
                st.HarvestPathed = false;
                st.LastPathTick  = 0;
                return;
            }

            if (!st.HarvestPathed || npc.controller == null || this.ShouldReplan(st))
            {
                Point[] candidates = {
                    new(target.X,     target.Y + 1),
                    new(target.X,     target.Y - 1),
                    new(target.X + 1, target.Y),
                    new(target.X - 1, target.Y),
                };
                bool ok = false;
                foreach (var adj in candidates)
                {
                    if (this.TryStartPath(npc, location, adj)) { ok = true; break; }
                }
                st.HarvestPathed = ok;
                st.LastPathTick  = _tickCounter;
                if (ok)
                {
                    st.PathFailCount = 0;
                }
                else
                {
                    st.PathFailCount++;
                }
                _log.Log(
                    $"[FollowSystem/HarvestCrops] {npcName}: pathing to ({target.X},{target.Y}) ok={ok} fails={st.PathFailCount}",
                    LogLevel.Debug);

                // Skip unreachable target after MaxPathFailures consecutive failures.
                if (!ok && st.PathFailCount >= MaxPathFailures)
                {
                    _log.Log(
                        $"[FollowSystem/HarvestCrops] {npcName}: giving up on ({target.X},{target.Y}) after {st.PathFailCount} failures",
                        LogLevel.Warn);
                    st.PathFailCount = 0;
                    if (st.HarvestQueue.Count == 0)
                    {
                        st.Mode = NpcBehaviorMode.Idle;
                        return;
                    }
                    st.HarvestTarget = st.HarvestQueue.Dequeue();
                    st.HarvestPathed = false;
                    st.LastPathTick  = 0;
                }
            }
        }

        private void TickWithdrawItems(NPC npc, string npcName, NpcBehaviorState st)
        {
            if (st.WithdrawInventory is null || st.WithdrawItemID is null)
            {
                st.Mode = NpcBehaviorMode.Idle;
                return;
            }

            // Resolve the target location.
            var location = string.IsNullOrEmpty(st.WithdrawChestMap)
                ? npc.currentLocation
                : Game1.getLocationFromName(st.WithdrawChestMap);

            if (location is null) { st.Mode = NpcBehaviorMode.Idle; return; }

            var chestV2 = new Vector2(st.WithdrawChestTile.X, st.WithdrawChestTile.Y);

            // Check chest still exists.
            if (!location.Objects.TryGetValue(chestV2, out var chestObj)
                || chestObj is not StardewValley.Objects.Chest chest)
            {
                _log.Log(
                    $"[FollowSystem/WithdrawItems] {npcName}: chest at ({st.WithdrawChestTile.X}," +
                    $"{st.WithdrawChestTile.Y}) gone → Idle (withdrawn={st.WithdrawnTotal})",
                    LogLevel.Debug);
                st.Mode = NpcBehaviorMode.Idle;
                return;
            }

            float dist = Vector2.Distance(npc.Tile,
                new Vector2(st.WithdrawChestTile.X, st.WithdrawChestTile.Y));
            bool pathDone = npc.controller == null
                            || npc.controller.pathToEndPoint == null
                            || npc.controller.pathToEndPoint.Count == 0;

            if (dist <= 1.5f && pathDone)
            {
                // ── Withdraw phase ────────────────────────────────────
                int remaining = st.WithdrawCount - st.WithdrawnTotal;
                if (remaining <= 0)
                {
                    _log.Log($"[FollowSystem/WithdrawItems] {npcName}: already got enough → Idle", LogLevel.Debug);
                    npc.doEmote(32);
                    st.Mode = NpcBehaviorMode.Idle;
                    return;
                }

                // Search chest for matching items.
                var matching = new List<StardewValley.Item>();
                foreach (var item in chest.Items)
                {
                    if (item != null && item.ItemId == st.WithdrawItemID)
                        matching.Add(item);
                }

                if (matching.Count == 0)
                {
                    _log.Log(
                        $"[FollowSystem/WithdrawItems] {npcName}: no {st.WithdrawItemID} in chest → Idle (withdrawn={st.WithdrawnTotal})",
                        LogLevel.Info);
                    npc.doEmote(16); // "!"
                    st.Mode = NpcBehaviorMode.Idle;
                    return;
                }

                int taken = 0;
                foreach (var item in matching)
                {
                    int canTake = Math.Min(item.Stack, remaining - taken);
                    if (canTake <= 0) break;

                    item.Stack -= canTake;
                    taken += canTake;

                    if (item.Stack <= 0)
                        chest.Items.Remove(item);
                }

                if (taken > 0)
                {
                    st.WithdrawInventory.Add(npcName, st.WithdrawItemID, taken);
                    st.WithdrawnTotal += taken;
                    _log.Log(
                        $"[FollowSystem/WithdrawItems] {npcName}: withdrew {taken}x {st.WithdrawItemID}, total={st.WithdrawnTotal}",
                        LogLevel.Debug);
                }

                npc.doEmote(32); // happy
                _log.Log(
                    $"[FollowSystem/WithdrawItems] {npcName}: done, total withdrawn={st.WithdrawnTotal} → Idle",
                    LogLevel.Info);
                st.Mode = NpcBehaviorMode.Idle;
                return;
            }

            // ── Pathing phase ──────────────────────────────────────────
            if ((!st.WithdrawPathed || npc.controller == null) && this.ShouldReplan(st))
            {
                Point ct = st.WithdrawChestTile;
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
                st.WithdrawPathed = ok;
                st.LastPathTick   = _tickCounter;
                if (ok)
                {
                    st.PathFailCount = 0;
                }
                else
                {
                    st.PathFailCount++;
                }
                _log.Log(
                    $"[FollowSystem/WithdrawItems] {npcName}: pathing to chest ({ct.X},{ct.Y}) ok={ok} fails={st.PathFailCount}",
                    LogLevel.Debug);

                if (!ok && st.PathFailCount >= MaxPathFailures)
                {
                    _log.Log(
                        $"[FollowSystem/WithdrawItems] {npcName}: giving up on chest ({ct.X},{ct.Y}) after {st.PathFailCount} failures → Idle",
                        LogLevel.Warn);
                    st.PathFailCount = 0;
                    st.Mode = NpcBehaviorMode.Idle;
                }
            }
        }

        private void TickBreakResource(NPC npc, string npcName, NpcBehaviorState st)
        {
            var location = npc.currentLocation;
            if (location is null || st.BreakResourceQueue is null || st.BreakResourceInventory is null)
            {
                st.Mode = NpcBehaviorMode.Idle;
                return;
            }

            ResourceTarget rt = st.BreakResourceTarget;
            Point target = rt.Tile;
            var targetV2 = new Vector2(target.X, target.Y);

            // Arrival check: ≤ 1.5 tiles and path finished.
            float dist = Vector2.Distance(npc.Tile, new Vector2(target.X, target.Y));
            bool pathDone = npc.controller == null
                            || npc.controller.pathToEndPoint == null
                            || npc.controller.pathToEndPoint.Count == 0;

            if (dist <= 1.5f && pathDone)
            {
                // ── Destroy phase ─────────────────────────────────────
                bool destroyed = false;
                npc.faceDirection(2); // face down

                if (rt.IsTree)
                {
                    // Remove tree from terrainFeatures.
                    if (location.terrainFeatures.TryGetValue(targetV2, out var tf)
                        && tf is StardewValley.TerrainFeatures.Tree)
                    {
                        location.terrainFeatures.Remove(targetV2);
                        AddTreeDrops(st.BreakResourceInventory, npcName, rt.TreeType, rt.IsStump, s_rng);
                        destroyed = true;
                        _log.Log(
                            $"[FollowSystem/BreakResource] {npcName}: broke {rt.TreeType} tree at ({target.X},{target.Y})",
                            LogLevel.Debug);
                    }
                }
                else
                {
                    // Remove stone from Objects.
                    if (location.Objects.TryGetValue(targetV2, out var obj) && obj is not null)
                    {
                        location.Objects.Remove(targetV2);
                        AddStoneDrops(st.BreakResourceInventory, npcName, rt.StoneIndex, s_rng);
                        destroyed = true;
                        _log.Log(
                            $"[FollowSystem/BreakResource] {npcName}: broke stone (PSI={rt.StoneIndex}) at ({target.X},{target.Y})",
                            LogLevel.Debug);
                    }
                }

                if (destroyed)
                {
                    st.BreakResourceCount++;
                    npc.doEmote(20); // heart — got item
                    _ = _broadcastEvent?.Invoke("resource_broken", new
                    {
                        npc       = npcName,
                        tile_x    = target.X,
                        tile_y    = target.Y,
                        is_tree   = rt.IsTree,
                        tree_type = rt.IsTree ? rt.TreeType : null,
                        location  = location.Name ?? "",
                    });
                }
                else
                {
                    _log.Log(
                        $"[FollowSystem/BreakResource] {npcName}: target at ({target.X},{target.Y}) already gone",
                        LogLevel.Debug);
                }

                // Advance to next target.
                if (st.BreakResourceQueue.Count == 0)
                {
                    _log.Log(
                        $"[FollowSystem/BreakResource] {npcName}: done, broken={st.BreakResourceCount} → Idle",
                        LogLevel.Info);
                    npc.doEmote(32); // happy
                    st.Mode = NpcBehaviorMode.Idle;
                    return;
                }

                st.BreakResourceTarget = st.BreakResourceQueue.Dequeue();
                st.BreakResourcePathed = false;
                st.LastPathTick        = 0;
                return;
            }

            // ── Pathing phase ──────────────────────────────────────────
            if ((!st.BreakResourcePathed || npc.controller == null) && this.ShouldReplan(st))
            {
                // Walk adjacent to the target (try 4 directions).
                Point[] candidates = {
                    new(target.X,     target.Y + 1),
                    new(target.X,     target.Y - 1),
                    new(target.X + 1, target.Y),
                    new(target.X - 1, target.Y),
                };
                bool ok = false;
                foreach (var adj in candidates)
                {
                    if (this.TryStartPath(npc, location, adj)) { ok = true; break; }
                }
                st.BreakResourcePathed = ok;
                st.LastPathTick        = _tickCounter;
                if (ok)
                {
                    st.PathFailCount = 0;
                }
                else
                {
                    st.PathFailCount++;
                }
                _log.Log(
                    $"[FollowSystem/BreakResource] {npcName}: pathing to ({target.X},{target.Y}) ok={ok} fails={st.PathFailCount}",
                    LogLevel.Debug);

                if (!ok && st.PathFailCount >= MaxPathFailures)
                {
                    _log.Log(
                        $"[FollowSystem/BreakResource] {npcName}: giving up on ({target.X},{target.Y}) after {st.PathFailCount} failures",
                        LogLevel.Warn);
                    st.PathFailCount = 0;
                    if (st.BreakResourceQueue.Count == 0)
                    {
                        st.Mode = NpcBehaviorMode.Idle;
                        return;
                    }
                    st.BreakResourceTarget = st.BreakResourceQueue.Dequeue();
                    st.BreakResourcePathed = false;
                    st.LastPathTick        = 0;
                }
            }
        }

        // ── helpers ───────────────────────────────────────────────────────

        private NpcBehaviorState GetOrCreate(string name)
        {
            if (!_states.TryGetValue(name, out var st))
            {
                st = new NpcBehaviorState();
                _states[name] = st;
            }
            return st;
        }

        private bool ShouldReplan(NpcBehaviorState st)
        {
            if (st.LastPathTick == 0) return true;
            return (_tickCounter - st.LastPathTick) >= ReplanIntervalTicks;
        }

        private bool TryStartPath(NPC npc, GameLocation location, Point endPoint)
        {
            try
            {
                npc.controller = new PathFindController(
                    c: npc,
                    location: location,
                    endPoint: endPoint,
                    finalFacingDirection: -1,
                    endBehaviorFunction: null);
            }
            catch (Exception ex)
            {
                _log.Log($"[FollowSystem] path {npc.Name}→({endPoint.X},{endPoint.Y}) failed: {ex.Message}", LogLevel.Trace);
                return false;
            }

            bool ok = npc.controller != null
                      && npc.controller.pathToEndPoint != null
                      && npc.controller.pathToEndPoint.Count > 0;
            return ok;
        }

        /// <summary>Tile one step "in front of" the player (based on facing).</summary>
        private Point TileInFront(Farmer player)
        {
            Vector2 t = player.Tile;
            return player.FacingDirection switch
            {
                0 => new Point((int)t.X,     (int)t.Y - 1), // up
                1 => new Point((int)t.X + 1, (int)t.Y),     // right
                2 => new Point((int)t.X,     (int)t.Y + 1), // down
                3 => new Point((int)t.X - 1, (int)t.Y),     // left
                _ => new Point((int)t.X,     (int)t.Y + 1),
            };
        }

        /// <summary>Tile one step "behind" the player (opposite of facing).</summary>
        private Vector2 TileBehind(Farmer player)
        {
            Vector2 t = player.Tile;
            return player.FacingDirection switch
            {
                0 => new Vector2(t.X,     t.Y + 1), // player faces up → behind is below
                1 => new Vector2(t.X - 1, t.Y),     // faces right → behind is left
                2 => new Vector2(t.X,     t.Y - 1), // faces down → behind is above
                3 => new Vector2(t.X + 1, t.Y),     // faces left → behind is right
                _ => new Vector2(t.X,     t.Y + 1),
            };
        }

        private static int FacePlayerDir(NPC npc, Farmer player)
        {
            Vector2 d = player.Tile - npc.Tile;
            if (Math.Abs(d.X) > Math.Abs(d.Y))
                return d.X > 0 ? 1 : 3;
            return d.Y > 0 ? 2 : 0;
        }

        private static int ManhattanDistance(Point a, Point b)
            => Math.Abs(a.X - b.X) + Math.Abs(a.Y - b.Y);

        private static int DirectionTo(Point from, Point to)
        {
            int dx = to.X - from.X;
            int dy = to.Y - from.Y;
            if (Math.Abs(dx) > Math.Abs(dy))
                return dx > 0 ? 1 : 3;
            if (dy != 0)
                return dy > 0 ? 2 : 0;
            return -1;
        }

        private void TickFertilize(NPC npc, string npcName, NpcBehaviorState st)
        {
            var location = npc.currentLocation;
            if (location is null || st.FertilizeQueue is null || st.FertilizeInventory is null || st.FertilizeID is null)
            {
                st.Mode = NpcBehaviorMode.Idle;
                return;
            }

            Point target = st.FertilizeTarget;
            var targetV2 = new Vector2(target.X, target.Y);

            // Verify HoeDirt still exists and is empty + unfertilized.
            location.terrainFeatures.TryGetValue(targetV2, out var tf);
            HoeDirt? dirt = tf as HoeDirt;
            bool canFertilize = dirt != null
                             && dirt.crop == null
                             && string.IsNullOrEmpty(dirt.fertilizer.Value);

            float dist = Vector2.Distance(npc.Tile, new Vector2(target.X, target.Y));
            bool pathDone = npc.controller == null
                            || npc.controller.pathToEndPoint == null
                            || npc.controller.pathToEndPoint.Count == 0;

            if (dist <= 1.5f && pathDone)
            {
                if (canFertilize)
                {
                    try
                    {
                        npc.faceDirection(2);
                        dirt!.fertilizer.Value = st.FertilizeID;
                        // Consume from inventory if available.
                        var items = st.FertilizeInventory.GetItems(npcName);
                        bool hasIt = items.Any(s => s.ItemId == st.FertilizeID && s.Count > 0);
                        if (hasIt)
                            st.FertilizeInventory.Take(npcName, st.FertilizeID, 1);
                        st.FertilizedCount++;

                        _log.Log(
                            $"[FollowSystem/Fertilize] {npcName}: applied {st.FertilizeID} at ({target.X},{target.Y})",
                            LogLevel.Debug);
                    }
                    catch (Exception ex)
                    {
                        _log.Log(
                            $"[FollowSystem/Fertilize] {npcName}: fertilize ({target.X},{target.Y}) failed: {ex.Message}",
                            LogLevel.Warn);
                    }
                }
                else
                {
                    _log.Log(
                        $"[FollowSystem/Fertilize] {npcName}: tile ({target.X},{target.Y}) no longer fertilizable, skipping",
                        LogLevel.Debug);
                }

                if (st.FertilizeQueue.Count == 0)
                {
                    _log.Log(
                        $"[FollowSystem/Fertilize] {npcName}: done, fertilized={st.FertilizedCount} → Idle",
                        LogLevel.Info);
                    npc.doEmote(32);
                    st.Mode = NpcBehaviorMode.Idle;
                    return;
                }

                st.FertilizeTarget = st.FertilizeQueue.Dequeue();
                st.FertilizePathed = false;
                st.LastPathTick    = 0;
                return;
            }

            if ((!st.FertilizePathed || npc.controller == null) && this.ShouldReplan(st))
            {
                Point[] candidates = {
                    new(target.X,     target.Y + 1),
                    new(target.X,     target.Y - 1),
                    new(target.X + 1, target.Y),
                    new(target.X - 1, target.Y),
                };
                bool ok = false;
                foreach (var adj in candidates)
                {
                    if (this.TryStartPath(npc, location, adj)) { ok = true; break; }
                }
                st.FertilizePathed = ok;
                st.LastPathTick    = _tickCounter;
                if (ok) { st.PathFailCount = 0; } else { st.PathFailCount++; }
                _log.Log(
                    $"[FollowSystem/Fertilize] {npcName}: pathing to ({target.X},{target.Y}) ok={ok} fails={st.PathFailCount}",
                    LogLevel.Debug);

                if (!ok && st.PathFailCount >= MaxPathFailures)
                {
                    _log.Log(
                        $"[FollowSystem/Fertilize] {npcName}: giving up on ({target.X},{target.Y}) after {st.PathFailCount} failures",
                        LogLevel.Warn);
                    st.PathFailCount = 0;
                    if (st.FertilizeQueue.Count == 0) { st.Mode = NpcBehaviorMode.Idle; return; }
                    st.FertilizeTarget = st.FertilizeQueue.Dequeue();
                    st.FertilizePathed = false;
                    st.LastPathTick    = 0;
                }
            }
        }

        // ── resource drop helpers ──────────────────────────────────────

        /// <summary>Add tree-felling drops to the NPC inventory based on tree type.</summary>
        internal static void AddTreeDrops(NpcInventory inventory, string npcName, string treeType, bool isStump, Random rng)
        {
            if (isStump)
            {
                inventory.Add(npcName, "(O)709", rng.Next(2, 5)); // Hardwood
                return;
            }

            switch (treeType.ToLowerInvariant())
            {
                case "oak":
                    inventory.Add(npcName, "(O)388", rng.Next(10, 21)); // Wood
                    inventory.Add(npcName, "(O)92",  rng.Next(3, 6));  // Sap
                    if (rng.Next(2) == 0) inventory.Add(npcName, "(O)309", 1); // Acorn
                    break;
                case "maple":
                    inventory.Add(npcName, "(O)388", rng.Next(10, 21));
                    inventory.Add(npcName, "(O)92",  rng.Next(3, 6));
                    if (rng.Next(2) == 0) inventory.Add(npcName, "(O)310", 1); // Maple Seed
                    break;
                case "pine":
                    inventory.Add(npcName, "(O)388", rng.Next(10, 21));
                    inventory.Add(npcName, "(O)92",  rng.Next(3, 6));
                    if (rng.Next(2) == 0) inventory.Add(npcName, "(O)311", 1); // Pine Cone
                    break;
                case "mahogany":
                    inventory.Add(npcName, "(O)709", rng.Next(8, 13));  // Hardwood
                    inventory.Add(npcName, "(O)92",  rng.Next(2, 5));
                    if (rng.Next(3) == 0) inventory.Add(npcName, "(O)292", 1); // Mahogany Seed
                    break;
                case "mushroom":
                    inventory.Add(npcName, "(O)420", rng.Next(1, 4));  // Red Mushroom
                    if (rng.Next(2) == 0) inventory.Add(npcName, "(O)422", rng.Next(1, 3));
                    break;
                case "palm":
                    inventory.Add(npcName, "(O)388", rng.Next(6, 11));
                    inventory.Add(npcName, "(O)92",  rng.Next(1, 4));
                    break;
                default:
                    inventory.Add(npcName, "(O)388", rng.Next(5, 11));
                    inventory.Add(npcName, "(O)92",  rng.Next(2, 5));
                    break;
            }
        }

        /// <summary>Add stone-breaking drops to the NPC inventory.</summary>
        internal static void AddStoneDrops(NpcInventory inventory, string npcName, int psi, Random rng)
        {
            int stone = psi >= 56 ? rng.Next(6, 13) : rng.Next(3, 9);
            inventory.Add(npcName, "(O)390", stone); // Stone
            if (rng.Next(4) == 0) inventory.Add(npcName, "(O)382", 1); // Coal (25%)
            // Small chance of a gem from larger stones.
            if (psi >= 50 && rng.Next(10) == 0)
                inventory.Add(npcName, "(O)80", 1); // Quartz
        }

        /// <summary>Point along (from→to) capped at <paramref name="maxSteps"/> tiles.</summary>
        private static Point SegmentTarget(Point from, Point to, int maxSteps)
        {
            int dx = to.X - from.X;
            int dy = to.Y - from.Y;
            int dist = Math.Abs(dx) + Math.Abs(dy);
            if (dist <= maxSteps) return to;

            // Move proportionally along the manhattan path.
            int stepX = 0, stepY = 0;
            int remaining = maxSteps;

            // Bias the axis with larger delta.
            if (Math.Abs(dx) >= Math.Abs(dy))
            {
                stepX = Math.Sign(dx) * Math.Min(Math.Abs(dx), remaining);
                remaining -= Math.Abs(stepX);
                stepY = Math.Sign(dy) * Math.Min(Math.Abs(dy), remaining);
            }
            else
            {
                stepY = Math.Sign(dy) * Math.Min(Math.Abs(dy), remaining);
                remaining -= Math.Abs(stepY);
                stepX = Math.Sign(dx) * Math.Min(Math.Abs(dx), remaining);
            }

            return new Point(from.X + stepX, from.Y + stepY);
        }
    }
}
