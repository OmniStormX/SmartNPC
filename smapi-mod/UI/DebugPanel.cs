// DebugPanel — F3 opens an in-game menu listing every Agent-managed NPC
// with four action buttons per row (召唤 / 跟随 / 带路 / 停止). Drives the
// FollowSystem directly (same game thread as the UI draw / click events).
//
// The 带路 button opens a secondary destination picker menu (a small list
// of named targets: farm left edge, front of farmhouse, lake, town gate,
// greenhouse, barn, chicken coop).

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
    /// Pre-defined lead-to destinations. Tile coordinates are best-effort
    /// targets; the exact tile does not matter because FollowSystem paths
    /// iteratively.
    /// </summary>
    internal static class LeadDestinations
    {
        public sealed record Dest(string Label, string Map, int X, int Y);

        public static readonly IReadOnlyList<Dest> All = new Dest[]
        {
            new("农场左边",    "Farm",       13, 24),
            new("房子前面",    "Farm",       64, 15),
            new("湖边",        "Mountain",   68, 31),
            new("大门",        "Town",       45, 76),
            new("温室",        "Farm",       25, 13),
            new("畜棚",        "Farm",       18,  8),
            new("鸡舍",        "Farm",       10,  8),
        };
    }

    /// <summary>
    /// Main debug panel. Opened with F3.
    /// </summary>
    internal sealed class DebugPanel : IClickableMenu
    {
        private const int WindowWidth = 560;
        private const int RowHeight   = 64;
        private const int Padding     = 16;
        private const int TitleHeight = 48;
        private const int BtnWidth    = 88;
        private const int BtnHeight   = 44;
        private const int BtnSpacing  = 8;

        private readonly FollowSystem _follow;
        private readonly List<string> _npcNames;

        public DebugPanel(FollowSystem follow)
            : base(0, 0, WindowWidth, 0, showUpperRightCloseButton: true)
        {
            _follow = follow;
            _npcNames = AgentNpcRegistry.GetAll();
            _npcNames.Sort(StringComparer.OrdinalIgnoreCase);

            height = Padding * 2 + TitleHeight + Math.Max(1, _npcNames.Count) * RowHeight + Padding;
            xPositionOnScreen = (Game1.uiViewport.Width - width) / 2;
            yPositionOnScreen = (Game1.uiViewport.Height - height) / 2;

            initializeUpperRightCloseButton();
        }

        public override void draw(SpriteBatch b)
        {
            // Dim background.
            b.Draw(Game1.fadeToBlackRect, Game1.graphics.GraphicsDevice.Viewport.Bounds, Color.Black * 0.4f);

            drawTextureBox(b, xPositionOnScreen, yPositionOnScreen, width, height, Color.White);

            // Title.
            b.DrawString(Game1.dialogueFont, "SmartNPC 调试面板 (F3)",
                new Vector2(xPositionOnScreen + Padding, yPositionOnScreen + Padding),
                Color.DarkSlateGray);

            int rowY = yPositionOnScreen + Padding + TitleHeight;
            if (_npcNames.Count == 0)
            {
                b.DrawString(Game1.smallFont, "无 Agent 托管 NPC",
                    new Vector2(xPositionOnScreen + Padding + 8, rowY + 12), Color.Gray);
            }

            for (int i = 0; i < _npcNames.Count; i++)
            {
                string npcName = _npcNames[i];
                NPC? npc = Game1.getCharacterFromName(npcName);
                string displayName = npc?.displayName ?? npcName;

                // Row bg (even/odd alternate).
                Rectangle rowBg = new(xPositionOnScreen + Padding, rowY,
                                      width - Padding * 2, RowHeight - 4);
                if ((i & 1) == 0)
                    b.Draw(Game1.fadeToBlackRect, rowBg, Color.LightGray * 0.2f);

                // Name + mode label.
                NpcBehaviorMode mode = _follow.GetMode(npcName);
                string modeLabel = mode.ToString().ToLowerInvariant();
                b.DrawString(Game1.smallFont, displayName,
                    new Vector2(xPositionOnScreen + Padding + 8, rowY + 8), Color.Black);
                b.DrawString(Game1.smallFont, $"[{modeLabel}]",
                    new Vector2(xPositionOnScreen + Padding + 8, rowY + 28),
                    mode == NpcBehaviorMode.Idle ? Color.Gray : new Color(50, 120, 50));

                // Four buttons aligned to the right edge.
                DrawButton(b, RowRect(rowY, 0), "召唤");
                DrawButton(b, RowRect(rowY, 1), "跟随");
                DrawButton(b, RowRect(rowY, 2), "带路");
                DrawButton(b, RowRect(rowY, 3), "停止");

                rowY += RowHeight;
            }

            base.draw(b);
            drawMouse(b);
        }

        public override void receiveLeftClick(int x, int y, bool playSound = true)
        {
            base.receiveLeftClick(x, y, playSound);

            int rowY = yPositionOnScreen + Padding + TitleHeight;
            for (int i = 0; i < _npcNames.Count; i++)
            {
                string npcName = _npcNames[i];

                if (RowRect(rowY, 0).Contains(x, y))      // 召唤
                {
                    _follow.Summon(npcName);
                    Game1.playSound("smallSelect");
                    return;
                }
                if (RowRect(rowY, 1).Contains(x, y))      // 跟随
                {
                    _follow.StartFollow(npcName);
                    Game1.playSound("smallSelect");
                    return;
                }
                if (RowRect(rowY, 2).Contains(x, y))      // 带路
                {
                    Game1.activeClickableMenu = new LeadDestinationPicker(_follow, npcName);
                    Game1.playSound("smallSelect");
                    return;
                }
                if (RowRect(rowY, 3).Contains(x, y))      // 停止
                {
                    _follow.StopFollow(npcName);
                    Game1.playSound("smallSelect");
                    return;
                }
                rowY += RowHeight;
            }
        }

        public override void receiveKeyPress(Keys key)
        {
            if (key == Keys.Escape || key == Keys.F3)
                exitThisMenu();
        }

        // Layout helper: rightmost button (index=0 = leftmost of the four).
        private Rectangle RowRect(int rowY, int btnIndex)
        {
            int groupRight = xPositionOnScreen + width - Padding - 8;
            int groupLeft  = groupRight - 4 * BtnWidth - 3 * BtnSpacing;
            int x = groupLeft + btnIndex * (BtnWidth + BtnSpacing);
            int y = rowY + (RowHeight - BtnHeight) / 2 - 2;
            return new Rectangle(x, y, BtnWidth, BtnHeight);
        }

        private static void DrawButton(SpriteBatch b, Rectangle rect, string label)
        {
            bool hover = rect.Contains(Game1.getMouseX(), Game1.getMouseY());
            Color fill = hover ? new Color(255, 240, 200) : new Color(240, 220, 170);
            b.Draw(Game1.fadeToBlackRect, rect, fill);
            // Border
            b.Draw(Game1.fadeToBlackRect, new Rectangle(rect.X, rect.Y, rect.Width, 2), Color.DarkGray);
            b.Draw(Game1.fadeToBlackRect, new Rectangle(rect.X, rect.Bottom - 2, rect.Width, 2), Color.DarkGray);
            b.Draw(Game1.fadeToBlackRect, new Rectangle(rect.X, rect.Y, 2, rect.Height), Color.DarkGray);
            b.Draw(Game1.fadeToBlackRect, new Rectangle(rect.Right - 2, rect.Y, 2, rect.Height), Color.DarkGray);

            Vector2 size = Game1.smallFont.MeasureString(label);
            b.DrawString(Game1.smallFont, label,
                new Vector2(rect.X + (rect.Width - size.X) / 2,
                            rect.Y + (rect.Height - size.Y) / 2),
                Color.Black);
        }
    }

    /// <summary>
    /// Secondary menu: pick a pre-defined lead destination.
    /// </summary>
    internal sealed class LeadDestinationPicker : IClickableMenu
    {
        private const int WindowWidth = 360;
        private const int RowHeight   = 48;
        private const int Padding     = 16;
        private const int TitleHeight = 40;

        private readonly FollowSystem _follow;
        private readonly string _npcName;

        public LeadDestinationPicker(FollowSystem follow, string npcName)
            : base(0, 0, WindowWidth, 0, showUpperRightCloseButton: true)
        {
            _follow = follow;
            _npcName = npcName;

            height = Padding * 2 + TitleHeight + LeadDestinations.All.Count * RowHeight + Padding;
            xPositionOnScreen = (Game1.uiViewport.Width - width) / 2;
            yPositionOnScreen = (Game1.uiViewport.Height - height) / 2;

            initializeUpperRightCloseButton();
        }

        public override void draw(SpriteBatch b)
        {
            b.Draw(Game1.fadeToBlackRect, Game1.graphics.GraphicsDevice.Viewport.Bounds, Color.Black * 0.5f);
            drawTextureBox(b, xPositionOnScreen, yPositionOnScreen, width, height, Color.White);

            NPC? npc = Game1.getCharacterFromName(_npcName);
            string displayName = npc?.displayName ?? _npcName;
            b.DrawString(Game1.dialogueFont, $"带路：{displayName}",
                new Vector2(xPositionOnScreen + Padding, yPositionOnScreen + Padding),
                Color.DarkSlateGray);

            int rowY = yPositionOnScreen + Padding + TitleHeight;
            for (int i = 0; i < LeadDestinations.All.Count; i++)
            {
                var d = LeadDestinations.All[i];
                Rectangle rowRect = new(xPositionOnScreen + Padding, rowY,
                                        width - Padding * 2, RowHeight - 4);
                bool hover = rowRect.Contains(Game1.getMouseX(), Game1.getMouseY());
                b.Draw(Game1.fadeToBlackRect, rowRect,
                    hover ? new Color(255, 240, 200) : Color.LightGray * 0.2f);
                b.DrawString(Game1.smallFont, $"{d.Label}  ({d.Map} {d.X},{d.Y})",
                    new Vector2(rowRect.X + 8, rowRect.Y + 10), Color.Black);
                rowY += RowHeight;
            }

            base.draw(b);
            drawMouse(b);
        }

        public override void receiveLeftClick(int x, int y, bool playSound = true)
        {
            base.receiveLeftClick(x, y, playSound);

            int rowY = yPositionOnScreen + Padding + TitleHeight;
            for (int i = 0; i < LeadDestinations.All.Count; i++)
            {
                Rectangle rowRect = new(xPositionOnScreen + Padding, rowY,
                                        width - Padding * 2, RowHeight - 4);
                if (rowRect.Contains(x, y))
                {
                    var d = LeadDestinations.All[i];
                    _follow.LeadTo(_npcName, d.X, d.Y, d.Map);
                    Game1.playSound("smallSelect");
                    exitThisMenu(playSound: false);
                    return;
                }
                rowY += RowHeight;
            }
        }

        public override void receiveKeyPress(Keys key)
        {
            if (key == Keys.Escape)
                exitThisMenu();
        }
    }
}
