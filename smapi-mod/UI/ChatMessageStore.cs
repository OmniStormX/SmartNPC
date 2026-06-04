using System;
using System.Collections.Generic;
using StardewValley;

namespace SmartNPC.Bridge
{
    internal sealed class ChatMessage
    {
        public string Speaker { get; set; } = "";
        public string Text { get; set; } = "";
        public bool IsPlayer { get; set; }
        public DateTime Time { get; set; } = DateTime.Now;
        public int GameTime { get; set; } // in-game time of day when this message was created (e.g. 1430)
    }

    /// <summary>
    /// Per-NPC chat history storage. Lives in memory for the session and is
    /// snapshotted to per-save data via <see cref="Snapshot"/> /
    /// <see cref="Restore"/>.
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
                GameTime = Game1.timeOfDay,
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

        /// <summary>Build a serialisable snapshot of the last N messages per NPC.</summary>
        public Dictionary<string, List<ChatMessage>> Snapshot()
        {
            var snap = new Dictionary<string, List<ChatMessage>>();
            foreach (var kv in _history)
            {
                int start = Math.Max(0, kv.Value.Count - _maxPerNpc);
                snap[kv.Key] = kv.Value.GetRange(start, kv.Value.Count - start);
            }
            return snap;
        }

        /// <summary>Restore from a snapshot. Replaces in-memory state.</summary>
        public void Restore(Dictionary<string, List<ChatMessage>>? snapshot)
        {
            _history.Clear();
            if (snapshot is null) return;
            foreach (var kv in snapshot)
            {
                if (kv.Value is null) continue;
                _history[kv.Key] = new List<ChatMessage>(kv.Value);
            }
        }
    }
}
