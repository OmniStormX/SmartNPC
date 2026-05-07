// Handles the `chat_say` ws action. Routes replies either to the game's native
// DialogueBox (with portrait) — closing the ChatWindow first if it's open, and
// reopening it once the player dismisses the dialogue — or straight to the
// native dialogue box when no ChatWindow is active.

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

        // Pending-reopen state: when we close a ChatWindow to show a DialogueBox,
        // we remember the NPC name so ModEntry can reopen the ChatWindow once
        // the DialogueBox is dismissed. Written on the game thread by
        // PumpOnGameTick; read on the game thread by ConsumePendingReopen.
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

                // Add to message store (always, for history).
                _store?.Add(p.Speaker!, p.Speaker!, p.Text!, isPlayer: false);

                NPC? npc = Game1.getCharacterFromName(p.Speaker!);

                // If ChatWindow is open for this NPC, swap to a native DialogueBox
                // (with portrait) and mark it for reopen once dismissed.
                if (ChatWindow.ActiveNpc == p.Speaker)
                {
                    if (npc != null)
                    {
                        // Remember who to reopen BEFORE closing the window
                        // (cleanupBeforeExit clears ChatWindow.ActiveNpc).
                        _pendingReopenNpc     = p.Speaker;
                        _awaitingDialogueClose = true;

                        // Close the ChatWindow cleanly.
                        if (Game1.activeClickableMenu is ChatWindow)
                            Game1.exitActiveMenu();

                        // Show native NPC dialogue (portrait + typewriter).
                        var dialogue = new Dialogue(npc, "SmartNPC:response", p.Text!);
                        Game1.DrawDialogue(dialogue);
                        _log.Log($"dialogue (reopen pending): <{p.Speaker}> {p.Text}", LogLevel.Trace);
                    }
                    else
                    {
                        // No NPC instance — just leave the message in the store;
                        // ChatWindow will pick it up on next draw.
                        _log.Log($"chat_say → UI store (no NPC instance): <{p.Speaker}> {p.Text}", LogLevel.Trace);
                    }
                    continue;
                }

                // ChatWindow not open for this speaker. Fall back to native
                // DialogueBox (or chat box if no NPC instance), suppressing
                // overlap with any existing DialogueBox.
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
        /// Call from ModEntry.OnUpdateTicked. Returns the NPC name whose
        /// ChatWindow should be reopened (because the DialogueBox we swapped in
        /// has just closed), or <c>null</c>. Caller is responsible for actually
        /// reopening the window.
        /// </summary>
        public string? ConsumePendingReopen()
        {
            if (!_awaitingDialogueClose) return null;

            // Still inside the DialogueBox — keep waiting.
            if (Game1.activeClickableMenu is DialogueBox) return null;

            // DialogueBox closed. Hand off the NPC name and clear state.
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
