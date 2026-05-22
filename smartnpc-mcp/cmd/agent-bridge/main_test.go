package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/OmniStormX/SmartNPC/pkg/agentbridge"
)

// TestAssemble_StardewEcho verifies bridge.yaml with the stardew adapter
// + echo backend assembles successfully via the side-effect-imported
// registry. Does NOT actually start the ws connection (Adapter.Start
// is the network boundary; we only test up to Assemble).
func TestAssemble_StardewEcho(t *testing.T) {
	body := `adapters:
  - kind: stardew
    config:
      ws_url: ws://127.0.0.1:18745/ws
relays:
  - kind: echo
transport:
  kind: stdio
`
	path := filepath.Join(t.TempDir(), "bridge.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write yaml: %v", err)
	}

	cfg, err := agentbridge.LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	mcpServer := mcp.NewServer(&mcp.Implementation{Name: "t", Version: "0"}, nil)
	srv, err := cfg.Assemble(mcpServer, nil)
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	_ = srv
}

// TestAssemble_UnknownAdapter exercises the "unknown kind" error path
// to make sure the registered-kinds list shows up in the message.
func TestAssemble_UnknownAdapter(t *testing.T) {
	body := `adapters:
  - kind: nonexistent
transport:
  kind: stdio
`
	path := filepath.Join(t.TempDir(), "bridge.yaml")
	_ = os.WriteFile(path, []byte(body), 0o644)

	cfg, err := agentbridge.LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	mcpServer := mcp.NewServer(&mcp.Implementation{Name: "t", Version: "0"}, nil)
	_, err = cfg.Assemble(mcpServer, nil)
	if err == nil || !strings.Contains(err.Error(), "stardew") {
		t.Errorf("expected error mentioning known adapters incl. stardew; got %v", err)
	}
}
