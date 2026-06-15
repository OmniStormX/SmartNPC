package hermesrelay

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// countingGateway captures POST hits so a test can assert which gateway
// saw which event. Each /v1/responses request increments hits.
//
// Named to avoid colliding with the function fakeGateway() defined in
// relay_test.go inside the same _test package.
type countingGateway struct {
	*httptest.Server
	hits *atomic.Int32
}

func newCountingGateway(t *testing.T, hits *atomic.Int32) *countingGateway {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/responses", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &countingGateway{Server: srv, hits: hits}
}

func waitFor(t *testing.T, cond func() bool, timeout time.Duration, msg string) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %s", msg)
}

func TestGroup_RoutesByNPCFilter(t *testing.T) {
	var xiamiHits, abigailHits atomic.Int32
	xiami := newCountingGateway(t, &xiamiHits)
	abigail := newCountingGateway(t, &abigailHits)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	g, err := NewGroup([]Config{
		{URL: xiami.URL, Conversation: "xiami", Model: "x", NPCName: "XiaMi"},
		{URL: abigail.URL, Conversation: "abigail", Model: "a", NPCName: "Abigail"},
	}, logger)
	if err != nil {
		t.Fatalf("NewGroup: %v", err)
	}

	// chat_message addressed to Abigail → only abigail gateway hit.
	chatToAbigail := json.RawMessage(`{"npc":"Abigail","text":"hi","source":"player"}`)
	g.HandleEvent(context.Background(), "chat_message", chatToAbigail)

	waitFor(t, func() bool { return abigailHits.Load() == 1 }, 2*time.Second, "abigail receive")
	if xiamiHits.Load() != 0 {
		t.Errorf("xiami should not have been hit, got %d", xiamiHits.Load())
	}
}

func TestGroup_DropsUnknownNPC(t *testing.T) {
	var hits atomic.Int32
	gw := newCountingGateway(t, &hits)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	g, err := NewGroup([]Config{
		{URL: gw.URL, Conversation: "x", Model: "x", NPCName: "XiaMi"},
	}, logger)
	if err != nil {
		t.Fatalf("NewGroup: %v", err)
	}

	g.HandleEvent(context.Background(), "chat_message",
		json.RawMessage(`{"npc":"Penny","text":"hi","source":"player"}`))

	// Give the goroutine a fair window to NOT fire.
	time.Sleep(100 * time.Millisecond)
	if hits.Load() != 0 {
		t.Errorf("unknown NPC should be dropped, got %d hits", hits.Load())
	}
}

func TestGroup_BroadcastEventReachesAll(t *testing.T) {
	var xiamiHits, abigailHits atomic.Int32
	xiami := newCountingGateway(t, &xiamiHits)
	abigail := newCountingGateway(t, &abigailHits)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	g, err := NewGroup([]Config{
		{URL: xiami.URL, Conversation: "xiami", Model: "x", NPCName: "XiaMi"},
		{URL: abigail.URL, Conversation: "abigail", Model: "a", NPCName: "Abigail"},
	}, logger)
	if err != nil {
		t.Fatalf("NewGroup: %v", err)
	}

	// day_started has no recipient → all relays receive it.
	g.HandleEvent(context.Background(), "day_started", json.RawMessage(`{"day":1}`))

	waitFor(t, func() bool { return xiamiHits.Load() == 1 && abigailHits.Load() == 1 },
		2*time.Second, "broadcast to both")
}

func TestNewGroup_EmptyConfigs(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	g, err := NewGroup(nil, logger)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if g == nil {
		t.Fatal("want non-nil group for empty configs")
	}
	if len(g.Relays()) != 0 {
		t.Errorf("want 0 relays, got %d", len(g.Relays()))
	}
}
