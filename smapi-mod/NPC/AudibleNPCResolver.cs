// Resolves which Agent-managed NPCs are "in earshot" of a position, so
// proactive / ambient game events can be routed to specific Hermes
// profiles instead of the legacy generic chat_received channel.
//
// Used by:
//   - the chat_received -> chat_message converter (when the player types
//     into the in-game chat box without explicitly addressing an NPC, we
//     try to deliver the message to the nearest Agent-managed NPC).
//   - debug commands that need to enumerate nearby NPCs without the
//     `npc_get_nearby` round-trip.
//
// Design notes:
//   - Read-only. Must be called on the game thread (touches Game1 state).
//   - Filter is intersection of {is Agent-managed} ∩ {distance ≤ radius}.
//   - Distance is Euclidean tile distance — we don't care about pathing,
//     only audible range.

using System;
using System.Collections.Generic;
using System.Linq;
using Microsoft.Xna.Framework;
using StardewValley;

namespace SmartNPC.Bridge
{
    /// <summary>One entry in the resolver's output.</summary>
    internal sealed class AudibleNpcEntry
    {
        public string Name = string.Empty;
        public string Map = string.Empty;
        public double Distance;     // tiles
        public int TileX;
        public int TileY;
    }

    internal static class AudibleNPCResolver
    {
        /// <summary>
        /// Default audible radius in tiles. Tuned for "the player and NPC
        /// could plausibly be having a conversation at this distance."
        /// </summary>
        public const double DefaultRadius = 8.0;

        /// <summary>
        /// Enumerate Agent-managed NPCs near the given position, sorted by
        /// distance (closest first). Returns an empty list when there are no
        /// matches or when arguments are invalid.
        /// </summary>
        /// <param name="originTile">Reference tile position (usually the player's <c>Tile</c>).</param>
        /// <param name="map">Map name. Pass <c>null</c> to skip the map filter.</param>
        /// <param name="radius">Audible radius in tiles. Non-positive uses <see cref="DefaultRadius"/>.</param>
        public static IList<AudibleNpcEntry> Resolve(Vector2 originTile, string? map, double radius = 0)
        {
            if (radius <= 0) radius = DefaultRadius;

            var result = new List<AudibleNpcEntry>();
            if (Game1.locations is null) return result;

            foreach (var location in Game1.locations)
            {
                if (location?.characters is null) continue;
                if (map != null && !string.Equals(location.Name, map, StringComparison.Ordinal)) continue;

                foreach (var npc in location.characters)
                {
                    if (npc?.Name is null) continue;
                    if (!AgentNpcRegistry.IsManaged(npc.Name)) continue;

                    Vector2 t = npc.Tile;
                    double dx = t.X - originTile.X;
                    double dy = t.Y - originTile.Y;
                    double dist = Math.Sqrt(dx * dx + dy * dy);
                    if (dist > radius) continue;

                    result.Add(new AudibleNpcEntry
                    {
                        Name = npc.Name,
                        Map = location.Name ?? string.Empty,
                        Distance = dist,
                        TileX = (int)t.X,
                        TileY = (int)t.Y,
                    });
                }
            }

            result.Sort((a, b) => a.Distance.CompareTo(b.Distance));
            return result;
        }

        /// <summary>
        /// Convenience overload: resolve from the local player's current tile
        /// and map. Returns an empty list if no save is loaded.
        /// </summary>
        public static IList<AudibleNpcEntry> ResolveAroundPlayer(double radius = 0)
        {
            if (Game1.player is null || Game1.currentLocation is null)
                return new List<AudibleNpcEntry>();

            return Resolve(Game1.player.Tile, Game1.currentLocation.Name, radius);
        }

        /// <summary>
        /// Pick the single closest Agent-managed NPC within radius around the
        /// player, or <c>null</c> if none are in range.
        /// </summary>
        public static AudibleNpcEntry? NearestToPlayer(double radius = 0)
        {
            return ResolveAroundPlayer(radius).FirstOrDefault();
        }
    }
}
