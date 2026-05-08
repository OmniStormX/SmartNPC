// Package chat implements an LLM-backed NPC chat agent. It listens for
// chat_received MCP notifications, runs a multi-turn tool-calling loop
// against the configured LLM, and sends the final reply via chat_say.
package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/smartnpc/smartnpc-agent/internal/llm"
)

const (
	maxToolRounds         = 5
	defaultTimeout        = 90 * time.Second
	defaultFriendshipWait = 1500 * time.Millisecond
)

// Config configures the chat agent.
type Config struct {
	// Provider is the default / single-LLM backend. When DecisionProvider is
	// nil the agent runs the legacy single-model loop using Provider. When
	// DecisionProvider is set, Provider is used as the persona/role-play layer
	// (a.k.a. PersonaProvider alias).
	Provider llm.Provider
	// DecisionProvider optionally enables the dual-LLM architecture. When set,
	// this model (typically GPT-5.5 or another tool-reliable model) runs the
	// decision / tool-calling stage; its outputs become structured context
	// that is then fed to PersonaProvider for the in-character reply.
	DecisionProvider llm.Provider
	// PersonaProvider is the role-play LLM (typically a local Hermes). Only
	// consulted when DecisionProvider is non-nil; otherwise the agent falls
	// back to Provider. If left nil in dual mode we reuse Provider.
	PersonaProvider llm.Provider
	// DecisionModel / PersonaModel override the model name per stage when the
	// two stages share a BaseURL but need different model strings. Leave empty
	// to use whatever the underlying Provider was constructed with.
	DecisionModel string
	PersonaModel  string
	// Speaker is the NPC display name (must match a game NPC for dialogue box).
	Speaker string
	// SystemPrompt seeds the LLM persona.
	SystemPrompt string
	// Persona, when set, drives dynamic prompt injection — primarily the
	// per-turn friendship tier. SystemPrompt should already contain the
	// static persona prompt (typically produced by LoadPersona).
	Persona *Persona
	// MaxHistory caps the number of user+assistant turns kept in memory.
	MaxHistory int
	// Timeout for a single LLM round-trip. Defaults to 90s.
	Timeout time.Duration
	// FriendshipTimeout caps the per-turn friendship_get query. Kept short so
	// a slow or missing game bridge cannot stall dialogue. Defaults to
	// defaultFriendshipWait; set < 0 to disable the query entirely.
	FriendshipTimeout time.Duration
	// Logger for diagnostics.
	Logger *slog.Logger
}

// Agent holds conversation state and handles incoming chat notifications.
type Agent struct {
	cfg     Config
	mu      sync.Mutex
	history []llm.Message
	session *mcp.ClientSession
	tools   []llm.ToolSpec
	// router is a back-reference used by the npc_send_message local tool so
	// one agent can deliver messages to another. Set via SetRouter() after the
	// router is constructed.
	router interface {
		DeliverNPCMessage(fromNPC, toNPC, message string) bool
		Speakers() []string
	}
	// locations is the named-location table used by the move-intent parser.
	// Initialized from cfg.Persona.NamedLocations when present, else defaults.
	locations *LocationTable
	// lastUserMsgTime tracks when the last player message arrived, used by the
	// proactive ticker to avoid interrupting active conversations.
	lastUserMsgTime time.Time
	// replyDone is an optional test hook: when non-nil, every completed
	// respondAndSay call sends on it (non-blocking). Tests use this to wait
	// for the async goroutine dispatched by HandleNotification without
	// resorting to timing-based sleeps. Never set in production code.
	replyDone chan<- struct{}
}

// New creates a chat agent ready to handle notifications.
func New(cfg Config) *Agent {
	if cfg.Speaker == "" {
		cfg.Speaker = "Abigail"
	}
	if cfg.SystemPrompt == "" {
		cfg.SystemPrompt = "You are a friendly NPC in Stardew Valley. Respond briefly and in character. Use the player's language."
	}
	if cfg.MaxHistory <= 0 {
		cfg.MaxHistory = 20
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaultTimeout
	}
	if cfg.FriendshipTimeout == 0 {
		cfg.FriendshipTimeout = defaultFriendshipWait
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	// Dual-mode sanity: when a DecisionProvider is supplied, the persona stage
	// defaults to Provider so callers who just want to enable the decision
	// layer don't have to wire a second Provider slot by hand.
	if cfg.DecisionProvider != nil && cfg.PersonaProvider == nil {
		cfg.PersonaProvider = cfg.Provider
	}
	a := &Agent{cfg: cfg}
	// Build the named-location table once: persona override wins, otherwise
	// fall back to the Farm defaults so built-in NPCs just work.
	var locs []NamedLocation
	if cfg.Persona != nil && len(cfg.Persona.NamedLocations) > 0 {
		locs = cfg.Persona.NamedLocations
	} else {
		locs = DefaultLocations()
	}
	a.locations = NewLocationTable(locs)
	return a
}

// SetSession wires the MCP session used for tool calls.
// Must be called after mcpclient.Spawn completes.
func (a *Agent) SetSession(session *mcp.ClientSession) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.session = session
}

// Speaker returns the NPC display name this agent is bound to. Useful for
// diagnostics and for routers that key agents by speaker.
func (a *Agent) Speaker() string {
	return a.cfg.Speaker
}

// SetRouter wires the back-reference to the Router so this agent can use the
// npc_send_message local tool to deliver messages to other NPC agents. Must be
// called after the Router has been fully constructed.
func (a *Agent) SetRouter(r interface {
	DeliverNPCMessage(fromNPC, toNPC, message string) bool
	Speakers() []string
}) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.router = r
}

// ReceiveNPCMessage injects a message from another NPC into this agent's
// history and triggers an asynchronous response. The message appears as a
// system-tagged entry so the LLM sees it as world context rather than player
// input. The agent will then formulate an in-character reply and speak it via
// chat_say (which the game displays as a normal dialogue).
func (a *Agent) ReceiveNPCMessage(fromNPC, message string) {
	injected := fmt.Sprintf("[%s 给你传话说：「%s」请你自然地回应这条来自 NPC 同伴的消息，可以选择通过 npc_send_message 回复对方。]", fromNPC, message)
	a.cfg.Logger.Info("received NPC message", "from", fromNPC, "message", message)

	a.mu.Lock()
	a.history = append(a.history, llm.Message{Role: llm.RoleUser, Content: injected})
	a.trimHistory()
	a.mu.Unlock()

	go a.respondAndSay(injected)
}

// LoadTools fetches available MCP tools and caches them for LLM requests.
// Additionally registers local-only tools (like npc_send_message) that the
// LLM can invoke but are handled in-process.
func (a *Agent) LoadTools(ctx context.Context) error {
	a.mu.Lock()
	s := a.session
	a.mu.Unlock()
	if s == nil {
		return fmt.Errorf("session not ready")
	}

	res, err := s.ListTools(ctx, nil)
	if err != nil {
		return fmt.Errorf("list tools: %w", err)
	}

	specs := convertMCPTools(res.Tools)

	// Append local-only tools visible to the LLM.
	specs = append(specs, a.localToolSpecs()...)

	a.mu.Lock()
	a.tools = specs
	a.mu.Unlock()
	a.cfg.Logger.Info("tools loaded", "count", len(specs))
	return nil
}

// localToolSpecs returns tool definitions for in-process tools that don't
// require an MCP roundtrip. These are registered alongside MCP tools so the
// LLM sees them in the same tool catalogue.
func (a *Agent) localToolSpecs() []llm.ToolSpec {
	return []llm.ToolSpec{
		{
			Name:        "npc_send_message",
			Description: "Send a message to another NPC. The recipient NPC will receive your message and may respond. Use this to gossip, relay information, ask for help, or maintain social relationships with other NPCs.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"to": map[string]any{
						"type":        "string",
						"description": "The internal name of the recipient NPC (e.g. \"Abigail\", \"Sebastian\", \"XiaMi\")",
					},
					"message": map[string]any{
						"type":        "string",
						"description": "The message content to deliver to the other NPC",
					},
				},
				"required": []string{"to", "message"},
			},
		},
	}
}

// Tools returns a copy of the tool specs currently visible to the LLM. The
// caller is free to mutate the returned slice; internal state is unaffected.
func (a *Agent) Tools() []llm.ToolSpec {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]llm.ToolSpec, len(a.tools))
	copy(out, a.tools)
	return out
}

// convertMCPTools turns MCP tool descriptors into the provider-agnostic
// llm.ToolSpec shape. The MCP SDK exposes InputSchema as an opaque value that
// JSON-marshals to a JSON Schema object; we normalize it to map[string]any so
// the OpenAI provider can embed it as the function `parameters` field.
func convertMCPTools(tools []*mcp.Tool) []llm.ToolSpec {
	specs := make([]llm.ToolSpec, 0, len(tools))
	for _, t := range tools {
		if t == nil || t.Name == "" {
			continue
		}
		specs = append(specs, llm.ToolSpec{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: normalizeSchema(t.InputSchema),
		})
	}
	return specs
}

// normalizeSchema coerces an arbitrary JSON-Schema-ish value into
// map[string]any. If the value cannot be represented as an object schema we
// fall back to an empty object schema so the LLM sees a valid (if permissive)
// parameters definition.
func normalizeSchema(s any) map[string]any {
	if s == nil {
		return map[string]any{"type": "object", "properties": map[string]any{}}
	}
	if m, ok := s.(map[string]any); ok {
		ensureProperties(m)
		return m
	}
	b, err := json.Marshal(s)
	if err != nil {
		return map[string]any{"type": "object", "properties": map[string]any{}}
	}
	out := map[string]any{}
	if err := json.Unmarshal(b, &out); err != nil {
		return map[string]any{"type": "object", "properties": map[string]any{}}
	}
	ensureProperties(out)
	return out
}

// ensureProperties guarantees the schema has "type" and "properties" fields,
// which OpenAI API requires for function parameters. All tool parameter schemas
// must include both fields; schemas passed through here are always top-level
// function parameter definitions.
func ensureProperties(m map[string]any) {
	if _, ok := m["type"]; !ok {
		m["type"] = "object"
	}
	if _, ok := m["properties"]; !ok {
		m["properties"] = map[string]any{}
	}
}

// HandleNotification returns a function suitable for mcp.ClientOptions.LoggingMessageHandler.
func (a *Agent) HandleNotification() func(context.Context, *mcp.LoggingMessageRequest) {
	return func(_ context.Context, req *mcp.LoggingMessageRequest) {
		// Debug: log all incoming notifications.
		if req != nil && req.Params != nil {
			a.cfg.Logger.Debug("notification received", "data", req.Params.Data)
		}

		// Try chat_message (player typed in custom ChatWindow UI).
		if npc, text, ok := extractChatMessage(req); ok && npc == a.cfg.Speaker {
			a.cfg.Logger.Info("chat_message received", "npc", npc, "text", text)
			go a.respondAndSay(text)
			return
		}

		// Try chat_received (player typed in Ctrl+T chat box).
		if text, ok := extractChatText(req, a.cfg.Speaker); ok {
			a.cfg.Logger.Info("chat received", "text", text)
			go a.respondAndSay(text)
			return
		}

		// Try npc_interact (player clicked on this NPC — no chat window open).
		if npc, ok := extractNpcInteract(req); ok && npc == a.cfg.Speaker {
			a.cfg.Logger.Info("npc_interact received", "npc", npc)
			go a.respondAndSay("[玩家走过来点击了你，主动和你打招呼。请用符合你人设的方式自然地回应。]")
			return
		}
	}
}

// respondAndSay runs the LLM and sends the reply via chat_say.
func (a *Agent) respondAndSay(userText string) {
	// Always signal completion so tests (and any future observer) can wait
	// for the async goroutine without sleeping. Done is best-effort; a
	// nil/unbuffered-and-unread channel simply gets skipped.
	defer a.signalReplyDone()

	// Track last user interaction so the proactive ticker can skip if active.
	a.mu.Lock()
	a.lastUserMsgTime = time.Now()
	a.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), a.cfg.Timeout)
	defer cancel()

	reply, err := a.respond(ctx, userText)
	if err != nil {
		a.cfg.Logger.Error("LLM call failed", "err", err)
		reply = "...抱歉，我刚才走神了。"
	}
	a.cfg.Logger.Info("LLM replied", "reply", reply)

	a.mu.Lock()
	s := a.session
	a.mu.Unlock()
	if s == nil {
		a.cfg.Logger.Warn("session not ready, dropping reply")
		return
	}

	_, err = s.CallTool(ctx, &mcp.CallToolParams{
		Name: "chat_say",
		Arguments: map[string]any{
			"speaker": a.cfg.Speaker,
			"text":    reply,
		},
	})
	if err != nil {
		a.cfg.Logger.Warn("chat_say failed", "err", err)
	}
}

// signalReplyDone fires the optional replyDone hook without blocking or
// racing against concurrent dispatches.
func (a *Agent) signalReplyDone() {
	a.mu.Lock()
	ch := a.replyDone
	a.mu.Unlock()
	if ch == nil {
		return
	}
	select {
	case ch <- struct{}{}:
	default:
	}
}

// getFriendshipContext queries friendship_get on the active MCP session and
// returns a compact system-prompt addendum describing the current tier. The
// returned string is empty when:
//
//   - the persona does not define friendship_behaviors,
//   - no session is wired yet,
//   - FriendshipTimeout is negative (query disabled),
//   - friendship_get fails or returns ok=false,
//   - hearts do not match any configured range.
//
// Failure is deliberately silent: missing friendship context degrades the
// system prompt gracefully rather than killing the chat turn.
func (a *Agent) getFriendshipContext(parent context.Context) string {
	a.mu.Lock()
	persona := a.cfg.Persona
	session := a.session
	timeout := a.cfg.FriendshipTimeout
	a.mu.Unlock()

	if persona == nil || len(persona.FriendshipBehaviors) == 0 {
		a.cfg.Logger.Debug("friendship skip: no persona or no friendship_behaviors")
		return ""
	}
	if session == nil {
		a.cfg.Logger.Debug("friendship skip: no session")
		return ""
	}
	if timeout < 0 {
		a.cfg.Logger.Debug("friendship skip: disabled by negative timeout")
		return ""
	}
	if timeout == 0 {
		timeout = defaultFriendshipWait
	}

	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "friendship_get",
		Arguments: map[string]any{"npc": a.cfg.Speaker},
	})
	if err != nil {
		a.cfg.Logger.Debug("friendship_get failed, skipping injection", "err", err)
		return ""
	}
	if res == nil || res.IsError {
		a.cfg.Logger.Debug("friendship_get returned error")
		return ""
	}

	var out struct {
		OK     bool   `json:"ok"`
		Hearts int    `json:"hearts"`
		Status string `json:"status"`
	}
	if res.StructuredContent != nil {
		b, _ := json.Marshal(res.StructuredContent)
		_ = json.Unmarshal(b, &out)
	} else {
		for _, c := range res.Content {
			if txt, ok := c.(*mcp.TextContent); ok && txt.Text != "" {
				_ = json.Unmarshal([]byte(txt.Text), &out)
				break
			}
		}
	}
	if !out.OK {
		a.cfg.Logger.Debug("friendship_get returned ok=false")
		return ""
	}

	key, behavior, ok := persona.RangeKeyAtHearts(out.Hearts)
	if !ok {
		a.cfg.Logger.Debug("friendship hearts not in any range", "hearts", out.Hearts)
		return ""
	}
	a.cfg.Logger.Info("friendship context", "hearts", out.Hearts, "tier", key, "status", out.Status)
	return formatFriendshipContext(out.Hearts, out.Status, key, behavior)
}

// formatFriendshipContext renders the per-turn friendship addendum that gets
// appended to the system prompt. Kept as a standalone helper so tests can
// lock the exact wording without spinning up a full Agent.
func formatFriendshipContext(hearts int, status, key string, b FriendshipBehavior) string {
	var sb strings.Builder
	sb.WriteString("[Current friendship: ")
	sb.WriteString(strconv.Itoa(hearts))
	sb.WriteString(" hearts")
	if status != "" && status != "none" {
		sb.WriteString(" · ")
		sb.WriteString(status)
	}
	sb.WriteString(" · tier ")
	sb.WriteString(key)
	sb.WriteString("]\n")

	if b.Tone != "" {
		sb.WriteString("Act with this tone: ")
		sb.WriteString(b.Tone)
		sb.WriteString(".")
	}
	if b.Willingness != "" {
		if sb.Len() > 0 && !strings.HasSuffix(sb.String(), "\n") {
			sb.WriteString(" ")
		}
		sb.WriteString("Openness level: ")
		sb.WriteString(b.Willingness)
		sb.WriteString(".")
	}
	if b.Greeting != "" {
		sb.WriteString(" If you greet the player first, borrow the spirit of: \"")
		sb.WriteString(b.Greeting)
		sb.WriteString("\".")
	}
	if b.Notes != "" {
		sb.WriteString(" Notes: ")
		sb.WriteString(b.Notes)
		sb.WriteString(".")
	}
	sb.WriteString(" Never quote the numeric heart value to the player.")
	return sb.String()
}

// getGameStateContext queries game_get_time and game_get_weather on the active
// MCP session, returning a compact system-prompt addendum with current game
// state. Returns "" on any failure (silent degradation).
func (a *Agent) getGameStateContext(parent context.Context) string {
	a.mu.Lock()
	session := a.session
	timeout := a.cfg.FriendshipTimeout // reuse same timeout budget
	a.mu.Unlock()

	if session == nil {
		return ""
	}
	if timeout <= 0 {
		timeout = defaultFriendshipWait
	}

	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	var parts []string

	// Query time
	timeRes, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "game_get_time",
		Arguments: map[string]any{},
	})
	if err == nil && timeRes != nil && !timeRes.IsError {
		var t struct {
			Hour      int    `json:"hour"`
			Minute    int    `json:"minute"`
			Day       int    `json:"day"`
			DayOfWeek string `json:"day_of_week"`
			Season    string `json:"season"`
			Year      int    `json:"year"`
		}
		if raw := extractToolText(timeRes); raw != "" {
			_ = json.Unmarshal([]byte(raw), &t)
			period := "morning"
			if t.Hour >= 18 {
				period = "evening"
			} else if t.Hour >= 12 {
				period = "afternoon"
			}
			parts = append(parts, fmt.Sprintf("Time: %02d:%02d (%s), %s %d %s Year %d",
				t.Hour, t.Minute, period, t.Season, t.Day, t.DayOfWeek, t.Year))
		}
	} else {
		a.cfg.Logger.Debug("game_get_time pre-query failed", "err", err)
	}

	// Query weather
	weatherRes, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "game_get_weather",
		Arguments: map[string]any{},
	})
	if err == nil && weatherRes != nil && !weatherRes.IsError {
		var w struct {
			Weather string `json:"weather"`
		}
		if raw := extractToolText(weatherRes); raw != "" {
			_ = json.Unmarshal([]byte(raw), &w)
			if w.Weather != "" {
				parts = append(parts, fmt.Sprintf("Weather: %s", w.Weather))
			}
		}
	} else {
		a.cfg.Logger.Debug("game_get_weather pre-query failed", "err", err)
	}

	if len(parts) == 0 {
		return ""
	}
	result := "[Current game state] " + strings.Join(parts, " | ") + ". Use this info naturally in conversation — do not recite raw data."
	a.cfg.Logger.Debug("game state injected", "context", result)
	return result
}

// extractToolText pulls the first text content from a CallToolResult.
func extractToolText(res *mcp.CallToolResult) string {
	if res.StructuredContent != nil {
		b, _ := json.Marshal(res.StructuredContent)
		return string(b)
	}
	for _, c := range res.Content {
		if txt, ok := c.(*mcp.TextContent); ok && txt.Text != "" {
			return txt.Text
		}
	}
	return ""
}

// applyMoveIntent inspects the user utterance for a movement keyword and,
// when appropriate, preemptively fires `npc_move_to` so the NPC starts
// walking regardless of whether the LLM emits tool_calls. It returns the
// user text with a short bracketed hint appended so the LLM can narrate the
// action in character.
//
// Three outcomes:
//
//  1. No move intent → returns userText unchanged.
//  2. Move intent with a resolved named location → fires the tool, appends
//     "[你正在走向X]" and returns.
//  3. Move intent without a location match → appends "[玩家要求移动但目的地
//     不明，请询问具体位置]" so the LLM asks for clarification.
//
// Tool execution failures are logged but never surface as errors: the chat
// turn should still produce a natural reply even when the motion fails.
func (a *Agent) applyMoveIntent(ctx context.Context, userText string) string {
	if a.locations == nil {
		return userText
	}
	intent := a.locations.DetectMoveIntent(userText)
	if !intent.HasIntent {
		return userText
	}

	if intent.Location == nil {
		a.cfg.Logger.Debug("move intent detected but no named location matched", "text", userText)
		return userText + "\n[系统：玩家让你移动但你不确定具体位置，请询问更明确的目的地]"
	}

	loc := intent.Location
	a.cfg.Logger.Info("auto-executing move_to", "location", loc.Name, "x", loc.X, "y", loc.Y)

	// Kick off npc_move_to on the active MCP session. If the session is not
	// wired (e.g. in unit tests) we still annotate the user message so the
	// LLM responds as if the move were in progress — tests can assert the
	// prompt shape without needing a live mod.
	a.mu.Lock()
	s := a.session
	a.mu.Unlock()
	if s != nil {
		args := map[string]any{
			"npc": a.cfg.Speaker,
			"x":   loc.X,
			"y":   loc.Y,
		}
		if loc.Map != "" {
			args["map"] = loc.Map
		}
		moveRes, err := s.CallTool(ctx, &mcp.CallToolParams{Name: "npc_move_to", Arguments: args})
		if err != nil {
			a.cfg.Logger.Warn("auto npc_move_to failed", "err", err, "location", loc.Name)
		} else {
			a.cfg.Logger.Info("auto npc_move_to result",
				"location", loc.Name,
				"args", args,
				"result", extractToolText(moveRes),
			)
		}
	}

	return userText + "\n[系统：你已经开始走向" + loc.Name + "了，请自然地回应]"
}

// applyBehaviorIntent handles high-level behavior verbs (summon/follow/lead/
// stop) before the normal move-intent parser runs. It returns (effectiveText,
// handled) — when handled is true the caller should skip `applyMoveIntent`
// entirely, since behavior keywords often overlap with plain move keywords
// (e.g. "带我去湖边" is BOTH lead + move).
//
// Outcomes:
//
//  1. No behavior intent → returns userText unchanged, handled=false.
//  2. "stop"   → calls npc_follow_stop, appends "[系统：你已停止跟随]".
//  3. "follow" → calls npc_follow_start, appends "[系统：你已开始跟着玩家]".
//  4. "summon" → calls npc_summon, appends "[系统：你正在赶往玩家身边]".
//  5. "lead"   → if a landmark was resolved, calls npc_lead_to and appends
//                "[系统：你正在带玩家前往X]"; if no landmark, falls through
//                to follow as a graceful fallback.
//
// Tool failures are logged but never surface — the chat turn must still
// produce a natural reply even when the motion fails.
func (a *Agent) applyBehaviorIntent(ctx context.Context, userText string) (string, bool) {
	if a.locations == nil {
		return userText, false
	}
	intent, loc := a.locations.DetectBehaviorIntent(userText)
	if intent == "" {
		return userText, false
	}

	a.mu.Lock()
	s := a.session
	speaker := a.cfg.Speaker
	a.mu.Unlock()

	call := func(name string, args map[string]any) {
		if s == nil {
			return
		}
		res, err := s.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			a.cfg.Logger.Warn("auto behavior tool failed", "name", name, "err", err)
		} else {
			a.cfg.Logger.Info("auto behavior tool result",
				"name", name,
				"args", args,
				"result", extractToolText(res),
			)
		}
	}

	switch intent {
	case "stop":
		a.cfg.Logger.Info("auto-executing follow_stop")
		call("npc_follow_stop", map[string]any{"npc": speaker})
		return userText + "\n[系统：你已停止跟随玩家，请自然地回应]", true

	case "follow":
		a.cfg.Logger.Info("auto-executing follow_start")
		call("npc_follow_start", map[string]any{"npc": speaker})
		return userText + "\n[系统：你已开始跟着玩家走，请自然地回应]", true

	case "summon":
		a.cfg.Logger.Info("auto-executing summon")
		call("npc_summon", map[string]any{"npc": speaker})
		return userText + "\n[系统：你正在赶往玩家身边，请自然地回应]", true

	case "lead":
		if loc == nil {
			// "lead the way" without a destination — degrade to follow so
			// something sensible happens.
			a.cfg.Logger.Info("lead intent without destination, falling back to follow_start")
			call("npc_follow_start", map[string]any{"npc": speaker})
			return userText + "\n[系统：玩家要你带路但没说具体地方，请自然地反问或先跟上]", true
		}
		a.cfg.Logger.Info("auto-executing lead_to", "location", loc.Name, "x", loc.X, "y", loc.Y)
		args := map[string]any{
			"npc": speaker,
			"x":   loc.X,
			"y":   loc.Y,
		}
		if loc.Map != "" {
			args["map"] = loc.Map
		}
		call("npc_lead_to", args)
		return userText + "\n[系统：你正在带玩家前往" + loc.Name + "，请自然地回应]", true
	}
	return userText, false
}

// respond runs the LLM loop: send messages → if tool_calls → execute → repeat.
func (a *Agent) respond(ctx context.Context, userText string) (string, error) {
	// Dual-LLM routing: when a decision layer is configured, defer to the
	// two-stage pipeline (decision → tool exec → persona). Otherwise fall
	// through to the legacy single-model loop below.
	a.mu.Lock()
	hasDecision := a.cfg.DecisionProvider != nil
	a.mu.Unlock()
	if hasDecision {
		return a.respondDual(ctx, userText)
	}

	// Look up friendship tier up front so the LLM can calibrate tone before it
	// picks any tools itself. A short timeout keeps a slow/missing bridge from
	// stalling the chat turn; on failure we fall back to an empty addendum.
	friendshipCtx := a.getFriendshipContext(ctx)

	// Pre-fetch game state (time + weather) so LLM has concrete info even if
	// it doesn't proactively call the tools itself.
	gameStateCtx := a.getGameStateContext(ctx)

	extra := friendshipCtx
	if gameStateCtx != "" {
		if extra != "" {
			extra += "\n\n"
		}
		extra += gameStateCtx
	}

	// All tool invocations (move, follow, summon, etc.) are delegated to the
	// LLM's own judgment via normal tool_calls. No keyword-based pre-processing.
	effectiveUserText := userText

	a.mu.Lock()
	a.history = append(a.history, llm.Message{Role: llm.RoleUser, Content: effectiveUserText})
	a.trimHistory()
	msgs := a.buildMessages(extra)
	tools := a.tools
	a.mu.Unlock()

	for round := 0; round < maxToolRounds; round++ {
		resp, err := a.cfg.Provider.Chat(ctx, llm.ChatRequest{
			Messages:    msgs,
			Tools:       tools,
			Temperature: 0.8,
			MaxTokens:   300,
		})
		if err != nil {
			return "", err
		}

		// If the model produced a text response, we're done.
		if len(resp.ToolCalls) == 0 {
			reply := resp.Content
			if reply == "" {
				reply = "(no response)"
			}
			a.mu.Lock()
			a.history = append(a.history, llm.Message{Role: llm.RoleAssistant, Content: reply})
			a.trimHistory()
			a.mu.Unlock()
			return reply, nil
		}

		// Model wants tool calls — log the structured data for debugging.
		for i, tc := range resp.ToolCalls {
			argsJSON, _ := json.Marshal(tc.Arguments)
			a.cfg.Logger.Info("LLM tool_call",
				"round", round,
				"index", i,
				"id", tc.ID,
				"name", tc.Name,
				"arguments", string(argsJSON),
			)
		}

		// Model wants tool calls — execute them and feed results back.
		// First, append assistant message with tool calls to history.
		assistantMsg := llm.Message{Role: llm.RoleAssistant, ToolCalls: resp.ToolCalls}
		msgs = append(msgs, assistantMsg)

		for _, tc := range resp.ToolCalls {
			result, err := a.executeTool(ctx, tc)
			if err != nil {
				result = fmt.Sprintf("error: %v", err)
			}
			msgs = append(msgs, llm.Message{
				Role:       llm.RoleTool,
				Content:    result,
				Name:       tc.Name,
				ToolCallID: tc.ID,
			})
			a.cfg.Logger.Info("tool result",
				"name", tc.Name,
				"call_id", tc.ID,
				"arguments", func() string { b, _ := json.Marshal(tc.Arguments); return string(b) }(),
				"success", err == nil,
				"result", truncateStr(result, 500),
			)
		}
	}

	// Exhausted tool rounds — ask LLM for a final text reply without tools.
	resp, err := a.cfg.Provider.Chat(ctx, llm.ChatRequest{
		Messages:    msgs,
		Temperature: 0.8,
		MaxTokens:   300,
	})
	if err != nil {
		return "", err
	}
	reply := resp.Content
	if reply == "" {
		reply = "(no response)"
	}
	a.mu.Lock()
	a.history = append(a.history, llm.Message{Role: llm.RoleAssistant, Content: reply})
	a.trimHistory()
	a.mu.Unlock()
	return reply, nil
}

// executeTool calls an MCP tool via the session, or handles local-only tools
// (like npc_send_message) in-process without going through the bridge.
func (a *Agent) executeTool(ctx context.Context, tc llm.ToolCall) (string, error) {
	// Local tools: handled in-process without MCP roundtrip.
	if result, handled := a.executeLocalTool(tc); handled {
		return result, nil
	}

	a.mu.Lock()
	s := a.session
	a.mu.Unlock()
	if s == nil {
		return "", fmt.Errorf("session not ready")
	}

	res, err := s.CallTool(ctx, &mcp.CallToolParams{
		Name:      tc.Name,
		Arguments: tc.Arguments,
	})
	if err != nil {
		return "", fmt.Errorf("call %s: %w", tc.Name, err)
	}

	if res.IsError {
		return fmt.Sprintf("tool error: %v", res.Content), nil
	}

	// Extract text content from result.
	if res.StructuredContent != nil {
		b, _ := json.Marshal(res.StructuredContent)
		return string(b), nil
	}
	var out string
	for _, c := range res.Content {
		if txt, ok := c.(*mcp.TextContent); ok {
			out += txt.Text
		}
	}
	if out == "" {
		out = "{}"
	}
	return out, nil
}

// executeLocalTool handles tools that run in-process (no MCP roundtrip).
// Returns (result, true) if handled, ("", false) if the tool should be
// routed to MCP normally.
func (a *Agent) executeLocalTool(tc llm.ToolCall) (string, bool) {
	switch tc.Name {
	case "npc_send_message":
		return a.handleNpcSendMessage(tc.Arguments), true
	default:
		return "", false
	}
}

// handleNpcSendMessage implements the local npc_send_message tool. It delivers
// a message from this agent to another NPC agent via the router.
func (a *Agent) handleNpcSendMessage(args map[string]any) string {
	toNPC, _ := args["to"].(string)
	message, _ := args["message"].(string)
	if toNPC == "" || message == "" {
		return `{"ok":false,"error":"missing required fields: to, message"}`
	}

	a.mu.Lock()
	r := a.router
	speaker := a.cfg.Speaker
	a.mu.Unlock()

	if r == nil {
		return `{"ok":false,"error":"router not configured, cannot send inter-NPC messages"}`
	}

	if ok := r.DeliverNPCMessage(speaker, toNPC, message); !ok {
		return fmt.Sprintf(`{"ok":false,"error":"recipient NPC %q not found"}`, toNPC)
	}

	a.cfg.Logger.Info("npc_send_message delivered", "from", speaker, "to", toNPC, "message", message)
	return fmt.Sprintf(`{"ok":true,"delivered_to":"%s"}`, toNPC)
}

// buildMessages constructs system + history for the LLM request. When extra
// is non-empty it is appended to the system message as a separate paragraph —
// used to inject per-turn dynamic context (e.g. current friendship tier)
// without mutating the cached persona prompt.
func (a *Agent) buildMessages(extra string) []llm.Message {
	system := a.cfg.SystemPrompt
	if extra != "" {
		system = system + "\n\n" + extra
	}
	msgs := make([]llm.Message, 0, 1+len(a.history))
	msgs = append(msgs, llm.Message{Role: llm.RoleSystem, Content: system})
	msgs = append(msgs, a.history...)
	return msgs
}

// trimHistory keeps only the most recent MaxHistory messages.
func (a *Agent) trimHistory() {
	if len(a.history) > a.cfg.MaxHistory*2 {
		a.history = a.history[len(a.history)-a.cfg.MaxHistory*2:]
	}
}

// truncateStr caps a string at max bytes for log readability.
func truncateStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

// extractChatText extracts player text from a chat_received notification.
func extractChatText(req *mcp.LoggingMessageRequest, selfSpeaker string) (string, bool) {
	text, src, ok := extractChatReceivedRaw(req)
	if !ok {
		return "", false
	}
	if src == selfSpeaker {
		return "", false
	}
	return text, true
}

// extractChatReceivedText is the router-friendly variant that does not
// filter by speaker. It returns the player text regardless of who reported
// it; the caller decides routing.
func extractChatReceivedText(req *mcp.LoggingMessageRequest) (string, bool) {
	text, _, ok := extractChatReceivedRaw(req)
	return text, ok
}

// extractChatReceivedRaw parses a chat_received notification and returns
// (text, source, ok). Shared helper for the two wrappers above.
func extractChatReceivedRaw(req *mcp.LoggingMessageRequest) (string, string, bool) {
	if req == nil || req.Params == nil {
		return "", "", false
	}
	m, ok := req.Params.Data.(map[string]any)
	if !ok {
		return "", "", false
	}
	if m["kind"] != "stardew/event" || m["name"] != "chat_received" {
		return "", "", false
	}
	raw, ok := m["data"]
	if !ok {
		return "", "", false
	}
	var inner struct {
		Text   string `json:"text"`
		Source string `json:"source"`
	}
	switch v := raw.(type) {
	case json.RawMessage:
		_ = json.Unmarshal(v, &inner)
	case map[string]any:
		if t, ok := v["text"].(string); ok {
			inner.Text = t
		}
		if s, ok := v["source"].(string); ok {
			inner.Source = s
		}
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return "", "", false
		}
		_ = json.Unmarshal(b, &inner)
	}
	if inner.Text == "" {
		return "", "", false
	}
	return inner.Text, inner.Source, true
}

// extractNpcInteract extracts the NPC name from an npc_interact notification.
func extractNpcInteract(req *mcp.LoggingMessageRequest) (string, bool) {
	if req == nil || req.Params == nil {
		return "", false
	}
	m, ok := req.Params.Data.(map[string]any)
	if !ok {
		return "", false
	}
	if m["kind"] != "stardew/event" || m["name"] != "npc_interact" {
		return "", false
	}
	raw, ok := m["data"]
	if !ok {
		return "", false
	}
	var inner struct {
		Npc    string `json:"npc"`
		Source string `json:"source"`
	}
	switch v := raw.(type) {
	case json.RawMessage:
		_ = json.Unmarshal(v, &inner)
	case map[string]any:
		if n, ok := v["npc"].(string); ok {
			inner.Npc = n
		}
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return "", false
		}
		_ = json.Unmarshal(b, &inner)
	}
	if inner.Npc == "" {
		return "", false
	}
	return inner.Npc, true
}

// extractChatMessage extracts NPC name and player text from a chat_message notification.
// This event is sent when the player types in the custom ChatWindow UI.
func extractChatMessage(req *mcp.LoggingMessageRequest) (npc string, text string, ok bool) {
	if req == nil || req.Params == nil {
		return "", "", false
	}
	m, mOk := req.Params.Data.(map[string]any)
	if !mOk {
		return "", "", false
	}
	if m["kind"] != "stardew/event" || m["name"] != "chat_message" {
		return "", "", false
	}
	raw, rawOk := m["data"]
	if !rawOk {
		return "", "", false
	}
	var inner struct {
		Npc    string `json:"npc"`
		Text   string `json:"text"`
		Source string `json:"source"`
	}
	switch v := raw.(type) {
	case json.RawMessage:
		_ = json.Unmarshal(v, &inner)
	case map[string]any:
		if n, k := v["npc"].(string); k {
			inner.Npc = n
		}
		if t, k := v["text"].(string); k {
			inner.Text = t
		}
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return "", "", false
		}
		_ = json.Unmarshal(b, &inner)
	}
	if inner.Npc == "" || inner.Text == "" {
		return "", "", false
	}
	return inner.Npc, inner.Text, true
}
