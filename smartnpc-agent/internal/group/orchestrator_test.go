package group

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// fakeRouter is a synchronous AgentRouter for tests. Records every
// PromptInGroup invocation and returns scripted replies.
type fakeRouter struct {
	mu      sync.Mutex
	agents  []string
	replies map[string][]string // npc → reply queue
	calls   []routerCall
	err     error
}

type routerCall struct {
	NPC      string
	Prompt   string
	LastMsg  GroupMessage
}

func (r *fakeRouter) ListAgents() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.agents))
	copy(out, r.agents)
	return out
}

func (r *fakeRouter) PromptInGroup(_ context.Context, npc string, prompt string, last GroupMessage) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, routerCall{NPC: npc, Prompt: prompt, LastMsg: last})
	if r.err != nil {
		return "", r.err
	}
	q := r.replies[npc]
	if len(q) == 0 {
		return "(no reply)", nil
	}
	out := q[0]
	r.replies[npc] = q[1:]
	return out, nil
}

func (r *fakeRouter) callsFor(npc string) []routerCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []routerCall
	for _, c := range r.calls {
		if c.NPC == npc {
			out = append(out, c)
		}
	}
	return out
}

// newSyncOrch creates an Orchestrator whose dispatcher runs respondents
// inline (no goroutines, no Delays) so tests can assert immediately after
// OnMessage returns.
func newSyncOrch(t *testing.T, router AgentRouter, cfg Config) *Orchestrator {
	t.Helper()
	o := NewOrchestrator(router, cfg, nil)
	o.dispatch = func(ctx context.Context, gid string, d RespondentDecision, last GroupMessage) {
		o.runRespondent(ctx, gid, d, last)
	}
	// Deterministic id generator for stable assertions.
	var i int
	o.idFn = func() string {
		i++
		return "g" + itoa(i)
	}
	o.nowFn = func() time.Time { return time.Unix(1_700_000_000, 0) }
	return o
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	digits := []byte{}
	for i > 0 {
		digits = append([]byte{byte('0' + i%10)}, digits...)
		i /= 10
	}
	return string(digits)
}

// ── group lifecycle ────────────────────────────────────────────────────

func TestCreateGroup_RegistersAndValidates(t *testing.T) {
	r := &fakeRouter{agents: []string{"Abigail", "Sebastian"}}
	o := newSyncOrch(t, r, DefaultConfig())

	id, err := o.CreateGroup([]string{"Abigail", "Sebastian"})
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty group id")
	}
	if g := o.GetGroup(id); g == nil || len(g.Participants) != 2 {
		t.Errorf("group not registered correctly: %+v", g)
	}
	if got := o.ListGroups(); len(got) != 1 || got[0] != id {
		t.Errorf("ListGroups = %v, want [%s]", got, id)
	}
}

func TestCreateGroup_RejectsUnknownAgent(t *testing.T) {
	r := &fakeRouter{agents: []string{"Abigail"}}
	o := newSyncOrch(t, r, DefaultConfig())
	if _, err := o.CreateGroup([]string{"Ghost"}); err == nil {
		t.Error("expected error for unknown agent")
	}
}

func TestAddRemoveParticipant(t *testing.T) {
	r := &fakeRouter{agents: []string{"Abigail", "Sebastian", "Penny"}}
	o := newSyncOrch(t, r, DefaultConfig())
	id, _ := o.CreateGroup([]string{"Abigail"})

	if err := o.AddParticipant(id, "Sebastian"); err != nil {
		t.Fatalf("AddParticipant: %v", err)
	}
	if g := o.GetGroup(id); len(g.Participants) != 2 {
		t.Errorf("after add: %v", g.Participants)
	}

	if err := o.RemoveParticipant(id, "Abigail"); err != nil {
		t.Fatalf("RemoveParticipant: %v", err)
	}
	g := o.GetGroup(id)
	if len(g.Participants) != 1 || g.Participants[0] != "Sebastian" {
		t.Errorf("after remove: %v", g.Participants)
	}
	// Stats entry remains so historic messages stay attributable.
	if s := g.statsFor("Abigail"); s.IsActive {
		t.Errorf("Abigail.IsActive = true after remove")
	}
}

func TestAddParticipant_UnknownGroupErrors(t *testing.T) {
	o := newSyncOrch(t, &fakeRouter{agents: []string{"A"}}, DefaultConfig())
	if err := o.AddParticipant("does-not-exist", "A"); !errors.Is(err, ErrGroupNotFound) {
		t.Errorf("got %v, want ErrGroupNotFound", err)
	}
}

// ── OnMessage triggers PromptInGroup for selected NPCs ───────────────

func TestOnMessage_TriggersResponses(t *testing.T) {
	r := &fakeRouter{
		agents:  []string{"Abigail", "Sebastian"},
		replies: map[string][]string{"Abigail": {"[PASS]"}, "Sebastian": {"[PASS]"}},
	}
	cfg := DefaultConfig()
	cfg.Turn.BaseResponseChance = 0.95 // ensure picks under any rng
	cfg.Turn.MaxSimultaneous = 2
	o := newSyncOrch(t, r, cfg)
	id, _ := o.CreateGroup([]string{"Abigail", "Sebastian"})

	o.OnMessage(context.Background(), id, GroupMessage{
		Speaker: SpeakerPlayer, Content: "Good morning everyone!",
	})

	if len(r.calls) == 0 {
		t.Fatal("expected ≥1 PromptInGroup call")
	}
	// History must contain the player's message.
	g := o.GetGroup(id)
	if len(g.History) == 0 || g.History[0].Speaker != SpeakerPlayer {
		t.Errorf("first history entry should be player: %+v", g.History)
	}
}

// ── [PASS] reply does not fold back into the room ───────────────────

func TestOnMessage_PassReplyDropped(t *testing.T) {
	r := &fakeRouter{
		agents:  []string{"Abigail", "Sebastian"},
		replies: map[string][]string{"Abigail": {"[PASS]"}, "Sebastian": {"[PASS]"}},
	}
	cfg := DefaultConfig()
	cfg.Turn.BaseResponseChance = 0.95
	cfg.Turn.MaxSimultaneous = 2
	o := newSyncOrch(t, r, cfg)
	id, _ := o.CreateGroup([]string{"Abigail", "Sebastian"})

	o.OnMessage(context.Background(), id, GroupMessage{
		Speaker: SpeakerPlayer, Content: "anything?",
	})

	g := o.GetGroup(id)
	// History should contain ONLY the player's message — no [PASS] entries.
	for _, m := range g.History {
		if IsPassReply(m.Content) {
			t.Errorf("[PASS] leaked into history: %+v", m)
		}
	}
	if len(g.History) != 1 {
		t.Errorf("expected 1 history entry, got %d: %+v", len(g.History), g.History)
	}
}

// ── non-PASS reply gets folded back; chain decay caps depth ─────────

func TestOnMessage_NonPassReplyAppendsAndCascades(t *testing.T) {
	r := &fakeRouter{
		agents: []string{"Abigail", "Sebastian"},
		// Each NPC speaks exactly once, then PASSes. With depth 0 → both
		// reply, depth 1 → maybe one replies, depth 2 → caps out.
		replies: map[string][]string{
			"Abigail":   {"morning, farmer!", "[PASS]", "[PASS]"},
			"Sebastian": {"hi.", "[PASS]", "[PASS]"},
		},
	}
	cfg := DefaultConfig()
	cfg.Turn.BaseResponseChance = 0.99
	cfg.Turn.NPCtoNPCChance = 0.99
	cfg.Turn.MaxSimultaneous = 2
	cfg.MaxChainDepth = 2
	o := newSyncOrch(t, r, cfg)
	id, _ := o.CreateGroup([]string{"Abigail", "Sebastian"})

	o.OnMessage(context.Background(), id, GroupMessage{
		Speaker: SpeakerPlayer, Content: "good morning!",
	})

	g := o.GetGroup(id)
	if len(g.History) < 2 {
		t.Fatalf("expected NPC replies to land in history; got: %+v", g.History)
	}
	// Player message must still be the first entry.
	if g.History[0].Speaker != SpeakerPlayer {
		t.Errorf("first history entry not player: %+v", g.History[0])
	}
	// All non-player entries must be NPCs we actually scripted.
	for _, m := range g.History {
		if m.Speaker == SpeakerPlayer {
			continue
		}
		if m.Speaker != "Abigail" && m.Speaker != "Sebastian" {
			t.Errorf("unexpected speaker in history: %+v", m)
		}
	}
}

// ── unknown group OnMessage is a quiet no-op ─────────────────────────

func TestOnMessage_UnknownGroupNoOp(t *testing.T) {
	r := &fakeRouter{agents: []string{"Abigail"}}
	o := newSyncOrch(t, r, DefaultConfig())
	o.OnMessage(context.Background(), "ghost-id", GroupMessage{Speaker: "player", Content: "hi"})
	if len(r.calls) != 0 {
		t.Errorf("expected zero calls for unknown group, got: %+v", r.calls)
	}
}

// ── BuildGroupPrompt sanity ─────────────────────────────────────────

func TestBuildGroupPrompt_FormatsAllSections(t *testing.T) {
	g := &GroupConversation{
		Participants: []string{"Abigail", "Sebastian"},
		History: []GroupMessage{
			{Speaker: SpeakerPlayer, Content: "hi"},
			{Speaker: "Abigail", Content: "hey"},
		},
	}
	last := GroupMessage{Speaker: "Abigail", Content: "hey"}
	prompt := BuildGroupPrompt("Sebastian", g, last)

	// Roster mentions peer (not self) plus the player.
	if !contains(prompt, "Abigail") {
		t.Error("prompt missing peer Abigail")
	}
	if contains(prompt, "with: Sebastian") {
		t.Error("prompt should not name self in roster")
	}
	if !contains(prompt, "and the player") {
		t.Error("prompt missing player mention")
	}
	// Recent block + last-said block + PASS instruction.
	if !contains(prompt, "Recent conversation:") {
		t.Error("prompt missing recent-conversation header")
	}
	if !contains(prompt, "Abigail just said: \"hey\"") {
		t.Error("prompt missing last-said line")
	}
	if !contains(prompt, "[PASS]") {
		t.Error("prompt missing [PASS] instruction")
	}
}

func TestIsPassReply(t *testing.T) {
	cases := map[string]bool{
		"[PASS]":   true,
		"  [pass] ": true,
		"":         true,
		"[PASS] yes": false,
		"hello":    false,
	}
	for in, want := range cases {
		if got := IsPassReply(in); got != want {
			t.Errorf("IsPassReply(%q) = %v, want %v", in, got, want)
		}
	}
}

// helper
func contains(s, substr string) bool { return indexOf(s, substr) >= 0 }
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
