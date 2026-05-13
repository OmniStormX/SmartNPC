using Microsoft.Xna.Framework;
using Microsoft.Xna.Framework.Graphics;
using Microsoft.Xna.Framework.Input;
using StardewValley;
using StardewValley.Menus;
using System;

namespace SmartNPC.Bridge
{
    internal sealed class NpcChatBar : IClickableMenu
    {
        private const int BarHeight = 64;
        private const int BarPadding = 12;
        private const int PortraitSize = 48;
        private const int LabelWidth = 100;
        private const int CloseBtnSize = 36;

        private readonly string _npcName;
        private readonly string _npcDisplayName;
        private readonly ChatMessageStore _store;
        private readonly Action<string, string> _onSend;
        private readonly TextBox _textBox;

        public static string? ActiveBarNpc { get; private set; }

        public NpcChatBar(string npcName, string npcDisplayName, ChatMessageStore store, Action<string, string> onSend)
            : base(0, Game1.uiViewport.Height - BarHeight, Game1.uiViewport.Width, BarHeight, showUpperRightCloseButton: false)
        {
            _npcName = npcName;
            _npcDisplayName = npcDisplayName;
            _store = store;
            _onSend = onSend;
            ActiveBarNpc = npcName;

            int tbX = xPositionOnScreen + BarPadding + PortraitSize + 8 + LabelWidth + 8;
            int tbY = yPositionOnScreen + (BarHeight - 48) / 2;
            int tbW = width - tbX - BarPadding - CloseBtnSize - 16;
            _textBox = new TextBox(
                Game1.content.Load<Texture2D>("LooseSprites\\textBox"),
                null, Game1.smallFont, Color.Black)
            {
                X = tbX,
                Y = tbY,
                Width = tbW,
                Text = "",
            };
            _textBox.OnEnterPressed += this.OnTextBoxEnter;
            Game1.keyboardDispatcher.Subscriber = _textBox;
            _textBox.Selected = true;
        }

        private Rectangle GetCloseButtonRect()
        {
            int x = xPositionOnScreen + width - BarPadding - CloseBtnSize;
            int y = yPositionOnScreen + (BarHeight - CloseBtnSize) / 2;
            return new Rectangle(x, y, CloseBtnSize, CloseBtnSize);
        }

        public override void draw(SpriteBatch b)
        {
            // Semi-transparent dark background
            b.Draw(Game1.fadeToBlackRect,
                new Rectangle(xPositionOnScreen, yPositionOnScreen, width, height),
                Color.Black * 0.8f);

            // Top border line (warm brown)
            b.Draw(Game1.fadeToBlackRect,
                new Rectangle(xPositionOnScreen, yPositionOnScreen, width, 2),
                new Color(120, 80, 40));

            // NPC portrait (48x48)
            int portraitX = xPositionOnScreen + BarPadding;
            int portraitY = yPositionOnScreen + (BarHeight - PortraitSize) / 2;
            try
            {
                Texture2D portrait = Game1.content.Load<Texture2D>($"Portraits\\{_npcName}");
                b.Draw(portrait,
                    new Rectangle(portraitX, portraitY, PortraitSize, PortraitSize),
                    new Rectangle(0, 0, 64, 64), Color.White);
            }
            catch
            {
                b.Draw(Game1.fadeToBlackRect,
                    new Rectangle(portraitX, portraitY, PortraitSize, PortraitSize),
                    Color.Gray);
            }

            // NPC name label
            int labelX = portraitX + PortraitSize + 8;
            int labelY = yPositionOnScreen + (BarHeight - (int)Game1.smallFont.MeasureString(_npcDisplayName).Y) / 2;
            b.DrawString(Game1.smallFont, _npcDisplayName, new Vector2(labelX, labelY), Color.White);

            // Input textbox
            _textBox.Draw(b);

            // Close button (X)
            Rectangle closeRect = GetCloseButtonRect();
            bool hover = closeRect.Contains(Game1.getMouseX(), Game1.getMouseY());
            Color closeBg = hover ? new Color(180, 60, 60) : new Color(100, 60, 60);
            b.Draw(Game1.fadeToBlackRect, closeRect, closeBg);
            string x = "X";
            Vector2 xSize = Game1.smallFont.MeasureString(x);
            b.DrawString(Game1.smallFont, x,
                new Vector2(closeRect.X + (closeRect.Width - xSize.X) / 2,
                            closeRect.Y + (closeRect.Height - xSize.Y) / 2),
                Color.White);

            drawMouse(b);
        }

        public override void receiveKeyPress(Keys key)
        {
            if (key == Keys.Escape)
            {
                exitThisMenu();
                return;
            }
        }

        public override void receiveLeftClick(int x, int y, bool playSound = true)
        {
            // Close button
            if (GetCloseButtonRect().Contains(x, y))
            {
                Game1.playSound("bigDeSelect");
                exitThisMenu();
                return;
            }

            base.receiveLeftClick(x, y, playSound);
            _textBox.Update();
        }

        public override void gameWindowSizeChanged(Rectangle oldBounds, Rectangle newBounds)
        {
            xPositionOnScreen = 0;
            yPositionOnScreen = Game1.uiViewport.Height - BarHeight;
            width = Game1.uiViewport.Width;

            int tbX = xPositionOnScreen + BarPadding + PortraitSize + 8 + LabelWidth + 8;
            int tbY = yPositionOnScreen + (BarHeight - 48) / 2;
            int tbW = width - tbX - BarPadding - 8;
            _textBox.X = tbX;
            _textBox.Y = tbY;
            _textBox.Width = tbW;
        }

        protected override void cleanupBeforeExit()
        {
            ActiveBarNpc = null;
            Game1.keyboardDispatcher.Subscriber = null;
            base.cleanupBeforeExit();
        }

        private void OnTextBoxEnter(TextBox sender)
        {
            string text = sender.Text?.Trim() ?? "";
            if (string.IsNullOrEmpty(text)) return;

            _store.Add(_npcName, Game1.player.Name, text, isPlayer: true);
            _onSend(_npcName, text);
            sender.Text = "";
        }
    }
}
