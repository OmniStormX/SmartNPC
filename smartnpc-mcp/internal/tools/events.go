// Forwarding bridge events (e.g. chat_received) to MCP clients as
// notifications. The MCP go-sdk uses logging notifications as a generic
// out-of-band channel; we use the data payload to encode the event.
package tools

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/smartnpc/smartnpc-mcp/internal/bridge"
)

// MakeEventForwarder returns a bridge.EventHandler that pushes each incoming
// event to every connected MCP session as a logging notification with the
// shape:
//
//	{ "kind": "stardew/event", "name": "<event>", "data": { ... } }
//
// MCP clients (e.g. Hermes profiles) subscribe by setting LoggingMessageHandler
// in their ClientOptions and filtering on kind == "stardew/event".
func MakeEventForwarder(server *mcp.Server, log *slog.Logger) bridge.EventHandler {
	return func(ctx context.Context, name string, data json.RawMessage) {
		payload := map[string]any{
			"kind":      "stardew/event",
			"name":      name,
			"data":      json.RawMessage(data),
			"timestamp": time.Now().UnixMilli(),
		}
		// Push to every active session. Errors are non-fatal — sessions may
		// have closed in flight. server.Sessions() returns iter.Seq, which
		// we drain with the explicit pull form to stay Go-1.22 compatible.
		server.Sessions()(func(sess *mcp.ServerSession) bool {
			if err := sess.Log(ctx, &mcp.LoggingMessageParams{
				Level: "info",
				Data:  payload,
			}); err != nil {
				log.Debug("notify session failed", "name", name, "err", err)
			}
			return true
		})
	}
}
