// Generic stub handler for actions whose Mod-side implementation isn't
// written yet. On invocation:
//   - Shows a text bubble above the target NPC's head ([action_name]).
//   - When ModConfig.DebugShowMessage is true, also pushes a debug line
//     into the player's chat panel (same path as chat_say) so you can see
//     stub fires alongside real NPC dialogue.
//   - Returns OK so the MCP-side tool completes successfully.
//
// Lets us validate the Hermes -> MCP -> ws -> Mod pipeline before writing
// real game logic. Replace per-action with a real handler in the appropriate
// domain file (BehaviorHandler / MovementHandler / etc.) once implemented;
// un-register from ModEntry when you do.

using System;
using System.Collections.Concurrent;
using System.Text.Json;
using System.Threading.Tasks;
using StardewModdingAPI;
using StardewValley;

namespace SmartNPC.Bridge
{
    internal sealed class StubActionHandler
    {
        private readonly IMonitor _log;
        private readonly ModConfig _config;
        private readonly ConcurrentQueue<Action> _pending = new();

        // Optional debug-message surfaces. Wired by ModEntry after construction.
        private ChatMessageStore? _store;
        private UnreadTracker? _unread;
        // (npcName, displayName, text, channel) — same signature as
        // ChatHandler._onMessage.
        private Action<string, string, string, string>? _onMessage;

        public StubActionHandler(IMonitor log, ModConfig config)
        {
            _log = log;
            _config = config;
        }

        /// <summary>
        /// Wire the chat-panel surfaces so debug messages can reach the
        /// player's UI when <c>DebugShowMessage</c> is on. Call after the
        /// store / unread / notifier are constructed in ModEntry.
        /// </summary>
        public void SetDebugSinks(
            ChatMessageStore? store,
            UnreadTracker? unread,
            Action<string, string, string, string>? onMessage)
        {
            _store = store;
            _unread = unread;
            _onMessage = onMessage;
        }

        /// <summary>
        /// Returns a RequestHandler closure bound to <paramref name="actionName"/>.
        /// Register one per action via <c>_router.Register(name, stub.MakeHandler(name))</c>.
        /// </summary>
        public RequestHandler MakeHandler(string actionName)
        {
            return (id, @params) =>
            {
                if (!Context.IsWorldReady)
                    return Task.FromResult(Response.Failure(id, "mod_not_ready", "no save loaded"));

                string? npcName = null;
                if (@params.ValueKind == JsonValueKind.Object &&
                    @params.TryGetProperty("npc", out JsonElement el) &&
                    el.ValueKind == JsonValueKind.String)
                {
                    npcName = el.GetString();
                }

                if (string.IsNullOrWhiteSpace(npcName))
                    return Task.FromResult(Response.Failure(id, "invalid_params", "npc is required"));

                string paramsRaw = @params.ValueKind == JsonValueKind.Undefined
                    ? "{}"
                    : @params.GetRawText();

                var tcs = new TaskCompletionSource<Response>();
                _pending.Enqueue(() =>
                {
                    try
                    {
                        NPC? npc = Game1.getCharacterFromName(npcName);
                        if (npc is null)
                        {
                            tcs.TrySetResult(Response.Failure(id, "npc_not_found", $"no NPC named '{npcName}'"));
                            return;
                        }

                        // Bubble shows the action name so we can visually confirm
                        // the tool fired in-game.
                        string bubble = $"[{actionName}]";
                        npc.showTextAboveHead(bubble);

                        _log.Log($"stub {actionName} npc={npcName} params={paramsRaw}", LogLevel.Info);

                        // Optional: surface the same info in the chat panel
                        // for easier debugging, but only when explicitly opted
                        // into via config. Goes on the same private channel as
                        // a normal chat_say from this NPC.
                        if (_config.DebugShowMessage)
                        {
                            string displayName = npc.displayName ?? npcName;
                            string debugText = $"[stub:{actionName}] {paramsRaw}";

                            _store?.Add(npcName, npcName, debugText, isPlayer: false);

                            bool isActiveConversation =
                                ChatPanel.IsOpen && ChatPanel.ActiveNpc == npcName;
                            if (!isActiveConversation)
                                _unread?.IncrementUnread(npcName);

                            _onMessage?.Invoke(npcName, displayName, debugText, "");
                        }

                        tcs.TrySetResult(Response.Success(id, new
                        {
                            ok = true,
                            npc = npcName,
                            action = actionName,
                            message = $"stub: {actionName} acknowledged on mod side",
                        }));
                    }
                    catch (Exception ex)
                    {
                        tcs.TrySetResult(Response.Failure(id, "handler_error", ex.Message));
                    }
                });
                return tcs.Task;
            };
        }

        /// <summary>Drain queued game-thread work. Call from OnUpdateTicked.</summary>
        public void PumpOnGameTick()
        {
            while (_pending.TryDequeue(out Action? action))
                action();
        }
    }
}
