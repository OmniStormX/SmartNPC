// Memory integration glue.
//
// This file extends the chat agent with optional persistent memory backed
// by internal/memory. When Config.Memory is non-nil the agent:
//
//   - opens (or reuses) a conversation per NPC via memory.ConversationTracker,
//   - mirrors every user / assistant / tool message into the SQLite messages
//     table as it produces them,
//   - injects a ContextBundle digest into the system prompt before each LLM
//     call (rendered by renderContextBundle below),
//   - exposes memory_recall / memory_store as in-process tools dispatched
//     before any MCP tool call (so they cannot collide with NPC tools).
//
// The integration is purely additive: when Config.Memory is nil, none of the
// helpers below have side effects and the agent behaves exactly as before.

package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/smartnpc/smartnpc-agent/internal/llm"
	"github.com/smartnpc/smartnpc-agent/internal/memory"
)

// memoryExtractionEveryN is the message-count threshold (per NPC) at which
// the agent fires an asynchronous Summarizer.ExtractMemories call. Counts
// reset after each successful extraction so we don't spam the LLM.
const memoryExtractionEveryN = 10

// memoryWiring holds the per-agent runtime state for the memory subsystem.
// Stored on Agent under the same mutex as history so updates can be made
// atomically with history mutations.
type memoryWiring struct {
	store      memory.Store
	tracker    *memory.ConversationTracker
	toolset    *memory.MemoryToolset
	summarizer *memory.Summarizer

	gameDateFn func() string

	// ownsStore is true when initMemory opened the SQLite database
	// itself (via Config.MemoryDBPath / EnableMemoryByDefault). In that
	// case Agent.Close() must Close the store; otherwise the caller
	// retains ownership.
	ownsStore bool

	// turnsSinceExtract counts user+assistant messages mirrored into the
	// store since the last successful ExtractMemories invocation. Reset
	// when the goroutine finishes.
	turnsSinceExtract int
	// extractInFlight prevents concurrent extraction goroutines from
	// piling up if the LLM is slower than the chat cadence.
	extractInFlight bool
	// extractMu serialises the in-flight bookkeeping. Separate from
	// Agent.mu to keep extraction off the hot chat path.
	extractMu sync.Mutex
}

// defaultMemoryDBPath is the auto-open location when callers enable
// persistence without specifying a custom path. Lives under ./data so a
// stock checkout has a stable location for the SQLite file.
const defaultMemoryDBPath = "data/smartnpc_memory.db"

// initMemory builds the memoryWiring struct from Config. Returns nil when
// memory is not configured (zero Memory + zero MemoryDBPath +
// !EnableMemoryByDefault). The returned wiring records whether it owns
// the store so Agent.Close() can close it deterministically.
func initMemory(cfg Config) (*memoryWiring, error) {
	store := cfg.Memory
	owns := false
	if store == nil {
		path := cfg.MemoryDBPath
		if path == "" && cfg.EnableMemoryByDefault {
			path = defaultMemoryDBPath
		}
		if path == "" {
			return nil, nil
		}
		// Ensure the parent directory exists; SQLite refuses to open a
		// path under a missing directory.
		if dir := filepath.Dir(path); dir != "" && dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return nil, fmt.Errorf("init memory: create dir %s: %w", dir, err)
			}
		}
		s, err := memory.Open(path)
		if err != nil {
			return nil, fmt.Errorf("init memory: open %s: %w", path, err)
		}
		store = s
		owns = true
	}

	idle := cfg.MemoryIdleTimeout
	if idle == 0 {
		idle = memory.DefaultIdleTimeout
	}

	var summarizer *memory.Summarizer
	if cfg.MemorySummarizer != nil {
		summarizer = cfg.MemorySummarizer
	} else if cfg.Provider != nil {
		// Default: reuse the persona/legacy provider for summarization.
		// Callers can override with MemorySummarizer to point at a
		// different (e.g. cheaper) model.
		summarizer = &memory.Summarizer{Provider: cfg.Provider}
	}

	tracker, err := memory.NewTracker(store, memory.TrackerOptions{
		IdleTimeout: idle,
		Logger:      cfg.Logger,
		Summarizer:  summarizer,
	})
	if err != nil {
		if owns {
			_ = store.Close()
		}
		return nil, fmt.Errorf("init memory tracker: %w", err)
	}
	toolset, err := memory.NewToolset(store, cfg.Speaker)
	if err != nil {
		if owns {
			_ = store.Close()
		}
		return nil, fmt.Errorf("init memory toolset: %w", err)
	}
	return &memoryWiring{
		store:      store,
		tracker:    tracker,
		toolset:    toolset,
		summarizer: summarizer,
		gameDateFn: cfg.MemoryGameDateFn,
		ownsStore:  owns,
	}, nil
}

// memoryEnabled is a tiny predicate that hides the nil-checks scattered
// across the chat path.
func (a *Agent) memoryEnabled() bool {
	return a.memory != nil && a.memory.store != nil
}

// memoryCurrentGameDate is the safe accessor for the optional game-date
// callback. Empty string is acceptable to the tracker.
func (a *Agent) memoryCurrentGameDate() string {
	if a.memory == nil || a.memory.gameDateFn == nil {
		return ""
	}
	return a.memory.gameDateFn()
}

// memoryStartTurn opens or reuses a conversation for this NPC. Returns 0 +
// nil error when memory is disabled, so callers can use the id without
// branching.
func (a *Agent) memoryStartTurn() (int64, error) {
	if !a.memoryEnabled() {
		return 0, nil
	}
	return a.memory.tracker.StartTurn(a.cfg.Speaker, a.memoryCurrentGameDate())
}

// memoryAppend mirrors a single message into the SQLite messages table.
// Failures are logged but never block the chat turn — long-term memory is
// best-effort.
func (a *Agent) memoryAppend(convID int64, role, content string, toolCalls []llm.ToolCall) {
	if !a.memoryEnabled() || convID <= 0 {
		return
	}
	var tcRaw string
	if len(toolCalls) > 0 {
		if b, err := json.Marshal(toolCalls); err == nil {
			tcRaw = string(b)
		}
	}
	if err := a.memory.store.AppendMessage(convID, memory.Message{
		ConversationID: convID,
		Role:           role,
		Content:        content,
		Timestamp:      time.Now().UTC(),
		ToolCalls:      tcRaw,
	}); err != nil {
		a.cfg.Logger.Warn("memory append failed", "err", err, "role", role)
	}
}

// memoryNoteTurn records that one user/assistant exchange completed and
// fires the summarizer goroutine when the threshold trips. Tool turns do
// not count toward the cadence so a chatty assistant doesn't run away from
// the player's actual messages.
func (a *Agent) memoryNoteTurn(convID int64) {
	if !a.memoryEnabled() || a.memory.summarizer == nil {
		return
	}
	a.memory.extractMu.Lock()
	a.memory.turnsSinceExtract++
	shouldRun := !a.memory.extractInFlight && a.memory.turnsSinceExtract >= memoryExtractionEveryN
	if shouldRun {
		a.memory.extractInFlight = true
		a.memory.turnsSinceExtract = 0
	}
	a.memory.extractMu.Unlock()
	if !shouldRun {
		return
	}

	go a.runMemoryExtraction(convID)
}

// runMemoryExtraction reads the recent transcript for the active
// conversation and asks the Summarizer to distill new long-term memories.
// Errors are logged. A short timeout keeps the goroutine from hanging the
// process during shutdown.
func (a *Agent) runMemoryExtraction(convID int64) {
	defer func() {
		a.memory.extractMu.Lock()
		a.memory.extractInFlight = false
		a.memory.extractMu.Unlock()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Pull the most recent dialogue from the in-memory ring; we deliberately
	// do not re-query SQLite here because the agent's history slice already
	// holds the same messages and is cheaper to walk.
	a.mu.Lock()
	hist := make([]memory.Message, 0, len(a.history))
	for _, m := range a.history {
		hist = append(hist, memory.Message{
			ConversationID: convID,
			Role:           string(m.Role),
			Content:        m.Content,
			Timestamp:      time.Now().UTC(),
		})
	}
	a.mu.Unlock()
	if len(hist) == 0 {
		return
	}

	memories, err := a.memory.summarizer.ExtractMemories(ctx, a.cfg.Speaker, hist)
	if err != nil {
		a.cfg.Logger.Debug("memory extraction failed", "err", err)
		return
	}
	for _, m := range memories {
		if err := a.memory.store.StoreMemory(m); err != nil {
			a.cfg.Logger.Debug("memory store failed", "err", err, "content", m.Content)
		}
	}
	a.cfg.Logger.Info("memory extraction done", "extracted", len(memories))
}

// memoryFlush ends every open conversation and triggers a final summary.
// Called by Close() and by the cmd shutdown path. Idempotent.
//
// When the wiring owns its Store (i.e. Agent opened the SQLite file via
// Config.MemoryDBPath / EnableMemoryByDefault) the Store is closed here as
// well so callers don't have to plumb it back out.
func (a *Agent) memoryFlush() {
	if !a.memoryEnabled() {
		return
	}
	if err := a.memory.tracker.Close(); err != nil {
		a.cfg.Logger.Warn("memory tracker close failed", "err", err)
	}
	if a.memory.ownsStore {
		if err := a.memory.store.Close(); err != nil {
			a.cfg.Logger.Warn("memory store close failed", "err", err)
		}
	}
}

// memoryToolDispatch checks whether the LLM's tool call targets one of the
// in-process memory tools. Returns (result, true) when matched; (_, false)
// otherwise so the caller can fall through to the MCP path.
func (a *Agent) memoryToolDispatch(tc llm.ToolCall) (string, bool) {
	if !a.memoryEnabled() || a.memory.toolset == nil {
		return "", false
	}
	switch tc.Name {
	case a.memory.toolset.RecallSpec.Name:
		return a.memory.toolset.Recall(tc.Arguments), true
	case a.memory.toolset.StoreSpec.Name:
		return a.memory.toolset.Store(tc.Arguments), true
	}
	return "", false
}

// memoryToolSpecs returns the LLM-shaped specs for the in-process memory
// tools. Empty when memory is disabled.
func (a *Agent) memoryToolSpecs() []llm.ToolSpec {
	if !a.memoryEnabled() || a.memory.toolset == nil {
		return nil
	}
	return []llm.ToolSpec{
		{
			Name:        a.memory.toolset.RecallSpec.Name,
			Description: a.memory.toolset.RecallSpec.Description,
			InputSchema: a.memory.toolset.RecallSpec.InputSchema,
		},
		{
			Name:        a.memory.toolset.StoreSpec.Name,
			Description: a.memory.toolset.StoreSpec.Description,
			InputSchema: a.memory.toolset.StoreSpec.InputSchema,
		},
	}
}

// memoryContextAddendum produces a plain-text block summarising the
// retriever's ContextBundle. Empty when memory is disabled, or when the
// retrieve fails (failure is silent — chat must keep working).
func (a *Agent) memoryContextAddendum() string {
	if !a.memoryEnabled() {
		return ""
	}
	bundle, err := a.memory.store.GetContextBundle(a.cfg.Speaker, "")
	if err != nil {
		a.cfg.Logger.Debug("memory context fetch failed", "err", err)
		return ""
	}
	return renderContextBundle(bundle)
}

// renderContextBundle turns a ContextBundle into the system-prompt block
// the LLM sees. Output is a markdown-flavoured digest matching the
// team-lead's spec ("## Your Memories" header). Returns "" when the bundle
// has nothing useful to say so the prompt isn't polluted with empty
// scaffolding.
func renderContextBundle(b *memory.ContextBundle) string {
	if b == nil {
		return ""
	}
	hasContent := b.TotalConversations > 0 ||
		len(b.RecentSummary) > 0 ||
		len(b.KeyMemories) > 0 ||
		len(b.RelationshipFacts) > 0
	if !hasContent {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("## Your Memories")

	if b.TotalConversations > 0 {
		fmt.Fprintf(&sb, "\nYou've had %d previous conversation(s) with this player.",
			b.TotalConversations)
	}

	if len(b.RecentSummary) > 0 {
		sb.WriteString("\n\n### Recent Conversations")
		for _, s := range b.RecentSummary {
			line := strings.TrimSpace(s.Summary)
			if line == "" {
				continue
			}
			when := strings.TrimSpace(memorySummaryDateLabel(s))
			if when == "" {
				when = "earlier"
			}
			fmt.Fprintf(&sb, "\n- %s: %s", when, line)
		}
	}

	if len(b.KeyMemories) > 0 {
		sb.WriteString("\n\n### Key Facts You Remember")
		for _, m := range b.KeyMemories {
			fmt.Fprintf(&sb, "\n- [%s] %s", m.Category, m.Content)
		}
	}

	if len(b.RelationshipFacts) > 0 {
		sb.WriteString("\n\n### Your Relationship With The Player")
		for _, m := range b.RelationshipFacts {
			c := strings.TrimSpace(m.Content)
			if c == "" {
				continue
			}
			fmt.Fprintf(&sb, "\n- %s", c)
		}
	}

	sb.WriteString("\n\nUse these memories naturally when relevant; never recite them verbatim.")
	return sb.String()
}

// memorySummaryDateLabel picks the most caller-friendly label for when a
// summary was captured. Game-date strings (e.g. "Spring 5") are preferred
// when populated by the conversation row; otherwise we fall back to the
// real-world date the summary was written.
func memorySummaryDateLabel(s memory.Summary) string {
	// Future: thread the parent Conversation.GameDate through Summary so
	// we can show "Spring 5" instead of an ISO date. Today the Summary
	// row only carries CreatedAt, so we render that.
	if s.CreatedAt.IsZero() {
		return ""
	}
	return s.CreatedAt.Format("2006-01-02")
}

// joinMemoryContents flattens a memory list to "content; content; ..." for
// inline rendering inside the prompt.
func joinMemoryContents(ms []memory.Memory, sep string) string {
	parts := make([]string, 0, len(ms))
	for _, m := range ms {
		c := strings.TrimSpace(m.Content)
		if c != "" {
			parts = append(parts, c)
		}
	}
	return strings.Join(parts, sep)
}
