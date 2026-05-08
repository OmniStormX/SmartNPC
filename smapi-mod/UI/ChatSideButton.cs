using Microsoft.Xna.Framework;
using Microsoft.Xna.Framework.Graphics;
using StardewModdingAPI;
using StardewValley;
using System;

namespace SmartNPC.Bridge
{
    internal sealed class ChatSideButton
    {
        private const int ButtonW = 48;
        private const int ButtonH = 44;
        private const int LeftMargin = 8;
        private const int DotSize = 10;

        private readonly Action _onClick;
        private Rectangle _bounds;
        private bool _hasUnread;
        private float _pulseTimer;

        public ChatSideButton(Action onClick)
        {
            _onClick = onClick;
            UpdatePosition();
        }

        public void SetUnread(bool value) => _hasUnread = value;

        public void UpdatePosition()
        {
            _bounds = new Rectangle(
                LeftMargin,
                (Game1.uiViewport.Height - ButtonH) / 2,
                ButtonW, ButtonH);
        }

        public void Draw(SpriteBatch b)
        {
            bool hover = _bounds.Contains(Game1.getMouseX(), Game1.getMouseY());
            _pulseTimer += 0.05f;

            // Outer shadow (wood frame depth)
            b.Draw(Game1.fadeToBlackRect,
                new Rectangle(_bounds.X + 2, _bounds.Y + 2, _bounds.Width, _bounds.Height),
                new Color(60, 40, 20) * 0.5f);

            // Button body (warm parchment gradient)
            Color bodyColor = hover ? new Color(255, 240, 200) : new Color(235, 215, 170);
            b.Draw(Game1.fadeToBlackRect, _bounds, bodyColor);

            // Inner highlight (top edge, lighter)
            b.Draw(Game1.fadeToBlackRect,
                new Rectangle(_bounds.X + 3, _bounds.Y + 3, _bounds.Width - 6, 2),
                Color.White * 0.4f);

            // Wood border frame (3px chunky)
            Color frameColor = hover ? new Color(150, 95, 35) : new Color(120, 75, 30);
            // Top
            b.Draw(Game1.fadeToBlackRect,
                new Rectangle(_bounds.X, _bounds.Y, _bounds.Width, 3), frameColor);
            // Bottom
            b.Draw(Game1.fadeToBlackRect,
                new Rectangle(_bounds.X, _bounds.Bottom - 3, _bounds.Width, 3), frameColor);
            // Left
            b.Draw(Game1.fadeToBlackRect,
                new Rectangle(_bounds.X, _bounds.Y, 3, _bounds.Height), frameColor);
            // Right
            b.Draw(Game1.fadeToBlackRect,
                new Rectangle(_bounds.Right - 3, _bounds.Y, 3, _bounds.Height), frameColor);

            // Corner accents (darker)
            Color cornerColor = new Color(80, 50, 20);
            int cs = 5;
            b.Draw(Game1.fadeToBlackRect, new Rectangle(_bounds.X, _bounds.Y, cs, cs), cornerColor);
            b.Draw(Game1.fadeToBlackRect, new Rectangle(_bounds.Right - cs, _bounds.Y, cs, cs), cornerColor);
            b.Draw(Game1.fadeToBlackRect, new Rectangle(_bounds.X, _bounds.Bottom - cs, cs, cs), cornerColor);
            b.Draw(Game1.fadeToBlackRect, new Rectangle(_bounds.Right - cs, _bounds.Bottom - cs, cs, cs), cornerColor);

            // Speech bubble icon (pixel art style using primitives)
            DrawSpeechBubble(b, _bounds, hover);

            // Unread notification dot (pulsing)
            if (_hasUnread)
            {
                float pulse = 1.0f + (float)Math.Sin(_pulseTimer * 3f) * 0.15f;
                int dotW = (int)(DotSize * pulse);
                int dotH = (int)(DotSize * pulse);
                Rectangle dot = new(
                    _bounds.Right - dotW / 2 - 1,
                    _bounds.Top - dotH / 2 + 1,
                    dotW, dotH);
                // Dot shadow
                b.Draw(Game1.fadeToBlackRect,
                    new Rectangle(dot.X + 1, dot.Y + 1, dot.Width, dot.Height),
                    Color.Black * 0.3f);
                // Dot body (red)
                b.Draw(Game1.fadeToBlackRect, dot, new Color(230, 50, 50));
                // Dot highlight
                b.Draw(Game1.fadeToBlackRect,
                    new Rectangle(dot.X + 2, dot.Y + 1, 3, 2),
                    Color.White * 0.5f);
            }
        }

        private static void DrawSpeechBubble(SpriteBatch b, Rectangle bounds, bool hover)
        {
            // Draw a pixel-art speech bubble in the center of the button
            Color iconColor = hover ? new Color(55, 40, 20) : new Color(80, 55, 30);
            Color iconFill = hover ? new Color(255, 255, 240) : new Color(245, 240, 225);

            int cx = bounds.X + bounds.Width / 2;
            int cy = bounds.Y + bounds.Height / 2 - 2;

            // Bubble body (rounded rect: 22x14)
            int bw = 22, bh = 14;
            int bx = cx - bw / 2;
            int by = cy - bh / 2;

            // Fill
            b.Draw(Game1.fadeToBlackRect, new Rectangle(bx + 2, by, bw - 4, bh), iconFill);
            b.Draw(Game1.fadeToBlackRect, new Rectangle(bx, by + 2, bw, bh - 4), iconFill);
            b.Draw(Game1.fadeToBlackRect, new Rectangle(bx + 1, by + 1, bw - 2, bh - 2), iconFill);

            // Border (top)
            b.Draw(Game1.fadeToBlackRect, new Rectangle(bx + 2, by, bw - 4, 2), iconColor);
            // Border (bottom)
            b.Draw(Game1.fadeToBlackRect, new Rectangle(bx + 2, by + bh - 2, bw - 4, 2), iconColor);
            // Border (left)
            b.Draw(Game1.fadeToBlackRect, new Rectangle(bx, by + 2, 2, bh - 4), iconColor);
            // Border (right)
            b.Draw(Game1.fadeToBlackRect, new Rectangle(bx + bw - 2, by + 2, 2, bh - 4), iconColor);
            // Rounded corners
            b.Draw(Game1.fadeToBlackRect, new Rectangle(bx + 1, by + 1, 2, 2), iconColor);
            b.Draw(Game1.fadeToBlackRect, new Rectangle(bx + bw - 3, by + 1, 2, 2), iconColor);
            b.Draw(Game1.fadeToBlackRect, new Rectangle(bx + 1, by + bh - 3, 2, 2), iconColor);
            b.Draw(Game1.fadeToBlackRect, new Rectangle(bx + bw - 3, by + bh - 3, 2, 2), iconColor);

            // Tail (triangle pointing down-left)
            b.Draw(Game1.fadeToBlackRect, new Rectangle(bx + 4, by + bh, 4, 2), iconColor);
            b.Draw(Game1.fadeToBlackRect, new Rectangle(bx + 5, by + bh + 2, 2, 2), iconColor);
            // Tail fill
            b.Draw(Game1.fadeToBlackRect, new Rectangle(bx + 5, by + bh, 2, 2), iconFill);

            // Three dots inside bubble (typing indicator style)
            int dotY = cy - 1;
            int dotSpacing = 5;
            int startX = cx - dotSpacing;
            for (int i = 0; i < 3; i++)
            {
                b.Draw(Game1.fadeToBlackRect,
                    new Rectangle(startX + i * dotSpacing, dotY, 3, 3),
                    iconColor);
            }
        }

        public bool HandleClick(SButton button, ICursorPosition cursor)
        {
            if (button != SButton.MouseLeft) return false;
            Point pos = Utility.Vector2ToPoint(cursor.GetScaledScreenPixels());
            if (!_bounds.Contains(pos)) return false;
            Game1.playSound("bigSelect");
            _onClick();
            return true;
        }
    }
}
