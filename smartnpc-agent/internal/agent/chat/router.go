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
	"encoding/json"
	"fmt"
	"sync"
	"time"

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
	// groupSessions holds live, persistent group chat sessions keyed by
	// session ID. Unlike the old transient GroupChatSession (single round,
	// stack-scoped), entries here survive across HandleGroupMessage calls
	// so subsequent player turns keep the same participants + history.
	groupSessions map[string]*GroupSession
}

// NewRouter returns an empty Router. Use Register to add agents.
//
// For convenience at call sites that already have a slice of agents,
// NewRouterFromAgents registers them in one call.
func NewRouter() *Router {
	return &Router{
		agents:        make(map[string]*Agent),
		groupSessions: make(map[string]*GroupSession),
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

		// npc_encounter: two agent NPCs met in the same map. Trigger a
		// one-shot memory-sharing exchange via npc_send_message (npcA → npcB).
		if npcA, npcB, ok := extractNpcEncounter(req); ok {
			if logger != nil {
				logger.Info("npc_encounter", "npc_a", npcA, "npc_b", npcB)
			}
			r.handleEncounter(npcA, npcB)
			return
		}

		// group_chat_message: player addressed multiple NPCs at once. We run
		// the group round asynchronously so the notification handler returns
		// promptly — RunGroupChat itself drives sequential replies + chat_say
		// per participant with the configured inter-NPC delay.
		//
		// NOTE: legacy event kept for backward-compat with the original
		// transient single-round API. New code should emit the explicit
		// group_create / group_message / group_close trio instead.
		if participants, text, ok := extractGroupChatMessage(req); ok {
			if logger != nil {
				logger.Info("group_chat_message",
					"participants", participants, "text", text)
			}
			go r.RunGroupChat(ctx, participants, text)
			return
		}

		// group_create: explicit session creation. Idempotent — re-creating
		// the same group_id resets participants and clears history (see
		// CreateGroupSession). No NPC replies are triggered here; the
		// session just sits ready for the next group_message.
		if groupID, participants, ok := extractGroupCreate(req); ok {
			if logger != nil {
				logger.Info("group_create",
					"group_id", groupID, "participants", participants)
			}
			r.CreateGroupSession(groupID, participants)
			return
		}

		// group_message: player utterance targeting an existing session.
		// Runs two rounds asynchronously so the handler returns promptly.
		// Unknown group IDs log a warning inside HandleGroupMessage.
		if groupID, text, ok := extractGroupMessage(req); ok {
			if logger != nil {
				logger.Info("group_message",
					"group_id", groupID, "text", text)
			}
			go r.HandleGroupMessage(groupID, text)
			return
		}

		// group_invite: add an NPC to an existing session mid-conversation.
		// Errors (unknown session, empty npc) are logged; no partial state.
		if groupID, npc, ok := extractGroupInvite(req); ok {
			if logger != nil {
				logger.Info("group_invite", "group_id", groupID, "npc", npc)
			}
			if err := r.AddGroupParticipant(groupID, npc); err != nil {
				if logger != nil {
					logger.Warn("group_invite failed",
						"group_id", groupID, "npc", npc, "err", err)
				}
			}
			return
		}

		// group_kick: remove an NPC from an existing session. Symmetric
		// with group_invite — errors logged, no retry.
		if groupID, npc, ok := extractGroupKick(req); ok {
			if logger != nil {
				logger.Info("group_kick", "group_id", groupID, "npc", npc)
			}
			if err := r.RemoveGroupParticipant(groupID, npc); err != nil {
				if logger != nil {
					logger.Warn("group_kick failed",
						"group_id", groupID, "npc", npc, "err", err)
				}
			}
			return
		}

		// group_close: tear down an existing session. Idempotent.
		if groupID, ok := extractGroupClose(req); ok {
			if logger != nil {
				logger.Info("group_close", "group_id", groupID)
			}
			r.CloseGroupSession(groupID)
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
	Warn(string, ...any)
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

// extractNpcEncounter parses an npc_encounter event from the MCP notification.
// Returns the two NPC names on success.
func extractNpcEncounter(req *mcp.LoggingMessageRequest) (npcA, npcB string, ok bool) {
	if req == nil || req.Params == nil {
		return "", "", false
	}
	m, mOk := req.Params.Data.(map[string]any)
	if !mOk {
		return "", "", false
	}
	if m["kind"] != "stardew/event" || m["name"] != "npc_encounter" {
		return "", "", false
	}
	raw, rawOk := m["data"]
	if !rawOk {
		return "", "", false
	}
	var inner struct {
		NpcA string `json:"npc_a"`
		NpcB string `json:"npc_b"`
	}
	switch v := raw.(type) {
	case map[string]any:
		inner.NpcA, _ = v["npc_a"].(string)
		inner.NpcB, _ = v["npc_b"].(string)
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return "", "", false
		}
		_ = json.Unmarshal(b, &inner)
	}
	if inner.NpcA == "" || inner.NpcB == "" {
		return "", "", false
	}
	return inner.NpcA, inner.NpcB, true
}

// handleEncounter triggers a one-shot memory-sharing exchange between two NPCs
// that met on the same map. NPC A sends a brief message to NPC B — no reply
// expected (npcReplyMode blocks the return path anyway).
//
// Skipped when NPC A is currently mid-conversation (recently received a player
// message within 30s) so encounters don't interrupt an active chat.
func (r *Router) handleEncounter(npcA, npcB string) {
	r.mu.RLock()
	agentA := r.agents[normalizeSpeaker(npcA)]
	r.mu.RUnlock()
	if agentA == nil {
		return
	}

	// Don't interrupt an active player conversation.
	if agentA.recentlyActive(30 * time.Second) {
		agentA.cfg.Logger.Debug("encounter skipped: agent recently active",
			"speaker", npcA, "other", npcB)
		return
	}

	// Inject a system message as if npcA decided to greet npcB.
	// The message goes through respondAndSay which will run the decision
	// stage — it may call npc_send_message to npcB (allowed because this
	// is NOT npcReplyMode). NpcB receives it and responds via chat_say only.
	encounter := fmt.Sprintf("[系统提示 — NPC 相遇]\n你刚好遇到了 %s。如果你想打招呼或分享最近发生的事，用 npc_send_message 跟对方说一句话（简短1-2句）。如果不想搭话，回复 idle。", npcB)
	go agentA.respondAndSay(encounter)
}

// extractGroupChatMessage parses a group_chat_message event. The payload shape
// mirrors the C# DTO: { participants: ["A","B",...], text: "...", source: "player" }.
// Participants with empty strings are filtered out so a single typo in the
// command parser cannot inject a phantom speaker into RunGroupChat.
func extractGroupChatMessage(req *mcp.LoggingMessageRequest) (participants []string, text string, ok bool) {
	if req == nil || req.Params == nil {
		return nil, "", false
	}
	m, mOk := req.Params.Data.(map[string]any)
	if !mOk {
		return nil, "", false
	}
	if m["kind"] != "stardew/event" || m["name"] != "group_chat_message" {
		return nil, "", false
	}
	raw, rawOk := m["data"]
	if !rawOk {
		return nil, "", false
	}
	var inner struct {
		Participants []string `json:"participants"`
		Text         string   `json:"text"`
		Source       string   `json:"source"`
	}
	switch v := raw.(type) {
	case map[string]any:
		if t, k := v["text"].(string); k {
			inner.Text = t
		}
		if s, k := v["source"].(string); k {
			inner.Source = s
		}
		if ps, k := v["participants"].([]any); k {
			for _, p := range ps {
				if name, nk := p.(string); nk && name != "" {
					inner.Participants = append(inner.Participants, name)
				}
			}
		}
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return nil, "", false
		}
		if err := json.Unmarshal(b, &inner); err != nil {
			return nil, "", false
		}
	}
	// Drop any empty entries that may have slipped through the json path.
	cleaned := inner.Participants[:0]
	for _, p := range inner.Participants {
		if p != "" {
			cleaned = append(cleaned, p)
		}
	}
	if len(cleaned) == 0 || inner.Text == "" {
		return nil, "", false
	}
	return cleaned, inner.Text, true
}

// extractGroupEventData is a shared helper for the group_* lifecycle
// events: it validates the envelope (kind=stardew/event, name matches)
// and returns the inner `data` map for the caller to pick fields out of.
// Centralised so each event-specific extractor is a short wrapper and
// the envelope-validation logic lives in one place.
func extractGroupEventData(req *mcp.LoggingMessageRequest, eventName string) (map[string]any, bool) {
	if req == nil || req.Params == nil {
		return nil, false
	}
	m, mOk := req.Params.Data.(map[string]any)
	if !mOk {
		return nil, false
	}
	if m["kind"] != "stardew/event" || m["name"] != eventName {
		return nil, false
	}
	raw, rawOk := m["data"]
	if !rawOk {
		return nil, false
	}
	// Accept either a native map (common path from SMAPI JSON → any) or a
	// json.RawMessage / arbitrary value that we reserialise. The nil case
	// below is defensive against upstream quirks.
	switch v := raw.(type) {
	case map[string]any:
		return v, true
	case json.RawMessage:
		var inner map[string]any
		if err := json.Unmarshal(v, &inner); err != nil {
			return nil, false
		}
		return inner, true
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return nil, false
		}
		var inner map[string]any
		if err := json.Unmarshal(b, &inner); err != nil {
			return nil, false
		}
		return inner, true
	}
}

// extractGroupCreate parses a group_create event:
//
//	{"data":{"group_id":"grp_xxx","participants":["XiaMi","Abigail"]}}
//
// Empty participant names are filtered so a stray comma in the UI can't
// inject a phantom speaker. Returns ok=false when group_id is missing or
// no non-empty participants remain.
func extractGroupCreate(req *mcp.LoggingMessageRequest) (groupID string, participants []string, ok bool) {
	data, dOk := extractGroupEventData(req, "group_create")
	if !dOk {
		return "", nil, false
	}
	id, _ := data["group_id"].(string)
	if id == "" {
		return "", nil, false
	}
	var ps []string
	switch raw := data["participants"].(type) {
	case []any:
		for _, p := range raw {
			if name, nk := p.(string); nk && name != "" {
				ps = append(ps, name)
			}
		}
	case []string:
		for _, name := range raw {
			if name != "" {
				ps = append(ps, name)
			}
		}
	}
	if len(ps) == 0 {
		return "", nil, false
	}
	return id, ps, true
}

// extractGroupMessage parses a group_message event:
//
//	{"data":{"group_id":"grp_xxx","text":"...","source":"player"}}
//
// source is accepted but not inspected — the router treats every
// group_message as a player turn (NPC-originated messages go through
// chat_say + npc_send_message, not this path).
func extractGroupMessage(req *mcp.LoggingMessageRequest) (groupID string, text string, ok bool) {
	data, dOk := extractGroupEventData(req, "group_message")
	if !dOk {
		return "", "", false
	}
	id, _ := data["group_id"].(string)
	t, _ := data["text"].(string)
	if id == "" || t == "" {
		return "", "", false
	}
	return id, t, true
}

// extractGroupInvite parses a group_invite event:
//
//	{"data":{"group_id":"grp_xxx","npc":"Sebastian"}}
func extractGroupInvite(req *mcp.LoggingMessageRequest) (groupID string, npc string, ok bool) {
	return extractGroupNpcOp(req, "group_invite")
}

// extractGroupKick parses a group_kick event:
//
//	{"data":{"group_id":"grp_xxx","npc":"Sebastian"}}
func extractGroupKick(req *mcp.LoggingMessageRequest) (groupID string, npc string, ok bool) {
	return extractGroupNpcOp(req, "group_kick")
}

// extractGroupNpcOp is the shared parser for the invite/kick pair since
// they share a payload shape. Kept unexported so callers go through the
// named wrappers above.
func extractGroupNpcOp(req *mcp.LoggingMessageRequest, eventName string) (groupID string, npc string, ok bool) {
	data, dOk := extractGroupEventData(req, eventName)
	if !dOk {
		return "", "", false
	}
	id, _ := data["group_id"].(string)
	n, _ := data["npc"].(string)
	if id == "" || n == "" {
		return "", "", false
	}
	return id, n, true
}

// extractGroupClose parses a group_close event:
//
//	{"data":{"group_id":"grp_xxx"}}
func extractGroupClose(req *mcp.LoggingMessageRequest) (groupID string, ok bool) {
	data, dOk := extractGroupEventData(req, "group_close")
	if !dOk {
		return "", false
	}
	id, _ := data["group_id"].(string)
	if id == "" {
		return "", false
	}
	return id, true
}
