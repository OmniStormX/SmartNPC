package memory

import (
	"strings"
	"testing"
	"time"
)

func openTestStore(t *testing.T) Store {
	t.Helper()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open(:memory:): %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return s
}

// TestOpen_RejectsEmptyPath proves the constructor refuses to spin up a DB
// when the caller forgot to pass a path. This guards against silent misuse.
func TestOpen_RejectsEmptyPath(t *testing.T) {
	if _, err := Open(""); err == nil {
		t.Fatal("Open(\"\") should fail")
	}
}

// TestOpen_AppliesSchema verifies the schema bootstraps cleanly on an empty
// in-memory database — every CRUD operation below depends on this.
func TestOpen_AppliesSchema(t *testing.T) {
	s := openTestStore(t)
	// Smoke test: a table that doesn't exist would surface as a Query error
	// from any subsequent call. Hit the simplest one.
	if _, err := s.GetMemories("Xiami", MemoryQuery{}); err != nil {
		t.Fatalf("GetMemories on empty schema: %v", err)
	}
}

func TestStore_ConversationLifecycle(t *testing.T) {
	s := openTestStore(t)

	t.Run("start invalid", func(t *testing.T) {
		if _, err := s.StartConversation("", "Spring 1"); err == nil {
			t.Error("expected error for empty npc name")
		}
	})

	t.Run("end invalid id", func(t *testing.T) {
		if err := s.EndConversation(0); err == nil {
			t.Error("expected error for zero id")
		}
	})

	t.Run("happy path", func(t *testing.T) {
		id, err := s.StartConversation("Xiami", "Spring 5")
		if err != nil {
			t.Fatalf("Start: %v", err)
		}
		if id <= 0 {
			t.Fatalf("expected positive id, got %d", id)
		}
		if err := s.EndConversation(id); err != nil {
			t.Fatalf("End: %v", err)
		}
	})
}

func TestStore_AppendMessage(t *testing.T) {
	s := openTestStore(t)
	convID, err := s.StartConversation("Xiami", "")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	tests := []struct {
		name    string
		convID  int64
		msg     Message
		wantErr bool
	}{
		{
			name:   "user msg ok",
			convID: convID,
			msg:    Message{Role: "user", Content: "hi"},
		},
		{
			name:   "assistant with tool calls ok",
			convID: convID,
			msg:    Message{Role: "assistant", Content: "", ToolCalls: `[{"name":"x"}]`},
		},
		{
			name:    "missing conv id",
			convID:  0,
			msg:     Message{Role: "user", Content: "x"},
			wantErr: true,
		},
		{
			name:    "missing role",
			convID:  convID,
			msg:     Message{Content: "x"},
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := s.AppendMessage(tc.convID, tc.msg)
			if (err != nil) != tc.wantErr {
				t.Fatalf("AppendMessage err=%v wantErr=%v", err, tc.wantErr)
			}
		})
	}
}

func TestStore_StoreAndQueryMemories(t *testing.T) {
	s := openTestStore(t)

	seed := []Memory{
		{NPCName: "Xiami", Category: CategoryFact, Content: "player likes sunflowers", Importance: 6},
		{NPCName: "Xiami", Category: CategoryPreference, Content: "player hates eggplant", Importance: 4},
		{NPCName: "Xiami", Category: CategoryRelationship, Content: "we agreed to be friends", Importance: 9},
		{NPCName: "Xiami", Category: CategoryEvent, Content: "we went fishing", Importance: 2}, // below MinImportance
		{NPCName: "Other", Category: CategoryFact, Content: "noise from another npc", Importance: 8},
	}
	for _, m := range seed {
		if err := s.StoreMemory(m); err != nil {
			t.Fatalf("StoreMemory(%q): %v", m.Content, err)
		}
	}

	t.Run("filter by npc", func(t *testing.T) {
		got, err := s.GetMemories("Xiami", MemoryQuery{})
		if err != nil {
			t.Fatalf("GetMemories: %v", err)
		}
		if len(got) != 4 {
			t.Fatalf("want 4 memories for Xiami, got %d", len(got))
		}
	})

	t.Run("filter by category and min importance", func(t *testing.T) {
		got, err := s.GetMemories("Xiami", MemoryQuery{
			Categories:    []string{CategoryFact, CategoryPreference, CategoryEvent},
			MinImportance: 3,
		})
		if err != nil {
			t.Fatalf("GetMemories: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("want 2 memories ≥3 importance, got %d", len(got))
		}
		// importance DESC ordering: 6 then 4
		if got[0].Importance < got[1].Importance {
			t.Errorf("results not ordered by importance: %v", got)
		}
	})

	t.Run("substring search", func(t *testing.T) {
		got, err := s.GetMemories("Xiami", MemoryQuery{Search: "fishing"})
		if err != nil {
			t.Fatalf("GetMemories: %v", err)
		}
		if len(got) != 1 || !strings.Contains(got[0].Content, "fishing") {
			t.Fatalf("search returned wrong rows: %v", got)
		}
	})

	t.Run("expire excludes by default", func(t *testing.T) {
		// Target a non-relationship row by substring search so the
		// later "touch" subtest still has a relationship memory to find.
		victims, err := s.GetMemories("Xiami", MemoryQuery{Search: "fishing"})
		if err != nil || len(victims) == 0 {
			t.Fatalf("seed lookup failed: err=%v rows=%d", err, len(victims))
		}
		all, _ := s.GetMemories("Xiami", MemoryQuery{})
		if err := s.ExpireMemory(victims[0].ID); err != nil {
			t.Fatalf("ExpireMemory: %v", err)
		}
		afterDefault, err := s.GetMemories("Xiami", MemoryQuery{})
		if err != nil {
			t.Fatalf("GetMemories: %v", err)
		}
		if len(afterDefault) != len(all)-1 {
			t.Fatalf("expired memory should be hidden by default")
		}
		afterIncl, err := s.GetMemories("Xiami", MemoryQuery{IncludeExpired: true})
		if err != nil {
			t.Fatalf("GetMemories include: %v", err)
		}
		if len(afterIncl) != len(all) {
			t.Fatalf("IncludeExpired should surface soft-deleted rows")
		}
	})

	t.Run("touch bumps access count", func(t *testing.T) {
		got, err := s.GetMemories("Xiami", MemoryQuery{Categories: []string{CategoryRelationship}})
		if err != nil {
			t.Fatalf("GetMemories: %v", err)
		}
		if len(got) == 0 {
			t.Fatal("expected at least one relationship memory")
		}
		before := got[0].AccessCount
		if err := s.TouchMemory(got[0].ID); err != nil {
			t.Fatalf("TouchMemory: %v", err)
		}
		got2, _ := s.GetMemories("Xiami", MemoryQuery{Categories: []string{CategoryRelationship}})
		if got2[0].AccessCount != before+1 {
			t.Fatalf("access_count not bumped: before=%d after=%d", before, got2[0].AccessCount)
		}
	})
}

func TestStore_StoreMemory_Validation(t *testing.T) {
	s := openTestStore(t)

	cases := []Memory{
		{NPCName: "", Category: CategoryFact, Content: "x"},
		{NPCName: "Xiami", Category: "", Content: "x"},
		{NPCName: "Xiami", Category: CategoryFact, Content: ""},
	}
	for i, c := range cases {
		if err := s.StoreMemory(c); err == nil {
			t.Errorf("case %d: expected validation error, got nil", i)
		}
	}
}

func TestStore_StoreMemory_DefaultsImportance(t *testing.T) {
	s := openTestStore(t)
	if err := s.StoreMemory(Memory{
		NPCName:  "Xiami",
		Category: CategoryFact,
		Content:  "no importance set",
	}); err != nil {
		t.Fatalf("StoreMemory: %v", err)
	}
	got, _ := s.GetMemories("Xiami", MemoryQuery{})
	if len(got) != 1 || got[0].Importance != 5 {
		t.Fatalf("importance default to 5, got %d", got[0].Importance)
	}
}

func TestStore_Summaries(t *testing.T) {
	s := openTestStore(t)
	convID, _ := s.StartConversation("Xiami", "Spring 1")

	now := time.Now().UTC()
	if err := s.StoreSummary(Summary{
		NPCName:        "Xiami",
		ConversationID: convID,
		Summary:        "Met the player by the river.",
		KeyTopics:      []string{"river", "fishing"},
		EmotionalTone:  "warm",
		CreatedAt:      now.Add(-time.Hour),
	}); err != nil {
		t.Fatalf("StoreSummary: %v", err)
	}
	if err := s.StoreSummary(Summary{
		NPCName:        "Xiami",
		ConversationID: convID,
		Summary:        "Discussed favorite food.",
		KeyTopics:      []string{"food"},
		EmotionalTone:  "playful",
		CreatedAt:      now,
	}); err != nil {
		t.Fatalf("StoreSummary: %v", err)
	}

	got, err := s.GetRecentSummaries("Xiami", 5)
	if err != nil {
		t.Fatalf("GetRecentSummaries: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 summaries, got %d", len(got))
	}
	// Newest first by spec.
	if !got[0].CreatedAt.After(got[1].CreatedAt) {
		t.Errorf("summaries not newest-first: %v vs %v", got[0].CreatedAt, got[1].CreatedAt)
	}
	if len(got[0].KeyTopics) == 0 {
		t.Error("KeyTopics should round-trip through JSON")
	}
}

func TestStore_StoreSummary_Validation(t *testing.T) {
	s := openTestStore(t)
	cases := []Summary{
		{NPCName: "", ConversationID: 1, Summary: "x"},
		{NPCName: "Xiami", ConversationID: 0, Summary: "x"},
		{NPCName: "Xiami", ConversationID: 1, Summary: ""},
	}
	for i, c := range cases {
		if err := s.StoreSummary(c); err == nil {
			t.Errorf("case %d: expected validation error", i)
		}
	}
}
