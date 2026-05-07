using System.Collections.Generic;
using System.Linq;

namespace SmartNPC.Bridge
{
    /// <summary>
    /// Registry of NPC names managed by an external Agent.
    /// Agent-managed NPCs bypass the once-per-day dialogue limit.
    /// </summary>
    internal static class AgentNpcRegistry
    {
        private static readonly HashSet<string> _managed = new();

        public static void Register(string npcName)
        {
            _managed.Add(npcName);
        }

        public static bool IsManaged(string npcName)
        {
            return _managed.Contains(npcName);
        }

        public static List<string> GetAll()
        {
            return _managed.ToList();
        }
    }
}
