package events

import (
	"encoding/json"
	"fmt"

	"github.com/smartnpc/smartnpc-mcp/internal/bridge"
)

// capitalize returns s with its first rune upper-cased. Empty input is a
// no-op. Used for presenting season names (e.g. "spring" -> "Spring") to
// the Hermes agent without pulling in unicode/cases.
func capitalize(s string) string {
	if s == "" {
		return s
	}
	head := s[:1]
	switch head {
	case "a", "b", "c", "d", "e", "f", "g", "h", "i", "j",
		"k", "l", "m", "n", "o", "p", "q", "r", "s", "t",
		"u", "v", "w", "x", "y", "z":
		return string(head[0]-32) + s[1:]
	}
	return s
}

// FormatForHermes renders an event into a short human-readable string
// suitable for passing as the `input` field to Hermes Gateway's
// /v1/responses endpoint. The goal is to give the NPC's Hermes profile
// enough context in one line to decide how to react, without leaking
// implementation details (JSON, MCP, tool names).
//
// Examples:
//
//	chat_message   -> "Farmer says to you: 你好"
//	npc_interact   -> "Player walked up to you and wants to talk."
//	day_started    -> "A new day begins: Spring 5 (Monday), year 1."
//	npc_message    -> "NPC Abigail says to you: 农场这边好像出事了"
//
// Returns a fallback description for events we don't have a specialized
// formatter for — keeps the system forgiving as new events are added.
func FormatForHermes(name string, data json.RawMessage) string {
	switch name {
	case bridge.EventChatMessage:
		var p ChatMessage
		if err := json.Unmarshal(data, &p); err == nil && p.Text != "" {
			who := "Farmer"
			if p.Source != "" && p.Source != "player" {
				who = p.Source
			}
			return fmt.Sprintf("%s says to you: %s", who, p.Text)
		}
	case bridge.EventChatReceived:
		var p ChatReceived
		if err := json.Unmarshal(data, &p); err == nil && p.Text != "" {
			if p.Source == "player_group" && p.GroupID != "" {
				return fmt.Sprintf(
					"[group_chat group_id=%q] Player says in the group: %s "+
						"(any chat_say reply must include channel=\"group\" and "+
						"group_id=%q; tool calls and silence remain valid)",
					p.GroupID, p.Text, p.GroupID)
			}
			return fmt.Sprintf("Someone in the chat says: %s", p.Text)
		}
	case bridge.EventNpcInteract:
		var p NpcInteract
		if err := json.Unmarshal(data, &p); err == nil {
			return "The player walked up to you and opened a conversation."
		}
	case bridge.EventDayStarted:
		var p DayStarted
		if err := json.Unmarshal(data, &p); err == nil {
			dow := p.DayOfWeek
			if dow == "" {
				dow = "?"
			}
			return fmt.Sprintf("A new day begins: %s %d (%s), year %d.",
				capitalize(p.Season), p.Day, dow, p.Year)
		}
	case bridge.EventLocationChanged:
		var p LocationChanged
		if err := json.Unmarshal(data, &p); err == nil {
			who := p.Who
			if p.Kind == "player" {
				who = "The player"
			}
			return fmt.Sprintf("%s moved from %s to %s.", who, p.FromMap, p.ToMap)
		}
	case bridge.EventFriendshipChanged:
		var p FriendshipChanged
		if err := json.Unmarshal(data, &p); err == nil {
			direction := "improved"
			if p.PointDelta < 0 {
				direction = "worsened"
			}
			return fmt.Sprintf("Your friendship with %s %s (%+d points, now %d hearts, trigger=%s).",
				p.NPC, direction, p.PointDelta, p.Hearts, p.Trigger)
		}
	case bridge.EventNpcMessage:
		var p NpcMessage
		if err := json.Unmarshal(data, &p); err == nil && p.Text != "" {
			return fmt.Sprintf("NPC %s says to you (privately): %s", p.From, p.Text)
		}
	case bridge.EventNpcBroadcast:
		var p NpcBroadcast
		if err := json.Unmarshal(data, &p); err == nil {
			return fmt.Sprintf("NPC %s broadcast a %s event.", p.From, p.Kind)
		}
	case bridge.EventDebugProactiveTrigger:
		var p DebugProactiveTrigger
		if err := json.Unmarshal(data, &p); err == nil && p.NPC != "" {
			return fmt.Sprintf(
				"[debug proactive-visit force=true target=%s] The operator "+
					"forced an immediate proactive-visit trigger for testing. "+
					"Follow smartnpc-proactive-visit but SKIP steps 1 and 2 "+
					"(cool-down and dice — this run is intentional, not "+
					"scheduled). Do steps 3-5 normally: check "+
					"player_get_status, check game_get_time politeness "+
					"window, then npc_summon+npc_emote(sparkle)+chat_say. "+
					"Do NOT write a 'proactive-visit: last=' memory line "+
					"for this run — operator testing should not poison the "+
					"60-minute cool-down.",
				p.NPC)
		}
	}

	return fmt.Sprintf("Game event %q occurred.", name)
}
