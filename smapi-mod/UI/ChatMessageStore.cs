using System;
using System.Collections.Generic;

namespace SmartNPC.Bridge
{
    internal sealed class ChatMessage
    {
        public string Speaker { get; set; } = "";
        public string Text { get; set; } = "";
        public bool IsPlayer { get; set; }
        public DateTime Time { get; set; } = DateTime.Now;
    }

    /// <summary>
    /// Per-NPC chat history storage. Lives in memory for the session.
    /// Tracks unread state per NPC for UI notification indicators.
    /// </summary>
    internal sealed class ChatMessageStore
    {
        private readonly Dictionary<string, List<ChatMessage>> _history = new();
        private readonly HashSet<string> _unread = new();
        private readonly Dictionary<string, List<ChatMessage>> _groupHistory = new();
        private readonly HashSet<string> _groupUnread = new();
        private readonly int _maxPerNpc;

        public ChatMessageStore(int maxPerNpc = 100)
        {
            _maxPerNpc = maxPerNpc;
        }

        public void Add(string npcName, string speaker, string text, bool isPlayer)
        {
            if (!_history.TryGetValue(npcName, out var list))
            {
                list = new List<ChatMessage>();
                _history[npcName] = list;
            }
            list.Add(new ChatMessage
            {
                Speaker = speaker,
                Text = text,
                IsPlayer = isPlayer,
                Time = DateTime.Now,
            });
            if (list.Count > _maxPerNpc)
                list.RemoveAt(0);

            if (!isPlayer)
                _unread.Add(npcName);
        }

        public List<ChatMessage> GetHistory(string npcName)
        {
            if (_history.TryGetValue(npcName, out var list))
                return list;
            return new List<ChatMessage>();
        }

        public bool HasUnread(string npcName) => _unread.Contains(npcName);

        public void MarkRead(string npcName) => _unread.Remove(npcName);

        public bool HasAnyUnread() => _unread.Count > 0;

        public void Clear(string npcName)
        {
            if (_history.ContainsKey(npcName))
                _history[npcName].Clear();
            _unread.Remove(npcName);
        }

        // ── group chat ─────────────────────────────────────────────────

        /// <summary>Append a message to a group chat's history.</summary>
        public void AddGroupMessage(string groupId, string speaker, string text, bool isPlayer)
        {
            if (!_groupHistory.TryGetValue(groupId, out var list))
            {
                list = new List<ChatMessage>();
                _groupHistory[groupId] = list;
            }
            list.Add(new ChatMessage
            {
                Speaker = speaker,
                Text = text,
                IsPlayer = isPlayer,
                Time = DateTime.Now,
            });
            if (list.Count > _maxPerNpc)
                list.RemoveAt(0);

            if (!isPlayer)
                _groupUnread.Add(groupId);
        }

        public List<ChatMessage> GetGroupHistory(string groupId)
        {
            if (_groupHistory.TryGetValue(groupId, out var list))
                return list;
            return new List<ChatMessage>();
        }

        public bool HasGroupUnread(string groupId) => _groupUnread.Contains(groupId);

        public void MarkGroupRead(string groupId) => _groupUnread.Remove(groupId);

        public void ClearGroup(string groupId)
        {
            if (_groupHistory.ContainsKey(groupId))
                _groupHistory[groupId].Clear();
            _groupUnread.Remove(groupId);
        }
    }
}
