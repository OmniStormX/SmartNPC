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
	"path/filepath"
	"sync"
	"time"

	"github.com/OmniStormX/SmartNPC/adapters/stardew/events"
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

	// CriticalPolicyFile is an optional path to a markdown file containing
	// critical runtime rules that MUST survive context compression. Its
	// contents are prepended to `instructions` on every POST so they are
	// always present in the system prompt regardless of conversation trimming.
	CriticalPolicyFile string

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

	// Store controls whether Hermes persists each turn into its
	// conversation log. Default true — Hermes owns conversation memory
	// (with its compression layer); mcp does NOT maintain a redundant
	// short-window history of its own.
	Store bool
}

// Relay forwards events to a Hermes Gateway. Safe for concurrent use.
type Relay struct {
	cfg              Config
	baseConversation string // original conversation from config (never mutates)
	conversation     string // current active conversation (rotated on day_started)
	mu               sync.RWMutex
	persona          string
	criticalRules    string // loaded from CriticalPolicyFile, sent with every POST
	http             *http.Client
	logger           *slog.Logger
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
		cfg:              cfg,
		baseConversation: cfg.Conversation,
		conversation:     cfg.Conversation,
		http:             &http.Client{Timeout: cfg.Timeout},
		logger:           logger,
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
	// Critical policy is loaded from a well-known path if not explicitly set.
	// It is sent as part of `instructions` on every POST so compression
	// cannot truncate it. Default: hermes/profiles/_master/critical-policy.md
	if cfg.CriticalPolicyFile == "" {
		cfg.CriticalPolicyFile = resolveCriticalPolicyFile()
	}
	if cfg.CriticalPolicyFile != "" {
		b, err := os.ReadFile(cfg.CriticalPolicyFile)
		if err != nil {
			logger.Warn("hermesrelay: critical policy not loaded",
				"path", cfg.CriticalPolicyFile, "err", err)
		} else {
			r.criticalRules = string(b)
			logger.Info("hermesrelay: critical policy loaded",
				"path", cfg.CriticalPolicyFile, "len", len(r.criticalRules))
		}
	}
	return r, nil
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
		"event", name, "conversation", r.getConversation())
	input := events.FormatForHermes(name, data)
	LogRelayRequest(r.cfg.NPCName, name, len(input), len(r.persona)+len(r.criticalRules))
	go r.post(input, name)
}

// resolveCriticalPolicyFile auto-discovers the critical policy file relative
// to the executable. Returns empty string when discovery fails.
func resolveCriticalPolicyFile() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	// From smartnpc-mcp/bin/ or smartnpc-mcp/cmd/smartnpc-mcp/, walk up
	// to the repo root and then into hermes/profiles/_master/.
	dir := filepath.Dir(exe)
	for i := 0; i < 4; i++ {
		candidate := filepath.Join(dir, "hermes", "profiles", "_master", "critical-policy.md")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		dir = filepath.Dir(dir)
	}
	return ""
}

// RotateSession replaces the active conversation ID with
// "<baseConversation>-<suffix>". This effectively starts a fresh Hermes
// session while keeping the same gateway/model/persona config. Thread-safe.
func (r *Relay) RotateSession(suffix string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.conversation = r.baseConversation + "-" + suffix
	r.logger.Info("hermesrelay session rotated",
		"npc", r.cfg.NPCName,
		"conversation", r.conversation)
}

// getConversation returns the current conversation ID under read lock.
func (r *Relay) getConversation() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.conversation
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
	// Snapshot the current conversation under lock so a concurrent
	// RotateSession doesn't race with the in-flight POST.
	conv := r.getConversation()

	// Build instructions: critical rules (always-loaded, survive compression)
	// prepended before persona (NPC personality from SOUL.md).
	instructions := r.criticalRules
	if r.persona != "" {
		if instructions != "" {
			instructions += "\n\n---\n\n"
		}
		instructions += r.persona
	}

	// Conversation memory is fully owned by Hermes (Store=true by default).
	// mcp sends only the current event; no client-side history window.
	body, err := json.Marshal(request{
		Model:        r.cfg.Model,
		Input:        input,
		Conversation: conv,
		Instructions: instructions,
		Store:        r.cfg.Store,
	})
	if err != nil {
		r.logger.Warn("hermesrelay marshal failed", "event", eventName, "err", err)
		return
	}

	if r.cfg.DebugPayload {
		r.cfg.PayloadLogger.Debug("hermesrelay outbound payload",
			"event", eventName,
			"conversation", conv,
			"model", r.cfg.Model,
			"input", input,
			"instructions_len", len(instructions),
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
		LogRelayError(r.cfg.NPCName, eventName, 0, elapsed, err.Error())
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		r.logger.Warn("hermesrelay non-2xx",
			"event", eventName, "status", resp.StatusCode,
			"elapsed_ms", elapsed.Milliseconds(), "body", string(b))
		LogRelayError(r.cfg.NPCName, eventName, resp.StatusCode, elapsed, string(b))
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
		"conversation", conv,
		"elapsed_ms", elapsed.Milliseconds(),
		"input_tokens", u.Usage.InputTokens,
		"cached_tokens", u.Usage.InputTokensDetails.CachedTokens,
		"cache_ratio", cacheRatio,
		"output_tokens", u.Usage.OutputTokens,
	)
	LogRelayResponse(r.cfg.NPCName, eventName, resp.StatusCode, elapsed,
		u.Usage.InputTokens, u.Usage.InputTokensDetails.CachedTokens, u.Usage.OutputTokens)

	if r.cfg.DebugPayload {
		r.cfg.PayloadLogger.Debug("hermesrelay inbound response",
			"event", eventName,
			"conversation", conv,
			"status", resp.StatusCode,
			"elapsed_ms", elapsed.Milliseconds(),
			"body_bytes", len(respBody),
			"body", string(respBody),
		)
	}
}

// Cfg returns the resolved configuration this relay was built with. Used by
// status reporters to enumerate live profiles without reaching into private
// fields.
func (r *Relay) Cfg() Config {
	return r.cfg
}
