// Base class for NPC behavior action handlers.
//
// Provides the common scaffolding: parse params.npc, find the NPC on the
// game thread, show a head bubble, call the virtual Execute method, and
// return a structured response. Subclasses only override Execute to add
// real game logic when ready.
//
// Usage:
//   class WanderHandler : NpcActionHandlerBase {
//       protected override string ActionName => "npc_wander";
//       protected override void Execute(NPC npc, JsonElement @params) { /* TODO */ }
//   }
//   _router.Register("npc_wander", new WanderHandler(log).Handle);

using System;
using System.Collections.Concurrent;
using System.Text.Json;
using System.Threading.Tasks;
using StardewModdingAPI;
using StardewValley;

namespace SmartNPC.Bridge
{
    internal abstract class NpcActionHandlerBase
    {
        protected readonly IMonitor Log;
        private readonly Func<bool> _showBubble;
        private readonly ConcurrentQueue<Action> _pending = new();

        // 配置 showbubble 配置
        protected NpcActionHandlerBase(IMonitor log, Func<bool>? showBubble = null)
        {
            Log = log;
            _showBubble = showBubble ?? (() => true);
        }

        /// <summary>The ws action name (e.g. "npc_wander").</summary>
        protected abstract string ActionName { get; }

        /// <summary>Public accessor for registration.</summary>
        public string ActionNamePublic => ActionName;

        /// <summary>
        /// Override to implement real game logic. Called on the game thread
        /// AFTER the bubble has been shown. The NPC is guaranteed non-null
        /// and the save is loaded.
        /// </summary>
        protected virtual void Execute(NPC npc, string npcName, JsonElement @params)
        {
            // Default: no-op (bubble-only). Override in subclass.
        }

        /// <summary>
        /// Resolve the bubble text shown above the NPC's head.
        /// Default: "[short_name] reason/text" or just "[short_name]".
        /// Override for actions like npc_show_text_bubble that use params.text directly.
        /// </summary>
        protected virtual string ResolveBubble(JsonElement @params)
        {
            string shortName = ActionName.StartsWith("npc_", StringComparison.Ordinal)
                ? ActionName.Substring(4)
                : ActionName;

            string? extra = null;
            if (@params.ValueKind == JsonValueKind.Object)
            {
                if (@params.TryGetProperty("reason", out JsonElement reasonEl) &&
                    reasonEl.ValueKind == JsonValueKind.String)
                    extra = reasonEl.GetString();
                else if (@params.TryGetProperty("text", out JsonElement txtEl) &&
                         txtEl.ValueKind == JsonValueKind.String)
                    extra = txtEl.GetString();
            }

            if (!string.IsNullOrEmpty(extra))
            {
                if (extra!.Length > 40)
                    extra = extra.Substring(0, 37) + "...";
                return $"[{shortName}] {extra}";
            }
            return $"[{shortName}]";
        }

        /// <summary>
        /// The RequestHandler delegate to register with the router.
        /// Usage: _router.Register("npc_wander", handler.Handle);
        /// </summary>
        public Task<Response> Handle(string id, JsonElement @params)
        {
            if (!Context.IsWorldReady)
                return Task.FromResult(Response.Failure(id, "mod_not_ready", "no save loaded"));

            string? npcName = null;
            if (@params.ValueKind == JsonValueKind.Object &&
                @params.TryGetProperty("npc", out JsonElement npcEl) &&
                npcEl.ValueKind == JsonValueKind.String)
            {
                npcName = npcEl.GetString();
            }

            if (string.IsNullOrWhiteSpace(npcName))
                return Task.FromResult(Response.Failure(id, "invalid_params", "npc is required"));

            string bubble = ResolveBubble(@params);
            var tcs = new TaskCompletionSource<Response>();
            string captured = npcName;
            JsonElement capturedParams = @params.Clone();

            _pending.Enqueue(() =>
            {
                try
                {
                    NPC? npc = Game1.getCharacterFromName(captured);
                    if (npc is null)
                    {
                        Log.Log($"[{ActionName}] NPC '{captured}' not found", LogLevel.Warn);
                        tcs.TrySetResult(Response.Failure(id, "npc_not_found", $"no NPC named '{captured}'"));
                        return;
                    }

                    if (_showBubble())
                    {
                        npc.showTextAboveHead(bubble);

                        string npcMap = npc.currentLocation?.Name ?? "<null>";
                        string playerMap = Game1.player?.currentLocation?.Name ?? "<null>";
                        bool sameMap = string.Equals(npcMap, playerMap, StringComparison.Ordinal);
                        Log.Log($"[{ActionName}] npc={captured} bubble=\"{bubble}\" sameMap={sameMap}", LogLevel.Debug);

                        if (!sameMap)
                            Log.Log($"[{ActionName}] {captured} on '{npcMap}' but player on '{playerMap}' — not visible", LogLevel.Warn);
                    }

                    Execute(npc, captured, capturedParams);

                    tcs.TrySetResult(Response.Success(id, new
                    {
                        ok = true,
                        npc = captured,
                        action = ActionName,
                        message = $"{ActionName} acknowledged",
                    }));
                }
                catch (Exception ex)
                {
                    Log.Log($"[{ActionName}] error: {ex.Message}", LogLevel.Error);
                    tcs.TrySetResult(Response.Failure(id, "handler_error", ex.Message));
                }
            });
            return tcs.Task;
        }

        /// <summary>Drain queued game-thread work. Call from OnUpdateTicked.</summary>
        public void PumpOnGameTick()
        {
            while (_pending.TryDequeue(out Action? action))
                action();
        }
    }
}
