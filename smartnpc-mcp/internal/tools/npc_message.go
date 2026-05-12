package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/smartnpc/smartnpc-mcp/internal/bridge"
)

// Inter-NPC messaging. These tools let one NPC's Hermes profile (or the
// legacy smartnpc-agent dev harness) send messages to other NPCs through
// smartnpc-mcp. The recipient is reached three ways, all driven from
// the same emit site:
//
//  1. Hermes profile wake (primary): the synthetic event is fed
//     through the shared bridge.EventHandler so hermesrelay POSTs the
//     recipient's Hermes Gateway, identical to a mod-sourced event.
//     See ADR-0001 (docs/adr/0001-synthetic-events-go-through-hermesrelay.md).
//
//  2. MCP push notification: by subscribing to MCP logging notifications with name
//     bridge.EventNpcMessage (targeted) or bridge.EventNpcBroadcast (fanout).
//     The forwarder (internal/tools/events.go) already delivers these.
//
//  3. Inbox pull: by calling npc_inbox_get. This is useful for consumers that
//     can't subscribe to notifications (e.g. an agent loop driven by
//     /v1/responses on a per-turn basis) or as a catch-up mechanism.
//
// The mailbox is purely in-memory and bounded per recipient; it is not
// persisted across restarts. That's intentional — durable state lives in
// Hermes memory, not here. See docs/events.md for the wire protocol.

// ── mailbox ────────────────────────────────────────────────────

// MailboxMessage is a single queued message.
type MailboxMessage struct {
	ID        string `json:"id"        jsonschema:"message uuid — echo back in npc_inbox_ack to remove"`
	From      string `json:"from"      jsonschema:"sender NPC internal name"`
	To        string `json:"to"        jsonschema:"recipient NPC internal name (empty for broadcast)"`
	Text      string `json:"text"      jsonschema:"message body"`
	Kind      string `json:"kind,omitempty"      jsonschema:"optional free-form tag, e.g. \"greeting\" / \"alert\""`
	Timestamp int64  `json:"timestamp" jsonschema:"unix millis when the message was sent"`
}

// mailbox is a bounded FIFO per recipient. Safe for concurrent use.
type mailbox struct {
	mu      sync.Mutex
	queues  map[string][]MailboxMessage
	maxPer  int
	dropped atomic.Uint64 // telemetry: total drops due to capacity
}

func newMailbox(maxPerRecipient int) *mailbox {
	if maxPerRecipient <= 0 {
		maxPerRecipient = 64
	}
	return &mailbox{
		queues: make(map[string][]MailboxMessage),
		maxPer: maxPerRecipient,
	}
}

// Enqueue appends msg to the recipient's queue. When the queue would exceed
// maxPer, the oldest message is dropped to make room (bounded memory).
func (m *mailbox) Enqueue(msg MailboxMessage) {
	m.mu.Lock()
	defer m.mu.Unlock()
	q := m.queues[msg.To]
	if len(q) >= m.maxPer {
		q = q[1:]
		m.dropped.Add(1)
	}
	m.queues[msg.To] = append(q, msg)
}

// Peek returns up to max pending messages for the recipient without
// removing them. When max == 0, all pending messages are returned.
func (m *mailbox) Peek(to string, max int) []MailboxMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	q := m.queues[to]
	if max > 0 && len(q) > max {
		q = q[:max]
	}
	out := make([]MailboxMessage, len(q))
	copy(out, q)
	return out
}

// Ack removes the given message IDs from the recipient's queue. Unknown
// IDs are silently ignored. Returns the number of messages actually removed.
func (m *mailbox) Ack(to string, ids []string) int {
	if len(ids) == 0 {
		return 0
	}
	wanted := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		wanted[id] = struct{}{}
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	q := m.queues[to]
	kept := q[:0]
	removed := 0
	for _, msg := range q {
		if _, ok := wanted[msg.ID]; ok {
			removed++
			continue
		}
		kept = append(kept, msg)
	}
	m.queues[to] = kept
	return removed
}

// ── tool inputs / outputs ─────────────────────────────────────

// NpcSendMessageInput drives the `npc_send_message` tool.
type NpcSendMessageInput struct {
	From string `json:"from" jsonschema:"sender NPC internal name, e.g. \"XiaMi\""`
	To   string `json:"to"   jsonschema:"recipient NPC internal name, e.g. \"Abigail\""`
	Text string `json:"text" jsonschema:"message body; plain text, keep short"`
	Kind string `json:"kind,omitempty" jsonschema:"optional free-form tag, e.g. \"greeting\" / \"alert\""`
}

// NpcSendMessageOutput is the ack.
type NpcSendMessageOutput struct {
	OK        bool   `json:"ok"         jsonschema:"true if the message was queued"`
	ID        string `json:"id"         jsonschema:"assigned message uuid — recipient echoes it in npc_inbox_ack"`
	Timestamp int64  `json:"timestamp"  jsonschema:"unix millis when the message was queued"`
}

// NpcBroadcastEventInput drives the `npc_broadcast_event` tool.
type NpcBroadcastEventInput struct {
	From string `json:"from"           jsonschema:"sender NPC internal name"`
	Kind string `json:"kind"           jsonschema:"event category, e.g. \"alarm\" / \"party_invite\""`
	Data any    `json:"data,omitempty" jsonschema:"optional JSON payload attached to the broadcast"`
}

// NpcBroadcastEventOutput is the ack. Broadcasts are fire-and-forget.
type NpcBroadcastEventOutput struct {
	OK        bool  `json:"ok"         jsonschema:"true if the broadcast was emitted"`
	Timestamp int64 `json:"timestamp"  jsonschema:"unix millis when the broadcast was emitted"`
}

// NpcInboxGetInput drives the `npc_inbox_get` tool.
type NpcInboxGetInput struct {
	NPC string `json:"npc" jsonschema:"recipient NPC internal name whose pending messages to read"`
	Max int    `json:"max,omitempty" jsonschema:"max messages to return (0 = all pending, max cap is 64)"`
}

// NpcInboxGetOutput returns pending messages without removing them.
type NpcInboxGetOutput struct {
	OK       bool             `json:"ok"       jsonschema:"true on success"`
	NPC      string           `json:"npc"      jsonschema:"echo of recipient name"`
	Count    int              `json:"count"    jsonschema:"number of messages returned"`
	Messages []MailboxMessage `json:"messages" jsonschema:"pending messages in FIFO order"`
}

// NpcInboxAckInput drives the `npc_inbox_ack` tool.
type NpcInboxAckInput struct {
	NPC string   `json:"npc" jsonschema:"recipient NPC internal name"`
	IDs []string `json:"ids" jsonschema:"message uuids to remove from the inbox"`
}

// NpcInboxAckOutput reports how many were removed.
type NpcInboxAckOutput struct {
	OK      bool `json:"ok"      jsonschema:"true on success"`
	Removed int  `json:"removed" jsonschema:"how many messages were actually removed (unknown ids are ignored)"`
}

// ── registration ──────────────────────────────────────────────

// registerNpcMessage wires the inter-NPC messaging tools onto the server.
// The mailbox is scoped to this registration; tests can call
// registerNpcMessage twice on different servers for isolation.
//
// hermes, when non-nil, is the same bridge.EventHandler the ws router uses
// to forward mod events to the Hermes Gateway. Synthetic events emitted
// here (npc_send_message / npc_broadcast_event) are fed back through it so
// the recipient NPC's Hermes profile is woken up immediately, without
// waiting for the recipient to talk to the player first.
func registerNpcMessage(s *mcp.Server, logger *slog.Logger, hermes bridge.EventHandler) {
	box := newMailbox(64)

	mcp.AddTool(s, &mcp.Tool{
		Name: "npc_send_message",
		Description: "Send a private message from one NPC to another. The synthetic " +
			"event is dispatched THREE ways from a single emit site: (1) the " +
			"recipient's Hermes profile is woken via Gateway POST through hermesrelay " +
			"(primary — recipient takes a turn), (2) any MCP notification subscriber " +
			"sees an `npc_message` event, (3) the message is buffered in an in-memory " +
			"FIFO inbox readable via `npc_inbox_get`. The mailbox is bounded at 64 " +
			"entries per recipient; oldest is dropped on overflow.\n\n" +
			"When to call: NPC-to-NPC coordination — gossiping, alerting a friend, " +
			"delegating a task. Do NOT use this to talk to the player (use `chat_say`).\n\n" +
			"Constraints: plain UTF-8 text, short. `from` and `to` must be NPC internal " +
			"names, not display names or player.\n\n" +
			"Avoid auto-replying to an incoming `npc_message` event with another " +
			"`npc_send_message` to the original sender — this can cause an infinite " +
			"ping-pong loop. If a reply is needed, set `kind=\"reply\"` and only call " +
			"once per inbox item.\n\n" +
			"Side-effect: WRITE (in-memory mailbox + fan-out notification). Always " +
			"available — does not require a loaded save.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in NpcSendMessageInput) (*mcp.CallToolResult, NpcSendMessageOutput, error) {
		if strings.TrimSpace(in.From) == "" || strings.TrimSpace(in.To) == "" {
			return nil, NpcSendMessageOutput{}, fmt.Errorf("from and to are required")
		}
		if strings.TrimSpace(in.Text) == "" {
			return nil, NpcSendMessageOutput{}, fmt.Errorf("text is required")
		}
		if in.From == in.To {
			return nil, NpcSendMessageOutput{}, fmt.Errorf("from and to must differ")
		}

		msg := MailboxMessage{
			ID:        uuid.NewString(),
			From:      in.From,
			To:        in.To,
			Text:      in.Text,
			Kind:      in.Kind,
			Timestamp: time.Now().UnixMilli(),
		}
		box.Enqueue(msg)

		// Fan out as a synthetic MCP notification so push consumers see it.
		payload, _ := json.Marshal(msg)
		emitSyntheticEvent(ctx, s, logger, hermes, bridge.EventNpcMessage, payload)

		return nil, NpcSendMessageOutput{OK: true, ID: msg.ID, Timestamp: msg.Timestamp}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "npc_broadcast_event",
		Description: "Broadcast an event from one NPC to every other subscribed NPC " +
			"(no explicit recipient). The synthetic event is dispatched two ways from " +
			"a single emit site: (1) every routed Hermes profile receives a Gateway " +
			"POST through hermesrelay (primary — each subscribed NPC may take a turn), " +
			"and (2) any MCP notification subscriber sees an `npc_broadcast` event. " +
			"Fire-and-forget — NOT queued in any inbox; offline consumers miss it.\n\n" +
			"When to call: world-wide signals that matter to many NPCs at once — " +
			"\"the mines are flooding\", \"wedding reception starting\", \"party at the saloon\". " +
			"For one-to-one messages prefer `npc_send_message`.\n\n" +
			"Constraints: `kind` is a short category tag. `data` is an optional JSON " +
			"payload forwarded verbatim — keep it small and serializable.\n\n" +
			"Side-effect: WRITE (fan-out notification only). Always available.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in NpcBroadcastEventInput) (*mcp.CallToolResult, NpcBroadcastEventOutput, error) {
		if strings.TrimSpace(in.From) == "" {
			return nil, NpcBroadcastEventOutput{}, fmt.Errorf("from is required")
		}
		if strings.TrimSpace(in.Kind) == "" {
			return nil, NpcBroadcastEventOutput{}, fmt.Errorf("kind is required")
		}

		ts := time.Now().UnixMilli()
		payload, _ := json.Marshal(struct {
			From      string `json:"from"`
			Kind      string `json:"kind"`
			Data      any    `json:"data,omitempty"`
			Timestamp int64  `json:"timestamp"`
		}{
			From:      in.From,
			Kind:      in.Kind,
			Data:      in.Data,
			Timestamp: ts,
		})
		emitSyntheticEvent(ctx, s, logger, hermes, bridge.EventNpcBroadcast, payload)

		return nil, NpcBroadcastEventOutput{OK: true, Timestamp: ts}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "npc_inbox_get",
		Description: "Read pending messages queued for a recipient NPC via " +
			"`npc_send_message`. Messages are returned in FIFO order and are NOT " +
			"removed — use `npc_inbox_ack` with the returned ids to clear them.\n\n" +
			"When to call: the recipient's agent loop is about to generate a reply " +
			"and wants to fold in any waiting inter-NPC messages. Also useful after " +
			"a disconnect/reconnect to catch up on missed notifications.\n\n" +
			"Side-effect: READ. Always available.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in NpcInboxGetInput) (*mcp.CallToolResult, NpcInboxGetOutput, error) {
		if strings.TrimSpace(in.NPC) == "" {
			return nil, NpcInboxGetOutput{}, fmt.Errorf("npc is required")
		}
		max := in.Max
		if max < 0 {
			max = 0
		}
		if max > 64 {
			max = 64
		}
		msgs := box.Peek(in.NPC, max)
		return nil, NpcInboxGetOutput{OK: true, NPC: in.NPC, Count: len(msgs), Messages: msgs}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "npc_inbox_ack",
		Description: "Remove messages from an NPC's inbox by id. Call this after " +
			"successfully handling the messages returned by `npc_inbox_get`; unknown " +
			"ids are silently ignored.\n\n" +
			"Side-effect: WRITE (drops buffered messages). Always available.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in NpcInboxAckInput) (*mcp.CallToolResult, NpcInboxAckOutput, error) {
		if strings.TrimSpace(in.NPC) == "" {
			return nil, NpcInboxAckOutput{}, fmt.Errorf("npc is required")
		}
		removed := box.Ack(in.NPC, in.IDs)
		return nil, NpcInboxAckOutput{OK: true, Removed: removed}, nil
	})
}

// emitSyntheticEvent pushes a synthetic event (mcp-originated, not from the
// game mod) through the same MCP logging notification channel used for
// mod events. Uses the same envelope shape as MakeEventForwarder so
// consumers see one uniform stream.
//
// When hermes is non-nil, the same event is also dispatched to the Hermes
// relay so the recipient NPC's profile receives a /v1/responses POST and
// wakes up immediately. We deliberately DETACH from the caller's context
// (context.Background) — the tool handler returns as soon as Enqueue is
// done, which would otherwise cancel the in-flight Hermes POST. The hermes
// relay must outlive the tool call.
func emitSyntheticEvent(ctx context.Context, server *mcp.Server, logger *slog.Logger, hermes bridge.EventHandler, name string, data json.RawMessage) {
	payload := map[string]any{
		"kind":      "stardew/event",
		"name":      name,
		"data":      data,
		"timestamp": time.Now().UnixMilli(),
	}
	server.Sessions()(func(sess *mcp.ServerSession) bool {
		if err := sess.Log(ctx, &mcp.LoggingMessageParams{
			Level: "info",
			Data:  payload,
		}); err != nil && logger != nil {
			logger.Debug("synthetic notify failed", "name", name, "err", err)
		}
		return true
	})
	if hermes != nil {
		go hermes(context.Background(), name, data)
	}
}
