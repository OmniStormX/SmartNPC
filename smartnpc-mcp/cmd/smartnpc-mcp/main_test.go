package main

import (
	"context"
	"encoding/json"
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
	tools.RegisterAll(server, nil, nil) // no bridge — only meta + in-process tools

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

func TestSynthChatMessageFromAudible(t *testing.T) {
	tests := []struct {
		name     string
		payload  string
		wantOK   bool
		wantNPC  string
		wantText string
	}{
		{
			name:     "nearest_npc_picked",
			payload:  `{"text":"hi","source":"player","audible_npcs":[{"name":"XiaMi","distance":2.0},{"name":"Abigail","distance":4.0}]}`,
			wantOK:   true,
			wantNPC:  "XiaMi",
			wantText: "hi",
		},
		{
			name:    "empty_audible_drops",
			payload: `{"text":"hi","source":"player","audible_npcs":[]}`,
			wantOK:  false,
		},
		{
			name:    "no_audible_field_drops",
			payload: `{"text":"hi","source":"player"}`,
			wantOK:  false,
		},
		{
			name:    "empty_text_drops",
			payload: `{"text":"","source":"player","audible_npcs":[{"name":"XiaMi"}]}`,
			wantOK:  false,
		},
		{
			name:    "first_entry_name_empty_drops",
			payload: `{"text":"hi","source":"player","audible_npcs":[{"name":""}]}`,
			wantOK:  false,
		},
		{
			name:    "malformed_json_drops",
			payload: `not json`,
			wantOK:  false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out, ok := synthChatMessageFromAudible(json.RawMessage(tc.payload))
			if ok != tc.wantOK {
				t.Fatalf("ok = %v want %v", ok, tc.wantOK)
			}
			if !tc.wantOK {
				return
			}
			var got struct {
				NPC    string `json:"npc"`
				Target string `json:"target"`
				Text   string `json:"text"`
				Source string `json:"source"`
			}
			if err := json.Unmarshal(out, &got); err != nil {
				t.Fatalf("unmarshal synth: %v", err)
			}
			if got.NPC != tc.wantNPC || got.Target != tc.wantNPC {
				t.Errorf("npc/target = %q/%q want both %q", got.NPC, got.Target, tc.wantNPC)
			}
			if got.Text != tc.wantText {
				t.Errorf("text = %q want %q", got.Text, tc.wantText)
			}
			if got.Source != "player" {
				t.Errorf("source = %q want player", got.Source)
			}
		})
	}
}
