// Handles the `chat_say` ws action. Routes replies into the chat history
// store, increments the unread counter, and either:
//   - feeds a NotificationToast (panel closed), or
//   - lets the open ChatPanel render the new bubble (panel open + selected
//     conversation matches).
//
// Falls back to the game's native DialogueBox only when the panel is closed
// AND a notification handler hasn't been wired (defensive default).

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
        private UnreadTracker? _unread;
        // (npcName, displayName, text, channel) → push toast / refresh panel.
        // channel is "group" for group-chat replies, "" / "private" otherwise.
        private Action<string, string, string, string>? _onMessage;

        public ChatHandler(IMonitor log) { _log = log; }

        public void SetMessageStore(ChatMessageStore store)
        {
            _store = store;
        }

        public void SetUnreadTracker(UnreadTracker unread)
        {
            _unread = unread;
        }

        /// <summary>Wire a callback fired on the game thread for every NPC
        /// message that arrives. ModEntry uses this to push toasts / refresh
        /// the panel.</summary>
        public void SetMessageNotifier(Action<string, string, string, string> onMessage)
        {
            _onMessage = onMessage;
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

                string channel = (p.Channel ?? "").ToLowerInvariant();
                bool isGroup = channel == "group";

                NPC? npc = Game1.getCharacterFromName(p.Speaker!);
                string displayName = npc?.displayName ?? p.Speaker!;

                if (!isGroup)
                {
                    // Private (1-on-1) reply: add to per-NPC store + unread.
                    // Group replies deliberately skip this so they don't
                    // pollute the NPC's private conversation history.
                    _store?.Add(p.Speaker!, p.Speaker!, p.Text!, isPlayer: false);

                    // If the ChatPanel is open and this conversation is
                    // selected, the user is actively reading — do not
                    // increment unread.
                    bool isActiveConversation = ChatPanel.IsOpen && ChatPanel.ActiveNpc == p.Speaker;
                    if (!isActiveConversation)
                        _unread?.IncrementUnread(p.Speaker!);
                }

                // Fan out to the panel/toast layer. The notifier decides
                // whether this message lands in a per-NPC surface or a group
                // surface based on the channel.
                _onMessage?.Invoke(p.Speaker!, displayName, p.Text!, channel);

                _log.Log($"chat_say → {(isGroup ? "group" : "private")}: <{p.Speaker}> {p.Text}", LogLevel.Trace);
            }
        }

        private sealed class ChatSayParams
        {
            [JsonPropertyName("speaker")]  public string? Speaker { get; set; }
            [JsonPropertyName("text")]     public string? Text    { get; set; }
            [JsonPropertyName("color")]    public string? Color   { get; set; }
            [JsonPropertyName("channel")]  public string? Channel { get; set; }
            [JsonPropertyName("group_id")] public string? GroupId { get; set; }
        }
    }
}
