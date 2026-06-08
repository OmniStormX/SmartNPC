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
using Microsoft.Xna.Framework;
using StardewModdingAPI;
using StardewValley;
using StardewValley.Pathfinding;

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
        DepositItems,
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

        // Tick scheduler: only repath on these boundaries.
        public uint LastPathTick { get; set; }

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

        // Follow radius: stay within this many tiles of the player. If farther,
        // the NPC is repathed behind the player.
        private const float FollowRadiusTiles = 3f;

        // Leading segment length: walk at most this many tiles per replan.
        private const int LeadSegmentTiles = 5;

        private static readonly Random s_rng = new();

        private readonly IMonitor _log;
        private readonly Dictionary<string, NpcBehaviorState> _states =
            new(StringComparer.OrdinalIgnoreCase);

        private uint _tickCounter;

        public FollowSystem(IMonitor log) { _log = log; }

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
        /// Start continuous wander for <paramref name="npcName"/>: pick a random passable tile
        /// within <paramref name="radius"/>, walk there, then repeat automatically until
        /// <see cref="StopFollow"/> is called or the mode is superseded.
        /// FollowSystem owns the controller so the Idle guard never cancels mid-walk.
        /// </summary>
        public void StartWander(string npcName, NPC npc, int radius)
        {
            var st = this.GetOrCreate(npcName);
            var prev = st.Mode;
            st.WanderRadius = Math.Clamp(radius, 1, 24);

            Point? dest = PickWanderDest(npc, st.WanderRadius, s_rng);
            if (dest is null)
            {
                _log.Log($"[FollowSystem/StartWander] {npcName}: no passable tile, aborting", LogLevel.Warn);
                return;
            }

            st.Mode = NpcBehaviorMode.Wander;
            st.WanderTarget = dest.Value;
            st.LastPathTick = 0;
            _log.Log(
                $"[FollowSystem/StartWander] {npcName}: mode {prev}→Wander " +
                $"dest=({dest.Value.X},{dest.Value.Y}) radius={radius}",
                LogLevel.Debug);
        }

        /// <summary>
        /// Pick a random passable tile within <paramref name="radius"/> tiles of <paramref name="npc"/>.
        /// Returns null if no candidate is found in 20 attempts.
        /// </summary>
        internal static Point? PickWanderDest(NPC npc, int radius, Random rng)
        {
            var location = npc.currentLocation;
            if (location is null) return null;

            int ox = (int)(npc.Position.X / 64f);
            int oy = (int)(npc.Position.Y / 64f);

            for (int attempt = 0; attempt < 20; attempt++)
            {
                int tx = ox + rng.Next(-radius, radius + 1);
                int ty = oy + rng.Next(-radius, radius + 1);
                if (tx == ox && ty == oy) continue;

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
                        case NpcBehaviorMode.DepositItems:
                            this.TickDepositItems(npc, name, st);
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

                st.LastPathTick = _tickCounter > 0 ? _tickCounter : 1;
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
            Point? next = PickWanderDest(npc, st.WanderRadius, s_rng);
            if (next is null)
            {
                _log.Log(
                    $"[FollowSystem/Wander] {npc.Name}: no passable tile found, going Idle",
                    LogLevel.Debug);
                st.Mode = NpcBehaviorMode.Idle;
                return;
            }

            st.WanderTarget = next.Value;
            st.LastPathTick = 0;   // trigger path kick on the very next tick
            _log.Log(
                $"[FollowSystem/Wander] {npc.Name}: arrived → next dest=({next.Value.X},{next.Value.Y})",
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

            // Not yet arrived — start or continue pathing.
            if (!st.DebrisPathed || npc.controller == null)
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
                st.LastPathTick = _tickCounter > 0 ? _tickCounter : 1;

                _log.Log(
                    $"[FollowSystem/ClearDebris] {npcName}: pathing to ({target.X},{target.Y}) ok={ok}",
                    LogLevel.Debug);
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
