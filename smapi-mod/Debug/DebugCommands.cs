// SMAPI console commands for debugging the SmartNPC integration.
//
// Registered on GameLaunched; handlers run on the SMAPI console thread
// (not the game tick), so we only read/write immediate game state that
// is safe to access outside of a tick (friendshipData mutation is OK
// because the console runs during the main loop; heavy game mutations
// should still use PumpOnGameTick elsewhere).

using System;
using System.Linq;
using System.Net.Http;
using System.Text.Json;
using System.Threading;
using StardewModdingAPI;
using StardewValley;

namespace SmartNPC.Bridge
{
    internal static class DebugCommands
    {
        private const string CmdFriendship = "smartnpc_friendship";
        private const string CmdDebug      = "smartnpc_debug";
        private const string CmdTeleport   = "smartnpc_teleport";
        private const string CmdProactive  = "smartnpc_proactive";
        private const string CmdStatus     = "smartnpc_status";
        private const string CmdTick       = "smartnpc_tick";
        private const string CmdGoto       = "smartnpc_goto";

        private static readonly Random s_rng = new();
        private static readonly HttpClient s_http = new() { Timeout = TimeSpan.FromSeconds(5) };

        public static void Register(ICommandHelper commands, IMonitor log, WebSocketServer ws)
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
                name: CmdProactive,
                documentation:
                    "Force an immediate proactive-visit trigger, bypassing the " +
                    "15-minute cron and the 60-minute cool-down. The targeted NPC " +
                    "runs the smartnpc-proactive-visit SKILL right now (still " +
                    "honoring player_get_status + game_get_time checks).\n" +
                    $"Usage:\n" +
                    $"  {CmdProactive}            Pick a random Agent-managed NPC.\n" +
                    $"  {CmdProactive} <NpcName>  Target a specific NPC (PascalCase).",
                callback: (_, args) => HandleProactive(args, log, ws));

            commands.Add(
                name: CmdStatus,
                documentation:
                    "Show the runtime status of the SmartNPC stack: ws connection " +
                    "to mcp, mcp uptime, and per-profile Hermes Gateway health.\n" +
                    $"Usage:\n" +
                    $"  {CmdStatus}                            Use default mcp HTTP base http://127.0.0.1:3000.\n" +
                    $"  {CmdStatus} http://<host>:<port>       Override mcp HTTP base URL.",
                callback: (_, args) => HandleStatus(args, log, ws));

            commands.Add(
                name: CmdTick,
                documentation:
                    "Advance in-game time by N hours (default 1). Triggers the " +
                    "full SDV time pipeline (NPC schedules, shop open/close, " +
                    "lighting), and emits one game_time_tick event per hour to " +
                    "the MCP bridge so NPC schedules fire as if real time " +
                    "passed.\n" +
                    $"Usage:\n" +
                    $"  {CmdTick}        Advance by 1 hour.\n" +
                    $"  {CmdTick} <N>    Advance by N hours (1..12).",
                callback: (_, args) => HandleTick(args, log));

            commands.Add(
                name: CmdGoto,
                documentation:
                    "Teleport the player to an NPC's current position (cross-map " +
                    "warp if needed). Inverse of smartnpc_teleport.\n" +
                    $"Usage: {CmdGoto} <NpcName>",
                callback: (_, args) => HandleGoto(args, log));

            log.Log($"[DebugCommands] registered: {CmdFriendship}, {CmdDebug}, {CmdTeleport}, {CmdProactive}, {CmdStatus}, {CmdTick}, {CmdGoto}", LogLevel.Trace);
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

        // ── smartnpc_proactive ─────────────────────────────────────────

        private static void HandleProactive(string[] args, IMonitor log, WebSocketServer ws)
        {
            var all = AgentNpcRegistry.GetAll().ToList();
            if (all.Count == 0)
            {
                log.Log("no Agent-managed NPCs registered; nothing to trigger.", LogLevel.Warn);
                return;
            }

            string pick;
            if (args.Length >= 1 && !string.IsNullOrWhiteSpace(args[0]))
            {
                pick = args[0];
                // Case-insensitive match, return the canonical PascalCase form.
                string? match = all.FirstOrDefault(n => string.Equals(n, pick, StringComparison.OrdinalIgnoreCase));
                if (match is null)
                {
                    log.Log(
                        $"NPC '{pick}' is not Agent-managed. Known: {string.Join(", ", all)}",
                        LogLevel.Warn);
                    return;
                }
                pick = match;
            }
            else
            {
                pick = all[s_rng.Next(all.Count)];
            }

            // Fire a ws event; hermesrelay picks it up via the `npc` field and
            // POSTs to that profile's Hermes Gateway. Fire-and-forget — the
            // real signal to the operator is the NPC showing up in-game.
            _ = ws.BroadcastEvent("debug_proactive_trigger", new { npc = pick });
            log.Log(
                $"[smartnpc_proactive] forced proactive-visit for {pick} — " +
                $"watch the mcp + hermes logs for the resulting npc_summon / " +
                $"npc_emote / chat_say calls.",
                LogLevel.Info);
        }

        // ── smartnpc_status ────────────────────────────────────────────

        // Local DTO matching mcp's /status response shape. Kept in this file
        // (not a shared header) so the contract is visible right next to the
        // command that consumes it.
        private sealed class StatusSnapshot
        {
            public string? version { get; set; }
            public long uptime_seconds { get; set; }
            public string? started_at { get; set; }
            public string? generated_at { get; set; }
            public string? mod_ws_url { get; set; }
            public bool mod_ws_connected { get; set; }
            public StatusProfile[]? profiles { get; set; }
        }

        private sealed class StatusProfile
        {
            public string? npc_filter { get; set; }
            public string? gateway_url { get; set; }
            public string? conversation { get; set; }
            public string? model { get; set; }
            public bool healthy { get; set; }
            public long latency_ms { get; set; }
            public string? error { get; set; }
        }

        private static void HandleStatus(string[] args, IMonitor log, WebSocketServer ws)
        {
            string baseUrl = (args.Length >= 1 && !string.IsNullOrWhiteSpace(args[0]))
                ? args[0].TrimEnd('/')
                : "http://127.0.0.1:3000";
            string url = baseUrl + "/status";

            // Brief mod-side preface so the operator immediately sees ws
            // server liveness even if mcp's HTTP is down.
            int wsClients = ws.ConnectedClientCount;
            string wsLabel = wsClients > 0 ? $"up, {wsClients} client(s)" : "up, no clients";
            log.Log($"[smartnpc_status] mod ws server: {wsLabel}", LogLevel.Info);
            log.Log($"[smartnpc_status] querying mcp at {url} ...", LogLevel.Info);

            StatusSnapshot? snap;
            try
            {
                using var cts = new CancellationTokenSource(TimeSpan.FromSeconds(8));
                using var resp = s_http.GetAsync(url, cts.Token).GetAwaiter().GetResult();
                if (!resp.IsSuccessStatusCode)
                {
                    log.Log($"  mcp /status returned HTTP {(int)resp.StatusCode}; mcp may be unreachable or version-skewed.", LogLevel.Warn);
                    return;
                }
                string body = resp.Content.ReadAsStringAsync(cts.Token).GetAwaiter().GetResult();
                snap = JsonSerializer.Deserialize<StatusSnapshot>(body, new JsonSerializerOptions
                {
                    PropertyNameCaseInsensitive = true,
                });
            }
            catch (Exception ex)
            {
                log.Log($"  mcp /status request failed: {ex.GetType().Name}: {ex.Message}", LogLevel.Error);
                log.Log($"  is mcp running at {baseUrl}? try: bin\\smartnpc-mcp.exe --http :3000 --hermes-config ...", LogLevel.Info);
                return;
            }

            if (snap is null)
            {
                log.Log("  mcp /status response was empty / unparseable", LogLevel.Warn);
                return;
            }

            log.Log($"  mcp version={snap.version} uptime={FormatDuration(snap.uptime_seconds)} (started {snap.started_at})", LogLevel.Info);
            log.Log($"  mcp ↔ mod ws ({snap.mod_ws_url}): {(snap.mod_ws_connected ? "CONNECTED" : "DISCONNECTED")}",
                snap.mod_ws_connected ? LogLevel.Info : LogLevel.Warn);

            if (snap.profiles is null || snap.profiles.Length == 0)
            {
                log.Log("  no Hermes profiles configured (mcp running without --hermes-config / --hermes-url)", LogLevel.Info);
                return;
            }

            int healthy = snap.profiles.Count(p => p.healthy);
            log.Log($"  hermes profiles: {healthy}/{snap.profiles.Length} healthy", LogLevel.Info);
            foreach (var p in snap.profiles)
            {
                string label = string.IsNullOrEmpty(p.npc_filter)
                    ? p.conversation ?? "<noname>"
                    : p.npc_filter!;
                if (p.healthy)
                {
                    log.Log($"    ✓ {label,-12} {p.gateway_url}  ({p.latency_ms}ms)", LogLevel.Info);
                }
                else
                {
                    log.Log($"    ✗ {label,-12} {p.gateway_url}  ({p.latency_ms}ms)  err: {p.error}", LogLevel.Warn);
                }
            }
        }

        private static string FormatDuration(long seconds)
        {
            if (seconds < 60) return $"{seconds}s";
            long minutes = seconds / 60;
            long secs = seconds % 60;
            if (minutes < 60) return $"{minutes}m{secs}s";
            long hours = minutes / 60;
            minutes %= 60;
            return $"{hours}h{minutes}m";
        }

        // ── smartnpc_tick ──────────────────────────────────────────────

        // SDV's internal clock advances in 10-minute increments; one in-game
        // hour = 6 ticks. Calling Game1.performTenMinuteClockUpdate() drives
        // the same path as natural time progression — NPC schedule routes,
        // shop closing logic, ambient lighting, and the SMAPI TimeChanged
        // event that ModEntry.OnTimeChanged hooks to broadcast game_time_tick
        // to mcp. So this command is end-to-end: tick → SDV state advances →
        // SMAPI fires TimeChanged → ws emits game_time_tick → mcp scheduler
        // fires schedule_trigger to the NPC's Hermes profile.
        private static void HandleTick(string[] args, IMonitor log)
        {
            if (!Context.IsWorldReady)
            {
                log.Log("no save loaded; start or load a save first.", LogLevel.Error);
                return;
            }

            int hours = 1;
            if (args.Length >= 1)
            {
                if (!int.TryParse(args[0], out hours) || hours < 1 || hours > 12)
                {
                    log.Log($"invalid hours: '{args[0]}' (must be int 1..12)", LogLevel.Error);
                    return;
                }
            }

            int before = Game1.timeOfDay;
            int totalTenMinTicks = hours * 6;
            for (int i = 0; i < totalTenMinTicks; i++)
            {
                Game1.performTenMinuteClockUpdate();
            }
            int after = Game1.timeOfDay;

            log.Log(
                $"[smartnpc_tick] advanced {hours}h: {before} -> {after} " +
                $"(SMAPI TimeChanged + game_time_tick will fire on next game tick)",
                LogLevel.Info);
        }

        // ── smartnpc_goto ──────────────────────────────────────────────

        // Inverse of smartnpc_teleport: take the player TO the NPC. Useful
        // when debugging proactive-visit / schedule-trigger behavior — you
        // can spawn the NPC's plan, then jump straight to wherever they are
        // without walking the map. Cross-map jumps go through Game1.warpFarmer
        // so the destination map is loaded correctly.
        private static void HandleGoto(string[] args, IMonitor log)
        {
            if (!Context.IsWorldReady)
            {
                log.Log("no save loaded; start or load a save first.", LogLevel.Error);
                return;
            }

            if (args.Length < 1)
            {
                log.Log($"usage: {CmdGoto} <NpcName>", LogLevel.Error);
                return;
            }

            string name = args[0];
            NPC? npc = Game1.getCharacterFromName(name);
            if (npc is null)
            {
                log.Log($"NPC '{name}' not found.", LogLevel.Warn);
                return;
            }

            var dest = npc.currentLocation;
            if (dest is null)
            {
                log.Log($"NPC '{name}' has no currentLocation (off-stage?)", LogLevel.Warn);
                return;
            }

            int tileX = (int)(npc.Position.X / 64f);
            int tileY = (int)(npc.Position.Y / 64f);

            var player = Game1.player;
            if (player.currentLocation != dest)
            {
                Game1.warpFarmer(dest.NameOrUniqueName, tileX, tileY, false);
            }
            else
            {
                player.Position = npc.Position;
            }

            // Face the NPC: invert the NPC's facing direction.
            int facing = npc.FacingDirection switch
            {
                0 => 2, 2 => 0, 1 => 3, 3 => 1, _ => 2,
            };
            player.faceDirection(facing);

            log.Log(
                $"warped player to {name} at {dest.Name} ({tileX},{tileY}) facing={FacingName(facing)}",
                LogLevel.Info);
        }
    }
}
