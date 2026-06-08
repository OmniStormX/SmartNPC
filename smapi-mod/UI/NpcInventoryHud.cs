// NpcInventoryHud — draws a small backpack icon above Agent-managed NPCs
// when their inventory is non-empty. Click the icon to open NpcInventoryPanel.
//
// Drawn in OnRenderedHud (above world, below menus).
// Click detection in OnButtonPressed (MouseLeft only).

using System;
using System.Collections.Generic;
using Microsoft.Xna.Framework;
using Microsoft.Xna.Framework.Graphics;
using StardewModdingAPI;
using StardewValley;
using StardewValley.Menus;

namespace SmartNPC.Bridge
{
    internal sealed class NpcInventoryHud
    {
        // Icon dimensions in screen pixels (pre-scale).
        private const int IconW = 18;
        private const int IconH = 18;
        // Pixel offset above the NPC's name tag.
        private const int OffsetYAboveHead = 80;

        // SDV Cursors sheet coords for the small backpack icon:
        // row=3, col=6 in the 16×16 grid → source rect (96, 48, 16, 16)
        // (This is the "inventory bag" mini-icon used in tooltips.)
        private static readonly Rectangle BackpackSrc = new(96, 48, 16, 16);

        private readonly NpcInventory       _inventory;
        private readonly Action<string>     _openPanel;
        private readonly List<(string Name, Rectangle Bounds)> _hotspots = new();

        public NpcInventoryHud(NpcInventory inventory, Action<string> openPanel)
        {
            _inventory = inventory;
            _openPanel = openPanel;
        }

        // ── draw ──────────────────────────────────────────────────

        public void Draw(SpriteBatch sb)
        {
            _hotspots.Clear();
            if (!Context.IsWorldReady) return;
            if (Game1.activeClickableMenu != null) return;

            var player   = Game1.player;
            var location = player?.currentLocation;
            if (location is null) return;

            foreach (var npcName in AgentNpcRegistry.GetAll())
            {
                if (!_inventory.HasItems(npcName)) continue;

                NPC? npc = Game1.getCharacterFromName(npcName);
                if (npc is null || npc.currentLocation != location) continue;

                // Convert world position → screen position.
                Vector2 npcWorldPos = npc.Position;
                float screenX = (npcWorldPos.X - Game1.viewport.X) * Game1.options.zoomLevel;
                float screenY = (npcWorldPos.Y - Game1.viewport.Y - OffsetYAboveHead) * Game1.options.zoomLevel;

                // Skip if off screen.
                if (screenX < -IconW * 2 || screenX > Game1.graphics.GraphicsDevice.Viewport.Width + IconW) continue;
                if (screenY < -IconH * 2 || screenY > Game1.graphics.GraphicsDevice.Viewport.Height) continue;

                float scale = Game1.options.zoomLevel * 2f;
                int drawW = (int)(IconW * scale);
                int drawH = (int)(IconH * scale);
                int drawX = (int)(screenX - drawW / 2f);
                int drawY = (int)(screenY - drawH);

                sb.Draw(
                    texture: Game1.mouseCursors,
                    destinationRectangle: new Rectangle(drawX, drawY, drawW, drawH),
                    sourceRectangle: BackpackSrc,
                    color: Color.White * 0.92f);

                _hotspots.Add((npcName, new Rectangle(drawX, drawY, drawW, drawH)));
            }
        }

        // ── click ─────────────────────────────────────────────────

        /// <summary>
        /// Call from OnButtonPressed for MouseLeft.
        /// Returns true if the click was consumed by a backpack icon.
        /// </summary>
        public bool TryClick(int screenX, int screenY)
        {
            foreach (var (name, bounds) in _hotspots)
            {
                if (bounds.Contains(screenX, screenY))
                {
                    _openPanel(name);
                    return true;
                }
            }
            return false;
        }
    }
}
