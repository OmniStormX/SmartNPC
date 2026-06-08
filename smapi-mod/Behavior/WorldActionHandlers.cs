// Per-action handler files for world actions.
// Each is a minimal subclass of NpcActionHandlerBase. Override Execute
// when ready to implement real game logic.

using System;
using System.Collections.Generic;
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
            return $"[wander] r={radius}";
        }

        protected override void Execute(NPC npc, string npcName, JsonElement @params)
        {
            DoWander(npc, npcName, ParseRadius(@params), _follow, Log);
        }

        /// <summary>
        /// Start continuous wander for <paramref name="npc"/>: walk to a random passable tile
        /// within <paramref name="radius"/>, arrive, pick another, repeat.
        /// Delegates controller ownership to FollowSystem so the Idle guard never cancels mid-walk.
        /// Game-thread safe — called from Execute and the smartnpc_wander debug command.
        /// </summary>
        public static void DoWander(NPC npc, string npcName, int radius, FollowSystem follow, IMonitor log)
        {
            follow.StartWander(npcName, npc, radius);
            log.Log($"[npc_wander] {npc.Name} continuous wander started radius={radius}", LogLevel.Debug);
        }

        private static int ParseRadius(JsonElement @params)
        {
            if (@params.ValueKind == JsonValueKind.Object &&
                @params.TryGetProperty("radius", out JsonElement el) &&
                el.TryGetInt32(out int r) && r > 0)
                return Math.Clamp(r, 1, 24);
            return 8;
        }
    }

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
                // Walk NPC toward target (best-effort; no async await here).
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
    }

    internal sealed class WaterCropsHandler : NpcActionHandlerBase
    {
        protected override string ActionName => "npc_water_crops";
        public WaterCropsHandler(IMonitor log, Func<bool> showBubble) : base(log, showBubble) { }
        // TODO: find unwatered crops in range and water them
    }

    internal sealed class HarvestCropsHandler : NpcActionHandlerBase
    {
        protected override string ActionName => "npc_harvest_crops";
        public HarvestCropsHandler(IMonitor log, Func<bool> showBubble) : base(log, showBubble) { }
        // TODO: find harvestable crops and collect them
    }

    internal sealed class DepositItemsHandler : NpcActionHandlerBase
    {
        protected override string ActionName => "npc_deposit_items";
        public DepositItemsHandler(IMonitor log, Func<bool> showBubble) : base(log, showBubble) { }
        // TODO: find nearby chest and deposit held items
    }

    internal sealed class DeliverItemsHandler : NpcActionHandlerBase
    {
        protected override string ActionName => "npc_deliver_items";
        public DeliverItemsHandler(IMonitor log, Func<bool> showBubble) : base(log, showBubble) { }
        // TODO: pathfind to target and hand over items
    }

    internal sealed class ForageCollectHandler : NpcActionHandlerBase
    {
        protected override string ActionName => "npc_forage_collect";
        public ForageCollectHandler(IMonitor log, Func<bool> showBubble) : base(log, showBubble) { }
        // TODO: find forage items in range and pick them up
    }

    internal sealed class PetAnimalHandler : NpcActionHandlerBase
    {
        protected override string ActionName => "npc_pet_animal";
        public PetAnimalHandler(IMonitor log, Func<bool> showBubble) : base(log, showBubble) { }
        // TODO: find nearby animal and pet it
    }

    internal sealed class PlantSeedsHandler : NpcActionHandlerBase
    {
        protected override string ActionName => "npc_plant_seeds";
        public PlantSeedsHandler(IMonitor log, Func<bool> showBubble) : base(log, showBubble) { }
        // TODO: find tilled soil and plant seeds from inventory
    }

    internal sealed class TillSoilHandler : NpcActionHandlerBase
    {
        protected override string ActionName => "npc_till_soil";
        public TillSoilHandler(IMonitor log, Func<bool> showBubble) : base(log, showBubble) { }
        // TODO: find diggable tiles and till them
    }

    internal sealed class InspectObjectHandler : NpcActionHandlerBase
    {
        protected override string ActionName => "npc_inspect_object";
        public InspectObjectHandler(IMonitor log, Func<bool> showBubble) : base(log, showBubble) { }
        // TODO: face target object and show inspection result
    }

    internal sealed class PlaceObjectHandler : NpcActionHandlerBase
    {
        protected override string ActionName => "npc_place_object";
        public PlaceObjectHandler(IMonitor log, Func<bool> showBubble) : base(log, showBubble) { }
        // TODO: place item from inventory onto target tile
    }
}
