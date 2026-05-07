using System.Collections.Generic;
using Microsoft.Xna.Framework;
using Microsoft.Xna.Framework.Graphics;
using Microsoft.Xna.Framework.Input;
using StardewValley;
using StardewValley.Menus;

namespace SmartNPC.Bridge
{
    /// <summary>
    /// Friend list showing all Agent-managed NPCs. Click one to open ChatWindow.
    /// </summary>
    internal sealed class FriendListWindow : IClickableMenu
    {
        private const int WindowWidth = 400;
        private const int RowHeight = 64;
        private const int Padding = 16;

        private readonly ChatMessageStore _store;
        private readonly System.Action<string> _onSelectNpc;
        private readonly List<string> _npcNames;

        public FriendListWindow(ChatMessageStore store, System.Action<string> onSelectNpc)
            : base(0, 0, WindowWidth, 0, showUpperRightCloseButton: true)
        {
            _store = store;
            _onSelectNpc = onSelectNpc;
            _npcNames = AgentNpcRegistry.GetAll();

            // Calculate height based on NPC count
            height = Padding * 2 + 48 + _npcNames.Count * RowHeight + Padding;
            xPositionOnScreen = (Game1.uiViewport.Width - width) / 2;
            yPositionOnScreen = (Game1.uiViewport.Height - height) / 2;

            initializeUpperRightCloseButton();
        }

        public override void draw(SpriteBatch b)
        {
            // Dim background
            b.Draw(Game1.fadeToBlackRect, Game1.graphics.GraphicsDevice.Viewport.Bounds, Color.Black * 0.4f);

            // Window
            drawTextureBox(b, xPositionOnScreen, yPositionOnScreen, width, height, Color.White);

            // Title
            string title = "好友列表";
            b.DrawString(Game1.dialogueFont, title,
                new Vector2(xPositionOnScreen + Padding, yPositionOnScreen + Padding), Color.DarkSlateGray);

            // NPC rows
            int rowY = yPositionOnScreen + Padding + 48;
            for (int i = 0; i < _npcNames.Count; i++)
            {
                string npcName = _npcNames[i];
                NPC? npc = Game1.getCharacterFromName(npcName);
                string displayName = npc?.displayName ?? npcName;

                // Row highlight on hover
                Rectangle rowBounds = new(xPositionOnScreen + Padding, rowY, width - Padding * 2, RowHeight - 4);
                bool hover = rowBounds.Contains(Game1.getMouseX(), Game1.getMouseY());
                if (hover)
                    b.Draw(Game1.fadeToBlackRect, rowBounds, Color.LightBlue * 0.3f);

                // NPC name
                b.DrawString(Game1.dialogueFont, displayName,
                    new Vector2(xPositionOnScreen + Padding + 8, rowY + 12), Color.Black);

                // Online indicator
                b.DrawString(Game1.smallFont, "● 在线",
                    new Vector2(xPositionOnScreen + width - Padding - 80, rowY + 20), Color.Green);

                rowY += RowHeight;
            }

            base.draw(b);
            drawMouse(b);
        }

        public override void receiveLeftClick(int x, int y, bool playSound = true)
        {
            base.receiveLeftClick(x, y, playSound);

            int rowY = yPositionOnScreen + Padding + 48;
            for (int i = 0; i < _npcNames.Count; i++)
            {
                Rectangle rowBounds = new(xPositionOnScreen + Padding, rowY, width - Padding * 2, RowHeight - 4);
                if (rowBounds.Contains(x, y))
                {
                    exitThisMenu(playSound: false);
                    _onSelectNpc(_npcNames[i]);
                    return;
                }
                rowY += RowHeight;
            }
        }

        public override void receiveKeyPress(Keys key)
        {
            if (key == Keys.Escape || key == Keys.F2)
            {
                exitThisMenu();
            }
        }
    }
}
