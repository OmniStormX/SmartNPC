// GroupChatManager — bridges the Mod UI with the Agent's group orchestrator.
//
// Responsibilities:
//   - Tracks the active group chat state (participants, groupID).
//   - Stores group messages into ChatMessageStore under a virtual key.
//   - Provides APIs for the ChatPanel to create/join/leave group chats.
//   - Formats NPC replies with speaker names for display.

using System;
using System.Collections.Generic;
using System.Linq;

namespace SmartNPC.Bridge
{
    /// <summary>
    /// Manages group chat state on the Mod side. Integrates with ChatPanel
    /// by storing messages under <see cref="GroupKey"/> in ChatMessageStore.
    /// </summary>
    internal sealed class GroupChatManager
    {
        /// <summary>Virtual NPC name used as the ChatMessageStore key for group chat.</summary>
        public const string GroupKey = "__group__";

        /// <summary>Display name shown in the ContactList for the group chat entry.</summary>
        public const string GroupDisplayName = "群聊";

        private readonly ChatMessageStore _store;
        private readonly WebSocketServer _ws;

        private string? _activeGroupId;
        private List<string> _participants = new();

        public GroupChatManager(ChatMessageStore store, WebSocketServer ws)
        {
            _store = store;
            _ws = ws;
        }

        /// <summary>Whether a group chat is currently active.</summary>
        public bool IsActive => _activeGroupId != null;

        /// <summary>The current group ID (null if no group active).</summary>
        public string? ActiveGroupId => _activeGroupId;

        /// <summary>Current group participants (NPC names).</summary>
        public IReadOnlyList<string> Participants => _participants;

        /// <summary>
        /// Create a group chat with the specified NPCs. Sends the group_create
        /// event to the agent via WebSocket.
        /// </summary>
        public void CreateGroup(IEnumerable<string> npcNames)
        {
            _participants = npcNames.ToList();
            // The agent will respond with the group_id via the session;
            // for now we use a local placeholder until confirmed.
            _activeGroupId = "pending";

            // Add a system message to the group history.
            string names = string.Join(", ", _participants);
            _store.Add(GroupKey, "系统", $"群聊已创建，参与者：{names}", false);

            // Fire the ws event to the agent.
            _ws?.BroadcastEvent("group_create", new { participants = _participants.ToArray() });
        }

        /// <summary>
        /// Called when the agent confirms group creation (sets the real group ID).
        /// </summary>
        public void ConfirmGroupId(string groupId)
        {
            _activeGroupId = groupId;
        }

        /// <summary>
        /// Send a player message in the active group chat.
        /// </summary>
        public void SendPlayerMessage(string text)
        {
            if (!IsActive) return;

            // Store locally.
            _store.Add(GroupKey, "我", text, true);

            // Send to agent — uses chat_received with source="player_group" so
            // the router routes it to the group orchestrator regardless of
            // whether the Ctrl+T global chat box also happens to be active.
            // Plain source="player" stays reserved for non-group chat so the
            // two surfaces can't pollute each other.
            _ws?.BroadcastEvent("chat_received", new
            {
                text,
                source = "player_group",
                group_id = _activeGroupId ?? string.Empty,
            });
        }

        /// <summary>
        /// Called when an NPC replies in the group chat (from chat_say callback).
        /// Adds the message to the group store with the NPC's name as speaker.
        /// </summary>
        public void OnNpcReply(string npcName, string text)
        {
            if (!IsActive) return;
            _store.Add(GroupKey, npcName, text, false);
        }

        /// <summary>
        /// End the active group chat.
        /// </summary>
        public void EndGroup()
        {
            if (!IsActive) return;
            _store.Add(GroupKey, "系统", "群聊已结束", false);
            _activeGroupId = null;
            _participants.Clear();
        }
    }
}
