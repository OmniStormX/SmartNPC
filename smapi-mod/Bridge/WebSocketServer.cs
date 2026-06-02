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
using System.Collections.Concurrent;
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

        // Debug-only: when DebugLogIncomingRequests is enabled, every
        // inbound request is enqueued here; PumpOnGameTick then drains
        // the queue from the SDV game thread and hands each event to the
        // sink (set by ModEntry to route into the per-NPC chat panel).
        //
        // The queue is unbounded but small — request throughput is
        // human-conversation-rate, not bulk traffic.
        private readonly ConcurrentQueue<DebugRequestEvent> _debugEvents = new();
        private Func<bool>? _debugRequestsEnabled;
        private Action<DebugRequestEvent>? _debugSink;

        /// <summary>
        /// Structured snapshot of an inbound mcp request, surfaced to the
        /// chat panel UI when DebugLogIncomingRequests is on. Built on the
        /// ws receive thread, consumed on the game thread.
        ///
        /// <see cref="NpcName"/> is empty for non-NPC tools (mail_send,
        /// game_get_*, friendship_get, player_get_status, etc.); the
        /// sink should route those to a system channel.
        /// </summary>
        internal sealed class DebugRequestEvent
        {
            public string Action { get; init; } = "";
            public string Id { get; init; } = "";
            public string ParamsJson { get; init; } = "";
            public string NpcName { get; init; } = "";
        }

        public WebSocketServer(string prefix, MessageRouter router, IMonitor log)
        {
            _prefix = prefix;
            _router = router;
            _log = log;
        }

        /// <summary>
        /// Wire up the debug-mirror channel. <paramref name="enabled"/> is
        /// polled per request so toggling <c>config.json</c> at runtime
        /// (after a SMAPI reload) takes effect without restarting the ws
        /// server. <paramref name="sink"/> is invoked from the game thread
        /// inside <see cref="PumpOnGameTick"/>; it owns deciding which
        /// chat-panel channel the event lands on.
        /// </summary>
        public void EnableRequestDebug(Func<bool> enabled, Action<DebugRequestEvent> sink)
        {
            _debugRequestsEnabled = enabled;
            _debugSink = sink;
            // Probe the gate once at wire-up so the SMAPI log tells us if
            // ModConfig.DebugLogIncomingRequests is actually true at this
            // moment (catches "edited config.json after game launch" cases).
            bool snapshot;
            try { snapshot = enabled?.Invoke() ?? false; }
            catch { snapshot = false; }
            _log.Log($"[debug-req] EnableRequestDebug wired (snapshot enabled={snapshot})", LogLevel.Info);
        }

        /// <summary>
        /// Drain queued debug events onto the chat panel. Call from
        /// <c>OnUpdateTicked</c>. No-op when no sink is wired or queue
        /// is empty.
        /// </summary>
        public void PumpOnGameTick()
        {
            if (_debugSink is null) return;
            while (_debugEvents.TryDequeue(out DebugRequestEvent? evt))
            {
                if (evt is null) continue;
                _log.Log($"[debug-req] pump → action={evt.Action} npc={evt.NpcName}", LogLevel.Debug);
                try { _debugSink(evt); }
                catch (Exception ex) { _log.Log($"[debug-req] sink failed: {ex.Message}", LogLevel.Warn); }
            }
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

            this.MaybeQueueDebugEvent(req);

            Response resp = await _router.Dispatch(req).ConfigureAwait(false);
            await this.SendJson(resp).ConfigureAwait(false);
        }

        // Mirror the inbound request into the chat panel when the debug
        // switch is on. Runs on the ws receive thread, so we ONLY build
        // the structured event here and enqueue it; the sink (registered
        // by ModEntry) is fired later from PumpOnGameTick on the game
        // thread, where it's safe to touch ChatMessageStore / UI.
        private void MaybeQueueDebugEvent(Request req)
        {
            if (_debugSink is null)
            {
                _log.Log($"[debug-req] skip (no sink wired) action={req.Action}", LogLevel.Trace);
                return;
            }
            bool gateOpen;
            try { gateOpen = _debugRequestsEnabled?.Invoke() ?? false; }
            catch (Exception ex)
            {
                _log.Log($"[debug-req] enabled() threw: {ex.Message}", LogLevel.Trace);
                return;
            }
            if (!gateOpen)
            {
                _log.Log($"[debug-req] skip (DebugLogIncomingRequests=false) action={req.Action}", LogLevel.Trace);
                return;
            }

            string action = req.Action ?? "?";
            string id = req.Id ?? "?";

            // Resolve the originating NPC. Priority:
            //   1. req.FromNpc — stamped by smartnpc-mcp once the calling
            //      Hermes profile has registered itself (agent_register_self
            //      tool). Authoritative; works for NPC-agnostic queries
            //      whose params carry no `npc` field.
            //   2. params.npc — fallback for legacy callers and tools that
            //      target an NPC explicitly (npc_move_to, chat_say, …).
            string npcName = req.FromNpc ?? "";
            string paramsRaw = "";
            try
            {
                if (req.Params.ValueKind != JsonValueKind.Undefined)
                {
                    paramsRaw = req.Params.GetRawText();
                    if (string.IsNullOrEmpty(npcName) &&
                        req.Params.ValueKind == JsonValueKind.Object &&
                        req.Params.TryGetProperty("npc", out JsonElement npcEl) &&
                        npcEl.ValueKind == JsonValueKind.String)
                    {
                        npcName = npcEl.GetString() ?? "";
                    }
                }
            }
            catch { paramsRaw = "<unreadable>"; }

            // Truncate over-long payloads (a single schedule_trigger or
            // env-update can be ~20kB which would otherwise blow up the
            // chat history).
            const int maxLen = 240;
            if (paramsRaw.Length > maxLen) paramsRaw = paramsRaw.Substring(0, maxLen - 3) + "...";

            _debugEvents.Enqueue(new DebugRequestEvent
            {
                Action     = action,
                Id         = id,
                ParamsJson = paramsRaw,
                NpcName    = npcName,
            });
            _log.Log($"[debug-req] enqueued action={action} npc={(string.IsNullOrEmpty(npcName) ? "<system>" : npcName)}", LogLevel.Info);
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
