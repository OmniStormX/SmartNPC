// SmartNPC SMAPI mod entry point — M3 ws bridge edition.
//
// Composition only; all the heavy lifting lives in:
//   - Bridge/WebSocketServer.cs  — ws server
//   - Bridge/MessageRouter.cs    — action dispatch
//   - Mail/MailHandler.cs        — `mail_send`
//   - Chat/ChatHandler.cs        — `chat_say`
//   - Chat/ChatInputCapture.cs   — Ctrl+T hotkey + Harmony patch for player chat
//
// See docs/protocol.md for the wire protocol.

using System;
using StardewModdingAPI;
using StardewModdingAPI.Events;

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

        public override void Entry(IModHelper helper)
        {
            this._config = helper.ReadConfig<ModConfig>();
            helper.Events.GameLoop.GameLaunched += this.OnGameLaunched;
            helper.Events.GameLoop.UpdateTicked += this.OnUpdateTicked;
        }

        private void OnGameLaunched(object? sender, GameLaunchedEventArgs e)
        {
            try
            {
                _router = new MessageRouter(this.Monitor);
                _mail   = new MailHandler(this.Monitor);
                _chat   = new ChatHandler(this.Monitor);

                _router.Register("mail_send", _mail.Handle);
                _router.Register("chat_say",  _chat.Handle);

                string prefix = this._config.ListenPrefix();
                _ws = new WebSocketServer(prefix, _router, this.Monitor);
                _ws.Start();

                _chatInput = new ChatInputCapture(this, this.ForwardPlayerMessage);
                this.Monitor.Log($"StardewMCPBridge ready (ws={prefix} + chat + mail)", LogLevel.Info);
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
