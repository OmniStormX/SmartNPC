package scheduler

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"
)

// --- mocks ---

type mockRouter struct {
	mu      sync.Mutex
	agents  []string
	replies map[string]string // npcName → reply
	calls   []string         // history of TriggerProactive calls
}

func (m *mockRouter) ListAgents() []string { return m.agents }
func (m *mockRouter) TriggerProactive(_ context.Context, npcName, _ string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, npcName)
	if r, ok := m.replies[npcName]; ok {
		return r, nil
	}
	return "DECISION: NO\nREASON: not now", nil
}

type mockSession struct {
	mu    sync.Mutex
	calls []toolCall
	// responses keyed by tool name
	responses map[string]json.RawMessage
}

type toolCall struct {
	Name string
	Args map[string]any
}

func (m *mockSession) CallTool(_ context.Context, name string, args map[string]any) (json.RawMessage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, toolCall{Name: name, Args: args})
	if r, ok := m.responses[name]; ok {
		return r, nil
	}
	return json.RawMessage(`{}`), nil
}

func defaultGameTimeResp() json.RawMessage {
	return json.RawMessage(`{"hour":10,"total_days":5,"weather":"sunny","player_location":"Farm"}`)
}

func newTestScheduler(router *mockRouter, session *mockSession) *Scheduler {
	cfg := DefaultConfig()
	cfg.CheckInterval = time.Millisecond // doesn't matter for manual Tick
	s := New(router, session, cfg, nil)
	s.nowFn = func() time.Time { return time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC) }
	return s
}

// --- tests ---

func TestTick_GlobalCooldown(t *testing.T) {
	router := &mockRouter{agents: []string{"Abigail"}}
	session := &mockSession{responses: map[string]json.RawMessage{
		"game_get_time": defaultGameTimeResp(),
	}}
	s := newTestScheduler(router, session)

	// Record a recent global action so cooldown is active
	s.cooldowns.RecordAction("Someone", s.nowFn())

	s.Tick(context.Background())

	if len(router.calls) != 0 {
		t.Fatalf("expected no NPC triggered during global cooldown, got %v", router.calls)
	}
}

func TestTick_OutsideActiveHours(t *testing.T) {
	router := &mockRouter{agents: []string{"Abigail"}}
	session := &mockSession{responses: map[string]json.RawMessage{
		"game_get_time": json.RawMessage(`{"hour":23,"total_days":5,"weather":"sunny","player_location":"Farm"}`),
	}}
	s := newTestScheduler(router, session)

	s.Tick(context.Background())

	if len(router.calls) != 0 {
		t.Fatalf("expected no NPC triggered outside active hours, got %v", router.calls)
	}
}

func TestTick_PlayerBusy(t *testing.T) {
	router := &mockRouter{agents: []string{"Abigail"}}
	session := &mockSession{responses: map[string]json.RawMessage{
		"game_get_time":     defaultGameTimeResp(),
		"player_get_status": json.RawMessage(`{"busy":true,"in_menu":false,"in_event":false}`),
	}}
	s := newTestScheduler(router, session)

	s.Tick(context.Background())

	if len(router.calls) != 0 {
		t.Fatalf("expected no NPC triggered when player is busy, got %v", router.calls)
	}
}

func TestTick_AllOnCooldown(t *testing.T) {
	router := &mockRouter{agents: []string{"Abigail", "Sebastian"}}
	session := &mockSession{responses: map[string]json.RawMessage{
		"game_get_time":     defaultGameTimeResp(),
		"player_get_status": json.RawMessage(`{"busy":false,"in_menu":false,"in_event":false}`),
	}}
	s := newTestScheduler(router, session)

	// Put all NPCs on cooldown
	now := s.nowFn()
	s.cooldowns.RecordAction("Abigail", now.Add(-10*time.Minute))  // 10 min ago < 60 min
	s.cooldowns.RecordAction("Sebastian", now.Add(-5*time.Minute)) // 5 min ago < 60 min
	// Reset global so it doesn't block
	s.cooldowns.mu.Lock()
	s.cooldowns.lastGlobal = now.Add(-10 * time.Minute)
	s.cooldowns.mu.Unlock()

	s.Tick(context.Background())

	if len(router.calls) != 0 {
		t.Fatalf("expected no NPC triggered when all on cooldown, got %v", router.calls)
	}
}

func TestTick_NPCDeclines(t *testing.T) {
	router := &mockRouter{
		agents:  []string{"Abigail"},
		replies: map[string]string{"Abigail": "DECISION: NO\nREASON: I'm busy reading"},
	}
	session := &mockSession{responses: map[string]json.RawMessage{
		"game_get_time":     defaultGameTimeResp(),
		"player_get_status": json.RawMessage(`{"busy":false,"in_menu":false,"in_event":false}`),
	}}
	s := newTestScheduler(router, session)

	s.Tick(context.Background())

	// NPC was asked but declined — no summon
	if len(router.calls) != 1 {
		t.Fatalf("expected 1 TriggerProactive call, got %d", len(router.calls))
	}
	session.mu.Lock()
	for _, c := range session.calls {
		if c.Name == "npc_summon" {
			t.Fatal("expected no npc_summon when NPC declines")
		}
	}
	session.mu.Unlock()
}

func TestTick_NPCApproaches(t *testing.T) {
	router := &mockRouter{
		agents: []string{"Abigail"},
		replies: map[string]string{
			"Abigail": "DECISION: YES\nREASON: I want to ask about the farm\nOPENING: Hey! How's your farm doing today?",
		},
	}
	session := &mockSession{responses: map[string]json.RawMessage{
		"game_get_time":     defaultGameTimeResp(),
		"player_get_status": json.RawMessage(`{"busy":false,"in_menu":false,"in_event":false}`),
	}}
	s := newTestScheduler(router, session)

	s.Tick(context.Background())

	// Verify summon + chat_say were called
	session.mu.Lock()
	var hasSummon, hasChatSay bool
	for _, c := range session.calls {
		if c.Name == "npc_summon" {
			hasSummon = true
		}
		if c.Name == "chat_say" {
			hasChatSay = true
			if text, ok := c.Args["text"].(string); ok {
				if text != "Hey! How's your farm doing today?" {
					t.Errorf("unexpected opening line: %s", text)
				}
			}
		}
	}
	session.mu.Unlock()

	if !hasSummon {
		t.Error("expected npc_summon call")
	}
	if !hasChatSay {
		t.Error("expected chat_say call")
	}

	// Verify cooldown was recorded
	if s.cooldowns.IsEligible("Abigail", s.nowFn(), s.config) {
		t.Error("expected Abigail to be on cooldown after action")
	}
}

func TestCooldown_DailyReset(t *testing.T) {
	ct := NewCooldownTracker()
	now := time.Now()

	ct.RecordAction("Abigail", now)
	ct.RecordAction("Abigail", now)

	cfg := DefaultConfig()

	// At max daily limit
	ct.mu.Lock()
	ct.lastAction["abigail"] = now.Add(-2 * time.Hour) // per-NPC cooldown elapsed
	ct.mu.Unlock()

	if ct.IsEligible("Abigail", now, cfg) {
		t.Fatal("should be ineligible: daily limit reached")
	}

	// New day resets
	ct.ResetIfNewDay(99)
	// Also clear per-NPC cooldown for this test
	ct.mu.Lock()
	ct.lastAction = make(map[string]time.Time)
	ct.mu.Unlock()

	if !ct.IsEligible("Abigail", now, cfg) {
		t.Fatal("should be eligible after daily reset")
	}
}

func TestParseDecision_Yes(t *testing.T) {
	reply := "DECISION: YES\nREASON: I want to chat\nOPENING: Hello there!"
	d := parseDecision(reply)
	if !d.WantsToApproach {
		t.Fatal("expected WantsToApproach=true")
	}
	if d.Reason != "I want to chat" {
		t.Errorf("unexpected reason: %q", d.Reason)
	}
	if d.OpeningLine != "Hello there!" {
		t.Errorf("unexpected opening: %q", d.OpeningLine)
	}
}

func TestParseDecision_No(t *testing.T) {
	reply := "DECISION: NO\nREASON: I'm busy"
	d := parseDecision(reply)
	if d.WantsToApproach {
		t.Fatal("expected WantsToApproach=false")
	}
	if d.Reason != "I'm busy" {
		t.Errorf("unexpected reason: %q", d.Reason)
	}
}

func TestParseDecision_FallbackHeuristic(t *testing.T) {
	// No structured format, but contains "yes"
	reply := "Sure, yes I'd love to go talk to the player about fishing!"
	d := parseDecision(reply)
	if !d.WantsToApproach {
		t.Fatal("expected fallback heuristic to detect 'yes'")
	}
}
