package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/OmniStormX/SmartNPC/adapters/stardew/bridge"
	"github.com/OmniStormX/SmartNPC/adapters/stardew/scheduler"
	"github.com/OmniStormX/SmartNPC/pkg/relay/hermes"
	"github.com/OmniStormX/SmartNPC/adapters/stardew/tools"
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
	br.SetEventHandler(makeRouter(mcpServer, logger, br, false, "", relay.HandleEvent, nil, nil, false))

	// Register tools so the MCP server is realistic — they aren't
	// exercised in this test but ensure RegisterAll didn't change shape.
	_ = tools.RegisterAll(mcpServer, br, nil, nil, logger)

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
	br.SetEventHandler(makeRouter(mcpServer, logger, br, false, "", relay.HandleEvent, nil, nil, false))

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
	br.SetEventHandler(makeRouter(mcpServer, logger, br, false, "", nil, nil, nil, false)) // ← relay disabled

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
	br.SetEventHandler(makeRouter(mcpServer, logger, br, false, "", relay.HandleEvent, nil, nil, false))

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

func TestPipeline_HermesConfigMultiProfile(t *testing.T) {
	// Two fake Hermes gateways (one for XiaMi, one for Abigail). Each
	// counts the requests it receives. We then synthesize a chat_message
	// event for Abigail and assert only her gateway is hit.

	var xiamiHits, abigailHits atomic.Int32

	makeGW := func(hits *atomic.Int32) *httptest.Server {
		mux := http.NewServeMux()
		mux.HandleFunc("/v1/responses", func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.Copy(io.Discard, r.Body)
			hits.Add(1)
			w.WriteHeader(http.StatusOK)
		})
		return httptest.NewServer(mux)
	}
	xiamiGW := makeGW(&xiamiHits)
	defer xiamiGW.Close()
	abigailGW := makeGW(&abigailHits)
	defer abigailGW.Close()

	// Write a runtime-config.yaml fixture pointing at both gateways.
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "runtime-config.yaml")
	yamlBody := fmt.Sprintf(`profiles:
  - name: xiami
    npc_filter: XiaMi
    gateway_url: %s
    conversation: xiami
    model: hermes-agent
  - name: abigail
    npc_filter: Abigail
    gateway_url: %s
    conversation: abigail
    model: hermes-agent
`, xiamiGW.URL, abigailGW.URL)
	if err := os.WriteFile(cfgPath, []byte(yamlBody), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfgs, err := hermesrelay.LoadConfigFile(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfigFile: %v", err)
	}
	group, err := hermesrelay.NewGroup(cfgs, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewGroup: %v", err)
	}

	// Synthesize a chat_message for Abigail and dispatch via the group.
	group.HandleEvent(context.Background(), "chat_message",
		json.RawMessage(`{"npc":"Abigail","text":"hi","source":"player"}`))

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if abigailHits.Load() == 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got := abigailHits.Load(); got != 1 {
		t.Errorf("abigail gateway hits = %d, want 1", got)
	}
	if got := xiamiHits.Load(); got != 0 {
		t.Errorf("xiami gateway hits = %d, want 0 (cross-pollination!)", got)
	}
}

// TestPipeline_GameTimeTickFiresScheduleTrigger verifies the full path:
// game_time_tick event → scheduler.Tick → schedule_trigger → hermesrelay.
func TestPipeline_GameTimeTickFiresScheduleTrigger(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))

	// Fake Hermes Gateway — captures POSTs.
	var mu sync.Mutex
	var bodies []struct {
		Input string
		NPC   string
	}
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var req struct {
			Input        string `json:"input"`
			Conversation string `json:"conversation_id"`
		}
		_ = json.Unmarshal(raw, &req)
		mu.Lock()
		bodies = append(bodies, struct {
			Input string
			NPC   string
		}{Input: req.Input, NPC: req.Conversation})
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer gw.Close()

	// Build relay config pointing at fake gateway, NPC filter = XiaMi.
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	cfgContent := fmt.Sprintf(`profiles:
  - npc_name: XiaMi
    gateway_url: %s/v1/responses
    api_key: test
    conversation: xiami
    model: test-model
`, gw.URL)
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfgs, err := hermesrelay.LoadConfigFile(cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	group, err := hermesrelay.NewGroup(cfgs, logger)
	if err != nil {
		t.Fatalf("new group: %v", err)
	}

	// Fake SMAPI mod ws server.
	mod := bridge.NewTestServer(func(context.Context, string, json.RawMessage) (any, error) {
		return map[string]any{"ok": true}, nil
	})
	defer mod.Close()

	mcpServer := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "t"}, nil)

	// Create scheduler and pre-populate a schedule for XiaMi.
	sched := tools.RegisterAll(mcpServer, nil, nil, nil, logger)
	sched.SetSchedule(scheduler.DaySchedule{
		NPC:    "XiaMi",
		Day:    15,
		Season: "spring",
		Year:   1,
		Entries: []scheduler.Entry{
			{GameHour: 9, Action: "npc_water_crops", Reason: "早起浇水"},
		},
	})

	// Wire the router with the scheduler.
	br := bridge.NewWSClient(bridge.WSClientOptions{URL: mod.URL_WS(), Logger: logger})
	br.SetEventHandler(makeRouter(mcpServer, logger, br, false, "", group.HandleEvent, nil, sched, false))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := br.Connect(ctx); err != nil {
		t.Fatalf("bridge connect: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	// Push a game_time_tick event for hour 9.
	if err := mod.PushEvent(bridge.EventGameTimeTick, map[string]any{
		"hour": 9,
	}); err != nil {
		t.Fatalf("push event: %v", err)
	}

	// Wait for Hermes to receive the POST.
	deadline := time.After(3 * time.Second)
	for {
		mu.Lock()
		n := len(bodies)
		mu.Unlock()
		if n >= 1 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("hermes gateway never received schedule_trigger POST")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if len(bodies) < 1 {
		t.Fatal("no POST received")
	}
	// The schedule_trigger payload should reference the action.
	found := false
	for _, b := range bodies {
		if strings.Contains(b.Input, "npc_water_crops") || strings.Contains(b.Input, "schedule_trigger") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected schedule_trigger content in POST bodies, got: %+v", bodies)
	}

	// Verify the schedule entry was marked fired (a second tick should not re-fire).
	pending := sched.PendingEntries("XiaMi")
	if len(pending) != 0 {
		t.Errorf("expected 0 pending after tick, got %d", len(pending))
	}
}
