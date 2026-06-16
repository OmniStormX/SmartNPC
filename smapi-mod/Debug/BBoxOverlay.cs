// In-world bbox debug overlay.
//
// When ModConfig.DebugShowBBoxOverlay is true, this module draws axis-aligned
// rectangles on top of the SDV world to make agent decisions visible:
//
//   - Red box   : the area an `npc_inspect_object` (farm_actions mode) just
//                 scanned. Auto-fades after a short TTL.
//   - Yellow box: the bbox a behavior tool (harvest/water/clear/till/forage)
//                 is currently acting in. Cleared when the NPC's FollowSystem
//                 mode goes back to Idle.
//
// Wiring (see ModEntry.cs):
//   - InspectObjectHandler (farm_actions branch) calls MarkObserve(...)
//   - Each behavior handler calls MarkAction(npcName, ...) before kicking
//     FollowSystem off; the overlay polls FollowSystem.GetMode each tick
//     and self-clears when idle.
//   - ModEntry.OnRenderedWorld -> BBoxOverlay.Instance.Draw(e.SpriteBatch)
//
// Coordinates are tile-based and inclusive on both ends, mirroring the
// tool schemas. The render layer converts to pixel coordinates via
// Game1.GlobalToLocal so the box stays glued to the world as the player
// scrolls.

using System;
using System.Collections.Generic;
using Microsoft.Xna.Framework;
using Microsoft.Xna.Framework.Graphics;
using StardewModdingAPI;
using StardewValley;

namespace SmartNPC.Bridge
{
    internal sealed class BBoxOverlay
    {
        // Tile size in pixels (SDV constant; Game1.tileSize == 64 in vanilla).
        private const int Tile = 64;

        public static BBoxOverlay Instance { get; } = new();

        private readonly object _lock = new();
        private readonly List<ObserveEntry> _observes = new();
        private readonly List<ActionEntry> _actions = new();

        private Func<bool>? _enabled;
        private FollowSystem? _follow;
        private Color _observeColor = new(255, 64, 64, 255);
        private Color _actionColor  = new(255, 208, 48, 255);

        // Texture used to draw filled rectangles (1×1 white pixel scaled by
        // SpriteBatch). Lazily initialised on first Draw call.
        private static Texture2D? _pixel;

        private BBoxOverlay() { }

        /// <summary>
        /// One-time wiring. Pass the config gate and the FollowSystem so the
        /// overlay can self-clear yellow boxes when the NPC goes idle.
        /// </summary>
        public void Init(Func<bool> enabled, FollowSystem follow,
            Color? observeColor = null, Color? actionColor = null)
        {
            _enabled = enabled;
            _follow  = follow;
            if (observeColor.HasValue) _observeColor = observeColor.Value;
            if (actionColor.HasValue)  _actionColor  = actionColor.Value;
        }

        /// <summary>
        /// Record a fresh observation rectangle. Auto-expires after
        /// <paramref name="ttlSeconds"/> real seconds.
        /// </summary>
        public void MarkObserve(string mapName, int x1, int y1, int x2, int y2,
            double ttlSeconds = 4.0)
        {
            if (_enabled == null || !_enabled()) return;
            if (string.IsNullOrEmpty(mapName)) return;
            lock (_lock)
            {
                _observes.Add(new ObserveEntry
                {
                    Map = mapName,
                    X1 = x1, Y1 = y1, X2 = x2, Y2 = y2,
                    ExpiresUtc = DateTime.UtcNow.AddSeconds(ttlSeconds),
                });
            }
        }

        /// <summary>
        /// Record an in-progress action bbox. Stays visible until the NPC's
        /// FollowSystem mode returns to Idle (cleared in <see cref="Tick"/>).
        /// Replaces any prior entry for the same NPC so re-fires don't stack.
        /// </summary>
        public void MarkAction(string npcName, string mapName,
            int x1, int y1, int x2, int y2)
        {
            if (_enabled == null || !_enabled()) return;
            if (string.IsNullOrEmpty(npcName) || string.IsNullOrEmpty(mapName)) return;
            lock (_lock)
            {
                // Drop any prior entry for the same NPC so a new tool call
                // overwrites the box rather than stacking duplicates.
                _actions.RemoveAll(a => a.NpcName == npcName);
                _actions.Add(new ActionEntry
                {
                    NpcName = npcName,
                    Map = mapName,
                    X1 = x1, Y1 = y1, X2 = x2, Y2 = y2,
                    StartedUtc = DateTime.UtcNow,
                });
            }
        }

        /// <summary>Drop expired observe entries and idle action entries.</summary>
        public void Tick()
        {
            if (_enabled == null || !_enabled()) return;
            DateTime now = DateTime.UtcNow;
            lock (_lock)
            {
                _observes.RemoveAll(o => o.ExpiresUtc <= now);
                if (_follow != null)
                {
                    _actions.RemoveAll(a =>
                    {
                        // Grace period: a brand-new entry might be polled
                        // before FollowSystem actually transitions out of
                        // Idle on the very same tick.
                        if ((now - a.StartedUtc).TotalMilliseconds < 200)
                            return false;
                        return _follow.GetMode(a.NpcName) == NpcBehaviorMode.Idle;
                    });
                }
            }
        }

        /// <summary>
        /// Draw all active boxes onto the world. Call from
        /// Display.RenderedWorld so coordinates align with terrain.
        /// </summary>
        public void Draw(SpriteBatch sb)
        {
            if (_enabled == null || !_enabled()) return;
            if (Game1.currentLocation is null) return;
            string mapName = Game1.currentLocation.Name ?? "";

            EnsurePixel(sb.GraphicsDevice);

            // Snapshot under lock to avoid mutation during draw.
            ObserveEntry[] obs;
            ActionEntry[] acts;
            lock (_lock)
            {
                obs = _observes.ToArray();
                acts = _actions.ToArray();
            }

            foreach (var o in obs)
                if (o.Map == mapName)
                    DrawTileRect(sb, o.X1, o.Y1, o.X2, o.Y2, _observeColor);

            foreach (var a in acts)
                if (a.Map == mapName)
                    DrawTileRect(sb, a.X1, a.Y1, a.X2, a.Y2, _actionColor);
        }

        // ── helpers ─────────────────────────────────────────────────────

        private static void EnsurePixel(GraphicsDevice gd)
        {
            if (_pixel != null) return;
            _pixel = new Texture2D(gd, 1, 1);
            _pixel.SetData(new[] { Color.White });
        }

        // Draw a hollow rectangle (4 thin filled bars) covering the inclusive
        // tile range [x1..x2] × [y1..y2]. Width is 4px to stay readable on
        // top of busy farm tiles.
        private static void DrawTileRect(SpriteBatch sb, int x1, int y1, int x2, int y2, Color color)
        {
            // Tile-to-pixel: tile (tx, ty) covers [tx*Tile .. tx*Tile + Tile).
            // The bbox is inclusive on both ends, so x2+1 is the exclusive right.
            int px1 = x1 * Tile;
            int py1 = y1 * Tile;
            int px2 = (x2 + 1) * Tile;
            int py2 = (y2 + 1) * Tile;

            // Convert world pixels -> screen pixels via the active viewport.
            Vector2 topLeft  = Game1.GlobalToLocal(Game1.viewport, new Vector2(px1, py1));
            Vector2 botRight = Game1.GlobalToLocal(Game1.viewport, new Vector2(px2, py2));

            int w = (int)(botRight.X - topLeft.X);
            int h = (int)(botRight.Y - topLeft.Y);
            if (w <= 0 || h <= 0) return;

            int x = (int)topLeft.X;
            int y = (int)topLeft.Y;
            const int thickness = 4;

            // Top, bottom, left, right.
            sb.Draw(_pixel, new Rectangle(x, y, w, thickness), color);
            sb.Draw(_pixel, new Rectangle(x, y + h - thickness, w, thickness), color);
            sb.Draw(_pixel, new Rectangle(x, y, thickness, h), color);
            sb.Draw(_pixel, new Rectangle(x + w - thickness, y, thickness, h), color);
        }

        private sealed class ObserveEntry
        {
            public string Map = "";
            public int X1, Y1, X2, Y2;
            public DateTime ExpiresUtc;
        }

        private sealed class ActionEntry
        {
            public string NpcName = "";
            public string Map = "";
            public int X1, Y1, X2, Y2;
            public DateTime StartedUtc;
        }
    }
}
