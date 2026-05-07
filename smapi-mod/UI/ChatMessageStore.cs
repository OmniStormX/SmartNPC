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
    /// </summary>
    internal sealed class ChatMessageStore
    {
        private readonly Dictionary<string, List<ChatMessage>> _history = new();
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
        }

        public List<ChatMessage> GetHistory(string npcName)
        {
            if (_history.TryGetValue(npcName, out var list))
                return list;
            return new List<ChatMessage>();
        }

        public void Clear(string npcName)
        {
            if (_history.ContainsKey(npcName))
                _history[npcName].Clear();
        }
    }
}
