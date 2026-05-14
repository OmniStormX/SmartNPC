package hermesrelay

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/smartnpc/smartnpc-mcp/internal/bridge"
)

// captured records what the test gateway received for one POST.
type captured struct {
	path        string
	auth        string
	contentType string
	body        request
}

// fakeGateway returns an httptest server that captures one POST and
// signals via the wait group when it lands. handler is the optional
// response-side behavior; if nil it returns 200 with `{"ok":true}`.
func fakeGateway(t *testing.T, wg *sync.WaitGroup, capPtr *captured, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	if handler == nil {
		handler = func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true}`))
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/responses", func(w http.ResponseWriter, r *http.Request) {
		defer wg.Done()
		var got request
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		*capPtr = captured{
			path:        r.URL.Path,
			auth:        r.Header.Get("Authorization"),
			contentType: r.Header.Get("Content-Type"),
			body:        got,
		}
		handler(w, r)
	})
	return httptest.NewServer(mux)
}

func TestRelay_PostsExpectedBody(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(1)
	var cap captured
	ts := fakeGateway(t, &wg, &cap, nil)
	defer ts.Close()

	r, err := New(Config{
		URL:          ts.URL,
		APIKey:       "test-key",
		Conversation: "xiami",
		Model:        "xiami",
		Timeout:      2 * time.Second,
	}, slog.Default())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	payload, _ := json.Marshal(map[string]any{
		"npc": "XiaMi", "text": "你好", "source": "player",
	})
	r.HandleEvent(context.Background(), bridge.EventChatMessage, payload)

	if !waitGroupTimeout(&wg, 3*time.Second) {
		t.Fatal("gateway never received POST")
	}

	if cap.path != "/v1/responses" {
		t.Errorf("path = %q want /v1/responses", cap.path)
	}
	if cap.auth != "Bearer test-key" {
		t.Errorf("auth = %q want Bearer test-key", cap.auth)
	}
	if cap.contentType != "application/json" {
		t.Errorf("content-type = %q want application/json", cap.contentType)
	}
	if cap.body.Model != "xiami" {
		t.Errorf("Model = %q want xiami", cap.body.Model)
	}
	if cap.body.Conversation != "xiami" {
		t.Errorf("Conversation = %q want xiami", cap.body.Conversation)
	}
	// Default config now uses mcp-managed history with Store=false. The
	// legacy "Hermes-side store always on" assertion is replaced by an
	// explicit Store=false check.
	if cap.body.Store {
		t.Errorf("Store = true; want false (mcp window now owns history)")
	}
	if !strings.Contains(cap.body.Input, "你好") {
		t.Errorf("Input missing player text: %q", cap.body.Input)
	}
}

func TestRelay_LoadsPersonaFile(t *testing.T) {
	dir := t.TempDir()
	persona := filepath.Join(dir, "SOUL.md")
	const body = "# Persona body\n\nYou are XiaMi."
	if err := os.WriteFile(persona, []byte(body), 0o644); err != nil {
		t.Fatalf("write persona: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	var cap captured
	ts := fakeGateway(t, &wg, &cap, nil)
	defer ts.Close()

	r, err := New(Config{
		URL:          ts.URL,
		Conversation: "xiami",
		Model:        "xiami",
		PersonaFile:  persona,
		Timeout:      2 * time.Second,
	}, slog.Default())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	r.HandleEvent(context.Background(), bridge.EventNpcInteract,
		json.RawMessage(`{"npc":"XiaMi","source":"player"}`))

	if !waitGroupTimeout(&wg, 3*time.Second) {
		t.Fatal("gateway never received POST")
	}
	if cap.body.Instructions != body {
		t.Errorf("Instructions = %q want %q", cap.body.Instructions, body)
	}
}

func TestRelay_MissingPersonaFileFailsNew(t *testing.T) {
	_, err := New(Config{
		URL:         "http://localhost:1",
		PersonaFile: "/nonexistent/path/SOUL.md",
	}, slog.Default())
	if err == nil {
		t.Fatal("expected error for missing persona file")
	}
}

func TestRelay_MissingURLFailsNew(t *testing.T) {
	_, err := New(Config{}, slog.Default())
	if err == nil {
		t.Fatal("expected error for empty URL")
	}
}

func TestRelay_RoutingFilter(t *testing.T) {
	tests := []struct {
		name    string
		npc     string
		event   string
		payload string
		want    bool
	}{
		{"no_filter_passes_all", "", bridge.EventChatMessage, `{"npc":"Abigail","text":"hi"}`, true},
		{"matching_npc_field", "XiaMi", bridge.EventChatMessage, `{"npc":"XiaMi","text":"hi"}`, true},
		{"matching_to_field", "XiaMi", bridge.EventNpcMessage, `{"from":"Abigail","to":"XiaMi","text":"hi"}`, true},
		{"matching_target_field", "XiaMi", bridge.EventChatMessage, `{"target":"XiaMi","text":"hi"}`, true},
		{"non_matching_npc", "XiaMi", bridge.EventChatMessage, `{"npc":"Abigail","text":"hi"}`, false},
		{"no_npc_field_passes", "XiaMi", bridge.EventDayStarted, `{"day":5,"season":"spring"}`, true},
		{"empty_payload_passes", "XiaMi", "x", `{}`, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := &Relay{cfg: Config{NPCName: tc.npc}, logger: slog.Default()}
			got := r.ShouldRoute(tc.event, json.RawMessage(tc.payload))
			if got != tc.want {
				t.Errorf("ShouldRoute = %v want %v", got, tc.want)
			}
		})
	}
}

func TestRelay_NonMatchingEventDoesNotPost(t *testing.T) {
	var calls atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(200)
	}))
	defer ts.Close()

	r, err := New(Config{
		URL:          ts.URL,
		Conversation: "xiami",
		Model:        "xiami",
		NPCName:      "XiaMi",
		Timeout:      500 * time.Millisecond,
	}, slog.Default())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	r.HandleEvent(context.Background(), bridge.EventChatMessage,
		json.RawMessage(`{"npc":"Abigail","text":"hi"}`))

	// Give any rogue goroutine a moment to POST. Capped at 100ms per
	// CLAUDE.md test-discipline rule.
	time.Sleep(100 * time.Millisecond)
	if got := calls.Load(); got != 0 {
		t.Errorf("expected 0 POSTs (event filtered out), got %d", got)
	}
}

func TestRelay_NonOK_StatusLogged(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(1)
	var cap captured
	ts := fakeGateway(t, &wg, &cap, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"boom"}`))
	})
	defer ts.Close()

	r, err := New(Config{URL: ts.URL, Conversation: "xiami", Model: "xiami", Timeout: 2 * time.Second}, slog.Default())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	r.HandleEvent(context.Background(), bridge.EventChatMessage,
		json.RawMessage(`{"npc":"XiaMi","text":"hi"}`))

	if !waitGroupTimeout(&wg, 3*time.Second) {
		t.Fatal("gateway never received POST")
	}
	// We just want to know the relay didn't panic on non-2xx; nothing
	// is asserted on the slog output.
}

// TestRelay_ParsesUsageFromResponse covers the cache-hit telemetry path:
// the response body's usage block is consumed and does not crash the
// post handler when fields are partial or missing. The body must drain
// even on malformed JSON so keep-alive can reuse the connection.
func TestRelay_ParsesUsageFromResponse(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{
			name: "full_usage_with_cache",
			body: `{"usage":{"input_tokens":1200,"input_tokens_details":{"cached_tokens":900},"output_tokens":40,"total_tokens":1240}}`,
		},
		{
			name: "no_usage_field",
			body: `{"id":"resp_123","ok":true}`,
		},
		{
			name: "empty_body",
			body: ``,
		},
		{
			name: "malformed_json",
			body: `{"usage":{"input_tokens":`,
		},
		{
			name: "zero_input_tokens",
			body: `{"usage":{"input_tokens":0,"output_tokens":0}}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var wg sync.WaitGroup
			wg.Add(1)
			var cap captured
			ts := fakeGateway(t, &wg, &cap, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.body))
			})
			defer ts.Close()

			r, err := New(Config{
				URL:          ts.URL,
				Conversation: "xiami",
				Model:        "xiami",
				Timeout:      2 * time.Second,
			}, slog.Default())
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			r.HandleEvent(context.Background(), bridge.EventChatMessage,
				json.RawMessage(`{"npc":"XiaMi","text":"hi"}`))

			if !waitGroupTimeout(&wg, 3*time.Second) {
				t.Fatal("gateway never received POST")
			}
		})
	}
}

// waitGroupTimeout blocks until wg is done or the timeout elapses,
// returning true if done, false on timeout.
func waitGroupTimeout(wg *sync.WaitGroup, d time.Duration) bool {
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(d):
		return false
	}
}

// TestRelay_HistoryWindowPrependsAndCaps drives two consecutive posts
// against a fake gateway that returns a real assistant message; the
// second post's input must contain the previous turn (player + npc),
// proving mcp-managed history works. After many turns, history must
// stay capped at MaxHistoryTurns.
func TestRelay_HistoryWindowPrependsAndCaps(t *testing.T) {
	mu := sync.Mutex{}
	var receivedInputs []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var got request
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		mu.Lock()
		receivedInputs = append(receivedInputs, got.Input)
		mu.Unlock()
		// Hermes-shaped response with one assistant message.
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"npc reply"}]}],"usage":{"input_tokens":10,"output_tokens":2}}`))
	}))
	defer ts.Close()

	r, err := New(Config{
		URL:             ts.URL,
		Conversation:    "xiami",
		Model:           "xiami",
		Timeout:         2 * time.Second,
		MaxHistoryTurns: 4,
	}, slog.Default())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Drive 5 turns. After 5 turns of (player, npc) pairs we have 10
	// entries; cap is 4 — the prepended history on turn 6 must contain
	// only the most recent 4.
	for i := 0; i < 6; i++ {
		payload := []byte(fmt.Sprintf(`{"npc":"XiaMi","text":"player turn %d"}`, i))
		r.HandleEvent(context.Background(), bridge.EventChatMessage, payload)
		// HandleEvent fires post() in a goroutine; give it time. Tests
		// elsewhere use a WaitGroup; here we just sleep < 100ms which
		// is well under the test-discipline cap and matches what the
		// fake server's instant-reply latency permits.
		time.Sleep(20 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(receivedInputs) != 6 {
		t.Fatalf("got %d POSTs, want 6", len(receivedInputs))
	}
	// First POST: empty history → no "Recent conversation" preamble.
	if strings.Contains(receivedInputs[0], "Recent conversation") {
		t.Errorf("turn 0 should have no history preamble, got: %q", receivedInputs[0])
	}
	// Last POST: history present and capped at 4 entries — count "[player]" + "[npc]" lines.
	last := receivedInputs[5]
	if !strings.Contains(last, "Recent conversation") {
		t.Errorf("turn 5 missing history preamble: %q", last)
	}
	playerLines := strings.Count(last, "[player]")
	npcLines := strings.Count(last, "[npc]")
	// Note: the "[player]" inside the current event's "Farmer says..." is
	// avoided because the format string uses "Farmer says to you:" not
	// our [player] tag. So the count below reflects only history.
	total := playerLines + npcLines
	if total != 4 {
		t.Errorf("history not capped at 4; got %d entries (player=%d, npc=%d). full input:\n%s",
			total, playerLines, npcLines, last)
	}
}

// TestRelay_HistorySkipsUpstreamErrorReplies makes sure rate-limit /
// "(empty)" / "API call failed..." style replies don't end up polluting
// the history window for the next turn.
func TestRelay_HistorySkipsUpstreamErrorReplies(t *testing.T) {
	respBody := `{"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"API call failed after 3 retries: HTTP 429"}]}],"usage":{"input_tokens":10}}`
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(respBody))
	}))
	defer ts.Close()

	r, err := New(Config{
		URL:             ts.URL,
		Conversation:    "xiami",
		Model:           "xiami",
		Timeout:         2 * time.Second,
		MaxHistoryTurns: 6,
	}, slog.Default())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	r.HandleEvent(context.Background(), bridge.EventChatMessage,
		json.RawMessage(`{"npc":"XiaMi","text":"hi"}`))
	time.Sleep(50 * time.Millisecond)

	r.histMu.Lock()
	defer r.histMu.Unlock()
	for _, h := range r.history {
		if h.role == "npc" {
			t.Errorf("upstream error reply leaked into history: %q", h.text)
		}
	}
}

func TestExtractAssistantReply(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "plain_assistant_message",
			body: `{"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hello"}]}]}`,
			want: "hello",
		},
		{
			name: "tool_call_then_message",
			body: `{"output":[{"type":"function_call","name":"chat_say","arguments":"{}"},{"type":"message","role":"assistant","content":[{"type":"output_text","text":"final"}]}]}`,
			want: "final",
		},
		{
			name: "tool_call_only_no_message",
			body: `{"output":[{"type":"function_call","name":"chat_say","arguments":"{}"}]}`,
			want: "",
		},
		{
			name: "empty_output",
			body: `{"output":[]}`,
			want: "",
		},
		{
			name: "malformed",
			body: `{not-json`,
			want: "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := extractAssistantReply([]byte(tc.body))
			if got != tc.want {
				t.Errorf("extractAssistantReply = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestLooksLikeUpstreamError(t *testing.T) {
	cases := map[string]bool{
		"":                                  true,
		"(empty)":                           true,
		"API call failed after 3 retries":   true,
		"api call failed":                   true,
		"HTTP 429: rate limited":            true,
		"got HTTP 503 from upstream":        true,
		"normal reply":                      false,
		"我正在去你那儿":                          false,
	}
	for in, want := range cases {
		if got := looksLikeUpstreamError(in); got != want {
			t.Errorf("looksLikeUpstreamError(%q) = %v want %v", in, got, want)
		}
	}
}

// TestRelay_DebugPayloadDoesNotBreakPost confirms turning on the debug
// payload flag still produces a normal POST + parse cycle. We don't
// assert on slog output (slog's default handler is the test's concern);
// we just verify the request still lands and the relay survives both
// the outbound dump and the response dump branches.
func TestRelay_DebugPayloadDoesNotBreakPost(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(1)
	var cap captured
	ts := fakeGateway(t, &wg, &cap, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"usage":{"input_tokens":42,"output_tokens":7}}`))
	})
	defer ts.Close()

	r, err := New(Config{
		URL:          ts.URL,
		Conversation: "xiami",
		Model:        "xiami",
		Timeout:      2 * time.Second,
		DebugPayload: true,
	}, slog.Default())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	r.HandleEvent(context.Background(), bridge.EventChatMessage,
		json.RawMessage(`{"npc":"XiaMi","text":"hi"}`))

	if !waitGroupTimeout(&wg, 3*time.Second) {
		t.Fatal("gateway never received POST with DebugPayload=true")
	}
	if cap.body.Conversation != "xiami" {
		t.Errorf("Conversation = %q want xiami", cap.body.Conversation)
	}
}

func TestDebugPayloadEnabled(t *testing.T) {
	tests := []struct {
		val  string
		want bool
	}{
		{"", false},
		{"0", false},
		{"false", false},
		{"no", false},
		{"random", false},
		{"1", true},
		{"true", true},
		{"TRUE", true},
		{"Yes", true},
		{"on", true},
		{"  on  ", true},
	}
	for _, tc := range tests {
		t.Run(tc.val, func(t *testing.T) {
			t.Setenv("SMARTNPC_RELAY_DEBUG_PAYLOAD", tc.val)
			if got := DebugPayloadEnabled(); got != tc.want {
				t.Errorf("DebugPayloadEnabled(%q) = %v want %v", tc.val, got, tc.want)
			}
		})
	}
}
