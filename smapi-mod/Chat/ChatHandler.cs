// Handles the `chat_say` ws action. Queues messages onto the game thread so
// that ChatBox APIs (which require the main thread) are touched safely.

using System;
using System.Collections.Concurrent;
using System.Text.Json;
using System.Text.Json.Serialization;
using System.Threading.Tasks;
using Microsoft.Xna.Framework;
using StardewModdingAPI;
using StardewValley;
using StardewValley.Menus;

namespace SmartNPC.Bridge
{
    internal sealed class ChatHandler
    {
        private readonly IMonitor _log;
        private readonly ConcurrentQueue<ChatSayParams> _pending = new();

        public ChatHandler(IMonitor log) { _log = log; }

        public Task<Response> Handle(string id, JsonElement @params)
        {
            ChatSayParams? p;
            try { p = JsonSerializer.Deserialize<ChatSayParams>(@params, JsonOpts.Web); }
            catch (Exception ex) { return Task.FromResult(Response.Failure(id, "invalid_params", ex.Message)); }

            if (p is null || string.IsNullOrWhiteSpace(p.Speaker) || string.IsNullOrWhiteSpace(p.Text))
                return Task.FromResult(Response.Failure(id, "invalid_params", "speaker and text are required"));

            if (!Context.IsWorldReady)
                return Task.FromResult(Response.Failure(id, "mod_not_ready", "no save loaded"));

            _pending.Enqueue(p);
            return Task.FromResult(Response.Success(id, new { ok = true }));
        }

        public void PumpOnGameTick()
        {
            if (_pending.IsEmpty || !Context.IsWorldReady) return;

            // If a dialogue is already on screen, wait until the player dismisses it.
            if (Game1.activeClickableMenu is StardewValley.Menus.DialogueBox) return;

            if (!_pending.TryDequeue(out ChatSayParams? p) || p is null) return;

            // Try to show a proper NPC dialogue box with portrait.
            NPC? npc = Game1.getCharacterFromName(p.Speaker);
            if (npc != null)
            {
                var dialogue = new Dialogue(npc, "SmartNPC:response", p.Text!);
                Game1.DrawDialogue(dialogue);
                _log.Log($"dialogue: <{p.Speaker}> {p.Text}", LogLevel.Trace);
            }
            else
            {
                // Fallback: speaker is not a known NPC, use chat box.
                Game1.chatBox?.addInfoMessage($"<{p.Speaker}> {p.Text}");
                _log.Log($"chat: <{p.Speaker}> {p.Text}", LogLevel.Trace);
            }
        }

        private static Color ResolveColor(string? name) => (name ?? "yellow").ToLowerInvariant() switch
        {
            "white"  => Color.White,
            "yellow" => Color.Yellow,
            "green"  => Color.LightGreen,
            "red"    => Color.Red,
            "cyan"   => Color.Cyan,
            "blue"   => Color.LightBlue,
            "purple" => Color.MediumPurple,
            "gray" or "grey" => Color.Gray,
            _ => Color.Yellow,
        };

        private sealed class ChatSayParams
        {
            [JsonPropertyName("speaker")] public string? Speaker { get; set; }
            [JsonPropertyName("text")]    public string? Text    { get; set; }
            [JsonPropertyName("color")]   public string? Color   { get; set; }
        }
    }
}
