using System;
using System.Collections.Generic;
using Microsoft.Xna.Framework;
using Microsoft.Xna.Framework.Graphics;
using Microsoft.Xna.Framework.Input;
using StardewValley;
using StardewValley.Menus;

namespace SmartNPC.Bridge
{
    /// <summary>
    /// In-game chat window for conversing with an Agent NPC.
    /// Shows message history + text input at the bottom.
    /// </summary>
    internal sealed class ChatWindow : IClickableMenu
    {
        private const int WindowWidth = 700;
        private const int WindowHeight = 500;
        private const int Padding = 16;
        private const int InputHeight = 48;
        private const int MessageSpacing = 8;

        private readonly string _npcName;
        private readonly string _npcDisplayName;
        private readonly ChatMessageStore _store;
        private readonly Action<string, string> _onSend;

        private readonly TextBox _textBox;
        private int _scrollOffset;

        /// <summary>Currently open chat target NPC name (null if closed).</summary>
        public static string? ActiveNpc { get; private set; }

        public ChatWindow(string npcName, string npcDisplayName, ChatMessageStore store, Action<string, string> onSend)
            : base(
                (Game1.uiViewport.Width - WindowWidth) / 2,
                (Game1.uiViewport.Height - WindowHeight) / 2,
                WindowWidth, WindowHeight, showUpperRightCloseButton: true)
        {
            _npcName = npcName;
            _npcDisplayName = npcDisplayName;
            _store = store;
            _onSend = onSend;
            ActiveNpc = npcName;

            // Create text input box at the bottom of the window.
            int tbX = xPositionOnScreen + Padding + 8;
            int tbY = yPositionOnScreen + height - InputHeight - Padding + 8;
            int tbW = width - Padding * 2 - 16;
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

        public override void draw(SpriteBatch b)
        {
            // Dim background
            b.Draw(Game1.fadeToBlackRect, Game1.graphics.GraphicsDevice.Viewport.Bounds, Color.Black * 0.5f);

            // Window background
            drawTextureBox(b, xPositionOnScreen, yPositionOnScreen, width, height, Color.White);

            // Title bar
            string title = _npcDisplayName;
            Vector2 titleSize = Game1.dialogueFont.MeasureString(title);
            b.DrawString(Game1.dialogueFont, title,
                new Vector2(xPositionOnScreen + Padding, yPositionOnScreen + Padding), Color.DarkSlateGray);

            // Message area bounds
            int msgAreaTop = yPositionOnScreen + Padding + (int)titleSize.Y + Padding;
            int msgAreaBottom = yPositionOnScreen + height - InputHeight - Padding * 2;

            // Draw messages (bottom-aligned, newest at bottom)
            var messages = _store.GetHistory(_npcName);
            int y = msgAreaBottom;
            int endIdx = Math.Max(0, messages.Count - _scrollOffset);
            for (int i = endIdx - 1; i >= 0 && y > msgAreaTop; i--)
            {
                var msg = messages[i];
                string line = msg.IsPlayer ? $"[你] {msg.Text}" : $"[{msg.Speaker}] {msg.Text}";
                Color color = msg.IsPlayer ? new Color(50, 80, 160) : new Color(160, 50, 50);

                string wrapped = Game1.parseText(line, Game1.smallFont, width - Padding * 4);
                Vector2 size = Game1.smallFont.MeasureString(wrapped);
                y -= (int)size.Y + MessageSpacing;

                if (y >= msgAreaTop)
                    b.DrawString(Game1.smallFont, wrapped, new Vector2(xPositionOnScreen + Padding * 2, y), color);
            }

            // Draw "waiting..." indicator if last message is from player
            if (messages.Count > 0 && messages[^1].IsPlayer)
            {
                string waiting = "对方正在输入...";
                Vector2 ws = Game1.smallFont.MeasureString(waiting);
                b.DrawString(Game1.smallFont, waiting,
                    new Vector2(xPositionOnScreen + Padding * 2, msgAreaBottom + 2), Color.Gray);
            }

            // Input box
            _textBox.Draw(b);

            // Close button + mouse
            base.draw(b);
            drawMouse(b);
        }

        public override void receiveKeyPress(Keys key)
        {
            if (key == Keys.Escape)
            {
                exitThisMenu();
                return;
            }
            // Don't pass other keys to base (it would close menu on E etc.)
        }

        public override void receiveLeftClick(int x, int y, bool playSound = true)
        {
            base.receiveLeftClick(x, y, playSound);
            _textBox.Update();
        }

        public override void receiveScrollWheelAction(int direction)
        {
            var messages = _store.GetHistory(_npcName);
            if (direction > 0 && _scrollOffset < messages.Count - 3)
                _scrollOffset++;
            else if (direction < 0 && _scrollOffset > 0)
                _scrollOffset--;
        }

        protected override void cleanupBeforeExit()
        {
            ActiveNpc = null;
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
            _scrollOffset = 0;
        }
    }
}
