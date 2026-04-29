// Package tools registers MCP tools on the server.
//
// Each functional group lives in its own file (mail.go, npc_query.go, ...).
package tools

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/smartnpc/smartnpc-mcp/internal/bridge"
)

// RegisterAll wires every available tool group onto the given server.
//
// br may be nil; tools requiring it will fail at call time with a clear
// "mod not configured" error. This makes it easy to use Claude Desktop just
// for the meta `ping` tool without having the SMAPI mod running.
func RegisterAll(s *mcp.Server, br *bridge.Client) {
	registerMeta(s)
	if br != nil {
		registerMail(s, br)
	}
}
