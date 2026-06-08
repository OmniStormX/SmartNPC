// Harmony patches that cut the game's automatic control over Agent-managed NPCs.
//
// Patched methods:
//   NPC.playSleepingAnimation  — prevent the game from forcing sleep pose/animation
//
// Schedule suppression (ignoreScheduleToday + Schedule.Clear) is handled in
// ModEntry.OnDayStarted rather than via a patch because the NPC has no single
// "go to sleep" entry point for schedule-driven movement; the flag is the
// official SMAPI/SDV way to disable schedule for a day.

using HarmonyLib;
using StardewModdingAPI;
using StardewValley;

namespace SmartNPC.Bridge
{
    internal static class AgentControlPatch
    {
        private static IMonitor? _log;

        public static void Apply(Harmony harmony, IMonitor log)
        {
            _log = log;

            harmony.Patch(
                original: AccessTools.Method(typeof(NPC), nameof(NPC.playSleepingAnimation)),
                prefix: new HarmonyMethod(typeof(AgentControlPatch), nameof(Prefix_playSleepingAnimation))
            );

            log.Log("[AgentControlPatch] patched NPC.playSleepingAnimation prefix", LogLevel.Trace);
        }

        // Suppress sleep animation for Agent-managed NPCs.
        // Returning false skips the original method entirely.
        private static bool Prefix_playSleepingAnimation(NPC __instance)
        {
            if (!AgentNpcRegistry.IsManaged(__instance.Name))
                return true;

            _log?.Log($"[AgentControlPatch] suppressed playSleepingAnimation for {__instance.Name}", LogLevel.Trace);
            return false;
        }
    }
}
