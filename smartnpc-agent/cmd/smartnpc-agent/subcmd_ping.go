package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/smartnpc/smartnpc-agent/internal/mcpclient"
)

func runPing(ctx context.Context, cli *mcpclient.Client, args []string) error {
	fs := flag.NewFlagSet("ping", flag.ExitOnError)
	msg := fs.String("message", "hello from smartnpc-agent", "echo payload")
	if err := fs.Parse(args); err != nil {
		return err
	}

	res, err := cli.Session().CallTool(ctx, &mcp.CallToolParams{
		Name:      "ping",
		Arguments: map[string]any{"message": *msg},
	})
	if err != nil {
		return fmt.Errorf("call tool: %w", err)
	}
	if res.IsError {
		return fmt.Errorf("tool returned error: %v", res.Content)
	}

	// Prefer structured content when available.
	if res.StructuredContent != nil {
		b, _ := json.MarshalIndent(res.StructuredContent, "", "  ")
		fmt.Println(string(b))
		return nil
	}
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			fmt.Println(tc.Text)
		}
	}
	return nil
}
