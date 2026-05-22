package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/OmniStormX/SmartNPC/adapters/stardew/bridge"
)

// playerQueryClientServer mirrors newClientServer (in game_query_test.go)
// but only registers the player_query tool group. Keeps the test surface
// tight so a regression here doesn't show up as game-state noise.
func playerQueryClientServer(t *testing.T, h bridge.TestActionHandler) (*mcp.ClientSession, context.Context, func()) {
	t.Helper()
	srv := bridge.NewTestServer(h)
	br := bridge.NewWSClient(bridge.WSClientOptions{URL: srv.URL_WS()})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	if err := br.Connect(ctx); err != nil {
		srv.Close()
		cancel()
		t.Fatalf("ws connect: %v", err)
	}

	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "test"}, nil)
	registerPlayerQuery(server, br)

	t1, t2 := mcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, t1, nil); err != nil {
		cancel()
		br.Close()
		srv.Close()
		t.Fatalf("server connect: %v", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "t"}, nil)
	cs, err := client.Connect(ctx, t2, nil)
	if err != nil {
		cancel()
		br.Close()
		srv.Close()
		t.Fatalf("client connect: %v", err)
	}
	return cs, ctx, func() {
		cs.Close()
		br.Close()
		srv.Close()
		cancel()
	}
}

func TestPlayerGetStatus_ListTool(t *testing.T) {
	cs, ctx, cleanup := playerQueryClientServer(t, func(context.Context, string, json.RawMessage) (any, error) {
		return nil, nil
	})
	defer cleanup()

	listed, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	var found bool
	for _, tool := range listed.Tools {
		if tool.Name == "player_get_status" {
			found = true
			if tool.Description == "" {
				t.Error("player_get_status: empty description")
			}
			if tool.InputSchema == nil {
				t.Error("player_get_status: nil InputSchema")
			}
		}
	}
	if !found {
		t.Error("player_get_status not advertised by ListTools")
	}
}

func TestPlayerGetStatus_HappyPath(t *testing.T) {
	cs, ctx, cleanup := playerQueryClientServer(t, func(_ context.Context, action string, _ json.RawMessage) (any, error) {
		if action != bridge.ActionPlayerGetStatus {
			t.Errorf("action=%s want %s", action, bridge.ActionPlayerGetStatus)
		}
		return map[string]any{
			"ok":        true,
			"busy":      false,
			"in_menu":   false,
			"in_event":  false,
			"is_moving": true,
			"location":  "Farm",
		}, nil
	})
	defer cleanup()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "player_get_status"})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError: %v", res.Content)
	}
	b, _ := json.Marshal(res.StructuredContent)
	var out PlayerGetStatusOutput
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !out.OK {
		t.Error("expected ok=true")
	}
	if !out.IsMoving {
		t.Error("expected is_moving=true")
	}
	if out.Location != "Farm" {
		t.Errorf("location=%q want Farm", out.Location)
	}
}

func TestPlayerGetStatus_BusyComposite(t *testing.T) {
	// The mod is the source of truth for what `busy` means; we just
	// pass the booleans through. This test pins the contract: when
	// the mod reports busy=true we surface it verbatim.
	cs, ctx, cleanup := playerQueryClientServer(t, func(context.Context, string, json.RawMessage) (any, error) {
		return map[string]any{
			"ok":        true,
			"busy":      true,
			"in_menu":   true,
			"in_event":  false,
			"is_moving": false,
			"location":  "ShopMenu",
		}, nil
	})
	defer cleanup()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "player_get_status"})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	b, _ := json.Marshal(res.StructuredContent)
	var out PlayerGetStatusOutput
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !out.Busy || !out.InMenu {
		t.Errorf("busy/in_menu not propagated: %+v", out)
	}
}

func TestPlayerGetStatus_ModError(t *testing.T) {
	cs, ctx, cleanup := playerQueryClientServer(t, func(context.Context, string, json.RawMessage) (any, error) {
		return nil, fmt.Errorf("no save loaded")
	})
	defer cleanup()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "player_get_status"})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if !res.IsError {
		t.Error("expected IsError=true when mod reports failure")
	}
}
