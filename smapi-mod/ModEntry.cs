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
using System.Linq;
using HarmonyLib;
using StardewModdingAPI;
using StardewModdingAPI.Events;
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
                    new ClearDebrisHandler(this.Monitor, showBubble, _npcInventory),
                    new WaterCropsHandler(this.Monitor, showBubble),
                    new HarvestCropsHandler(this.Monitor, showBubble),
                    new DepositItemsHandler(this.Monitor, showBubble),
                    new DeliverItemsHandler(this.Monitor, showBubble),
                    new ForageCollectHandler(this.Monitor, showBubble),
                    new PetAnimalHandler(this.Monitor, showBubble),
                    new PlantSeedsHandler(this.Monitor, showBubble),
                    new TillSoilHandler(this.Monitor, showBubble),
                    new InspectObjectHandler(this.Monitor, showBubble),
                    new PlaceObjectHandler(this.Monitor, showBubble),
                    // Social actions.
                    new ApproachAndSpeakHandler(this.Monitor, showBubble),
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

                string prefix = this._config.ListenPrefix();
                _ws = new WebSocketServer(prefix, _router, this.Monitor);
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
                DebugCommands.Register(this.Helper.ConsoleCommands, this.Monitor, _ws, _follow);

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
        }

        /// <summary>
        /// Register vanilla NPCs as Agent-managed and restore chat history /
        /// unread state from the save file once the save is loaded.
        /// </summary>
        private void OnSaveLoaded(object? sender, SaveLoadedEventArgs e)
        {
            string[] managedVanilla = { "Abigail", "Sebastian", "Haley", "Harvey", "Penny" };
            foreach (string npc in managedVanilla)
            {
                AgentNpcRegistry.Register(npc);
                _follow?.EnsureRegistered(npc);   // pre-register Idle guard
            }

            try
            {
                // Skip restoring chat history — start fresh each session.
                // var hist = this.Helper.Data.ReadSaveData<Dictionary<string, List<ChatMessage>>>(SaveKey_ChatHistory);
                // _messageStore.Restore(hist);
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

            this.Monitor.Log(
                $"Agent-managed NPCs registered: {string.Join(", ", managedVanilla)}",
                LogLevel.Info);

            // Suppress schedule on the current day immediately after save load.
            SuppressScheduleForAgentNpcs();
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

            // Cut game schedule control for all Agent-managed NPCs.
            // dayupdate() resets ignoreScheduleToday = false and calls TryLoadSchedule()
            // before DayStarted fires, so we must re-suppress here every morning.
            SuppressScheduleForAgentNpcs();
        }

        /// <summary>
        /// For every Agent-managed NPC: set ignoreScheduleToday=true and clear the
        /// loaded Schedule dictionary so the game's checkSchedule() is a no-op.
        /// Also clear any in-flight PathFindController left over from yesterday.
        /// </summary>
        private void SuppressScheduleForAgentNpcs()
        {
            foreach (string npcName in AgentNpcRegistry.GetAll())
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
        /// Emits a `game_time_tick` event every in-game hour (when the time
        /// changes to XX:00). Go-side scheduler uses this to fire due schedule
        /// entries and send schedule_trigger events to NPC Hermes profiles.
        /// </summary>
        private void OnTimeChanged(object? sender, TimeChangedEventArgs e)
        {
            if (_ws is null) return;

            // Only fire on the hour (e.g. 600, 700, ..., 2500).
            if (e.NewTime % 100 != 0) return;

            int hour = e.NewTime / 100;
            _ = _ws.BroadcastEvent("game_time_tick", new
            {
                hour,
                time = e.NewTime,
                day = Game1.dayOfMonth,
                season = Game1.currentSeason,
                year = Game1.year,
                day_of_week = Game1.shortDayNameFromDayOfSeason(Game1.dayOfMonth),
            });
            this.Monitor.Log($"[Schedule] game_time_tick emitted: hour={hour} (raw={e.NewTime})", LogLevel.Trace);
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
