// SmartNPC SMAPI Mod entry point — M2 minimal viable bridge.
//
// Spins up an HttpListener on http://127.0.0.1:18745 and accepts a single
// route:
//
//   POST /mail_send   { "text": "..." }
//   →    200          { "ok": true, "message": "displayed" }
//
// On a valid request it queues a HUD message which is shown on the next game
// tick (HUD modifications must happen on the game thread).
//
// This is intentionally tiny and disposable: M3 will replace HTTP with a
// proper WebSocket bridge and refactor the routing layer. Do not extend
// this file with more endpoints — start a new file (and likely a new
// architecture) instead.

using System;
using System.IO;
using System.Net;
using System.Text;
using System.Text.Json;
using System.Text.Json.Serialization;
using System.Threading;
using System.Threading.Tasks;
using StardewModdingAPI;
using StardewModdingAPI.Events;
using StardewValley;

namespace SmartNPC.Bridge
{
    /// <summary>The mod entry point.</summary>
    public sealed class ModEntry : Mod
    {
        private const string ListenPrefix = "http://127.0.0.1:18745/";

        private HttpListener? _listener;
        private CancellationTokenSource? _cts;

        // Pending HUD messages queued from the listener thread, drained on the
        // game thread via the UpdateTicked event.
        private readonly System.Collections.Concurrent.ConcurrentQueue<string> _pendingHud = new();

        /// <summary>SMAPI entry point.</summary>
        public override void Entry(IModHelper helper)
        {
            helper.Events.GameLoop.GameLaunched += this.OnGameLaunched;
            helper.Events.GameLoop.UpdateTicked += this.OnUpdateTicked;
            helper.Events.GameLoop.GameLaunched += (_, _) =>
                this.Monitor.Log($"StardewMCPBridge ready, will listen on {ListenPrefix}", LogLevel.Info);
        }

        private void OnGameLaunched(object? sender, GameLaunchedEventArgs e)
        {
            try
            {
                this._cts = new CancellationTokenSource();
                this._listener = new HttpListener();
                this._listener.Prefixes.Add(ListenPrefix);
                this._listener.Start();
                _ = Task.Run(() => this.AcceptLoop(this._cts.Token));
                this.Monitor.Log($"HTTP listener started on {ListenPrefix}", LogLevel.Info);
            }
            catch (Exception ex)
            {
                this.Monitor.Log($"failed to start HTTP listener: {ex.Message}", LogLevel.Error);
            }
        }

        /// <summary>
        /// Drain queued HUD messages on the game thread. HUD APIs are not
        /// thread-safe and must be called from here, never from AcceptLoop.
        /// </summary>
        private void OnUpdateTicked(object? sender, UpdateTickedEventArgs e)
        {
            if (this._pendingHud.IsEmpty)
                return;
            if (!Context.IsWorldReady)   // wait for a save to be loaded
                return;

            while (this._pendingHud.TryDequeue(out string? text))
            {
                if (string.IsNullOrEmpty(text))
                    continue;
                Game1.addHUDMessage(new HUDMessage(text, HUDMessage.newQuest_type));
                this.Monitor.Log($"HUD: {text}", LogLevel.Debug);
            }
        }

        // ── HTTP server ─────────────────────────────────────────────────

        private async Task AcceptLoop(CancellationToken ct)
        {
            while (!ct.IsCancellationRequested && this._listener?.IsListening == true)
            {
                HttpListenerContext ctx;
                try
                {
                    ctx = await this._listener.GetContextAsync().ConfigureAwait(false);
                }
                catch (ObjectDisposedException) { return; }
                catch (HttpListenerException) { return; }

                _ = Task.Run(() => this.HandleRequest(ctx));
            }
        }

        private async Task HandleRequest(HttpListenerContext ctx)
        {
            try
            {
                if (ctx.Request.HttpMethod != "POST" || ctx.Request.Url?.AbsolutePath != "/mail_send")
                {
                    await WriteJson(ctx.Response, 404, new { ok = false, message = "not found" });
                    return;
                }

                MailSendRequest? body;
                using (var reader = new StreamReader(ctx.Request.InputStream, Encoding.UTF8))
                {
                    string raw = await reader.ReadToEndAsync().ConfigureAwait(false);
                    body = JsonSerializer.Deserialize<MailSendRequest>(raw, JsonOpts);
                }

                if (body is null || string.IsNullOrWhiteSpace(body.Text))
                {
                    await WriteJson(ctx.Response, 400, new { ok = false, message = "text is required" });
                    return;
                }

                this._pendingHud.Enqueue(body.Text);
                await WriteJson(ctx.Response, 200, new { ok = true, message = "displayed" });
            }
            catch (Exception ex)
            {
                this.Monitor.Log($"request error: {ex}", LogLevel.Warn);
                try { await WriteJson(ctx.Response, 500, new { ok = false, message = ex.Message }); }
                catch { /* response may already be closed */ }
            }
        }

        private static readonly JsonSerializerOptions JsonOpts = new(JsonSerializerDefaults.Web)
        {
            DefaultIgnoreCondition = JsonIgnoreCondition.WhenWritingNull,
        };

        private static async Task WriteJson(HttpListenerResponse resp, int status, object payload)
        {
            byte[] data = JsonSerializer.SerializeToUtf8Bytes(payload, JsonOpts);
            resp.StatusCode = status;
            resp.ContentType = "application/json";
            resp.ContentLength64 = data.LongLength;
            await resp.OutputStream.WriteAsync(data).ConfigureAwait(false);
            resp.OutputStream.Close();
        }

        // ── DTO ─────────────────────────────────────────────────────────

        private sealed class MailSendRequest
        {
            [JsonPropertyName("text")] public string? Text { get; set; }
        }

        protected override void Dispose(bool disposing)
        {
            try
            {
                this._cts?.Cancel();
                this._listener?.Stop();
                this._listener?.Close();
            }
            catch { /* shutdown best-effort */ }
            base.Dispose(disposing);
        }
    }
}
