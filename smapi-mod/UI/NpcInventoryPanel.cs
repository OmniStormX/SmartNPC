// NpcInventoryPanel — modal grid view of one NPC's backpack.
// 4 columns, rows grow with item count (min 2 rows = 8 slots).
// Hover a slot to see item name + count + quality in the bottom bar.
// Read-only; close with Esc or the X button.

using System;
using System.Collections.Generic;
using Microsoft.Xna.Framework;
using Microsoft.Xna.Framework.Graphics;
using StardewValley;
using StardewValley.Menus;

namespace SmartNPC.Bridge
{
    internal sealed class NpcInventoryPanel : IClickableMenu
    {
        private const int Cols        = 4;
        private const int CellSize    = 64;
        private const int Padding     = 16;
        private const int TitleHeight = 48;
        private const int FooterH     = 40;
        private const int MinRows     = 2;

        private readonly string          _npcName;
        private readonly NpcInventory    _inventory;
        private readonly List<ItemSlot>  _items;

        private int    _hoveredIndex = -1;

        public NpcInventoryPanel(string npcName, NpcInventory inventory)
            : base(0, 0, 0, 0, showUpperRightCloseButton: true)
        {
            _npcName   = npcName;
            _inventory = inventory;
            _items     = new List<ItemSlot>(inventory.GetItems(npcName));

            // Compute panel size.
            int rows    = Math.Max(MinRows, (int)Math.Ceiling(_items.Count / (double)Cols));
            int panelW  = Cols * CellSize + Padding * 2;
            int panelH  = TitleHeight + rows * CellSize + Padding * 2 + FooterH;

            // Centre on screen.
            int x = (Game1.uiViewport.Width  - panelW) / 2;
            int y = (Game1.uiViewport.Height - panelH) / 2;

            initialize(x, y, panelW, panelH, showUpperRightCloseButton: true);
        }

        public override void draw(SpriteBatch b)
        {
            // Dim background.
            b.Draw(Game1.fadeToBlackRect,
                new Rectangle(0, 0, Game1.uiViewport.Width, Game1.uiViewport.Height),
                Color.Black * 0.5f);

            // Panel background.
            IClickableMenu.drawTextureBox(b, xPositionOnScreen, yPositionOnScreen, width, height, Color.White);

            // Title.
            string title = $"{_npcName} 的背包";
            b.DrawString(Game1.dialogueFont, title,
                new Vector2(xPositionOnScreen + Padding, yPositionOnScreen + Padding),
                Game1.textColor);

            // Grid cells.
            int gridTop = yPositionOnScreen + TitleHeight + Padding;
            int gridLeft = xPositionOnScreen + Padding;

            for (int i = 0; i < Math.Max(MinRows * Cols, _items.Count + 1); i++)
            {
                int col = i % Cols;
                int row = i / Cols;
                int cx  = gridLeft + col * CellSize;
                int cy  = gridTop  + row * CellSize;
                var cellRect = new Rectangle(cx, cy, CellSize, CellSize);

                // Cell background / highlight.
                Color bg = (i == _hoveredIndex) ? Color.LightYellow : Color.White * 0.3f;
                b.Draw(Game1.fadeToBlackRect, cellRect, bg);
                IClickableMenu.drawTextureBox(b, Game1.menuTexture,
                    new Rectangle(0, 256, 60, 60), cx, cy, CellSize, CellSize,
                    Color.White, drawShadow: false);

                if (i >= _items.Count) continue;
                var slot = _items[i];

                // Draw item icon.
                try
                {
                    var item = StardewValley.ItemRegistry.Create(slot.ItemId, slot.Count, slot.Quality);
                    item?.drawInMenu(b, new Vector2(cx + 4, cy + 4), 0.85f,
                        transparency: 1f, layerDepth: 0.9f,
                        drawStackNumber: StackDrawType.Draw,
                        color: Color.White, drawShadow: true);
                }
                catch
                {
                    // Unknown item id — draw placeholder text.
                    b.DrawString(Game1.smallFont, "?",
                        new Vector2(cx + CellSize / 2f - 6, cy + CellSize / 2f - 8),
                        Color.DarkRed);
                }
            }

            // Footer: hovered item detail.
            int footerY = yPositionOnScreen + height - FooterH - Padding / 2;
            if (_hoveredIndex >= 0 && _hoveredIndex < _items.Count)
            {
                var slot = _items[_hoveredIndex];
                string qualityLabel = slot.Quality switch
                {
                    1 => "银质",
                    2 => "金质",
                    4 => "铱质",
                    _ => "普通",
                };
                string detail = $"{slot.ItemId}  ×{slot.Count}  品质：{qualityLabel}";
                try
                {
                    var item = StardewValley.ItemRegistry.Create(slot.ItemId);
                    if (item is not null)
                        detail = $"{item.DisplayName}  ×{slot.Count}  品质：{qualityLabel}";
                }
                catch { }
                b.DrawString(Game1.smallFont, detail,
                    new Vector2(xPositionOnScreen + Padding, footerY),
                    Game1.textColor);
            }

            // Close button.
            base.draw(b);
            drawMouse(b);
        }

        public override void performHoverAction(int x, int y)
        {
            base.performHoverAction(x, y);
            _hoveredIndex = HitTestSlot(x, y);
        }

        public override void receiveRightClick(int x, int y, bool playSound = true) =>
            exitThisMenu();

        public override void receiveKeyPress(Microsoft.Xna.Framework.Input.Keys key)
        {
            if (key == Microsoft.Xna.Framework.Input.Keys.Escape)
                exitThisMenu();
            base.receiveKeyPress(key);
        }

        // ── helpers ───────────────────────────────────────────────

        private int HitTestSlot(int sx, int sy)
        {
            int gridTop  = yPositionOnScreen + TitleHeight + Padding;
            int gridLeft = xPositionOnScreen + Padding;
            int rows     = Math.Max(MinRows, (int)Math.Ceiling(_items.Count / (double)Cols));

            for (int i = 0; i < rows * Cols; i++)
            {
                int col = i % Cols;
                int row = i / Cols;
                var r   = new Rectangle(gridLeft + col * CellSize, gridTop + row * CellSize,
                                        CellSize, CellSize);
                if (r.Contains(sx, sy)) return i;
            }
            return -1;
        }
    }
}
