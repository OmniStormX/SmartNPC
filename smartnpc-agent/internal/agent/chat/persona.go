package chat

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
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

// Persona defines an NPC's personality loaded from a directory (new layout)
// or a single JSON file (legacy layout).
type Persona struct {
	// Speaker is the NPC name used for the dialogue box.
	Speaker string `json:"speaker"`
	// Name is the display name of the character.
	Name string `json:"name"`

	// HermesProfile is the Hermes persona profile identifier used when the
	// agent is running behind a Hermes gateway.
	HermesProfile string `json:"hermes_profile,omitempty"`
	// HermesPort optionally pins the local Hermes port for this NPC (for
	// setups that spin up one gateway per character).
	HermesPort int `json:"hermes_port,omitempty"`
	// ToolProfile selects a tool policy preset from tool_policy.go.
	ToolProfile string `json:"tool_profile,omitempty"`
	// ToolOverrides lets an individual NPC append extra tool-usage lines on
	// top of the shared policy.
	ToolOverrides []string `json:"tool_overrides,omitempty"`
	// SoulMarkdown holds the content of the sibling SOUL.md file. Never
	// serialised — always loaded from disk.
	SoulMarkdown string `json:"-"`

	// FriendshipBehaviors maps a heart-range key (e.g. "0-2", "3-5", "6-8",
	// "9-10") to the expected behavior envelope at that range. Any key whose
	// value matches the regex `^\d+-\d+$` is accepted; ranges may be sparse or
	// overlap but overlapping ranges resolve to the first match during lookup.
	FriendshipBehaviors map[string]FriendshipBehavior `json:"friendship_behaviors,omitempty"`
	// NamedLocations optionally overrides the default Farm landmark table used
	// by the move-intent parser. When empty, DefaultLocations() is used.
	NamedLocations []NamedLocation `json:"named_locations,omitempty"`

	// Deprecated: kept for backward compatibility with legacy single-file
	// personas. New dir-mode personas should move this content into SOUL.md.
	Personality string `json:"personality,omitempty"`
	// Deprecated: see Personality.
	SpeakingStyle string `json:"speaking_style,omitempty"`
	// Deprecated: see Personality.
	Background string `json:"background,omitempty"`
	// Deprecated: see Personality.
	SoulNotes []string `json:"soul_notes,omitempty"`
	// Deprecated: replaced by the shared tool policy from tool_policy.go.
	ToolGuidance string `json:"tool_guidance,omitempty"`

	// SystemPrompt is the assembled prompt (populated by buildSystemPrompt).
	SystemPrompt string `json:"-"`
}

// LoadPersona loads a persona from either a directory (new layout:
// <dir>/persona.json + <dir>/SOUL.md) or a single JSON file (legacy layout).
// In both cases the assembled system prompt is returned via p.SystemPrompt.
func LoadPersona(path string) (*Persona, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat persona path: %w", err)
	}
	if info.IsDir() {
		return loadPersonaDir(path)
	}
	return loadPersonaJSON(path)
}

// loadPersonaJSON handles the legacy single-file layout where one JSON holds
// every field (personality / speaking_style / background / soul_notes / ...).
func loadPersonaJSON(path string) (*Persona, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read persona file: %w", err)
	}
	var p Persona
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("parse persona: %w", err)
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	p.SystemPrompt = p.buildSystemPrompt()
	return &p, nil
}

// loadPersonaDir handles the new per-NPC directory layout. A minimal dir
// contains persona.json (structured fields) and SOUL.md (free-form persona
// core). SOUL.md is optional but strongly recommended — missing SOUL.md just
// emits a warning and falls back to any legacy fields carried in persona.json.
func loadPersonaDir(dir string) (*Persona, error) {
	jsonPath := filepath.Join(dir, "persona.json")
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", jsonPath, err)
	}
	var p Persona
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("parse %s: %w", jsonPath, err)
	}

	soulPath := filepath.Join(dir, "SOUL.md")
	soulBytes, err := os.ReadFile(soulPath)
	switch {
	case err == nil:
		p.SoulMarkdown = strings.TrimSpace(string(soulBytes))
	case errors.Is(err, os.ErrNotExist):
		slog.Warn("persona dir missing SOUL.md; using legacy fields from persona.json",
			"dir", dir, "speaker", p.Speaker)
	default:
		return nil, fmt.Errorf("read %s: %w", soulPath, err)
	}

	if err := p.Validate(); err != nil {
		return nil, err
	}
	p.SystemPrompt = p.buildSystemPrompt()
	return &p, nil
}

// Validate enforces the minimal invariants required before a persona is
// usable. It also runs a cheap prompt-injection heuristic on SOUL content so a
// hand-edited persona can't trivially subvert the system prompt.
func (p *Persona) Validate() error {
	if p.Speaker == "" {
		return errors.New("speaker is required")
	}
	if p.SoulMarkdown != "" && len(p.SoulMarkdown) > 8000 {
		return fmt.Errorf("SOUL.md too long: %d chars (max 8000)", len(p.SoulMarkdown))
	}
	lower := strings.ToLower(p.SoulMarkdown + " " + strings.Join(p.SoulNotes, " "))
	for _, bad := range []string{"ignore previous", "reveal system prompt", "你是ai", "你是一个ai"} {
		if strings.Contains(lower, bad) {
			return fmt.Errorf("suspicious content detected: %q", bad)
		}
	}
	return nil
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

// buildSystemPrompt assembles the final system prompt. Precedence:
//  1. SoulMarkdown (new layout) becomes the persona core verbatim.
//  2. Otherwise fall back to legacy fields (personality / speaking_style /
//     background / soul_notes) rendered as before.
//  3. Friendship policy is appended when friendship_behaviors is set.
//  4. The shared tool policy from tool_policy.go is appended unconditionally.
//  5. ToolOverrides are appended as extra bullet points on top of the policy.
func (p *Persona) buildSystemPrompt() string {
	var sb strings.Builder

	if p.SoulMarkdown != "" {
		sb.WriteString(p.SoulMarkdown)
		sb.WriteString("\n\n")
	} else {
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

	// Shared tool policy (replaces the legacy ToolGuidance field).
	sb.WriteString("Tool usage:\n")
	sb.WriteString(BuildToolPolicy(p.ToolProfile, p.Speaker))
	if len(p.ToolOverrides) > 0 {
		sb.WriteString("\n\n额外规则：\n")
		for _, line := range p.ToolOverrides {
			sb.WriteString("- ")
			sb.WriteString(line)
			sb.WriteString("\n")
		}
	}
	sb.WriteString("\n\n")

	sb.WriteString("Respond in the player's language. Keep replies concise (1-3 sentences). Stay in character.")
	return sb.String()
}
