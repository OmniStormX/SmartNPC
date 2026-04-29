// Player-side chat capture:
//   - Ctrl+T opens the chat box (normally single-player disables it).
//   - Harmony postfix on ChatBox.receiveChatMessage(string, long) forwards
//     every player-authored line to the ws bridge as a chat_received event.
//
// We only forward messages whose source is the local player (sourceFarmer ==
// Game1.player.UniqueMultiplayerID). Info messages added by the mod itself
// (chatBox.addInfoMessage) have sourceFarmer == 0 and are ignored.

using System;
using System.Threading.Tasks;
using HarmonyLib;
using StardewModdingAPI;
using StardewModdingAPI.Events;
using StardewValley;
using StardewValley.Menus;

namespace SmartNPC.Bridge
{
    internal sealed class ChatInputCapture
    {
        private static IMonitor? _log;
        private static Func<string, Task>? _onPlayerMessage;

        private readonly IModHelper _helper;

        public ChatInputCapture(IMod mod, Func<string, Task> onPlayerMessage)
        {
            _helper = mod.Helper;
            _log = mod.Monitor;
            _onPlayerMessage = onPlayerMessage;

            var harmony = new Harmony(mod.ModManifest.UniqueID);
            harmony.Patch(
                original: AccessTools.Method(typeof(ChatBox), nameof(ChatBox.receiveChatMessage)),
                postfix: new HarmonyMethod(typeof(ChatInputCapture), nameof(Postfix_receiveChatMessage))
            );

            _helper.Events.Input.ButtonsChanged += OnButtonsChanged;
        }

        /// <summary>Ctrl+T → activate chat box (single-player friendly).</summary>
        private static void OnButtonsChanged(object? sender, ButtonsChangedEventArgs e)
        {
            if (!Context.IsWorldReady || Game1.chatBox is null) return;
            if (Game1.chatBox.isActive()) return;

            // Trigger on T pressed this frame while Ctrl is held.
            if (!IsPressed(e, SButton.T)) return;

            bool ctrl = IsDown(e, SButton.LeftControl) || IsDown(e, SButton.RightControl);
            if (!ctrl) return;

            Game1.chatBox.activate();
            _log?.Log("chat box activated (Ctrl+T)", LogLevel.Trace);
        }

        private static bool IsPressed(ButtonsChangedEventArgs e, SButton btn)
        {
            foreach (SButton b in e.Pressed)
                if (b == btn) return true;
            return false;
        }

        private static bool IsDown(ButtonsChangedEventArgs e, SButton btn)
        {
            foreach (SButton b in e.Held)
                if (b == btn) return true;
            foreach (SButton b in e.Pressed)
                if (b == btn) return true;
            return false;
        }

        /// <summary>Harmony postfix: forward each message emitted from ChatBox to the bridge.</summary>
        public static void Postfix_receiveChatMessage(string message, long sourceFarmer)
        {
            try
            {
                // Only forward messages originating from the local player. Info
                // messages (addInfoMessage) come in with sourceFarmer == 0.
                if (sourceFarmer == 0) return;
                if (sourceFarmer != Game1.player.UniqueMultiplayerID) return;
                if (string.IsNullOrWhiteSpace(message)) return;

                var fn = _onPlayerMessage;
                if (fn is null) return;
                _ = fn(message);
            }
            catch (Exception ex)
            {
                _log?.Log($"chat capture failed: {ex}", LogLevel.Warn);
            }
        }
    }
}
