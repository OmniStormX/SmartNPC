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
        private const string CmdGoto        = "smartnpc_goto";
        private const string CmdGather      = "smartnpc_gather";
        private const string CmdWander      = "smartnpc_wander";
        private const string CmdClearDebris  = "smartnpc_clear_debris";
        private const string CmdDeposit      = "smartnpc_deposit_items";
        private const string CmdDeliver      = "smartnpc_deliver_items";
        private const string CmdTillSoil     = "smartnpc_till_soil";
        private const string CmdApproach     = "smartnpc_approach_speak";
        private const string CmdForage       = "smartnpc_forage_collect";
        private const string CmdGiveChest    = "smartnpc_give_chest";

        private static readonly Random s_rng = new();
        private static readonly HttpClient s_http = new() { Timeout = TimeSpan.FromSeconds(5) };

        public static void Register(ICommandHelper commands, IMonitor log, WebSocketServer ws, FollowSystem follow, NpcInventory inventory)
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
                    "runs the smartnpc-visit SKILL right now (still " +
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
                    "passed. After the time advance, also broadcasts a final " +
                    "game_time_tick at the new current hour so any pending " +
                    "schedule entry (planned_hour <= now && !fired) is reviewed " +
                    "and dispatched immediately, even if the tick stopped on a " +
                    "non-integer hour.\n" +
                    $"Usage:\n" +
                    $"  {CmdTick}        Advance by 1 hour.\n" +
                    $"  {CmdTick} <N>    Advance by N hours (1..12).",
                callback: (_, args) => HandleTick(args, log, ws));

            commands.Add(
                name: CmdGoto,
                documentation:
                    "Teleport the player to an NPC's current position (cross-map " +
                    "warp if needed). Inverse of smartnpc_teleport.\n" +
                    $"Usage: {CmdGoto} <NpcName>",
                callback: (_, args) => HandleGoto(args, log));

            commands.Add(
                name: CmdGather,
                documentation:
                    "Warp all Agent-managed NPCs to the player's current farm " +
                    "in a loose ring around the player. Useful for staging " +
                    "multi-NPC scenes without using Agent commands.\n" +
                    $"Usage: {CmdGather}",
                callback: (_, args) => HandleGather(args, log));

            commands.Add(
                name: CmdWander,
                documentation:
                    "Force an NPC to immediately wander to a random nearby tile.\n" +
                    "Uses the same PathFindController logic as the npc_wander MCP tool.\n" +
                    $"Usage:\n" +
                    $"  {CmdWander} <NpcName>           Wander with default radius (8 tiles).\n" +
                    $"  {CmdWander} <NpcName> <radius>  Wander within radius tiles (1..24).",
                callback: (_, args) => HandleWander(args, log, follow));

            commands.Add(
                name: CmdClearDebris,
                documentation:
                    "Force an NPC to clear nearby debris (weeds/twigs/stones) and collect drops into their backpack.\n" +
                    $"Usage:\n" +
                    $"  {CmdClearDebris} <NpcName>                    radius=5 max=3\n" +
                    $"  {CmdClearDebris} <NpcName> <radius> <max>     e.g. Abigail 8 5",
                callback: (_, args) => HandleClearDebris(args, log, inventory, follow));

            commands.Add(
                name: CmdDeposit,
                documentation:
                    "Force an NPC to walk to the nearest chest and deposit backpack items.\n" +
                    $"Usage:\n" +
                    $"  {CmdDeposit} <NpcName>                      auto-find nearest chest, deposit all\n" +
                    $"  {CmdDeposit} <NpcName> <chestX> <chestY>   deposit to specific chest, deposit all\n" +
                    $"  {CmdDeposit} <NpcName> auto (O)390 (O)388  auto-find, filter by item ids",
                callback: (_, args) => HandleDeposit(args, log, inventory, follow));

            commands.Add(
                name: CmdDeliver,
                documentation:
                    "Force an NPC to walk to the player and hand over all backpack items.\n" +
                    $"Usage:\n" +
                    $"  {CmdDeliver} <NpcName>",
                callback: (_, args) => HandleDeliver(args, log, inventory, follow));

            commands.Add(
                name: CmdTillSoil,
                documentation:
                    "Force an NPC to till empty soil within a radius.\n" +
                    $"Usage:\n" +
                    $"  {CmdTillSoil} <NpcName>                    radius=3 max=5\n" +
                    $"  {CmdTillSoil} <NpcName> <radius> <max>    e.g. Abigail 5 8",
                callback: (_, args) => HandleTillSoil(args, log, follow));

            commands.Add(
                name: CmdApproach,
                documentation:
                    "Force an NPC to walk to the player and show a heart emote (approach and speak).\n" +
                    $"Usage:\n" +
                    $"  {CmdApproach} <NpcName>              walk to player\n" +
                    $"  {CmdApproach} <NpcName> <reason>    walk to player with reason",
                callback: (_, args) => HandleApproach(args, log, follow));

            commands.Add(
                name: CmdForage,
                documentation:
                    "Force an NPC to collect nearby forageable items (mushrooms, shells, berries, etc.).\n" +
                    $"Usage:\n" +
                    $"  {CmdForage} <NpcName>                    radius=8 max=3\n" +
                    $"  {CmdForage} <NpcName> <radius> <max>    e.g. XiaMi 10 5",
                callback: (_, args) => HandleForage(args, log, inventory, follow));

            commands.Add(
                name: CmdGiveChest,
                documentation:
                    "Give the player a Chest (item ID 130) directly into their inventory.\n" +
                    $"Usage: {CmdGiveChest}",
                callback: (_, args) => HandleGiveChest(log));

            log.Log($"[DebugCommands] registered: {CmdFriendship}, {CmdDebug}, {CmdTeleport}, {CmdProactive}, {CmdStatus}, {CmdTick}, {CmdGoto}, {CmdGather}, {CmdWander}, {CmdClearDebris}, {CmdDeposit}, {CmdDeliver}, {CmdTillSoil}, {CmdApproach}, {CmdForage}, {CmdGiveChest}", LogLevel.Trace);
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
        private static void HandleTick(string[] args, IMonitor log, WebSocketServer ws)
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

            // Force an immediate schedule review on mcp side. SDV's
            // performTenMinuteClockUpdate does fire SMAPI TimeChanged for
            // each in-game integer hour we cross, and ModEntry.OnTimeChanged
            // already broadcasts game_time_tick for those — but only when
            // NewTime % 100 == 0. If `hours` advanced past several hours
            // those hourly ticks are still emitted, so for a normal
            // "advance 1h+" call this is just a redundant safety net. The
            // real value: after the loop we ALSO send one game_time_tick
            // for the *current* hour, so any entry whose planned hour is
            // now <= current (but somehow !Fired — say an earlier failed
            // dispatch, or schedule submitted mid-day) is picked up
            // immediately instead of having to wait for the next natural
            // integer-hour rollover.
            int currentHour = after / 100;
            try
            {
                _ = ws.BroadcastEvent("game_time_tick", new
                {
                    hour = currentHour,
                    time = after,
                    forced = true,
                });
            }
            catch (Exception ex)
            {
                log.Log($"[smartnpc_tick] forced schedule review broadcast failed: {ex.Message}", LogLevel.Warn);
            }

            log.Log(
                $"[smartnpc_tick] advanced {hours}h: {before} -> {after} " +
                $"(emitted SMAPI TimeChanged per hour + forced game_time_tick at hour={currentHour} for immediate schedule review)",
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

        // ── smartnpc_gather ────────────────────────────────────────────

        // Warp all Agent-managed NPCs to the player's current map in a loose
        // ring around the player.  Each NPC is placed on its own spoke so they
        // don't overlap.  We try tiles at increasing radius on each spoke until
        // we find a passable, unoccupied tile — this keeps them spread out even
        // on busy maps.
        private static void HandleGather(string[] args, IMonitor log)
        {
            if (!Context.IsWorldReady)
            {
                log.Log("no save loaded; start or load a save first.", LogLevel.Error);
                return;
            }

            var managed = AgentNpcRegistry.GetAll()
                .Where(n => Game1.getCharacterFromName(n) != null)
                .ToList();

            if (managed.Count == 0)
            {
                log.Log("no Agent-managed NPCs found in the world.", LogLevel.Warn);
                return;
            }

            var player   = Game1.player;
            var location = player.currentLocation;
            if (location is null)
            {
                log.Log("player has no currentLocation.", LogLevel.Error);
                return;
            }

            int baseTileX = (int)(player.Position.X / 64f);
            int baseTileY = (int)(player.Position.Y / 64f);

            // Spoke angles: evenly distributed around the player.
            // We start each spoke at radius 3 and step outward until we find a
            // passable tile, so NPCs land roughly 3–6 tiles away from the player
            // and don't cluster at a single point.
            int count = managed.Count;
            int placed = 0;

            for (int i = 0; i < count; i++)
            {
                string npcName = managed[i];
                NPC? npc = Game1.getCharacterFromName(npcName);
                if (npc is null) continue;

                double angle = (2.0 * Math.PI * i) / count;

                // Search outward from radius 3 until we find a clear tile.
                Microsoft.Xna.Framework.Vector2 destTile = FindPassableTile(
                    location, baseTileX, baseTileY, angle, minRadius: 3, maxRadius: 8);

                npc.Halt();
                if (npc.controller != null) npc.controller = null;

                Game1.warpCharacter(npc, location, destTile);

                // Face the player from the destination tile.
                double dx = baseTileX - destTile.X;
                double dy = baseTileY - destTile.Y;
                int facing = (Math.Abs(dx) >= Math.Abs(dy))
                    ? (dx >= 0 ? 1 : 3)   // right or left
                    : (dy >= 0 ? 2 : 0);  // down or up
                npc.faceDirection(facing);

                log.Log(
                    $"[smartnpc_gather] {npcName} → {location.Name} ({(int)destTile.X},{(int)destTile.Y}) facing={FacingName(facing)}",
                    LogLevel.Info);
                placed++;
            }

            log.Log($"[smartnpc_gather] gathered {placed}/{count} NPC(s) to {location.Name} around player ({baseTileX},{baseTileY}).", LogLevel.Info);
        }

        /// <summary>
        /// Walk outward along <paramref name="angle"/> from the origin tile,
        /// returning the first passable, unoccupied tile found between
        /// <paramref name="minRadius"/> and <paramref name="maxRadius"/>.
        /// Falls back to the minRadius tile if nothing passable is found.
        /// </summary>
        private static Microsoft.Xna.Framework.Vector2 FindPassableTile(
            GameLocation location,
            int originX, int originY,
            double angle,
            int minRadius, int maxRadius)
        {
            Microsoft.Xna.Framework.Vector2 fallback =
                new(originX + (int)Math.Round(Math.Cos(angle) * minRadius),
                    originY + (int)Math.Round(Math.Sin(angle) * minRadius));

            for (int r = minRadius; r <= maxRadius; r++)
            {
                int tx = originX + (int)Math.Round(Math.Cos(angle) * r);
                int ty = originY + (int)Math.Round(Math.Sin(angle) * r);
                var tile = new Microsoft.Xna.Framework.Vector2(tx, ty);

                // isTilePassableAndClearOfDebris checks the tile layer + object layer.
                if (location.isTilePassable(new xTile.Dimensions.Location(tx, ty),
                        Game1.viewport)
                    && !location.isObjectAtTile(tx, ty)
                    && Game1.getCharacterFromName(location.characters
                           .FirstOrDefault(c => (int)(c.Position.X / 64f) == tx
                                             && (int)(c.Position.Y / 64f) == ty)
                           ?.Name ?? "") == null)
                {
                    return tile;
                }
            }
            return fallback;
        }

        // ── smartnpc_wander ────────────────────────────────────────────

        // Immediately triggers the npc_wander action on the specified NPC.
        // Delegates directly to WanderHandler.DoWander so the debug path
        // and the MCP path exercise exactly the same logic.
        private static void HandleWander(string[] args, IMonitor log, FollowSystem follow)
        {
            if (!Context.IsWorldReady)
            {
                log.Log("no save loaded; start or load a save first.", LogLevel.Error);
                return;
            }

            if (args.Length < 1)
            {
                log.Log($"usage: {CmdWander} <NpcName> [radius]", LogLevel.Error);
                return;
            }

            string name = args[0];
            NPC? npc = Game1.getCharacterFromName(name);
            if (npc is null)
            {
                log.Log($"NPC '{name}' not found.", LogLevel.Warn);
                return;
            }

            int radius = 8;
            if (args.Length >= 2)
            {
                if (!int.TryParse(args[1], out radius) || radius < 1 || radius > 24)
                {
                    log.Log($"invalid radius: '{args[1]}' (must be int 1..24)", LogLevel.Error);
                    return;
                }
            }

            WanderHandler.DoWander(npc, name, radius, follow, log);
            log.Log($"[smartnpc_wander] triggered wander for {name} radius={radius}", LogLevel.Info);
        }

        // ── smartnpc_clear_debris ──────────────────────────────────────

        private static void HandleClearDebris(string[] args, IMonitor log, NpcInventory inventory, FollowSystem follow)
        {
            if (!Context.IsWorldReady)
            {
                log.Log("no save loaded; start or load a save first.", LogLevel.Error);
                return;
            }
            if (args.Length < 1)
            {
                log.Log($"usage: {CmdClearDebris} <NpcName> [radius] [max_count]", LogLevel.Error);
                return;
            }

            string name = args[0];
            NPC? npc = Game1.getCharacterFromName(name);
            if (npc is null)
            {
                log.Log($"NPC '{name}' not found.", LogLevel.Warn);
                return;
            }

            int radius   = 5;
            int maxCount = 3;
            if (args.Length >= 2 && (!int.TryParse(args[1], out radius) || radius < 1 || radius > 10))
            {
                log.Log($"invalid radius '{args[1]}' (1..10)", LogLevel.Error);
                return;
            }
            if (args.Length >= 3 && (!int.TryParse(args[2], out maxCount) || maxCount < 1 || maxCount > 10))
            {
                log.Log($"invalid max_count '{args[2]}' (1..10)", LogLevel.Error);
                return;
            }

            // Build a JsonElement params object and delegate to ClearDebrisHandler.Execute
            // via the same path the MCP tool uses, so both exercise identical logic.
            var paramsDict = new System.Collections.Generic.Dictionary<string, object>
            {
                ["npc"]       = name,
                ["radius"]    = radius,
                ["max_count"] = maxCount,
            };
            string json = System.Text.Json.JsonSerializer.Serialize(paramsDict);
            var paramsEl = System.Text.Json.JsonDocument.Parse(json).RootElement;

            var handler = new ClearDebrisHandler(log, () => false, inventory, follow);
            handler.ExecuteDebug(npc, name, paramsEl);
            log.Log($"[smartnpc_clear_debris] triggered for {name} radius={radius} max={maxCount}", LogLevel.Info);
        }

        // ── smartnpc_forage_collect ────────────────────────────────────────

        private static void HandleForage(string[] args, IMonitor log, NpcInventory inventory, FollowSystem follow)
        {
            if (!Context.IsWorldReady)
            {
                log.Log("no save loaded", LogLevel.Error);
                return;
            }
            if (args.Length < 1)
            {
                log.Log($"usage: {CmdForage} <NpcName> [radius] [max_count]", LogLevel.Error);
                return;
            }

            string name = args[0];
            int radius   = args.Length >= 2 && int.TryParse(args[1], out int r) ? Math.Clamp(r, 1, 15) : 8;
            int maxCount = args.Length >= 3 && int.TryParse(args[2], out int m) ? Math.Clamp(m, 1, 10) : 3;

            NPC? npc = Game1.getCharacterFromName(name);
            if (npc is null)
            {
                log.Log($"NPC '{name}' not found", LogLevel.Error);
                return;
            }

            using var doc = JsonDocument.Parse(
                $"{{\"npc\":\"{name}\",\"radius\":{radius},\"max_count\":{maxCount}}}");
            var handler = new ForageCollectHandler(log, () => false, inventory, follow);
            handler.ExecuteDebug(npc, name, doc.RootElement);
            log.Log($"[smartnpc_forage_collect] triggered for {name} radius={radius} max={maxCount}", LogLevel.Info);
        }

        // ── smartnpc_deposit_items ─────────────────────────────────────────

        // Usage:
        //   smartnpc_deposit_items <NpcName>                      → auto-find, all items
        //   smartnpc_deposit_items <NpcName> <chestX> <chestY>   → specific chest, all items
        //   smartnpc_deposit_items <NpcName> auto (O)390 (O)388  → auto-find, filtered
        private static void HandleDeposit(string[] args, IMonitor log, NpcInventory inventory, FollowSystem follow)
        {
            if (!Context.IsWorldReady)
            {
                log.Log("no save loaded; start or load a save first.", LogLevel.Error);
                return;
            }
            if (args.Length < 1)
            {
                log.Log($"usage: {CmdDeposit} <NpcName> [chestX chestY | auto] [(O)itemId ...]", LogLevel.Error);
                return;
            }

            string name = args[0];
            NPC? npc = Game1.getCharacterFromName(name);
            if (npc is null)
            {
                log.Log($"NPC '{name}' not found.", LogLevel.Warn);
                return;
            }

            bool autoFind = true;
            int  chestX   = 0;
            int  chestY   = 0;
            System.Collections.Generic.List<string>? itemIds = null;
            int argIdx = 1;

            if (args.Length > 1)
            {
                if (string.Equals(args[1], "auto", StringComparison.OrdinalIgnoreCase))
                {
                    autoFind = true;
                    argIdx   = 2;
                }
                else if (args.Length >= 3 && int.TryParse(args[1], out int px) && int.TryParse(args[2], out int py))
                {
                    autoFind = false;
                    chestX   = px;
                    chestY   = py;
                    argIdx   = 3;
                }
            }

            // Remaining args are item_ids.
            if (argIdx < args.Length)
            {
                itemIds = new System.Collections.Generic.List<string>();
                for (int i = argIdx; i < args.Length; i++)
                    itemIds.Add(args[i]);
            }

            bool started = follow.StartDepositItems(
                name, npc,
                new Microsoft.Xna.Framework.Point(chestX, chestY),
                autoFind, null,
                itemIds,
                inventory);

            if (started)
                log.Log($"[smartnpc_deposit_items] triggered for {name} autoFind={autoFind} chest=({chestX},{chestY})", LogLevel.Info);
            else
                log.Log($"[smartnpc_deposit_items] could not start (no chest found?)", LogLevel.Warn);
        }

        // ── smartnpc_deliver_items ────────────────────────────────────────

        // Usage:
        //   smartnpc_deliver_items <NpcName>   → walk to player and hand over all items
        private static void HandleDeliver(string[] args, IMonitor log, NpcInventory inventory, FollowSystem follow)
        {
            if (!Context.IsWorldReady)
            {
                log.Log("no save loaded; start or load a save first.", LogLevel.Error);
                return;
            }
            if (args.Length < 1)
            {
                log.Log($"usage: {CmdDeliver} <NpcName>", LogLevel.Error);
                return;
            }

            string name = args[0];
            NPC? npc = Game1.getCharacterFromName(name);
            if (npc is null)
            {
                log.Log($"NPC '{name}' not found.", LogLevel.Warn);
                return;
            }

            var items = inventory.GetItems(name);
            if (items.Count == 0)
            {
                log.Log($"[smartnpc_deliver_items] {name}: backpack empty, nothing to deliver", LogLevel.Warn);
                return;
            }

            follow.StartDeliverItems(name, inventory);
            log.Log($"[smartnpc_deliver_items] triggered for {name} ({items.Count} item types)", LogLevel.Info);
        }

        // ── smartnpc_till_soil ────────────────────────────────────────────

        // Usage:
        //   smartnpc_till_soil <NpcName>                 radius=3 max=5
        //   smartnpc_till_soil <NpcName> <radius> <max>  e.g. Abigail 5 8
        private static void HandleTillSoil(string[] args, IMonitor log, FollowSystem follow)
        {
            if (!Context.IsWorldReady)
            {
                log.Log("no save loaded; start or load a save first.", LogLevel.Error);
                return;
            }
            if (args.Length < 1)
            {
                log.Log($"usage: {CmdTillSoil} <NpcName> [radius] [max_count]", LogLevel.Error);
                return;
            }

            string name = args[0];
            NPC? npc = Game1.getCharacterFromName(name);
            if (npc is null)
            {
                log.Log($"NPC '{name}' not found.", LogLevel.Warn);
                return;
            }

            int radius   = args.Length > 1 && int.TryParse(args[1], out int pr) ? Math.Clamp(pr, 1, 8) : 3;
            int maxCount = args.Length > 2 && int.TryParse(args[2], out int pm) ? Math.Clamp(pm, 1, 15) : 5;

            using var doc = JsonDocument.Parse(
                $"{{\"npc\":\"{name}\",\"radius\":{radius},\"max_count\":{maxCount}}}");
            var handler = new TillSoilHandler(log, () => false, follow);
            handler.ExecuteDebug(npc, name, doc.RootElement);
            log.Log($"[smartnpc_till_soil] triggered for {name} radius={radius} max={maxCount}", LogLevel.Info);
        }

        // ── smartnpc_approach_speak ──────────────────────────────────────

        // Usage:
        //   smartnpc_approach_speak <NpcName>              walk to player
        //   smartnpc_approach_speak <NpcName> <reason>    walk to player with reason
        private static void HandleApproach(string[] args, IMonitor log, FollowSystem follow)
        {
            if (!Context.IsWorldReady)
            {
                log.Log("no save loaded; start or load a save first.", LogLevel.Error);
                return;
            }
            if (args.Length < 1)
            {
                log.Log($"usage: {CmdApproach} <NpcName> [reason]", LogLevel.Error);
                return;
            }

            string name = args[0];
            NPC? npc = Game1.getCharacterFromName(name);
            if (npc is null)
            {
                log.Log($"NPC '{name}' not found.", LogLevel.Warn);
                return;
            }

            string? reason = args.Length > 1 ? args[1] : null;

            using var doc = JsonDocument.Parse(
                $"{{\"npc\":\"{name}\"{(reason is not null ? $",\"reason\":\"{reason}\"" : "")}}}");
            var handler = new ApproachAndSpeakHandler(log, () => false, follow);
            handler.ExecuteDebug(npc, name, doc.RootElement);
            log.Log(
                $"[smartnpc_approach_speak] triggered for {name}{(reason is not null ? $" reason=\"{reason}\"" : "")}",
                LogLevel.Info);
        }

        // ── smartnpc_give_chest ────────────────────────────────────────────

        private static void HandleGiveChest(IMonitor log)
        {
            if (!Context.IsWorldReady)
            {
                log.Log("no save loaded; start or load a save first.", LogLevel.Error);
                return;
            }

            var chest = StardewValley.ItemRegistry.Create("(BC)130"); // Big Craftable 130 = Chest
            bool placed = Game1.player.addItemToInventoryBool(chest);
            if (placed)
                log.Log("[smartnpc_give_chest] Chest added to player inventory.", LogLevel.Info);
            else
                log.Log("[smartnpc_give_chest] Inventory full — chest dropped at player's feet.", LogLevel.Warn);
        }
    }
}
