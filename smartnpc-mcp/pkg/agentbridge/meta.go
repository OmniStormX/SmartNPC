package agentbridge

import (
	"context"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// PingInput is the request payload for the `ping` tool.
type PingInput struct {
	Message string `json:"message,omitempty" jsonschema:"optional echo payload"`
}

// PingOutput is the structured response of the `ping` tool.
type PingOutput struct {
	OK        bool   `json:"ok"           jsonschema:"true on success"`
	Echo      string `json:"echo"         jsonschema:"echoed back from input.message"`
	ServerNow string `json:"serverNow"    jsonschema:"server timestamp in RFC3339"`
}

// RegisterMeta installs the framework-level introspection / liveness tools
// onto the given mcp.Server. Today: a single `ping` tool that adapters and
// MCP clients can rely on regardless of whether a domain adapter is wired.
//
// Adapter-agnostic: ping never touches game state, so it works during
// startup, when no save is loaded, and on bridges that have no event source
// attached at all.
func RegisterMeta(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "ping",
		Description: "Liveness check. Returns the server's current UTC timestamp and " +
			"echoes back `message`. Use during startup, health checks, or to verify " +
			"the MCP connection is alive before attempting game-state tools.\n\n" +
			"Side-effect: READ — no game state is touched; works even when no save is loaded.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in PingInput) (*mcp.CallToolResult, PingOutput, error) {
		return nil, PingOutput{
			OK:        true,
			Echo:      in.Message,
			ServerNow: time.Now().UTC().Format(time.RFC3339Nano),
		}, nil
	})
}
