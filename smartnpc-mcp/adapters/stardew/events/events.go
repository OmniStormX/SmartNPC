// Package events defines typed payloads for the events that smartnpc-mcp
// surfaces to MCP clients (Hermes profiles, Claude
// Desktop, ...).
//
// Two sources:
//
//   - SMAPI mod events arriving via bridge.WSClient. The mod is free to add
//     more fields in the future; payloads are forward-compatible — consumers
//     that don't know a new field MUST ignore it rather than error.
//
//   - Synthetic events produced by smartnpc-mcp itself (e.g. inter-NPC
//     messaging via npc_send_message). These are emitted through the same
//     notification channel (tools.MakeEventForwarder) so consumers get a
//     single uniform stream.
//
// Shapes below are the canonical ground truth — docs/events.md mirrors them
// in human form. If you change a payload here, update the doc in the same PR.
//
// Not all events listed have mod-side implementations yet; schema-only
// entries are marked "Reserved" and will be populated by later milestones.
package events

import (
	"encoding/json"

	"github.com/OmniStormX/SmartNPC/adapters/stardew/bridge"
)

// ChatMessage — player sent a line targeted at a specific NPC via the
// in-game chat panel. Emitted by smapi-mod ModEntry.OnChatSend.
//
// This is the primary "player talks to NPC" trigger for Hermes profiles.
type ChatMessage struct {
	NPC    string `json:"npc"`    // recipient NPC internal name, e.g. "XiaMi"
	Target string `json:"target"` // redundant with npc today; reserved for multi-target
	Text   string `json:"text"`   // raw UTF-8 text typed by the player
	Source string `json:"source"` // always "player" today
}

// ChatReceived — legacy generic chat channel. Emitted by the old single
// chat window and group chat. Not recommended for new consumers; prefer
// ChatMessage for targeted player→NPC conversation.
//
// Source enum:
//   - "player"        — player typed in the (non-group) public chat window
//   - "player_group"  — player typed in a group-chat session; GroupID is set
type ChatReceived struct {
	Text    string `json:"text"`
	Source  string `json:"source"`
	GroupID string `json:"group_id,omitempty"` // present iff source == "player_group"
}

// NpcInteract — player clicked an Agent-managed NPC sprite. Emitted by
// NpcDialoguePatch.PumpInteractions. Should wake the target NPC's Hermes
// profile for a proactive greeting.
type NpcInteract struct {
	NPC    string `json:"npc"`    // clicked NPC internal name
	Source string `json:"source"` // always "player" today
}

// GroupCreate — legacy group chat session was opened in the mod UI.
// Carries the list of NPC names included.
type GroupCreate struct {
	Participants []string `json:"participants"`
}

// DayStarted — a new in-game day began. Emitted by the SMAPI mod at the
// start of each game day. Triggers scheduler clear + NPC day planning.
type DayStarted struct {
	Day      int    `json:"day"`      // 1-28
	Season   string `json:"season"`   // spring/summer/fall/winter
	Year     int    `json:"year"`     // in-game year
	DayOfWeek string `json:"day_of_week"`
}

// GameTimeTick — emitted every in-game hour (when time changes to XX:00).
// Drives the NPC schedule dispatcher in the router.
type GameTimeTick struct {
	Hour      int    `json:"hour"`       // 6-25 (SDV convention)
	Time      int    `json:"time"`       // raw SDV time value, e.g. 900 = 9:00am
	Day       int    `json:"day"`        // 1-28
	Season    string `json:"season"`     // spring/summer/fall/winter
	Year      int    `json:"year"`       // in-game year
	DayOfWeek string `json:"day_of_week"` // short day name, e.g. Mon
}

// LocationChanged — a watched NPC or the player moved between maps.
// Reserved; not yet emitted.
type LocationChanged struct {
	Who       string `json:"who"`       // npc name or "player"
	Kind      string `json:"kind"`      // "npc" | "player"
	FromMap   string `json:"from_map"`
	ToMap     string `json:"to_map"`
}

// FriendshipChanged — friendship points for an NPC changed by more than
// a trivial threshold. Reserved; not yet emitted.
type FriendshipChanged struct {
	NPC        string `json:"npc"`
	Points     int    `json:"points"`      // new raw points
	PointDelta int    `json:"point_delta"` // change since last notification
	Hearts     int    `json:"hearts"`      // new heart level
	Trigger    string `json:"trigger"`     // "gift" | "quest" | "decay" | "other"
}

// NpcMessage — synthetic event emitted when one NPC calls npc_send_message
// targeting a single recipient. Fans out through the regular MCP
// notification channel; the recipient's Hermes profile decides whether
// and how to respond.
type NpcMessage struct {
	ID        string `json:"id"`             // uuid; echo back in npc_inbox_ack
	From      string `json:"from"`           // sender NPC name
	To        string `json:"to"`             // recipient NPC name
	Text      string `json:"text"`           // message body
	Kind      string `json:"kind,omitempty"` // optional free-form tag (e.g. "greeting", "alert")
	Timestamp int64  `json:"timestamp"`      // unix millis when sent
}

// NpcBroadcast — synthetic event emitted when an NPC calls
// npc_broadcast_event. No explicit recipient; every Hermes profile
// subscribed to the notification stream sees it.
type NpcBroadcast struct {
	From      string          `json:"from"` // sender NPC name
	Kind      string          `json:"kind"` // event category, e.g. "alarm", "party_invite"
	Data      json.RawMessage `json:"data,omitempty"`
	Timestamp int64           `json:"timestamp"` // unix millis when broadcast
}

// ScheduleTrigger — synthetic event emitted by the game-time scheduler
// when a planned activity's hour arrives. Delivered to the target NPC's
// Hermes profile so the LLM can execute the planned action.
//
// The trigger intentionally carries no tool parameters: the LLM decides
// concrete params at fire time based on live game state (where it is,
// who's nearby, weather, inventory, etc.).
type ScheduleTrigger struct {
	NPC         string `json:"npc"`                       // target NPC
	GameHour    int    `json:"game_hour"`                 // the hour that triggered
	GameMinute  int    `json:"game_minute,omitempty"`     // minute within the hour (0/10/20/30/40/50)
	GameMinutes int    `json:"game_minutes,omitempty"`    // absolute minute-of-day (hour*60+minute)
	Action      string `json:"action"`                    // planned MCP tool name
	Reason      string `json:"reason,omitempty"`          // LLM's original reasoning
}

// DebugProactiveTrigger — operator-initiated forced trigger of the
// smartnpc-visit SKILL for a named NPC. Emitted by the mod's
// `sn_proactive` SMAPI console command. The `npc` field drives
// hermesrelay routing; the format layer renders a system nudge that
// tells the target profile to skip the dice roll + cool-down and go
// straight to the availability/politeness checks.
type DebugProactiveTrigger struct {
	NPC string `json:"npc"` // PascalCase internal name, e.g. "Abigail"
}

// WorkflowLLMChoice is the payload for EventWorkflowLLMChoice — the engine
// asks the NPC's LLM to pick one option from a fixed list.
type WorkflowLLMChoice struct {
	RequestID string   `json:"request_id"`
	Prompt    string   `json:"prompt"`
	Options   []string `json:"options"`
}

// EventDescriptor is the uniform envelope all consumers see (identical to
// the payload tools.MakeEventForwarder writes into MCP logging notifications).
// Re-declared here so consumers don't need to import internal/tools.
type EventDescriptor struct {
	Kind      string          `json:"kind"` // always "stardew/event"
	Name      string          `json:"name"` // one of the bridge.Event* constants
	Data      json.RawMessage `json:"data"`
	Timestamp int64           `json:"timestamp"` // unix millis
}

// DecodeChatMessage pulls a typed ChatMessage out of a raw event payload.
// Returns zero value + error if the JSON does not match.
func DecodeChatMessage(data json.RawMessage) (ChatMessage, error) {
	var out ChatMessage
	err := json.Unmarshal(data, &out)
	return out, err
}

// DecodeNpcInteract pulls a typed NpcInteract out of a raw event payload.
func DecodeNpcInteract(data json.RawMessage) (NpcInteract, error) {
	var out NpcInteract
	err := json.Unmarshal(data, &out)
	return out, err
}

// IsMod reports whether the event name corresponds to a SMAPI mod-sourced
// event that the mod actually emits today. Reserved schemas declared in
// bridge but not yet wired return false — use IsReserved for those.
func IsMod(name string) bool {
	switch name {
	case bridge.EventChatMessage,
		bridge.EventChatReceived,
		bridge.EventNpcInteract,
		bridge.EventGroupCreate,
		bridge.EventDayStarted,
		bridge.EventGameTimeTick:
		return true
	}
	return false
}

// IsReserved reports whether the event name is declared as a mod-sourced
// event in the protocol but is not currently emitted by the mod. Useful
// for typechecking against future schemas without breaking on unknown
// names (callers can choose to silently ignore reserved events).
func IsReserved(name string) bool {
	switch name {
	case bridge.EventLocationChanged,
		bridge.EventFriendshipChanged,
		bridge.EventNpcPerceptionUpdate:
		return true
	}
	return false
}

// IsSynthetic reports whether the event name is emitted by smartnpc-mcp
// itself (inter-NPC messaging, schedule triggers, etc.).
func IsSynthetic(name string) bool {
	switch name {
	case bridge.EventNpcMessage, bridge.EventNpcBroadcast, bridge.EventScheduleTrigger:
		return true
	}
	return false
}

// RecipientNPC extracts the recipient NPC name from a raw event payload by
// probing the common recipient fields (npc / to / target) in priority order.
//
// Used by hermesrelay.ShouldRoute to decide whether an event should be
// forwarded to a specific NPC's Hermes profile.
//
// Returns:
//
//   - (name, true, nil)  — payload carries a non-empty recipient field
//   - ("",   false, nil) — payload is well-formed but has no recipient
//     (e.g. day_started, broadcast); caller treats this as "deliver to all"
//   - ("",   false, err) — payload is malformed JSON; caller decides policy
//
// The function is event-name agnostic: every typed event in this package
// that names a recipient uses one of npc/to/target, so a single probe
// suffices and the function stays open to schema additions.
func RecipientNPC(_ string, data json.RawMessage) (string, bool, error) {
	if len(data) == 0 {
		return "", false, nil
	}
	var probe struct {
		NPC    string `json:"npc"`
		To     string `json:"to"`
		Target string `json:"target"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return "", false, err
	}
	switch {
	case probe.NPC != "":
		return probe.NPC, true, nil
	case probe.To != "":
		return probe.To, true, nil
	case probe.Target != "":
		return probe.Target, true, nil
	}
	return "", false, nil
}
