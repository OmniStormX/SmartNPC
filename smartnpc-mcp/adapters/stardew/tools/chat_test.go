package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/OmniStormX/SmartNPC/adapters/stardew/bridge"
)

// chatSayTestRig wires an in-memory MCP server + client + a fake mod ws server
// behind a real bridge.WSClient. guard is what registerChat sees; pass nil to
// disable runtime quota enforcement. delivered counts the number of chat_say
// calls the mod actually saw (the test asserts on it to distinguish "rejected
// before hitting the bridge" from "delivered").
type chatSayTestRig struct {
	cs        *mcp.ClientSession
	delivered *int
	cleanup   func()
}

func newChatSayRig(t *testing.T, guard *ChatSayGuard) *chatSayTestRig {
	t.Helper()
	delivered := 0
	srv := bridge.NewTestServer(func(_ context.Context, action string, _ json.RawMessage) (any, error) {
		if action == bridge.ActionChatSay {
			delivered++
		}
		return ChatSayOutput{OK: true}, nil
	})
	br := bridge.NewWSClient(bridge.WSClientOptions{URL: srv.URL_WS()})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	if err := br.Connect(ctx); err != nil {
		cancel()
		srv.Close()
		t.Fatalf("connect: %v", err)
	}

	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "test"}, nil)
	registerChat(server, br, guard)
	t1, t2 := mcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, t1, nil); err != nil {
		cancel()
		br.Close()
		srv.Close()
		t.Fatalf("server: %v", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "t"}, nil)
	cs, err := client.Connect(ctx, t2, nil)
	if err != nil {
		cancel()
		br.Close()
		srv.Close()
		t.Fatalf("client: %v", err)
	}
	return &chatSayTestRig{
		cs:        cs,
		delivered: &delivered,
		cleanup: func() {
			cs.Close()
			cancel()
			br.Close()
			srv.Close()
		},
	}
}

func (r *chatSayTestRig) call(t *testing.T, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	res, err := r.cs.CallTool(ctx, &mcp.CallToolParams{Name: "chat_say", Arguments: args})
	if err != nil {
		t.Fatalf("CallTool transport: %v", err)
	}
	return res
}

func TestChatSayEndToEnd(t *testing.T) {
	var got ChatSayInput
	srv := bridge.NewTestServer(func(_ context.Context, action string, params json.RawMessage) (any, error) {
		if action != bridge.ActionChatSay {
			t.Errorf("action=%s", action)
		}
		_ = json.Unmarshal(params, &got)
		return ChatSayOutput{OK: true}, nil
	})
	defer srv.Close()

	br := bridge.NewWSClient(bridge.WSClientOptions{URL: srv.URL_WS()})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := br.Connect(ctx); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer br.Close()

	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "test"}, nil)
	registerChat(server, br, nil)

	t1, t2 := mcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, t1, nil); err != nil {
		t.Fatalf("server: %v", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "t"}, nil)
	cs, err := client.Connect(ctx, t2, nil)
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	defer cs.Close()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "chat_say",
		Arguments: map[string]any{
			"speaker": "SmartNPC",
			"text":    "hello there",
			"color":   "yellow",
		},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError: %v", res.Content)
	}
	if got.Speaker != "SmartNPC" || got.Text != "hello there" || got.Color != "yellow" {
		t.Errorf("got=%+v", got)
	}
}

// TestChatSay_PrivateQuotaExhausted pins the "一问一答" runtime cap: when the
// guard is present, the second consecutive chat_say from the same speaker
// without an intervening wake-up event is hard-rejected — but, importantly,
// returned as a STRUCTURED no-op (ok=false + directive hint), NOT as a Go
// error. The latter would surface to Hermes as a tool failure and trigger
// the LLM's retry instinct, producing the chat_say loop we're suppressing.
func TestChatSay_PrivateQuotaExhausted(t *testing.T) {
	guard := NewChatSayGuard()
	rig := newChatSayRig(t, guard)
	defer rig.cleanup()

	args := map[string]any{"speaker": "XiaMi", "text": "你好"}

	res1 := rig.call(t, args)
	if res1.IsError {
		t.Fatalf("first private call errored unexpectedly: %v", res1.Content)
	}
	res2 := rig.call(t, args)
	if res2.IsError {
		t.Fatalf("second private call should be a structured no-op (not an MCP error), got IsError=true: %v", res2.Content)
	}
	if !errContains(res2, "noop_chat_say_private_quota_exhausted") {
		t.Errorf("expected hint to mention noop_chat_say_private_quota_exhausted, got %v", res2.Content)
	}
	if !errContains(res2, "TURN_END") {
		t.Errorf("expected hint to carry TURN_END stop signal, got %v", res2.Content)
	}
	if *rig.delivered != 1 {
		t.Errorf("expected 1 delivery (second blocked before bridge), got %d", *rig.delivered)
	}

	// A different speaker has its own private budget.
	other := map[string]any{"speaker": "Abigail", "text": "嗨"}
	if res := rig.call(t, other); res.IsError {
		t.Fatalf("different-speaker private call should succeed, got error: %v", res.Content)
	}
	if *rig.delivered != 2 {
		t.Errorf("expected 2 deliveries total, got %d", *rig.delivered)
	}
}

// TestChatSay_PrivateResetsOnInboundEvent drives the runtime flow: NPC speaks
// once → second chat_say is blocked (soft no-op) → a fresh inbound event
// addressed to that NPC refreshes the budget → third chat_say succeeds.
func TestChatSay_PrivateResetsOnInboundEvent(t *testing.T) {
	guard := NewChatSayGuard()
	rig := newChatSayRig(t, guard)
	defer rig.cleanup()

	args := map[string]any{"speaker": "XiaMi", "text": "你好"}
	if res := rig.call(t, args); res.IsError {
		t.Fatalf("first call: %v", res.Content)
	}
	if res := rig.call(t, args); !errContains(res, "noop_chat_say_private_quota_exhausted") {
		t.Fatalf("second call should be soft no-op with quota hint, got %v", res.Content)
	}

	// Simulate the router observing a fresh chat_message addressed to XiaMi
	// (the same code path that fires on the next player wake-up).
	wakeup := []byte(`{"npc":"XiaMi","target":"XiaMi","text":"再说一句","source":"player"}`)
	MaybeResetGuard(guard, bridge.EventChatMessage, wakeup)

	// Budget refreshed: same speaker, third call goes through.
	if res := rig.call(t, args); res.IsError {
		t.Fatalf("third call after reset should succeed, got error: %v", res.Content)
	}
	if *rig.delivered != 2 {
		t.Errorf("expected 2 deliveries (1st + 3rd), got %d", *rig.delivered)
	}

	// Reset only affects the recipient: an event addressed to Abigail must
	// not unblock XiaMi.
	if res := rig.call(t, args); !errContains(res, "noop_chat_say_private_quota_exhausted") {
		t.Fatalf("fourth call (no fresh reset for XiaMi) should be soft no-op, got %v", res.Content)
	}
	wrongTarget := []byte(`{"npc":"Abigail","target":"Abigail","text":"...","source":"player"}`)
	MaybeResetGuard(guard, bridge.EventChatMessage, wrongTarget)
	if res := rig.call(t, args); !errContains(res, "noop_chat_say_private_quota_exhausted") {
		t.Fatalf("fifth call should still hit quota (Abigail reset doesn't affect XiaMi), got %v", res.Content)
	}

	// An event without a recipient (e.g. chat_received w/ no audible_npcs)
	// must NOT reset either — the player_group source carries no per-NPC
	// recipient, and the broadcast shape has no `npc`/`to`/`target`.
	noRecipient := []byte(`{"text":"hi","source":"player"}`)
	MaybeResetGuard(guard, bridge.EventChatReceived, noRecipient)
	if res := rig.call(t, args); !errContains(res, "noop_chat_say_private_quota_exhausted") {
		t.Fatalf("call after non-recipient event should still hit quota, got %v", res.Content)
	}
}

// TestChatSay_GroupQuotaExhausted verifies that within a single player turn,
// the same speaker gets exactly one chat_say budget per group, and the
// over-cap second call is delivered as a structured no-op (not a Go error).
func TestChatSay_GroupQuotaExhausted(t *testing.T) {
	guard := NewChatSayGuard()
	rig := newChatSayRig(t, guard)
	defer rig.cleanup()

	args := map[string]any{
		"speaker":  "XiaMi",
		"text":     "你好",
		"channel":  "group",
		"group_id": "g1",
	}
	res1 := rig.call(t, args)
	if res1.IsError {
		t.Fatalf("first group call errored: %v", res1.Content)
	}
	res2 := rig.call(t, args)
	if res2.IsError {
		t.Fatalf("second group call should be a structured no-op (not an MCP error), got IsError=true: %v", res2.Content)
	}
	if !errContains(res2, "noop_chat_say_group_quota_exhausted") {
		t.Errorf("expected hint to mention noop_chat_say_group_quota_exhausted, got %v", res2.Content)
	}
	if !errContains(res2, "TURN_END") {
		t.Errorf("expected hint to carry TURN_END stop signal, got %v", res2.Content)
	}
	if *rig.delivered != 1 {
		t.Errorf("expected 1 delivery (second blocked before bridge), got %d", *rig.delivered)
	}

	// A different speaker in the same group has its own budget.
	other := map[string]any{
		"speaker": "Abigail", "text": "嗨", "channel": "group", "group_id": "g1",
	}
	res3 := rig.call(t, other)
	if res3.IsError {
		t.Fatalf("different-speaker group call should succeed, got error: %v", res3.Content)
	}

	// And the same speaker in a different group has its own budget too.
	otherGroup := map[string]any{
		"speaker": "XiaMi", "text": "嗨", "channel": "group", "group_id": "g2",
	}
	res4 := rig.call(t, otherGroup)
	if res4.IsError {
		t.Fatalf("same-speaker different-group call should succeed, got error: %v", res4.Content)
	}
	if *rig.delivered != 3 {
		t.Errorf("expected 3 deliveries total, got %d", *rig.delivered)
	}
}

// TestChatSay_GroupQuotaResetsOnPlayerInput drives the same flow that
// happens at runtime: NPC speaks once, hits the cap on second try (soft
// no-op), then a player line into the group resets the budget and the third
// try succeeds.
func TestChatSay_GroupQuotaResetsOnPlayerInput(t *testing.T) {
	guard := NewChatSayGuard()
	rig := newChatSayRig(t, guard)
	defer rig.cleanup()

	args := map[string]any{
		"speaker":  "XiaMi",
		"text":     "你好",
		"channel":  "group",
		"group_id": "g1",
	}
	if res := rig.call(t, args); res.IsError {
		t.Fatalf("first call: %v", res.Content)
	}
	if res := rig.call(t, args); !errContains(res, "noop_chat_say_group_quota_exhausted") {
		t.Fatalf("second call should be soft no-op with group quota hint, got %v", res.Content)
	}

	// Simulate the router seeing a player_group chat event for g1.
	playerInto := []byte(`{"text":"再说一句","source":"player_group","group_id":"g1"}`)
	MaybeResetGuard(guard, bridge.EventChatReceived, playerInto)

	// Budget refreshed: same speaker, same group, third call now goes through.
	if res := rig.call(t, args); res.IsError {
		t.Fatalf("third call after reset should succeed, got error: %v", res.Content)
	}
	if *rig.delivered != 2 {
		t.Errorf("expected 2 deliveries (1st + 3rd), got %d", *rig.delivered)
	}

	// Reset only affects the group it targeted: a player_group event for g2
	// must not unblock g1.
	if res := rig.call(t, args); !errContains(res, "noop_chat_say_group_quota_exhausted") {
		t.Fatalf("fourth call (no fresh reset for g1) should be soft no-op, got %v", res.Content)
	}
	wrongGroup := []byte(`{"text":"...","source":"player_group","group_id":"g2"}`)
	MaybeResetGuard(guard, bridge.EventChatReceived, wrongGroup)
	if res := rig.call(t, args); !errContains(res, "noop_chat_say_group_quota_exhausted") {
		t.Fatalf("fifth call should still hit quota (g2 reset doesn't affect g1), got %v", res.Content)
	}

	// Non-player-group chat_received without group_id must NOT reset the
	// group budget either.
	MaybeResetGuard(guard, bridge.EventChatReceived,
		[]byte(`{"text":"hi","source":"player_group"}`)) // missing group_id
	if res := rig.call(t, args); !errContains(res, "noop_chat_say_group_quota_exhausted") {
		t.Fatalf("call after group reset with missing group_id should still hit quota, got %v", res.Content)
	}
}

// TestChatSay_GroupCallWithoutGroupID skips the guard (defensive fallback)
// and lets the call through — the guard refuses to lock a key against an
// empty group id. The mod-side rendering will likely reject or misroute,
// but that's not this tool's concern.
//
// Caveat: with the new private guard, even though the group key is empty
// (so AllowGroup is bypassed), the channel is still treated as group via
// the isGroup branch only when group_id is non-empty — so an empty group_id
// falls through to the private branch. The call succeeds the first time
// but is blocked the second time by the private quota (as a soft no-op).
func TestChatSay_GroupCallWithoutGroupID(t *testing.T) {
	guard := NewChatSayGuard()
	rig := newChatSayRig(t, guard)
	defer rig.cleanup()

	args := map[string]any{
		"speaker": "XiaMi", "text": "你好", "channel": "group",
		// group_id omitted on purpose
	}
	// First call: succeeds (private branch, fresh budget).
	if res := rig.call(t, args); res.IsError {
		t.Fatalf("first call should not be group-guarded (empty group_id), got error: %v", res.Content)
	}
	// Second call without an intervening wake-up: private quota soft no-op.
	if res := rig.call(t, args); !errContains(res, "noop_chat_say_private_quota_exhausted") {
		t.Fatalf("second call should hit private quota (empty group_id falls back to private), got %v", res.Content)
	}
	if *rig.delivered != 1 {
		t.Errorf("expected 1 delivery, got %d", *rig.delivered)
	}
}

func TestChatSay_RejectsMissingFields(t *testing.T) {
	srv := bridge.NewTestServer(func(context.Context, string, json.RawMessage) (any, error) {
		t.Fatal("handler should not be reached")
		return nil, nil
	})
	defer srv.Close()
	br := bridge.NewWSClient(bridge.WSClientOptions{URL: srv.URL_WS()})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := br.Connect(ctx); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer br.Close()

	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "test"}, nil)
	registerChat(server, br, nil)
	t1, t2 := mcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, t1, nil); err != nil {
		t.Fatalf("server: %v", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "t"}, nil)
	cs, err := client.Connect(ctx, t2, nil)
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	defer cs.Close()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "chat_say",
		Arguments: map[string]any{"speaker": "SmartNPC"}, // missing text
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Error("expected IsError=true")
	}
}

// errContains scans the rendered text content of a CallToolResult for the
// given substring — the SDK packs the handler's returned error into a
// TextContent entry on the result.
func errContains(res *mcp.CallToolResult, sub string) bool {
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok && strings.Contains(tc.Text, sub) {
			return true
		}
	}
	return false
}
