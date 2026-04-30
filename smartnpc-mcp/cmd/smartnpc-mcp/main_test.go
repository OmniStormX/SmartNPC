package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/smartnpc/smartnpc-mcp/internal/tools"
)

// TestHTTPTransport_PingRoundTrip wires the same MCP server we serve in
// production over Streamable HTTP, opens a real client connection through
// httptest, and verifies the meta `ping` tool is callable.
//
// This is the smoke test for `--http` mode used by Hermes in WSL.
func TestHTTPTransport_PingRoundTrip(t *testing.T) {
	server := mcp.NewServer(&mcp.Implementation{
		Name: "smartnpc-mcp-test", Version: "test",
	}, nil)
	tools.RegisterAll(server, nil) // no bridge — only meta tools

	handler := mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return server },
		&mcp.StreamableHTTPOptions{},
	)
	mux := http.NewServeMux()
	mux.Handle("/mcp", handler)
	mux.Handle("/mcp/", handler)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client := mcp.NewClient(&mcp.Implementation{Name: "client", Version: "test"}, nil)
	transport := &mcp.StreamableClientTransport{Endpoint: ts.URL + "/mcp"}
	cs, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer cs.Close()

	// ListTools must include ping.
	listed, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	var found bool
	for _, tl := range listed.Tools {
		if tl.Name == "ping" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("ping tool not advertised over HTTP transport")
	}

	// Call ping and verify the structured response.
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ping",
		Arguments: map[string]any{"message": "http-mode"},
	})
	if err != nil {
		t.Fatalf("call ping: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool returned error: %v", res.Content)
	}
	if res.StructuredContent == nil {
		t.Fatal("expected structured content")
	}
}
