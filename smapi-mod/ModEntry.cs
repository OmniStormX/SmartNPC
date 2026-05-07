// SmartNPC SMAPI mod entry point.
//
// Composition only; all the heavy lifting lives in:
//   - Bridge/WebSocketServer.cs  — ws server
//   - Bridge/MessageRouter.cs    — action dispatch
//   - Mail/MailHandler.cs        — `mail_send`
//   - Chat/ChatHandler.cs        — `chat_say`
//   - Chat/ChatInputCapture.cs   — Ctrl+T hotkey + Harmony patch for player chat
//   - UI/ChatWindow.cs           — in-game chat UI
//   - UI/FriendListWindow.cs     — NPC friend list UI
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
        private ChatMessageStore _messageStore = new();

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

                // Wire up patches and UI.
                NpcDialoguePatch.SetBridge(_ws);
                NpcDialoguePatch.SetUI(_messageStore, this.OpenChatWindow);

                // Configure ChatHandler to route replies to the message store.
                _chat.SetMessageStore(_messageStore);

                _chatInput = new ChatInputCapture(this, this.ForwardPlayerMessage);

                // Register SMAPI console debug commands.
                DebugCommands.Register(this.Helper.ConsoleCommands, this.Monitor);

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
            NpcDialoguePatch.PumpInteractions();

            // If ChatHandler swapped a ChatWindow out for a native DialogueBox,
            // reopen the ChatWindow once the DialogueBox is dismissed.
            if (_chat != null)
            {
                string? reopenNpc = _chat.ConsumePendingReopen();
                if (reopenNpc != null)
                    this.OpenChatWindow(reopenNpc);
            }
        }

        /// <summary>
        /// Register vanilla NPCs as Agent-managed once the save is loaded.
        /// This bypasses the once-per-day dialogue limit for these NPCs so the
        /// external Agent can drive their conversations. Vanilla NPCs already
        /// have friendshipData populated by the game, so no manual Add is needed.
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

        /// <summary>F2 → friend list; F3 → debug panel.</summary>
        private void OnButtonsChanged(object? sender, ButtonsChangedEventArgs e)
        {
            if (!Context.IsWorldReady) return;
            if (Game1.activeClickableMenu != null) return;

            foreach (SButton btn in e.Pressed)
            {
                if (btn == SButton.F2)
                {
                    Game1.activeClickableMenu = new FriendListWindow(_messageStore, this.OpenChatWindow);
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

        /// <summary>Open chat window for a given NPC.</summary>
        private void OpenChatWindow(string npcName)
        {
            NPC? npc = Game1.getCharacterFromName(npcName);
            string displayName = npc?.displayName ?? npcName;

            var window = new ChatWindow(npcName, displayName, _messageStore, this.OnChatSend);
            Game1.activeClickableMenu = window;
        }

        /// <summary>Called when player sends a message from the chat window.</summary>
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
