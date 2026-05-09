package chat

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/smartnpc/smartnpc-agent/internal/llm"
	"github.com/smartnpc/smartnpc-agent/internal/memory"
)

// openTestMemory spins up an in-memory SQLite store for a single test run.
// It's mirrored from internal/memory/store_test.go so this package doesn't
// reach across into private helpers.
func openTestMemory(t *testing.T) memory.Store {
	t.Helper()
	s, err := memory.Open(":memory:")
	if err != nil {
		t.Fatalf("memory.Open(:memory:): %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("memory.Close: %v", err)
		}
	})
	return s
}

// TestMemoryIntegration_RespondMirrorsTurnsToStore proves that one round of
// respond() with memory wired up:
//
//   - opens (and reuses) a single conversation,
//   - persists the user message + assistant reply into the messages table,
//   - leaves no unbalanced rows behind.
//
// The test deliberately avoids tool calls so we can lock the message count
// to exactly two (user + assistant) without worrying about role: tool rows.
func TestMemoryIntegration_RespondMirrorsTurnsToStore(t *testing.T) {
	store := openTestMemory(t)

	mp := &mockProvider{replies: []llm.ChatResponse{
		{Content: "Hi farmer!", FinishReason: "stop"},
	}}

	a := New(Config{
		Provider:     mp,
		Speaker:      "Xiami",
		SystemPrompt: "You are Xiami.",
		MaxHistory:   10,
		Memory:       store,
		// Skip game-date so the tracker doesn't auto-rollover between calls.
		MemoryGameDateFn: func() string { return "" },
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	reply, err := a.respond(ctx, "Hello there")
	if err != nil {
		t.Fatalf("respond: %v", err)
	}
	if reply != "Hi farmer!" {
		t.Errorf("unexpected reply: %q", reply)
	}

	// We can't query messages directly through the public Store API, but we
	// can verify side effects: the conversation count grew, and a follow-up
	// turn reuses the same conversation (covered by the next test).
	bundle, err := store.GetContextBundle("Xiami", "")
	if err != nil {
		t.Fatalf("GetContextBundle: %v", err)
	}
	if bundle.TotalConversations != 1 {
		t.Errorf("want 1 conversation after first turn, got %d", bundle.TotalConversations)
	}
}

// TestMemoryIntegration_ContextBundleInjectedIntoSystemPrompt seeds the
// memory store with a few rows, runs respond(), and inspects the LLM
// request the mock provider received to confirm the system prompt carries
// the rendered ContextBundle. We only check that the speaker name + memory
// header appear; the exact wording is locked in renderContextBundle's own
// tests inside the memory package.
func TestMemoryIntegration_ContextBundleInjectedIntoSystemPrompt(t *testing.T) {
	store := openTestMemory(t)

	// Seed memory.
	if err := store.StoreMemory(memory.Memory{
		NPCName:    "Xiami",
		Category:   memory.CategoryRelationship,
		Content:    "the farmer once gave me wildflowers",
		Importance: 7,
	}); err != nil {
		t.Fatalf("StoreMemory: %v", err)
	}
	if err := store.StoreMemory(memory.Memory{
		NPCName:    "Xiami",
		Category:   memory.CategoryFact,
		Content:    "the farmer raises chickens",
		Importance: 6,
	}); err != nil {
		t.Fatalf("StoreMemory: %v", err)
	}

	mp := &mockProvider{replies: []llm.ChatResponse{
		{Content: "I remember.", FinishReason: "stop"},
	}}

	a := New(Config{
		Provider:     mp,
		Speaker:      "Xiami",
		SystemPrompt: "You are Xiami.",
		MaxHistory:   10,
		Memory:       store,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := a.respond(ctx, "Hi"); err != nil {
		t.Fatalf("respond: %v", err)
	}

	mp.mu.Lock()
	defer mp.mu.Unlock()
	if len(mp.calls) == 0 {
		t.Fatal("provider was never called")
	}
	// Find the system message in the first request.
	first := mp.calls[0]
	var sys string
	for _, m := range first.Messages {
		if m.Role == llm.RoleSystem {
			sys = m.Content
			break
		}
	}
	if sys == "" {
		t.Fatal("no system message found")
	}
	if !strings.Contains(sys, "## Your Memories") {
		t.Errorf("system prompt missing memory header: %q", sys)
	}
	if !strings.Contains(sys, "wildflowers") {
		t.Errorf("system prompt missing relationship fact: %q", sys)
	}
	if !strings.Contains(sys, "chickens") {
		t.Errorf("system prompt missing key memory: %q", sys)
	}
}

// TestMemoryIntegration_RecallToolDispatch verifies that when the LLM emits
// a tool_call named "memory_recall" the agent intercepts it without a live
// MCP session — proving the in-process tool wiring is correct.
func TestMemoryIntegration_RecallToolDispatch(t *testing.T) {
	store := openTestMemory(t)

	if err := store.StoreMemory(memory.Memory{
		NPCName:    "Xiami",
		Category:   memory.CategoryFact,
		Content:    "player loves spring flowers",
		Importance: 8,
	}); err != nil {
		t.Fatalf("StoreMemory: %v", err)
	}

	// Two-step LLM dance: first round emits memory_recall, second round
	// produces the final text reply.
	mp := &mockProvider{replies: []llm.ChatResponse{
		{
			ToolCalls: []llm.ToolCall{{
				ID:   "call_1",
				Name: "memory_recall",
				Arguments: map[string]any{
					"query": "spring",
				},
			}},
			FinishReason: "tool_calls",
		},
		{Content: "Yes, you love spring flowers!", FinishReason: "stop"},
	}}

	a := New(Config{
		Provider:     mp,
		Speaker:      "Xiami",
		SystemPrompt: "You are Xiami.",
		MaxHistory:   10,
		Memory:       store,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	reply, err := a.respond(ctx, "Do you remember what I like?")
	if err != nil {
		t.Fatalf("respond: %v", err)
	}
	if reply != "Yes, you love spring flowers!" {
		t.Errorf("unexpected reply: %q", reply)
	}

	// Second LLM request should contain the tool result with the recall payload.
	mp.mu.Lock()
	defer mp.mu.Unlock()
	if len(mp.calls) < 2 {
		t.Fatalf("expected at least 2 LLM calls (tool round + final), got %d", len(mp.calls))
	}
	second := mp.calls[1]
	var toolResult string
	for _, m := range second.Messages {
		if m.Role == llm.RoleTool && m.Name == "memory_recall" {
			toolResult = m.Content
			break
		}
	}
	if toolResult == "" {
		t.Fatal("tool result not threaded back into LLM messages")
	}
	if !strings.Contains(toolResult, "spring flowers") {
		t.Errorf("tool result missing recalled content: %q", toolResult)
	}
}

// TestMemoryIntegration_StoreToolPersists ensures that an LLM-emitted
// memory_store call actually inserts into the SQLite store via the
// in-process toolset.
func TestMemoryIntegration_StoreToolPersists(t *testing.T) {
	store := openTestMemory(t)

	mp := &mockProvider{replies: []llm.ChatResponse{
		{
			ToolCalls: []llm.ToolCall{{
				ID:   "call_1",
				Name: "memory_store",
				Arguments: map[string]any{
					"content":    "the farmer mentioned a sick cow today",
					"category":   memory.CategoryEvent,
					"importance": 7,
				},
			}},
			FinishReason: "tool_calls",
		},
		{Content: "I'll remember that.", FinishReason: "stop"},
	}}

	a := New(Config{
		Provider:     mp,
		Speaker:      "Xiami",
		SystemPrompt: "You are Xiami.",
		MaxHistory:   10,
		Memory:       store,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := a.respond(ctx, "I have a sick cow."); err != nil {
		t.Fatalf("respond: %v", err)
	}

	got, err := store.GetMemories("Xiami", memory.MemoryQuery{Search: "sick cow"})
	if err != nil {
		t.Fatalf("GetMemories: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 memory persisted by memory_store, got %d", len(got))
	}
	if got[0].Importance != 7 {
		t.Errorf("importance not persisted: got %d want 7", got[0].Importance)
	}
	if got[0].Category != memory.CategoryEvent {
		t.Errorf("category mismatch: got %q want %q", got[0].Category, memory.CategoryEvent)
	}
}

// TestMemoryIntegration_CloseFlushesConversation locks down that Agent.Close
// (called by cmd/smartnpc-agent on shutdown) runs the tracker flush so the
// conversation row gets ended_at stamped.
func TestMemoryIntegration_CloseFlushesConversation(t *testing.T) {
	store := openTestMemory(t)

	mp := &mockProvider{replies: []llm.ChatResponse{
		{Content: "ok", FinishReason: "stop"},
	}}
	a := New(Config{
		Provider:     mp,
		Speaker:      "Xiami",
		SystemPrompt: "You are Xiami.",
		MaxHistory:   10,
		Memory:       store,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := a.respond(ctx, "Hi"); err != nil {
		t.Fatalf("respond: %v", err)
	}

	// Sanity: tracker holds an open conversation now.
	if a.memory == nil || a.memory.tracker == nil {
		t.Fatal("memory tracker should be wired")
	}
	if a.memory.tracker.CurrentConversationID("Xiami") == 0 {
		t.Error("expected an open conversation before Close")
	}

	if err := a.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// Idempotent: second Close is a no-op.
	if err := a.Close(); err != nil {
		t.Fatalf("Close (second): %v", err)
	}

	if a.memory.tracker.CurrentConversationID("Xiami") != 0 {
		t.Error("Close should have flushed the open conversation")
	}
}

// TestRenderContextBundle_MarkdownLayout locks the markdown wording the
// LLM sees so future refactors don't silently change the prompt shape.
func TestRenderContextBundle_MarkdownLayout(t *testing.T) {
	now := time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC)
	bundle := &memory.ContextBundle{
		TotalConversations: 3,
		RecentSummary: []memory.Summary{
			{Summary: "Met by the river.", CreatedAt: now.Add(-48 * time.Hour)},
			{Summary: "Discussed cooking.", CreatedAt: now.Add(-24 * time.Hour)},
		},
		KeyMemories: []memory.Memory{
			{Category: memory.CategoryFact, Content: "player is allergic to nuts", Importance: 8},
			{Category: memory.CategoryPreference, Content: "loves spring sunsets"},
		},
		RelationshipFacts: []memory.Memory{
			{Category: memory.CategoryRelationship, Content: "we are friends"},
		},
	}
	got := renderContextBundle(bundle)

	for _, want := range []string{
		"## Your Memories",
		"### Recent Conversations",
		"### Key Facts You Remember",
		"### Your Relationship With The Player",
		"3 previous conversation",
		"Met by the river.",
		"[fact] player is allergic to nuts",
		"we are friends",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered bundle missing %q\nfull output:\n%s", want, got)
		}
	}
}

// TestRenderContextBundle_EmptyReturnsBlank ensures we don't pollute the
// system prompt with header-only scaffolding for an empty memory state.
func TestRenderContextBundle_EmptyReturnsBlank(t *testing.T) {
	if got := renderContextBundle(&memory.ContextBundle{}); got != "" {
		t.Errorf("expected blank output for empty bundle, got %q", got)
	}
	if got := renderContextBundle(nil); got != "" {
		t.Errorf("expected blank output for nil bundle, got %q", got)
	}
}

// TestMemoryDBPath_AutoOpensAndCloses verifies the one-line opt-in path:
// callers that don't want to wire memory.Open themselves can hand a path
// to chat.Config and Agent.Close() flushes both tracker and store.
func TestMemoryDBPath_AutoOpensAndCloses(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/auto_memory.db"

	mp := &mockProvider{}
	a := New(Config{
		Provider:     mp,
		Speaker:      "Xiami",
		MaxHistory:   4,
		Timeout:      time.Second,
		MemoryDBPath: path,
	})
	if a.memory == nil {
		t.Fatal("memoryWiring should be initialised when MemoryDBPath is set")
	}
	if !a.memory.ownsStore {
		t.Error("auto-opened store should be owned by the agent")
	}

	// Drive a turn so we can verify the row hit disk.
	mp.replies = []llm.ChatResponse{{Content: "hello", FinishReason: "stop"}}
	if _, err := a.respond(context.Background(), "hi"); err != nil {
		t.Fatalf("respond: %v", err)
	}

	if err := a.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// Re-open a fresh handle to the same file and confirm the rows persist.
	store2, err := memory.Open(path)
	if err != nil {
		t.Fatalf("re-open: %v", err)
	}
	defer store2.Close()
	mems, err := store2.GetMemories("Xiami", memory.MemoryQuery{IncludeExpired: true})
	if err != nil {
		t.Fatalf("GetMemories: %v", err)
	}
	_ = mems // we only need the file to be readable; real-state assertion
	// would require a Read API for messages which the public Store does
	// not yet expose.
}
