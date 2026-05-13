package memory

import (
	"testing"
	"time"
)

func seedRetrieverFixture(t *testing.T, s Store) {
	t.Helper()

	convID, err := s.StartConversation("Xiami", "Spring 5")
	if err != nil {
		t.Fatalf("StartConversation: %v", err)
	}

	// Two summaries, oldest first when surfaced (the retriever reverses
	// the DESC SQL order so chronological reading is natural).
	now := time.Now().UTC()
	if err := s.StoreSummary(Summary{
		NPCName: "Xiami", ConversationID: convID,
		Summary: "first conversation", KeyTopics: []string{"intro"},
		CreatedAt: now.Add(-3 * time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.StoreSummary(Summary{
		NPCName: "Xiami", ConversationID: convID,
		Summary: "second conversation", KeyTopics: []string{"food"},
		CreatedAt: now.Add(-time.Hour),
	}); err != nil {
		t.Fatal(err)
	}

	// Memories. Importance bands selected so retriever defaults exclude
	// the importance=2 row.
	memories := []Memory{
		{NPCName: "Xiami", Category: CategoryFact, Content: "fact-high", Importance: 9},
		{NPCName: "Xiami", Category: CategoryFact, Content: "fact-mid", Importance: 5},
		{NPCName: "Xiami", Category: CategoryEvent, Content: "event-low", Importance: 2},
		{NPCName: "Xiami", Category: CategoryRelationship, Content: "rel-1", Importance: 1},
		{NPCName: "Xiami", Category: CategoryRelationship, Content: "rel-2", Importance: 8},
	}
	for _, m := range memories {
		if err := s.StoreMemory(m); err != nil {
			t.Fatalf("StoreMemory: %v", err)
		}
	}
}

func TestRetriever_GetContextBundle(t *testing.T) {
	s := openTestStore(t)
	seedRetrieverFixture(t, s)

	bundle, err := s.GetContextBundle("Xiami", "say hi")
	if err != nil {
		t.Fatalf("GetContextBundle: %v", err)
	}
	if bundle == nil {
		t.Fatal("expected non-nil bundle")
	}

	t.Run("recent summary chronological", func(t *testing.T) {
		if len(bundle.RecentSummary) != 2 {
			t.Fatalf("want 2 summaries, got %d", len(bundle.RecentSummary))
		}
		if bundle.RecentSummary[0].Summary != "first conversation" {
			t.Errorf("summaries should be oldest-first: %v", bundle.RecentSummary)
		}
	})

	t.Run("relationship facts always included", func(t *testing.T) {
		if len(bundle.RelationshipFacts) != 2 {
			t.Fatalf("want 2 relationship rows (regardless of importance), got %d",
				len(bundle.RelationshipFacts))
		}
	})

	t.Run("key memories filter by importance", func(t *testing.T) {
		if len(bundle.KeyMemories) != 2 {
			t.Fatalf("want 2 key memories (importance ≥ 3), got %d", len(bundle.KeyMemories))
		}
		// Highest importance first.
		if bundle.KeyMemories[0].Importance < bundle.KeyMemories[1].Importance {
			t.Errorf("key memories not ordered importance DESC: %v", bundle.KeyMemories)
		}
		for _, m := range bundle.KeyMemories {
			if m.Category == CategoryRelationship {
				t.Errorf("relationship row leaked into KeyMemories: %v", m)
			}
		}
	})

	t.Run("conversation count", func(t *testing.T) {
		if bundle.TotalConversations != 1 {
			t.Errorf("want 1 conversation, got %d", bundle.TotalConversations)
		}
	})

	t.Run("touch bumps access counts", func(t *testing.T) {
		// Re-fetch and confirm at least one row's access_count grew above 0.
		got, _ := s.GetMemories("Xiami", MemoryQuery{
			Categories: []string{CategoryRelationship},
		})
		var bumped bool
		for _, m := range got {
			if m.AccessCount > 0 {
				bumped = true
				break
			}
		}
		if !bumped {
			t.Error("expected at least one relationship memory to be touched")
		}
	})
}

func TestRetriever_EmptyDatabase(t *testing.T) {
	s := openTestStore(t)
	bundle, err := s.GetContextBundle("Xiami", "")
	if err != nil {
		t.Fatalf("GetContextBundle: %v", err)
	}
	if bundle == nil {
		t.Fatal("expected non-nil bundle")
	}
	if len(bundle.RecentSummary) != 0 {
		t.Errorf("empty DB should yield no summaries")
	}
	if len(bundle.KeyMemories) != 0 {
		t.Errorf("empty DB should yield no key memories")
	}
	if len(bundle.RelationshipFacts) != 0 {
		t.Errorf("empty DB should yield no relationship facts")
	}
	if bundle.TotalConversations != 0 {
		t.Errorf("empty DB should report 0 conversations")
	}
}

func TestRetriever_RejectsEmptyName(t *testing.T) {
	s := openTestStore(t)
	if _, err := s.GetContextBundle("", ""); err == nil {
		t.Fatal("expected error for empty NPC name")
	}
}

func TestRetriever_RecentSummaryRespectsLimit(t *testing.T) {
	s := openTestStore(t)
	convID, _ := s.StartConversation("Xiami", "")
	now := time.Now().UTC()
	for i := 0; i < 5; i++ {
		if err := s.StoreSummary(Summary{
			NPCName: "Xiami", ConversationID: convID,
			Summary:   "summary",
			CreatedAt: now.Add(time.Duration(i) * time.Minute),
		}); err != nil {
			t.Fatal(err)
		}
	}
	bundle, err := s.GetContextBundle("Xiami", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(bundle.RecentSummary) != defaultRecentSummaryCount {
		t.Errorf("want %d summaries, got %d", defaultRecentSummaryCount, len(bundle.RecentSummary))
	}
}

func TestExtractJSONObject(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{`{"a":1}`, `{"a":1}`},
		{"```json\n{\"a\":1}\n```", `{"a":1}`},
		{"prefix {\"a\":\"}\"} suffix", `{"a":"}"}`},
		{"no json here", ""},
	}
	for _, c := range cases {
		got := extractJSONObject(c.in)
		if got != c.want {
			t.Errorf("extractJSONObject(%q) = %q want %q", c.in, got, c.want)
		}
	}
}

func TestRenderTranscript_SkipsSystemAndEmpty(t *testing.T) {
	got := renderTranscript([]Message{
		{Role: "system", Content: "ignored"},
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: ""},
		{Role: "tool", Content: "result"},
	})
	if got == "" {
		t.Fatal("transcript should not be empty")
	}
	if contains := indexOf(got, "ignored"); contains >= 0 {
		t.Errorf("system role should be filtered out, got %q", got)
	}
	if indexOf(got, "user: hi") < 0 {
		t.Errorf("user line missing: %q", got)
	}
	if indexOf(got, "tool: result") < 0 {
		t.Errorf("tool line missing: %q", got)
	}
}

// indexOf is a tiny helper to avoid pulling strings into every test.
func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
