// ContactList — left-pane sub-component of ChatPanel.
//
// Shows every Agent-managed NPC as a QQ-style row with avatar, name, online
// status and unread badge. Sorted by most recent message timestamp.
//
// Supports two modes:
//   - Normal mode: clicking a row selects it for 1-on-1 chat.
//   - Multi-select mode: clicking rows toggles checkmarks for group creation.

using System;
using System.Collections.Generic;
using System.Linq;
using Microsoft.Xna.Framework;
using Microsoft.Xna.Framework.Graphics;
using StardewValley;

namespace SmartNPC.Bridge
{
    /// <summary>
    /// Left pane of the QQ-style chat panel. Lists Agent-managed NPCs with
    /// online state, last-message preview and unread badges.
    /// Includes "创建群聊" / "解散群聊" buttons.
    /// </summary>
    internal sealed class ContactList
    {
        public const int Width = 300;
        private const int RowHeight = 72;
        private const int HeaderHeight = 56;
        private const int FooterHeight = 48;
        private const int Padding = 12;

        private readonly ChatMessageStore _store;
        private readonly UnreadTracker _unread;
        private readonly Action<string> _onSelect;
        private readonly Action<List<string>>? _onCreateGroup;
        private readonly Action? _onDisbandGroup;

        private Rectangle _bounds;
        private string? _selected;
        private int _scrollOffset;
        private List<string> _orderedNpcs = new();

        // Multi-select state for group creation.
        private bool _multiSelectMode;
        private readonly HashSet<string> _checkedNpcs = new();

        // Footer button rectangles.
        private Rectangle _btnRect;

        public ContactList(
            ChatMessageStore store,
            UnreadTracker unread,
            Action<string> onSelect,
            Action<List<string>>? onCreateGroup = null,
            Action? onDisbandGroup = null)
        {
            _store = store;
            _unread = unread;
            _onSelect = onSelect;
            _onCreateGroup = onCreateGroup;
            _onDisbandGroup = onDisbandGroup;
        }

        public string? SelectedNpc => _selected;
        public bool IsMultiSelectMode => _multiSelectMode;

        /// <summary>Whether a group chat entry exists (active group).</summary>
        public bool HasActiveGroup => _store.GetHistory(GroupChatManager.GroupKey).Count > 0;

        public void SetBounds(Rectangle bounds)
        {
            _bounds = bounds;
            _btnRect = new Rectangle(
                bounds.X + Padding, bounds.Bottom - FooterHeight + 8,
                bounds.Width - Padding * 2, 32);
        }

        /// <summary>Refresh ordered NPC list from registry + history timestamps.</summary>
        public void Refresh()
        {
            var names = AgentNpcRegistry.GetAll();
            _orderedNpcs = names
                .OrderByDescending(n =>
                {
                    var hist = _store.GetHistory(n);
                    return hist.Count > 0 ? hist[^1].Time : DateTime.MinValue;
                })
                .ThenBy(n => n, StringComparer.OrdinalIgnoreCase)
                .ToList();

            // If a group chat is active, insert the group entry at the top.
            if (_store.GetHistory(GroupChatManager.GroupKey).Count > 0)
            {
                _orderedNpcs.Remove(GroupChatManager.GroupKey);
                _orderedNpcs.Insert(0, GroupChatManager.GroupKey);
            }

            if (_selected != null && !_orderedNpcs.Contains(_selected))
                _selected = null;
            if (_selected is null && _orderedNpcs.Count > 0)
            {
                _selected = _orderedNpcs[0];
                _onSelect(_selected);
            }
        }

        public void Select(string npcName)
        {
            if (string.IsNullOrEmpty(npcName)) return;
            if (!_orderedNpcs.Contains(npcName)) Refresh();
            _selected = npcName;
            _onSelect(npcName);
        }

        public IReadOnlyList<string> NpcNames => _orderedNpcs;

        public void Draw(SpriteBatch b)
        {
            // Pane background.
            b.Draw(Game1.fadeToBlackRect, _bounds, new Color(245, 240, 220));

            // Right-edge separator.
            b.Draw(Game1.fadeToBlackRect,
                new Rectangle(_bounds.Right - 2, _bounds.Y, 2, _bounds.Height),
                new Color(180, 160, 110));

            // Header.
            int headerY = _bounds.Y;
            b.Draw(Game1.fadeToBlackRect,
                new Rectangle(_bounds.X, headerY, _bounds.Width, HeaderHeight),
                new Color(100, 130, 180));

            string headerTitle = _multiSelectMode ? "选择群成员" : "联系人";
            b.DrawString(Game1.dialogueFont, headerTitle,
                new Vector2(_bounds.X + Padding, headerY + 12), Color.White);

            if (!_multiSelectMode)
            {
                string totalLabel = $"未读 {_unread.TotalUnread}";
                Vector2 totalSize = Game1.smallFont.MeasureString(totalLabel);
                b.DrawString(Game1.smallFont, totalLabel,
                    new Vector2(_bounds.Right - Padding - totalSize.X, headerY + 22),
                    Color.White);
            }
            else
            {
                string countLabel = $"已选 {_checkedNpcs.Count}";
                Vector2 countSize = Game1.smallFont.MeasureString(countLabel);
                b.DrawString(Game1.smallFont, countLabel,
                    new Vector2(_bounds.Right - Padding - countSize.X, headerY + 22),
                    Color.Yellow);
            }

            // Rows — clip to the list area.
            int listTop = headerY + HeaderHeight;
            int listBottom = _bounds.Bottom - FooterHeight;
            int rowsVisible = (listBottom - listTop) / RowHeight;

            Rectangle prevScissor = b.GraphicsDevice.ScissorRectangle;
            b.End();
            var rastState = new Microsoft.Xna.Framework.Graphics.RasterizerState { ScissorTestEnable = true };
            b.Begin(SpriteSortMode.Deferred, BlendState.AlphaBlend, SamplerState.PointClamp,
                null, rastState);
            b.GraphicsDevice.ScissorRectangle = new Rectangle(
                _bounds.X, listTop, _bounds.Width, listBottom - listTop);

            for (int i = 0; i < rowsVisible && i + _scrollOffset < _orderedNpcs.Count; i++)
            {
                int idx = i + _scrollOffset;
                string npcName = _orderedNpcs[idx];
                int rowY = listTop + i * RowHeight;
                DrawRow(b, npcName, rowY);
            }

            if (_orderedNpcs.Count == 0)
            {
                b.DrawString(Game1.smallFont, "暂无 Agent NPC",
                    new Vector2(_bounds.X + Padding, listTop + Padding), Color.Gray);
            }

            // Restore scissor.
            b.End();
            b.Begin(SpriteSortMode.Deferred, BlendState.AlphaBlend, SamplerState.PointClamp,
                null, null);
            b.GraphicsDevice.ScissorRectangle = prevScissor;

            // Footer button.
            DrawFooterButton(b);
        }

        private void DrawFooterButton(SpriteBatch b)
        {
            // Footer background.
            b.Draw(Game1.fadeToBlackRect,
                new Rectangle(_bounds.X, _bounds.Bottom - FooterHeight, _bounds.Width, FooterHeight),
                new Color(235, 230, 210));

            bool hover = _btnRect.Contains(Game1.getMouseX(), Game1.getMouseY());

            if (_multiSelectMode)
            {
                // Show "确认创建" and "取消" side by side.
                int halfW = (_btnRect.Width - 8) / 2;
                Rectangle confirmRect = new(_btnRect.X, _btnRect.Y, halfW, _btnRect.Height);
                Rectangle cancelRect = new(_btnRect.X + halfW + 8, _btnRect.Y, halfW, _btnRect.Height);

                bool hoverConfirm = confirmRect.Contains(Game1.getMouseX(), Game1.getMouseY());
                bool hoverCancel = cancelRect.Contains(Game1.getMouseX(), Game1.getMouseY());

                Color confirmColor = _checkedNpcs.Count >= 2
                    ? (hoverConfirm ? new Color(80, 180, 80) : new Color(60, 150, 60))
                    : new Color(150, 150, 150);
                b.Draw(Game1.fadeToBlackRect, confirmRect, confirmColor);
                string confirmText = $"确认({_checkedNpcs.Count})";
                Vector2 cs = Game1.smallFont.MeasureString(confirmText);
                b.DrawString(Game1.smallFont, confirmText,
                    new Vector2(confirmRect.X + (confirmRect.Width - cs.X) / 2, confirmRect.Y + 4),
                    Color.White);

                Color cancelColor = hoverCancel ? new Color(200, 100, 100) : new Color(160, 80, 80);
                b.Draw(Game1.fadeToBlackRect, cancelRect, cancelColor);
                string cancelText = "取消";
                Vector2 xs = Game1.smallFont.MeasureString(cancelText);
                b.DrawString(Game1.smallFont, cancelText,
                    new Vector2(cancelRect.X + (cancelRect.Width - xs.X) / 2, cancelRect.Y + 4),
                    Color.White);
            }
            else if (HasActiveGroup)
            {
                // Show "解散群聊" button.
                Color color = hover ? new Color(200, 80, 80) : new Color(170, 60, 60);
                b.Draw(Game1.fadeToBlackRect, _btnRect, color);
                string text = "解散群聊";
                Vector2 ts = Game1.smallFont.MeasureString(text);
                b.DrawString(Game1.smallFont, text,
                    new Vector2(_btnRect.X + (_btnRect.Width - ts.X) / 2, _btnRect.Y + 4),
                    Color.White);
            }
            else
            {
                // Show "创建群聊" button.
                Color color = hover ? new Color(80, 150, 200) : new Color(60, 120, 170);
                b.Draw(Game1.fadeToBlackRect, _btnRect, color);
                string text = "创建群聊";
                Vector2 ts = Game1.smallFont.MeasureString(text);
                b.DrawString(Game1.smallFont, text,
                    new Vector2(_btnRect.X + (_btnRect.Width - ts.X) / 2, _btnRect.Y + 4),
                    Color.White);
            }
        }

        private void DrawRow(SpriteBatch b, string npcName, int rowY)
        {
            bool isGroup = npcName == GroupChatManager.GroupKey;
            NPC? npc = isGroup ? null : Game1.getCharacterFromName(npcName);
            string displayName = isGroup ? GroupChatManager.GroupDisplayName
                : (npc?.displayName ?? npcName);

            Rectangle rowRect = new(_bounds.X, rowY, _bounds.Width - 2, RowHeight);
            bool hover = rowRect.Contains(Game1.getMouseX(), Game1.getMouseY());
            bool selected = npcName == _selected;

            if (selected && !_multiSelectMode)
                b.Draw(Game1.fadeToBlackRect, rowRect, new Color(255, 235, 180));
            else if (hover)
                b.Draw(Game1.fadeToBlackRect, rowRect, new Color(220, 220, 200) * 0.6f);

            // In multi-select mode, draw checkbox.
            if (_multiSelectMode && !isGroup)
            {
                bool isChecked = _checkedNpcs.Contains(npcName);
                Rectangle cbRect = new(rowRect.X + 8, rowRect.Y + 24, 24, 24);
                b.Draw(Game1.fadeToBlackRect, cbRect,
                    isChecked ? new Color(80, 180, 80) : new Color(200, 200, 200));
                if (isChecked)
                    b.DrawString(Game1.smallFont, "✓",
                        new Vector2(cbRect.X + 4, cbRect.Y + 0), Color.White);
            }

            // Avatar slot.
            int avatarOffset = _multiSelectMode && !isGroup ? 36 : 0;
            Rectangle avatarRect = new(rowRect.X + Padding + avatarOffset, rowRect.Y + 12, 48, 48);
            if (isGroup)
            {
                b.Draw(Game1.fadeToBlackRect, avatarRect, new Color(140, 100, 200));
                b.DrawString(Game1.smallFont, "群",
                    new Vector2(avatarRect.X + 14, avatarRect.Y + 12), Color.White);
            }
            else
            {
                DrawAvatar(b, npc, avatarRect);
            }

            // Online indicator dot.
            if (!_multiSelectMode)
            {
                b.Draw(Game1.fadeToBlackRect,
                    new Rectangle(avatarRect.Right - 12, avatarRect.Bottom - 12, 12, 12),
                    new Color(80, 200, 80));
            }

            // Name.
            int textX = avatarRect.Right + 10;
            b.DrawString(Game1.smallFont, displayName,
                new Vector2(textX, rowRect.Y + 12), Color.Black);

            // Last message preview (only in normal mode).
            if (!_multiSelectMode)
            {
                var hist = _store.GetHistory(npcName);
                if (hist.Count > 0)
                {
                    string preview = hist[^1].Text;
                    int nlIdx = preview.IndexOfAny(new[] { '\n', '\r' });
                    if (nlIdx >= 0) preview = preview[..nlIdx];
                    if (preview.Length > 10) preview = preview[..10] + "…";
                    string prefix = hist[^1].IsPlayer ? "我: " : "";
                    string previewText = prefix + preview;
                    int maxPreviewWidth = rowRect.Width - (textX - rowRect.X) - Padding - 30;
                    while (Game1.smallFont.MeasureString(previewText).X > maxPreviewWidth && previewText.Length > 4)
                        previewText = previewText[..^4] + "…";
                    b.DrawString(Game1.smallFont, previewText,
                        new Vector2(textX, rowRect.Y + 38), Color.DimGray);
                }
                else
                {
                    b.DrawString(Game1.smallFont, "（暂无消息）",
                        new Vector2(textX, rowRect.Y + 38), Color.LightGray);
                }

                // Unread badge.
                int unread = _unread.GetUnread(npcName);
                if (unread > 0)
                    DrawBadge(b, rowRect.Right - 30, rowRect.Y + 16, unread);
            }
        }

        private static void DrawAvatar(SpriteBatch b, NPC? npc, Rectangle dst)
        {
            b.Draw(Game1.fadeToBlackRect, dst, new Color(120, 160, 200));
            try
            {
                if (npc != null && npc.Sprite?.Texture != null)
                {
                    Rectangle src = new(0, 0, 16, 24);
                    b.Draw(npc.Sprite.Texture, dst, src, Color.White);
                    return;
                }
            }
            catch { /* fall through to placeholder */ }
        }

        private static void DrawBadge(SpriteBatch b, int x, int y, int count)
        {
            string text = count > 99 ? "99+" : count.ToString();
            Vector2 size = Game1.smallFont.MeasureString(text);
            int w = (int)System.MathF.Max(24, size.X + 12);
            int h = 22;
            Rectangle bg = new(x, y, w, h);
            b.Draw(Game1.fadeToBlackRect, bg, new Color(220, 60, 60));
            b.DrawString(Game1.smallFont, text,
                new Vector2(bg.X + (bg.Width - size.X) / 2, bg.Y + 2), Color.White);
        }

        public bool ReceiveLeftClick(int x, int y)
        {
            if (!_bounds.Contains(x, y)) return false;

            // Footer button click.
            if (_btnRect.Contains(x, y))
            {
                HandleFooterClick(x, y);
                return true;
            }

            int listTop = _bounds.Y + HeaderHeight;
            int listBottom = _bounds.Bottom - FooterHeight;
            int rowsVisible = (listBottom - listTop) / RowHeight;

            for (int i = 0; i < rowsVisible && i + _scrollOffset < _orderedNpcs.Count; i++)
            {
                Rectangle rowRect = new(_bounds.X, listTop + i * RowHeight,
                                        _bounds.Width - 2, RowHeight);
                if (rowRect.Contains(x, y))
                {
                    string npcName = _orderedNpcs[i + _scrollOffset];

                    if (_multiSelectMode)
                    {
                        // Toggle checkbox (skip group entry).
                        if (npcName != GroupChatManager.GroupKey)
                        {
                            if (_checkedNpcs.Contains(npcName))
                                _checkedNpcs.Remove(npcName);
                            else
                                _checkedNpcs.Add(npcName);
                            Game1.playSound("smallSelect");
                        }
                    }
                    else
                    {
                        _selected = npcName;
                        _onSelect(npcName);
                        Game1.playSound("smallSelect");
                    }
                    return true;
                }
            }
            return false;
        }

        private void HandleFooterClick(int x, int y)
        {
            if (_multiSelectMode)
            {
                int halfW = (_btnRect.Width - 8) / 2;
                Rectangle confirmRect = new(_btnRect.X, _btnRect.Y, halfW, _btnRect.Height);
                Rectangle cancelRect = new(_btnRect.X + halfW + 8, _btnRect.Y, halfW, _btnRect.Height);

                if (confirmRect.Contains(x, y) && _checkedNpcs.Count >= 2)
                {
                    // Confirm group creation.
                    _onCreateGroup?.Invoke(_checkedNpcs.ToList());
                    _multiSelectMode = false;
                    _checkedNpcs.Clear();
                    Game1.playSound("bigSelect");
                    Refresh();
                }
                else if (cancelRect.Contains(x, y))
                {
                    _multiSelectMode = false;
                    _checkedNpcs.Clear();
                    Game1.playSound("bigDeSelect");
                }
            }
            else if (HasActiveGroup)
            {
                // Disband group.
                _onDisbandGroup?.Invoke();
                Game1.playSound("trashcan");
                Refresh();
            }
            else
            {
                // Enter multi-select mode.
                _multiSelectMode = true;
                _checkedNpcs.Clear();
                Game1.playSound("smallSelect");
            }
        }

        public void ReceiveScrollWheelAction(int direction)
        {
            int listBottom = _bounds.Bottom - FooterHeight;
            int rowsVisible = (listBottom - _bounds.Y - HeaderHeight) / RowHeight;
            int maxOffset = System.Math.Max(0, _orderedNpcs.Count - rowsVisible);

            if (direction < 0 && _scrollOffset < maxOffset) _scrollOffset++;
            else if (direction > 0 && _scrollOffset > 0) _scrollOffset--;
        }
    }
}
