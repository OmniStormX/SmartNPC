// agent_register_self — bind the current MCP session to an NPC profile.
//
// Every Hermes profile MUST call this tool exactly once at startup before
// invoking any game-state tool. The mapping (mcp.ServerSession.ID() → NPC
// name) is consulted by WSClient.Call to stamp the outbound ws Request
// frame's `from_npc` field, so the SMAPI mod can route inbound debug
// mirrors to the correct chat-panel conversation regardless of whether the
// tool's own params carry an `npc` argument.
//
// Without this registration, NPC-agnostic queries like game_get_time /
// game_get_weather / mail_send arrive at the mod with no way to attribute
// them back to the originating profile.

package tools

import (
	"context"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/OmniStormX/SmartNPC/adapters/stardew/bridge"
)

// callCtx wraps the incoming MCP context with the session id from req so
// the underlying WSClient.Call can stamp the outbound ws Request frame's
// from_npc field via the per-session agent registry.
//
// Every tool handler that calls br.Call MUST pass through this helper —
// otherwise the mod side cannot route inbound debug mirrors back to the
// originating NPC's chat-panel conversation.
//
//nolint:unused // referenced from every adapters/stardew/tools/*.go
func callCtx(ctx context.Context, req *mcp.CallToolRequest) context.Context {
	if req == nil {
		return ctx
	}
	sess := req.GetSession()
	if sess == nil {
		return ctx
	}
	return bridge.WithCallSession(ctx, sess.ID())
}

// AgentRegisterSelfInput identifies the calling profile.
type AgentRegisterSelfInput struct {
	NPC string `json:"npc" jsonschema:"NPC profile name driving this MCP session, e.g. \"XiaMi\" or \"Abigail\". Must match the npc_filter / SMAPI character name used elsewhere in the bridge."`
}

// AgentRegisterSelfOutput reports back the bound NPC + session id (mostly for
// debugging — the LLM doesn't need it).
type AgentRegisterSelfOutput struct {
	OK        bool   `json:"ok"         jsonschema:"true on success"`
	NPC       string `json:"npc"        jsonschema:"the NPC name now bound to this session"`
	SessionID string `json:"session_id" jsonschema:"MCP server session id (echo)"`
}

// errAgentRegisterNoNPC is returned when input.NPC is empty.
var errAgentRegisterNoNPC = errors.New("agent_register_self: npc is required")

// registerAgent installs the agent_register_self tool. br must be non-nil:
// the session→NPC registry lives on it.
func registerAgent(s *mcp.Server, br *bridge.WSClient) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "agent_register_self",
		Description: "Bind this MCP session to a specific NPC profile. " +
			"Every Hermes profile MUST call this exactly once on startup, before any " +
			"other tool. The bridge uses the binding to stamp every outbound game " +
			"command with the originating NPC, so the in-game debug mirror can route " +
			"each tool call to that NPC's chat-panel conversation — even for " +
			"NPC-agnostic queries (game_get_time, game_get_weather, mail_send, etc.) " +
			"whose params carry no `npc` field.\n\n" +
			"When to call: as the very first tool call of the session. Idempotent: " +
			"calling again with the same `npc` is a no-op; calling with a different " +
			"`npc` overwrites the binding (don't do that unless you know why).\n\n" +
			"Side-effect: WRITE (mcp-internal registry only; no game state touched).",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in AgentRegisterSelfInput) (*mcp.CallToolResult, AgentRegisterSelfOutput, error) {
		logToolCall("agent_register_self", in)
		if in.NPC == "" {
			return nil, AgentRegisterSelfOutput{}, errAgentRegisterNoNPC
		}
		sid := req.GetSession().ID()
		// RegisterAgent maps empty sid to a synthetic single-session key
		// so stdio / InMemoryTransport callers still get a working
		// binding. Always succeeds when in.NPC is non-empty.
		_ = br.RegisterAgent(sid, in.NPC)
		return nil, AgentRegisterSelfOutput{
			OK:        true,
			NPC:       in.NPC,
			SessionID: sid,
		}, nil
	})
}


