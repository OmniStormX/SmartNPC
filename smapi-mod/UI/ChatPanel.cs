// ChatPanel — top-level QQ-style chat menu.
//
// Layout (W=900, H=560 by default; adapts to viewport):
//
//   ┌────────────┬───────────────────────────────────┐
//   │ ContactList│       ConversationView            │
//   │  (300 px)  │                                   │
//   └────────────┴───────────────────────────────────┘
//
// Hotkeys:
//   - Tab    → toggles the panel from outside; closes it from inside.
//   - F2     → opens the panel and gives focus to the contact list.
//   - Esc    → closes the panel.
//
// Marks the active conversation as read whenever the selection changes or
// the panel is opened with a pre-selected NPC.

using System.Collections.Generic;
using Microsoft.Xna.Framework;
using Microsoft.Xna.Framework.Graphics;
using Microsoft.Xna.Framework.Input;
using StardewValley;
using StardewValley.Menus;

namespace SmartNPC.Bridge
{
    /// <summary>
    /// Unified QQ-style chat panel. Hosts a <see cref="ContactList"/> on the
    /// left and a <see cref="ConversationView"/> on the right.
    /// </summary>
    internal sealed class ChatPanel : IClickableMenu
    {
        private const int PanelWidth = 900;
        private const int PanelHeight = 560;

        private readonly ChatMessageStore _store;
        private readonly UnreadTracker _unread;
        private readonly System.Action<string, string> _onSend;
        private readonly GroupChatManager? _groupMgr;

        private readonly ContactList _contacts;
        private readonly ConversationView _conversation;

        /// <summary>Currently open chat target NPC name (null if no panel open).</summary>
        public static string? ActiveNpc { get; private set; }

        /// <summary>True if a ChatPanel is currently the active menu.</summary>
        public static bool IsOpen => Game1.activeClickableMenu is ChatPanel;

        public ChatPanel(
            ChatMessageStore store,
            UnreadTracker unread,
            System.Action<string, string> onSend,
            string? initialNpc = null,
            GroupChatManager? groupMgr = null)
            : base(
                (Game1.uiViewport.Width - PanelWidth) / 2,
                (Game1.uiViewport.Height - PanelHeight) / 2,
                PanelWidth, PanelHeight, showUpperRightCloseButton: true)
        {
            _store = store;
            _unread = unread;
            _onSend = onSend;
            _groupMgr = groupMgr;

            _contacts = new ContactList(_store, _unread, this.OnContactSelected,
                this.OnCreateGroup, this.OnDisbandGroup);
            _conversation = new ConversationView(_store, this.OnConversationSend);

            ApplyLayout();

            _contacts.Refresh();
            if (!string.IsNullOrEmpty(initialNpc))
                _contacts.Select(initialNpc!);

            // If after Refresh nothing was selected, ensure ConversationView clears.
            if (_contacts.SelectedNpc == null)
                _conversation.SetNpc(null);

            initializeUpperRightCloseButton();
        }

        private void ApplyLayout()
        {
            Rectangle leftBounds = new(
                xPositionOnScreen, yPositionOnScreen,
                ContactList.Width, height);
            Rectangle rightBounds = new(
                xPositionOnScreen + ContactList.Width, yPositionOnScreen,
                width - ContactList.Width, height);

            _contacts.SetBounds(leftBounds);
            _conversation.SetBounds(rightBounds);
        }

        public override void draw(SpriteBatch b)
        {
            // Dim background.
            b.Draw(Game1.fadeToBlackRect, Game1.graphics.GraphicsDevice.Viewport.Bounds, Color.Black * 0.4f);

            // Outer frame.
            drawTextureBox(b, xPositionOnScreen - 20, yPositionOnScreen - 20,
                width + 40, height + 40, Color.White);

            _contacts.Draw(b);
            _conversation.Draw(b);

            base.draw(b);
            drawMouse(b);
        }

        public override void receiveLeftClick(int x, int y, bool playSound = true)
        {
            base.receiveLeftClick(x, y, playSound);

            // ContactList click takes precedence; if it accepts, refresh focus.
            if (_contacts.ReceiveLeftClick(x, y))
            {
                _conversation.Focus();
                return;
            }

            _conversation.ReceiveLeftClick(x, y);
        }

        public override void receiveScrollWheelAction(int direction)
        {
            int mx = Game1.getMouseX();
            // Route by mouse X position: left pane scrolls contacts.
            if (mx < xPositionOnScreen + ContactList.Width)
                _contacts.ReceiveScrollWheelAction(direction);
            else
                _conversation.ReceiveScrollWheelAction(direction);
        }

        public override void receiveKeyPress(Keys key)
        {
            // Esc / Tab close the panel; let TextBox keep typing for everything else.
            if (key == Keys.Escape || key == Keys.Tab)
            {
                exitThisMenu();
                return;
            }
        }

        public override void gameWindowSizeChanged(Rectangle oldBounds, Rectangle newBounds)
        {
            xPositionOnScreen = (Game1.uiViewport.Width - width) / 2;
            yPositionOnScreen = (Game1.uiViewport.Height - height) / 2;
            ApplyLayout();
        }

        protected override void cleanupBeforeExit()
        {
            ActiveNpc = null;
            _conversation.Unfocus();
            base.cleanupBeforeExit();
        }

        // ── Internal callbacks ──────────────────────────────────────────────

        private void OnContactSelected(string npcName)
        {
            ActiveNpc = npcName;
            _unread.MarkRead(npcName);
            _conversation.SetNpc(npcName);
            _conversation.ResetScroll();
            _conversation.Focus();
        }

        private void OnConversationSend(string npcName, string text)
        {
            if (npcName == GroupChatManager.GroupKey)
            {
                // Group chat mode: route through GroupChatManager.
                _groupMgr?.SendPlayerMessage(text);
            }
            else
            {
                _onSend(npcName, text);
            }
            _contacts.Refresh(); // re-sort by latest message time.
        }

        private void OnCreateGroup(List<string> participants)
        {
            if (_groupMgr == null) return;
            _groupMgr.CreateGroup(participants);
            _contacts.Refresh();
            // Auto-switch to the group conversation.
            _contacts.Select(GroupChatManager.GroupKey);
        }

        private void OnDisbandGroup()
        {
            if (_groupMgr == null) return;
            _groupMgr.EndGroup();
            _contacts.Refresh();
            // Switch to first available NPC.
            if (_contacts.NpcNames.Count > 0)
                _contacts.Select(_contacts.NpcNames[0]);
        }

        // ── Public API used by ModEntry ─────────────────────────────────────

        /// <summary>Opens the chat panel as the active menu, selecting the given
        /// NPC if provided.</summary>
        public static ChatPanel Open(
            ChatMessageStore store,
            UnreadTracker unread,
            System.Action<string, string> onSend,
            string? npc = null,
            GroupChatManager? groupMgr = null)
        {
            var panel = new ChatPanel(store, unread, onSend, npc, groupMgr);
            Game1.activeClickableMenu = panel;
            ActiveNpc = panel._conversation.CurrentNpc;
            return panel;
        }

        /// <summary>If the panel is open, refresh the contact list (e.g. after
        /// new message arrived).</summary>
        public void RefreshContacts()
        {
            _contacts.Refresh();
        }
    }
}
