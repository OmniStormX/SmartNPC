package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/OmniStormX/SmartNPC/internal/bridge"
	"github.com/OmniStormX/SmartNPC/internal/events"
)

// ChatSayInput drives the `chat_say` tool.
type ChatSayInput struct {
	Speaker string `json:"speaker"           jsonschema:"display name shown in the chat box, e.g. \"SmartNPC\""`
	Text    string `json:"text"              jsonschema:"message body, plain text"`
	Color   string `json:"color,omitempty"   jsonschema:"optional color: white|yellow|green|red|cyan|blue|purple|gray (default yellow)"`
	// Channel scopes the reply to a conversation surface. "group" routes the
	// line exclusively to the group chat panel; "private" (default, empty) is
	// the standard per-NPC 1-on-1 channel. Mod-side uses this to prevent a
	// group reply from polluting a private NPC panel and vice versa.
	Channel string `json:"channel,omitempty" jsonschema:"conversation channel: \"group\" for group-chat replies, empty/\"private\" for 1-on-1 (default)"`
	GroupID string `json:"group_id,omitempty" jsonschema:"optional group id (required only when channel=\"group\")"`
}

// ChatSayOutput is the structured tool response. OK reflects whether the
// line was actually delivered to the mod: false means the call was a no-op
// (e.g. the per-wake-up budget was already used). Hint always carries a
// short instruction to the LLM about how to proceed — most importantly, when
// to stop emitting further tool calls.
type ChatSayOutput struct {
	OK   bool   `json:"ok"`
	Hint string `json:"hint,omitempty" jsonschema:"reminder to the caller — turn is over, do not call chat_say again"`
}

// ChatSayGuard enforces "one chat_say per wake-up" per speaker, scoped by
// channel. The two channels track independently:
//
//   - Private (1-on-1): each NPC has at most ONE chat_say budget per inbound
//     wake-up event addressed to them. The budget refreshes whenever a fresh
//     event with that NPC as recipient comes through the router (player
//     message, NPC-to-NPC message, npc_interact, ...). This is the runtime
//     enforcement of the "一问一答" design — even if the LLM ignores the
//     prompt-level "exactly one chat_say per reply" rule, the second call
//     within the same wake-up is hard-rejected.
//
//   - Group: each (group_id, speaker) pair has at most ONE chat_say budget
//     per player turn in that group. The budget refreshes only on player
//     input into the group (chat_received w/ source="player_group"), NOT on
//     NPC-to-NPC chatter — that's what lets group conversations be
//     probabilistic without an NPC turning into a monologue generator.
//
// Concurrency: methods are safe for concurrent use. The two maps share a
// single mutex; contention is minimal (one Allow / Reset per wake-up).
type ChatSayGuard struct {
	mu      sync.Mutex
	private map[string]bool            // speaker_lower → spoken since last wake-up
	group   map[string]map[string]bool // group_id → speaker_lower → spoken since last player input
}

// NewChatSayGuard returns an initialized, empty guard.
func NewChatSayGuard() *ChatSayGuard {
	return &ChatSayGuard{
		private: make(map[string]bool),
		group:   make(map[string]map[string]bool),
	}
}

// AllowPrivate records that speaker is about to talk in private (1-on-1);
// returns false if the speaker already used their chat_say budget for this
// wake-up. Empty speaker is treated as "skip the guard" and returns true.
func (g *ChatSayGuard) AllowPrivate(speakerLower string) bool {
	if speakerLower == "" {
		return true
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.private[speakerLower] {
		return false
	}
	g.private[speakerLower] = true
	return true
}

// AllowGroup records that speaker is about to talk in group; returns false if
// the speaker already spoke in the current round (since the last ResetGroup).
// Empty group is treated as "not a group call" and always returns true — the
// caller must skip AllowGroup entirely in that case (this fallback exists
// only as a safety net).
func (g *ChatSayGuard) AllowGroup(group, speakerLower string) bool {
	if group == "" {
		return true
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	sub := g.group[group]
	if sub == nil {
		sub = make(map[string]bool)
		g.group[group] = sub
	}
	if sub[speakerLower] {
		return false
	}
	sub[speakerLower] = true
	return true
}

// ResetPrivate clears the speaker's private spoken mark, opening a fresh
// chat_say budget for the next wake-up. Called by the router whenever an
// inbound event addressed to speaker enters the relay.
func (g *ChatSayGuard) ResetPrivate(speakerLower string) {
	if speakerLower == "" {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.private, speakerLower)
}

// ResetGroup clears the speak history for one group. Called on every fresh
// player input into that group.
func (g *ChatSayGuard) ResetGroup(group string) {
	if group == "" {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.group, group)
}

// MaybeResetGuard is the router-side helper that refreshes chat_say budgets
// based on the inbound event:
//
//   - chat_received w/ source="player_group" + group_id → ResetGroup
//   - any event carrying a recipient NPC (npc/to/target) → ResetPrivate(recipient)
//
// Safe to call on every router event; unknown shapes are silently ignored.
func MaybeResetGuard(g *ChatSayGuard, name string, data json.RawMessage) {
	if g == nil {
		return
	}
	if name == bridge.EventChatReceived {
		var p events.ChatReceived
		if err := json.Unmarshal(data, &p); err == nil {
			if p.Source == "player_group" && p.GroupID != "" {
				g.ResetGroup(p.GroupID)
			}
		}
	}
	if recipient, ok, _ := events.RecipientNPC(name, data); ok && recipient != "" {
		g.ResetPrivate(strings.ToLower(strings.TrimSpace(recipient)))
	}
}

func registerChat(s *mcp.Server, br *bridge.WSClient, guard *ChatSayGuard) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "chat_say",
		Description: "Speak a single in-character line as the NPC. The text appears in " +
			"the in-game chat box (bottom-left) attributed to `speaker`. This is the " +
			"ONLY tool that produces visible NPC dialogue — every reply to the player " +
			"must end with exactly one chat_say call.\n\n" +
			"When to call: after you have decided the final reply. Do not call it to " +
			"think out loud, narrate, or stream partial output.\n\n" +
			"Constraints:\n" +
			"- Plain UTF-8 text only. No markdown, code fences, JSON, emoji-as-image.\n" +
			"- Keep `text` short — one to three sentences. The SDV chat box truncates long lines.\n" +
			"- Stay in character: never reveal you are an AI / agent / Hermes / tool caller.\n" +
			"- `speaker` MUST be the NPC's English internal name (PascalCase), e.g. \"XiaMi\", " +
			"  \"Abigail\", \"Haley\", \"Harvey\", \"Penny\", \"Sebastian\". DO NOT use a Chinese " +
			"  display name like \"夏弥\" or \"阿比盖尔\" — the mod looks up NPCs by their internal " +
			"  name and will silently misroute your reply to a non-existent panel. When in doubt, " +
			"  use the same name the inbound event's `npc` field used.\n" +
			"- `color` is optional cosmetic (yellow default). Use sparingly for emphasis.\n" +
			"- `channel` defaults to private (1-on-1). When the inbound event was a " +
			"  `chat_received` with `source=\"player_group\"` (the rendered prompt prefix " +
			"  starts with `[group_chat group_id=...]`), you MUST set `channel=\"group\"` " +
			"  AND `group_id=<the inbound group_id>` — otherwise the reply leaks into the " +
			"  private toast/panel instead of the group panel. For private 1:1 chat, omit " +
			"  `channel`.\n\n" +
			"Per-wake-up budget (HARD limit, enforced at runtime):\n" +
			"- Private (channel empty / \"private\"): ONE chat_say per inbound event. A second " +
			"  chat_say from the same speaker within the same wake-up returns a STRUCTURED " +
			"  no-op result (`ok=false`, `hint` starts with `noop_chat_say_private_quota_exhausted`). " +
			"  This is NOT a tool error — do NOT retry with different text/color/speaker. " +
			"  Treat the `hint` as `TURN_END` and stop emitting tool calls. Budget refreshes " +
			"  when the next inbound event addressed to this NPC arrives.\n" +
			"- Group (channel=\"group\"): ONE chat_say per (group, speaker) per player turn. " +
			"  A second chat_say from the same speaker in the same group before the player " +
			"  speaks again returns the same shape with `noop_chat_say_group_quota_exhausted`. " +
			"  Same rule: stop emitting tool calls. Budget refreshes only on player input " +
			"  into that group, not on NPC-to-NPC chatter — that's what keeps group " +
			"  conversations probabilistic instead of monologuing.\n\n" +
			"Stop signal: every chat_say result (delivered or no-op) carries `TURN_END` in `hint`. " +
			"When you see `TURN_END`, you MUST stop generating tool calls and stop generating text " +
			"for this turn. Hermes will wake you on the next inbound event.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in ChatSayInput) (*mcp.CallToolResult, ChatSayOutput, error) {
		if in.Speaker == "" || in.Text == "" {
			return nil, ChatSayOutput{}, fmt.Errorf("speaker and text are required")
		}
		speakerKey := strings.ToLower(strings.TrimSpace(in.Speaker))
		isGroup := strings.EqualFold(in.Channel, "group") && in.GroupID != ""
		if guard != nil {
			switch {
			case isGroup:
				if !guard.AllowGroup(in.GroupID, speakerKey) {
					// Soft no-op (NOT an MCP error). Returning a Go error here
					// makes Hermes surface the call as a tool failure, which
					// triggers the LLM's "fix-and-retry" instinct (different
					// text/color/speaker) and produces the chat_say loop we're
					// trying to suppress. By returning a structured success
					// with ok=false + a directive Hint, the LLM sees this as
					// a normal tool result that explicitly tells it the turn
					// has ended — far more reliable at terminating the loop.
					return nil, ChatSayOutput{
						OK: false,
						Hint: fmt.Sprintf(
							"noop_chat_say_group_quota_exhausted: %q already spoke in group %q this player turn. "+
								"TURN_END — stop emitting tool calls now. The next chat_say budget for this "+
								"(group, speaker) refreshes ONLY when the player speaks into this group again. "+
								"Do NOT retry with different text/color/speaker.",
							in.Speaker, in.GroupID),
					}, nil
				}
			default:
				if !guard.AllowPrivate(speakerKey) {
					return nil, ChatSayOutput{
						OK: false,
						Hint: fmt.Sprintf(
							"noop_chat_say_private_quota_exhausted: %q already used the one chat_say budget "+
								"for this wake-up. TURN_END — stop emitting tool calls now. Private chat is "+
								"strictly one-question-one-answer (一问一答). The budget refreshes on the next "+
								"inbound event addressed to %q. Do NOT retry with different text/color/speaker.",
							in.Speaker, in.Speaker),
					}, nil
				}
			}
		}
		raw, err := br.Call(ctx, bridge.ActionChatSay, in)
		if err != nil {
			return nil, ChatSayOutput{}, fmt.Errorf("chat_say: %w", err)
		}
		var out ChatSayOutput
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &out)
		}
		out.OK = true
		out.Hint = "delivered. TURN_END — stop emitting tool calls and stop generating text now. " +
			"Do NOT call chat_say again for this event. Hermes will resume on the next inbound event."
		return nil, out, nil
	})
}
