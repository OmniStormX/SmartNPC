// Handles the `chat_say` ws action. Routes NPC replies to:
// - Mode A (NpcChatBar): close bar → native DialogueBox → reopen bar
// - Mode B (ChatPanel): in-panel rendering (no DialogueBox)
// - Fallback: native DialogueBox when neither UI is open

using System;
using System.Collections.Concurrent;
using System.Text.Json;
using System.Text.Json.Serialization;
using System.Threading.Tasks;
using Microsoft.Xna.Framework;
using StardewModdingAPI;
using StardewValley;
using StardewValley.Menus;

namespace SmartNPC.Bridge
{
    internal sealed class ChatHandler
    {
        private readonly IMonitor _log;
        private readonly ConcurrentQueue<ChatSayParams> _pending = new();
        private ChatMessageStore? _store;

        private string? _pendingReopenNpc;
        private bool    _awaitingDialogueClose;

        public ChatHandler(IMonitor log) { _log = log; }

        public void SetMessageStore(ChatMessageStore store)
        {
            _store = store;
        }

        public Task<Response> Handle(string id, JsonElement @params)
        {
            ChatSayParams? p;
            try { p = JsonSerializer.Deserialize<ChatSayParams>(@params, JsonOpts.Web); }
            catch (Exception ex) { return Task.FromResult(Response.Failure(id, "invalid_params", ex.Message)); }

            if (p is null || string.IsNullOrWhiteSpace(p.Speaker) || string.IsNullOrWhiteSpace(p.Text))
                return Task.FromResult(Response.Failure(id, "invalid_params", "speaker and text are required"));

            if (!Context.IsWorldReady)
                return Task.FromResult(Response.Failure(id, "mod_not_ready", "no save loaded"));

            _pending.Enqueue(p);
            return Task.FromResult(Response.Success(id, new { ok = true }));
        }

        public void PumpOnGameTick()
        {
            if (_pending.IsEmpty || !Context.IsWorldReady) return;

            while (_pending.TryDequeue(out ChatSayParams? p))
            {
                if (p is null) continue;

                // Always store for history.
                _store?.Add(p.Speaker!, p.Speaker!, p.Text!, isPlayer: false);

                NPC? npc = Game1.getCharacterFromName(p.Speaker!);

                // ─── Mode A: NpcChatBar active for this speaker ───
                if (NpcChatBar.ActiveBarNpc == p.Speaker)
                {
                    if (npc != null)
                    {
                        _pendingReopenNpc = p.Speaker;
                        _awaitingDialogueClose = true;

                        if (Game1.activeClickableMenu is NpcChatBar)
                            Game1.exitActiveMenu();

                        var dialogue = new Dialogue(npc, "SmartNPC:response", p.Text!);
                        Game1.DrawDialogue(dialogue);
                        _log.Log($"dialogue (bar reopen): <{p.Speaker}> {p.Text}", LogLevel.Trace);
                    }
                    continue;
                }

                // ─── Mode B: ChatPanel open ───
                if (ChatPanel.IsOpen)
                {
                    // Message already stored above — ChatPanel reads from store
                    // each frame, so it appears live. No DialogueBox.
                    _log.Log($"panel msg: <{p.Speaker}> {p.Text}", LogLevel.Trace);
                    continue;
                }

                // ─── Fallback: neither UI open ───
                if (Game1.activeClickableMenu is DialogueBox) continue;

                if (npc != null)
                {
                    var dialogue = new Dialogue(npc, "SmartNPC:response", p.Text!);
                    Game1.DrawDialogue(dialogue);
                    _log.Log($"dialogue: <{p.Speaker}> {p.Text}", LogLevel.Trace);
                }
                else
                {
                    Game1.chatBox?.addInfoMessage($"<{p.Speaker}> {p.Text}");
                    _log.Log($"chat: <{p.Speaker}> {p.Text}", LogLevel.Trace);
                }
            }
        }

        /// <summary>
        /// Returns NPC name whose NpcChatBar should be reopened after the
        /// DialogueBox has been dismissed; null otherwise.
        /// </summary>
        public string? ConsumePendingReopen()
        {
            if (!_awaitingDialogueClose) return null;
            if (Game1.activeClickableMenu is DialogueBox) return null;

            string? npc = _pendingReopenNpc;
            _pendingReopenNpc      = null;
            _awaitingDialogueClose = false;
            return npc;
        }

        private sealed class ChatSayParams
        {
            [JsonPropertyName("speaker")] public string? Speaker { get; set; }
            [JsonPropertyName("text")]    public string? Text    { get; set; }
            [JsonPropertyName("color")]   public string? Color   { get; set; }
        }
    }
}
