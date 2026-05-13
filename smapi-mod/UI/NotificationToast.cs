// NotificationToast — non-modal floating toast for incoming NPC messages
// while the ChatPanel is closed.
//
// Stack policy: at most 3 toasts visible, stacked from the bottom-right.
// Each toast lives ~3 seconds and fades out over the last 0.5s.
//
// The mod entry calls Push(...) when a chat_say arrives and the panel is
// closed. The mod entry also draws and ticks the singleton via the SMAPI
// Display.Rendered / GameLoop.UpdateTicked events.

using System;
using System.Collections.Generic;
using Microsoft.Xna.Framework;
using Microsoft.Xna.Framework.Graphics;
using StardewValley;

namespace SmartNPC.Bridge
{
    /// <summary>
    /// Floating toast notifications for NPC messages received while the chat
    /// panel is closed. Click a toast to open the conversation.
    /// </summary>
    internal sealed class NotificationToast
    {
        private const int ToastWidth = 320;
        private const int ToastHeight = 72;
        private const int ToastSpacing = 8;
        private const int Padding = 12;
        private const float TotalSeconds = 3.0f;
        private const float FadeSeconds = 0.6f;
        private const int MaxVisible = 3;

        private sealed class Item
        {
            public string NpcName = "";
            public string DisplayName = "";
            public string Preview = "";
            public float Remaining = TotalSeconds;
            public Rectangle LastBounds;
        }

        private readonly Action<string> _onClick;
        private readonly Queue<Item> _items = new();

        public NotificationToast(Action<string> onClick)
        {
            _onClick = onClick;
        }

        public void Push(string npcName, string displayName, string text)
        {
            string preview = text.Length > 20 ? text[..20] + "..." : text;
            _items.Enqueue(new Item
            {
                NpcName = npcName,
                DisplayName = displayName,
                Preview = preview,
                Remaining = TotalSeconds,
            });
            // Drop overflow.
            while (_items.Count > MaxVisible)
                _items.Dequeue();
        }

        /// <summary>Tick down lifetimes; remove expired toasts.</summary>
        public void Update(float dt)
        {
            if (_items.Count == 0) return;
            foreach (var it in _items)
                it.Remaining -= dt;
            while (_items.Count > 0 && _items.Peek().Remaining <= 0f)
                _items.Dequeue();
        }

        public bool TryClick(int x, int y)
        {
            foreach (var it in _items)
            {
                if (it.LastBounds.Contains(x, y))
                {
                    string npc = it.NpcName;
                    _items.Clear();
                    _onClick(npc);
                    return true;
                }
            }
            return false;
        }

        public void Draw(SpriteBatch b)
        {
            if (_items.Count == 0) return;

            int viewportW = Game1.uiViewport.Width;
            int viewportH = Game1.uiViewport.Height;
            int x = viewportW - ToastWidth - 24;
            int y = viewportH - ToastHeight - 80;

            // Iterate Queue (newest at tail) — draw newest at top, oldest below.
            var arr = _items.ToArray();
            for (int i = arr.Length - 1; i >= 0; i--)
            {
                var it = arr[i];
                Rectangle rect = new(x, y, ToastWidth, ToastHeight);
                it.LastBounds = rect;

                float alpha = it.Remaining < FadeSeconds
                    ? Math.Max(0f, it.Remaining / FadeSeconds)
                    : 1f;

                Color bg = new Color(50, 50, 60) * (0.85f * alpha);
                Color border = new Color(255, 220, 130) * alpha;
                Color titleColor = new Color(255, 230, 150) * alpha;
                Color textColor = new Color(245, 245, 245) * alpha;

                b.Draw(Game1.fadeToBlackRect, rect, bg);
                DrawBorder(b, rect, border);

                b.DrawString(Game1.smallFont, it.DisplayName,
                    new Vector2(rect.X + Padding, rect.Y + 8), titleColor);
                b.DrawString(Game1.smallFont, it.Preview,
                    new Vector2(rect.X + Padding, rect.Y + 36), textColor);

                y -= (ToastHeight + ToastSpacing);
            }
        }

        public void Clear() => _items.Clear();

        private static void DrawBorder(SpriteBatch b, Rectangle rect, Color color)
        {
            b.Draw(Game1.fadeToBlackRect, new Rectangle(rect.X, rect.Y, rect.Width, 2), color);
            b.Draw(Game1.fadeToBlackRect, new Rectangle(rect.X, rect.Bottom - 2, rect.Width, 2), color);
            b.Draw(Game1.fadeToBlackRect, new Rectangle(rect.X, rect.Y, 2, rect.Height), color);
            b.Draw(Game1.fadeToBlackRect, new Rectangle(rect.Right - 2, rect.Y, 2, rect.Height), color);
        }
    }
}
