package memory

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// contextWithTimeout is a tiny wrapper that returns a Background-rooted
// context with the given deadline. Centralised so async extraction calls
// have one place to tweak cancellation policy.
func contextWithTimeout(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}

// DefaultIdleTimeout is the wall-clock idle threshold after which a
// conversation is auto-ended. Picked to match the milestone spec (5 min).
const DefaultIdleTimeout = 5 * time.Minute

// ConversationTracker bookkeeps the open conversation per NPC and emits
// EndConversation calls under three triggers:
//
//  1. Idle timeout (DefaultIdleTimeout / configurable).
//  2. Game-date change — when the caller reports a new game date for an NPC
//     whose open conversation was started on a different date, the previous
//     conversation is closed before a new one is opened.
//  3. Shutdown — Close() flushes every still-open conversation.
//
// Tracker is safe for concurrent use. It does not own the Store; the caller
// is responsible for closing the Store after the tracker is closed.
type ConversationTracker struct {
	store         Store
	idleTimeout   time.Duration
	logger        *slog.Logger
	summarizer    *Summarizer
	extractEveryN int
	// now is overridable so tests can drive idle expiry deterministically.
	now func() time.Time

	mu       sync.Mutex
	sessions map[string]*trackerSession // key: lowercase npc name

	// extractInFlight is the per-NPC guard preventing parallel extraction
	// goroutines from queueing up when the LLM is slower than chat cadence.
	extractInFlight map[string]bool

	closed bool
}

// trackerSession is one open conversation row plus the timestamp of the
// most recent activity, used to compute idle expiry.
type trackerSession struct {
	npcName    string // canonical (case as supplied by caller)
	convID     int64
	gameDate   string
	lastActive time.Time
	// msgCount counts user+assistant messages (tool turns excluded) since
	// the conversation was opened. Used to fire async memory extraction
	// every memoryExtractEveryN turns.
	msgCount int
}

// TrackerOptions configures a ConversationTracker.
type TrackerOptions struct {
	// IdleTimeout overrides DefaultIdleTimeout. Zero ⇒ use the default.
	// Negative ⇒ idle expiry disabled (only game-date / Close trigger).
	IdleTimeout time.Duration
	// Logger is used for trace-level events. nil ⇒ slog.Default().
	Logger *slog.Logger
	// Now is a clock injection point; nil ⇒ time.Now.
	Now func() time.Time
	// Summarizer is consulted by OnMessage every ExtractEveryN turns and
	// at conversation end to distill new long-term memories. nil disables
	// asynchronous extraction (the tracker keeps mirroring messages but
	// never calls the LLM itself).
	Summarizer *Summarizer
	// ExtractEveryN overrides the every-N-message threshold for async
	// extraction. Zero defaults to defaultExtractEveryN.
	ExtractEveryN int
}

// defaultExtractEveryN is the per-conversation message-count threshold
// (user+assistant only) at which OnMessage fires Summarizer.ExtractMemories
// asynchronously. Tool turns do not count toward the cadence.
const defaultExtractEveryN = 10

// NewTracker constructs a ConversationTracker bound to the given Store.
func NewTracker(store Store, opts TrackerOptions) (*ConversationTracker, error) {
	if store == nil {
		return nil, errors.New("memory.NewTracker: store is required")
	}
	idle := opts.IdleTimeout
	if idle == 0 {
		idle = DefaultIdleTimeout
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	extractN := opts.ExtractEveryN
	if extractN <= 0 {
		extractN = defaultExtractEveryN
	}
	return &ConversationTracker{
		store:           store,
		idleTimeout:     idle,
		logger:          logger,
		summarizer:      opts.Summarizer,
		extractEveryN:   extractN,
		now:             now,
		sessions:        make(map[string]*trackerSession),
		extractInFlight: make(map[string]bool),
	}, nil
}

// StartTurn ensures there is an open conversation for npcName at game-time
// gameDate. It returns the conversation id to be passed to AppendMessage.
//
// Behaviour:
//   - No open conversation → starts a new one.
//   - Existing conversation idle for more than IdleTimeout → ends + replaces.
//   - Existing conversation but gameDate differs → ends + replaces.
//   - Otherwise reuses the existing conversation and bumps lastActive.
func (t *ConversationTracker) StartTurn(npcName, gameDate string) (int64, error) {
	if npcName == "" {
		return 0, errors.New("ConversationTracker.StartTurn: npcName is required")
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return 0, errors.New("ConversationTracker.StartTurn: tracker closed")
	}

	key := normalizeNPCName(npcName)
	now := t.now()

	if sess, ok := t.sessions[key]; ok {
		// Idle expiry?
		if t.idleTimeout > 0 && now.Sub(sess.lastActive) >= t.idleTimeout {
			t.logger.Debug("conversation expired by idle timeout",
				"npc", sess.npcName, "conv_id", sess.convID,
				"idle", now.Sub(sess.lastActive).String())
			t.endLocked(sess)
			delete(t.sessions, key)
		} else if gameDate != "" && sess.gameDate != "" && sess.gameDate != gameDate {
			t.logger.Debug("conversation rolled over by game-date change",
				"npc", sess.npcName, "conv_id", sess.convID,
				"old_date", sess.gameDate, "new_date", gameDate)
			t.endLocked(sess)
			delete(t.sessions, key)
		} else {
			// Reuse existing.
			sess.lastActive = now
			if sess.gameDate == "" && gameDate != "" {
				sess.gameDate = gameDate
			}
			return sess.convID, nil
		}
	}

	// Open a fresh conversation.
	convID, err := t.store.StartConversation(npcName, gameDate)
	if err != nil {
		return 0, fmt.Errorf("ConversationTracker.StartTurn: %w", err)
	}
	t.sessions[key] = &trackerSession{
		npcName:    npcName,
		convID:     convID,
		gameDate:   gameDate,
		lastActive: now,
	}
	t.logger.Debug("conversation opened", "npc", npcName, "conv_id", convID, "game_date", gameDate)
	return convID, nil
}

// Touch updates the lastActive marker for an NPC's open conversation
// without altering the conversation id. Useful when something other than
// StartTurn (e.g. an inbound notification while the LLM is mid-call) should
// reset the idle clock.
//
// Returns true if there was an open conversation to touch.
func (t *ConversationTracker) Touch(npcName string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return false
	}
	sess, ok := t.sessions[normalizeNPCName(npcName)]
	if !ok {
		return false
	}
	sess.lastActive = t.now()
	return true
}

// End closes the open conversation for npcName immediately. No-op when
// nothing is open. Useful when the caller knows a conversation has ended
// (e.g. NPC walked away).
func (t *ConversationTracker) End(npcName string) error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil
	}
	key := normalizeNPCName(npcName)
	sess, ok := t.sessions[key]
	if !ok {
		t.mu.Unlock()
		return nil
	}
	delete(t.sessions, key)
	convID := sess.convID
	t.mu.Unlock()

	if t.summarizer != nil {
		// Fire-and-forget: end-of-conversation summary + extraction. Done
		// outside the lock so the LLM call cannot stall other tracker ops.
		go t.summarizeAndExtract(npcName, convID)
	}
	if err := t.store.EndConversation(convID); err != nil {
		return fmt.Errorf("ConversationTracker.End: %w", err)
	}
	t.logger.Debug("conversation ended", "npc", npcName, "conv_id", convID)
	return nil
}

// OnMessage is the one-call entrypoint chat agents use to record a single
// turn. It opens (or reuses) a conversation, mirrors the message into the
// store, bumps the per-conversation counter, and — when the threshold trips
// — fires Summarizer.ExtractMemories asynchronously.
//
// role must be one of "user", "assistant", "system", "tool". Tool turns do
// NOT count toward the extract cadence so a chatty assistant cannot
// preempt extraction triggered by genuine player exchanges.
//
// Returns the conversation id so callers can chain other store operations
// (e.g. AppendMessage with a custom timestamp). Failures are wrapped but
// never silent; callers may log and continue — chat must stay alive even
// when persistence is unavailable.
func (t *ConversationTracker) OnMessage(npcName, role, content, gameDate string) (int64, error) {
	if npcName == "" {
		return 0, errors.New("ConversationTracker.OnMessage: npcName is required")
	}
	if role == "" {
		return 0, errors.New("ConversationTracker.OnMessage: role is required")
	}

	convID, err := t.StartTurn(npcName, gameDate)
	if err != nil {
		return 0, err
	}

	if err := t.store.AppendMessage(convID, Message{
		ConversationID: convID,
		Role:           role,
		Content:        content,
		Timestamp:      t.now().UTC(),
	}); err != nil {
		return convID, fmt.Errorf("OnMessage: append: %w", err)
	}

	// Only player/NPC turns push the cadence forward; tool turns are noise.
	if role != "user" && role != "assistant" {
		return convID, nil
	}

	// Decide whether to fire extraction under the lock so the in-flight
	// guard cannot race with another OnMessage.
	t.mu.Lock()
	key := normalizeNPCName(npcName)
	sess, ok := t.sessions[key]
	if !ok {
		t.mu.Unlock()
		return convID, nil
	}
	sess.msgCount++
	shouldExtract := t.summarizer != nil &&
		sess.msgCount%t.extractEveryN == 0 &&
		!t.extractInFlight[key]
	if shouldExtract {
		t.extractInFlight[key] = true
	}
	t.mu.Unlock()

	if shouldExtract {
		go t.extractMemoriesAsync(npcName, convID)
	}
	return convID, nil
}

// extractMemoriesAsync runs Summarizer.ExtractMemories on the recent
// transcript and persists every returned Memory. Bounded by extractTimeout.
func (t *ConversationTracker) extractMemoriesAsync(npcName string, convID int64) {
	defer func() {
		t.mu.Lock()
		delete(t.extractInFlight, normalizeNPCName(npcName))
		t.mu.Unlock()
	}()

	ctx, cancel := contextWithTimeout(extractTimeout)
	defer cancel()

	msgs, err := t.recentMessages(convID, extractWindowSize)
	if err != nil {
		t.logger.Debug("extract: load recent messages failed", "err", err)
		return
	}
	if len(msgs) == 0 {
		return
	}
	memories, err := t.summarizer.ExtractMemories(ctx, npcName, msgs)
	if err != nil {
		t.logger.Debug("extract: ExtractMemories failed", "err", err)
		return
	}
	for _, m := range memories {
		if err := t.store.StoreMemory(m); err != nil {
			t.logger.Debug("extract: StoreMemory failed", "err", err)
		}
	}
	t.logger.Info("memory extraction complete", "npc", npcName, "extracted", len(memories))
}

// summarizeAndExtract is invoked at end-of-conversation: it produces both
// a Summary row AND the durable memories. Failures of either step are
// independent — a missing summary should not block memory persistence.
func (t *ConversationTracker) summarizeAndExtract(npcName string, convID int64) {
	if t.summarizer == nil {
		return
	}
	ctx, cancel := contextWithTimeout(extractTimeout)
	defer cancel()

	msgs, err := t.recentMessages(convID, summaryWindowSize)
	if err != nil || len(msgs) == 0 {
		return
	}

	if summary, topics, tone, sErr := t.summarizer.SummarizeConversation(ctx, msgs); sErr == nil && summary != "" {
		if err := t.store.StoreSummary(Summary{
			NPCName:        npcName,
			ConversationID: convID,
			Summary:        summary,
			KeyTopics:      topics,
			EmotionalTone:  tone,
			CreatedAt:      t.now().UTC(),
		}); err != nil {
			t.logger.Debug("end: StoreSummary failed", "err", err)
		}
	} else if sErr != nil {
		t.logger.Debug("end: Summarize failed", "err", sErr)
	}

	memories, err := t.summarizer.ExtractMemories(ctx, npcName, msgs)
	if err != nil {
		t.logger.Debug("end: ExtractMemories failed", "err", err)
		return
	}
	for _, m := range memories {
		_ = t.store.StoreMemory(m)
	}
}

// recentMessages reads the last `n` messages from a conversation in
// chronological order. Implemented locally rather than via Store so the
// tracker is the only consumer of this read path.
func (t *ConversationTracker) recentMessages(convID int64, n int) ([]Message, error) {
	concrete, ok := t.store.(*sqliteStore)
	if !ok {
		return nil, nil // foreign Store implementations skip the read; OK.
	}
	rows, err := concrete.db.Query(
		`SELECT id, conversation_id, role, content, timestamp, IFNULL(tool_calls, '')
		 FROM messages WHERE conversation_id = ?
		 ORDER BY id DESC LIMIT ?`,
		convID, n,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		var (
			m  Message
			ts string
		)
		if err := rows.Scan(&m.ID, &m.ConversationID, &m.Role, &m.Content, &ts, &m.ToolCalls); err != nil {
			return nil, err
		}
		m.Timestamp = parseTime(ts)
		out = append(out, m)
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, rows.Err()
}

// extractTimeout caps a single async extraction LLM call.
const extractTimeout = 30 * time.Second

// extractWindowSize / summaryWindowSize are the per-call message limits
// fed to the summarizer. Kept tighter for periodic extraction (the latest
// exchange is enough) and wider for end-of-conversation summaries.
const (
	extractWindowSize = 20
	summaryWindowSize = 60
)

// Shutdown is an alias for Close kept around to match the team-lead's
// proposed surface. Idempotent.
func (t *ConversationTracker) Shutdown() error { return t.Close() }

// SweepIdle ends every conversation whose lastActive is older than the
// configured idle timeout. Returns the number of conversations closed.
// Safe to call from a periodic goroutine.
func (t *ConversationTracker) SweepIdle() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed || t.idleTimeout <= 0 {
		return 0
	}
	now := t.now()
	var closed int
	for key, sess := range t.sessions {
		if now.Sub(sess.lastActive) >= t.idleTimeout {
			t.endLocked(sess)
			delete(t.sessions, key)
			closed++
		}
	}
	if closed > 0 {
		t.logger.Debug("idle sweep closed conversations", "count", closed)
	}
	return closed
}

// CurrentConversationID returns the open conversation id for npcName, or 0
// if none. Useful for tests and diagnostics.
func (t *ConversationTracker) CurrentConversationID(npcName string) int64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	if sess, ok := t.sessions[normalizeNPCName(npcName)]; ok {
		return sess.convID
	}
	return 0
}

// Close flushes every still-open conversation through EndConversation and
// blocks new StartTurn calls. When a Summarizer is configured, each open
// conversation also gets a synchronous summarize-and-extract pass so the
// long-term store sees the latest dialogue before shutdown completes.
// Idempotent.
func (t *ConversationTracker) Close() error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil
	}
	t.closed = true
	// Snapshot sessions so we can run summarization outside the lock —
	// the LLM call must not contend with concurrent OnMessage waiters.
	snapshot := make([]*trackerSession, 0, len(t.sessions))
	for key, sess := range t.sessions {
		snapshot = append(snapshot, sess)
		delete(t.sessions, key)
	}
	t.mu.Unlock()

	for _, sess := range snapshot {
		if t.summarizer != nil {
			t.summarizeAndExtract(sess.npcName, sess.convID)
		}
		t.endLocked(sess)
	}
	return nil
}

// endLocked ends a single session via the Store. Caller must hold t.mu.
// Errors are logged and swallowed so a single broken row cannot prevent
// shutdown from finishing.
func (t *ConversationTracker) endLocked(sess *trackerSession) {
	if err := t.store.EndConversation(sess.convID); err != nil {
		t.logger.Warn("EndConversation failed during tracker flush",
			"npc", sess.npcName, "conv_id", sess.convID, "err", err)
	}
}

// normalizeNPCName lowercases ASCII NPC names for case-insensitive map
// lookup. Mirrors the chat router's normalizeSpeaker helper but kept
// duplicated to avoid a circular import (chat → memory → chat).
func normalizeNPCName(name string) string {
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
