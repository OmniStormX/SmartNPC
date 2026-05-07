package chat

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestParseHeartRange covers the "lo-hi" parser used by BehaviorAtHearts and
// the prompt-ordering helper. Malformed keys must never panic or return ok.
func TestParseHeartRange(t *testing.T) {
	cases := []struct {
		in       string
		lo, hi   int
		wantOK   bool
	}{
		{"0-2", 0, 2, true},
		{"3-5", 3, 5, true},
		{"9-10", 9, 10, true},
		{"10-10", 10, 10, true},
		{" 0 - 2 ", 0, 2, true}, // tolerant of spaces
		{"", 0, 0, false},
		{"5", 0, 0, false},
		{"a-b", 0, 0, false},
		{"-1-3", 0, 0, false},  // lo negative
		{"0-11", 0, 0, false},  // hi > 10
		{"5-3", 0, 0, false},   // lo > hi
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			lo, hi, ok := parseHeartRange(tc.in)
			if ok != tc.wantOK {
				t.Fatalf("ok=%v want %v", ok, tc.wantOK)
			}
			if ok && (lo != tc.lo || hi != tc.hi) {
				t.Errorf("(lo,hi)=(%d,%d) want (%d,%d)", lo, hi, tc.lo, tc.hi)
			}
		})
	}
}

// TestBehaviorAtHearts covers the heart-range → behavior lookup, including
// the boundary cases that matter for real gameplay (0, 2, 3, 8, 9, 10) and
// the clamp on out-of-range inputs.
func TestBehaviorAtHearts(t *testing.T) {
	p := &Persona{
		FriendshipBehaviors: map[string]FriendshipBehavior{
			"0-2":  {Tone: "cold"},
			"3-5":  {Tone: "warm"},
			"6-8":  {Tone: "close"},
			"9-10": {Tone: "intimate"},
		},
	}

	cases := []struct {
		hearts   int
		wantTone string
	}{
		{-5, "cold"},   // clamped to 0
		{0, "cold"},
		{2, "cold"},
		{3, "warm"},
		{5, "warm"},
		{6, "close"},
		{8, "close"},
		{9, "intimate"},
		{10, "intimate"},
		{99, "intimate"}, // clamped to 10
	}
	for _, tc := range cases {
		b, ok := p.BehaviorAtHearts(tc.hearts)
		if !ok {
			t.Errorf("hearts=%d: not found", tc.hearts)
			continue
		}
		if b.Tone != tc.wantTone {
			t.Errorf("hearts=%d: tone=%q want %q", tc.hearts, b.Tone, tc.wantTone)
		}
	}
}

// TestBehaviorAtHearts_NoBehaviors ensures agents without friendship_behaviors
// don't crash — the lookup simply returns ok=false.
func TestBehaviorAtHearts_NoBehaviors(t *testing.T) {
	p := &Persona{}
	_, ok := p.BehaviorAtHearts(5)
	if ok {
		t.Error("expected ok=false when no behaviors defined")
	}
}

// TestBehaviorAtHearts_SkipsBadKeys checks that malformed keys are silently
// ignored without poisoning valid lookups.
func TestBehaviorAtHearts_SkipsBadKeys(t *testing.T) {
	p := &Persona{
		FriendshipBehaviors: map[string]FriendshipBehavior{
			"junk":  {Tone: "nope"},
			"5-3":   {Tone: "nope"}, // inverted
			"0-10":  {Tone: "catch-all"},
		},
	}
	b, ok := p.BehaviorAtHearts(4)
	if !ok || b.Tone != "catch-all" {
		t.Errorf("got (%v, %v), want (catch-all, true)", b, ok)
	}
}

// TestLoadPersona_WithFriendshipBehaviors loads a full persona file and
// confirms all four behavior ranges round-trip through JSON and that the
// assembled system prompt mentions them in ascending order.
func TestLoadPersona_WithFriendshipBehaviors(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "persona.json")
	obj := map[string]any{
		"speaker":        "Test",
		"name":           "Test",
		"personality":    "friendly",
		"speaking_style": "brief",
		"background":     "test",
		"friendship_behaviors": map[string]any{
			"9-10": map[string]any{"tone": "intimate", "willingness": "very_high", "greeting": "oi"},
			"0-2":  map[string]any{"tone": "cold", "willingness": "low", "greeting": "hmph"},
			"6-8":  map[string]any{"tone": "close", "willingness": "high", "greeting": "hey!"},
			"3-5":  map[string]any{"tone": "warm", "willingness": "medium", "greeting": "hi"},
		},
	}
	b, _ := json.Marshal(obj)
	if err := os.WriteFile(tmp, b, 0644); err != nil {
		t.Fatal(err)
	}

	p, err := LoadPersona(tmp)
	if err != nil {
		t.Fatalf("LoadPersona: %v", err)
	}

	// 1. All four ranges parsed.
	if got := len(p.FriendshipBehaviors); got != 4 {
		t.Errorf("behavior count = %d, want 4", got)
	}
	if g, _ := p.BehaviorAtHearts(10); g.Tone != "intimate" {
		t.Errorf("10 hearts tone = %q", g.Tone)
	}

	// 2. System prompt contains every range in ascending order.
	sp := p.SystemPrompt
	order := []string{"0-2", "3-5", "6-8", "9-10"}
	pos := 0
	for _, key := range order {
		idx := strings.Index(sp[pos:], key+" hearts")
		if idx < 0 {
			t.Errorf("system prompt missing %q in order; prompt=%s", key, sp)
			break
		}
		pos += idx + len(key)
	}

	// 3. Greetings appear verbatim (LLM reads them as samples).
	for _, want := range []string{"hmph", "hi", "hey!", "oi"} {
		if !strings.Contains(sp, want) {
			t.Errorf("system prompt missing greeting %q", want)
		}
	}

	// 4. Guardrail: the prompt warns the LLM not to quote numeric hearts.
	if !strings.Contains(sp, "never quote the numbers") {
		t.Errorf("system prompt should caution against quoting heart numbers; prompt=%s", sp)
	}
}

// TestLoadPersona_ShippedAbigail loads the real personas/abigail.json shipped
// with the repo to make sure the JSON actually parses and exposes all four
// heart ranges. Catches accidental regressions when the file is hand-edited.
func TestLoadPersona_ShippedPersonas(t *testing.T) {
	cases := []struct {
		name string
		path string
	}{
		{"abigail", "../../../personas/abigail.json"},
		{"xiami", "../../../personas/xiami.json"},
		{"haley", "../../../personas/haley.json"},
		{"harvey", "../../../personas/harvey.json"},
		{"penny", "../../../personas/penny.json"},
		{"sebastian", "../../../personas/sebastian.json"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, err := LoadPersona(tc.path)
			if err != nil {
				t.Fatalf("LoadPersona(%s): %v", tc.path, err)
			}
			for _, key := range []string{"0-2", "3-5", "6-8", "9-10"} {
				b, ok := p.FriendshipBehaviors[key]
				if !ok {
					t.Errorf("%s: missing heart range %q", tc.name, key)
					continue
				}
				if b.Tone == "" {
					t.Errorf("%s[%s]: empty tone", tc.name, key)
				}
				if b.Willingness == "" {
					t.Errorf("%s[%s]: empty willingness", tc.name, key)
				}
				if b.Greeting == "" {
					t.Errorf("%s[%s]: empty greeting", tc.name, key)
				}
			}
			// Each band's mid-point should resolve to the band itself.
			for hearts, wantKey := range map[int]string{1: "0-2", 4: "3-5", 7: "6-8", 10: "9-10"} {
				got, ok := p.BehaviorAtHearts(hearts)
				if !ok {
					t.Errorf("%s: no behavior for %d hearts", tc.name, hearts)
					continue
				}
				if got.Tone != p.FriendshipBehaviors[wantKey].Tone {
					t.Errorf("%s: hearts=%d resolved to wrong band", tc.name, hearts)
				}
			}
		})
	}
}

// TestSortedFriendshipKeys verifies deterministic ordering regardless of map
// iteration order and graceful handling of malformed keys.
func TestSortedFriendshipKeys(t *testing.T) {
	p := &Persona{
		FriendshipBehaviors: map[string]FriendshipBehavior{
			"9-10":  {},
			"0-2":   {},
			"junk":  {}, // ignored
			"6-8":   {},
			"3-5":   {},
		},
	}
	got := p.sortedFriendshipKeys()
	want := []string{"0-2", "3-5", "6-8", "9-10"}
	if len(got) != len(want) {
		t.Fatalf("len=%d want %d; got=%v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestRangeKeyAtHearts returns both the matching range key and the behavior.
// Useful for downstream logic (e.g. injecting "you are currently at the 6-8
// heart band" into the prompt) and should stay in sync with BehaviorAtHearts.
func TestRangeKeyAtHearts(t *testing.T) {
	p := &Persona{
		FriendshipBehaviors: map[string]FriendshipBehavior{
			"0-2":  {Tone: "cold"},
			"3-5":  {Tone: "warm"},
			"6-8":  {Tone: "close"},
			"9-10": {Tone: "intimate"},
		},
	}
	cases := []struct {
		hearts  int
		wantKey string
	}{
		{0, "0-2"},
		{2, "0-2"},
		{3, "3-5"},
		{7, "6-8"},
		{10, "9-10"},
	}
	for _, tc := range cases {
		key, b, ok := p.RangeKeyAtHearts(tc.hearts)
		if !ok {
			t.Errorf("hearts=%d: not found", tc.hearts)
			continue
		}
		if key != tc.wantKey {
			t.Errorf("hearts=%d: key=%q want %q", tc.hearts, key, tc.wantKey)
		}
		if b.Tone != p.FriendshipBehaviors[tc.wantKey].Tone {
			t.Errorf("hearts=%d: tone mismatch", tc.hearts)
		}
	}

	// No-behaviors persona: ok=false, empty key.
	empty := &Persona{}
	if key, _, ok := empty.RangeKeyAtHearts(5); ok || key != "" {
		t.Errorf("empty persona: got (%q, %v), want (\"\", false)", key, ok)
	}
}
