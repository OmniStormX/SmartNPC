package memory

import (
	"testing"
	"time"
)

// fakeClock is a deterministic time source for tracker tests. It is unsafe
// for concurrent use; tests use it from the goroutine that drives the
// tracker.
type fakeClock struct {
	t time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time { return c.t }
func (c *fakeClock) Advance(d time.Duration) {
	c.t = c.t.Add(d)
}

func newTrackerForTest(t *testing.T, store Store, idle time.Duration) (*ConversationTracker, *fakeClock) {
	t.Helper()
	clk := newFakeClock()
	tr, err := NewTracker(store, TrackerOptions{
		IdleTimeout: idle,
		Now:         clk.Now,
	})
	if err != nil {
		t.Fatalf("NewTracker: %v", err)
	}
	t.Cleanup(func() { _ = tr.Close() })
	return tr, clk
}

func TestTracker_NewRequiresStore(t *testing.T) {
	if _, err := NewTracker(nil, TrackerOptions{}); err == nil {
		t.Fatal("expected error when store is nil")
	}
}

func TestTracker_StartTurnReusesUntilIdle(t *testing.T) {
	store := openTestStore(t)
	tr, clk := newTrackerForTest(t, store, time.Minute)

	id1, err := tr.StartTurn("Xiami", "Spring 1")
	if err != nil {
		t.Fatalf("StartTurn 1: %v", err)
	}
	id2, err := tr.StartTurn("Xiami", "Spring 1")
	if err != nil {
		t.Fatalf("StartTurn 2: %v", err)
	}
	if id1 != id2 {
		t.Fatalf("expected reuse, got %d -> %d", id1, id2)
	}

	// Advance past the idle threshold and the next StartTurn must roll
	// over to a fresh conversation.
	clk.Advance(2 * time.Minute)
	id3, err := tr.StartTurn("Xiami", "Spring 1")
	if err != nil {
		t.Fatalf("StartTurn 3: %v", err)
	}
	if id3 == id1 {
		t.Fatalf("expected fresh conversation after idle expiry, still got %d", id3)
	}
}

func TestTracker_StartTurnGameDateRollover(t *testing.T) {
	store := openTestStore(t)
	tr, _ := newTrackerForTest(t, store, time.Hour)

	id1, _ := tr.StartTurn("Xiami", "Spring 1")
	id2, err := tr.StartTurn("Xiami", "Spring 2")
	if err != nil {
		t.Fatalf("StartTurn 2: %v", err)
	}
	if id1 == id2 {
		t.Fatalf("game-date change must open a new conversation")
	}
}

func TestTracker_TouchExtendsIdleWindow(t *testing.T) {
	store := openTestStore(t)
	tr, clk := newTrackerForTest(t, store, time.Minute)

	id1, _ := tr.StartTurn("Xiami", "")
	clk.Advance(45 * time.Second)
	if !tr.Touch("Xiami") {
		t.Fatal("Touch on open conversation should return true")
	}
	clk.Advance(45 * time.Second) // total idle from last touch: 45s
	id2, _ := tr.StartTurn("Xiami", "")
	if id1 != id2 {
		t.Fatalf("Touch should have prevented rollover, got %d != %d", id1, id2)
	}

	// And after enough time has passed, rollover still triggers.
	clk.Advance(2 * time.Minute)
	id3, _ := tr.StartTurn("Xiami", "")
	if id3 == id1 {
		t.Fatalf("expected rollover after second idle window")
	}
}

func TestTracker_TouchUnknownReturnsFalse(t *testing.T) {
	store := openTestStore(t)
	tr, _ := newTrackerForTest(t, store, time.Minute)
	if tr.Touch("Ghost") {
		t.Error("Touch on unknown NPC should return false")
	}
}

func TestTracker_EndIsIdempotent(t *testing.T) {
	store := openTestStore(t)
	tr, _ := newTrackerForTest(t, store, time.Minute)

	if _, err := tr.StartTurn("Xiami", ""); err != nil {
		t.Fatal(err)
	}
	if err := tr.End("Xiami"); err != nil {
		t.Fatalf("End: %v", err)
	}
	if err := tr.End("Xiami"); err != nil {
		t.Fatalf("End (second call) should no-op, got %v", err)
	}
	if id := tr.CurrentConversationID("Xiami"); id != 0 {
		t.Errorf("expected no current conversation, got %d", id)
	}
}

func TestTracker_SweepIdleClosesExpired(t *testing.T) {
	store := openTestStore(t)
	tr, clk := newTrackerForTest(t, store, time.Minute)

	if _, err := tr.StartTurn("Alpha", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := tr.StartTurn("Beta", ""); err != nil {
		t.Fatal(err)
	}
	clk.Advance(2 * time.Minute)

	closed := tr.SweepIdle()
	if closed != 2 {
		t.Fatalf("want 2 sweeps, got %d", closed)
	}
	if id := tr.CurrentConversationID("Alpha"); id != 0 {
		t.Errorf("Alpha should be closed, got %d", id)
	}
}

func TestTracker_CloseFlushesEverything(t *testing.T) {
	store := openTestStore(t)
	tr, _ := newTrackerForTest(t, store, time.Hour)

	if _, err := tr.StartTurn("Alpha", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := tr.StartTurn("Beta", ""); err != nil {
		t.Fatal(err)
	}
	if err := tr.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// Subsequent StartTurn must error out.
	if _, err := tr.StartTurn("Alpha", ""); err == nil {
		t.Error("StartTurn after Close should fail")
	}
}

func TestTracker_NegativeIdleDisablesExpiry(t *testing.T) {
	store := openTestStore(t)
	tr, clk := newTrackerForTest(t, store, -1)

	id1, _ := tr.StartTurn("Xiami", "")
	clk.Advance(24 * time.Hour)
	id2, _ := tr.StartTurn("Xiami", "")
	if id1 != id2 {
		t.Fatalf("negative idle should disable expiry; got %d -> %d", id1, id2)
	}
	if n := tr.SweepIdle(); n != 0 {
		t.Errorf("SweepIdle should be no-op with negative idle, swept %d", n)
	}
}

func TestTracker_CaseInsensitiveNPCKey(t *testing.T) {
	store := openTestStore(t)
	tr, _ := newTrackerForTest(t, store, time.Minute)

	id1, _ := tr.StartTurn("XiaMi", "")
	id2, _ := tr.StartTurn("xiami", "")
	if id1 != id2 {
		t.Fatalf("case-insensitive lookup expected, got %d != %d", id1, id2)
	}
}

// ── OnMessage path ────────────────────────────────────────────────────

func TestTracker_OnMessage_HappyPath(t *testing.T) {
	store := openTestStore(t)
	tr, _ := newTrackerForTest(t, store, time.Hour)

	convID, err := tr.OnMessage("Xiami", "user", "hi", "Spring 1")
	if err != nil {
		t.Fatalf("OnMessage user: %v", err)
	}
	if convID <= 0 {
		t.Fatal("expected positive conv id")
	}
	if _, err := tr.OnMessage("Xiami", "assistant", "hello!", "Spring 1"); err != nil {
		t.Fatalf("OnMessage assistant: %v", err)
	}
	if _, err := tr.OnMessage("Xiami", "tool", "{}", ""); err != nil {
		t.Fatalf("OnMessage tool: %v", err)
	}
	// Conversation must still be open after the three turns.
	if id := tr.CurrentConversationID("Xiami"); id != convID {
		t.Errorf("conversation should still be open: got %d want %d", id, convID)
	}

	// Verify the messages actually landed in the store. recentMessages
	// is a package-private helper but we can call it from the same
	// package's tests.
	got, err := tr.recentMessages(convID, 10)
	if err != nil {
		t.Fatalf("recentMessages: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("want 3 messages, got %d", len(got))
	}
}

func TestTracker_OnMessage_Validation(t *testing.T) {
	store := openTestStore(t)
	tr, _ := newTrackerForTest(t, store, time.Hour)

	if _, err := tr.OnMessage("", "user", "x", ""); err == nil {
		t.Error("empty npc should error")
	}
	if _, err := tr.OnMessage("Xiami", "", "x", ""); err == nil {
		t.Error("empty role should error")
	}
}

func TestTracker_OnMessage_GameDateRolloverPreservesCounter(t *testing.T) {
	store := openTestStore(t)
	tr, _ := newTrackerForTest(t, store, time.Hour)

	id1, _ := tr.OnMessage("Xiami", "user", "day1", "Spring 1")
	id2, _ := tr.OnMessage("Xiami", "user", "day2", "Spring 2")
	if id1 == id2 {
		t.Fatalf("OnMessage should roll over conversations on game-date change")
	}
}
