// NPC perception system — scans the NPC's map on a configurable interval
// and caches what other characters / map features are nearby. Exposes the
// cached snapshot via the `npc_get_nearby` and `npc_get_environment` ws
// actions.
//
// Design notes:
//   - Read-only. Does not mutate game state.
//   - Cache is keyed by NPC internal name. Handlers read from the cache only,
//     never touch Game1 directly — that avoids thread-safety issues (handlers
//     run on the ws receive loop, the scan runs on the game thread).
//   - Scan interval is configurable (default 60 ticks ≈ 1s). A single scan
//     refreshes all registered NPCs.
//   - Event push (`npc_perception_update`) is intentionally NOT emitted yet;
//     the field set is spec'd in docs/protocol.md but wiring is deferred
//     until the Agent Loop actually consumes it.
//
// Public surface:
//   - PerceptionSystem.PumpOnGameTick(): drive the scan from ModEntry.OnUpdateTicked
//   - PerceptionSystem.HandleGetNearby / HandleGetEnvironment: ws handlers

using System;
using System.Collections.Generic;
using System.Linq;
using System.Text.Json;
using System.Text.Json.Serialization;
using System.Threading.Tasks;
using Microsoft.Xna.Framework;
using StardewModdingAPI;
using StardewValley;
using StardewValley.TerrainFeatures;

namespace SmartNPC.Bridge
{
    /// <summary>
    /// Maintains a tick-driven snapshot of each Agent-managed NPC's surroundings.
    /// </summary>
    internal sealed class PerceptionSystem
    {
        // ── tunables ────────────────────────────────────────────────
        private const int DefaultScanIntervalTicks = 60;      // ~1 second at 60 fps
        private const float DefaultRadiusTiles     = 10f;
        private const int   MaxNearbyObjects       = 16;      // cap per env query

        private readonly IMonitor _log;
        private readonly int _scanIntervalTicks;

        // Cache: NPC name -> last snapshot (produced on the game thread, read by handlers).
        private readonly Dictionary<string, PerceptionSnapshot> _snapshots =
            new(StringComparer.OrdinalIgnoreCase);
        private readonly object _snapshotLock = new();

        private uint _tickCounter;

        public PerceptionSystem(IMonitor log, int scanIntervalTicks = DefaultScanIntervalTicks)
        {
            _log = log;
            _scanIntervalTicks = scanIntervalTicks > 0 ? scanIntervalTicks : DefaultScanIntervalTicks;
        }

        // ── tick hook ───────────────────────────────────────────────

        /// <summary>Called from ModEntry.OnUpdateTicked.</summary>
        public void PumpOnGameTick()
        {
            if (!Context.IsWorldReady) return;

            _tickCounter++;
            if (_tickCounter % (uint)_scanIntervalTicks != 0) return;

            try
            {
                this.ScanAll();
            }
            catch (Exception ex)
            {
                _log.Log($"[Perception] scan failed: {ex}", LogLevel.Warn);
            }
        }

        private void ScanAll()
        {
            foreach (string name in AgentNpcRegistry.GetAll())
            {
                NPC? npc = Game1.getCharacterFromName(name);
                if (npc == null || npc.currentLocation == null) continue;

                PerceptionSnapshot snap = BuildSnapshot(npc, DefaultRadiusTiles);
                lock (_snapshotLock)
                {
                    _snapshots[name] = snap;
                }
            }
        }

        // ── handlers ────────────────────────────────────────────────

        public Task<Response> HandleGetNearby(string id, JsonElement @params)
        {
            if (!Context.IsWorldReady)
                return Task.FromResult(Response.Failure(id, "mod_not_ready", "no save loaded"));

            NearbyParams? p;
            try { p = JsonSerializer.Deserialize<NearbyParams>(@params, JsonOpts.Web); }
            catch (Exception ex) { return Task.FromResult(Response.Failure(id, "invalid_params", ex.Message)); }

            if (p is null || string.IsNullOrWhiteSpace(p.Npc))
                return Task.FromResult(Response.Failure(id, "invalid_params", "npc is required"));

            float radius = p.Radius.HasValue && p.Radius.Value > 0
                ? (float)p.Radius.Value
                : DefaultRadiusTiles;

            // If the cache is fresh (last scan covered this NPC at the default radius)
            // AND the caller asked for the default radius, reuse it. Otherwise run an
            // on-demand scan so non-default radii produce correct output.
            PerceptionSnapshot? snap = null;
            if (Math.Abs(radius - DefaultRadiusTiles) < 0.001f)
            {
                lock (_snapshotLock)
                {
                    _snapshots.TryGetValue(p.Npc, out snap);
                }
            }

            if (snap == null)
            {
                NPC? npc = Game1.getCharacterFromName(p.Npc);
                if (npc == null || npc.currentLocation == null)
                    return Task.FromResult(Response.Failure(id, "npc_not_found", $"no npc named '{p.Npc}'"));
                snap = BuildSnapshot(npc, radius);
            }

            var result = new
            {
                ok     = true,
                npc    = p.Npc,
                map    = snap.Map,
                radius = (double)radius,
                count  = snap.Nearby.Count,
                nearby = snap.Nearby,
            };
            return Task.FromResult(Response.Success(id, result));
        }

        public Task<Response> HandleGetEnvironment(string id, JsonElement @params)
        {
            if (!Context.IsWorldReady)
                return Task.FromResult(Response.Failure(id, "mod_not_ready", "no save loaded"));

            EnvParams? p;
            try { p = JsonSerializer.Deserialize<EnvParams>(@params, JsonOpts.Web); }
            catch (Exception ex) { return Task.FromResult(Response.Failure(id, "invalid_params", ex.Message)); }

            if (p is null || string.IsNullOrWhiteSpace(p.Npc))
                return Task.FromResult(Response.Failure(id, "invalid_params", "npc is required"));

            NPC? npc = Game1.getCharacterFromName(p.Npc);
            if (npc == null || npc.currentLocation == null)
                return Task.FromResult(Response.Failure(id, "npc_not_found", $"no npc named '{p.Npc}'"));

            var tile = npc.Tile;
            string map = npc.currentLocation.Name ?? "";

            string weather = "sunny";
            if (Game1.isLightning)    weather = "stormy";
            else if (Game1.isRaining) weather = "rainy";
            else if (Game1.isSnowing) weather = "snowy";

            List<NearbyObject> objects = CollectNearbyObjects(npc, DefaultRadiusTiles);

            // nearby_count: other NPCs + farmers on the same map (excluding self).
            GameLocation loc = npc.currentLocation!;
            int nearbyCount = 0;
            foreach (NPC other in loc.characters)
                if (other != null && other != npc) nearbyCount++;
            foreach (Farmer farmer in loc.farmers)
                if (farmer != null) nearbyCount++;

            var result = new
            {
                ok             = true,
                npc            = p.Npc,
                map,
                x              = (double)tile.X,
                y              = (double)tile.Y,
                facing         = npc.FacingDirection,
                direction      = FacingName(npc.FacingDirection),
                time_of_day    = Game1.timeOfDay,
                hour           = Game1.timeOfDay / 100,
                minute         = Game1.timeOfDay % 100,
                season         = Game1.currentSeason,
                weather,
                nearby_count   = nearbyCount,
                nearby_objects = objects,
            };
            return Task.FromResult(Response.Success(id, result));
        }

        // ── scan implementation ─────────────────────────────────────

        private static PerceptionSnapshot BuildSnapshot(NPC self, float radius)
        {
            var selfTile = self.Tile;
            string map = self.currentLocation?.Name ?? "";
            var nearby = new List<NearbyEntity>();

            GameLocation? loc = self.currentLocation;
            if (loc == null) return new PerceptionSnapshot(map, nearby);

            // Other NPCs on the same map.
            foreach (NPC other in loc.characters)
            {
                if (other == null || other == self) continue;
                float d = Vector2.Distance(selfTile, other.Tile);
                if (d > radius) continue;
                nearby.Add(new NearbyEntity
                {
                    Name     = other.Name ?? "",
                    Type     = "npc",
                    X        = other.Tile.X,
                    Y        = other.Tile.Y,
                    Distance = Math.Round(d, 2),
                    Facing   = other.FacingDirection,
                    Direction = FacingName(other.FacingDirection),
                    Map      = map,
                });
            }

            // Players on the same map (single-player = just the host farmer).
            foreach (Farmer farmer in loc.farmers)
            {
                if (farmer == null) continue;
                float d = Vector2.Distance(selfTile, farmer.Tile);
                if (d > radius) continue;
                nearby.Add(new NearbyEntity
                {
                    Name     = farmer.Name ?? "Farmer",
                    Type     = "player",
                    X        = farmer.Tile.X,
                    Y        = farmer.Tile.Y,
                    Distance = Math.Round(d, 2),
                    Facing   = farmer.FacingDirection,
                    Direction = FacingName(farmer.FacingDirection),
                    Map      = map,
                    Action   = DescribePlayerAction(farmer),
                });
            }

            nearby.Sort((a, b) => a.Distance.CompareTo(b.Distance));
            return new PerceptionSnapshot(map, nearby);
        }

        private static string DescribePlayerAction(Farmer f)
        {
            if (f.isMoving())    return "walking";
            if (f.UsingTool)     return "using_tool";
            if (f.IsSitting())   return "sitting";
            return "idle";
        }

        private static string FacingName(int dir) => dir switch
        {
            0 => "up",
            1 => "right",
            2 => "down",
            3 => "left",
            _ => "down",
        };

        private static List<NearbyObject> CollectNearbyObjects(NPC self, float radius)
        {
            var result = new List<NearbyObject>();
            GameLocation? loc = self.currentLocation;
            if (loc == null) return result;

            var selfTile = self.Tile;

            // Placed objects (crafting stations, lanterns, artisan goods, etc.).
            foreach (var kv in loc.Objects.Pairs)
            {
                Vector2 tile = kv.Key;
                float d = Vector2.Distance(selfTile, tile);
                if (d > radius) continue;
                var obj = kv.Value;
                result.Add(new NearbyObject
                {
                    Name     = obj?.DisplayName ?? obj?.Name ?? "object",
                    Category = "object",
                    X        = tile.X,
                    Y        = tile.Y,
                    Distance = Math.Round(d, 2),
                });
                if (result.Count >= MaxNearbyObjects) return result;
            }

            // Terrain features — include crops (HoeDirt with a Crop) explicitly.
            foreach (var kv in loc.terrainFeatures.Pairs)
            {
                Vector2 tile = kv.Key;
                float d = Vector2.Distance(selfTile, tile);
                if (d > radius) continue;
                TerrainFeature tf = kv.Value;
                string category = "terrain";
                string name = tf.GetType().Name;
                if (tf is HoeDirt dirt && dirt.crop != null)
                {
                    category = "crop";
                    name     = $"crop_{dirt.crop.indexOfHarvest.Value}";
                }
                else if (tf is Tree)
                {
                    category = "terrain";
                    name = "tree";
                }
                result.Add(new NearbyObject
                {
                    Name     = name,
                    Category = category,
                    X        = tile.X,
                    Y        = tile.Y,
                    Distance = Math.Round(d, 2),
                });
                if (result.Count >= MaxNearbyObjects) return result;
            }

            result.Sort((a, b) => a.Distance.CompareTo(b.Distance));
            return result;
        }

        // ── DTOs ────────────────────────────────────────────────────

        private sealed class NearbyParams
        {
            [JsonPropertyName("npc")]    public string? Npc    { get; set; }
            [JsonPropertyName("radius")] public double? Radius { get; set; }
        }

        private sealed class EnvParams
        {
            [JsonPropertyName("npc")] public string? Npc { get; set; }
        }

        private sealed class PerceptionSnapshot
        {
            public string Map { get; }
            public IReadOnlyList<NearbyEntity> Nearby { get; }

            public PerceptionSnapshot(string map, IReadOnlyList<NearbyEntity> nearby)
            {
                Map = map;
                Nearby = nearby;
            }
        }

        // Public so System.Text.Json can serialize them.
        public sealed class NearbyEntity
        {
            [JsonPropertyName("name")]              public string Name     { get; set; } = "";
            [JsonPropertyName("type")]              public string Type     { get; set; } = "";
            [JsonPropertyName("x")]                 public float  X        { get; set; }
            [JsonPropertyName("y")]                 public float  Y        { get; set; }
            [JsonPropertyName("distance")]          public double Distance { get; set; }
            [JsonPropertyName("facing")]            public int    Facing   { get; set; }
            [JsonPropertyName("direction")]         public string Direction { get; set; } = "down";
            [JsonPropertyName("map")]               public string Map      { get; set; } = "";
            [JsonPropertyName("action")]
            [JsonIgnore(Condition = JsonIgnoreCondition.WhenWritingNull)]
            public string? Action { get; set; }
        }

        public sealed class NearbyObject
        {
            [JsonPropertyName("name")]     public string Name     { get; set; } = "";
            [JsonPropertyName("category")] public string Category { get; set; } = "";
            [JsonPropertyName("x")]        public float  X        { get; set; }
            [JsonPropertyName("y")]        public float  Y        { get; set; }
            [JsonPropertyName("distance")] public double Distance { get; set; }
        }
    }
}
