// ConversationView — right-pane sub-component of ChatPanel.
//
// Shows a chat-bubble transcript with the selected NPC, an input box and a
// "Send" button. Player messages render right-aligned; NPC messages left-
// aligned. Mouse wheel scrolls history. Enter (or Send button) submits.

using System;
using System.Collections.Generic;
using Microsoft.Xna.Framework;
using Microsoft.Xna.Framework.Graphics;
using Microsoft.Xna.Framework.Input;
using StardewModdingAPI;
using StardewValley;
using StardewValley.Menus;

namespace SmartNPC.Bridge
{
    /// <summary>
    /// Right pane: chat transcript + input. Owned by <see cref="ChatPanel"/>.
    /// </summary>
    internal sealed class ConversationView
    {
        private const int HeaderHeight = 56;
        private const int InputAreaHeight = 64;
        private const int Padding = 16;
        private const int BubblePadding = 10;
        private const int BubbleSpacing = 10;
        private const int MaxBubbleWidthRatio = 75; // % of pane width.

        private readonly ChatMessageStore _store;
        private readonly Action<string, string> _onSend; // (npcName, text)

        private Rectangle _bounds;
        private string? _npcName;
        private string? _displayName;
        private TextBox? _textBox;
        private Rectangle _sendBtnRect;
        private int _scrollOffset; // bubbles skipped from bottom.

        public ConversationView(ChatMessageStore store, Action<string, string> onSend)
        {
            _store = store;
            _onSend = onSend;
        }

        public string? CurrentNpc => _npcName;

        public void SetBounds(Rectangle bounds)
        {
            _bounds = bounds;
            RebuildTextBox();
        }

        public void SetNpc(string? npcName)
        {
            if (_npcName == npcName) return;
            _npcName = npcName;
            if (npcName == GroupChatManager.GroupKey)
                _displayName = GroupChatManager.GroupDisplayName;
            else
                _displayName = npcName == null ? null
                    : (Game1.getCharacterFromName(npcName)?.displayName ?? npcName);
            _scrollOffset = 0;
            RebuildTextBox();
        }

        private void RebuildTextBox()
        {
            if (_bounds.Width <= 0)
            {
                _textBox = null;
                return;
            }
            int tbX = _bounds.X + Padding;
            int tbY = _bounds.Bottom - InputAreaHeight + 8;
            int tbW = _bounds.Width - Padding * 2 - 96; // leave room for Send button.
            int sendBtnX = tbX + tbW + 8;
            _sendBtnRect = new Rectangle(sendBtnX, tbY - 4, 84, 44);

            var existing = _textBox?.Text ?? "";
            _textBox = new TextBox(
                Game1.content.Load<Texture2D>("LooseSprites\\textBox"),
                null, Game1.smallFont, Color.Black)
            {
                X = tbX,
                Y = tbY,
                Width = tbW,
                Text = existing,
            };
            _textBox.OnEnterPressed += this.OnTextBoxEnter;
        }

        public void Focus()
        {
            if (_textBox is null) return;
            Game1.keyboardDispatcher.Subscriber = _textBox;
            _textBox.Selected = true;
        }

        public void Unfocus()
        {
            if (Game1.keyboardDispatcher.Subscriber == _textBox)
                Game1.keyboardDispatcher.Subscriber = null;
            if (_textBox != null) _textBox.Selected = false;
        }

        public void Draw(SpriteBatch b)
        {
            // Background.
            b.Draw(Game1.fadeToBlackRect, _bounds, new Color(252, 248, 232));

            // Header.
            Rectangle header = new(_bounds.X, _bounds.Y, _bounds.Width, HeaderHeight);
            b.Draw(Game1.fadeToBlackRect, header, new Color(80, 110, 160));
            string title = _displayName ?? "选择一个联系人开始聊天";
            b.DrawString(Game1.dialogueFont, title,
                new Vector2(_bounds.X + Padding, _bounds.Y + 12), Color.White);

            if (_npcName == null)
            {
                b.DrawString(Game1.smallFont, "← 在左侧选择一个 NPC",
                    new Vector2(_bounds.X + Padding, _bounds.Y + HeaderHeight + Padding),
                    Color.Gray);
                return;
            }

            DrawTranscript(b);
            DrawInputArea(b);
        }

        private void DrawTranscript(SpriteBatch b)
        {
            int areaTop = _bounds.Y + HeaderHeight + 4;
            int areaBottom = _bounds.Bottom - InputAreaHeight - 4;
            int paneWidth = _bounds.Width;
            int maxBubbleWidth = paneWidth * MaxBubbleWidthRatio / 100;

            var messages = _store.GetHistory(_npcName!);
            if (messages.Count == 0)
            {
                b.DrawString(Game1.smallFont, "（开始你们的对话吧）",
                    new Vector2(_bounds.X + Padding, areaTop + Padding),
                    Color.LightGray);
                return;
            }

            int y = areaBottom;
            int endIdx = Math.Max(0, messages.Count - _scrollOffset);
            bool isGroupMode = _npcName == GroupChatManager.GroupKey;

            for (int i = endIdx - 1; i >= 0 && y > areaTop; i--)
            {
                var msg = messages[i];

                // In group mode, prepend speaker name for non-player messages.
                string displayText = msg.Text;
                if (isGroupMode && !msg.IsPlayer && msg.Speaker != "系统")
                    displayText = $"[{msg.Speaker}] {msg.Text}";

                string wrapped = Game1.parseText(displayText, Game1.smallFont, maxBubbleWidth - BubblePadding * 2);
                Vector2 textSize = Game1.smallFont.MeasureString(wrapped);

                int bubbleW = (int)textSize.X + BubblePadding * 2;
                int bubbleH = (int)textSize.Y + BubblePadding * 2;

                int bubbleX, timeX;
                Color bubbleColor, textColor;
                if (msg.IsPlayer)
                {
                    bubbleX = _bounds.Right - Padding - bubbleW;
                    bubbleColor = new Color(150, 200, 110);
                    textColor = Color.Black;
                    timeX = bubbleX - 60;
                }
                else
                {
                    bubbleX = _bounds.X + Padding;
                    bubbleColor = new Color(255, 255, 255);
                    textColor = Color.Black;
                    timeX = bubbleX + bubbleW + 8;
                }

                int bubbleY = y - bubbleH;
                if (bubbleY < areaTop) break;

                // Bubble box.
                Rectangle bubble = new(bubbleX, bubbleY, bubbleW, bubbleH);
                b.Draw(Game1.fadeToBlackRect, bubble, bubbleColor);
                // Subtle border.
                DrawBorder(b, bubble, new Color(0, 0, 0, 40));

                // Text inside bubble.
                b.DrawString(Game1.smallFont, wrapped,
                    new Vector2(bubble.X + BubblePadding, bubble.Y + BubblePadding),
                    textColor);

                // Timestamp (game-time formatted).
                string ts = FormatTime(msg.Time);
                b.DrawString(Game1.tinyFont, ts,
                    new Vector2(timeX, bubbleY + 2), Color.Gray);

                y = bubbleY - BubbleSpacing;
            }

            // "Waiting" hint when last message is player's.
            if (messages.Count > 0 && messages[^1].IsPlayer && _scrollOffset == 0)
            {
                b.DrawString(Game1.smallFont, "对方正在输入...",
                    new Vector2(_bounds.X + Padding, areaBottom - 4),
                    Color.Gray);
            }
        }

        private void DrawInputArea(SpriteBatch b)
        {
            // Top separator.
            b.Draw(Game1.fadeToBlackRect,
                new Rectangle(_bounds.X, _bounds.Bottom - InputAreaHeight, _bounds.Width, 2),
                new Color(180, 160, 110));

            _textBox?.Draw(b);

            // Send button.
            bool hover = _sendBtnRect.Contains(Game1.getMouseX(), Game1.getMouseY());
            Color fill = hover ? new Color(255, 220, 140) : new Color(245, 200, 110);
            b.Draw(Game1.fadeToBlackRect, _sendBtnRect, fill);
            DrawBorder(b, _sendBtnRect, new Color(120, 90, 40));
            string label = "发送";
            Vector2 lsize = Game1.smallFont.MeasureString(label);
            b.DrawString(Game1.smallFont, label,
                new Vector2(_sendBtnRect.X + (_sendBtnRect.Width - lsize.X) / 2,
                            _sendBtnRect.Y + (_sendBtnRect.Height - lsize.Y) / 2),
                Color.Black);
        }

        private static void DrawBorder(SpriteBatch b, Rectangle rect, Color color)
        {
            b.Draw(Game1.fadeToBlackRect, new Rectangle(rect.X, rect.Y, rect.Width, 2), color);
            b.Draw(Game1.fadeToBlackRect, new Rectangle(rect.X, rect.Bottom - 2, rect.Width, 2), color);
            b.Draw(Game1.fadeToBlackRect, new Rectangle(rect.X, rect.Y, 2, rect.Height), color);
            b.Draw(Game1.fadeToBlackRect, new Rectangle(rect.Right - 2, rect.Y, 2, rect.Height), color);
        }

        private static string FormatTime(DateTime t)
        {
            // Prefer in-game time when world is loaded; otherwise wall clock.
            try
            {
                if (Context.IsWorldReady)
                {
                    int gameTime = Game1.timeOfDay; // e.g. 1430 = 14:30
                    int hh = gameTime / 100;
                    int mm = gameTime % 100;
                    return $"{hh:D2}:{mm:D2}";
                }
            }
            catch { /* fall through */ }
            return t.ToString("HH:mm");
        }

        public bool ReceiveLeftClick(int x, int y)
        {
            if (_npcName == null) return false;

            // Send button.
            if (_sendBtnRect.Contains(x, y))
            {
                if (_textBox != null) OnTextBoxEnter(_textBox);
                return true;
            }

            // Click input area: focus text box.
            if (_textBox != null
                && new Rectangle(_textBox.X, _textBox.Y - 8, _textBox.Width, 44).Contains(x, y))
            {
                Focus();
                _textBox.Update();
                return true;
            }

            return false;
        }

        public void ReceiveScrollWheelAction(int direction)
        {
            if (_npcName == null) return;
            var messages = _store.GetHistory(_npcName);
            if (direction > 0 && _scrollOffset < messages.Count - 1) _scrollOffset++;
            else if (direction < 0 && _scrollOffset > 0) _scrollOffset--;
        }

        public bool HandleKeyPress(Keys key)
        {
            // While a TextBox is selected, the keyboard dispatcher consumes
            // letter keys; we only need to handle special keys here.
            return false;
        }

        public void ResetScroll()
        {
            _scrollOffset = 0;
        }

        private void OnTextBoxEnter(TextBox sender)
        {
            string text = sender.Text?.Trim() ?? "";
            if (string.IsNullOrEmpty(text) || _npcName == null) return;

            _store.Add(_npcName, Game1.player.Name, text, isPlayer: true);
            _onSend(_npcName, text);
            sender.Text = "";
            _scrollOffset = 0;
        }
    }
}
