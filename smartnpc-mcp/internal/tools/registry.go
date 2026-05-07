// Package tools registers MCP tools on the server.
package tools

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/smartnpc/smartnpc-mcp/internal/bridge"
)

// RegisterAll wires every available tool group onto the given server.
//
// br may be nil; mod-backed tools (mail_send, chat_say) are then omitted so
// that the server is still usable for the meta `ping` tool alone.
func RegisterAll(s *mcp.Server, br *bridge.WSClient) {
	registerMeta(s)
	if br != nil {
		registerMail(s, br)
		registerChat(s, br)
		registerGameQuery(s, br)
		registerNpcPerception(s, br)
		registerNpcMovement(s, br)
		registerNpcBehavior(s, br)
	}
}
