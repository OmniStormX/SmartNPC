package tools

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

// registerMeta installs introspection / liveness tools.
func registerMeta(s *mcp.Server) {
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
