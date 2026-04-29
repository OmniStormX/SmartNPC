// Package tools registers MCP tools on the server.
//
// Each functional group lives in its own file (npc_query.go, npc_dialogue.go,
// etc.). Today only the meta tools are implemented.
package tools

import "github.com/modelcontextprotocol/go-sdk/mcp"

// RegisterAll wires every available tool group onto the given server.
// Future tool groups (npc_*, friendship_*, ...) should be added here.
func RegisterAll(s *mcp.Server) {
	registerMeta(s)
}
