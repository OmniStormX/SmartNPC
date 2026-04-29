// Handles the `mail_send` ws action (migrated from the M2 HTTP endpoint).
// Queues HUD messages onto the game thread.

using System;
using System.Collections.Concurrent;
using System.Text.Json;
using System.Text.Json.Serialization;
using System.Threading.Tasks;
using StardewModdingAPI;
using StardewValley;

namespace SmartNPC.Bridge
{
    internal sealed class MailHandler
    {
        private readonly IMonitor _log;
        private readonly ConcurrentQueue<string> _pending = new();

        public MailHandler(IMonitor log) { _log = log; }

        public Task<Response> Handle(string id, JsonElement @params)
        {
            MailSendParams? p;
            try { p = JsonSerializer.Deserialize<MailSendParams>(@params, JsonOpts.Web); }
            catch (Exception ex) { return Task.FromResult(Response.Failure(id, "invalid_params", ex.Message)); }

            if (p is null || string.IsNullOrWhiteSpace(p.Text))
                return Task.FromResult(Response.Failure(id, "invalid_params", "text is required"));

            _pending.Enqueue(p.Text!);
            return Task.FromResult(Response.Success(id, new MailSendResult { Ok = true, Message = "displayed" }));
        }

        /// <summary>Drain pending messages on the game thread.</summary>
        public void PumpOnGameTick()
        {
            if (_pending.IsEmpty || !Context.IsWorldReady) return;
            while (_pending.TryDequeue(out string? text))
            {
                if (string.IsNullOrEmpty(text)) continue;
                Game1.addHUDMessage(new HUDMessage(text, HUDMessage.newQuest_type));
                _log.Log($"HUD: {text}", LogLevel.Debug);
            }
        }

        private sealed class MailSendParams
        {
            [JsonPropertyName("text")] public string? Text { get; set; }
        }

        private sealed class MailSendResult
        {
            [JsonPropertyName("ok")]      public bool   Ok      { get; set; }
            [JsonPropertyName("message")] public string? Message { get; set; }
        }
    }
}
