// WebSocket handlers for NPC behavior control (summon / follow / lead / query).
//
// Handlers run on the ws receive loop. They cannot directly invoke
// FollowSystem APIs (which may touch Game1, e.g. npc.Halt() in StopFollow)
// so every state transition is marshalled onto the game thread via the
// pending queue pumped by PumpOnGameTick().

using System;
using System.Collections.Concurrent;
using System.Text.Json;
using System.Text.Json.Serialization;
using System.Threading.Tasks;
using StardewModdingAPI;
using StardewValley;

namespace SmartNPC.Bridge
{
    internal sealed class BehaviorHandler
    {
        private readonly IMonitor _log;
        private readonly FollowSystem _follow;
        private readonly ConcurrentQueue<Action> _pending = new();

        public BehaviorHandler(IMonitor log, FollowSystem follow)
        {
            _log = log;
            _follow = follow;
        }

        // ── npc_summon ─────────────────────────────────────────────

        public Task<Response> HandleSummon(string id, JsonElement @params)
        {
            if (!Context.IsWorldReady)
                return Task.FromResult(Response.Failure(id, "mod_not_ready", "no save loaded"));

            NpcOnlyParams? p;
            try { p = JsonSerializer.Deserialize<NpcOnlyParams>(@params, JsonOpts.Web); }
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

                    _follow.Summon(p.Npc!);
                    tcs.TrySetResult(Response.Success(id, new BehaviorResult
                    {
                        Ok = true,
                        Npc = p.Npc,
                        Mode = "summoning",
                        Message = "summon started",
                    }));
                }
                catch (Exception ex)
                {
                    tcs.TrySetResult(Response.Failure(id, "handler_error", ex.Message));
                }
            });
            return tcs.Task;
        }

        // ── npc_follow_start ───────────────────────────────────────

        public Task<Response> HandleFollowStart(string id, JsonElement @params)
        {
            if (!Context.IsWorldReady)
                return Task.FromResult(Response.Failure(id, "mod_not_ready", "no save loaded"));

            NpcOnlyParams? p;
            try { p = JsonSerializer.Deserialize<NpcOnlyParams>(@params, JsonOpts.Web); }
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

                    _follow.StartFollow(p.Npc!);
                    tcs.TrySetResult(Response.Success(id, new BehaviorResult
                    {
                        Ok = true,
                        Npc = p.Npc,
                        Mode = "following",
                        Message = "follow started",
                    }));
                }
                catch (Exception ex)
                {
                    tcs.TrySetResult(Response.Failure(id, "handler_error", ex.Message));
                }
            });
            return tcs.Task;
        }

        // ── npc_follow_stop ────────────────────────────────────────

        public Task<Response> HandleFollowStop(string id, JsonElement @params)
        {
            if (!Context.IsWorldReady)
                return Task.FromResult(Response.Failure(id, "mod_not_ready", "no save loaded"));

            NpcOnlyParams? p;
            try { p = JsonSerializer.Deserialize<NpcOnlyParams>(@params, JsonOpts.Web); }
            catch (Exception ex) { return Task.FromResult(Response.Failure(id, "invalid_params", ex.Message)); }

            if (p is null || string.IsNullOrWhiteSpace(p.Npc))
                return Task.FromResult(Response.Failure(id, "invalid_params", "npc is required"));

            var tcs = new TaskCompletionSource<Response>();
            _pending.Enqueue(() =>
            {
                try
                {
                    _follow.StopFollow(p.Npc!);
                    tcs.TrySetResult(Response.Success(id, new BehaviorResult
                    {
                        Ok = true,
                        Npc = p.Npc,
                        Mode = "idle",
                        Message = "stopped",
                    }));
                }
                catch (Exception ex)
                {
                    tcs.TrySetResult(Response.Failure(id, "handler_error", ex.Message));
                }
            });
            return tcs.Task;
        }

        // ── npc_lead_to ────────────────────────────────────────────

        public Task<Response> HandleLeadTo(string id, JsonElement @params)
        {
            if (!Context.IsWorldReady)
                return Task.FromResult(Response.Failure(id, "mod_not_ready", "no save loaded"));

            LeadToParams? p;
            try { p = JsonSerializer.Deserialize<LeadToParams>(@params, JsonOpts.Web); }
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

                    if (!string.IsNullOrWhiteSpace(p.Map)
                        && Game1.getLocationFromName(p.Map) == null)
                    {
                        tcs.TrySetResult(Response.Failure(id, "map_not_found", $"no map named '{p.Map}'"));
                        return;
                    }

                    _follow.LeadTo(p.Npc!, p.X, p.Y, p.Map);
                    tcs.TrySetResult(Response.Success(id, new BehaviorResult
                    {
                        Ok = true,
                        Npc = p.Npc,
                        Mode = "leading",
                        Message = $"leading to ({p.X},{p.Y})",
                    }));
                }
                catch (Exception ex)
                {
                    tcs.TrySetResult(Response.Failure(id, "handler_error", ex.Message));
                }
            });
            return tcs.Task;
        }

        // ── npc_emote ──────────────────────────────────────────────

        // SDV's Character.doEmote(int) takes a numeric emote id. These IDs are
        // defined in StardewValley.Character as public constants. Map the
        // string kinds from the MCP tool to those numbers. "sparkle" has no
        // native SDV equivalent — we reuse exclamationEmote (16) because the
        // "!" bubble is the classic "someone just appeared / has something to
        // say" visual, which matches the proactive-visit use case best.
        private static int MapEmoteKind(string? kind)
        {
            return (kind ?? "sparkle").ToLowerInvariant() switch
            {
                "question" or "?" => 8,       // questionEmote
                "angry" or "mad" => 12,        // angryEmote
                "exclamation" or "!" or "sparkle" => 16, // exclamationEmote (sparkle → reuse)
                "heart" or "love" => 20,       // heartEmote
                "sleep" or "zzz" => 24,        // sleepEmote
                "sad" => 28,                   // sadEmote
                "happy" or "smile" => 32,      // happyEmote
                "x" or "cross" => 36,          // xEmote
                "pause" => 40,                 // pauseEmote
                "music" or "note" => 56,       // musicNoteEmote
                _ => 16,                       // unknown → exclamation (safe default)
            };
        }

        public Task<Response> HandleEmote(string id, JsonElement @params)
        {
            if (!Context.IsWorldReady)
                return Task.FromResult(Response.Failure(id, "mod_not_ready", "no save loaded"));

            NpcEmoteParams? p;
            try { p = JsonSerializer.Deserialize<NpcEmoteParams>(@params, JsonOpts.Web); }
            catch (Exception ex) { return Task.FromResult(Response.Failure(id, "invalid_params", ex.Message)); }

            if (p is null || string.IsNullOrWhiteSpace(p.Npc))
                return Task.FromResult(Response.Failure(id, "invalid_params", "npc is required"));

            string kind = string.IsNullOrWhiteSpace(p.Kind) ? "sparkle" : p.Kind!;
            int emoteId = MapEmoteKind(kind);

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

                    npc.doEmote(emoteId);
                    tcs.TrySetResult(Response.Success(id, new BehaviorResult
                    {
                        Ok = true,
                        Npc = p.Npc,
                        Mode = kind.ToLowerInvariant(),
                        Message = $"emote {emoteId}",
                    }));
                }
                catch (Exception ex)
                {
                    tcs.TrySetResult(Response.Failure(id, "handler_error", ex.Message));
                }
            });
            return tcs.Task;
        }

        // ── npc_get_behavior ───────────────────────────────────────

        public Task<Response> HandleGetBehavior(string id, JsonElement @params)
        {
            if (!Context.IsWorldReady)
                return Task.FromResult(Response.Failure(id, "mod_not_ready", "no save loaded"));

            NpcOnlyParams? p;
            try { p = JsonSerializer.Deserialize<NpcOnlyParams>(@params, JsonOpts.Web); }
            catch (Exception ex) { return Task.FromResult(Response.Failure(id, "invalid_params", ex.Message)); }

            if (p is null || string.IsNullOrWhiteSpace(p.Npc))
                return Task.FromResult(Response.Failure(id, "invalid_params", "npc is required"));

            NpcBehaviorMode mode = _follow.GetMode(p.Npc);
            return Task.FromResult(Response.Success(id, new BehaviorResult
            {
                Ok = true,
                Npc = p.Npc,
                Mode = mode.ToString().ToLowerInvariant(),
                Message = null,
            }));
        }

        // ── pump ───────────────────────────────────────────────────

        public void PumpOnGameTick()
        {
            if (_pending.IsEmpty) return;
            while (_pending.TryDequeue(out Action? work))
            {
                try { work?.Invoke(); }
                catch (Exception ex) { _log.Log($"[Behavior] pump work threw: {ex}", LogLevel.Warn); }
            }
        }

        // ── DTOs ───────────────────────────────────────────────────

        private sealed class NpcOnlyParams
        {
            [JsonPropertyName("npc")] public string? Npc { get; set; }
        }

        private sealed class LeadToParams
        {
            [JsonPropertyName("npc")] public string? Npc { get; set; }
            [JsonPropertyName("x")]   public int X { get; set; }
            [JsonPropertyName("y")]   public int Y { get; set; }
            [JsonPropertyName("map")] public string? Map { get; set; }
        }

        private sealed class NpcEmoteParams
        {
            [JsonPropertyName("npc")]  public string? Npc  { get; set; }
            [JsonPropertyName("kind")] public string? Kind { get; set; }
        }

        private sealed class BehaviorResult
        {
            [JsonPropertyName("ok")]      public bool    Ok      { get; set; }
            [JsonPropertyName("npc")]     public string? Npc     { get; set; }
            [JsonPropertyName("mode")]    public string? Mode    { get; set; }
            [JsonPropertyName("message")]
            [JsonIgnore(Condition = JsonIgnoreCondition.WhenWritingNull)]
            public string? Message { get; set; }
        }
    }
}
