// Minimal WebSocket server built on System.Net.HttpListener (comes with
// .NET 6 on Windows). Single-connection is fine for our use case; if a
// second client connects, we close the previous one.
//
// Features:
//   - accept ws upgrade on /ws
//   - receive JSON text frames -> MessageRouter -> reply with Response
//   - Broadcast(event) sends JSON to the current connection (no-op if none)
//   - auto-ping every 30s (browser-compatible ping/pong handled by framework)

using System;
using System.Net;
using System.Net.WebSockets;
using System.Text;
using System.Text.Json;
using System.Threading;
using System.Threading.Tasks;
using StardewModdingAPI;

namespace SmartNPC.Bridge
{
    internal sealed class WebSocketServer : IDisposable
    {
        private readonly IMonitor _log;
        private readonly string _prefix;
        private readonly MessageRouter _router;

        private HttpListener? _listener;
        private CancellationTokenSource? _cts;
        private WebSocket? _current;         // latest connected client (at most one)
        private readonly SemaphoreSlim _sendLock = new(1, 1);

        public WebSocketServer(string prefix, MessageRouter router, IMonitor log)
        {
            _prefix = prefix;
            _router = router;
            _log = log;
        }

        public void Start()
        {
            _cts = new CancellationTokenSource();
            _listener = new HttpListener();
            _listener.Prefixes.Add(_prefix);
            _listener.Start();
            _ = Task.Run(() => this.AcceptLoop(_cts.Token));
            _log.Log($"WebSocket listening on {_prefix}ws", LogLevel.Info);
        }

        public void Dispose()
        {
            try { _cts?.Cancel(); } catch { }
            try { _listener?.Stop(); _listener?.Close(); } catch { }
            try { _current?.Dispose(); } catch { }
        }

        // ── accept loop ─────────────────────────────────────────────────

        private async Task AcceptLoop(CancellationToken ct)
        {
            while (!ct.IsCancellationRequested && _listener?.IsListening == true)
            {
                HttpListenerContext ctx;
                try { ctx = await _listener.GetContextAsync().ConfigureAwait(false); }
                catch (ObjectDisposedException) { return; }
                catch (HttpListenerException) { return; }

                if (!ctx.Request.IsWebSocketRequest || ctx.Request.Url?.AbsolutePath != "/ws")
                {
                    ctx.Response.StatusCode = 400;
                    ctx.Response.Close();
                    continue;
                }

                _ = Task.Run(() => this.HandleClient(ctx, ct));
            }
        }

        private async Task HandleClient(HttpListenerContext ctx, CancellationToken ct)
        {
            HttpListenerWebSocketContext wsCtx;
            try { wsCtx = await ctx.AcceptWebSocketAsync(subProtocol: null).ConfigureAwait(false); }
            catch (Exception ex) { _log.Log($"ws accept failed: {ex.Message}", LogLevel.Warn); return; }

            // Close previous connection if any — we only support one client.
            var prev = Interlocked.Exchange(ref _current, wsCtx.WebSocket);
            if (prev is not null)
            {
                try { await prev.CloseAsync(WebSocketCloseStatus.PolicyViolation, "replaced", CancellationToken.None); } catch { }
                prev.Dispose();
            }

            _log.Log("ws client connected", LogLevel.Info);
            try { await this.ReceiveLoop(wsCtx.WebSocket, ct).ConfigureAwait(false); }
            finally
            {
                _log.Log("ws client disconnected", LogLevel.Debug);
                Interlocked.CompareExchange(ref _current, null, wsCtx.WebSocket);
                wsCtx.WebSocket.Dispose();
            }
        }

        private async Task ReceiveLoop(WebSocket ws, CancellationToken ct)
        {
            var buf = new byte[16 * 1024];
            var ms = new System.IO.MemoryStream();

            while (ws.State == WebSocketState.Open && !ct.IsCancellationRequested)
            {
                ms.SetLength(0);
                WebSocketReceiveResult res;
                do
                {
                    try { res = await ws.ReceiveAsync(new ArraySegment<byte>(buf), ct).ConfigureAwait(false); }
                    catch { return; }
                    if (res.MessageType == WebSocketMessageType.Close)
                    {
                        try { await ws.CloseAsync(WebSocketCloseStatus.NormalClosure, "", CancellationToken.None); } catch { }
                        return;
                    }
                    ms.Write(buf, 0, res.Count);
                } while (!res.EndOfMessage);

                string text = Encoding.UTF8.GetString(ms.GetBuffer(), 0, (int)ms.Length);
                _ = Task.Run(() => this.HandleFrame(text));
            }
        }

        private async Task HandleFrame(string text)
        {
            Request? req;
            try { req = JsonSerializer.Deserialize<Request>(text, JsonOpts.Web); }
            catch (Exception ex)
            {
                _log.Log($"invalid ws frame: {ex.Message}", LogLevel.Warn);
                return;
            }
            if (req is null || req.Type != "request") return;

            Response resp = await _router.Dispatch(req).ConfigureAwait(false);
            await this.SendJson(resp).ConfigureAwait(false);
        }

        // ── status ──────────────────────────────────────────────────────

        // The mod's ws server is single-client by design (only one mcp at a
        // time); ConnectedClientCount returns 0 or 1. Exposed for status
        // commands so operators can confirm liveness from inside the game
        // without poking around at the network level.
        public int ConnectedClientCount =>
            (_current is { State: WebSocketState.Open }) ? 1 : 0;

        // ── outbound ────────────────────────────────────────────────────

        public Task BroadcastEvent(string name, object? data)
        {
            var evt = new Event
            {
                Name = name,
                Data = data,
                Timestamp = DateTimeOffset.UtcNow.ToUnixTimeMilliseconds(),
            };
            return this.SendJson(evt);
        }

        private async Task SendJson(object payload)
        {
            var ws = _current;
            if (ws is null || ws.State != WebSocketState.Open) return;

            byte[] data = JsonSerializer.SerializeToUtf8Bytes(payload, JsonOpts.Web);
            await _sendLock.WaitAsync().ConfigureAwait(false);
            try
            {
                await ws.SendAsync(new ArraySegment<byte>(data),
                    WebSocketMessageType.Text, endOfMessage: true, CancellationToken.None)
                    .ConfigureAwait(false);
            }
            catch (Exception ex) { _log.Log($"ws send failed: {ex.Message}", LogLevel.Debug); }
            finally { _sendLock.Release(); }
        }
    }
}
