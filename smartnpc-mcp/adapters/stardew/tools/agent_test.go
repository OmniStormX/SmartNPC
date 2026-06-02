package tools

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/OmniStormX/SmartNPC/adapters/stardew/bridge"
)

// TestAgentRegisterSelf_EndToEnd drives the agent_register_self tool through
// an in-memory MCP transport and asserts that the WSClient's session→NPC
// registry is populated. This locks in the routing fix: every subsequent
// br.Call from this session will stamp Request.FromNPC = "Abigail".
//
// Uses a dummy WSClient (never connected to a ws server) — agent_register_self
// only writes to the in-memory registry on br, it doesn't open any ws frames.
func TestAgentRegisterSelf_EndToEnd(t *testing.T) {
	ctx := context.Background()

	br := bridge.NewWSClient(bridge.WSClientOptions{URL: "ws://localhost:0/ws"})
	defer br.Close()

	server := mcp.NewServer(&mcp.Implementation{
		Name: "stardew-tools-test", Version: "test",
	}, nil)
	registerAgent(server, br)

	t1, t2 := mcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, t1, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}

	client := mcp.NewClient(&mcp.Implementation{
		Name: "stardew-tools-test", Version: "test",
	}, nil)
	cs, err := client.Connect(ctx, t2, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer cs.Close()

	// 1. ListTools must include agent_register_self so the SOUL/skill
	//    can rely on it being there.
	listed, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	var found bool
	for _, tool := range listed.Tools {
		if tool.Name == "agent_register_self" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("agent_register_self missing from tool list: %v", listed.Tools)
	}

	// 2. CallTool with a real npc binds the session.
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "agent_register_self",
		Arguments: map[string]any{"npc": "Abigail"},
	})
	if err != nil {
		t.Fatalf("call agent_register_self: %v", err)
	}
	if res.IsError {
		// Surface the underlying message so we know if it's the empty-session
		// guard tripping vs a schema violation.
		var msg string
		for _, c := range res.Content {
			if tc, ok := c.(*mcp.TextContent); ok {
				msg += tc.Text
			}
		}
		t.Fatalf("agent_register_self returned error: %s", msg)
	}
	b, _ := json.Marshal(res.StructuredContent)
	var out AgentRegisterSelfOutput
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal structured: %v (raw=%s)", err, b)
	}
	if !out.OK {
		t.Fatalf("OK=false; out=%+v", out)
	}
	if out.NPC != "Abigail" {
		t.Errorf("Output.NPC=%q want Abigail", out.NPC)
	}
	// Note: InMemoryTransport doesn't allocate a session id; it bubbles up
	// as "". The registry maps that to a synthetic solo-session key, which
	// is fine since stdio + tests are always single-client. In production
	// (HTTP streamable transport) the id is non-empty.

	// 3. The registry on br must have the binding so subsequent Call()s
	//    from this session resolve to Abigail. Look up via the same empty
	//    session id the SDK reported.
	if got := br.AgentForSession(out.SessionID); got != "Abigail" {
		t.Errorf("br.AgentForSession(%q)=%q want Abigail", out.SessionID, got)
	}

	// 4. Empty npc is rejected.
	res2, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "agent_register_self",
		Arguments: map[string]any{"npc": ""},
	})
	if err != nil {
		t.Fatalf("call agent_register_self (empty npc): %v", err)
	}
	if !res2.IsError {
		t.Errorf("expected IsError=true for empty npc, got result %+v", res2)
	}
}

// TestRegisterAgent_RegistryAPI exercises the bridge.WSClient registry
// surface directly without going through MCP — covers de-registration and
// the empty-session-id guard.
func TestRegisterAgent_RegistryAPI(t *testing.T) {
	br := bridge.NewWSClient(bridge.WSClientOptions{URL: "ws://localhost:0/ws"})
	defer br.Close()

	// Empty session id is mapped to a synthetic solo-session key so stdio /
	// InMemoryTransport still get a working binding. After this call,
	// AgentForSession("") must hit "Abigail".
	if !br.RegisterAgent("", "Abigail") {
		t.Errorf("empty session id should be accepted (mapped to solo key)")
	}
	if got := br.AgentForSession(""); got != "Abigail" {
		t.Errorf("AgentForSession(\"\") after solo-register: got %q want Abigail", got)
	}
	// Distinct, never-seen session id resolves to empty.
	if got := br.AgentForSession("never-seen"); got != "" {
		t.Errorf("unknown session id should resolve to empty, got %q", got)
	}
	// Clean up the solo binding so it doesn't leak into subsequent assertions.
	br.RegisterAgent("", "")
	if !br.RegisterAgent("sess-1", "Abigail") {
		t.Fatalf("RegisterAgent failed")
	}
	if got := br.AgentForSession("sess-1"); got != "Abigail" {
		t.Errorf("after register, got %q want Abigail", got)
	}
	// Re-register with a different npc overwrites.
	br.RegisterAgent("sess-1", "Penny")
	if got := br.AgentForSession("sess-1"); got != "Penny" {
		t.Errorf("after re-register, got %q want Penny", got)
	}
	// Empty npc on a known session id de-registers.
	br.RegisterAgent("sess-1", "")
	if got := br.AgentForSession("sess-1"); got != "" {
		t.Errorf("after de-register, got %q want empty", got)
	}
}

// TestFromNPC_StampedOnDownstreamCall is the end-to-end regression test
// that locks in the routing fix: after agent_register_self binds the
// session, a subsequent mail_send (which has no `npc` argument of its own)
// must arrive at the SMAPI mod with Request.FromNPC == the registered
// profile.
//
// Flow:
//
//	mcp client ── CallTool(agent_register_self, npc="Abigail") ──> mcp server
//	mcp client ── CallTool(mail_send, text="...")              ──> mcp server
//	                                                               └─> br.Call ──> ws TestServer
//	                                                                                 └─> capture Request.FromNPC
func TestFromNPC_StampedOnDownstreamCall(t *testing.T) {
	ctx := context.Background()

	// 1. ws TestServer with raw-frame capture.
	var (
		mu         sync.Mutex
		mailFromNPC string
		mailSeen   bool
	)
	ts := bridge.NewTestServer(func(_ context.Context, action string, _ json.RawMessage) (any, error) {
		// mail_send's response shape: {ok: true} is enough for the tool to succeed.
		_ = action
		return map[string]any{"ok": true}, nil
	})
	ts.OnRawRequest = func(req bridge.Request) {
		if req.Action != "mail_send" {
			return
		}
		mu.Lock()
		mailFromNPC = req.FromNPC
		mailSeen = true
		mu.Unlock()
	}
	defer ts.Close()

	// 2. WSClient connected to the test server.
	br := bridge.NewWSClient(bridge.WSClientOptions{URL: ts.URL_WS()})
	defer br.Close()
	if err := br.Connect(ctx); err != nil {
		t.Fatalf("ws connect: %v", err)
	}

	// 3. MCP server with both tools registered.
	server := mcp.NewServer(&mcp.Implementation{
		Name: "stardew-tools-test", Version: "test",
	}, nil)
	registerAgent(server, br)
	registerMail(server, br)

	t1, t2 := mcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, t1, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	client := mcp.NewClient(&mcp.Implementation{
		Name: "stardew-tools-test", Version: "test",
	}, nil)
	cs, err := client.Connect(ctx, t2, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer cs.Close()

	// 4. Bootstrap: register the session as Abigail.
	regRes, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "agent_register_self",
		Arguments: map[string]any{"npc": "Abigail"},
	})
	if err != nil {
		t.Fatalf("agent_register_self: %v", err)
	}
	if regRes.IsError {
		t.Fatalf("agent_register_self returned error: %+v", regRes.Content)
	}

	// 5. Now invoke mail_send. The tool has no `npc` argument; the only
	//    way the mod can know who issued it is the FromNPC field stamped
	//    by br.Call from the session→NPC binding.
	mailRes, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "mail_send",
		Arguments: map[string]any{"text": "hi from abigail"},
	})
	if err != nil {
		t.Fatalf("mail_send: %v", err)
	}
	if mailRes.IsError {
		t.Fatalf("mail_send returned error: %+v", mailRes.Content)
	}

	// 6. Inspect the captured raw frame.
	mu.Lock()
	defer mu.Unlock()
	if !mailSeen {
		t.Fatalf("OnRawRequest was never invoked for mail_send — frame did not reach the test server")
	}
	if mailFromNPC != "Abigail" {
		t.Errorf("Request.FromNPC = %q, want \"Abigail\" (NPC-agnostic tool must inherit from session binding)", mailFromNPC)
	}
}

// TestSessionContext_RoundTrip exercises the ctx-tagging helper used by
// every tool handler.
func TestSessionContext_RoundTrip(t *testing.T) {
	if got := bridge.SessionFromContext(context.Background()); got != "" {
		t.Errorf("bare ctx returned %q want empty", got)
	}
	ctx := bridge.WithCallSession(context.Background(), "sess-xyz")
	if got := bridge.SessionFromContext(ctx); got != "sess-xyz" {
		t.Errorf("got %q want sess-xyz", got)
	}
}
