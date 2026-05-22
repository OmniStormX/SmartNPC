package agentbridge

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/OmniStormX/SmartNPC/pkg/eventbus"
)

// recordingBackend stores every Event it sees. Goroutine-safe for the
// test's sequential assertions.
type recordingBackend struct {
	name string
	mu   sync.Mutex
	got  []eventbus.Event
	err  error // when non-nil, returned from Forward
}

func (r *recordingBackend) Name() string { return r.name }

func (r *recordingBackend) Forward(_ context.Context, ev eventbus.Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.got = append(r.got, ev)
	return r.err
}

func (r *recordingBackend) snapshot() []eventbus.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]eventbus.Event, len(r.got))
	copy(out, r.got)
	return out
}

// scriptedSource emits a fixed slice of events on Start, then waits for ctx.
type scriptedSource struct {
	name   string
	events []eventbus.Event
}

func (s *scriptedSource) Name() string { return s.name }

func (s *scriptedSource) Start(ctx context.Context, sink Sink) error {
	for _, ev := range s.events {
		sink(ctx, ev)
	}
	<-ctx.Done()
	return ctx.Err()
}

func newTestServer(t *testing.T) *Server {
	t.Helper()
	m := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.0.0"}, nil)
	return New(m, Options{})
}

func TestServer_DispatchFansOutToAllBackends(t *testing.T) {
	srv := newTestServer(t)

	b1 := &recordingBackend{name: "b1"}
	b2 := &recordingBackend{name: "b2"}
	srv.AttachBackend(b1)
	srv.AttachBackend(b2)

	ev1, _ := eventbus.New("chat.message", "sdv", "XiaMi", map[string]string{"text": "hi"})
	ev2, _ := eventbus.New("actor.interact", "sdv", "Abigail", nil)
	srv.AttachEventSource(&scriptedSource{name: "s", events: []eventbus.Event{ev1, ev2}})

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	go func() {
		// Let the source emit, then cancel so Run returns.
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	_ = srv.Run(ctx)

	for _, b := range []*recordingBackend{b1, b2} {
		got := b.snapshot()
		if len(got) != 2 {
			t.Fatalf("backend %s: got %d events, want 2", b.name, len(got))
		}
		if got[0].Kind != "chat.message" || got[1].Kind != "actor.interact" {
			t.Errorf("backend %s: kinds = %v / %v", b.name, got[0].Kind, got[1].Kind)
		}
	}
}

func TestServer_BackendErrorDoesNotStopFanOut(t *testing.T) {
	srv := newTestServer(t)

	bad := &recordingBackend{name: "bad", err: errors.New("boom")}
	good := &recordingBackend{name: "good"}
	srv.AttachBackend(bad)
	srv.AttachBackend(good)

	ev, _ := eventbus.New("world.tick", "sdv", "", nil)
	srv.AttachEventSource(&scriptedSource{name: "s", events: []eventbus.Event{ev}})

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	_ = srv.Run(ctx)

	if len(good.snapshot()) != 1 {
		t.Errorf("good backend missed event despite preceding bad backend; got %d", len(good.snapshot()))
	}
}

func TestServer_NoSources_BlocksUntilCancel(t *testing.T) {
	srv := newTestServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := srv.Run(ctx)
	elapsed := time.Since(start)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Run err = %v, want DeadlineExceeded", err)
	}
	if elapsed < 50*time.Millisecond {
		t.Errorf("Run returned too quickly (%v); should block until ctx done", elapsed)
	}
}

// dummyToolGroup registers a placeholder tool to verify Mount wires
// fn(*mcp.Server) correctly.
func TestServer_Mount_RegistersToolGroup(t *testing.T) {
	srv := newTestServer(t)
	called := false
	err := srv.Mount("dummy", func(_ *mcp.Server) error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatalf("Mount: %v", err)
	}
	if !called {
		t.Error("Mount did not invoke the ToolGroup")
	}
}

func TestServer_Mount_PropagatesError(t *testing.T) {
	srv := newTestServer(t)
	want := errors.New("registration failed")
	err := srv.Mount("bad", func(_ *mcp.Server) error { return want })
	if err == nil || !errors.Is(err, want) {
		t.Errorf("Mount err = %v, want chain to %v", err, want)
	}
}
