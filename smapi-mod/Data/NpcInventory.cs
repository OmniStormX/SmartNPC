// smapi-mod/Data/NpcInventory.cs
// Per-NPC persistent backpack. Serialized with SMAPI's WriteSaveData.

using System.Collections.Generic;
using System.Linq;

namespace SmartNPC.Bridge
{
    /// <summary>One stack of items in an NPC's backpack.</summary>
    internal sealed class ItemSlot
    {
        public string ItemId  { get; set; } = "";  // SDV qualified id, e.g. "(O)390"
        public int    Count   { get; set; } = 1;
        public int    Quality { get; set; } = 0;   // 0=normal 1=silver 2=gold 4=iridium
    }

    /// <summary>
    /// In-memory backpack store for all Agent-managed NPCs.
    /// One instance lives in ModEntry; persisted per save via SMAPI.
    /// </summary>
    internal sealed class NpcInventory
    {
        private readonly Dictionary<string, List<ItemSlot>> _bags = new();

        // ── read ──────────────────────────────────────────────────

        public IReadOnlyList<ItemSlot> GetItems(string npcName)
        {
            if (string.IsNullOrEmpty(npcName)) return System.Array.Empty<ItemSlot>();
            return _bags.TryGetValue(npcName, out var bag)
                ? bag.AsReadOnly()
                : System.Array.Empty<ItemSlot>();
        }

        public bool HasItems(string npcName) =>
            _bags.TryGetValue(npcName, out var b) && b.Count > 0;

        // ── write ─────────────────────────────────────────────────

        /// <summary>Add (or stack) items. Returns the new total count for that stack.</summary>
        public int Add(string npcName, string itemId, int count, int quality = 0)
        {
            if (string.IsNullOrEmpty(npcName) || string.IsNullOrEmpty(itemId) || count <= 0)
                return 0;

            var bag = GetOrCreate(npcName);
            var slot = bag.FirstOrDefault(s =>
                s.ItemId == itemId && s.Quality == quality);

            if (slot is null)
            {
                slot = new ItemSlot { ItemId = itemId, Count = 0, Quality = quality };
                bag.Add(slot);
            }
            slot.Count += count;
            return slot.Count;
        }

        /// <summary>
        /// Remove up to <paramref name="count"/> of <paramref name="itemId"/>.
        /// Returns how many were actually removed.
        /// </summary>
        public int Take(string npcName, string itemId, int count)
        {
            if (string.IsNullOrEmpty(npcName) || string.IsNullOrEmpty(itemId) || count <= 0)
                return 0;

            if (!_bags.TryGetValue(npcName, out var bag)) return 0;

            int taken = 0;
            foreach (var slot in bag.Where(s => s.ItemId == itemId).ToList())
            {
                int canTake = System.Math.Min(slot.Count, count - taken);
                slot.Count -= canTake;
                taken      += canTake;
                if (slot.Count <= 0) bag.Remove(slot);
                if (taken >= count) break;
            }
            return taken;
        }

        public void Clear(string npcName)
        {
            _bags.Remove(npcName);
        }

        // ── persistence ───────────────────────────────────────────

        /// <summary>Snapshot for SMAPI WriteSaveData.</summary>
        public Dictionary<string, List<ItemSlot>> Snapshot() =>
            _bags.ToDictionary(kv => kv.Key, kv => kv.Value.ToList());

        /// <summary>Restore from a snapshot. Replaces all in-memory state.</summary>
        public void Restore(Dictionary<string, List<ItemSlot>>? snapshot)
        {
            _bags.Clear();
            if (snapshot is null) return;
            foreach (var kv in snapshot)
                if (kv.Value?.Count > 0)
                    _bags[kv.Key] = kv.Value;
        }

        // ── helpers ───────────────────────────────────────────────

        private List<ItemSlot> GetOrCreate(string npcName)
        {
            if (!_bags.TryGetValue(npcName, out var bag))
            {
                bag = new List<ItemSlot>();
                _bags[npcName] = bag;
            }
            return bag;
        }
    }
}
