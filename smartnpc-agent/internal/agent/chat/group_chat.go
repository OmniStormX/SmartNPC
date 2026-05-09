// Group chat orchestration.
//
// A GroupSession represents a persistent, player-initiated multi-NPC
// conversation: it survives across player turns so participants accumulate
// shared history, and a turn counter bounds the number of replies per
// player message. Lifecycle is explicit:
//
//	CreateGroupSession(id, participants) → HandleGroupMessage(id, text) × N → CloseGroupSession(id)
//
// Each HandleGroupMessage call runs up to two rounds:
//   - Round 1: every participant responds once, seeing the player message
//     + prior in-round replies.
//   - Round 2: every participant may respond again, seeing Round 1's full
//     transcript, and is told to reply "idle" if they have nothing new.
//     Idle replies are dropped (not persisted, not sent to the game).
//
// The per-call TurnCount is reset at the top of HandleGroupMessage and
// capped at len(participants)*2 so a misbehaving persona can't emit an
// unbounded reply storm. The session History keeps the most recent 50
// messages (player + NPC) so prompts stay bounded over long sessions.
//
// Legacy RunGroupChat is preserved as a thin wrapper: Create → Handle →
// Close. Existing single-round tests and the old HandleNotification path
// keep working unchanged.

package chat

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// groupChatReplyDelay is the inter-NPC pause so the player can read each line
// and so the underlying chat_say writes serialize cleanly on the mod side.
// Exposed as a var (not const) so tests can shrink it to zero without sleeping.
var groupChatReplyDelay = 1500 * time.Millisecond

// groupChatRespondTimeout caps a single participant's LLM round-trip inside a
// group chat round. Independent of Agent.cfg.Timeout so a slow model for one
// NPC does not stall the whole group. Set to 90s to accommodate the dual-LLM
// pipeline (decision + persona via Hermes).
const groupChatRespondTimeout = 90 * time.Second

// groupChatOverallTimeout caps a single HandleGroupMessage call (both rounds).
// Derived from context.Background so a short-lived notification ctx can't
// cancel the session mid-round.
const groupChatOverallTimeout = 3 * time.Minute

// groupHistoryCap bounds session history to keep prompts from growing
// without limit across many player turns. When exceeded we trim from the
// front, preserving the most recent entries (typical LLM-friendly tail).
const groupHistoryCap = 50

// GroupSession is a persistent, player-initiated multi-NPC conversation.
// It accumulates history across player turns and is looked up by ID.
type GroupSession struct {
	ID           string
	Participants []string
	History      []GroupMessage
	TurnCount    int
	Active       bool
	mu           sync.Mutex
}

// GroupMessage is one utterance inside a group chat. Speaker "player" denotes
// the human-side input; any other value names an NPC.
type GroupMessage struct {
	Speaker string
	Text    string
}

// GroupChatSession is retained as a type alias to keep older external
// callers/tests compiling. New code should use GroupSession directly.
type GroupChatSession = GroupSession

// CreateGroupSession registers a new persistent session. If a session with
// the same ID already exists, its participants are replaced and history is
// cleared so repeated Create calls behave as a reset. The returned pointer
// is owned by the Router; callers must not mutate fields directly.
func (r *Router) CreateGroupSession(id string, participants []string) *GroupSession {
	s := &GroupSession{
		ID:           id,
		Participants: append([]string(nil), participants...),
		Active:       true,
	}
	r.mu.Lock()
	r.groupSessions[id] = s
	r.mu.Unlock()
	return s
}

// CloseGroupSession removes a session by ID. Safe to call on an unknown ID.
// After close, the session is marked inactive so any in-flight goroutine
// holding the pointer can bail out cooperatively.
func (r *Router) CloseGroupSession(id string) {
	r.mu.Lock()
	if s, ok := r.groupSessions[id]; ok {
		s.mu.Lock()
		s.Active = false
		s.mu.Unlock()
		delete(r.groupSessions, id)
	}
	r.mu.Unlock()
}

// GetGroupSession returns the session by ID, or nil if not found.
func (r *Router) GetGroupSession(id string) *GroupSession {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.groupSessions[id]
}

// AddGroupParticipant adds an NPC to an existing session. Returns an error
// when the session is unknown. Duplicate adds are no-ops.
func (r *Router) AddGroupParticipant(id, npc string) error {
	if npc == "" {
		return fmt.Errorf("group_session: empty npc name")
	}
	r.mu.RLock()
	s, ok := r.groupSessions[id]
	r.mu.RUnlock()
	if !ok {
		return fmt.Errorf("group_session: unknown id %q", id)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.Participants {
		if p == npc {
			return nil
		}
	}
	s.Participants = append(s.Participants, npc)
	return nil
}

// RemoveGroupParticipant drops an NPC from an existing session. Returns an
// error when the session is unknown. Removing an absent NPC is a no-op.
func (r *Router) RemoveGroupParticipant(id, npc string) error {
	r.mu.RLock()
	s, ok := r.groupSessions[id]
	r.mu.RUnlock()
	if !ok {
		return fmt.Errorf("group_session: unknown id %q", id)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.Participants[:0]
	for _, p := range s.Participants {
		if p != npc {
			out = append(out, p)
		}
	}
	s.Participants = out
	return nil
}

// HandleGroupMessage drives one player turn on a persistent session: two
// rounds of NPC replies, bounded by len(participants)*2, with each reply
// dispatched to the game via chat_say (carrying group_id so the mod can
// render it in the group panel).
//
// Uses context.Background() so a short-lived notification context cannot
// cancel the round. Returns the slice of replies produced during this call
// (excluding the player message). Unknown / idle / failed participants are
// silently skipped — the same contract as RunGroupChat.
func (r *Router) HandleGroupMessage(groupID, playerText string) []GroupMessage {
	return r.runGroupRounds(groupID, playerText, 2)
}

// runGroupRounds is the shared core for HandleGroupMessage (2 rounds) and
// the legacy RunGroupChat shim (1 round). maxRounds caps the number of
// replies per participant; len(participants)*maxRounds caps the total
// TurnCount.
func (r *Router) runGroupRounds(groupID, playerText string, maxRounds int) []GroupMessage {
	session := r.GetGroupSession(groupID)
	if session == nil {
		if logger := r.anyLogger(); logger != nil {
			logger.Info("group_chat: session not found, dropping message",
				"group_id", groupID)
		}
		return nil
	}

	groupCtx, groupCancel := context.WithTimeout(context.Background(), groupChatOverallTimeout)
	defer groupCancel()

	// Snapshot participants under the session lock so a concurrent
	// Add/Remove can't reshape the slice mid-round.
	session.mu.Lock()
	session.TurnCount = 0
	participants := append([]string(nil), session.Participants...)
	session.History = appendGroupHistory(session.History, GroupMessage{Speaker: "player", Text: playerText})
	session.mu.Unlock()

	maxTurns := len(participants) * maxRounds
	var replies []GroupMessage

	// Run the rounds. The outer loop lets us break cleanly when the turn
	// cap is hit mid-round.
rounds:
	for round := 1; round <= maxRounds; round++ {
		for _, name := range participants {
			// Bail early when the turn cap is reached (defense-in-depth
			// against an upstream bug registering way more participants
			// than expected).
			session.mu.Lock()
			done := session.TurnCount >= maxTurns
			historySnapshot := append([]GroupMessage(nil), session.History...)
			session.mu.Unlock()
			if done {
				break rounds
			}

			agent := r.GetAgent(name)
			if agent == nil {
				if logger := r.anyLogger(); logger != nil {
					logger.Debug("group_chat: unknown participant, skipping",
						"speaker", name, "group_id", groupID)
				}
				continue
			}

			prompt := buildGroupChatPrompt(playerText, historySnapshot, name, round)

			agent.cfg.Logger.Info("group_chat: calling respond",
				"speaker", name, "group_id", groupID, "round", round)
			replyCtx, cancel := context.WithTimeout(groupCtx, groupChatRespondTimeout)
			reply, err := agent.respond(replyCtx, prompt)
			cancel()
			if err != nil {
				agent.cfg.Logger.Warn("group_chat: respond failed, skipping participant",
					"speaker", name, "group_id", groupID, "err", err)
				continue
			}
			if isIdleReply(reply) {
				agent.cfg.Logger.Info("group_chat: participant idle, skipping",
					"speaker", name, "group_id", groupID, "round", round)
				continue
			}

			msg := GroupMessage{Speaker: name, Text: reply}
			session.mu.Lock()
			session.History = appendGroupHistory(session.History, msg)
			session.TurnCount++
			session.mu.Unlock()
			replies = append(replies, msg)

			// Dispatch via chat_say. group_id lets the mod render the line
			// in the correct panel / suppress the one-on-one dialog path.
			agent.mu.Lock()
			s := agent.session
			agent.mu.Unlock()
			if s != nil {
				if _, sayErr := s.CallTool(groupCtx, &mcp.CallToolParams{
					Name: "chat_say",
					Arguments: map[string]any{
						"speaker":  name,
						"text":     reply,
						"group_id": session.ID,
					},
				}); sayErr != nil {
					agent.cfg.Logger.Warn("group_chat: chat_say failed",
						"speaker", name, "group_id", groupID, "err", sayErr)
				}
			} else {
				agent.cfg.Logger.Warn("group_chat: session not ready, reply not sent",
					"speaker", name, "group_id", groupID)
			}

			// Pause between NPCs so the player can read each dialog box.
			select {
			case <-groupCtx.Done():
				return replies
			case <-time.After(groupChatReplyDelay):
			}
		}
	}

	return replies
}

// RunGroupChat is a compatibility shim: it creates a transient session,
// runs a single round via runGroupRounds, and closes the session. New
// callers should use the explicit lifecycle API (CreateGroupSession +
// HandleGroupMessage); this wrapper is kept so pre-existing tests and the
// HandleNotification group_chat_message branch keep compiling and
// exhibiting the old single-round semantics.
func (r *Router) RunGroupChat(_ context.Context, participants []string, playerText string) []GroupMessage {
	id := fmt.Sprintf("transient-%d", time.Now().UnixNano())
	r.CreateGroupSession(id, participants)
	defer r.CloseGroupSession(id)
	return r.runGroupRounds(id, playerText, 1)
}

// buildGroupChatPrompt constructs the prompt for each NPC in the group chat.
// The [群聊] tag is the cue that this turn is multi-party — it also lands in
// the NPC's one-on-one history via agent.respond so future private turns
// can still spot the group context. The round parameter toggles the
// second-round "reply idle if nothing to add" guidance.
func buildGroupChatPrompt(playerText string, history []GroupMessage, currentSpeaker string, round int) string {
	// Filter the player message out of the "other NPCs said" block — it's
	// already surfaced verbatim at the top of the prompt. Also omit the
	// currentSpeaker's own earlier lines so they don't get echoed back.
	var prior []GroupMessage
	for _, m := range history {
		if m.Speaker == "player" || m.Speaker == currentSpeaker {
			continue
		}
		prior = append(prior, m)
	}

	var sb strings.Builder
	sb.WriteString("[群聊] 玩家说：")
	sb.WriteString(playerText)
	sb.WriteString("\n")

	if len(prior) > 0 {
		sb.WriteString("\n其他NPC已经说了：\n")
		for _, msg := range prior {
			sb.WriteString(fmt.Sprintf("- %s：「%s」\n", msg.Speaker, msg.Text))
		}
	}

	sb.WriteString(fmt.Sprintf("\n现在轮到你（%s）发言。根据你的性格简短回复1-2句话。不要重复别人说过的内容。", currentSpeaker))
	if round >= 2 {
		sb.WriteString("第二轮发言，如果你觉得没有新的话要说，回复 idle。")
	}
	return sb.String()
}

// appendGroupHistory appends msg to history, trimming from the front when
// the cap is exceeded. Separate helper so the two append sites (player +
// NPC) use the same trimming rule.
func appendGroupHistory(history []GroupMessage, msg GroupMessage) []GroupMessage {
	history = append(history, msg)
	if len(history) > groupHistoryCap {
		// Copy the tail into a fresh slice so the dropped prefix is eligible
		// for GC (rather than lingering under the old backing array).
		trimmed := make([]GroupMessage, groupHistoryCap)
		copy(trimmed, history[len(history)-groupHistoryCap:])
		return trimmed
	}
	return history
}
