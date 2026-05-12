// Package tools registers MCP tools on the server.
package tools

import (
	"log/slog"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/smartnpc/smartnpc-mcp/internal/bridge"
)

// RegisterAll wires every available tool group onto the given server.
//
// br may be nil; mod-backed tools (chat_say, mail_send, game_*, npc_*) are
// then omitted so that the server is still usable for the meta `ping` tool
// and inter-NPC messaging alone (which run entirely in-process).
//
// hermes, when non-nil, is the same bridge.EventHandler used for forwarding
// mod events to the Hermes Gateway. Inter-NPC messaging tools fan their
// synthetic events through it so the recipient's Hermes profile is woken
// up immediately. Pass nil when running without a Hermes backend.
//
// logger is used for non-fatal notification delivery failures in the
// inter-NPC message fan-out. When nil, slog.Default() is assumed.
func RegisterAll(s *mcp.Server, br *bridge.WSClient, hermes bridge.EventHandler, logger *slog.Logger) {
	if logger == nil {
		logger = slog.Default()
	}
	registerMeta(s)
	registerNpcMessage(s, logger, hermes)
	if br != nil {
		registerMail(s, br)
		registerChat(s, br)
		registerGameQuery(s, br)
		registerNpcPerception(s, br)
		registerNpcMovement(s, br)
		registerNpcBehavior(s, br)
		registerPlayerQuery(s, br)
	}
}
