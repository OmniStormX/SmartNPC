package memory

import (
	"encoding/json"
	"strings"
	"testing"
)

func parseToolJSON(t *testing.T, raw string) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("tool returned invalid JSON %q: %v", raw, err)
	}
	return out
}

func TestNewToolset_Validation(t *testing.T) {
	store := openTestStore(t)
	if _, err := NewToolset(nil, "Xiami"); err == nil {
		t.Error("nil store should error")
	}
	if _, err := NewToolset(store, ""); err == nil {
		t.Error("empty NPC name should error")
	}
}

func TestMemoryStore_Tool_HappyPath(t *testing.T) {
	store := openTestStore(t)
	ts, err := NewToolset(store, "Xiami")
	if err != nil {
		t.Fatalf("NewToolset: %v", err)
	}

	out := parseToolJSON(t, ts.Store(map[string]any{
		"content":    "player promised to bring me a starfruit",
		"category":   CategoryPromise,
		"importance": float64(7), // emulate JSON-decoded number
	}))
	if out["ok"] != true {
		t.Fatalf("expected ok=true, got %v", out)
	}
	if out["category"] != CategoryPromise {
		t.Errorf("category roundtrip wrong: %v", out["category"])
	}

	// Verify the row hit the store.
	got, err := store.GetMemories("Xiami", MemoryQuery{Categories: []string{CategoryPromise}})
	if err != nil {
		t.Fatalf("GetMemories: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 memory, got %d", len(got))
	}
	if got[0].Importance != 7 {
		t.Errorf("importance %d, want 7", got[0].Importance)
	}
}

func TestMemoryStore_Tool_Validation(t *testing.T) {
	store := openTestStore(t)
	ts, _ := NewToolset(store, "Xiami")

	cases := []map[string]any{
		{"category": CategoryFact},                                    // missing content
		{"content": "x"},                                              // missing category
		{"content": "x", "category": "bogus"},                         // invalid category
	}
	for i, args := range cases {
		out := parseToolJSON(t, ts.Store(args))
		if out["ok"] != false {
			t.Errorf("case %d: expected ok=false, got %v", i, out)
		}
		if _, ok := out["error"]; !ok {
			t.Errorf("case %d: missing error field", i)
		}
	}
}

func TestMemoryStore_Tool_ClampsImportance(t *testing.T) {
	store := openTestStore(t)
	ts, _ := NewToolset(store, "Xiami")

	if out := parseToolJSON(t, ts.Store(map[string]any{
		"content":    "out-of-range",
		"category":   CategoryFact,
		"importance": float64(99),
	})); out["importance"] != float64(10) {
		t.Errorf("importance not clamped to 10: %v", out["importance"])
	}
	if out := parseToolJSON(t, ts.Store(map[string]any{
		"content":    "out-of-range-low",
		"category":   CategoryFact,
		"importance": float64(-5),
	})); out["importance"] != float64(1) {
		t.Errorf("importance not clamped to 1: %v", out["importance"])
	}
}

func TestMemoryRecall_Tool_HappyPath(t *testing.T) {
	store := openTestStore(t)
	seed := []Memory{
		{NPCName: "Xiami", Category: CategoryFact, Content: "player likes sunflowers", Importance: 6},
		{NPCName: "Xiami", Category: CategoryPreference, Content: "player hates eggplant", Importance: 4},
		{NPCName: "Xiami", Category: CategoryEvent, Content: "we went fishing in summer", Importance: 5},
	}
	for _, m := range seed {
		_ = store.StoreMemory(m)
	}
	ts, _ := NewToolset(store, "Xiami")

	t.Run("substring search", func(t *testing.T) {
		out := parseToolJSON(t, ts.Recall(map[string]any{"query": "sunflower"}))
		if out["ok"] != true {
			t.Fatalf("recall failed: %v", out)
		}
		mems, _ := out["memories"].([]any)
		if len(mems) != 1 {
			t.Fatalf("want 1 memory, got %d", len(mems))
		}
	})

	t.Run("category filter", func(t *testing.T) {
		out := parseToolJSON(t, ts.Recall(map[string]any{"category": CategoryEvent}))
		mems, _ := out["memories"].([]any)
		if len(mems) != 1 {
			t.Fatalf("want 1 event memory, got %d", len(mems))
		}
	})

	t.Run("limit honoured", func(t *testing.T) {
		out := parseToolJSON(t, ts.Recall(map[string]any{"limit": float64(2)}))
		mems, _ := out["memories"].([]any)
		if len(mems) != 2 {
			t.Fatalf("want 2 memories, got %d", len(mems))
		}
	})

	t.Run("invalid category silently ignored", func(t *testing.T) {
		// Spec: invalid category falls through (no filter), since we don't
		// want a typo to crash the recall path.
		out := parseToolJSON(t, ts.Recall(map[string]any{"category": "nope"}))
		mems, _ := out["memories"].([]any)
		if len(mems) != 3 {
			t.Errorf("want 3 (no filter), got %d", len(mems))
		}
	})
}

func TestMemoryRecall_Tool_BumpsAccessCount(t *testing.T) {
	store := openTestStore(t)
	_ = store.StoreMemory(Memory{
		NPCName: "Xiami", Category: CategoryFact,
		Content: "the player's name is Sam", Importance: 5,
	})
	ts, _ := NewToolset(store, "Xiami")

	_ = ts.Recall(map[string]any{"query": "sam"})

	got, _ := store.GetMemories("Xiami", MemoryQuery{})
	if got[0].AccessCount == 0 {
		t.Errorf("access_count should be bumped after recall")
	}
}

func TestToolset_SchemaShape(t *testing.T) {
	store := openTestStore(t)
	ts, _ := NewToolset(store, "Xiami")

	for _, spec := range []ToolSpec{ts.RecallSpec, ts.StoreSpec} {
		if spec.Name == "" {
			t.Errorf("spec missing name")
		}
		if spec.InputSchema["type"] != "object" {
			t.Errorf("%s: schema type should be 'object'", spec.Name)
		}
		if spec.Description == "" || !strings.Contains(spec.Description, "memory") {
			t.Errorf("%s: description should reference memory", spec.Name)
		}
	}
}
