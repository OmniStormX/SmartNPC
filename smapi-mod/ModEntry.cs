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
        private StubActionHandler? _stub;

        private readonly ChatMessageStore _messageStore = new();
        private readonly UnreadTracker    _unread       = new();
        private NotificationToast?        _toast;
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

                _router = new MessageRouter(this.Monitor);
                _mail   = new MailHandler(this.Monitor);
                _chat   = new ChatHandler(this.Monitor);
                _query  = new GameQueryHandler(this.Monitor);
                _playerQuery = new PlayerQueryHandler(this.Monitor);
                _perception = new PerceptionSystem(this.Monitor);
                _movement = new MovementHandler(this.Monitor);
                _follow   = new FollowSystem(this.Monitor);
                _behavior = new BehaviorHandler(this.Monitor, _follow);
                _stub     = new StubActionHandler(this.Monitor, this._config);

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

                // ── Stub actions (real-mod-side implementation TBD) ─────────
                // World-action stubs.
                string[] stubActions =
                {
                    "npc_wander",
                    "npc_clear_debris",
                    "npc_water_crops",
                    "npc_harvest_crops",
                    "npc_deposit_items",
                    "npc_deliver_items",
                    "npc_forage_collect",
                    "npc_pet_animal",
                    "npc_plant_seeds",
                    "npc_till_soil",
                    "npc_inspect_object",
                    "npc_place_object",
                    // Social-action stubs.
                    "npc_approach_and_speak",
                    "npc_express_emotion",
                    "npc_shy_retreat",
                    "npc_show_text_bubble",
                    "npc_idle_activity",
                    "npc_dance_happy",
                    "npc_react_surprise",
                    "npc_pace_anxiously",
                };
                foreach (string action in stubActions)
                    _router.Register(action, _stub.MakeHandler(action));

                string prefix = this._config.ListenPrefix();
                _ws = new WebSocketServer(prefix, _router, this.Monitor);
                _ws.Start();

                // Wire up patches and UI.
                NpcDialoguePatch.SetBridge(_ws);
                NpcDialoguePatch.SetUI(_messageStore, this.OpenChatPanelForNpc);

                // Configure ChatHandler to route replies to the message store /
                // unread tracker / toast.
                _chat.SetMessageStore(_messageStore);
                _chat.SetUnreadTracker(_unread);
                _chat.SetMessageNotifier(this.OnIncomingChatMessage);

                // Same surfaces for the stub handler so DebugShowMessage can
                // mirror stub fires into the chat panel — but quietly: no
                // toast popup, just append to the contact's conversation
                // thread (and refresh the panel if it's already open).
                _stub.SetDebugSinks(_messageStore, _unread, (npcName, displayName, text, channel) =>
                {
                    if (Game1.activeClickableMenu is ChatPanel panel)
                        panel.RefreshContacts();
                });

                _chatInput = new ChatInputCapture(this, this.ForwardPlayerMessage);

                // Group chat manager.
                _groupMgr = new GroupChatManager(_messageStore, _ws);
                _chat.SetGroupManager(_groupMgr);

                // Register SMAPI console debug commands.
                DebugCommands.Register(this.Helper.ConsoleCommands, this.Monitor, _ws);

                this.Monitor.Log($"StardewMCPBridge ready (ws={prefix} + chat + mail + UI)", LogLevel.Info);
            }
            catch (Exception ex)
            {
                this.Monitor.Log($"startup failed: {ex}", LogLevel.Error);
            }
        }

        private void OnUpdateTicked(object? sender, UpdateTickedEventArgs e)
        {
            _mail?.PumpOnGameTick();
            _chat?.PumpOnGameTick();
            _perception?.PumpOnGameTick();
            _movement?.PumpOnGameTick();
            _behavior?.PumpOnGameTick();
            _follow?.PumpOnGameTick();
            _stub?.PumpOnGameTick();
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
                AgentNpcRegistry.Register(npc);

            try
            {
                // Skip restoring chat history — start fresh each session.
                // var hist = this.Helper.Data.ReadSaveData<Dictionary<string, List<ChatMessage>>>(SaveKey_ChatHistory);
                // _messageStore.Restore(hist);
                var un = this.Helper.Data.ReadSaveData<Dictionary<string, int>>(SaveKey_Unread);
                _unread.Restore(un);
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
        }

        /// <summary>Persist chat history + unread counters into the save file.</summary>
        private void OnSaving(object? sender, SavingEventArgs e)
        {
            try
            {
                this.Helper.Data.WriteSaveData(SaveKey_ChatHistory, _messageStore.Snapshot());
                this.Helper.Data.WriteSaveData(SaveKey_Unread,      _unread.Snapshot());
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
            if (_toast == null) return;

            int mx = (int)e.Cursor.ScreenPixels.X;
            int my = (int)e.Cursor.ScreenPixels.Y;
            _toast.TryClick(mx, my);
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
