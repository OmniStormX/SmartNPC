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
            if (_pending.IsEmpty || !Context.IsWorldReady || Game1.chatBox is null) return;
            while (_pending.TryDequeue(out ChatSayParams? p))
            {
                if (p is null) continue;
                Color color = ResolveColor(p.Color);
                // Format: "<speaker> text" so the speaker is visually attributed.
                // Using addInfoMessage so it doesn't show a fake player name.
                Game1.chatBox.addInfoMessage($"<{p.Speaker}> {p.Text}");
                _log.Log($"chat: <{p.Speaker}> {p.Text}", LogLevel.Trace);
                _ = color; // color is reserved for future styling; addInfoMessage uses a fixed color
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
