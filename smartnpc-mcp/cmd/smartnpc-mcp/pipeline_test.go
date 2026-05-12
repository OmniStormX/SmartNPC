package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/smartnpc/smartnpc-mcp/internal/bridge"
	"github.com/smartnpc/smartnpc-mcp/internal/hermesrelay"
	"github.com/smartnpc/smartnpc-mcp/internal/tools"
)

// TestPipeline_ChatMessageReachesHermes wires the full server-side
// pipeline (fake SMAPI ws → mcp bridge → makeRouter → hermesrelay → fake
// Hermes Gateway) and verifies a `chat_message` event lands as an OpenAI
// /v1/responses POST with the right shape.
//
// This is the M5.6 / M5.7 regression test: real wire path, mocked
// endpoints, no game required.
func TestPipeline_ChatMessageReachesHermes(t *testing.T) {
	// Fake Hermes Gateway — captures the POST.
	var got struct {
		path  string
		auth  string
		input string
		conv  string
		model string
		instr string
	}
	gotWG := &sync.WaitGroup{}
	gotWG.Add(1)
	hermes := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer gotWG.Done()
		body, _ := io.ReadAll(r.Body)
		var p struct {
			Model        string `json:"model"`
			Input        string `json:"input"`
			Conversation string `json:"conversation"`
			Instructions string `json:"instructions"`
		}
		_ = json.Unmarshal(body, &p)
		got.path = r.URL.Path
		got.auth = r.Header.Get("Authorization")
		got.input = p.Input
		got.conv = p.Conversation
		got.model = p.Model
		got.instr = p.Instructions
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_x","object":"response"}`))
	}))
	defer hermes.Close()

	// Construct the relay pointed at the fake Hermes.
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	relay, err := hermesrelay.New(hermesrelay.Config{
		URL:          hermes.URL,
		APIKey:       "test-key",
		Conversation: "xiami",
		Model:        "xiami",
		NPCName:      "XiaMi",
		Timeout:      3 * time.Second,
	}, logger)
	if err != nil {
		t.Fatalf("relay: %v", err)
	}

	// Fake SMAPI mod ws server.
	mod := bridge.NewTestServer(func(_ context.Context, action string, _ json.RawMessage) (any, error) {
		// We don't issue any requests in this test, but the handler
		// must exist for the server to accept connections.
		t.Logf("unexpected mod-side request: %s", action)
		return map[string]any{"ok": true}, nil
	})
	defer mod.Close()

	// MCP server (so makeRouter has a forwarder target).
	mcpServer := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "t"}, nil)

	// Bridge ws client, with the same makeRouter the production main() uses.
	br := bridge.NewWSClient(bridge.WSClientOptions{URL: mod.URL_WS(), Logger: logger})
	br.SetEventHandler(makeRouter(mcpServer, logger, br, false, "", relay))

	// Register tools so the MCP server is realistic — they aren't
	// exercised in this test but ensure RegisterAll didn't change shape.
	tools.RegisterAll(mcpServer, br, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := br.Connect(ctx); err != nil {
		t.Fatalf("bridge connect: %v", err)
	}
	// Wait briefly for the server to register the connection.
	time.Sleep(100 * time.Millisecond)

	// Inject a chat_message event from the fake mod.
	if err := mod.PushEvent(bridge.EventChatMessage, map[string]any{
		"npc":    "XiaMi",
		"target": "XiaMi",
		"text":   "你好",
		"source": "player",
	}); err != nil {
		t.Fatalf("push event: %v", err)
	}

	// Wait for Hermes to receive the POST.
	done := make(chan struct{})
	go func() { gotWG.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("fake Hermes never received a POST")
	}

	if got.path != "/v1/responses" {
		t.Errorf("path = %q want /v1/responses", got.path)
	}
	if got.auth != "Bearer test-key" {
		t.Errorf("auth = %q want Bearer test-key", got.auth)
	}
	if got.model != "xiami" {
		t.Errorf("model = %q want xiami", got.model)
	}
	if got.conv != "xiami" {
		t.Errorf("conversation = %q want xiami", got.conv)
	}
	if !strings.Contains(got.input, "你好") {
		t.Errorf("input missing player text: %q", got.input)
	}
}

// TestPipeline_NonMatchingNPCDropped verifies the relay's NPC filter
// works end-to-end through makeRouter — a chat_message for Abigail must
// NOT POST to XiaMi's gateway.
func TestPipeline_NonMatchingNPCDropped(t *testing.T) {
	var posts int
	var mu sync.Mutex
	hermes := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		posts++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer hermes.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	relay, err := hermesrelay.New(hermesrelay.Config{
		URL:          hermes.URL,
		Conversation: "xiami",
		Model:        "xiami",
		NPCName:      "XiaMi",
		Timeout:      1 * time.Second,
	}, logger)
	if err != nil {
		t.Fatalf("relay: %v", err)
	}

	mod := bridge.NewTestServer(func(context.Context, string, json.RawMessage) (any, error) {
		return map[string]any{"ok": true}, nil
	})
	defer mod.Close()

	mcpServer := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "t"}, nil)
	br := bridge.NewWSClient(bridge.WSClientOptions{URL: mod.URL_WS(), Logger: logger})
	br.SetEventHandler(makeRouter(mcpServer, logger, br, false, "", relay))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := br.Connect(ctx); err != nil {
		t.Fatalf("bridge connect: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	if err := mod.PushEvent(bridge.EventChatMessage, map[string]any{
		"npc":    "Abigail",
		"target": "Abigail",
		"text":   "hi",
		"source": "player",
	}); err != nil {
		t.Fatalf("push event: %v", err)
	}

	// Give any rogue goroutine time to misroute. Capped at 100ms per
	// CLAUDE.md test-discipline rule; the relay POST path is fast enough
	// that a real bug would still race within this window.
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if posts != 0 {
		t.Errorf("expected 0 POSTs (NPC filter), got %d", posts)
	}
}

// TestPipeline_RelayOff verifies that when relay is nil, events flow
// through makeRouter without triggering any outbound HTTP at all.
func TestPipeline_RelayOff(t *testing.T) {
	var posts int
	hermes := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		posts++
		w.WriteHeader(http.StatusOK)
	}))
	defer hermes.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	mod := bridge.NewTestServer(func(context.Context, string, json.RawMessage) (any, error) {
		return map[string]any{"ok": true}, nil
	})
	defer mod.Close()

	mcpServer := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "t"}, nil)
	br := bridge.NewWSClient(bridge.WSClientOptions{URL: mod.URL_WS(), Logger: logger})
	br.SetEventHandler(makeRouter(mcpServer, logger, br, false, "", nil)) // ← relay disabled

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := br.Connect(ctx); err != nil {
		t.Fatalf("bridge connect: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	if err := mod.PushEvent(bridge.EventChatMessage, map[string]any{
		"npc": "XiaMi", "text": "hi", "source": "player",
	}); err != nil {
		t.Fatalf("push event: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	if posts != 0 {
		t.Errorf("expected 0 POSTs when relay is nil, got %d", posts)
	}
}

// TestPipeline_AudibleChatReceivedSynthesizesChatMessage verifies the
// Go-side audible-routing policy: a chat_received event carrying a
// non-empty audible_npcs list lands in the relay as a chat_message
// targeting the closest audible NPC. The original chat_received must NOT
// also POST (avoiding double-delivery).
func TestPipeline_AudibleChatReceivedSynthesizesChatMessage(t *testing.T) {
	var bodies []struct {
		Input string `json:"input"`
	}
	var mu sync.Mutex
	wg := &sync.WaitGroup{}
	wg.Add(1)
	hermes := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var p struct {
			Input string `json:"input"`
		}
		_ = json.Unmarshal(body, &p)
		mu.Lock()
		bodies = append(bodies, p)
		first := len(bodies) == 1
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_x","object":"response"}`))
		if first {
			wg.Done()
		}
	}))
	defer hermes.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	relay, err := hermesrelay.New(hermesrelay.Config{
		URL:          hermes.URL,
		Conversation: "xiami",
		Model:        "xiami",
		NPCName:      "XiaMi",
		Timeout:      2 * time.Second,
	}, logger)
	if err != nil {
		t.Fatalf("relay: %v", err)
	}

	mod := bridge.NewTestServer(func(context.Context, string, json.RawMessage) (any, error) {
		return map[string]any{"ok": true}, nil
	})
	defer mod.Close()

	mcpServer := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "t"}, nil)
	br := bridge.NewWSClient(bridge.WSClientOptions{URL: mod.URL_WS(), Logger: logger})
	br.SetEventHandler(makeRouter(mcpServer, logger, br, false, "", relay))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := br.Connect(ctx); err != nil {
		t.Fatalf("bridge connect: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	// Push chat_received with XiaMi as the closest audible NPC.
	if err := mod.PushEvent(bridge.EventChatReceived, map[string]any{
		"text":   "你好",
		"source": "player",
		"audible_npcs": []map[string]any{
			{"name": "XiaMi", "distance": 2.0},
			{"name": "Abigail", "distance": 5.0},
		},
	}); err != nil {
		t.Fatalf("push event: %v", err)
	}

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("relay never received the synthesized chat_message")
	}

	// Give a tick for any erroneous second POST to land.
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 1 {
		t.Fatalf("expected exactly 1 POST (synth only), got %d: %+v", len(bodies), bodies)
	}
	if !strings.Contains(bodies[0].Input, "你好") {
		t.Errorf("input missing player text: %q", bodies[0].Input)
	}
	// FormatForHermes for chat_message renders as "Farmer says to you: 你好".
	if !strings.Contains(bodies[0].Input, "says to you") {
		t.Errorf("expected chat_message phrasing, got: %q", bodies[0].Input)
	}
}
