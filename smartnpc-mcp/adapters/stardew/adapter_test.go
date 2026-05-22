package stardew

import (
	"encoding/json"
	"testing"
)

// TestExtractSubject_PrefersNPC verifies the NPC field wins over target
// and to. This mirrors the precedence order documented in events.md.
func TestExtractSubject_NPCWins(t *testing.T) {
	data := json.RawMessage(`{"npc":"XiaMi","target":"Abigail","to":"Penny"}`)
	if got := extractSubject("chat_message", data); got != "XiaMi" {
		t.Errorf("got %q, want XiaMi", got)
	}
}

func TestExtractSubject_TargetFallback(t *testing.T) {
	data := json.RawMessage(`{"target":"Abigail"}`)
	if got := extractSubject("npc_interact", data); got != "Abigail" {
		t.Errorf("got %q, want Abigail", got)
	}
}

func TestExtractSubject_ToFallback(t *testing.T) {
	data := json.RawMessage(`{"to":"Penny"}`)
	if got := extractSubject("npc_message", data); got != "Penny" {
		t.Errorf("got %q, want Penny", got)
	}
}

func TestExtractSubject_NoSubjectFields(t *testing.T) {
	data := json.RawMessage(`{"text":"hi"}`)
	if got := extractSubject("chat_received", data); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestExtractSubject_MalformedJSON(t *testing.T) {
	data := json.RawMessage(`not json`)
	if got := extractSubject("chat_message", data); got != "" {
		t.Errorf("got %q, want empty (malformed should not panic)", got)
	}
}

// TestNew_DefaultsWSURL ensures the constructor fills in the SDV mod
// default when ws_url is omitted from the yaml config.
func TestNew_DefaultsWSURL(t *testing.T) {
	a := New(Config{}, nil)
	if a.cfg.WSURL == "" {
		t.Error("WSURL not defaulted")
	}
}
