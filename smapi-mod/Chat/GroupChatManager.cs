// Group chat session manager.
//
// Tracks active group chat sessions (group_id → participants) and
// broadcasts ws events when groups are created/modified/closed or when
// the player sends a message into a group.
//
// The agent side listens for these events and drives NPC replies; this
// class is a thin state holder + event dispatcher.

using System;
using System.Collections.Generic;
using System.Linq;
using StardewModdingAPI;

namespace SmartNPC.Bridge
{
    internal sealed class GroupChatManager
    {
        private readonly WebSocketServer _ws;
        private readonly IMonitor _log;
        private readonly Dictionary<string, GroupChatInfo> _sessions = new();

        public GroupChatManager(WebSocketServer ws, IMonitor log)
        {
            _ws = ws;
            _log = log;
        }

        /// <summary>Create a new group session with the given participants.</summary>
        public string CreateGroup(List<string> participants)
        {
            string id = "grp_" + Guid.NewGuid().ToString("N")[..8];
            var info = new GroupChatInfo
            {
                Id = id,
                Participants = new List<string>(participants),
            };
            _sessions[id] = info;

            _ = _ws.BroadcastEvent("group_create", new
            {
                group_id = id,
                participants = info.Participants.ToArray(),
            });

            _log.Log(
                $"[GroupChat] Created {id} with [{string.Join(", ", info.Participants)}]",
                LogLevel.Info);
            return id;
        }

        /// <summary>Broadcast a player message into an existing group.</summary>
        public void SendMessage(string groupId, string text)
        {
            if (!_sessions.ContainsKey(groupId))
            {
                _log.Log($"[GroupChat] SendMessage: unknown group {groupId}", LogLevel.Warn);
                return;
            }

            _ = _ws.BroadcastEvent("group_message", new
            {
                group_id = groupId,
                text,
                source = "player",
            });
        }

        /// <summary>Invite an NPC into an existing group.</summary>
        public void InviteMember(string groupId, string npc)
        {
            if (!_sessions.TryGetValue(groupId, out var info))
            {
                _log.Log($"[GroupChat] InviteMember: unknown group {groupId}", LogLevel.Warn);
                return;
            }
            if (!info.Participants.Contains(npc))
                info.Participants.Add(npc);

            _ = _ws.BroadcastEvent("group_invite", new { group_id = groupId, npc });
            _log.Log($"[GroupChat] {groupId} invited {npc}", LogLevel.Info);
        }

        /// <summary>Remove an NPC from a group.</summary>
        public void KickMember(string groupId, string npc)
        {
            if (!_sessions.TryGetValue(groupId, out var info))
            {
                _log.Log($"[GroupChat] KickMember: unknown group {groupId}", LogLevel.Warn);
                return;
            }
            info.Participants.Remove(npc);

            _ = _ws.BroadcastEvent("group_kick", new { group_id = groupId, npc });
            _log.Log($"[GroupChat] {groupId} kicked {npc}", LogLevel.Info);
        }

        /// <summary>Close and remove a group session.</summary>
        public void CloseGroup(string groupId)
        {
            if (!_sessions.Remove(groupId))
                return;

            _ = _ws.BroadcastEvent("group_close", new { group_id = groupId });
            _log.Log($"[GroupChat] Closed {groupId}", LogLevel.Info);
        }

        /// <summary>Lookup a session by id (null if not found).</summary>
        public GroupChatInfo? GetSession(string groupId) =>
            _sessions.TryGetValue(groupId, out var s) ? s : null;

        /// <summary>Snapshot of all active sessions.</summary>
        public List<GroupChatInfo> GetActiveSessions() => _sessions.Values.ToList();
    }

    internal sealed class GroupChatInfo
    {
        public string Id { get; set; } = "";
        public List<string> Participants { get; set; } = new();
    }
}
