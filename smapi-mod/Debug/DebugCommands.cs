// SMAPI console commands for debugging the SmartNPC integration.
//
// Registered on GameLaunched; handlers run on the SMAPI console thread
// (not the game tick), so we only read/write immediate game state that
// is safe to access outside of a tick (friendshipData mutation is OK
// because the console runs during the main loop; heavy game mutations
// should still use PumpOnGameTick elsewhere).

using System;
using System.Linq;
using StardewModdingAPI;
using StardewValley;

namespace SmartNPC.Bridge
{
    internal static class DebugCommands
    {
        private const string CmdFriendship = "smartnpc_friendship";
        private const string CmdDebug      = "smartnpc_debug";
        private const string CmdTeleport   = "smartnpc_teleport";
        private const string CmdGroup      = "smartnpc_group";

        public static void Register(ICommandHelper commands, IMonitor log, WebSocketServer? ws = null)
        {
            commands.Add(
                name: CmdFriendship,
                documentation:
                    "Usage:\n" +
                    $"  {CmdFriendship} <NpcName>           Show current friendship (points, hearts, status).\n" +
                    $"  {CmdFriendship} <NpcName> <points>  Set friendship points (clamped to 0..2500).",
                callback: (_, args) => HandleFriendship(args, log));

            commands.Add(
                name: CmdDebug,
                documentation:
                    "Usage:\n" +
                    $"  {CmdDebug}  Dump status of all Agent-managed NPCs (friendship, location, online).",
                callback: (_, args) => HandleDebug(args, log));

            commands.Add(
                name: CmdTeleport,
                documentation:
                    "Teleport an NPC to the player's current position (facing the player).\n" +
                    $"Usage: {CmdTeleport} <NpcName>",
                callback: (_, args) => HandleTeleport(args, log));

            commands.Add(
                name: CmdGroup,
                documentation:
                    "Broadcast a group_chat_message event to the agent.\n" +
                    $"Usage: {CmdGroup} <Npc1,Npc2,...> <message ...>\n" +
                    $"Example: {CmdGroup} XiaMi,Abigail,Sebastian What do you want to do today?",
                callback: (_, args) => HandleGroup(args, log, ws));

            log.Log(
                $"[DebugCommands] registered: {CmdFriendship}, {CmdDebug}, {CmdTeleport}, {CmdGroup}",
                LogLevel.Trace);
        }

        // ── smartnpc_friendship ────────────────────────────────────────

        private static void HandleFriendship(string[] args, IMonitor log)
        {
            if (!Context.IsWorldReady)
            {
                log.Log("no save loaded; start or load a save first.", LogLevel.Error);
                return;
            }

            if (args.Length < 1 || args.Length > 2)
            {
                log.Log($"usage: {CmdFriendship} <NpcName> [points]", LogLevel.Error);
                return;
            }

            string npcName = args[0];

            // Read-only path.
            if (args.Length == 1)
            {
                PrintFriendship(npcName, log);
                return;
            }

            // Set path.
            if (!int.TryParse(args[1], out int points))
            {
                log.Log($"invalid points value: '{args[1]}' (must be integer)", LogLevel.Error);
                return;
            }

            // Clamp to SDV's max (10 hearts * 250).
            int clamped = Math.Clamp(points, 0, 2500);
            if (clamped != points)
                log.Log($"points clamped to {clamped} (range 0..2500)", LogLevel.Warn);

            if (!Game1.player.friendshipData.ContainsKey(npcName))
                Game1.player.friendshipData.Add(npcName, new Friendship());

            var f = Game1.player.friendshipData[npcName];
            f.Points = clamped;

            log.Log($"[{npcName}] friendship set to {clamped} points ({clamped / 250} hearts)", LogLevel.Info);
            PrintFriendship(npcName, log);
        }

        private static void PrintFriendship(string npcName, IMonitor log)
        {
            if (!Game1.player.friendshipData.TryGetValue(npcName, out var f))
            {
                log.Log($"[{npcName}] no friendship record (NPC unknown or not yet met)", LogLevel.Warn);
                return;
            }

            int points = f.Points;
            int hearts = points / 250;
            string status = f.Status.ToString().ToLower();

            log.Log(
                $"[{npcName}] points={points} hearts={hearts}/10 status={status}",
                LogLevel.Info);
        }

        // ── smartnpc_debug ─────────────────────────────────────────────

        private static void HandleDebug(string[] args, IMonitor log)
        {
            if (!Context.IsWorldReady)
            {
                log.Log("no save loaded; start or load a save first.", LogLevel.Error);
                return;
            }

            var managed = AgentNpcRegistry.GetAll();
            if (managed.Count == 0)
            {
                log.Log("no Agent-managed NPCs registered.", LogLevel.Info);
                return;
            }

            log.Log($"Agent-managed NPCs ({managed.Count}):", LogLevel.Info);

            foreach (string name in managed.OrderBy(n => n, StringComparer.OrdinalIgnoreCase))
            {
                NPC? npc = Game1.getCharacterFromName(name);
                string locLabel;
                bool online = npc != null;

                if (npc != null)
                {
                    string map = npc.currentLocation?.Name ?? "<null>";
                    int tx = (int)(npc.Position.X / 64f);
                    int ty = (int)(npc.Position.Y / 64f);
                    locLabel = $"{map} ({tx},{ty})";
                }
                else
                {
                    locLabel = "<not spawned>";
                }

                int points = 0;
                int hearts = 0;
                string status = "none";
                if (Game1.player.friendshipData.TryGetValue(name, out var f))
                {
                    points = f.Points;
                    hearts = points / 250;
                    status = f.Status.ToString().ToLower();
                }

                log.Log(
                    $"  - {name,-12} online={online,-5} loc={locLabel,-24} " +
                    $"points={points,-5} hearts={hearts}/10 status={status}",
                    LogLevel.Info);
            }
        }

        // ── smartnpc_teleport ──────────────────────────────────────────

        private static void HandleTeleport(string[] args, IMonitor log)
        {
            if (!Context.IsWorldReady)
            {
                log.Log("no save loaded; start or load a save first.", LogLevel.Error);
                return;
            }

            if (args.Length < 1)
            {
                log.Log($"usage: {CmdTeleport} <NpcName>", LogLevel.Error);
                return;
            }

            string name = args[0];
            NPC? npc = Game1.getCharacterFromName(name);
            if (npc is null)
            {
                log.Log($"NPC '{name}' not found.", LogLevel.Warn);
                return;
            }

            var player = Game1.player;
            var dest = player.currentLocation;
            if (dest is null)
            {
                log.Log("player has no currentLocation (save not fully loaded?)", LogLevel.Error);
                return;
            }

            // Cancel any active pathfinding so the NPC stays at the new spot.
            npc.Halt();
            if (npc.controller != null) npc.controller = null;

            if (npc.currentLocation != dest)
            {
                // Cross-map warp. warpCharacter expects a TILE vector.
                int tileX = (int)(player.Position.X / 64f);
                int tileY = (int)(player.Position.Y / 64f);
                Game1.warpCharacter(npc, dest, new Microsoft.Xna.Framework.Vector2(tileX, tileY));
            }
            else
            {
                // Same map — snap directly to the player's pixel position.
                npc.Position = player.Position;
            }

            // Face the player: flip the player's facing direction.
            int facing = player.FacingDirection switch
            {
                0 => 2, // player up    → npc down
                2 => 0, // player down  → npc up
                1 => 3, // player right → npc left
                3 => 1, // player left  → npc right
                _ => 2,
            };
            npc.faceDirection(facing);

            int pxTile = (int)(player.Position.X / 64f);
            int pyTile = (int)(player.Position.Y / 64f);
            log.Log(
                $"teleported {name} to player at {dest.Name} ({pxTile},{pyTile}) facing={FacingName(facing)}",
                LogLevel.Info);
        }

        private static string FacingName(int f) => f switch
        {
            0 => "up", 1 => "right", 2 => "down", 3 => "left", _ => "?",
        };

        // ── smartnpc_group ─────────────────────────────────────────────

        private static void HandleGroup(string[] args, IMonitor log, WebSocketServer? ws)
        {
            if (ws is null)
            {
                log.Log("WebSocketServer not initialized; cannot broadcast group_chat_message.", LogLevel.Error);
                return;
            }

            if (args.Length < 2)
            {
                log.Log($"usage: {CmdGroup} <Npc1,Npc2,...> <message ...>", LogLevel.Error);
                return;
            }

            string[] participants = args[0]
                .Split(',', StringSplitOptions.RemoveEmptyEntries)
                .Select(s => s.Trim())
                .Where(s => s.Length > 0)
                .ToArray();

            if (participants.Length == 0)
            {
                log.Log("no valid participant names parsed from first argument.", LogLevel.Error);
                return;
            }

            string text = string.Join(" ", args.Skip(1)).Trim();
            if (string.IsNullOrWhiteSpace(text))
            {
                log.Log("message text is empty.", LogLevel.Error);
                return;
            }

            _ = ws.BroadcastEvent("group_chat_message", new
            {
                participants,
                text,
                source = "player",
            });

            log.Log(
                $"[GroupChat] broadcast to [{string.Join(", ", participants)}]: {text}",
                LogLevel.Info);
        }
    }
}
