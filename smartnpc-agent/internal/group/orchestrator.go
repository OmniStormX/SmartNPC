// Group orchestrator.
//
// Orchestrator owns a registry of GroupConversations and brokers the
// "someone said something → who replies?" pipeline. It does not own NPCs;
// it speaks to them through the AgentRouter interface so it can be wired
// against either a real chat.Router or an in-memory test fake.
package group

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
)

// AgentRouter is the contract Orchestrator needs from whatever owns the
// underlying NPC agents. chat.Router implements it (see chat.PromptInGroup).
//
// PromptInGroup runs a one-shot persona-stage call on the named NPC, fed
// the orchestrator-rendered groupPrompt as the user message. Implementations
// MUST NOT mutate the agent's persistent history — group turns are isolated
// from the NPC's 1-on-1 conversation just like F3 internal queries.
//
// A successful return of "[PASS]" (case-insensitive, trimmed) signals the
// NPC chose not to speak; the orchestrator then drops the slot.
type AgentRouter interface {
	ListAgents() []string
	PromptInGroup(ctx context.Context, npcName string, groupPrompt string, lastMsg GroupMessage) (string, error)
}

// Config bundles the orchestrator's tunables. All fields have sensible
// package defaults applied at NewOrchestrator time.
type Config struct {
	Turn          TurnConfig
	MaxHistory    int           // per-group transcript cap; default 60
	MaxChainDepth int           // hard ceiling on cascade depth; default 3
	PerNPCTimeout time.Duration // PromptInGroup deadline; default 12s
	// OnNPCReply is called each time an NPC produces a non-PASS reply in a
	// group conversation. The caller typically wires this to chat_say so the
	// reply appears in-game. nil = replies are only recorded in history.
	OnNPCReply func(ctx context.Context, groupID, npcName, reply string)
}

// DefaultConfig returns the recommended orchestrator configuration.
func DefaultConfig() Config {
	return Config{
		Turn:          DefaultTurnConfig(),
		MaxHistory:    60,
		MaxChainDepth: 3,
		PerNPCTimeout: 12 * time.Second,
	}
}

// Orchestrator is the package's facade. Concurrent-safe: every public
// method acquires the internal mutex.
type Orchestrator struct {
	mu     sync.RWMutex
	groups map[string]*GroupConversation

	router  AgentRouter
	turnMgr *TurnManager
	cfg     Config
	logger  *slog.Logger

	// nowFn is overridable for deterministic tests. Production: time.Now.
	nowFn func() time.Time
	// idFn is overridable for deterministic tests. Production: uuid.NewString.
	idFn func() string
	// dispatch is the function used to actually fire one NPC reply. Tests
	// substitute a synchronous variant; production runs it as a goroutine
	// inside OnMessage so the caller isn't blocked by NPC response latency.
	dispatch func(ctx context.Context, groupID string, decision RespondentDecision, lastMsg GroupMessage)
}

// NewOrchestrator wires the orchestrator. router is required; logger may be
// nil (a discard logger is substituted). Pass an empty Config to take all
// defaults.
func NewOrchestrator(router AgentRouter, cfg Config, logger *slog.Logger) *Orchestrator {
	cfg = mergeOrchestratorDefaults(cfg)
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(discardWriter{}, &slog.HandlerOptions{Level: slog.LevelError + 1}))
	}
	o := &Orchestrator{
		groups:  make(map[string]*GroupConversation),
		router:  router,
		turnMgr: NewTurnManager(cfg.Turn, nil),
		cfg:     cfg,
		logger:  logger,
		nowFn:   time.Now,
		idFn:    uuid.NewString,
	}
	o.dispatch = o.dispatchAsync
	return o
}

// ── lifecycle ──────────────────────────────────────────────────────────────

// CreateGroup instantiates a new GroupConversation containing the given
// participant NPCs. Empty roster is allowed (room with just the player).
// Returns the new group's stable ID. Names are validated against the
// router's known agent set so a typo doesn't silently produce a dead group.
func (o *Orchestrator) CreateGroup(participants []string) (string, error) {
	known := o.knownAgents()
	for _, p := range participants {
		if p == "" {
			return "", fmt.Errorf("create group: empty participant name")
		}
		if !known[p] {
			return "", fmt.Errorf("create group: unknown agent %q", p)
		}
	}

	id := o.idFn()
	now := o.nowFn()
	g := &GroupConversation{
		ID:           id,
		Participants: append([]string(nil), participants...),
		History:      nil,
		CreatedAt:    now,
		LastActivity: now,
		MaxHistory:   o.cfg.MaxHistory,
		stats:        make(map[string]*Participant, len(participants)),
	}
	for _, name := range participants {
		g.stats[name] = &Participant{Name: name, IsActive: true}
	}

	o.mu.Lock()
	o.groups[id] = g
	o.mu.Unlock()
	o.logger.Info("group created", "id", id, "participants", participants)
	return id, nil
}

// AddParticipant adds an NPC to an existing group. No-op when the NPC is
// already present.
func (o *Orchestrator) AddParticipant(groupID, npcName string) error {
	if npcName == "" {
		return fmt.Errorf("add participant: empty name")
	}
	if !o.knownAgents()[npcName] {
		return fmt.Errorf("add participant: unknown agent %q", npcName)
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	g, ok := o.groups[groupID]
	if !ok {
		return ErrGroupNotFound
	}
	for _, p := range g.Participants {
		if p == npcName {
			// Already a member — bump active flag in case they were removed
			// then re-added.
			g.statsFor(p).IsActive = true
			return nil
		}
	}
	g.Participants = append(g.Participants, npcName)
	g.statsFor(npcName).IsActive = true
	g.LastActivity = o.nowFn()
	o.logger.Info("participant added", "group", groupID, "npc", npcName)
	return nil
}

// RemoveParticipant removes an NPC from a group. Their stats stay around
// (so historic messages stay attributable) but IsActive flips false.
func (o *Orchestrator) RemoveParticipant(groupID, npcName string) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	g, ok := o.groups[groupID]
	if !ok {
		return ErrGroupNotFound
	}
	for i, p := range g.Participants {
		if p == npcName {
			g.Participants = append(g.Participants[:i], g.Participants[i+1:]...)
			if s, ok := g.stats[npcName]; ok {
				s.IsActive = false
			}
			g.LastActivity = o.nowFn()
			o.logger.Info("participant removed", "group", groupID, "npc", npcName)
			return nil
		}
	}
	return fmt.Errorf("remove participant: %q not in group %q", npcName, groupID)
}

// GetGroup returns the live group object. Caller MUST NOT mutate it; treat
// the returned pointer as a read-only handle. Returns nil when absent so
// callers can do a simple existence check.
func (o *Orchestrator) GetGroup(groupID string) *GroupConversation {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.groups[groupID]
}

// ListGroups returns every active group ID. Order is unspecified.
func (o *Orchestrator) ListGroups() []string {
	o.mu.RLock()
	defer o.mu.RUnlock()
	ids := make([]string, 0, len(o.groups))
	for id := range o.groups {
		ids = append(ids, id)
	}
	return ids
}

// ── message flow ───────────────────────────────────────────────────────────

// OnMessage records a new message into the group transcript and asks the
// turn manager which NPCs (if any) should respond. Each chosen NPC is
// dispatched asynchronously after their staggered Delay. This call returns
// promptly; replies materialise as further OnMessage invocations once the
// per-NPC goroutines finish.
//
// chainDepth is forwarded to the turn manager so cascading NPC↔NPC follow-
// ups taper off; callers normally pass 0 for player-originated messages.
func (o *Orchestrator) OnMessage(ctx context.Context, groupID string, msg GroupMessage) {
	o.onMessageDepth(ctx, groupID, msg, 0)
}

// onMessageDepth is the depth-aware variant. Chain dispatches recurse here
// with chainDepth+1.
func (o *Orchestrator) onMessageDepth(ctx context.Context, groupID string, msg GroupMessage, chainDepth int) {
	if chainDepth > o.cfg.MaxChainDepth {
		o.logger.Debug("group chain depth exceeded", "group", groupID, "depth", chainDepth)
		return
	}

	if msg.Timestamp.IsZero() {
		msg.Timestamp = o.nowFn()
	}

	o.mu.Lock()
	g, ok := o.groups[groupID]
	if !ok {
		o.mu.Unlock()
		o.logger.Debug("OnMessage to unknown group", "group", groupID)
		return
	}

	g.History = append(g.History, msg)
	if len(g.History) > g.MaxHistory && g.MaxHistory > 0 {
		g.History = g.History[len(g.History)-g.MaxHistory:]
	}
	g.LastActivity = msg.Timestamp

	// Update speaker stats when the speaker is one of ours (NOT for the
	// player — they're not in g.stats).
	if msg.Speaker != SpeakerPlayer && msg.Speaker != "" {
		s := g.statsFor(msg.Speaker)
		s.LastSpoke = msg.Timestamp
		s.SpeakCount++
	}
	o.mu.Unlock()

	decisions := o.turnMgr.DetermineRespondents(g, msg, chainDepth)
	if len(decisions) == 0 {
		return
	}

	for _, d := range decisions {
		d := d
		o.dispatch(ctx, groupID, d, msg)
	}
}

// dispatchAsync is the production dispatcher: spawns a goroutine that
// honours the staggered delay, then calls runRespondent. Tests swap this
// for a synchronous variant via test-only setters.
func (o *Orchestrator) dispatchAsync(ctx context.Context, groupID string, d RespondentDecision, lastMsg GroupMessage) {
	go func() {
		if d.Delay > 0 {
			select {
			case <-time.After(d.Delay):
			case <-ctx.Done():
				return
			}
		}
		o.runRespondent(ctx, groupID, d, lastMsg)
	}()
}

// runRespondent executes one PromptInGroup call and, on a non-PASS reply,
// folds the new utterance back into the orchestrator with chainDepth + 1.
func (o *Orchestrator) runRespondent(ctx context.Context, groupID string, d RespondentDecision, lastMsg GroupMessage) {
	g := o.GetGroup(groupID)
	if g == nil {
		return
	}
	prompt := BuildGroupPrompt(d.NPC, g, lastMsg)

	callCtx, cancel := context.WithTimeout(ctx, o.cfg.PerNPCTimeout)
	defer cancel()

	reply, err := o.router.PromptInGroup(callCtx, d.NPC, prompt, lastMsg)
	if err != nil {
		o.logger.Warn("PromptInGroup failed", "group", groupID, "npc", d.NPC, "err", err)
		return
	}
	if IsPassReply(reply) {
		o.logger.Debug("npc passed", "group", groupID, "npc", d.NPC)
		return
	}

	// Notify external listener (typically chat_say to display in-game).
	if o.cfg.OnNPCReply != nil {
		o.cfg.OnNPCReply(ctx, groupID, d.NPC, reply)
	}
	o.logger.Info("group npc replied", "group", groupID, "npc", d.NPC, "reply_len", len(reply))

	// Compute the new chain depth. Look up the previous depth from the
	// dispatch context — for now we just increment per reply; the formal
	// chain is preserved by re-entering onMessageDepth with chainDepth+1.
	prevDepth := chainDepthFromContext(ctx)
	o.onMessageDepth(withChainDepth(ctx, prevDepth+1), groupID, GroupMessage{
		Speaker:   d.NPC,
		Content:   reply,
		Timestamp: o.nowFn(),
		ReplyTo:   lastMsg.Speaker,
	}, prevDepth+1)
}

// ── helpers / errors ───────────────────────────────────────────────────────

// ErrGroupNotFound is returned by lookup-style methods when the groupID is
// absent.
var ErrGroupNotFound = errors.New("group not found")

func (o *Orchestrator) knownAgents() map[string]bool {
	if o.router == nil {
		return map[string]bool{}
	}
	out := make(map[string]bool)
	for _, name := range o.router.ListAgents() {
		out[name] = true
	}
	return out
}

// chainDepthKey carries the current chain depth through ctx.WithValue so
// runRespondent can resume cascades correctly without leaking into the
// public API surface.
type chainDepthKey struct{}

func chainDepthFromContext(ctx context.Context) int {
	v, _ := ctx.Value(chainDepthKey{}).(int)
	return v
}

func withChainDepth(parent context.Context, depth int) context.Context {
	return context.WithValue(parent, chainDepthKey{}, depth)
}

func mergeOrchestratorDefaults(cfg Config) Config {
	d := DefaultConfig()
	if cfg.MaxHistory == 0 {
		cfg.MaxHistory = d.MaxHistory
	}
	if cfg.MaxChainDepth == 0 {
		cfg.MaxChainDepth = d.MaxChainDepth
	}
	if cfg.PerNPCTimeout == 0 {
		cfg.PerNPCTimeout = d.PerNPCTimeout
	}
	return cfg
}

// discardWriter swallows everything; used as the default slog target so the
// orchestrator does not spam stdout in tests that don't pass a logger.
type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }
