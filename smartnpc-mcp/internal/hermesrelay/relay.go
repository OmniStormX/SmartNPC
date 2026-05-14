// Package hermesrelay forwards game events to a Hermes Gateway via
// POST /v1/responses. See docs/hermes-event-trigger.md for the design.
package hermesrelay

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/smartnpc/smartnpc-mcp/internal/events"
)

// Config controls a single Relay. One relay = one Hermes profile / one NPC.
type Config struct {
	// URL is the Hermes Gateway base URL (e.g. "http://127.0.0.1:8642").
	// The relay appends "/v1/responses".
	URL string

	// APIKey is sent as `Authorization: Bearer <APIKey>` when non-empty.
	APIKey string

	// Conversation is the Hermes conversation id (per-NPC dialog memory).
	Conversation string

	// Model is the value of API_SERVER_MODEL_NAME in the target profile.
	Model string

	// NPCName, when non-empty, filters which events get forwarded. Events
	// that carry a recipient NPC field must match; events with no
	// recipient (e.g. day_started) always pass.
	NPCName string

	// PersonaFile is an optional path to a markdown file whose contents
	// are sent as the request's `instructions` field on every turn.
	PersonaFile string

	// Timeout for the POST. Defaults to 120s if zero — long enough to cover
	// a cold-cache GPT-class response with full persona attached.
	Timeout time.Duration

	// DebugPayload, when true, makes post() emit two Debug records per turn —
	// the full outbound JSON body and the full response body. Routed to
	// PayloadLogger when that field is non-nil so the main slog stream stays
	// clean of multi-KB persona dumps.
	DebugPayload bool

	// PayloadLogger, when non-nil, receives the outbound/inbound Debug
	// records that DebugPayload turns on. Typically a slog handler writing
	// to a dedicated file. When nil but DebugPayload is true, the records
	// fall back to the Relay's main logger.
	PayloadLogger *slog.Logger

	// MaxHistoryTurns is the cap on the per-relay short-window history
	// (player + npc combined) the relay maintains and prepends to every
	// outbound input. Set to 0 to disable; defaults to 6 in LoadConfigFile.
	// The window is the alternative to Hermes' server-side conversation
	// store: with Store=false and a small window, Hermes sees a stable
	// short-context prompt instead of an ever-growing history that blows
	// past the 64k context window and starves prompt caching.
	MaxHistoryTurns int

	// Store controls whether Hermes persists each turn into its
	// conversation log. Default false (mcp-managed window above replaces
	// it). Flip to true only when you need Hermes' long-term memory and
	// can tolerate the input_tokens explosion that comes with it.
	Store bool
}

// historyTurn is one (player|npc, text) pair in a Relay's short window.
type historyTurn struct {
	role string // "player" or "npc"
	text string
}

// Relay forwards events to a Hermes Gateway. Safe for concurrent use.
type Relay struct {
	cfg     Config
	persona string
	http    *http.Client
	logger  *slog.Logger

	histMu  sync.Mutex
	history []historyTurn // capped at cfg.MaxHistoryTurns
}

// New constructs a Relay. PersonaFile, if given, is loaded once and cached.
func New(cfg Config, logger *slog.Logger) (*Relay, error) {
	if cfg.URL == "" {
		return nil, fmt.Errorf("hermesrelay: URL is required")
	}
	if cfg.Conversation == "" {
		return nil, fmt.Errorf("hermesrelay: Conversation is required when URL is set")
	}
	if cfg.Model == "" {
		return nil, fmt.Errorf("hermesrelay: Model is required when URL is set")
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 120 * time.Second
	}
	if logger == nil {
		logger = slog.Default()
	}
	r := &Relay{
		cfg:    cfg,
		http:   &http.Client{Timeout: cfg.Timeout},
		logger: logger,
	}
	// When DebugPayload is on but no dedicated PayloadLogger was supplied,
	// fall back to the main logger so the records aren't silently dropped.
	if r.cfg.DebugPayload && r.cfg.PayloadLogger == nil {
		r.cfg.PayloadLogger = logger
	}
	if cfg.PersonaFile != "" {
		b, err := os.ReadFile(cfg.PersonaFile)
		if err != nil {
			return nil, fmt.Errorf("hermesrelay: read persona file %q: %w", cfg.PersonaFile, err)
		}
		r.persona = string(b)
	}
	return r, nil
}

// historyText returns the rendered short-window prefix the relay prepends to
// the next outbound input, or "" when the window is empty / disabled.
func (r *Relay) historyText() string {
	if r.cfg.MaxHistoryTurns == 0 {
		return ""
	}
	r.histMu.Lock()
	defer r.histMu.Unlock()
	if len(r.history) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("## Recent conversation (most recent at the bottom)\n")
	for _, t := range r.history {
		b.WriteString("[")
		b.WriteString(t.role)
		b.WriteString("] ")
		b.WriteString(t.text)
		b.WriteString("\n")
	}
	b.WriteString("\n## Current event\n")
	return b.String()
}

// appendHistory adds one turn to the window and trims to MaxHistoryTurns.
// Concurrency-safe; no-op when the window is disabled or text is empty.
func (r *Relay) appendHistory(role, text string) {
	if r.cfg.MaxHistoryTurns == 0 || text == "" {
		return
	}
	r.histMu.Lock()
	defer r.histMu.Unlock()
	r.history = append(r.history, historyTurn{role: role, text: text})
	if len(r.history) > r.cfg.MaxHistoryTurns {
		r.history = r.history[len(r.history)-r.cfg.MaxHistoryTurns:]
	}
}

// extractAssistantReply pulls the most recent assistant message text out of
// a /v1/responses body. Returns "" when the response has no text message
// (tool-only turn) or the body is malformed.
func extractAssistantReply(body []byte) string {
	var m struct {
		Output []struct {
			Type    string `json:"type"`
			Role    string `json:"role,omitempty"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content,omitempty"`
		} `json:"output"`
	}
	if err := json.Unmarshal(body, &m); err != nil {
		return ""
	}
	// Walk back-to-front: the last assistant message is the final reply
	// after any tool calls.
	for i := len(m.Output) - 1; i >= 0; i-- {
		item := m.Output[i]
		if item.Type != "message" || item.Role != "assistant" {
			continue
		}
		for _, c := range item.Content {
			if c.Type == "output_text" && c.Text != "" {
				return c.Text
			}
		}
	}
	return ""
}

// looksLikeUpstreamError returns true for response texts that indicate the
// LLM upstream surfaced its own error string instead of a real reply
// (rate-limit retries, "(empty)" marker, etc.). We don't want these in the
// history window — they'd poison the next turn.
func looksLikeUpstreamError(text string) bool {
	if text == "" || text == "(empty)" {
		return true
	}
	t := strings.ToLower(text)
	return strings.HasPrefix(t, "api call failed") ||
		strings.Contains(t, "http 429") ||
		strings.Contains(t, "http 5")
}

// HandleEvent implements bridge.EventHandler. The POST runs on its own
// goroutine — the bridge read loop never blocks on a slow Hermes. We detach
// from the caller's context so a ws reconnect doesn't cancel in-flight POSTs.
func (r *Relay) HandleEvent(_ context.Context, name string, data json.RawMessage) {
	if !r.ShouldRoute(name, data) {
		return
	}
	// T0 anchor for the turn: this is the moment the ws event landed inside
	// mcp. Compare with the upcoming "hermesrelay forwarded event" elapsed_ms
	// to see how much time mcp itself spends before/after the Hermes call.
	r.logger.Info("hermesrelay event received",
		"event", name, "conversation", r.cfg.Conversation)
	input := events.FormatForHermes(name, data)
	go r.post(input, name)
}

// ShouldRoute reports whether this event matches the relay's NPC filter.
// Events with no recipient field pass (broadcast). Malformed payloads are
// dropped when a filter is configured — the safe default for an NPC filter
// is "do not deliver" rather than "deliver to everyone".
//
// Exported so a Group can route a single event across multiple relays
// without each one re-doing the parse.
func (r *Relay) ShouldRoute(name string, data json.RawMessage) bool {
	if r.cfg.NPCName == "" {
		return true
	}
	recipient, ok, err := events.RecipientNPC(name, data)
	if err != nil {
		r.logger.Debug("hermesrelay malformed payload dropped",
			"event", name, "err", err)
		return false
	}
	if !ok {
		return true
	}
	return recipient == r.cfg.NPCName
}

// request mirrors the subset of OpenAI /v1/responses fields the Hermes
// Gateway api_server platform accepts.
type request struct {
	Model        string `json:"model,omitempty"`
	Input        string `json:"input"`
	Conversation string `json:"conversation,omitempty"`
	Instructions string `json:"instructions,omitempty"`
	Store        bool   `json:"store"`
}

// usageResponse picks the token-accounting fields out of /v1/responses.
// Only what we need for cache-hit telemetry — silently zero when Hermes
// omits them.
type usageResponse struct {
	Usage struct {
		InputTokens        int `json:"input_tokens"`
		OutputTokens       int `json:"output_tokens"`
		TotalTokens        int `json:"total_tokens"`
		InputTokensDetails struct {
			CachedTokens int `json:"cached_tokens"`
		} `json:"input_tokens_details"`
	} `json:"usage"`
}

func (r *Relay) post(input, eventName string) {
	// Prepend the per-relay short-window history so Hermes sees a stable
	// short-context prompt (instead of relying on Store=true which makes
	// input_tokens grow unbounded across turns and starves prompt caching).
	composedInput := r.historyText() + input
	body, err := json.Marshal(request{
		Model:        r.cfg.Model,
		Input:        composedInput,
		Conversation: r.cfg.Conversation,
		Instructions: r.persona,
		Store:        r.cfg.Store,
	})
	if err != nil {
		r.logger.Warn("hermesrelay marshal failed", "event", eventName, "err", err)
		return
	}

	if r.cfg.DebugPayload {
		r.cfg.PayloadLogger.Debug("hermesrelay outbound payload",
			"event", eventName,
			"conversation", r.cfg.Conversation,
			"model", r.cfg.Model,
			"input", composedInput,
			"instructions_len", len(r.persona),
			"body_bytes", len(body),
			"body", string(body),
		)
	}

	ctx, cancel := context.WithTimeout(context.Background(), r.cfg.Timeout)
	defer cancel()

	url := r.cfg.URL + "/v1/responses"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		r.logger.Warn("hermesrelay build request failed", "event", eventName, "err", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if r.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+r.cfg.APIKey)
	}

	started := time.Now()
	resp, err := r.http.Do(req)
	elapsed := time.Since(started)
	if err != nil {
		r.logger.Warn("hermesrelay POST failed",
			"event", eventName, "url", url, "elapsed_ms", elapsed.Milliseconds(), "err", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		r.logger.Warn("hermesrelay non-2xx",
			"event", eventName, "status", resp.StatusCode,
			"elapsed_ms", elapsed.Milliseconds(), "body", string(b))
		return
	}

	// Read the body so we can parse usage and so the keep-alive connection
	// can be reused. 64KB cap is plenty — /v1/responses returns a small
	// JSON envelope, the conversation history is server-side.
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	_, _ = io.Copy(io.Discard, resp.Body)

	var u usageResponse
	_ = json.Unmarshal(respBody, &u) // best-effort; zero values on parse miss

	cacheRatio := 0.0
	if u.Usage.InputTokens > 0 {
		cacheRatio = float64(u.Usage.InputTokensDetails.CachedTokens) / float64(u.Usage.InputTokens)
	}
	r.logger.Info("hermesrelay forwarded event",
		"event", eventName,
		"status", resp.StatusCode,
		"conversation", r.cfg.Conversation,
		"elapsed_ms", elapsed.Milliseconds(),
		"input_tokens", u.Usage.InputTokens,
		"cached_tokens", u.Usage.InputTokensDetails.CachedTokens,
		"cache_ratio", cacheRatio,
		"output_tokens", u.Usage.OutputTokens,
	)

	if r.cfg.DebugPayload {
		r.cfg.PayloadLogger.Debug("hermesrelay inbound response",
			"event", eventName,
			"conversation", r.cfg.Conversation,
			"status", resp.StatusCode,
			"elapsed_ms", elapsed.Milliseconds(),
			"body_bytes", len(respBody),
			"body", string(respBody),
		)
	}

	// Update short-window history. The player turn we just sent is recorded
	// regardless of reply outcome (so the player's next message has continuity
	// even if the LLM tool-only-replied). The NPC reply is recorded only when
	// it looks like a real assistant message — upstream errors like rate-limit
	// strings would poison the next prompt.
	r.appendHistory("player", input)
	if reply := extractAssistantReply(respBody); reply != "" && !looksLikeUpstreamError(reply) {
		r.appendHistory("npc", reply)
	}
}

// Cfg returns the resolved configuration this relay was built with. Used by
// status reporters to enumerate live profiles without reaching into private
// fields.
func (r *Relay) Cfg() Config {
	return r.cfg
}
