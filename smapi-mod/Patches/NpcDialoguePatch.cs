// Harmony prefix on NPC.checkAction — for Agent-managed NPCs, open the custom
// ChatWindow instead of the default dialogue flow, and broadcast a ws event.

using System.Collections.Concurrent;
using HarmonyLib;
using StardewModdingAPI;
using StardewValley;

namespace SmartNPC.Bridge
{
    internal static class NpcDialoguePatch
    {
        private static IMonitor? _log;
        private static WebSocketServer? _ws;
        private static ChatMessageStore? _store;
        private static System.Action<string>? _openChatWindow;

        private static readonly ConcurrentQueue<string> _pendingInteractions = new();

        public static void Apply(Harmony harmony, IMonitor log)
        {
            _log = log;
            harmony.Patch(
                original: AccessTools.Method(typeof(NPC), nameof(NPC.checkAction)),
                prefix: new HarmonyMethod(typeof(NpcDialoguePatch), nameof(Prefix_checkAction))
            );
            log.Log("[NpcDialoguePatch] patched NPC.checkAction prefix", LogLevel.Trace);
        }

        public static void SetBridge(WebSocketServer ws)
        {
            _ws = ws;
        }

        public static void SetUI(ChatMessageStore store, System.Action<string> openChatWindow)
        {
            _store = store;
            _openChatWindow = openChatWindow;
        }

        public static void PumpInteractions()
        {
            while (_pendingInteractions.TryDequeue(out string? npcName))
            {
                if (_ws is null)
                {
                    _log?.Log($"[NpcDialoguePatch] WARNING: ws is null, cannot send npc_interact for {npcName}", LogLevel.Warn);
                    continue;
                }
                _ = _ws.BroadcastEvent("npc_interact", new { npc = npcName, source = "player" });
                _log?.Log($"[NpcDialoguePatch] broadcast npc_interact for {npcName}", LogLevel.Debug);
            }
        }

        private static bool Prefix_checkAction(NPC __instance, ref bool __result)
        {
            if (!AgentNpcRegistry.IsManaged(__instance.Name))
                return true;

            // Face the player.
            __instance.faceTowardFarmerForPeriod(2000, 4, faceAway: false, Game1.player);

            // Open chat window UI.
            _openChatWindow?.Invoke(__instance.Name);

            __result = true;
            return false;
        }
    }
}
