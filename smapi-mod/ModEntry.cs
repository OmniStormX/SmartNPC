// SmartNPC SMAPI mod entry point.
//
// Composition only; all the heavy lifting lives in:
//   - Bridge/WebSocketServer.cs  — ws server
//   - Bridge/MessageRouter.cs    — action dispatch
//   - Mail/MailHandler.cs        — `mail_send`
//   - Chat/ChatHandler.cs        — `chat_say`
//   - Chat/ChatInputCapture.cs   — Ctrl+T hotkey + Harmony patch for player chat
//   - UI/NpcChatBar.cs           — bottom input bar (near-NPC quick chat)
//   - UI/ChatPanel.cs            — QQ-style full chat panel
//   - UI/ChatSideButton.cs       — HUD floating button
//
// See docs/protocol.md for the wire protocol.

using System;
using HarmonyLib;
using StardewModdingAPI;
using StardewModdingAPI.Events;
using StardewValley;

namespace SmartNPC.Bridge
{
    public sealed class ModEntry : Mod
    {
        private ModConfig _config = null!;

        private WebSocketServer? _ws;
        private MessageRouter?   _router;
        private MailHandler?     _mail;
        private ChatHandler?     _chat;
        private ChatInputCapture? _chatInput;
        private XiaMiData?       _xiami;
        private GameQueryHandler? _query;
        private PerceptionSystem? _perception;
        private MovementHandler?  _movement;
        private FollowSystem?     _follow;
        private BehaviorHandler?  _behavior;
        private WanderSystem?     _wander;
        private ChatMessageStore _messageStore = new();
        private ChatSideButton?  _sideButton;
        private GroupChatManager? _groupChat;

        public override void Entry(IModHelper helper)
        {
            this._config = helper.ReadConfig<ModConfig>();

            _xiami = new XiaMiData(helper, this.Monitor);
            _xiami.Register(helper.Events);

            helper.Events.GameLoop.GameLaunched += this.OnGameLaunched;
            helper.Events.GameLoop.UpdateTicked += this.OnUpdateTicked;
            helper.Events.GameLoop.SaveLoaded += this.OnSaveLoaded;
            helper.Events.Input.ButtonsChanged += this.OnButtonsChanged;
            helper.Events.Player.Warped += this.OnPlayerWarped;
            helper.Events.Display.RenderedHud += this.OnRenderedHud;
            helper.Events.Display.WindowResized += this.OnWindowResized;
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
                _perception = new PerceptionSystem(this.Monitor);
                _movement = new MovementHandler(this.Monitor);
                _follow   = new FollowSystem(this.Monitor);
                _behavior = new BehaviorHandler(this.Monitor, _follow);
                _wander   = new WanderSystem(this.Monitor, _follow);
                _wander.OnNpcEncounter += this.HandleNpcEncounter;

                _router.Register("mail_send",            _mail.Handle);
                _router.Register("chat_say",             _chat.Handle);
                _router.Register("game_get_time",        _query.HandleGetTime);
                _router.Register("game_get_weather",     _query.HandleGetWeather);
                _router.Register("friendship_get",       _query.HandleGetFriendship);
                _router.Register("npc_get_nearby",       _perception.HandleGetNearby);
                _router.Register("npc_get_environment",  _perception.HandleGetEnvironment);
                _router.Register("npc_move_to",          _movement.HandleMoveTo);
                _router.Register("npc_face_direction",   _movement.HandleFaceDirection);
                _router.Register("npc_get_position",     _movement.HandleGetPosition);
                _router.Register("npc_summon",           _behavior.HandleSummon);
                _router.Register("npc_follow_start",     _behavior.HandleFollowStart);
                _router.Register("npc_follow_stop",      _behavior.HandleFollowStop);
                _router.Register("npc_lead_to",          _behavior.HandleLeadTo);
                _router.Register("npc_get_behavior",     _behavior.HandleGetBehavior);

                string prefix = this._config.ListenPrefix();
                _ws = new WebSocketServer(prefix, _router, this.Monitor);
                _ws.Start();

                // Group chat manager (depends on ws).
                _groupChat = new GroupChatManager(_ws, this.Monitor);

                // Wire up patches — opens NpcChatBar on NPC click.
                NpcDialoguePatch.SetBridge(_ws);
                NpcDialoguePatch.SetUI(_messageStore, this.OpenNpcChatBar);

                // Configure ChatHandler to route replies to the message store.
                _chat.SetMessageStore(_messageStore);

                // HUD side button for opening ChatPanel.
                _sideButton = new ChatSideButton(() => this.OpenChatPanel());

                _chatInput = new ChatInputCapture(this, this.ForwardPlayerMessage);

                // Register SMAPI console debug commands.
                DebugCommands.Register(this.Helper.ConsoleCommands, this.Monitor, _ws);

                this.Monitor.Log($"StardewMCPBridge ready (ws={prefix} + chat bar + panel + side button)", LogLevel.Info);
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
            _wander?.Tick();
            NpcDialoguePatch.PumpInteractions();

            // After DialogueBox dismissed, reopen NpcChatBar.
            if (_chat != null)
            {
                string? reopenNpc = _chat.ConsumePendingReopen();
                if (reopenNpc != null)
                    this.OpenNpcChatBar(reopenNpc);
            }

            // Update side button unread indicator.
            _sideButton?.SetUnread(_messageStore.HasAnyUnread());
        }

        /// <summary>Draw floating chat side button on HUD.</summary>
        private void OnRenderedHud(object? sender, RenderedHudEventArgs e)
        {
            if (!Context.IsWorldReady) return;
            if (Game1.activeClickableMenu != null) return;
            _sideButton?.Draw(e.SpriteBatch);
        }

        private void OnWindowResized(object? sender, WindowResizedEventArgs e)
        {
            _sideButton?.UpdatePosition();
        }

        /// <summary>
        /// Register vanilla NPCs as Agent-managed once the save is loaded.
        /// </summary>
        private void OnSaveLoaded(object? sender, SaveLoadedEventArgs e)
        {
            string[] managedVanilla = { "Abigail", "Sebastian", "Haley", "Harvey", "Penny" };
            foreach (string npc in managedVanilla)
                AgentNpcRegistry.Register(npc);

            this.Monitor.Log(
                $"Agent-managed NPCs registered: {string.Join(", ", managedVanilla)}",
                LogLevel.Info);
        }

        /// <summary>F2 → ChatPanel; F3 → debug panel; side button click.</summary>
        private void OnButtonsChanged(object? sender, ButtonsChangedEventArgs e)
        {
            if (!Context.IsWorldReady) return;

            foreach (SButton btn in e.Pressed)
            {
                // Side button click (check before menu guard so it works from HUD)
                if (btn == SButton.MouseLeft && Game1.activeClickableMenu == null)
                {
                    if (_sideButton?.HandleClick(btn, this.Helper.Input.GetCursorPosition()) == true)
                        return;
                }

                if (Game1.activeClickableMenu != null) continue;

                if (btn == SButton.F2)
                {
                    this.OpenChatPanel();
                    return;
                }
                if (btn == SButton.F3 && _follow != null)
                {
                    Game1.activeClickableMenu = new DebugPanel(_follow);
                    return;
                }
            }
        }

        private void OnPlayerWarped(object? sender, WarpedEventArgs e)
        {
            if (!e.IsLocalPlayer) return;
            _follow?.OnPlayerWarped(e.NewLocation);
        }

        /// <summary>
        /// Called by WanderSystem when two agent NPCs meet. Broadcasts an
        /// npc_encounter event so the Agent can trigger memory-sharing dialogue.
        /// </summary>
        private void HandleNpcEncounter(string npcA, string npcB, string mapName)
        {
            if (_ws == null) return;
            _ = _ws.BroadcastEvent("npc_encounter", new { npc_a = npcA, npc_b = npcB, map = mapName });
        }

        /// <summary>Open bottom chat bar for near-NPC quick interaction.</summary>
        private void OpenNpcChatBar(string npcName)
        {
            // If ChatPanel is already open, switch NPC there instead.
            if (Game1.activeClickableMenu is ChatPanel panel)
            {
                panel.SelectNpc(npcName);
                return;
            }

            NPC? npc = Game1.getCharacterFromName(npcName);
            string displayName = npc?.displayName ?? npcName;

            var bar = new NpcChatBar(npcName, displayName, _messageStore, this.OnChatSend);
            Game1.activeClickableMenu = bar;
        }

        /// <summary>Open QQ-style full chat panel.</summary>
        private void OpenChatPanel(string? initialNpc = null)
        {
            // If NpcChatBar is showing, transfer its NPC to the panel.
            if (Game1.activeClickableMenu is NpcChatBar)
            {
                string? current = NpcChatBar.ActiveBarNpc;
                Game1.exitActiveMenu();
                Game1.activeClickableMenu = new ChatPanel(_messageStore, this.OnChatSend, current ?? initialNpc, _groupChat);
                return;
            }

            if (Game1.activeClickableMenu != null) return;

            Game1.activeClickableMenu = new ChatPanel(_messageStore, this.OnChatSend, initialNpc, _groupChat);
        }

        /// <summary>Called when player sends a message from any chat UI.</summary>
        private void OnChatSend(string npcName, string text)
        {
            if (_ws is null) return;
            _ = _ws.BroadcastEvent("chat_message", new { npc = npcName, text, source = "player" });
            this.Monitor.Log($"[ChatUI] player → {npcName}: {text}", LogLevel.Debug);
        }

        /// <summary>Called from the Harmony postfix on ChatBox.receiveChatMessage.</summary>
        private System.Threading.Tasks.Task ForwardPlayerMessage(string text)
        {
            if (_ws is null) return System.Threading.Tasks.Task.CompletedTask;
            return _ws.BroadcastEvent("chat_received", new { text, source = "player" });
        }

        protected override void Dispose(bool disposing)
        {
            try { _ws?.Dispose(); } catch { /* shutdown best-effort */ }
            base.Dispose(disposing);
        }
    }
}
