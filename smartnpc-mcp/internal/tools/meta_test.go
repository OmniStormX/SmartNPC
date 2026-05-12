package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestPingEndToEnd wires up an in-memory MCP server + client, calls `ping`,
// and verifies both the listing and the structured tool result. It also
// catches any breakage of the AddTool generic signature against the live SDK.
func TestPingEndToEnd(t *testing.T) {
	ctx := context.Background()

	server := mcp.NewServer(&mcp.Implementation{
		Name: "smartnpc-mcp-test", Version: "test",
	}, nil)
	RegisterAll(server, nil, nil)

	t1, t2 := mcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, t1, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}

	client := mcp.NewClient(&mcp.Implementation{
		Name: "smartnpc-agent-test", Version: "test",
	}, nil)
	cs, err := client.Connect(ctx, t2, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer cs.Close()

	// 1) ListTools must include `ping`.
	listed, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	var found bool
	for _, tool := range listed.Tools {
		if tool.Name == "ping" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected `ping` in tools list, got %v", listed.Tools)
	}

	// 2) CallTool returns a structured result with the expected echo.
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ping",
		Arguments: map[string]any{"message": "hello-test"},
	})
	if err != nil {
		t.Fatalf("call ping: %v", err)
	}
	if res.IsError {
		t.Fatalf("ping returned error: %v", res.Content)
	}
	if res.StructuredContent == nil {
		t.Fatalf("expected structured content, got nil; content=%v", res.Content)
	}
	b, _ := json.Marshal(res.StructuredContent)
	var out PingOutput
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal structured: %v (raw=%s)", err, b)
	}
	if !out.OK {
		t.Errorf("OK=false")
	}
	if out.Echo != "hello-test" {
		t.Errorf("Echo=%q want %q", out.Echo, "hello-test")
	}
	if !strings.Contains(out.ServerNow, "T") {
		t.Errorf("ServerNow not RFC3339-ish: %q", out.ServerNow)
	}
}
