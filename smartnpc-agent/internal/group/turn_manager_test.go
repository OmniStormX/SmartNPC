package group

import (
	"math/rand"
	"strings"
	"testing"
	"time"
)

// fixedRng returns a *rand.Rand that always rolls below 0.5 — i.e. a
// deterministic "always pass the dice" generator so chance > 0 ⇒ pick.
// Used to test the deterministic side of DetermineRespondents (selection
// rules, not probability draws).
func fixedRng() *rand.Rand {
	return rand.New(rand.NewSource(0))
}

// alwaysHitRng returns an rng whose first N calls all return 0 — guaranteed
// to take any chance > 0. Helpful when we want to test that "chance > 0
// ⇒ NPC selected" without flakiness.
type alwaysHitRng struct{}

func (alwaysHitRng) Float64() float64                  { return 0 }
func (alwaysHitRng) Int63() int64                      { return 0 }
func (alwaysHitRng) Seed(int64)                        {}

// makeAlwaysHitTM constructs a TurnManager whose rng always picks (rolls
// 0 < chance for any positive chance).
func makeAlwaysHitTM(cfg TurnConfig) *TurnManager {
	tm := NewTurnManager(cfg, fixedRng())
	// Replace the inner rng with one that always rolls 0 by re-seeding;
	// rand.NewSource(0) deterministically rolls < 0.5 on its first calls,
	// so for chance >= 0.5 the rolls hit. For tighter control use a
	// custom rng wrapper.
	return tm
}

// rollAllRng is a math/rand.Source that always returns 0; exposed so the
// turn manager rolls below any positive chance.
type rollAllRng struct{}

func (rollAllRng) Int63() int64 { return 0 }
func (rollAllRng) Seed(int64)   {}

func mustGroup(participants ...string) *GroupConversation {
	g := &GroupConversation{
		Participants: participants,
		MaxHistory:   60,
		stats:        make(map[string]*Participant),
		CreatedAt:    time.Now(),
		LastActivity: time.Now(),
	}
	for _, p := range participants {
		g.stats[p] = &Participant{Name: p, IsActive: true}
	}
	return g
}

// ── happy-path: player message reaches active NPCs ──────────────────────

func TestDetermineRespondents_PlayerMessage(t *testing.T) {
	g := mustGroup("Abigail", "Sebastian")
	tm := NewTurnManager(DefaultTurnConfig(), rand.New(rollAllRng{}))
	msg := GroupMessage{Speaker: SpeakerPlayer, Content: "morning everyone"}

	out := tm.DetermineRespondents(g, msg, 0)
	if len(out) == 0 {
		t.Fatal("expected ≥1 respondent for player message at chainDepth=0")
	}
	// MaxSimultaneous default is 2; both NPCs should be eligible.
	if len(out) > tm.Config().MaxSimultaneous {
		t.Errorf("got %d respondents > MaxSimultaneous=%d", len(out), tm.Config().MaxSimultaneous)
	}
	// Delays must be 1s, 2s, ... (no zero delays).
	for i, d := range out {
		want := time.Duration(i+1) * time.Second
		if d.Delay != want {
			t.Errorf("decision[%d].Delay = %v, want %v", i, d.Delay, want)
		}
	}
}

// ── NPC message uses NPCtoNPCChance ─────────────────────────────────────

func TestDetermineRespondents_NPCMessage(t *testing.T) {
	g := mustGroup("Abigail", "Sebastian", "Penny")
	// NPC speaker → only Abigail and Penny remain candidates; with the
	// always-hit rng both should be picked but capped at MaxSimultaneous=2.
	tm := NewTurnManager(DefaultTurnConfig(), rand.New(rollAllRng{}))
	msg := GroupMessage{Speaker: "Sebastian", Content: "I'm tired"}

	out := tm.DetermineRespondents(g, msg, 0)
	for _, d := range out {
		if d.NPC == "Sebastian" {
			t.Errorf("speaker must not be picked: %+v", d)
		}
	}
	if len(out) == 0 {
		t.Errorf("expected ≥1 respondent with always-hit rng")
	}
}

// ── addressed boost selects the named NPC even with reluctant rng ──────

func TestDetermineRespondents_AddressedBoost(t *testing.T) {
	g := mustGroup("Abigail", "Sebastian")
	// Construct a config where base chance is too low to clear an
	// "always roll 0.5" rng without the boost; with boost the named NPC
	// must clear.
	cfg := DefaultTurnConfig()
	cfg.BaseResponseChance = 0.3
	cfg.AddressedBoost = 0.4 // → 0.7 once boosted
	tm := NewTurnManager(cfg, rand.New(halfRng{}))

	msg := GroupMessage{Speaker: SpeakerPlayer, Content: "Hey Sebastian, you free?"}
	out := tm.DetermineRespondents(g, msg, 0)

	var sawSebastian bool
	for _, d := range out {
		if d.NPC == "Sebastian" {
			sawSebastian = true
		}
	}
	if !sawSebastian {
		t.Errorf("addressed NPC Sebastian missing from respondents: %+v", out)
	}
}

// halfRng always returns 0.5 — every probability strictly > 0.5 hits,
// strictly < 0.5 misses. Useful to bracket a single selection threshold.
type halfRng struct{}

func (halfRng) Int63() int64 { return 1 << 62 } // → Float64() == 0.5
func (halfRng) Seed(int64)   {}

// ── consecutive-speak hard cap ──────────────────────────────────────────

func TestDetermineRespondents_ConsecutiveLimit(t *testing.T) {
	g := mustGroup("Abigail", "Sebastian")
	// Tail of history is Abigail × MaxConsecutiveSame → Abigail must be
	// excluded entirely.
	cfg := DefaultTurnConfig()
	cfg.MaxConsecutiveSame = 2
	for i := 0; i < cfg.MaxConsecutiveSame; i++ {
		g.History = append(g.History, GroupMessage{Speaker: "Abigail", Content: "..."})
	}
	tm := NewTurnManager(cfg, rand.New(rollAllRng{}))

	msg := GroupMessage{Speaker: SpeakerPlayer, Content: "anyone?"}
	out := tm.DetermineRespondents(g, msg, 0)

	for _, d := range out {
		if d.NPC == "Abigail" {
			t.Errorf("Abigail exceeded MaxConsecutiveSame; should be muted: %+v", d)
		}
	}
}

// ── chain decay: deeper chains pick fewer respondents ──────────────────

func TestDetermineRespondents_ChainDecay(t *testing.T) {
	g := mustGroup("Abigail", "Sebastian", "Penny")
	cfg := DefaultTurnConfig()
	cfg.NPCtoNPCChance = 0.5
	cfg.ChainDecay = 0.5
	// halfRng rolls 0.5 → at depth=0 chance 0.5 misses (need strictly >);
	// at depth=1 chance 0.25 misses; at depth=2 chance 0.125 misses too.
	tm := NewTurnManager(cfg, rand.New(halfRng{}))
	msg := GroupMessage{Speaker: "Abigail", Content: "the rain again"}

	if got := tm.DetermineRespondents(g, msg, 0); len(got) != 0 {
		t.Errorf("at chainDepth=0 with halfRng/cfg, expected 0 picks, got %+v", got)
	}
}

// ── MaxSimultaneous caps responder count ──────────────────────────────

func TestDetermineRespondents_MaxSimultaneous(t *testing.T) {
	g := mustGroup("A", "B", "C", "D", "E")
	cfg := DefaultTurnConfig()
	cfg.MaxSimultaneous = 2
	tm := NewTurnManager(cfg, rand.New(rollAllRng{}))
	msg := GroupMessage{Speaker: SpeakerPlayer, Content: "anything happening?"}

	out := tm.DetermineRespondents(g, msg, 0)
	if len(out) != 2 {
		t.Errorf("expected exactly 2 picks (MaxSimultaneous), got %d: %+v", len(out), out)
	}
}

// ── speaker is never picked to reply to themselves ─────────────────────

func TestDetermineRespondents_SkipsSelf(t *testing.T) {
	g := mustGroup("Abigail", "Sebastian")
	tm := NewTurnManager(DefaultTurnConfig(), rand.New(rollAllRng{}))
	msg := GroupMessage{Speaker: "Abigail", Content: "hello"}
	out := tm.DetermineRespondents(g, msg, 0)
	for _, d := range out {
		if strings.EqualFold(d.NPC, "Abigail") {
			t.Errorf("speaker selected as own respondent: %+v", d)
		}
	}
}

// ── isAddressed exposed via behaviour: ReplyTo also boosts ────────────

func TestDetermineRespondents_ReplyToTriggersBoost(t *testing.T) {
	g := mustGroup("Abigail", "Sebastian")
	cfg := DefaultTurnConfig()
	cfg.BaseResponseChance = 0.3
	cfg.AddressedBoost = 0.4
	tm := NewTurnManager(cfg, rand.New(halfRng{}))

	msg := GroupMessage{Speaker: SpeakerPlayer, Content: "what do you think?", ReplyTo: "Sebastian"}
	out := tm.DetermineRespondents(g, msg, 0)

	var sebFound bool
	for _, d := range out {
		if d.NPC == "Sebastian" {
			sebFound = true
		}
	}
	if !sebFound {
		t.Errorf("ReplyTo boost failed; Sebastian missing: %+v", out)
	}
}

// ── recent-speak penalty halves chance ────────────────────────────────

func TestDetermineRespondents_RecentSpeakPenalty(t *testing.T) {
	g := mustGroup("Abigail", "Sebastian")
	g.statsFor("Sebastian").LastSpoke = time.Now() // < 30s ago
	// Construct cfg where the un-penalised chance just barely beats 0.5
	// (cleared) but the halved chance does not. Base 0.7 → halved to 0.35.
	cfg := DefaultTurnConfig()
	cfg.BaseResponseChance = 0.7
	tm := NewTurnManager(cfg, rand.New(halfRng{}))
	msg := GroupMessage{Speaker: SpeakerPlayer, Content: "hi"}
	out := tm.DetermineRespondents(g, msg, 0)
	for _, d := range out {
		if d.NPC == "Sebastian" {
			t.Errorf("Sebastian should be penalised below 0.5 threshold: %+v", out)
		}
	}
}
