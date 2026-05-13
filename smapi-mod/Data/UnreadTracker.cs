// Unread message counter, keyed by NPC name. Lives in memory; ModEntry
// persists snapshots through SMAPI's per-save data API.

using System.Collections.Generic;
using System.Linq;

namespace SmartNPC.Bridge
{
    /// <summary>
    /// Tracks per-NPC unread message counts for the chat panel UI.
    /// </summary>
    internal sealed class UnreadTracker
    {
        private readonly Dictionary<string, int> _counts = new();

        public void IncrementUnread(string npcName)
        {
            if (string.IsNullOrEmpty(npcName)) return;
            _counts[npcName] = (_counts.TryGetValue(npcName, out int n) ? n : 0) + 1;
        }

        public void MarkRead(string npcName)
        {
            if (string.IsNullOrEmpty(npcName)) return;
            _counts.Remove(npcName);
        }

        public int GetUnread(string npcName)
        {
            if (string.IsNullOrEmpty(npcName)) return 0;
            return _counts.TryGetValue(npcName, out int n) ? n : 0;
        }

        public int TotalUnread => _counts.Values.Sum();

        /// <summary>Snapshot for save persistence.</summary>
        public Dictionary<string, int> Snapshot()
        {
            return new Dictionary<string, int>(_counts);
        }

        /// <summary>Restore from a snapshot. Replaces in-memory state.</summary>
        public void Restore(Dictionary<string, int>? snapshot)
        {
            _counts.Clear();
            if (snapshot is null) return;
            foreach (var kv in snapshot)
                if (kv.Value > 0) _counts[kv.Key] = kv.Value;
        }

        public void Reset()
        {
            _counts.Clear();
        }
    }
}
