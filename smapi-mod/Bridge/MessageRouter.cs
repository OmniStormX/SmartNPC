// Action name -> handler dispatch. Handlers are registered at mod startup and
// invoked on the ws receive loop. They must not touch Game1 state directly —
// queue work onto the game thread (see e.g. ChatHandler).

using System;
using System.Collections.Generic;
using System.Text.Json;
using System.Threading.Tasks;
using StardewModdingAPI;

namespace SmartNPC.Bridge
{
    internal delegate Task<Response> RequestHandler(string id, JsonElement @params);

    internal sealed class MessageRouter
    {
        private readonly IMonitor _log;
        private readonly Dictionary<string, RequestHandler> _handlers = new();

        public MessageRouter(IMonitor log) { _log = log; }

        public void Register(string action, RequestHandler handler)
        {
            _handlers[action] = handler;
        }

        public async Task<Response> Dispatch(Request req)
        {
            if (string.IsNullOrEmpty(req.Id))
                return Response.Failure("", "invalid_request", "missing id");
            if (string.IsNullOrEmpty(req.Action))
                return Response.Failure(req.Id!, "invalid_request", "missing action");
            if (!_handlers.TryGetValue(req.Action!, out RequestHandler? h))
                return Response.Failure(req.Id!, "unknown_action", $"no handler for '{req.Action}'");

            try
            {
                return await h(req.Id!, req.Params).ConfigureAwait(false);
            }
            catch (Exception ex)
            {
                _log.Log($"handler {req.Action} threw: {ex}", LogLevel.Warn);
                return Response.Failure(req.Id!, "handler_error", ex.Message);
            }
        }
    }
}
