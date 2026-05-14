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
using System.Linq;
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
        private GroupChatManager? _groupMgr;
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

        /// <summary>Wire the group-chat manager so PumpOnGameTick can apply
        /// the "active group, speaker is a participant → assume group" fallback
        /// when a chat_say arrives without channel="group". Compensates for
        /// the LLM occasionally forgetting the channel field on group turns.
        /// </summary>
        public void SetGroupManager(GroupChatManager mgr)
        {
            _groupMgr = mgr;
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

                // Fallback: if there's an active group chat AND the speaker
                // is a participant AND the LLM did not explicitly opt out
                // ("private"), promote this turn to group. Compensates for
                // models that drop the channel/group_id arguments on group
                // turns (we have skill + tool prompts telling them to set it,
                // but Llama-class models still forget). Reading "private"
                // explicitly still goes private — the NPC can override.
                if (!isGroup
                    && channel != "private"
                    && _groupMgr != null
                    && _groupMgr.IsActive
                    && _groupMgr.Participants.Contains(p.Speaker!))
                {
                    isGroup = true;
                    if (string.IsNullOrEmpty(p.GroupId))
                        p.GroupId = _groupMgr.ActiveGroupId;
                    _log.Log($"chat_say: missing channel='group' but group active and {p.Speaker} is a participant — promoting to group reply",
                        LogLevel.Debug);
                }

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
                _onMessage?.Invoke(p.Speaker!, displayName, p.Text!, isGroup ? "group" : channel);

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
