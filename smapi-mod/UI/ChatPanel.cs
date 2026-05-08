using Microsoft.Xna.Framework;
using Microsoft.Xna.Framework.Graphics;
using Microsoft.Xna.Framework.Input;
using StardewValley;
using StardewValley.Menus;
using System;
using System.Collections.Generic;

namespace SmartNPC.Bridge
{
    internal sealed class ChatPanel : IClickableMenu
    {
        private const int PanelWidth = 920;
        private const int PanelHeight = 620;
        private const int SidebarWidth = 130;
        private const int HeaderHeight = 50;
        private const int InputHeight = 52;
        private const int Padding = 12;
        private const int PortraitSlotSize = 84;
        private const int PortraitDrawSize = 64;
        private const int MessageSpacing = 10;
        private const int BubblePadding = 8;
        private const int MaxBubbleWidth = 500;

        private static readonly Color SidebarBg = new(38, 38, 50);
        private static readonly Color SidebarSelected = new(60, 60, 82);
        private static readonly Color HeaderBg = new(250, 248, 245);
        private static readonly Color MessageAreaBg = new(245, 243, 240);
        private static readonly Color PlayerBubble = new(55, 130, 210);
        private static readonly Color NpcBubble = new(255, 255, 255);
        private static readonly Color InputAreaBg = new(252, 250, 248);
        private static readonly Color UnreadDot = new(230, 60, 60);
        private static readonly Color WoodBorder = new(110, 70, 35);

        private readonly ChatMessageStore _store;
        private readonly Action<string, string> _onSend;
        private readonly List<string> _npcList;
        private readonly TextBox _textBox;
        private string _selectedNpc;
        private int _scrollOffset;

        public static bool IsOpen { get; private set; }

        public ChatPanel(ChatMessageStore store, Action<string, string> onSend, string? initialNpc = null)
            : base(
                (Game1.uiViewport.Width - PanelWidth) / 2,
                (Game1.uiViewport.Height - PanelHeight) / 2,
                PanelWidth, PanelHeight, showUpperRightCloseButton: true)
        {
            _store = store;
            _onSend = onSend;
            _npcList = AgentNpcRegistry.GetAll();
            _selectedNpc = initialNpc ?? (_npcList.Count > 0 ? _npcList[0] : "");
            IsOpen = true;

            if (!string.IsNullOrEmpty(_selectedNpc))
                _store.MarkRead(_selectedNpc);

            int inputAreaX = xPositionOnScreen + SidebarWidth + Padding;
            int inputAreaY = yPositionOnScreen + height - InputHeight - Padding;
            int inputW = width - SidebarWidth - Padding * 3 - 60;
            _textBox = new TextBox(
                Game1.content.Load<Texture2D>("LooseSprites\\textBox"),
                null, Game1.smallFont, Color.Black)
            {
                X = inputAreaX,
                Y = inputAreaY,
                Width = inputW,
                Text = "",
            };
            _textBox.OnEnterPressed += this.OnTextBoxEnter;
            Game1.keyboardDispatcher.Subscriber = _textBox;
            _textBox.Selected = true;
        }

        public void SelectNpc(string npcName)
        {
            if (_npcList.Contains(npcName))
            {
                _selectedNpc = npcName;
                _scrollOffset = 0;
                _store.MarkRead(npcName);
            }
        }

        public override void draw(SpriteBatch b)
        {
            // Dim game background
            b.Draw(Game1.fadeToBlackRect, Game1.graphics.GraphicsDevice.Viewport.Bounds, Color.Black * 0.5f);

            // Main panel background
            b.Draw(Game1.fadeToBlackRect,
                new Rectangle(xPositionOnScreen, yPositionOnScreen, width, height),
                MessageAreaBg);

            // Wood border (4px)
            DrawBorder(b, xPositionOnScreen, yPositionOnScreen, width, height, 4, WoodBorder);

            // Sidebar
            DrawSidebar(b);

            // Header
            DrawHeader(b);

            // Messages
            DrawMessages(b);

            // Input area
            DrawInputArea(b);

            base.draw(b);
            drawMouse(b);
        }

        private void DrawSidebar(SpriteBatch b)
        {
            Rectangle sidebar = new(xPositionOnScreen, yPositionOnScreen, SidebarWidth, height);
            b.Draw(Game1.fadeToBlackRect, sidebar, SidebarBg);

            // Right border of sidebar
            b.Draw(Game1.fadeToBlackRect,
                new Rectangle(xPositionOnScreen + SidebarWidth - 1, yPositionOnScreen, 1, height),
                new Color(55, 55, 70));

            int y = yPositionOnScreen + Padding + 8;
            foreach (string npcName in _npcList)
            {
                int slotX = xPositionOnScreen + (SidebarWidth - PortraitDrawSize) / 2;
                Rectangle slotRect = new(xPositionOnScreen, y - 4, SidebarWidth, PortraitSlotSize);

                // Selected highlight
                if (npcName == _selectedNpc)
                {
                    b.Draw(Game1.fadeToBlackRect, slotRect, SidebarSelected);
                    // Left accent bar
                    b.Draw(Game1.fadeToBlackRect,
                        new Rectangle(xPositionOnScreen, y - 4, 3, PortraitSlotSize),
                        new Color(80, 180, 120));
                }
                else if (slotRect.Contains(Game1.getMouseX(), Game1.getMouseY()))
                {
                    b.Draw(Game1.fadeToBlackRect, slotRect, new Color(50, 50, 65));
                }

                // Portrait
                Rectangle destRect = new(slotX, y, PortraitDrawSize, PortraitDrawSize);
                DrawPortrait(b, npcName, destRect);

                // Unread dot
                if (_store.HasUnread(npcName))
                {
                    b.Draw(Game1.fadeToBlackRect,
                        new Rectangle(destRect.Right - 6, destRect.Top - 2, 10, 10),
                        UnreadDot);
                }

                y += PortraitSlotSize;
            }
        }

        private void DrawHeader(SpriteBatch b)
        {
            int headerX = xPositionOnScreen + SidebarWidth;
            Rectangle headerRect = new(headerX, yPositionOnScreen, width - SidebarWidth, HeaderHeight);
            b.Draw(Game1.fadeToBlackRect, headerRect, HeaderBg);

            // Bottom border
            b.Draw(Game1.fadeToBlackRect,
                new Rectangle(headerX, yPositionOnScreen + HeaderHeight - 1, width - SidebarWidth, 1),
                new Color(220, 215, 210));

            // NPC display name
            if (!string.IsNullOrEmpty(_selectedNpc))
            {
                NPC? npc = Game1.getCharacterFromName(_selectedNpc);
                string displayName = npc?.displayName ?? _selectedNpc;
                Vector2 nameSize = Game1.dialogueFont.MeasureString(displayName);
                b.DrawString(Game1.dialogueFont, displayName,
                    new Vector2(headerX + Padding + 4, yPositionOnScreen + (HeaderHeight - nameSize.Y) / 2),
                    new Color(50, 50, 55));
            }
        }

        private void DrawMessages(SpriteBatch b)
        {
            if (string.IsNullOrEmpty(_selectedNpc)) return;

            int msgX = xPositionOnScreen + SidebarWidth;
            int msgTop = yPositionOnScreen + HeaderHeight;
            int msgBottom = yPositionOnScreen + height - InputHeight - Padding;
            int msgWidth = width - SidebarWidth;

            // Message area background
            b.Draw(Game1.fadeToBlackRect,
                new Rectangle(msgX, msgTop, msgWidth, msgBottom - msgTop),
                MessageAreaBg);

            var messages = _store.GetHistory(_selectedNpc);
            if (messages.Count == 0)
            {
                string hint = "还没有消息，开始聊天吧...";
                Vector2 hintSize = Game1.smallFont.MeasureString(hint);
                b.DrawString(Game1.smallFont, hint,
                    new Vector2(msgX + (msgWidth - hintSize.X) / 2, msgTop + 60),
                    Color.Gray);
                return;
            }

            // Draw messages bottom-aligned
            int y = msgBottom - Padding;
            int endIdx = Math.Max(0, messages.Count - _scrollOffset);
            for (int i = endIdx - 1; i >= 0 && y > msgTop + Padding; i--)
            {
                var msg = messages[i];
                string text = Game1.parseText(msg.Text, Game1.smallFont, MaxBubbleWidth - BubblePadding * 2);
                Vector2 textSize = Game1.smallFont.MeasureString(text);

                int bubbleW = (int)textSize.X + BubblePadding * 2;
                int bubbleH = (int)textSize.Y + BubblePadding * 2;

                y -= bubbleH + MessageSpacing;
                if (y < msgTop + Padding) break;

                int bubbleX;
                Color bubbleColor;
                Color textColor;

                if (msg.IsPlayer)
                {
                    bubbleX = msgX + msgWidth - Padding - bubbleW - 8;
                    bubbleColor = PlayerBubble;
                    textColor = Color.White;
                }
                else
                {
                    bubbleX = msgX + Padding + 8;
                    bubbleColor = NpcBubble;
                    textColor = new Color(30, 30, 35);
                }

                // Bubble background
                Rectangle bubbleRect = new(bubbleX, y, bubbleW, bubbleH);
                b.Draw(Game1.fadeToBlackRect, bubbleRect, bubbleColor);

                // Bubble shadow (subtle)
                b.Draw(Game1.fadeToBlackRect,
                    new Rectangle(bubbleX + 1, y + bubbleH, bubbleW - 2, 1),
                    Color.Black * 0.1f);

                // Text
                b.DrawString(Game1.smallFont, text,
                    new Vector2(bubbleX + BubblePadding, y + BubblePadding), textColor);
            }

            // "Typing..." indicator
            if (messages.Count > 0 && messages[^1].IsPlayer)
            {
                string typing = "对方正在输入...";
                b.DrawString(Game1.smallFont, typing,
                    new Vector2(msgX + Padding + 8, msgBottom - 20), Color.Gray * 0.7f);
            }
        }

        private void DrawInputArea(SpriteBatch b)
        {
            int inputAreaX = xPositionOnScreen + SidebarWidth;
            int inputAreaY = yPositionOnScreen + height - InputHeight - Padding;
            int inputAreaW = width - SidebarWidth;

            // Background
            b.Draw(Game1.fadeToBlackRect,
                new Rectangle(inputAreaX, inputAreaY - 4, inputAreaW, InputHeight + Padding + 4),
                InputAreaBg);

            // Top border
            b.Draw(Game1.fadeToBlackRect,
                new Rectangle(inputAreaX, inputAreaY - 4, inputAreaW, 1),
                new Color(220, 215, 210));

            // Textbox
            _textBox.Draw(b);

            // Send button
            Rectangle sendBtn = GetSendButtonRect();
            bool hover = sendBtn.Contains(Game1.getMouseX(), Game1.getMouseY());
            b.Draw(Game1.fadeToBlackRect, sendBtn, hover ? new Color(45, 160, 90) : new Color(60, 180, 100));
            string sendLabel = "发送";
            Vector2 sendSize = Game1.smallFont.MeasureString(sendLabel);
            b.DrawString(Game1.smallFont, sendLabel,
                new Vector2(sendBtn.X + (sendBtn.Width - sendSize.X) / 2, sendBtn.Y + (sendBtn.Height - sendSize.Y) / 2),
                Color.White);
        }

        private Rectangle GetSendButtonRect()
        {
            int btnW = 52;
            int btnH = 36;
            int btnX = xPositionOnScreen + width - Padding - btnW - 4;
            int btnY = yPositionOnScreen + height - InputHeight - Padding + (InputHeight - btnH) / 2;
            return new Rectangle(btnX, btnY, btnW, btnH);
        }

        public override void receiveLeftClick(int x, int y, bool playSound = true)
        {
            base.receiveLeftClick(x, y, playSound);

            // Sidebar click
            Rectangle sidebar = new(xPositionOnScreen, yPositionOnScreen, SidebarWidth, height);
            if (sidebar.Contains(x, y))
            {
                int relY = y - yPositionOnScreen - Padding - 8;
                int index = relY / PortraitSlotSize;
                if (index >= 0 && index < _npcList.Count)
                {
                    _selectedNpc = _npcList[index];
                    _scrollOffset = 0;
                    _store.MarkRead(_selectedNpc);
                    Game1.playSound("smallSelect");
                }
                return;
            }

            // Send button
            if (GetSendButtonRect().Contains(x, y))
            {
                SubmitMessage();
                return;
            }

            _textBox.Update();
        }

        public override void receiveKeyPress(Keys key)
        {
            if (key == Keys.Escape)
            {
                exitThisMenu();
                return;
            }
        }

        public override void receiveScrollWheelAction(int direction)
        {
            var msgs = _store.GetHistory(_selectedNpc);
            if (direction > 0 && _scrollOffset < msgs.Count - 5)
                _scrollOffset++;
            else if (direction < 0 && _scrollOffset > 0)
                _scrollOffset--;
        }

        public override void gameWindowSizeChanged(Rectangle oldBounds, Rectangle newBounds)
        {
            xPositionOnScreen = (Game1.uiViewport.Width - PanelWidth) / 2;
            yPositionOnScreen = (Game1.uiViewport.Height - PanelHeight) / 2;

            int inputAreaX = xPositionOnScreen + SidebarWidth + Padding;
            int inputAreaY = yPositionOnScreen + height - InputHeight - Padding;
            int inputW = width - SidebarWidth - Padding * 3 - 60;
            _textBox.X = inputAreaX;
            _textBox.Y = inputAreaY;
            _textBox.Width = inputW;
        }

        protected override void cleanupBeforeExit()
        {
            IsOpen = false;
            Game1.keyboardDispatcher.Subscriber = null;
            base.cleanupBeforeExit();
        }

        private void SubmitMessage()
        {
            string text = _textBox.Text?.Trim() ?? "";
            if (string.IsNullOrEmpty(text) || string.IsNullOrEmpty(_selectedNpc)) return;

            _store.Add(_selectedNpc, Game1.player.Name, text, isPlayer: true);
            _onSend(_selectedNpc, text);
            _textBox.Text = "";
            _scrollOffset = 0;
        }

        private void OnTextBoxEnter(TextBox sender)
        {
            SubmitMessage();
        }

        private static void DrawPortrait(SpriteBatch b, string npcName, Rectangle dest)
        {
            try
            {
                Texture2D tex = Game1.content.Load<Texture2D>($"Portraits\\{npcName}");
                b.Draw(tex, dest, new Rectangle(0, 0, 64, 64), Color.White);
            }
            catch
            {
                b.Draw(Game1.fadeToBlackRect, dest, Color.Gray);
                string initial = npcName.Length > 0 ? npcName[..1] : "?";
                Vector2 sz = Game1.smallFont.MeasureString(initial);
                b.DrawString(Game1.smallFont, initial,
                    new Vector2(dest.X + (dest.Width - sz.X) / 2, dest.Y + (dest.Height - sz.Y) / 2),
                    Color.White);
            }
        }

        private static void DrawBorder(SpriteBatch b, int x, int y, int w, int h, int thickness, Color color)
        {
            b.Draw(Game1.fadeToBlackRect, new Rectangle(x, y, w, thickness), color);
            b.Draw(Game1.fadeToBlackRect, new Rectangle(x, y + h - thickness, w, thickness), color);
            b.Draw(Game1.fadeToBlackRect, new Rectangle(x, y, thickness, h), color);
            b.Draw(Game1.fadeToBlackRect, new Rectangle(x + w - thickness, y, thickness, h), color);
        }
    }
}
