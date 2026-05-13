// Multi-NPC routing.
//
// A Router holds a set of per-speaker Agent instances that share a single
// MCP session + tool catalogue. Its HandleNotification dispatches each
// incoming stardew/event to the correct agent based on the event's `npc`
// field (for chat_message / npc_interact), or the most recently interacted
// agent (for chat_received, which carries no speaker). Events that target
// an unknown speaker are dropped with a debug log.
//
// Router is safe for concurrent use: Register is guarded by RWMutex so
// late additions (dev/tests) don't race the reader side; each agent's
// respond loop runs asynchronously and owns its own history.

package chat

import (
	"context"
	"fmt"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Router dispatches MCP notifications to per-speaker agents.
type Router struct {
	mu     sync.RWMutex
	agents map[string]*Agent // keyed by lowercase speaker
	order  []string          // insertion order, useful for diagnostics
	// lastActive holds the speaker name of the most recent targeted event.
	// Used to route chat_received (which has no npc field) to the agent the
	// player is presumably talking to.
	lastActive string

	// groupHandler is an optional group-chat dispatcher. When set and a
	// group_create / group_message event arrives, it's routed here instead
	// of to a single agent. Injected via SetGroupHandler to avoid circular
	// imports with the group package.
	groupHandler GroupHandler
	// activeGroupID tracks which group the player is currently chatting in.
	// When non-empty, chat_received routes to the group instead of lastActive.
	activeGroupID string
}

// GroupHandler abstracts the group orchestrator so the router doesn't need
// to import the group package directly (avoiding circular deps).
type GroupHandler interface {
	CreateGroup(participants []string) (string, error)
	OnPlayerMessage(ctx context.Context, groupID, text string)
}

// NewRouter returns an empty Router. Use Register to add agents.
//
// For convenience at call sites that already have a slice of agents,
// NewRouterFromAgents registers them in one call.
func NewRouter() *Router {
	return &Router{
		agents: make(map[string]*Agent),
	}
}

// NewRouterFromAgents is shorthand for NewRouter followed by a Register
// loop. Kept as a helper so existing callers / tests don't need to rewrite
// their setup when switching to the Register-based API.
func NewRouterFromAgents(agents []*Agent) *Router {
	r := NewRouter()
	for _, a := range agents {
		if a == nil {
			continue
		}
		r.Register(a.Speaker(), a)
	}
	return r
}

// Register adds (or overwrites) an agent under the given speaker name.
// Empty speaker / nil agent are no-ops. The first registration of a
// speaker establishes its insertion order; later registrations overwrite
// the stored Agent but do not move the speaker in the order list.
func (r *Router) Register(speaker string, agent *Agent) {
	if speaker == "" || agent == nil {
		return
	}
	key := normalizeSpeaker(speaker)

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.agents[key]; !exists {
		r.order = append(r.order, speaker)
	}
	r.agents[key] = agent
	// Wire the back-reference so the agent can issue consult_npc calls.
	agent.attachRouter(r)
}

// LookupAgent returns the agent registered under speaker (case-insensitive)
// or nil when absent. Exported so other in-package files (delegate plumbing)
// can reach across the registry without grabbing the lock by hand.
func (r *Router) LookupAgent(speaker string) *Agent {
	if speaker == "" {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.agents[normalizeSpeaker(speaker)]
}

// ConsultAgent dispatches a delegated question from `from` to `to` and
// returns the consulted agent's reply.
//
// The call is bounded by both:
//   - MaxDelegateDepth (carried in ctx via withChain), and
//   - DefaultDelegateTimeout (per-call hard cap, even when ctx has no deadline).
//
// Returns a graceful error (one of ErrDelegate*) on guard-rail violations
// so the caller can surface a soft fallback to its decision layer.
func (r *Router) ConsultAgent(ctx context.Context, from, to, question, contextHint string) (*DelegateResponse, error) {
	if r == nil {
		return nil, ErrDelegateNoRouter
	}
	if to == "" || question == "" {
		return nil, fmt.Errorf("consult: missing target or question")
	}

	chain := chainFromContext(ctx)
	if len(chain) >= MaxDelegateDepth {
		return nil, ErrDelegateMaxDepth
	}
	// `to` cannot already be on the chain (cycle). Also reject delegating to
	// the asker itself.
	if normalizeSpeaker(to) == normalizeSpeaker(from) || containsCI(chain, to) {
		return nil, ErrDelegateCycle
	}

	target := r.LookupAgent(to)
	if target == nil {
		return nil, ErrDelegateUnknownTarget
	}

	// Append `from` to the chain — the consulted agent now sees the full
	// ancestry leading up to it. (Not `to`, because `to` is the *receiver*;
	// once it executes, any nested consult it issues will have `to` itself
	// as `from`, which gets appended in this same code path.)
	childCtx, cancel := context.WithTimeout(withChain(ctx, from), DefaultDelegateTimeout)
	defer cancel()

	resp, err := target.HandleInternalQuery(childCtx, InternalQuery{
		FromAgent: from,
		Question:  question,
		Context:   contextHint,
	})
	if err != nil {
		return nil, fmt.Errorf("consult %s: %w", to, err)
	}
	return &DelegateResponse{
		Answer:    resp.Answer,
		Consulted: target.Speaker(),
		ToolsUsed: resp.ToolsUsed,
	}, nil
}

// Agents returns the registered agents in insertion order. Useful for
// wiring shared resources (e.g. iterating SetSession on each one).
func (r *Router) Agents() []*Agent {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Agent, 0, len(r.order))
	for _, name := range r.order {
		if a, ok := r.agents[normalizeSpeaker(name)]; ok {
			out = append(out, a)
		}
	}
	return out
}

// Speakers returns the list of registered speakers in insertion order.
func (r *Router) Speakers() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, len(r.order))
	copy(out, r.order)
	return out
}

// SetGroupHandler injects the group chat orchestrator. When set, events
// named "group_create" and "group_message" are routed to it, and
// chat_received messages while a group is active go to the group instead
// of the lastActive single agent.
func (r *Router) SetGroupHandler(h GroupHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.groupHandler = h
}

// ActiveGroupID returns the currently active group (empty if none).
func (r *Router) ActiveGroupID() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.activeGroupID
}

// SetSession wires the same MCP session into every registered agent. Call
// after mcpclient.Spawn completes so all agents can invoke tools.
func (r *Router) SetSession(session *mcp.ClientSession) {
	for _, a := range r.Agents() {
		a.SetSession(session)
	}
}

// LoadTools loads the MCP tool catalogue into every registered agent. Any
// per-agent failure is returned immediately — callers that want best-effort
// loading should loop over r.Agents() themselves.
func (r *Router) LoadTools(ctx context.Context) error {
	for _, a := range r.Agents() {
		if err := a.LoadTools(ctx); err != nil {
			return err
		}
	}
	return nil
}

// HandleNotification returns a handler suitable for mcp.ClientOptions.
// LoggingMessageHandler. It inspects each incoming event and dispatches to
// the target agent's respondAndSay, or drops the event when no agent
// matches.
//
// Special case: when exactly one agent is registered we delegate to its
// own handler so the single-NPC behaviour (including the "accept untargeted
// chat_received" semantics) is byte-for-byte identical to the pre-Router
// path. This lets --persona-only callers keep working without surprises.
func (r *Router) HandleNotification() func(context.Context, *mcp.LoggingMessageRequest) {
	return func(ctx context.Context, req *mcp.LoggingMessageRequest) {
		if req == nil || req.Params == nil {
			return
		}

		// Fast-path for the 1-agent case — delegate to the agent's own
		// handler so legacy behaviour is preserved. We check under the
		// read lock so concurrent Register calls can't make us read a
		// half-updated map.
		r.mu.RLock()
		soleAgent, single := r.singleAgentLocked()
		r.mu.RUnlock()
		if single {
			soleAgent.HandleNotification()(ctx, req)
			return
		}

		logger := r.anyLogger()

		// ── Group chat events ──────────────────────────────────────────
		// Intercept group_create and group_message before single-NPC routing.
		if participants, ok := extractGroupCreate(req); ok {
			r.mu.RLock()
			gh := r.groupHandler
			r.mu.RUnlock()
			if gh == nil {
				if logger != nil {
					logger.Debug("group_create dropped: no group handler")
				}
				return
			}
			groupID, err := gh.CreateGroup(participants)
			if err != nil {
				if logger != nil {
					logger.Info("group_create failed", "err", err)
				}
				return
			}
			r.mu.Lock()
			r.activeGroupID = groupID
			r.mu.Unlock()
			if logger != nil {
				logger.Info("group created", "group_id", groupID, "participants", participants)
			}
			return
		}
		if groupID, text, ok := extractGroupMessage(req); ok {
			r.mu.RLock()
			gh := r.groupHandler
			r.mu.RUnlock()
			if gh == nil {
				return
			}
			gh.OnPlayerMessage(ctx, groupID, text)
			return
		}

		// chat_message carries an explicit npc field — primary routing key.
		if npc, text, ok := extractChatMessage(req); ok {
			r.dispatch(npc, text, "chat_message")
			return
		}

		// npc_interact also carries an npc field.
		if npc, ok := extractNpcInteract(req); ok {
			r.dispatch(npc, "[玩家走过来点击了你，主动和你打招呼。请用符合你人设的方式自然地回应。]", "npc_interact")
			return
		}

		// chat_received is untargeted. The player can be typing in either:
		//   source="player"       → Ctrl+T global chat box → lastActive NPC
		//   source="player_group" → group-chat panel       → group orchestrator
		// Routing is driven by source, NOT by activeGroupID, so an active
		// group can't siphon messages that were typed into the Ctrl+T box.
		if text, source, ok := extractChatReceivedRaw(req); ok {
			r.mu.RLock()
			gh := r.groupHandler
			gid := r.activeGroupID
			last := r.lastActive
			r.mu.RUnlock()

			if source == "player_group" {
				if gh == nil || gid == "" {
					if logger != nil {
						logger.Debug("player_group chat_received dropped: no active group", "text", text)
					}
					return
				}
				gh.OnPlayerMessage(ctx, gid, text)
				return
			}

			// Non-group source → standard per-NPC routing by lastActive.
			if last == "" {
				if logger != nil {
					logger.Debug("chat_received dropped: no active speaker yet", "text", text)
				}
				return
			}
			r.dispatch(last, text, "chat_received")
			return
		}
	}
}

// singleAgentLocked returns the sole agent + true when exactly one is
// registered. Caller must hold the read lock.
func (r *Router) singleAgentLocked() (*Agent, bool) {
	if len(r.agents) != 1 {
		return nil, false
	}
	for _, a := range r.agents {
		return a, true
	}
	return nil, false
}

// dispatch sends the message to the matching agent and updates lastActive.
// Unknown speakers are logged and dropped.
func (r *Router) dispatch(speaker, text, source string) {
	r.mu.RLock()
	a, ok := r.agents[normalizeSpeaker(speaker)]
	known := append([]string(nil), r.order...)
	r.mu.RUnlock()
	if !ok {
		if logger := r.anyLogger(); logger != nil {
			logger.Debug("router: unknown speaker, dropping event",
				"speaker", speaker, "source", source, "known", known)
		}
		return
	}
	r.mu.Lock()
	r.lastActive = speaker
	r.mu.Unlock()
	a.cfg.Logger.Info("router dispatch", "speaker", speaker, "source", source)
	go a.respondAndSay(text)
}

// anyLogger returns one of the managed agents' loggers so router-level
// messages go through the same slog pipeline. Returns nil when empty.
func (r *Router) anyLogger() interface {
	Debug(string, ...any)
	Info(string, ...any)
} {
	for _, a := range r.Agents() {
		if a.cfg.Logger != nil {
			return a.cfg.Logger
		}
	}
	return nil
}

// normalizeSpeaker canonicalises speaker names for map lookup. We match
// case-insensitively because Stardew's internal names are mixed-case
// (Abigail, Harvey, XiaMi) and dialog UIs sometimes normalise them.
func normalizeSpeaker(name string) string {
	// byte-wise ASCII fold — NPC internal names are ASCII in vanilla SDV
	// and the custom "XiaMi" also fits. Keeping this dependency-free.
	b := make([]byte, len(name))
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		b[i] = c
	}
	return string(b)
}

// ── group event extractors ────────────────────────────────────────────────

// extractGroupCreate parses a group_create event:
// {kind:"stardew/event", name:"group_create", data:{participants:["Abigail","Sebastian"]}}
func extractGroupCreate(req *mcp.LoggingMessageRequest) ([]string, bool) {
	if req == nil || req.Params == nil {
		return nil, false
	}
	m, ok := req.Params.Data.(map[string]any)
	if !ok {
		return nil, false
	}
	if m["kind"] != "stardew/event" || m["name"] != "group_create" {
		return nil, false
	}
	raw, ok := m["data"]
	if !ok {
		return nil, false
	}
	data, ok := raw.(map[string]any)
	if !ok {
		return nil, false
	}
	rawParticipants, ok := data["participants"]
	if !ok {
		return nil, false
	}
	// participants can be []any from JSON unmarshal.
	switch v := rawParticipants.(type) {
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		if len(out) == 0 {
			return nil, false
		}
		return out, true
	case []string:
		if len(v) == 0 {
			return nil, false
		}
		return v, true
	}
	return nil, false
}

// extractGroupMessage parses a group_message event:
// {kind:"stardew/event", name:"group_message", data:{group_id:"xxx", text:"hello"}}
func extractGroupMessage(req *mcp.LoggingMessageRequest) (groupID string, text string, ok bool) {
	if req == nil || req.Params == nil {
		return "", "", false
	}
	m, mOk := req.Params.Data.(map[string]any)
	if !mOk {
		return "", "", false
	}
	if m["kind"] != "stardew/event" || m["name"] != "group_message" {
		return "", "", false
	}
	raw, rawOk := m["data"]
	if !rawOk {
		return "", "", false
	}
	data, dOk := raw.(map[string]any)
	if !dOk {
		return "", "", false
	}
	gid, _ := data["group_id"].(string)
	txt, _ := data["text"].(string)
	if gid == "" || txt == "" {
		return "", "", false
	}
	return gid, txt, true
}
