package tools

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/smartnpc/smartnpc-mcp/internal/hermesrelay"
)

// TestNpcSendMessage_RoutesToHermesGroup is the integration test pinning the
// delegate-fix bug: A calls npc_send_message(to=B), so B's Hermes profile
// must receive a /v1/responses POST and only B's — A's gateway must not be
// hit by its own outbound message.
//
// Wiring:
//
//	in-process MCP server
//	    │
//	    └── tools.RegisterAll(server, br=nil, hermes=group.HandleEvent, ...)
//	             │
//	             └── hermesrelay.Group with two relays:
//	                   - NPCName=XiaMi   → fakeXiamiGW
//	                   - NPCName=Abigail → fakeAbigailGW
//
// Then a client calls npc_send_message(from=XiaMi, to=Abigail) and we assert
// (1) abigail gateway sees one POST,
// (2) xiami gateway sees zero POSTs (recipient filter respected),
// (3) the POST body carries the correct conversation/model and the input
//     contains the original message text (so Abigail's Hermes profile knows
//     why it's being woken up).
func TestNpcSendMessage_RoutesToHermesGroup(t *testing.T) {
	var (
		xiamiHits   atomic.Int32
		abigailHits atomic.Int32
		mu          sync.Mutex
		abigailBody hermesPostBody
	)

	xiamiGW := newCapturingGateway(t, &xiamiHits, nil, nil)
	abigailGW := newCapturingGateway(t, &abigailHits, &mu, &abigailBody)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	group, err := hermesrelay.NewGroup([]hermesrelay.Config{
		{URL: xiamiGW.URL, Conversation: "xiami", Model: "xiami-model", NPCName: "XiaMi"},
		{URL: abigailGW.URL, Conversation: "abigail", Model: "abigail-model", NPCName: "Abigail"},
	}, logger)
	if err != nil {
		t.Fatalf("NewGroup: %v", err)
	}

	ctx := context.Background()
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "t"}, nil)
	RegisterAll(server, nil, group.HandleEvent, nil, logger)

	t1, t2 := mcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, t1, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "t"}, nil)
	cs, err := client.Connect(ctx, t2, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer cs.Close()

	if _, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "npc_send_message",
		Arguments: map[string]any{
			"from": "XiaMi",
			"to":   "Abigail",
			"text": "农场出事了",
			"kind": "alert",
		},
	}); err != nil {
		t.Fatalf("send: %v", err)
	}

	// hermesrelay.Relay.post runs in a goroutine; allow up to 5s — happy
	// path is sub-second locally, the wider window absorbs CI noise.
	if !waitFor(5*time.Second, func() bool {
		return abigailHits.Load() == 1
	}) {
		t.Fatalf("abigail gateway never received POST (hits=%d)", abigailHits.Load())
	}

	// Filter must hold: XiaMi's own gateway must not receive its own
	// outbound message. Brief grace window for any rogue goroutine.
	time.Sleep(100 * time.Millisecond)
	if got := xiamiHits.Load(); got != 0 {
		t.Errorf("xiami gateway should not be hit, got %d", got)
	}

	mu.Lock()
	defer mu.Unlock()
	if abigailBody.Conversation != "abigail" {
		t.Errorf("abigail conversation = %q want %q", abigailBody.Conversation, "abigail")
	}
	if abigailBody.Model != "abigail-model" {
		t.Errorf("abigail model = %q want %q", abigailBody.Model, "abigail-model")
	}
	if !strings.Contains(abigailBody.Input, "农场出事了") {
		t.Errorf("abigail input missing message text: %q", abigailBody.Input)
	}
	// Body should also surface the sender so Abigail's profile can address
	// XiaMi back. The exact format is FormatForHermes's contract; we only
	// assert presence of the sender name.
	if !strings.Contains(abigailBody.Input, "XiaMi") {
		t.Errorf("abigail input missing sender name: %q", abigailBody.Input)
	}
}

// hermesPostBody mirrors hermesrelay's private request struct just enough
// to pull out fields we want to assert on. We can't import the private type
// across packages so we redeclare it here.
type hermesPostBody struct {
	Model        string `json:"model"`
	Input        string `json:"input"`
	Conversation string `json:"conversation"`
	Instructions string `json:"instructions"`
	Store        bool   `json:"store"`
}

// newCapturingGateway returns an httptest server that increments hits on
// each POST to /v1/responses. When body+mu are non-nil, the most recent
// body is decoded into *body under the mutex.
//
// Modeled on hermesrelay.countingGateway / fakeGateway but redeclared here
// because those helpers are unexported in the hermesrelay package.
func newCapturingGateway(t *testing.T, hits *atomic.Int32, mu *sync.Mutex, body *hermesPostBody) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/responses", func(w http.ResponseWriter, r *http.Request) {
		if mu != nil && body != nil {
			raw, _ := io.ReadAll(r.Body)
			mu.Lock()
			_ = json.Unmarshal(raw, body)
			mu.Unlock()
		} else {
			_, _ = io.Copy(io.Discard, r.Body)
		}
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// waitFor polls cond every 5ms until either it returns true (then waitFor
// returns true) or the deadline elapses (returns false). 5ms cap is
// CLAUDE.md test-discipline compliant (no sleep > 100ms).
func waitFor(timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return cond()
}
