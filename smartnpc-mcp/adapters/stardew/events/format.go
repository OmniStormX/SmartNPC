package events

import (
	"encoding/json"
	"fmt"

	"github.com/OmniStormX/SmartNPC/adapters/stardew/bridge"
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
			speaker := p.NPC
			if speaker == "" {
				speaker = p.Target
			}
			if speaker == "" {
				speaker = "your exact internal NPC name"
			}
			return fmt.Sprintf(
				"[private_chat npc=%q] %s says to you: %s\n\n"+
					"⚠️ This is a PRIVATE player turn. If you answer, you MUST call chat_say "+
					"with speaker=%q. Plain assistant text is invisible to the player; "+
					"do not finish with text-only output. Call at most one chat_say, then stop.",
				speaker, who, p.Text, speaker)
		}

	case bridge.EventChatReceived:
		var p ChatReceived
		if err := json.Unmarshal(data, &p); err == nil && p.Text != "" {
			if p.Source == "player_group" && p.GroupID != "" {
				return fmt.Sprintf(
					"[group_chat group_id=%q] Player says in the group: %s\n\n"+
						"⚠️ This is a GROUP-CHAT turn. If you decide to call chat_say, "+
						"you MUST pass channel=\"group\" AND group_id=%q. Forgetting "+
						"either argument routes the line to a private 1:1 panel that "+
						"no other group participant can see — the player will perceive "+
						"silence from your side.\n"+
						"Tool calls (game_*, npc_send_message, ...) and remaining silent "+
						"are still valid responses; this constraint applies ONLY when "+
						"chat_say fires.",
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
			return fmt.Sprintf(
				"⚡ SYSTEM: This is a day_started turn. Text-only output will be "+
					"silently IGNORED — you MUST use tool calls. If unsure of the "+
					"procedure, call `skill_view` to load `smartnpc-day-plan-policy`.\n\n"+
					"A new day begins: %s %d (%s), year %d.\n\n"+
					"⚠️ MANDATORY — call these tools IN ORDER:\n"+
					"  1. `game_get_time` — confirm day/season/year.\n"+
					"  2. `game_get_weather` — check weather (skip outdoor work if rainy).\n"+
					"  3. `npc_plan_day` with 3-8 entries spread across hours 7-22.\n\n"+
					"Do NOT produce assistant text before or after these tool calls. "+
					"Do NOT call `chat_say` — the player has not spoken to you. "+
					"This turn is valid ONLY if `npc_plan_day` is invoked.",
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
			kindHint := ""
			if p.Kind != "" {
				kindHint = fmt.Sprintf(" The sender tagged it kind=%q.", p.Kind)
			}
			return fmt.Sprintf(
				"[inter_npc_message from=%q to=%q] NPC %s sent you a private message: %s\n\n"+
					"⚠️ This is an INTER-NPC wake-up, NOT a player turn. The player did "+
					"NOT speak; do NOT chat_say back to the player as if they did.%s\n\n"+
					"Follow `smartnpc-inter-npc-message` (load it via skill_view if "+
					"unfamiliar). Mandatory flow:\n"+
					"  1. `npc_inbox_get(npc=%q)` to read the full item (id + kind).\n"+
					"  2. Decide audibility via `npc_get_position` + `player_get_status`.\n"+
					"  3. Branch on kind:\n"+
					"     • kind=\"query\"      → compose answer, call `npc_send_message"+
					"(to=%q, kind=\"reply\", ...)`. **Do NOT chat_say** — the player has "+
					"not heard the question.\n"+
					"     • kind=\"behavioral\" → run the implied game tool, then maybe "+
					"one short chat_say IFF audible, then `npc_send_message(kind=\"reply\")`.\n"+
					"     • kind=\"reply\"      → fold into your NEXT player turn; do NOT "+
					"counter-reply via npc_send_message. No chat_say right now if player "+
					"isn't here.\n"+
					"  4. `npc_inbox_ack(npc=%q, ids=[...])` for every item handled.\n"+
					"Hard rule: never send `kind=\"query\"`/`\"behavioral\"` back to the "+
					"original sender — only `kind=\"reply\"`, once per inbox item.",
				p.From, p.To, p.From, p.Text, kindHint, p.To, p.From, p.To)
		}
	case bridge.EventNpcBroadcast:
		var p NpcBroadcast
		if err := json.Unmarshal(data, &p); err == nil {
			return fmt.Sprintf("NPC %s broadcast a %s event.", p.From, p.Kind)
		}
	case bridge.EventGameTimeTick:
		var p GameTimeTick
		if err := json.Unmarshal(data, &p); err == nil {
			// Rich format when mod sends day/season/year (M5.14+).
			// Falls back to bare hour format for older mod payloads.
			if p.Season != "" && p.Day > 0 {
				return fmt.Sprintf(
					"%s %s %d (Y%d) — %d:00. "+
						"This is a time-of-day tick; it is NOT a new day. "+
						"If you already called npc_plan_day today, do NOT plan again — "+
						"use npc_get_schedule if you need to check.",
					capitalize(p.Season), p.DayOfWeek, p.Day, p.Year, p.Hour)
			}
			return fmt.Sprintf("The time is now %d:00 (game hour %d). "+
				"If you already planned your day (npc_plan_day), do NOT plan again — "+
				"check with npc_get_schedule if unsure.", p.Hour, p.Hour)
		}
	case bridge.EventScheduleTrigger:
		var p ScheduleTrigger
		if err := json.Unmarshal(data, &p); err == nil {
			hint := ""
			if p.Reason != "" {
				hint = fmt.Sprintf(" (reason: %s)", p.Reason)
			}
			return fmt.Sprintf(
				"[schedule_trigger] It is now %d:00 — your planned activity is: `%s`%s.\n\n"+
					"If unsure of the procedure, call `skill_view` to load "+
					"`smartnpc-schedule-action-policy`.\n\n"+
					"Call `%s` NOW with concrete arguments you choose based on live "+
					"game state — your current location, what's nearby, weather, "+
					"inventory, who else is around, etc. The schedule only commits "+
					"to *which* tool, not its parameters; that's your call here.\n\n"+
					"If conditions have changed (rain on a watering chore, the "+
					"player is mid-conversation, you're not where you'd need to "+
					"be) you may adapt the parameters, swap to a related tool, "+
					"or skip — but briefly note why in your reasoning.",
				p.GameHour, p.Action, hint, p.Action)
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
