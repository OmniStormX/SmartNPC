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

// SetSession wires the same MCP session into every registered agent. Call
// after mcpclient.Spawn completes so all agents can invoke tools.
func (r *Router) SetSession(session *mcp.ClientSession) {
	for _, a := range r.Agents() {
		a.SetSession(session)
	}
}

// WireAgentRouters sets a back-reference to this Router on every registered
// agent so they can use the npc_send_message local tool to communicate with
// each other. Call after all agents are registered.
func (r *Router) WireAgentRouters() {
	for _, a := range r.Agents() {
		a.SetRouter(r)
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

		// chat_received is untargeted (player typing in the global box).
		// Route to the most recently interacted agent when one exists;
		// otherwise drop — broadcasting would wake every NPC at once.
		if text, ok := extractChatReceivedText(req); ok {
			r.mu.RLock()
			last := r.lastActive
			r.mu.RUnlock()
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

// GetAgent returns the Agent registered under the given speaker name, or nil
// if no such agent exists. Used by the inter-NPC messaging system: when Agent
// A calls npc_send_message targeting Agent B, the local tool handler resolves
// the recipient via this method.
func (r *Router) GetAgent(speaker string) *Agent {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.agents[normalizeSpeaker(speaker)]
}

// DeliverNPCMessage delivers a message from one NPC agent to another. The
// message is injected into the recipient's history as a system-tagged entry
// and triggers an asynchronous respond-and-say cycle so the recipient reacts
// to the incoming NPC message naturally.
//
// Returns false if the recipient is not registered.
func (r *Router) DeliverNPCMessage(fromNPC, toNPC, message string) bool {
	recipient := r.GetAgent(toNPC)
	if recipient == nil {
		return false
	}
	recipient.ReceiveNPCMessage(fromNPC, message)
	return true
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
