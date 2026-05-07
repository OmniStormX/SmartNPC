// Handles NPC movement actions: npc_move_to, npc_face_direction, npc_get_position.
//
// All operations that touch game state (PathFindController, NPC.faceDirection,
// tile lookups) are marshalled onto the game thread via PumpOnGameTick. The
// inbound ws thread enqueues a work item with a TaskCompletionSource, then
// awaits the response.

using System;
using System.Collections.Concurrent;
using System.Text.Json;
using System.Text.Json.Serialization;
using System.Threading.Tasks;
using Microsoft.Xna.Framework;
using StardewModdingAPI;
using StardewValley;
using StardewValley.Pathfinding;

namespace SmartNPC.Bridge
{
    internal sealed class MovementHandler
    {
        private readonly IMonitor _log;
        private readonly ConcurrentQueue<Action> _pending = new();

        public MovementHandler(IMonitor log) { _log = log; }

        // ── npc_move_to ────────────────────────────────────────────

        public Task<Response> HandleMoveTo(string id, JsonElement @params)
        {
            if (!Context.IsWorldReady)
                return Task.FromResult(Response.Failure(id, "mod_not_ready", "no save loaded"));

            MoveToParams? p;
            try { p = JsonSerializer.Deserialize<MoveToParams>(@params, JsonOpts.Web); }
            catch (Exception ex) { return Task.FromResult(Response.Failure(id, "invalid_params", ex.Message)); }

            if (p is null || string.IsNullOrWhiteSpace(p.Npc))
                return Task.FromResult(Response.Failure(id, "invalid_params", "npc is required"));

            var tcs = new TaskCompletionSource<Response>();
            _pending.Enqueue(() =>
            {
                try
                {
                    NPC? npc = Game1.getCharacterFromName(p.Npc!);
                    if (npc is null)
                    {
                        tcs.TrySetResult(Response.Failure(id, "npc_not_found", $"no NPC named '{p.Npc}'"));
                        return;
                    }

                    // Decide target location. Default to NPC's current map.
                    GameLocation? target;
                    if (string.IsNullOrWhiteSpace(p.Map))
                    {
                        target = npc.currentLocation;
                    }
                    else
                    {
                        target = Game1.getLocationFromName(p.Map);
                        if (target is null)
                        {
                            tcs.TrySetResult(Response.Failure(id, "map_not_found", $"no map named '{p.Map}'"));
                            return;
                        }
                    }

                    if (target is null)
                    {
                        tcs.TrySetResult(Response.Failure(id, "no_location", "could not resolve target location"));
                        return;
                    }

                    var endPoint = new Point(p.X, p.Y);
                    string resolvedMap = target.NameOrUniqueName ?? target.Name;

                    // Same-map path: use PathFindController.
                    if (npc.currentLocation == target)
                    {
                        try
                        {
                            npc.controller = new PathFindController(
                                c: npc,
                                location: target,
                                endPoint: endPoint,
                                finalFacingDirection: -1,
                                endBehaviorFunction: null);
                        }
                        catch (Exception ex)
                        {
                            tcs.TrySetResult(Response.Failure(id, "pathfind_error", ex.Message));
                            return;
                        }

                        bool ok = npc.controller != null && npc.controller.pathToEndPoint != null && npc.controller.pathToEndPoint.Count > 0;
                        var res = new MoveToResult
                        {
                            Ok = ok,
                            Npc = p.Npc,
                            Map = resolvedMap,
                            X = p.X,
                            Y = p.Y,
                            Message = ok ? "pathing" : "no_route",
                        };
                        tcs.TrySetResult(Response.Success(id, res));
                        return;
                    }

                    // Cross-map warp: teleport the NPC instantly. (Full cross-map
                    // pathfinding would require chained PathFindController hops;
                    // keep M3 simple.)
                    Game1.warpCharacter(npc, target, new Vector2(p.X, p.Y));
                    tcs.TrySetResult(Response.Success(id, new MoveToResult
                    {
                        Ok = true,
                        Npc = p.Npc,
                        Map = resolvedMap,
                        X = p.X,
                        Y = p.Y,
                        Message = "warped",
                    }));
                }
                catch (Exception ex)
                {
                    tcs.TrySetResult(Response.Failure(id, "handler_error", ex.Message));
                }
            });

            return tcs.Task;
        }

        // ── npc_face_direction ─────────────────────────────────────

        public Task<Response> HandleFaceDirection(string id, JsonElement @params)
        {
            if (!Context.IsWorldReady)
                return Task.FromResult(Response.Failure(id, "mod_not_ready", "no save loaded"));

            FaceDirectionParams? p;
            try { p = JsonSerializer.Deserialize<FaceDirectionParams>(@params, JsonOpts.Web); }
            catch (Exception ex) { return Task.FromResult(Response.Failure(id, "invalid_params", ex.Message)); }

            if (p is null || string.IsNullOrWhiteSpace(p.Npc))
                return Task.FromResult(Response.Failure(id, "invalid_params", "npc is required"));

            int facing = DirectionToInt(p.Direction);
            if (facing < 0)
                return Task.FromResult(Response.Failure(id, "invalid_params",
                    $"direction must be one of up/down/left/right (got '{p.Direction}')"));

            var tcs = new TaskCompletionSource<Response>();
            _pending.Enqueue(() =>
            {
                try
                {
                    NPC? npc = Game1.getCharacterFromName(p.Npc!);
                    if (npc is null)
                    {
                        tcs.TrySetResult(Response.Failure(id, "npc_not_found", $"no NPC named '{p.Npc}'"));
                        return;
                    }

                    npc.faceDirection(facing);

                    tcs.TrySetResult(Response.Success(id, new FaceDirectionResult
                    {
                        Ok = true,
                        Npc = p.Npc,
                        Direction = p.Direction!.ToLowerInvariant(),
                        Facing = facing,
                    }));
                }
                catch (Exception ex)
                {
                    tcs.TrySetResult(Response.Failure(id, "handler_error", ex.Message));
                }
            });

            return tcs.Task;
        }

        // ── npc_get_position ───────────────────────────────────────

        public Task<Response> HandleGetPosition(string id, JsonElement @params)
        {
            if (!Context.IsWorldReady)
                return Task.FromResult(Response.Failure(id, "mod_not_ready", "no save loaded"));

            GetPositionParams? p;
            try { p = JsonSerializer.Deserialize<GetPositionParams>(@params, JsonOpts.Web); }
            catch (Exception ex) { return Task.FromResult(Response.Failure(id, "invalid_params", ex.Message)); }

            if (p is null || string.IsNullOrWhiteSpace(p.Npc))
                return Task.FromResult(Response.Failure(id, "invalid_params", "npc is required"));

            var tcs = new TaskCompletionSource<Response>();
            _pending.Enqueue(() =>
            {
                try
                {
                    NPC? npc = Game1.getCharacterFromName(p.Npc!);
                    if (npc is null)
                    {
                        tcs.TrySetResult(Response.Failure(id, "npc_not_found", $"no NPC named '{p.Npc}'"));
                        return;
                    }

                    Vector2 tile = npc.Position / 64f;
                    string mapName = npc.currentLocation?.NameOrUniqueName
                                     ?? npc.currentLocation?.Name
                                     ?? string.Empty;
                    int facing = npc.FacingDirection;

                    tcs.TrySetResult(Response.Success(id, new GetPositionResult
                    {
                        Ok = true,
                        Npc = p.Npc,
                        X = tile.X,
                        Y = tile.Y,
                        Map = mapName,
                        Facing = facing,
                        Direction = IntToDirection(facing),
                        IsMoving = npc.controller != null,
                    }));
                }
                catch (Exception ex)
                {
                    tcs.TrySetResult(Response.Failure(id, "handler_error", ex.Message));
                }
            });

            return tcs.Task;
        }

        // ── pump ───────────────────────────────────────────────────

        /// <summary>Drain all queued work on the game thread.</summary>
        public void PumpOnGameTick()
        {
            if (_pending.IsEmpty) return;
            while (_pending.TryDequeue(out Action? work))
            {
                try { work?.Invoke(); }
                catch (Exception ex) { _log.Log($"[Movement] pump work threw: {ex}", LogLevel.Warn); }
            }
        }

        // ── helpers ────────────────────────────────────────────────

        private static int DirectionToInt(string? dir)
        {
            if (string.IsNullOrEmpty(dir)) return -1;
            return dir.ToLowerInvariant() switch
            {
                "up"    => 0,
                "right" => 1,
                "down"  => 2,
                "left"  => 3,
                _       => -1,
            };
        }

        private static string IntToDirection(int facing) => facing switch
        {
            0 => "up",
            1 => "right",
            2 => "down",
            3 => "left",
            _ => "down",
        };

        // ── DTOs ───────────────────────────────────────────────────

        private sealed class MoveToParams
        {
            [JsonPropertyName("npc")] public string? Npc { get; set; }
            [JsonPropertyName("x")]   public int X { get; set; }
            [JsonPropertyName("y")]   public int Y { get; set; }
            [JsonPropertyName("map")] public string? Map { get; set; }
        }

        private sealed class MoveToResult
        {
            [JsonPropertyName("ok")]      public bool Ok { get; set; }
            [JsonPropertyName("npc")]     public string? Npc { get; set; }
            [JsonPropertyName("map")]     public string? Map { get; set; }
            [JsonPropertyName("x")]       public int X { get; set; }
            [JsonPropertyName("y")]       public int Y { get; set; }
            [JsonPropertyName("message")] public string? Message { get; set; }
        }

        private sealed class FaceDirectionParams
        {
            [JsonPropertyName("npc")]       public string? Npc { get; set; }
            [JsonPropertyName("direction")] public string? Direction { get; set; }
        }

        private sealed class FaceDirectionResult
        {
            [JsonPropertyName("ok")]        public bool Ok { get; set; }
            [JsonPropertyName("npc")]       public string? Npc { get; set; }
            [JsonPropertyName("direction")] public string? Direction { get; set; }
            [JsonPropertyName("facing")]    public int Facing { get; set; }
        }

        private sealed class GetPositionParams
        {
            [JsonPropertyName("npc")] public string? Npc { get; set; }
        }

        private sealed class GetPositionResult
        {
            [JsonPropertyName("ok")]        public bool Ok { get; set; }
            [JsonPropertyName("npc")]       public string? Npc { get; set; }
            [JsonPropertyName("x")]         public float X { get; set; }
            [JsonPropertyName("y")]         public float Y { get; set; }
            [JsonPropertyName("map")]       public string? Map { get; set; }
            [JsonPropertyName("facing")]    public int Facing { get; set; }
            [JsonPropertyName("direction")] public string? Direction { get; set; }
            [JsonPropertyName("is_moving")] public bool IsMoving { get; set; }
        }
    }
}
