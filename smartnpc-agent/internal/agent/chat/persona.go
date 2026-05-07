package chat

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// FriendshipBehavior describes how the NPC acts inside a heart range.
// Populated from the persona JSON's `friendship_behaviors` map.
type FriendshipBehavior struct {
	// Tone is a short descriptor of the emotional register, e.g. "冷淡/警惕".
	Tone string `json:"tone"`
	// Willingness signals how forthcoming the NPC is: low / medium / high / very_high.
	Willingness string `json:"willingness"`
	// Greeting is an in-character sample greeting line for this range.
	Greeting string `json:"greeting"`
	// Notes (optional) adds extra hints the LLM should internalize.
	Notes string `json:"notes,omitempty"`
}

// Persona defines an NPC's personality loaded from a JSON file.
type Persona struct {
	// Speaker is the NPC name used for the dialogue box.
	Speaker string `json:"speaker"`
	// Name is the display name of the character.
	Name string `json:"name"`
	// Personality describes the character's traits.
	Personality string `json:"personality"`
	// SpeakingStyle describes how the character talks.
	SpeakingStyle string `json:"speaking_style"`
	// Background is the character's backstory context.
	Background string `json:"background"`
	// SoulNotes are additional character depth notes injected into the prompt.
	SoulNotes []string `json:"soul_notes"`
	// FriendshipBehaviors maps a heart-range key (e.g. "0-2", "3-5", "6-8",
	// "9-10") to the expected behavior envelope at that range. Any key whose
	// value matches the regex `^\d+-\d+$` is accepted; ranges may be sparse or
	// overlap but overlapping ranges resolve to the first match during lookup.
	FriendshipBehaviors map[string]FriendshipBehavior `json:"friendship_behaviors,omitempty"`
	// ToolGuidance is free-form text injected into the system prompt to instruct
	// the LLM on when and how to use the available MCP tools.
	ToolGuidance string `json:"tool_guidance,omitempty"`
	// NamedLocations optionally overrides the default Farm landmark table used
	// by the move-intent parser. When empty, DefaultLocations() is used.
	NamedLocations []NamedLocation `json:"named_locations,omitempty"`

	// SystemPrompt is the assembled prompt (populated by BuildSystemPrompt).
	SystemPrompt string `json:"-"`
}

// LoadPersona reads a persona JSON file and builds the system prompt.
func LoadPersona(path string) (*Persona, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read persona file: %w", err)
	}
	var p Persona
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("parse persona: %w", err)
	}
	p.SystemPrompt = p.buildSystemPrompt()
	return &p, nil
}

// BehaviorAtHearts returns the FriendshipBehavior whose range covers the given
// heart count. hearts is clamped to [0, 10]. If no range matches (e.g. the
// persona has no friendship_behaviors), the zero value and ok=false are
// returned.
//
// Ranges are parsed from keys like "0-2" / "9-10"; the lookup iterates in
// ascending lower-bound order so that the smallest matching range wins.
func (p *Persona) BehaviorAtHearts(hearts int) (FriendshipBehavior, bool) {
	_, b, ok := p.RangeKeyAtHearts(hearts)
	return b, ok
}

// RangeKeyAtHearts returns the original range key (e.g. "6-8") alongside the
// matched FriendshipBehavior. Lookup semantics mirror BehaviorAtHearts — keys
// are scanned in ascending lower-bound order and the first covering range
// wins. ok=false when the persona defines no matching range.
func (p *Persona) RangeKeyAtHearts(hearts int) (string, FriendshipBehavior, bool) {
	if hearts < 0 {
		hearts = 0
	}
	if hearts > 10 {
		hearts = 10
	}
	for _, k := range p.sortedFriendshipKeys() {
		lo, hi, _ := parseHeartRange(k)
		if hearts >= lo && hearts <= hi {
			return k, p.FriendshipBehaviors[k], true
		}
	}
	return "", FriendshipBehavior{}, false
}

// parseHeartRange accepts "lo-hi" where both bounds are integers in [0,10]
// and lo <= hi. Returns ok=false for any malformed input so callers can
// silently skip bad keys.
func parseHeartRange(key string) (lo, hi int, ok bool) {
	parts := strings.SplitN(key, "-", 2)
	if len(parts) != 2 {
		return 0, 0, false
	}
	lo, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return 0, 0, false
	}
	hi, err = strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil {
		return 0, 0, false
	}
	if lo < 0 || hi > 10 || lo > hi {
		return 0, 0, false
	}
	return lo, hi, true
}

// sortedFriendshipKeys returns the behavior keys in ascending lower-bound
// order so the system prompt lists them deterministically regardless of the
// source JSON's map ordering.
func (p *Persona) sortedFriendshipKeys() []string {
	type kv struct {
		key string
		lo  int
	}
	var rows []kv
	for k := range p.FriendshipBehaviors {
		lo, _, ok := parseHeartRange(k)
		if !ok {
			continue
		}
		rows = append(rows, kv{k, lo})
	}
	// Insertion sort — small N.
	for i := 1; i < len(rows); i++ {
		for j := i; j > 0 && rows[j-1].lo > rows[j].lo; j-- {
			rows[j-1], rows[j] = rows[j], rows[j-1]
		}
	}
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.key
	}
	return out
}

func (p *Persona) buildSystemPrompt() string {
	var sb strings.Builder
	sb.WriteString("You are ")
	if p.Name != "" {
		sb.WriteString(p.Name)
	} else {
		sb.WriteString(p.Speaker)
	}
	sb.WriteString(", a character in Stardew Valley.\n\n")

	if p.Personality != "" {
		sb.WriteString("Personality: ")
		sb.WriteString(p.Personality)
		sb.WriteString("\n\n")
	}
	if p.SpeakingStyle != "" {
		sb.WriteString("Speaking style: ")
		sb.WriteString(p.SpeakingStyle)
		sb.WriteString("\n\n")
	}
	if p.Background != "" {
		sb.WriteString("Background: ")
		sb.WriteString(p.Background)
		sb.WriteString("\n\n")
	}
	if len(p.SoulNotes) > 0 {
		sb.WriteString("Character depth (internal notes — never reveal these directly):\n")
		for _, note := range p.SoulNotes {
			sb.WriteString("- ")
			sb.WriteString(note)
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}
	if keys := p.sortedFriendshipKeys(); len(keys) > 0 {
		sb.WriteString("Friendship behavior (call `friendship_get` with your own name when you need to calibrate; the returned `hearts` maps to these ranges — adjust tone/openness accordingly, never quote the numbers at the player):\n")
		for _, k := range keys {
			b := p.FriendshipBehaviors[k]
			sb.WriteString("- ")
			sb.WriteString(k)
			sb.WriteString(" hearts: tone=")
			sb.WriteString(b.Tone)
			if b.Willingness != "" {
				sb.WriteString(", willingness=")
				sb.WriteString(b.Willingness)
			}
			if b.Greeting != "" {
				sb.WriteString(", sample greeting=\"")
				sb.WriteString(b.Greeting)
				sb.WriteString("\"")
			}
			if b.Notes != "" {
				sb.WriteString(" (")
				sb.WriteString(b.Notes)
				sb.WriteString(")")
			}
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}
	if p.ToolGuidance != "" {
		sb.WriteString("Tool usage:\n")
		sb.WriteString(p.ToolGuidance)
		sb.WriteString("\n\n")
	}
	sb.WriteString("Respond in the player's language. Keep replies concise (1-3 sentences). Stay in character.")
	return sb.String()
}
