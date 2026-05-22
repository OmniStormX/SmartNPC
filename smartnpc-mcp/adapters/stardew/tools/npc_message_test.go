package tools

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestMailbox_EnqueuePeekAck verifies the in-memory mailbox preserves FIFO
// ordering and enforces the per-recipient cap by dropping the oldest entry.
func TestMailbox_EnqueuePeekAck(t *testing.T) {
	box := newMailbox(3)

	msgs := []MailboxMessage{
		{ID: "a", From: "XiaMi", To: "Abigail", Text: "hi"},
		{ID: "b", From: "XiaMi", To: "Abigail", Text: "there"},
		{ID: "c", From: "XiaMi", To: "Abigail", Text: "friend"},
	}
	for _, m := range msgs {
		box.Enqueue(m)
	}

	got := box.Peek("Abigail", 0)
	if len(got) != 3 {
		t.Fatalf("peek: got %d want 3", len(got))
	}
	for i, m := range got {
		if m.ID != msgs[i].ID {
			t.Errorf("order broken at %d: got %q want %q", i, m.ID, msgs[i].ID)
		}
	}

	// Fourth message must evict the oldest (ID "a").
	box.Enqueue(MailboxMessage{ID: "d", From: "XiaMi", To: "Abigail", Text: "still"})
	got = box.Peek("Abigail", 0)
	if len(got) != 3 {
		t.Fatalf("after overflow: got %d want 3", len(got))
	}
	if got[0].ID != "b" || got[2].ID != "d" {
		t.Errorf("unexpected order after overflow: %+v", got)
	}
	if dropped := box.dropped.Load(); dropped != 1 {
		t.Errorf("expected 1 drop, got %d", dropped)
	}

	// Ack removes specific IDs; unknown IDs are silent.
	removed := box.Ack("Abigail", []string{"b", "unknown"})
	if removed != 1 {
		t.Errorf("ack removed=%d want 1", removed)
	}
	got = box.Peek("Abigail", 0)
	if len(got) != 2 || got[0].ID != "c" {
		t.Errorf("post-ack state unexpected: %+v", got)
	}
}

func TestMailbox_PeekMax(t *testing.T) {
	box := newMailbox(10)
	for i, id := range []string{"a", "b", "c", "d"} {
		box.Enqueue(MailboxMessage{ID: id, From: "X", To: "Y", Text: id, Timestamp: int64(i)})
	}

	got := box.Peek("Y", 2)
	if len(got) != 2 {
		t.Fatalf("peek max=2: got %d", len(got))
	}
	if got[0].ID != "a" || got[1].ID != "b" {
		t.Errorf("wrong prefix: %+v", got)
	}

	// max=0 means all
	all := box.Peek("Y", 0)
	if len(all) != 4 {
		t.Fatalf("peek all: got %d want 4", len(all))
	}
}

// TestNpcSendMessage_EndToEnd wires an in-memory MCP server + client and
// verifies that npc_send_message -> npc_inbox_get -> npc_inbox_ack round-trips
// correctly.
func TestNpcSendMessage_EndToEnd(t *testing.T) {
	ctx := context.Background()
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "t"}, nil)
	RegisterAll(server, nil, nil, nil, nil)

	t1, t2 := mcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, t1, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "t"}, nil)
	cs, err := client.Connect(ctx, t2, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer cs.Close()

	// send
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "npc_send_message",
		Arguments: map[string]any{
			"from": "XiaMi",
			"to":   "Abigail",
			"text": "农场出事了",
			"kind": "alert",
		},
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if res.IsError {
		t.Fatalf("send returned error: %v", res.Content)
	}
	var sendOut NpcSendMessageOutput
	mustStructured(t, res, &sendOut)
	if !sendOut.OK || sendOut.ID == "" {
		t.Fatalf("unexpected send output: %+v", sendOut)
	}

	// inbox_get
	res, err = cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "npc_inbox_get",
		Arguments: map[string]any{"npc": "Abigail"},
	})
	if err != nil || res.IsError {
		t.Fatalf("inbox_get failed: %v %v", err, res)
	}
	var getOut NpcInboxGetOutput
	mustStructured(t, res, &getOut)
	if getOut.Count != 1 {
		t.Fatalf("expected 1 queued message, got %d: %+v", getOut.Count, getOut.Messages)
	}
	if getOut.Messages[0].Text != "农场出事了" || getOut.Messages[0].Kind != "alert" {
		t.Errorf("unexpected queued message: %+v", getOut.Messages[0])
	}
	if getOut.Messages[0].ID != sendOut.ID {
		t.Errorf("id mismatch: send=%q inbox=%q", sendOut.ID, getOut.Messages[0].ID)
	}

	// ack
	res, err = cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "npc_inbox_ack",
		Arguments: map[string]any{
			"npc": "Abigail",
			"ids": []string{sendOut.ID},
		},
	})
	if err != nil || res.IsError {
		t.Fatalf("ack failed: %v %v", err, res)
	}
	var ackOut NpcInboxAckOutput
	mustStructured(t, res, &ackOut)
	if !ackOut.OK || ackOut.Removed != 1 {
		t.Errorf("unexpected ack output: %+v", ackOut)
	}

	// inbox should now be empty
	res, err = cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "npc_inbox_get",
		Arguments: map[string]any{"npc": "Abigail"},
	})
	if err != nil || res.IsError {
		t.Fatalf("inbox_get post-ack: %v %v", err, res)
	}
	mustStructured(t, res, &getOut)
	if getOut.Count != 0 {
		t.Errorf("expected empty inbox after ack, got %d", getOut.Count)
	}
}

func TestNpcSendMessage_Validation(t *testing.T) {
	ctx := context.Background()
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "t"}, nil)
	RegisterAll(server, nil, nil, nil, nil)

	t1, t2 := mcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, t1, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "t"}, nil)
	cs, err := client.Connect(ctx, t2, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer cs.Close()

	cases := []struct {
		name string
		args map[string]any
	}{
		{"missing_from", map[string]any{"to": "A", "text": "hi"}},
		{"missing_to", map[string]any{"from": "X", "text": "hi"}},
		{"empty_text", map[string]any{"from": "X", "to": "A", "text": ""}},
		{"self_send", map[string]any{"from": "X", "to": "X", "text": "hi"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := cs.CallTool(ctx, &mcp.CallToolParams{
				Name: "npc_send_message", Arguments: tc.args,
			})
			if err == nil && !res.IsError {
				t.Errorf("expected validation error, got ok; res=%+v", res)
			}
		})
	}
}

func TestNpcBroadcast_Ack(t *testing.T) {
	ctx := context.Background()
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "t"}, nil)
	RegisterAll(server, nil, nil, nil, nil)

	t1, t2 := mcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, t1, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "t"}, nil)
	cs, err := client.Connect(ctx, t2, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer cs.Close()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "npc_broadcast_event",
		Arguments: map[string]any{
			"from": "XiaMi",
			"kind": "alarm",
			"data": map[string]any{"level": "urgent"},
		},
	})
	if err != nil || res.IsError {
		t.Fatalf("broadcast failed: err=%v isError=%v content=%s",
			err, res.IsError, mustText(res))
	}
	var out NpcBroadcastEventOutput
	mustStructured(t, res, &out)
	if !out.OK || out.Timestamp == 0 {
		t.Errorf("unexpected broadcast output: %+v", out)
	}
}

// mustStructured unmarshals the tool's structured content into dst, failing
// the test on any JSON issue.
func mustStructured(t *testing.T, res *mcp.CallToolResult, dst any) {
	t.Helper()
	if res.StructuredContent == nil {
		t.Fatalf("no structured content; text=%v", res.Content)
	}
	b, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured: %v", err)
	}
	if err := json.Unmarshal(b, dst); err != nil {
		t.Fatalf("unmarshal to %T: %v (raw=%s)", dst, err, b)
	}
}

// mustText concatenates any TextContent blocks for use in failure messages.
func mustText(res *mcp.CallToolResult) string {
	var buf []byte
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			buf = append(buf, []byte(tc.Text+"\n")...)
		}
	}
	return string(buf)
}

// TestNpcSendMessage_FeedsHermesRelay verifies the hermes handler injected
// into RegisterAll receives synthetic events emitted by npc_send_message
// and npc_broadcast_event. This is the wiring that wakes up a recipient
// NPC's Hermes profile without requiring the player to talk to it first.
func TestNpcSendMessage_FeedsHermesRelay(t *testing.T) {
	type capture struct {
		name string
		data json.RawMessage
	}
	var (
		mu   sync.Mutex
		seen []capture
	)
	hermes := func(_ context.Context, name string, data json.RawMessage) {
		mu.Lock()
		defer mu.Unlock()
		// Copy data; the caller's buffer can be reused.
		cp := append(json.RawMessage(nil), data...)
		seen = append(seen, capture{name: name, data: cp})
	}

	ctx := context.Background()
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "t"}, nil)
	RegisterAll(server, nil, hermes, nil, nil)

	t1, t2 := mcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, t1, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "t"}, nil)
	cs, err := client.Connect(ctx, t2, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer cs.Close()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "npc_send_message",
		Arguments: map[string]any{
			"from": "XiaMi",
			"to":   "Abigail",
			"text": "delegated task",
		},
	})
	if err != nil || res.IsError {
		t.Fatalf("send: err=%v isErr=%v", err, res.IsError)
	}
	res, err = cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "npc_broadcast_event",
		Arguments: map[string]any{
			"from": "XiaMi",
			"kind": "alarm",
		},
	})
	if err != nil || res.IsError {
		t.Fatalf("broadcast: err=%v isErr=%v", err, res.IsError)
	}

	// hermes handler is dispatched on its own goroutine; poll briefly.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(seen)
		mu.Unlock()
		if n >= 2 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 2 {
		t.Fatalf("expected 2 hermes events, got %d: %+v", len(seen), seen)
	}

	// First event: npc_message with to=Abigail (so relay's NPC filter routes
	// it correctly to Abigail's profile).
	if seen[0].name != "npc_message" {
		t.Errorf("event[0] name: got %q want %q", seen[0].name, "npc_message")
	}
	var msg MailboxMessage
	if err := json.Unmarshal(seen[0].data, &msg); err != nil {
		t.Fatalf("unmarshal msg: %v (raw=%s)", err, seen[0].data)
	}
	if msg.From != "XiaMi" || msg.To != "Abigail" || msg.Text != "delegated task" {
		t.Errorf("event[0] payload mismatch: %+v", msg)
	}

	// Second event: npc_broadcast.
	if seen[1].name != "npc_broadcast" {
		t.Errorf("event[1] name: got %q want %q", seen[1].name, "npc_broadcast")
	}
}

// TestNpcSendMessage_DetachesContext verifies the synthetic event is dispatched
// to the hermes handler with a context that survives cancellation of the
// caller's tool-call context. Without detach, an in-flight Hermes POST would
// be canceled the moment the tool returns.
func TestNpcSendMessage_DetachesContext(t *testing.T) {
	gotCtx := make(chan context.Context, 1)
	hermes := func(c context.Context, _ string, _ json.RawMessage) {
		gotCtx <- c
	}

	ctx := context.Background()
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "t"}, nil)
	RegisterAll(server, nil, hermes, nil, nil)

	t1, t2 := mcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, t1, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "t"}, nil)
	cs, err := client.Connect(ctx, t2, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer cs.Close()

	callCtx, cancel := context.WithCancel(ctx)
	if _, err := cs.CallTool(callCtx, &mcp.CallToolParams{
		Name: "npc_send_message",
		Arguments: map[string]any{
			"from": "X", "to": "Y", "text": "ping",
		},
	}); err != nil {
		t.Fatalf("send: %v", err)
	}
	// Cancel the caller's ctx and confirm the hermes handler's ctx is not
	// derived from it (i.e. still alive).
	cancel()

	select {
	case c := <-gotCtx:
		select {
		case <-c.Done():
			t.Fatalf("hermes handler ctx was canceled after caller cancel — not detached")
		default:
			// good: detached ctx still alive
		}
	case <-time.After(time.Second):
		t.Fatal("hermes handler not invoked within 1s")
	}
}

// TestNpcSendMessage_NilHermesDoesNotPanic confirms the legacy zero-arg
// wiring (no Hermes backend) still works: npc_send_message succeeds, the
// mailbox is populated, and no panic fires from the nil hermes handler.
// Locks in the "Hermes fan-out is purely additive" invariant.
func TestNpcSendMessage_NilHermesDoesNotPanic(t *testing.T) {
	ctx := context.Background()
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "t"}, nil)
	RegisterAll(server, nil, nil, nil, nil)

	t1, t2 := mcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, t1, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "t"}, nil)
	cs, err := client.Connect(ctx, t2, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer cs.Close()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "npc_send_message",
		Arguments: map[string]any{
			"from": "XiaMi", "to": "Abigail", "text": "hi",
		},
	})
	if err != nil || res.IsError {
		t.Fatalf("send with nil hermes: err=%v isErr=%v", err, res.IsError)
	}

	// Mailbox path must still work — Hermes wiring is additive, not a
	// replacement for the inbox-pull fallback.
	res, err = cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "npc_inbox_get",
		Arguments: map[string]any{"npc": "Abigail"},
	})
	if err != nil || res.IsError {
		t.Fatalf("inbox_get with nil hermes: err=%v isErr=%v", err, res.IsError)
	}
	var got NpcInboxGetOutput
	mustStructured(t, res, &got)
	if got.Count != 1 {
		t.Errorf("expected 1 message in mailbox, got %d", got.Count)
	}

	// Same for broadcast — must not panic when hermes is nil.
	res, err = cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "npc_broadcast_event",
		Arguments: map[string]any{
			"from": "XiaMi", "kind": "alarm",
		},
	})
	if err != nil || res.IsError {
		t.Fatalf("broadcast with nil hermes: err=%v isErr=%v", err, res.IsError)
	}
}

// TestNpcSendMessage_StillEmitsMCPNotification proves the original push
// channel (MCP logging notification) is preserved even when a hermes handler
// is also wired up. Hermes fan-out is "additive": existing subscribers
// (Claude Desktop, other MCP clients) keep getting events.
func TestNpcSendMessage_StillEmitsMCPNotification(t *testing.T) {
	type logged struct {
		level mcp.LoggingLevel
		data  json.RawMessage
	}
	var (
		mu     sync.Mutex
		notifs []logged
	)
	clientOpts := &mcp.ClientOptions{
		LoggingMessageHandler: func(_ context.Context, req *mcp.LoggingMessageRequest) {
			mu.Lock()
			defer mu.Unlock()
			b, _ := json.Marshal(req.Params.Data)
			notifs = append(notifs, logged{level: req.Params.Level, data: b})
		},
	}

	hermesCalled := make(chan struct{}, 1)
	hermes := func(_ context.Context, _ string, _ json.RawMessage) {
		select {
		case hermesCalled <- struct{}{}:
		default:
		}
	}

	ctx := context.Background()
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "t"}, nil)
	RegisterAll(server, nil, hermes, nil, nil)

	t1, t2 := mcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, t1, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "t"}, clientOpts)
	cs, err := client.Connect(ctx, t2, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer cs.Close()

	// MCP server only forwards logging notifications at-or-above the level
	// the client subscribed to. Without this, sess.Log() is filtered out.
	if err := cs.SetLoggingLevel(ctx, &mcp.SetLoggingLevelParams{Level: "debug"}); err != nil {
		t.Fatalf("set logging level: %v", err)
	}

	if _, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "npc_send_message",
		Arguments: map[string]any{
			"from": "XiaMi", "to": "Abigail", "text": "still notifies",
		},
	}); err != nil {
		t.Fatalf("send: %v", err)
	}

	// Wait for both push paths to fire: hermes (sync goroutine) + MCP
	// logging notification (async session.Log).
	select {
	case <-hermesCalled:
	case <-time.After(time.Second):
		t.Fatal("hermes handler not invoked within 1s")
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(notifs)
		mu.Unlock()
		if n >= 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(notifs) == 0 {
		t.Fatal("expected at least one MCP logging notification, got none — Hermes fan-out replaced the original channel instead of adding to it")
	}

	// The envelope shape mirrors MakeEventForwarder: we look for one whose
	// "name" field is "npc_message".
	var matched bool
	for _, n := range notifs {
		var env struct {
			Kind string `json:"kind"`
			Name string `json:"name"`
		}
		if err := json.Unmarshal(n.data, &env); err != nil {
			continue
		}
		if env.Kind == "stardew/event" && env.Name == "npc_message" {
			matched = true
			break
		}
	}
	if !matched {
		t.Errorf("no logging notification with name=npc_message found; got %d notifs", len(notifs))
	}
}
