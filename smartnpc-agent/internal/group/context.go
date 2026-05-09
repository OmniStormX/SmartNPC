// Group-prompt rendering.
//
// BuildGroupPrompt assembles the runtime context block fed to an NPC when
// it's about to respond inside a group chat. The shape mirrors the F3
// HandleInternalQuery convention so the persona LLM treats it as a
// special-purpose user turn rather than the whole base prompt.
package group

import (
	"strings"
)

// MaxPromptHistoryEntries is the soft cap on how many recent messages we
// embed in the rendered prompt. Beyond this, older messages are dropped
// (oldest first).
const MaxPromptHistoryEntries = 20

// BuildGroupPrompt produces the user-message-shaped context block.
// The returned string is meant to be passed verbatim to the NPC agent's
// internal-query / persona pipeline; it does NOT include the persona /
// system prompt — that comes from the agent's own LoadPersona output.
//
// Output layout:
//
//	You are in a group conversation with: <peer1>, <peer2> and the player.
//
//	Recent conversation:
//	Player: ...
//	<NpcA>: ...
//	<NpcB>: ...
//
//	<Speaker> just said: "<content>"
//
//	Respond naturally as yourself. Keep responses concise (1-3 sentences).
//	Reply with [PASS] if you have nothing to add.
func BuildGroupPrompt(npcName string, group *GroupConversation, newMsg GroupMessage) string {
	var sb strings.Builder

	peers := otherParticipants(npcName, group.Participants)
	sb.WriteString("You are in a group conversation with: ")
	if len(peers) == 0 {
		sb.WriteString("the player only")
	} else {
		sb.WriteString(joinHumanList(peers))
		sb.WriteString(" and the player")
	}
	sb.WriteString(".\n\n")

	if recent := tailMessages(group.History, MaxPromptHistoryEntries); len(recent) > 0 {
		sb.WriteString("Recent conversation:\n")
		for _, m := range recent {
			sb.WriteString(formatSpeaker(m.Speaker))
			sb.WriteString(": ")
			sb.WriteString(m.Content)
			sb.WriteByte('\n')
		}
		sb.WriteByte('\n')
	}

	sb.WriteString(formatSpeaker(newMsg.Speaker))
	sb.WriteString(" just said: \"")
	sb.WriteString(newMsg.Content)
	sb.WriteString("\"\n\n")

	sb.WriteString("Respond naturally as yourself. Keep responses concise (1-3 sentences).\n")
	sb.WriteString("Reply with [PASS] if you have nothing to add.")
	return sb.String()
}

// otherParticipants returns the participant list excluding self. Order is
// preserved so prompts are deterministic given a stable participant list.
func otherParticipants(self string, all []string) []string {
	out := make([]string, 0, len(all))
	for _, name := range all {
		if !strings.EqualFold(name, self) {
			out = append(out, name)
		}
	}
	return out
}

// formatSpeaker renders the SpeakerPlayer constant as a friendly "Player"
// (matching the spec's prompt template). NPC names pass through as-is so
// case is preserved.
func formatSpeaker(s string) string {
	if s == SpeakerPlayer {
		return "Player"
	}
	return s
}

// tailMessages returns the last n messages from history (or the whole slice
// when shorter). Always a fresh sub-slice; callers must not mutate it.
func tailMessages(history []GroupMessage, n int) []GroupMessage {
	if n <= 0 || len(history) == 0 {
		return nil
	}
	if len(history) <= n {
		return history
	}
	return history[len(history)-n:]
}

// joinHumanList renders ["A","B","C"] as "A, B, C". One-element slices are
// returned verbatim. Used for the room roster line so the prompt reads as
// natural English regardless of participant count.
func joinHumanList(items []string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	default:
		return strings.Join(items, ", ")
	}
}

// IsPassReply reports whether reply matches the [PASS] sentinel that an NPC
// uses to opt out of speaking. Tolerant of leading/trailing whitespace and
// case so the LLM doesn't have to be precise.
func IsPassReply(reply string) bool {
	trimmed := strings.TrimSpace(reply)
	if trimmed == "" {
		return true
	}
	return strings.EqualFold(trimmed, "[PASS]")
}
