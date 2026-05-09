// ChatPanel — QQ-style full chat UI.
//
// Runs in three modes:
//   SingleNpc   existing 1:1 chat; sidebar shows active groups (top)
//               + NPC portraits (middle) + "+群聊" button (bottom)
//   NpcPicker   sidebar turns into a multi-select list for building a
//               new group; "确认创建" button in the bottom slot
//   GroupChat   group-session chat; header shows participant names + a
//               "解散" button; input routes through GroupChatManager
//
// State transitions:
//   SingleNpc --click +群聊--> NpcPicker
//   NpcPicker --click 确认创建 (≥2 selected)--> GroupChat (new session)
//   NpcPicker --click group in sidebar--> GroupChat
//   GroupChat --click 解散--> SingleNpc
//   SingleNpc --click group in sidebar--> GroupChat
//
// Group history lives in ChatMessageStore under _groupHistory; group
// lifecycle + ws broadcast lives in GroupChatManager.

using Microsoft.Xna.Framework;
using Microsoft.Xna.Framework.Graphics;
using Microsoft.Xna.Framework.Input;
using StardewValley;
using StardewValley.Menus;
using System;
using System.Collections.Generic;
using System.Linq;

namespace SmartNPC.Bridge
{
    internal enum ChatPanelMode
    {
        SingleNpc,
        NpcPicker,
        GroupChat,
    }

    internal sealed class ChatPanel : IClickableMenu
    {
        private const int PanelWidth = 920;
        private const int PanelHeight = 620;
        private const int SidebarWidth = 160;
        private const int HeaderHeight = 50;
        private const int InputHeight = 52;
        private const int Padding = 12;
        private const int PortraitSlotSize = 84;
        private const int PortraitDrawSize = 64;
        private const int MessageSpacing = 10;
        private const int BubblePadding = 8;
        private const int MaxBubbleWidth = 500;

        // Sidebar entry heights for group rows + picker rows + bottom btn.
        private const int GroupRowHeight = 44;
        private const int PickerRowHeight = 36;
        private const int BottomButtonHeight = 40;
        private const int BottomButtonMargin = 8;
        private const int SidebarSectionGap = 6;

        private static readonly Color SidebarBg = new(38, 38, 50);
        private static readonly Color SidebarSelected = new(60, 60, 82);
        private static readonly Color SidebarHover = new(50, 50, 65);
        private static readonly Color HeaderBg = new(250, 248, 245);
        private static readonly Color MessageAreaBg = new(245, 243, 240);
        private static readonly Color PlayerBubble = new(55, 130, 210);
        private static readonly Color NpcBubble = new(255, 255, 255);
        private static readonly Color InputAreaBg = new(252, 250, 248);
        private static readonly Color UnreadDot = new(230, 60, 60);
        private static readonly Color WoodBorder = new(110, 70, 35);
        private static readonly Color GroupAccent = new(120, 90, 200);
        private static readonly Color ConfirmBtn = new(60, 180, 100);
        private static readonly Color ConfirmBtnDisabled = new(110, 110, 120);
        private static readonly Color DissolveBtn = new(210, 80, 70);
        private static readonly Color PlusBtn = new(80, 120, 200);

        // Deterministic palette used to color NPC speaker labels in group
        // chat; indexed by stable hash of the name.
        private static readonly Color[] SpeakerPalette =
        {
            new(55, 130, 210),   // blue
            new(200, 90, 140),   // pink
            new(60, 160, 100),   // green
            new(220, 140, 50),   // orange
            new(120, 90, 200),   // purple
            new(30, 150, 170),   // teal
            new(180, 70, 70),    // red
        };

        private readonly ChatMessageStore _store;
        private readonly GroupChatManager? _groupChat;
        private readonly Action<string, string> _onSend;
        private readonly List<string> _npcList;
        private readonly TextBox _textBox;

        // Mode state.
        private ChatPanelMode _mode = ChatPanelMode.SingleNpc;
        private string _selectedNpc;
        private string? _activeGroupId;
        private readonly HashSet<string> _pickerSelection = new();

        // Click-region caches rebuilt every frame.
        private readonly List<(Rectangle rect, string groupId)> _groupRowRects = new();
        private readonly List<(Rectangle rect, string npcName)> _npcRowRects = new();
        private Rectangle _bottomBtnRect;
        private Rectangle _headerActionBtnRect;

        private int _scrollOffset;

        public static bool IsOpen { get; private set; }

        public ChatPanel(
            ChatMessageStore store,
            Action<string, string> onSend,
            string? initialNpc = null,
            GroupChatManager? groupChat = null)
            : base(
                (Game1.uiViewport.Width - PanelWidth) / 2,
                (Game1.uiViewport.Height - PanelHeight) / 2,
                PanelWidth, PanelHeight, showUpperRightCloseButton: true)
        {
            _store = store;
            _groupChat = groupChat;
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
                _mode = ChatPanelMode.SingleNpc;
                _selectedNpc = npcName;
                _activeGroupId = null;
                _scrollOffset = 0;
                _store.MarkRead(npcName);
            }
        }

        // ── draw ────────────────────────────────────────────────────────

        public override void draw(SpriteBatch b)
        {
            _groupRowRects.Clear();
            _npcRowRects.Clear();

            // Dim game background.
            b.Draw(Game1.fadeToBlackRect, Game1.graphics.GraphicsDevice.Viewport.Bounds, Color.Black * 0.5f);

            // Main panel background.
            b.Draw(Game1.fadeToBlackRect,
                new Rectangle(xPositionOnScreen, yPositionOnScreen, width, height),
                MessageAreaBg);

            DrawBorder(b, xPositionOnScreen, yPositionOnScreen, width, height, 4, WoodBorder);

            DrawSidebar(b);
            DrawHeader(b);
            DrawMessages(b);
            DrawInputArea(b);

            base.draw(b);
            drawMouse(b);
        }

        private void DrawSidebar(SpriteBatch b)
        {
            Rectangle sidebar = new(xPositionOnScreen, yPositionOnScreen, SidebarWidth, height);
            b.Draw(Game1.fadeToBlackRect, sidebar, SidebarBg);

            // Right border of sidebar.
            b.Draw(Game1.fadeToBlackRect,
                new Rectangle(xPositionOnScreen + SidebarWidth - 1, yPositionOnScreen, 1, height),
                new Color(55, 55, 70));

            if (_mode == ChatPanelMode.NpcPicker)
                DrawSidebarPicker(b);
            else
                DrawSidebarNormal(b);
        }

        private void DrawSidebarNormal(SpriteBatch b)
        {
            int mouseX = Game1.getMouseX();
            int mouseY = Game1.getMouseY();

            int y = yPositionOnScreen + Padding;

            // Active groups section (top).
            var sessions = _groupChat?.GetActiveSessions() ?? new List<GroupChatInfo>();
            if (sessions.Count > 0)
            {
                DrawSidebarLabel(b, "群聊", y - 2);
                y += 18;

                foreach (var session in sessions)
                {
                    Rectangle row = new(xPositionOnScreen + 2, y, SidebarWidth - 4, GroupRowHeight);
                    bool selected = _mode == ChatPanelMode.GroupChat && _activeGroupId == session.Id;
                    bool hover = row.Contains(mouseX, mouseY);

                    if (selected)
                    {
                        b.Draw(Game1.fadeToBlackRect, row, SidebarSelected);
                        b.Draw(Game1.fadeToBlackRect,
                            new Rectangle(xPositionOnScreen, y, 3, GroupRowHeight),
                            GroupAccent);
                    }
                    else if (hover)
                    {
                        b.Draw(Game1.fadeToBlackRect, row, SidebarHover);
                    }

                    // Group icon (small rounded-ish square).
                    Rectangle icon = new(row.X + 6, row.Y + (GroupRowHeight - 28) / 2, 28, 28);
                    b.Draw(Game1.fadeToBlackRect, icon, GroupAccent);
                    string initials = session.Participants.Count.ToString();
                    Vector2 initSize = Game1.smallFont.MeasureString(initials);
                    b.DrawString(Game1.smallFont, initials,
                        new Vector2(icon.X + (icon.Width - initSize.X) / 2,
                                    icon.Y + (icon.Height - initSize.Y) / 2),
                        Color.White);

                    // Participant abbreviated label.
                    string label = ShortenParticipants(session.Participants, 14);
                    b.DrawString(Game1.smallFont, label,
                        new Vector2(icon.Right + 6, row.Y + (GroupRowHeight - Game1.smallFont.LineSpacing) / 2),
                        Color.White);

                    // Unread dot.
                    if (_store.HasGroupUnread(session.Id))
                    {
                        b.Draw(Game1.fadeToBlackRect,
                            new Rectangle(row.Right - 14, row.Y + 6, 8, 8),
                            UnreadDot);
                    }

                    _groupRowRects.Add((row, session.Id));
                    y += GroupRowHeight + 2;
                }
                y += SidebarSectionGap;
            }

            // NPC portrait list.
            DrawSidebarLabel(b, "NPC", y - 2);
            y += 18;

            int bottomLimit = yPositionOnScreen + height - BottomButtonHeight - BottomButtonMargin * 2;
            foreach (string npcName in _npcList)
            {
                if (y + PortraitSlotSize > bottomLimit) break;

                int slotX = xPositionOnScreen + (SidebarWidth - PortraitDrawSize) / 2;
                Rectangle slotRect = new(xPositionOnScreen, y, SidebarWidth, PortraitSlotSize);

                bool selected = _mode == ChatPanelMode.SingleNpc && npcName == _selectedNpc;
                bool hover = slotRect.Contains(mouseX, mouseY);

                if (selected)
                {
                    b.Draw(Game1.fadeToBlackRect, slotRect, SidebarSelected);
                    b.Draw(Game1.fadeToBlackRect,
                        new Rectangle(xPositionOnScreen, y, 3, PortraitSlotSize),
                        new Color(80, 180, 120));
                }
                else if (hover)
                {
                    b.Draw(Game1.fadeToBlackRect, slotRect, SidebarHover);
                }

                Rectangle destRect = new(slotX, y + 6, PortraitDrawSize, PortraitDrawSize);
                DrawPortrait(b, npcName, destRect);

                if (_store.HasUnread(npcName))
                {
                    b.Draw(Game1.fadeToBlackRect,
                        new Rectangle(destRect.Right - 6, destRect.Top - 2, 10, 10),
                        UnreadDot);
                }

                _npcRowRects.Add((slotRect, npcName));
                y += PortraitSlotSize;
            }

            // Bottom "+群聊" button.
            DrawBottomButton(b, "+ 群聊", PlusBtn, enabled: _groupChat != null);
        }

        private void DrawSidebarPicker(SpriteBatch b)
        {
            int mouseX = Game1.getMouseX();
            int mouseY = Game1.getMouseY();

            int y = yPositionOnScreen + Padding;
            DrawSidebarLabel(b, "选择 NPC (≥2)", y - 2);
            y += 20;

            int bottomLimit = yPositionOnScreen + height - BottomButtonHeight - BottomButtonMargin * 2;
            foreach (string npcName in _npcList)
            {
                if (y + PickerRowHeight > bottomLimit) break;

                Rectangle row = new(xPositionOnScreen + 2, y, SidebarWidth - 4, PickerRowHeight);
                bool checkedState = _pickerSelection.Contains(npcName);
                bool hover = row.Contains(mouseX, mouseY);

                if (hover) b.Draw(Game1.fadeToBlackRect, row, SidebarHover);

                // Checkbox.
                Rectangle box = new(row.X + 8, row.Y + (PickerRowHeight - 18) / 2, 18, 18);
                b.Draw(Game1.fadeToBlackRect, box, Color.White);
                DrawBorder(b, box.X, box.Y, box.Width, box.Height, 1, new Color(60, 60, 70));
                if (checkedState)
                {
                    Rectangle inner = new(box.X + 3, box.Y + 3, box.Width - 6, box.Height - 6);
                    b.Draw(Game1.fadeToBlackRect, inner, new Color(60, 180, 100));
                }

                // NPC name.
                NPC? npc = Game1.getCharacterFromName(npcName);
                string display = npc?.displayName ?? npcName;
                b.DrawString(Game1.smallFont, display,
                    new Vector2(box.Right + 8, row.Y + (PickerRowHeight - Game1.smallFont.LineSpacing) / 2),
                    Color.White);

                _npcRowRects.Add((row, npcName));
                y += PickerRowHeight + 2;
            }

            // Bottom "确认创建" button.
            string label = $"确认创建 ({_pickerSelection.Count})";
            bool canConfirm = _pickerSelection.Count >= 2 && _groupChat != null;
            DrawBottomButton(b, label, canConfirm ? ConfirmBtn : ConfirmBtnDisabled, enabled: canConfirm);
        }

        private void DrawBottomButton(SpriteBatch b, string label, Color fill, bool enabled)
        {
            int btnX = xPositionOnScreen + BottomButtonMargin;
            int btnY = yPositionOnScreen + height - BottomButtonHeight - BottomButtonMargin;
            int btnW = SidebarWidth - BottomButtonMargin * 2;
            Rectangle r = new(btnX, btnY, btnW, BottomButtonHeight);
            _bottomBtnRect = r;

            bool hover = enabled && r.Contains(Game1.getMouseX(), Game1.getMouseY());
            Color c = hover ? new Color(Math.Max(0, fill.R - 20), Math.Max(0, fill.G - 20), Math.Max(0, fill.B - 20)) : fill;
            b.Draw(Game1.fadeToBlackRect, r, c);

            Vector2 sz = Game1.smallFont.MeasureString(label);
            b.DrawString(Game1.smallFont, label,
                new Vector2(r.X + (r.Width - sz.X) / 2, r.Y + (r.Height - sz.Y) / 2),
                Color.White);
        }

        private void DrawSidebarLabel(SpriteBatch b, string text, int y)
        {
            b.DrawString(Game1.smallFont, text,
                new Vector2(xPositionOnScreen + 10, y),
                new Color(160, 160, 180));
        }

        // ── header ──────────────────────────────────────────────────────

        private void DrawHeader(SpriteBatch b)
        {
            int headerX = xPositionOnScreen + SidebarWidth;
            Rectangle headerRect = new(headerX, yPositionOnScreen, width - SidebarWidth, HeaderHeight);
            b.Draw(Game1.fadeToBlackRect, headerRect, HeaderBg);

            b.Draw(Game1.fadeToBlackRect,
                new Rectangle(headerX, yPositionOnScreen + HeaderHeight - 1, width - SidebarWidth, 1),
                new Color(220, 215, 210));

            _headerActionBtnRect = Rectangle.Empty;

            switch (_mode)
            {
                case ChatPanelMode.SingleNpc:
                    DrawHeaderSingle(b, headerX);
                    break;
                case ChatPanelMode.NpcPicker:
                    DrawHeaderPicker(b, headerX);
                    break;
                case ChatPanelMode.GroupChat:
                    DrawHeaderGroup(b, headerX);
                    break;
            }
        }

        private void DrawHeaderSingle(SpriteBatch b, int headerX)
        {
            if (string.IsNullOrEmpty(_selectedNpc)) return;
            NPC? npc = Game1.getCharacterFromName(_selectedNpc);
            string displayName = npc?.displayName ?? _selectedNpc;
            Vector2 nameSize = Game1.dialogueFont.MeasureString(displayName);
            b.DrawString(Game1.dialogueFont, displayName,
                new Vector2(headerX + Padding + 4, yPositionOnScreen + (HeaderHeight - nameSize.Y) / 2),
                new Color(50, 50, 55));
        }

        private void DrawHeaderPicker(SpriteBatch b, int headerX)
        {
            string label = "新建群聊";
            Vector2 sz = Game1.dialogueFont.MeasureString(label);
            b.DrawString(Game1.dialogueFont, label,
                new Vector2(headerX + Padding + 4, yPositionOnScreen + (HeaderHeight - sz.Y) / 2),
                new Color(50, 50, 55));

            // "取消" button (right side).
            string cancel = "取消";
            Vector2 csz = Game1.smallFont.MeasureString(cancel);
            int btnW = (int)csz.X + 24;
            int btnH = HeaderHeight - 14;
            int btnX = xPositionOnScreen + width - Padding - btnW - 4;
            int btnY = yPositionOnScreen + (HeaderHeight - btnH) / 2;
            Rectangle r = new(btnX, btnY, btnW, btnH);
            _headerActionBtnRect = r;
            bool hover = r.Contains(Game1.getMouseX(), Game1.getMouseY());
            b.Draw(Game1.fadeToBlackRect, r, hover ? new Color(150, 150, 160) : new Color(180, 180, 190));
            b.DrawString(Game1.smallFont, cancel,
                new Vector2(r.X + (r.Width - csz.X) / 2, r.Y + (r.Height - csz.Y) / 2),
                Color.White);
        }

        private void DrawHeaderGroup(SpriteBatch b, int headerX)
        {
            var session = _activeGroupId == null ? null : _groupChat?.GetSession(_activeGroupId);
            string title = session != null
                ? $"群聊 ({session.Participants.Count}人): {ShortenParticipants(session.Participants, 28)}"
                : "群聊 (已解散)";
            Vector2 sz = Game1.dialogueFont.MeasureString(title);
            b.DrawString(Game1.dialogueFont, title,
                new Vector2(headerX + Padding + 4, yPositionOnScreen + (HeaderHeight - sz.Y) / 2),
                new Color(50, 50, 55));

            // "解散" button (right side).
            string dissolve = "解散";
            Vector2 dsz = Game1.smallFont.MeasureString(dissolve);
            int btnW = (int)dsz.X + 24;
            int btnH = HeaderHeight - 14;
            int btnX = xPositionOnScreen + width - Padding - btnW - 4;
            int btnY = yPositionOnScreen + (HeaderHeight - btnH) / 2;
            Rectangle r = new(btnX, btnY, btnW, btnH);
            _headerActionBtnRect = r;
            bool hover = r.Contains(Game1.getMouseX(), Game1.getMouseY());
            b.Draw(Game1.fadeToBlackRect, r, hover ? new Color(190, 60, 50) : DissolveBtn);
            b.DrawString(Game1.smallFont, dissolve,
                new Vector2(r.X + (r.Width - dsz.X) / 2, r.Y + (r.Height - dsz.Y) / 2),
                Color.White);
        }

        // ── messages ────────────────────────────────────────────────────

        private void DrawMessages(SpriteBatch b)
        {
            int msgX = xPositionOnScreen + SidebarWidth;
            int msgTop = yPositionOnScreen + HeaderHeight;
            int msgBottom = yPositionOnScreen + height - InputHeight - Padding;
            int msgWidth = width - SidebarWidth;

            b.Draw(Game1.fadeToBlackRect,
                new Rectangle(msgX, msgTop, msgWidth, msgBottom - msgTop),
                MessageAreaBg);

            if (_mode == ChatPanelMode.NpcPicker)
            {
                string hint = _pickerSelection.Count == 0
                    ? "在左侧勾选至少 2 位 NPC 来创建群聊。"
                    : $"已选 {_pickerSelection.Count} 位：{string.Join("，", _pickerSelection)}";
                Vector2 hs = Game1.smallFont.MeasureString(hint);
                b.DrawString(Game1.smallFont, hint,
                    new Vector2(msgX + (msgWidth - hs.X) / 2, msgTop + 80),
                    Color.Gray);
                return;
            }

            List<ChatMessage> messages;
            if (_mode == ChatPanelMode.GroupChat)
            {
                if (string.IsNullOrEmpty(_activeGroupId)) return;
                messages = _store.GetGroupHistory(_activeGroupId);
            }
            else
            {
                if (string.IsNullOrEmpty(_selectedNpc)) return;
                messages = _store.GetHistory(_selectedNpc);
            }

            if (messages.Count == 0)
            {
                string hint = "还没有消息，开始聊天吧...";
                Vector2 hintSize = Game1.smallFont.MeasureString(hint);
                b.DrawString(Game1.smallFont, hint,
                    new Vector2(msgX + (msgWidth - hintSize.X) / 2, msgTop + 60),
                    Color.Gray);
                return;
            }

            int y = msgBottom - Padding;
            int endIdx = Math.Max(0, messages.Count - _scrollOffset);
            for (int i = endIdx - 1; i >= 0 && y > msgTop + Padding; i--)
            {
                var msg = messages[i];
                string text = Game1.parseText(msg.Text, Game1.smallFont, MaxBubbleWidth - BubblePadding * 2);
                Vector2 textSize = Game1.smallFont.MeasureString(text);

                int bubbleW = (int)textSize.X + BubblePadding * 2;
                int bubbleH = (int)textSize.Y + BubblePadding * 2;

                // In group mode reserve an extra line above the bubble for
                // the [Speaker] label so NPC utterances can be disambiguated.
                bool showSpeaker = _mode == ChatPanelMode.GroupChat && !msg.IsPlayer;
                int speakerH = showSpeaker ? Game1.smallFont.LineSpacing + 2 : 0;

                y -= bubbleH + speakerH + MessageSpacing;
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

                if (showSpeaker)
                {
                    NPC? npc = Game1.getCharacterFromName(msg.Speaker);
                    string speaker = npc?.displayName ?? msg.Speaker;
                    Color speakerColor = SpeakerColor(msg.Speaker);
                    b.DrawString(Game1.smallFont, speaker,
                        new Vector2(bubbleX, y), speakerColor);
                }

                Rectangle bubbleRect = new(bubbleX, y + speakerH, bubbleW, bubbleH);
                b.Draw(Game1.fadeToBlackRect, bubbleRect, bubbleColor);
                b.Draw(Game1.fadeToBlackRect,
                    new Rectangle(bubbleX + 1, y + speakerH + bubbleH, bubbleW - 2, 1),
                    Color.Black * 0.1f);

                b.DrawString(Game1.smallFont, text,
                    new Vector2(bubbleX + BubblePadding, y + speakerH + BubblePadding), textColor);
            }

            if (messages.Count > 0 && messages[^1].IsPlayer)
            {
                string typing = _mode == ChatPanelMode.GroupChat
                    ? "NPC 正在回复..."
                    : "对方正在输入...";
                b.DrawString(Game1.smallFont, typing,
                    new Vector2(msgX + Padding + 8, msgBottom - 20), Color.Gray * 0.7f);
            }
        }

        // ── input ───────────────────────────────────────────────────────

        private void DrawInputArea(SpriteBatch b)
        {
            int inputAreaX = xPositionOnScreen + SidebarWidth;
            int inputAreaY = yPositionOnScreen + height - InputHeight - Padding;
            int inputAreaW = width - SidebarWidth;

            b.Draw(Game1.fadeToBlackRect,
                new Rectangle(inputAreaX, inputAreaY - 4, inputAreaW, InputHeight + Padding + 4),
                InputAreaBg);

            b.Draw(Game1.fadeToBlackRect,
                new Rectangle(inputAreaX, inputAreaY - 4, inputAreaW, 1),
                new Color(220, 215, 210));

            // In picker mode the input box is hidden — prompt instead.
            if (_mode == ChatPanelMode.NpcPicker)
            {
                string hint = "请选择群聊成员，然后点击左下角 \"确认创建\"";
                b.DrawString(Game1.smallFont, hint,
                    new Vector2(inputAreaX + Padding + 8, inputAreaY + 16),
                    Color.Gray);
                return;
            }

            _textBox.Draw(b);

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

        // ── input handling ──────────────────────────────────────────────

        public override void receiveLeftClick(int x, int y, bool playSound = true)
        {
            base.receiveLeftClick(x, y, playSound);

            // Header action button (cancel-picker / dissolve-group).
            if (_headerActionBtnRect.Contains(x, y))
            {
                if (_mode == ChatPanelMode.NpcPicker)
                {
                    _pickerSelection.Clear();
                    _mode = ChatPanelMode.SingleNpc;
                    Game1.playSound("smallSelect");
                }
                else if (_mode == ChatPanelMode.GroupChat && _activeGroupId != null && _groupChat != null)
                {
                    _groupChat.CloseGroup(_activeGroupId);
                    _activeGroupId = null;
                    _mode = ChatPanelMode.SingleNpc;
                    Game1.playSound("smallSelect");
                }
                return;
            }

            Rectangle sidebar = new(xPositionOnScreen, yPositionOnScreen, SidebarWidth, height);
            if (sidebar.Contains(x, y))
            {
                // Bottom button.
                if (_bottomBtnRect.Contains(x, y))
                {
                    OnBottomButtonClick();
                    return;
                }

                // Group rows (always first in the sidebar when visible).
                foreach (var (rect, gid) in _groupRowRects)
                {
                    if (rect.Contains(x, y))
                    {
                        _mode = ChatPanelMode.GroupChat;
                        _activeGroupId = gid;
                        _scrollOffset = 0;
                        _store.MarkGroupRead(gid);
                        Game1.playSound("smallSelect");
                        return;
                    }
                }

                // NPC rows (portrait list or picker checkboxes).
                foreach (var (rect, npc) in _npcRowRects)
                {
                    if (rect.Contains(x, y))
                    {
                        OnNpcRowClick(npc);
                        return;
                    }
                }
                return;
            }

            if (_mode != ChatPanelMode.NpcPicker && GetSendButtonRect().Contains(x, y))
            {
                SubmitMessage();
                return;
            }

            _textBox.Update();
        }

        private void OnBottomButtonClick()
        {
            if (_groupChat == null)
            {
                Game1.playSound("cancel");
                return;
            }

            if (_mode == ChatPanelMode.NpcPicker)
            {
                if (_pickerSelection.Count < 2)
                {
                    Game1.playSound("cancel");
                    return;
                }
                var picked = _pickerSelection.OrderBy(n => n).ToList();
                string gid = _groupChat.CreateGroup(picked);
                _pickerSelection.Clear();
                _activeGroupId = gid;
                _mode = ChatPanelMode.GroupChat;
                _scrollOffset = 0;
                Game1.playSound("bigSelect");
                return;
            }

            // SingleNpc or GroupChat → enter picker.
            _mode = ChatPanelMode.NpcPicker;
            _pickerSelection.Clear();
            _scrollOffset = 0;
            Game1.playSound("smallSelect");
        }

        private void OnNpcRowClick(string npc)
        {
            if (_mode == ChatPanelMode.NpcPicker)
            {
                if (!_pickerSelection.Remove(npc))
                    _pickerSelection.Add(npc);
                Game1.playSound("smallSelect");
                return;
            }

            _mode = ChatPanelMode.SingleNpc;
            _selectedNpc = npc;
            _activeGroupId = null;
            _scrollOffset = 0;
            _store.MarkRead(npc);
            Game1.playSound("smallSelect");
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
            List<ChatMessage> msgs;
            if (_mode == ChatPanelMode.GroupChat && _activeGroupId != null)
                msgs = _store.GetGroupHistory(_activeGroupId);
            else if (_mode == ChatPanelMode.SingleNpc)
                msgs = _store.GetHistory(_selectedNpc);
            else
                return;

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
            if (string.IsNullOrEmpty(text)) return;

            if (_mode == ChatPanelMode.GroupChat && _activeGroupId != null && _groupChat != null)
            {
                _store.AddGroupMessage(_activeGroupId, "玩家", text, isPlayer: true);
                _groupChat.SendMessage(_activeGroupId, text);
                _textBox.Text = "";
                _scrollOffset = 0;
                return;
            }

            if (_mode == ChatPanelMode.SingleNpc && !string.IsNullOrEmpty(_selectedNpc))
            {
                _store.Add(_selectedNpc, Game1.player.Name, text, isPlayer: true);
                _onSend(_selectedNpc, text);
                _textBox.Text = "";
                _scrollOffset = 0;
            }
        }

        private void OnTextBoxEnter(TextBox sender)
        {
            SubmitMessage();
        }

        // ── helpers ─────────────────────────────────────────────────────

        private static Color SpeakerColor(string speaker)
        {
            if (string.IsNullOrEmpty(speaker)) return SpeakerPalette[0];
            int h = 0;
            foreach (char c in speaker) h = unchecked(h * 31 + c);
            int idx = (h & 0x7fffffff) % SpeakerPalette.Length;
            return SpeakerPalette[idx];
        }

        /// <summary>Joined participant list, truncated to maxChars with "…".</summary>
        private static string ShortenParticipants(List<string> list, int maxChars)
        {
            string joined = string.Join(",", list);
            if (joined.Length <= maxChars) return joined;
            return joined[..Math.Max(0, maxChars - 1)] + "…";
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
