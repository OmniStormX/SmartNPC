package main

import (
	"context"
	"fmt"

	"github.com/smartnpc/smartnpc-agent/internal/mcpclient"
)

func runListTools(ctx context.Context, cli *mcpclient.Client) error {
	res, err := cli.Session().ListTools(ctx, nil)
	if err != nil {
		return fmt.Errorf("list tools: %w", err)
	}
	fmt.Printf("Found %d tools:\n", len(res.Tools))
	for _, t := range res.Tools {
		fmt.Printf("  - %-30s %s\n", t.Name, t.Description)
	}
	return nil
}
