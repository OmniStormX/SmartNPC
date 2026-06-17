// SmartNPC SMAPI mod entry point.
//
// Composition only; all the heavy lifting lives in:
//   - Bridge/WebSocketServer.cs  — ws server
//   - Bridge/MessageRouter.cs    — action dispatch
//   - Mail/MailHandler.cs        — `mail_send`
//   - Chat/ChatHandler.cs        — `chat_say`
//   - Chat/ChatInputCapture.cs   — Ctrl+T hotkey + Harmony patch for player chat
//   - UI/ChatPanel.cs            — QQ-style unified chat panel
//   - UI/ContactList.cs          — left pane (NPC list)
//   - UI/ConversationView.cs     — right pane (transcript + input)
//   - UI/NotificationToast.cs    — floating toast for incoming messages
//   - Data/UnreadTracker.cs      — per-NPC unread counter
//
// See docs/protocol.md for the wire protocol.

using System;
using System.Collections.Generic;
using System.IO;
using System.Linq;
using System.Text.Json;
using HarmonyLib;
using StardewModdingAPI;
using StardewModdingAPI.Events;
using Microsoft.Xna.Framework;
using StardewValley;

namespace SmartNPC.Bridge
{
    public sealed class ModEntry : Mod
    {
        // Per-save persistence keys.
        private const string SaveKey_ChatHistory = "smartnpc.chat_history";
        private const string SaveKey_Unread      = "smartnpc.unread";
        private const string SaveKey_Inventories = "smartnpc.inventories";

        private ModConfig _config = null!;
        private Dictionary<string, bool> _npcConfig = new(); // cached from agent_npcs.json

        private WebSocketServer? _ws;
        private MessageRouter?   _router;
        private MailHandler?     _mail;
        private ChatHandler?     _chat;
        private ChatInputCapture? _chatInput;
        private XiaMiData?       _xiami;
        private GameQueryHandler? _query;
        private PlayerQueryHandler? _playerQuery;
        private PerceptionSystem? _perception;
        private MovementHandler?  _movement;
        private FollowSystem?     _follow;
        private BehaviorHandler?  _behavior;
        private NpcActionHandlerBase[]? _actionHandlers;

        private readonly ChatMessageStore _messageStore = new();
        private readonly UnreadTracker    _unread       = new();
        private readonly NpcInventory     _npcInventory = new();
        private NotificationToast?        _toast;
        private NpcInventoryHud?          _inventoryHud;
        private GroupChatManager?         _groupMgr;

        public override void Entry(IModHelper helper)
        {
            this._config = helper.ReadConfig<ModConfig>();

            _xiami = new XiaMiData(helper, this.Monitor);
            _xiami.Register(helper.Events);

            _toast = new NotificationToast(this.OpenChatPanelForNpc);

            helper.Events.GameLoop.GameLaunched   += this.OnGameLaunched;
            helper.Events.GameLoop.UpdateTicked   += this.OnUpdateTicked;
            helper.Events.GameLoop.SaveLoaded     += this.OnSaveLoaded;
            helper.Events.GameLoop.Saving         += this.OnSaving;
            helper.Events.GameLoop.DayStarted     += this.OnDayStarted;
            helper.Events.GameLoop.TimeChanged    += this.OnTimeChanged;
            helper.Events.Display.RenderedHud     += this.OnRenderedHud;
            helper.Events.Display.RenderedWorld   += this.OnRenderedWorld;
            helper.Events.Input.ButtonsChanged    += this.OnButtonsChanged;
            helper.Events.Input.ButtonPressed     += this.OnButtonPressed;
            helper.Events.Player.Warped           += this.OnPlayerWarped;
        }

        private void OnGameLaunched(object? sender, GameLaunchedEventArgs e)
        {
            try
            {
                // Harmony patches (centralized).
                var harmony = new Harmony(this.ModManifest.UniqueID);
                NpcDialoguePatch.Apply(harmony, this.Monitor);
                AgentControlPatch.Apply(harmony, this.Monitor);

                _router = new MessageRouter(this.Monitor);
                _mail   = new MailHandler(this.Monitor);
                _chat   = new ChatHandler(this.Monitor);
                _query  = new GameQueryHandler(this.Monitor);
                _playerQuery = new PlayerQueryHandler(this.Monitor);
                _perception = new PerceptionSystem(this.Monitor);
                _movement = new MovementHandler(this.Monitor);
                _follow   = new FollowSystem(this.Monitor);
                _behavior = new BehaviorHandler(this.Monitor, _follow);

                _router.Register("mail_send",            _mail.Handle);
                _router.Register("chat_say",             _chat.Handle);
                _router.Register("game_get_time",        _query.HandleGetTime);
                _router.Register("game_get_weather",     _query.HandleGetWeather);
                _router.Register("friendship_get",       _query.HandleGetFriendship);
                _router.Register("player_get_status",    _playerQuery.HandleGetStatus);
                _router.Register("npc_get_nearby",       _perception.HandleGetNearby);
                _router.Register("npc_get_environment",  _perception.HandleGetEnvironment);
                _router.Register("npc_move_to",          _movement.HandleMoveTo);
                _router.Register("npc_face_direction",   _movement.HandleFaceDirection);
                _router.Register("npc_get_position",     _movement.HandleGetPosition);
                _router.Register("npc_summon",           _behavior.HandleSummon);
                _router.Register("npc_emote",            _behavior.HandleEmote);
                _router.Register("npc_give_item",        _behavior.HandleGiveItem);
                _router.Register("npc_follow_start",     _behavior.HandleFollowStart);
                _router.Register("npc_follow_stop",      _behavior.HandleFollowStop);
                _router.Register("npc_lead_to",          _behavior.HandleLeadTo);
                _router.Register("npc_get_behavior",     _behavior.HandleGetBehavior);

                // NPC inventory ws actions — read/write the in-memory NpcInventory.
                _router.Register("npc_inventory_get",  HandleInventoryGet);
                _router.Register("npc_inventory_put",  HandleInventoryPut);
                _router.Register("npc_inventory_take", HandleInventoryTake);

                // ── Behavior actions (per-action handler, bubble + TODO real logic)
                // Each action has its own handler class (subclass of
                // NpcActionHandlerBase). Current default: show head bubble.
                // Implement real game logic by overriding Execute in each.
                Func<bool> showBubble = () => this._config.DebugShowBubble;
                var actionHandlers = new NpcActionHandlerBase[]
                {
                    // World actions.
                    new WanderHandler(this.Monitor, showBubble, _follow),
                    new ClearDebrisHandler(this.Monitor, showBubble, _npcInventory, _follow),
                    new WaterCropsHandler(this.Monitor, showBubble, _follow),
                    new HarvestCropsHandler(this.Monitor, showBubble, _npcInventory, _follow),
                    new DepositItemsHandler(this.Monitor, showBubble, _npcInventory, _follow),
                    new DeliverItemsHandler(this.Monitor, showBubble, _npcInventory, _follow),
                    new ForageCollectHandler(this.Monitor, showBubble, _npcInventory, _follow),
                    new PetAnimalHandler(this.Monitor, showBubble, _follow),
                    new PlantSeedsHandler(this.Monitor, showBubble, _npcInventory, _follow),
                    new TillSoilHandler(this.Monitor, showBubble, _follow),
                    new InspectObjectHandler(this.Monitor, showBubble),
                    new PlaceObjectHandler(this.Monitor, showBubble),
                    new BreakResourceHandler(this.Monitor, showBubble, _npcInventory, _follow),
                    new FertilizeHandler(this.Monitor, showBubble, _npcInventory, _follow),
                    new FillGapsHandler(this.Monitor, showBubble, _follow),
                    new WithdrawFromChestHandler(this.Monitor, showBubble, _npcInventory, _follow),
                    new TransferItemHandler(this.Monitor, showBubble, _npcInventory),
                    // Social actions.
                    new ApproachAndSpeakHandler(this.Monitor, showBubble, _follow),
                    new ExpressEmotionHandler(this.Monitor, showBubble),
                    new ShyRetreatHandler(this.Monitor, showBubble),
                    new ShowTextBubbleHandler(this.Monitor, showBubble),
                    new IdleActivityHandler(this.Monitor, showBubble),
                    new DanceHappyHandler(this.Monitor, showBubble),
                    new ReactSurpriseHandler(this.Monitor, showBubble),
                    new PaceAnxiouslyHandler(this.Monitor, showBubble),
                };
                _actionHandlers = actionHandlers;
                foreach (var h in actionHandlers)
                    _router.Register(h.ActionNamePublic, h.Handle);

                // Wire up the in-world bbox debug overlay. Reads the
                // toggle from config each call so editing config.json
                // and reloading takes effect without restarting.
                BBoxOverlay.Instance.Init(
                    enabled: () => this._config.DebugShowBBoxOverlay,
                    follow: _follow);

                // Per-NPC serial action queue: long-running tools (harvest/water/
                // till/...) append here when the NPC is already mid-task, and
                // drain to FollowSystem when GetMode() returns Idle. Configured
                // once with the FollowSystem reference; the actual dispatcher
                // runs each tick from OnUpdateTicked below.
                NpcActionQueue.Configure(_follow);

                // Per-NPC serial action queue: long-running tools (harvest/water/
                // till/...) append here when the NPC is already mid-task, and
                // drain to FollowSystem when GetMode() returns Idle. Configured
                // once with the FollowSystem reference; the actual dispatcher
                // runs each tick from OnUpdateTicked below.
                NpcActionQueue.Configure(_follow);

                string prefix = this._config.ListenPrefix();
                _ws = new WebSocketServer(prefix, _router, this.Monitor);
                _follow.SetBroadcast((name, data) => _ws.BroadcastEvent(name, data));
                // Optional: mirror every inbound mcp request into the
                // custom chat panel for live debugging. Each request is
                // appended as a bubble in its target NPC's conversation
                // (or the synthetic "__system__" channel for tools that
                // don't address an NPC, like mail_send / game_get_*).
                // Polled per request so toggling config.json + SMAPI
                // hot-reload takes effect without restarting ws.
                _ws.EnableRequestDebug(
                    enabled: () => this._config.DebugLogIncomingRequests,
                    sink: this.HandleDebugRequest);
                _ws.Start();

                // Wire up patches and UI.
                NpcDialoguePatch.SetBridge(_ws);
                NpcDialoguePatch.SetUI(_messageStore, this.OpenChatPanelForNpc);

                // Configure ChatHandler to route replies to the message store /
                // unread tracker / toast.
                _chat.SetMessageStore(_messageStore);
                _chat.SetUnreadTracker(_unread);
                _chat.SetMessageNotifier(this.OnIncomingChatMessage);

                _chatInput = new ChatInputCapture(this, this.ForwardPlayerMessage);

                // Group chat manager.
                _groupMgr = new GroupChatManager(_messageStore, _ws);
                _chat.SetGroupManager(_groupMgr);

                // Register SMAPI console debug commands.
                DebugCommands.Register(this.Helper.ConsoleCommands, this.Monitor, _ws, _follow, _npcInventory);

                _inventoryHud = new NpcInventoryHud(_npcInventory, OpenInventoryPanel);

                this.Monitor.Log($"StardewMCPBridge ready (ws={prefix} + chat + mail + UI)", LogLevel.Info);
            }
            catch (Exception ex)
            {
                this.Monitor.Log($"startup failed: {ex}", LogLevel.Error);
            }
        }

        private void OnUpdateTicked(object? sender, UpdateTickedEventArgs e)
        {
            _ws?.PumpOnGameTick();
            _mail?.PumpOnGameTick();
            _chat?.PumpOnGameTick();
            _perception?.PumpOnGameTick();
            _movement?.PumpOnGameTick();
            _behavior?.PumpOnGameTick();
            _follow?.PumpOnGameTick();
            if (_actionHandlers != null)
                foreach (var h in _actionHandlers)
                    h.PumpOnGameTick();
            NpcDialoguePatch.PumpInteractions();

            // Tick toast lifetimes (~16 ms per tick at 60fps).
            _toast?.Update(1f / 60f);

            // Expire observe entries and clear yellow boxes for idle NPCs.
            BBoxOverlay.Instance.Tick();

            // Drain any per-NPC serial action queue entries whose owners
            // have just gone Idle. One pop per NPC per tick — backs of the
            // queue stay sequential and each task gets a clean tick of
            // FollowSystem state to settle into its new mode.
            NpcActionQueue.DrainReadyTasks(this.Monitor);

            // Drain any per-NPC serial action queue entries whose owners
            // have just gone Idle. One pop per NPC per tick — backs of the
            // queue stay sequential and each task gets a clean tick of
            // FollowSystem state to settle into its new mode.
            NpcActionQueue.DrainReadyTasks(this.Monitor);
        }

        /// <summary>
        /// Register vanilla NPCs as Agent-managed and restore chat history /
        /// unread state from the save file once the save is loaded.
        /// </summary>
        private void OnSaveLoaded(object? sender, SaveLoadedEventArgs e)
        {
            // Read NPC config from agent_npcs.json (generated by
            // scripts/render_profiles.sh from hermes/npcs.yaml — the single
            // source of truth for NPC enable/disable across all layers).
            // Format: { "GameName": true|false } for ALL NPCs.
            _npcConfig = ReadAgentNpcConfig();

            // Register enabled NPCs for Agent AI control.
            foreach (var kv in _npcConfig)
            {
                if (kv.Value)
                {
                    AgentNpcRegistry.Register(kv.Key);
                    _follow?.EnsureRegistered(kv.Key);
                }
            }

            try
            {
                var un = this.Helper.Data.ReadSaveData<Dictionary<string, int>>(SaveKey_Unread);
                _unread.Restore(un);
                var inv = this.Helper.Data.ReadSaveData<Dictionary<string, List<ItemSlot>>>(SaveKey_Inventories);
                _npcInventory.Restore(inv);
                this.Monitor.Log($"NPC inventories restored ({AgentNpcRegistry.GetAll().Count} NPCs)", LogLevel.Debug);
                this.Monitor.Log(
                    $"Chat history cleared on load; unread restored ({_unread.TotalUnread})",
                    LogLevel.Debug);
            }
            catch (Exception ex)
            {
                this.Monitor.Log($"failed to restore chat data: {ex.Message}", LogLevel.Warn);
            }

            var managed = AgentNpcRegistry.GetAll();
            var enabled = _npcConfig.Where(kv => kv.Value).Select(kv => kv.Key).ToList();
            var disabled = _npcConfig.Where(kv => !kv.Value).Select(kv => kv.Key).ToList();
            this.Monitor.Log(
                $"[Config] Agent-managed: {string.Join(", ", enabled)} | schedule-only: {string.Join(", ", disabled)}",
                LogLevel.Info);

            // Suppress schedules for ALL known NPCs (enabled + disabled).
            // Enabled  → Agent AI controls movement, schedule suppressed.
            // Disabled → schedule suppressed, NPC stays in place (no AI, no vanilla schedule).
            SuppressScheduleForAllNpcs(_npcConfig.Keys);
        }

        /// <summary>
        /// Read the NPC enable/disable config from assets/agent_npcs.json.
        /// Returns a dictionary of { GameName → enabled } for ALL known NPCs.
        /// Falls back to empty dict if the file is missing or malformed.
        /// </summary>
        private Dictionary<string, bool> ReadAgentNpcConfig()
        {
            try
            {
                string path = Path.Combine(this.Helper.DirectoryPath, "assets", "agent_npcs.json");
                if (!File.Exists(path))
                {
                    this.Monitor.Log($"[Config] agent_npcs.json not found at {path} — no NPCs managed", LogLevel.Warn);
                    return new Dictionary<string, bool>();
                }
                string json = File.ReadAllText(path);
                var dict = JsonSerializer.Deserialize<Dictionary<string, bool>>(json);
                if (dict is null || dict.Count == 0)
                {
                    this.Monitor.Log("[Config] agent_npcs.json is empty — no NPCs managed", LogLevel.Warn);
                    return new Dictionary<string, bool>();
                }
                int on = dict.Values.Count(v => v);
                int off = dict.Count - on;
                this.Monitor.Log($"[Config] loaded {dict.Count} NPCs from agent_npcs.json ({on} enabled, {off} disabled)", LogLevel.Info);
                return dict;
            }
            catch (Exception ex)
            {
                this.Monitor.Log($"[Config] failed to read agent_npcs.json: {ex.Message}", LogLevel.Error);
                return new Dictionary<string, bool>();
            }
        }

        /// <summary>Persist chat history + unread counters into the save file.</summary>
        private void OnSaving(object? sender, SavingEventArgs e)
        {
            try
            {
                this.Helper.Data.WriteSaveData(SaveKey_ChatHistory, _messageStore.Snapshot());
                this.Helper.Data.WriteSaveData(SaveKey_Unread,      _unread.Snapshot());
                this.Helper.Data.WriteSaveData(SaveKey_Inventories, _npcInventory.Snapshot());
            }
            catch (Exception ex)
            {
                this.Monitor.Log($"failed to persist chat data: {ex.Message}", LogLevel.Warn);
            }
        }

        private void OnRenderedHud(object? sender, RenderedHudEventArgs e)
        {
            // Toasts are HUD-level; draw above world but below menus.
            if (Game1.activeClickableMenu is ChatPanel) return;
            _toast?.Draw(e.SpriteBatch);
            _inventoryHud?.Draw(e.SpriteBatch);
        }

        private void OnRenderedWorld(object? sender, RenderedWorldEventArgs e)
        {
            // Bbox debug overlay: gated by ModConfig.DebugShowBBoxOverlay.
            // Drawn AFTER the world so the rectangles sit on top of terrain
            // but under HUD/menus.
            BBoxOverlay.Instance.Draw(e.SpriteBatch);
        }

        /// <summary>Hotkey handler: F2 (panel + focus list), Tab (toggle), F3 (debug).</summary>
        private void OnButtonsChanged(object? sender, ButtonsChangedEventArgs e)
        {
            if (!Context.IsWorldReady) return;

            foreach (SButton btn in e.Pressed)
            {
                // Tab: toggle panel; only respond when no other modal menu is up.
                if (btn == SButton.Tab)
                {
                    if (Game1.activeClickableMenu is ChatPanel)
                    {
                        Game1.exitActiveMenu();
                        return;
                    }
                    if (Game1.activeClickableMenu == null)
                    {
                        OpenChatPanel(null);
                        return;
                    }
                }

                if (Game1.activeClickableMenu != null) continue;

                if (btn == SButton.F2)
                {
                    OpenChatPanel(null);
                    return;
                }
                if (btn == SButton.F3 && _follow != null)
                {
                    Game1.activeClickableMenu = new DebugPanel(_follow);
                    return;
                }
            }
        }

        /// <summary>Click-through for floating toast notifications.</summary>
        private void OnButtonPressed(object? sender, ButtonPressedEventArgs e)
        {
            if (e.Button != SButton.MouseLeft) return;
            if (Game1.activeClickableMenu != null) return;

            int mx = (int)e.Cursor.ScreenPixels.X;
            int my = (int)e.Cursor.ScreenPixels.Y;

            // Backpack icon takes priority over toast (icon is smaller / more precise).
            // Suppress the input so SDV's NPC.checkAction doesn't fire and open the chat panel.
            if (_inventoryHud != null && _inventoryHud.TryClick(mx, my))
            {
                this.Helper.Input.Suppress(e.Button);
                return;
            }
            _toast?.TryClick(mx, my);
        }

        private void OnPlayerWarped(object? sender, WarpedEventArgs e)
        {
            if (!e.IsLocalPlayer) return;
            _follow?.OnPlayerWarped(e.NewLocation);
        }

        // ── Schedule-driving events ─────────────────────────────────────────

        /// <summary>
        /// Emits a `day_started` event at the beginning of each new game day.
        /// Go-side scheduler clears all NPC schedules; Hermes profiles receive
        /// this event and call npc_plan_day to generate a new daily plan.
        /// </summary>
        private void OnDayStarted(object? sender, DayStartedEventArgs e)
        {
            if (_ws is null) return;

            var season = Game1.currentSeason;
            var day    = Game1.dayOfMonth;
            var year   = Game1.year;
            var dow    = Game1.shortDayNameFromDayOfSeason(day);

            _ = _ws.BroadcastEvent("day_started", new
            {
                day,
                season,
                year,
                day_of_week = dow,
            });
            this.Monitor.Log($"[Schedule] day_started emitted: Y{year} {season} {day} ({dow})", LogLevel.Debug);

            // Cut game schedule control for all managed NPCs (enabled + disabled).
            // dayupdate() resets ignoreScheduleToday = false and calls TryLoadSchedule()
            // before DayStarted fires, so we must re-suppress here every morning.
            SuppressScheduleForAllNpcs(_npcConfig.Keys);

            // Teleport all Agent-managed NPCs to the farmhouse porch each morning.
            TeleportAgentNpcsToFarmDoor();
        }

        /// <summary>
        /// For every known NPC (enabled + disabled): set ignoreScheduleToday=true and
        /// clear the loaded Schedule dictionary so the game's checkSchedule() is a no-op.
        /// Also clear any in-flight PathFindController left over from yesterday.
        ///
        /// Enabled NPCs  → Agent AI controls movement instead of schedule.
        /// Disabled NPCs → NPC stays in place (no AI, no vanilla schedule).
        /// </summary>
        private void SuppressScheduleForAllNpcs(IEnumerable<string> npcNames)
        {
            foreach (string npcName in npcNames)
            {
                NPC? npc = Game1.getCharacterFromName(npcName);
                if (npc is null) continue;

                npc.ignoreScheduleToday = true;
                npc.Schedule?.Clear();

                // Cancel any controller the game set during dayupdate warp-home logic.
                if (npc.controller != null)
                {
                    try { npc.Halt(); } catch { /* non-fatal */ }
                    npc.controller = null;
                }

                this.Monitor.Log($"[AgentControl] schedule suppressed for {npcName}", LogLevel.Debug);
            }
        }

        /// <summary>
        /// Teleport all Agent-managed NPCs to the farmhouse porch tile each morning.
        /// Uses the dynamic farmhouse door position so it works across all farm types.
        /// </summary>
        private void TeleportAgentNpcsToFarmDoor()
        {
            var farm = Game1.getFarm();
            if (farm is null) return;

            var farmhouse = Utility.getHomeOfFarmer(Game1.player);
            if (farmhouse is null) return;

            var doorTile = farmhouse.getPorchStandingSpot();
            var doorPos  = new Vector2(doorTile.X, doorTile.Y);

            foreach (string npcName in AgentNpcRegistry.GetAll())
            {
                NPC? npc = Game1.getCharacterFromName(npcName);
                if (npc is null) continue;

                // Ensure NPC is on the Farm map, then position at the door.
                if (npc.currentLocation != farm)
                    Game1.warpCharacter(npc, farm, doorPos);
                else
                    npc.Position = doorPos * 64f;

                npc.faceDirection(0); // face up — toward the house
                this.Monitor.Log($"[AgentControl] {npcName} teleported to farmhouse door ({doorTile.X},{doorTile.Y})", LogLevel.Debug);
            }
        }

        // ── NPC inventory ws handlers ───────────────────────────────────────

        private System.Threading.Tasks.Task<Response> HandleInventoryGet(string id, System.Text.Json.JsonElement p)
        {
            string? npc = p.ValueKind == System.Text.Json.JsonValueKind.Object &&
                          p.TryGetProperty("npc", out var el) ? el.GetString() : null;
            if (string.IsNullOrWhiteSpace(npc))
                return System.Threading.Tasks.Task.FromResult(Response.Failure(id, "invalid_params", "npc is required"));

            var items = _npcInventory.GetItems(npc!)
                .Select(s => new { item_id = s.ItemId, count = s.Count, quality = s.Quality })
                .ToArray();

            return System.Threading.Tasks.Task.FromResult(Response.Success(id, new
            {
                ok    = true,
                npc,
                items,
            }));
        }

        private System.Threading.Tasks.Task<Response> HandleInventoryPut(string id, System.Text.Json.JsonElement p)
        {
            if (p.ValueKind != System.Text.Json.JsonValueKind.Object)
                return System.Threading.Tasks.Task.FromResult(Response.Failure(id, "invalid_params", "object params required"));

            string? npc    = p.TryGetProperty("npc",     out var e1) ? e1.GetString()    : null;
            string? itemId = p.TryGetProperty("item_id", out var e2) ? e2.GetString()    : null;
            int count      = p.TryGetProperty("count",   out var e3) && e3.TryGetInt32(out int c) ? c : 1;
            int quality    = p.TryGetProperty("quality", out var e4) && e4.TryGetInt32(out int q) ? q : 0;

            if (string.IsNullOrWhiteSpace(npc) || string.IsNullOrWhiteSpace(itemId))
                return System.Threading.Tasks.Task.FromResult(Response.Failure(id, "invalid_params", "npc and item_id are required"));

            int newTotal = _npcInventory.Add(npc!, itemId!, count, quality);
            return System.Threading.Tasks.Task.FromResult(Response.Success(id, new
            {
                ok        = true,
                npc,
                new_total = newTotal,
                message   = $"added {count}× {itemId} to {npc}",
            }));
        }

        private System.Threading.Tasks.Task<Response> HandleInventoryTake(string id, System.Text.Json.JsonElement p)
        {
            if (p.ValueKind != System.Text.Json.JsonValueKind.Object)
                return System.Threading.Tasks.Task.FromResult(Response.Failure(id, "invalid_params", "object params required"));

            string? npc    = p.TryGetProperty("npc",     out var e1) ? e1.GetString() : null;
            string? itemId = p.TryGetProperty("item_id", out var e2) ? e2.GetString() : null;
            int count      = p.TryGetProperty("count",   out var e3) && e3.TryGetInt32(out int c) ? c : 1;

            if (string.IsNullOrWhiteSpace(npc) || string.IsNullOrWhiteSpace(itemId))
                return System.Threading.Tasks.Task.FromResult(Response.Failure(id, "invalid_params", "npc and item_id are required"));

            int taken = _npcInventory.Take(npc!, itemId!, count);
            return System.Threading.Tasks.Task.FromResult(Response.Success(id, new
            {
                ok      = true,
                npc,
                taken,
                message = taken > 0 ? $"removed {taken}× {itemId} from {npc}" : "item not found in inventory",
            }));
        }

        /// <summary>
        /// Emits a `game_time_tick` event every 20 in-game minutes (when
        /// e.NewTime aligns to 0/20/40 of an hour: 600, 620, 640, 700, ...).
        /// Go-side scheduler uses this to fire due schedule entries and
        /// send schedule_trigger events to NPC Hermes profiles.
        ///
        /// Why 20-minute cadence: schedule entries can land on any 10-minute
        /// boundary (e.g. 06:30) but TimeChanged fires every 10 in-game
        /// minutes. Ticking every 20 halves relay/Hermes load while still
        /// catching every entry at most 20 minutes late — scheduler.Tick
        /// uses "&lt;= current time" semantics so a 06:30 entry fires at
        /// the 06:40 tick if the 06:30 tick was skipped.
        /// </summary>
        private void OnTimeChanged(object? sender, TimeChangedEventArgs e)
        {
            if (_ws is null) return;

            // SDV time format: HHMM. Lower 2 digits = minutes (0/10/20/30/40/50).
            // Fire when minutes-of-hour are 0/20/40, i.e. mod 20 == 0.
            int minuteOfHour = e.NewTime % 100;
            if (minuteOfHour % 20 != 0) return;

            int hour    = e.NewTime / 100;
            int minutes = hour * 60 + minuteOfHour; // total minutes from midnight
            _ = _ws.BroadcastEvent("game_time_tick", new
            {
                hour,
                minute  = minuteOfHour,
                minutes,
                time = e.NewTime,
                day = Game1.dayOfMonth,
                season = Game1.currentSeason,
                year = Game1.year,
                day_of_week = Game1.shortDayNameFromDayOfSeason(Game1.dayOfMonth),
            });
            this.Monitor.Log($"[Schedule] game_time_tick emitted: time={e.NewTime} minutes={minutes}", LogLevel.Trace);
        }

        // ── UI helpers ──────────────────────────────────────────────────────

        /// <summary>Open the chat panel; optionally select a specific NPC.</summary>
        private void OpenChatPanel(string? npcName)
        {
            ChatPanel.Open(_messageStore, _unread, this.OnChatSend, npcName, _groupMgr);
            _toast?.Clear();
        }

        /// <summary>Open the chat panel for a specific NPC (used by Harmony
        /// patch and toast click handler).</summary>
        private void OpenChatPanelForNpc(string npcName)
        {
            OpenChatPanel(npcName);
        }

        private void OpenInventoryPanel(string npcName)
        {
            Game1.activeClickableMenu = new NpcInventoryPanel(npcName, _npcInventory);
        }

        /// <summary>Called when player sends a message from the chat window.</summary>
        private void OnChatSend(string npcName, string text)
        {
            if (_ws is null) return;
            _ = _ws.BroadcastEvent("chat_message",
                new { npc = npcName, target = npcName, text, source = "player" });
            this.Monitor.Log($"[ChatUI] player → {npcName}: {text}", LogLevel.Debug);
        }

        /// <summary>Wired into <see cref="ChatHandler"/>: fires on the game thread
        /// for every NPC reply that arrives. Drives toasts + panel refresh.
        /// <para>
        /// channel == "group": the reply belongs to the group chat surface —
        /// store it there, do NOT push toast / refresh per-NPC panel.
        /// channel == "" / "private": normal 1-on-1 reply. Do NOT leak it into
        /// the group panel even if a group is active.
        /// </para>
        /// </summary>
        private void OnIncomingChatMessage(string npcName, string displayName, string text, string channel)
        {
            bool isGroup = string.Equals(channel, "group", System.StringComparison.OrdinalIgnoreCase);

            if (isGroup)
            {
                _groupMgr?.OnNpcReply(npcName, text);
                // If the panel is open on the group conversation, refresh it
                // so the new bubble appears; otherwise stay silent (no toast
                // for group chatter).
                if (Game1.activeClickableMenu is ChatPanel gp)
                    gp.RefreshContacts();
                return;
            }

            if (Game1.activeClickableMenu is ChatPanel panel)
            {
                panel.RefreshContacts();
                return;
            }
            _toast?.Push(npcName, displayName, text);
        }

        /// <summary>
        /// Sink for inbound mcp request mirroring (DebugLogIncomingRequests).
        /// Runs on the SDV game thread (drained from <see cref="WebSocketServer.PumpOnGameTick"/>).
        ///
        /// Routing: only requests addressed to a specific NPC are surfaced
        /// — they land in that NPC's conversation as a `[action] params`
        /// bubble. NPC-agnostic tools (game_get_time / game_get_weather /
        /// mail_send / friendship_get / player_get_status / …) are
        /// deliberately dropped: those are query helpers, not debug
        /// signals worth UI real estate.
        ///
        /// Stores the bubble, bumps unread when the panel isn't focused
        /// stores the bubble, bumps unread when the panel isn't focused
        /// on this conversation, refreshes the panel if it's already
        /// open. No toast (these are dev signals, not player-facing
        /// chatter).
        /// </summary>
        private void HandleDebugRequest(WebSocketServer.DebugRequestEvent evt)
        {
            if (string.IsNullOrEmpty(evt.NpcName))
            {
                // Reaching here means smartnpc-mcp dispatched a request
                // without stamping `from_npc`, AND the params didn't carry
                // an `npc` field either. By design the bridge guarantees
                // every tool call originates from a registered Hermes
                // profile, so this is either:
                //   - a profile that forgot to call agent_register_self
                //     before its first tool invocation
                //   - an operator-side fan-out site that should be using
                //     WSClient.CallAs instead of Call
                //   - the legacy --echo-mode chat_say (intentionally
                //     unattributed; safe to drop)
                // Warn so the misconfigured profile / call site is visible
                // in the SMAPI log; don't try to invent a fallback channel.
                this.Monitor.Log(
                    $"[debug-req] dropped (no from_npc and no params.npc) action={evt.Action} id={evt.Id}",
                    LogLevel.Warn);
                return;
            }

            string channel = evt.NpcName;
            NPC? npc = Game1.getCharacterFromName(channel);
            string speaker = npc?.displayName ?? channel;

            string text = string.IsNullOrEmpty(evt.ParamsJson)
                ? $"[{evt.Action}]"
                : $"[{evt.Action}] {evt.ParamsJson}";

            _messageStore.Add(channel, speaker, text, isPlayer: false);

            bool isActiveConversation =
                ChatPanel.IsOpen && string.Equals(ChatPanel.ActiveNpc, channel, System.StringComparison.Ordinal);
            if (!isActiveConversation)
                _unread.IncrementUnread(channel);

            if (Game1.activeClickableMenu is ChatPanel panel)
                panel.RefreshContacts();

            this.Monitor.Log(
                $"[debug-req] sink → channel={channel} speaker={speaker} active={isActiveConversation} text={text}",
                LogLevel.Info);
        }

        /// <summary>Called from the Harmony postfix on ChatBox.receiveChatMessage.</summary>
        private System.Threading.Tasks.Task ForwardPlayerMessage(string text)
        {
            if (_ws is null) return System.Threading.Tasks.Task.CompletedTask;

            // Intercept /group command: "/group Abigail Sebastian" creates a group chat.
            if (text.StartsWith("/group ", System.StringComparison.OrdinalIgnoreCase))
            {
                var names = text.Substring(7).Trim().Split(new[] { ' ', ',' }, System.StringSplitOptions.RemoveEmptyEntries);
                if (names.Length >= 1 && _groupMgr != null)
                {
                    Monitor.Log($"[Group] Creating group with: {string.Join(", ", names)}", StardewModdingAPI.LogLevel.Info);
                    _groupMgr.CreateGroup(names);
                    // Open the chat panel to the group view.
                    ChatPanel.Open(_messageStore, _unread, this.OnChatSend, GroupChatManager.GroupKey, _groupMgr);
                    return System.Threading.Tasks.Task.CompletedTask;
                }
            }

            // Intercept /endgroup command to exit group chat mode.
            if (text.Equals("/endgroup", System.StringComparison.OrdinalIgnoreCase))
            {
                Monitor.Log("[Group] Ending group chat", StardewModdingAPI.LogLevel.Info);
                _groupMgr?.EndGroup();
                return System.Threading.Tasks.Task.CompletedTask;
            }

            // The player typed into the in-game chat box without explicitly
            // addressing an NPC. Emit a single chat_received event with the
            // list of Agent-managed NPCs in earshot; smartnpc-mcp owns the
            // policy of whether to synthesize a chat_message for the nearest
            // one. Keeping all routing logic on the Go side per CLAUDE.md
            // ("C# 只放 SMAPI 胶水，业务逻辑在 Go").
            var audible = AudibleNPCResolver.ResolveAroundPlayer()
                .Select(e => new
                {
                    name = e.Name,
                    map = e.Map,
                    distance = e.Distance,
                    x = e.TileX,
                    y = e.TileY,
                })
                .ToArray();

            if (audible.Length > 0)
            {
                Monitor.Log($"[AudibleRouting] {audible.Length} NPC(s) in earshot; nearest={audible[0].name} d={audible[0].distance:F1}t",
                    StardewModdingAPI.LogLevel.Debug);
            }

            return _ws.BroadcastEvent("chat_received", new
            {
                text,
                source = "player",
                audible_npcs = audible,
            });
        }

        protected override void Dispose(bool disposing)
        {
            try { _ws?.Dispose(); } catch { /* shutdown best-effort */ }
            base.Dispose(disposing);
        }
    }
}
