package chat

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

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
	sb.WriteString("Respond in the player's language. Keep replies concise (1-3 sentences). Stay in character.")
	return sb.String()
}
