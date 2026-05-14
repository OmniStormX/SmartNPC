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

	// Timeout for the POST. Defaults to 30s if zero.
	Timeout time.Duration

	// DebugPayload, when true, makes post() log the full outbound JSON body
	// and full response body in addition to the usage summary. Use only for
	// short debug sessions — the payload contains the entire persona +
	// instructions and can be tens of KB per turn.
	DebugPayload bool
}

// Relay forwards events to a Hermes Gateway. Safe for concurrent use.
type Relay struct {
	cfg     Config
	persona string
	http    *http.Client
	logger  *slog.Logger
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
		cfg.Timeout = 30 * time.Second
	}
	if logger == nil {
		logger = slog.Default()
	}
	r := &Relay{
		cfg:    cfg,
		http:   &http.Client{Timeout: cfg.Timeout},
		logger: logger,
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

// HandleEvent implements bridge.EventHandler. The POST runs on its own
// goroutine — the bridge read loop never blocks on a slow Hermes. We detach
// from the caller's context so a ws reconnect doesn't cancel in-flight POSTs.
func (r *Relay) HandleEvent(_ context.Context, name string, data json.RawMessage) {
	if !r.ShouldRoute(name, data) {
		return
	}
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
	body, err := json.Marshal(request{
		Model:        r.cfg.Model,
		Input:        input,
		Conversation: r.cfg.Conversation,
		Instructions: r.persona,
		Store:        true,
	})
	if err != nil {
		r.logger.Warn("hermesrelay marshal failed", "event", eventName, "err", err)
		return
	}

	if r.cfg.DebugPayload {
		r.logger.Debug("hermesrelay outbound payload",
			"event", eventName,
			"conversation", r.cfg.Conversation,
			"model", r.cfg.Model,
			"input", input,
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
		r.logger.Debug("hermesrelay inbound response",
			"event", eventName,
			"conversation", r.cfg.Conversation,
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
