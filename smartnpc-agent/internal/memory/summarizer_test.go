package memory

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/smartnpc/smartnpc-agent/internal/llm"
)

// fakeProvider is a deterministic llm.Provider used by the summarizer tests.
// Setting Reply makes Chat return that string as Content; Err overrides
// everything to surface a forced error.
type fakeProvider struct {
	Reply string
	Err   error
}

func (f *fakeProvider) Name() string { return "fake" }
func (f *fakeProvider) Chat(_ context.Context, _ llm.ChatRequest) (*llm.ChatResponse, error) {
	if f.Err != nil {
		return nil, f.Err
	}
	return &llm.ChatResponse{Content: f.Reply, FinishReason: "stop"}, nil
}

func TestSummarizer_SummarizeConversation(t *testing.T) {
	cases := []struct {
		name      string
		reply     string
		err       error
		wantSum   string
		wantTopic string
		wantTone  string
		wantErr   bool
	}{
		{
			name:      "plain JSON",
			reply:     `{"summary":"S","key_topics":["a","b"],"emotional_tone":"warm"}`,
			wantSum:   "S",
			wantTopic: "a",
			wantTone:  "warm",
		},
		{
			name:      "code-fenced JSON",
			reply:     "```json\n{\"summary\":\"S\",\"key_topics\":[\"a\"],\"emotional_tone\":\"warm\"}\n```",
			wantSum:   "S",
			wantTopic: "a",
			wantTone:  "warm",
		},
		{
			name:    "provider error",
			err:     errors.New("boom"),
			wantErr: true,
		},
		{
			name:    "no JSON in response",
			reply:   "no json here",
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &Summarizer{Provider: &fakeProvider{Reply: tc.reply, Err: tc.err}}
			summary, topics, tone, err := s.SummarizeConversation(
				context.Background(),
				[]Message{{Role: "user", Content: "hi"}, {Role: "assistant", Content: "hello"}},
			)
			if tc.wantErr {
				if err == nil {
					t.Fatal("want error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if summary != tc.wantSum {
				t.Errorf("summary=%q want %q", summary, tc.wantSum)
			}
			if tc.wantTopic != "" {
				if len(topics) == 0 || topics[0] != tc.wantTopic {
					t.Errorf("topics=%v want first=%q", topics, tc.wantTopic)
				}
			}
			if tone != tc.wantTone {
				t.Errorf("tone=%q want %q", tone, tc.wantTone)
			}
		})
	}
}

func TestSummarizer_ExtractMemories(t *testing.T) {
	t.Run("happy path", func(t *testing.T) {
		s := &Summarizer{Provider: &fakeProvider{
			Reply: `{"memories":[
				{"category":"fact","content":"player loves spring","importance":7},
				{"category":"PROMISE","content":"bring tea tomorrow","importance":11},
				{"category":"unknown","content":"should drop","importance":5},
				{"category":"event","content":"   ","importance":4}
			]}`,
		}}
		got, err := s.ExtractMemories(context.Background(), "Xiami",
			[]Message{{Role: "user", Content: "hi"}})
		if err != nil {
			t.Fatalf("ExtractMemories: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("want 2 valid memories (drop unknown + empty), got %d: %v", len(got), got)
		}
		// Verify normalization: lowercased category, clamped importance, npc fixed.
		for _, m := range got {
			if m.NPCName != "Xiami" {
				t.Errorf("NPCName not set: %v", m)
			}
		}
		// promise with importance 11 → clamped to 10
		var promise *Memory
		for i := range got {
			if got[i].Category == CategoryPromise {
				promise = &got[i]
			}
		}
		if promise == nil {
			t.Fatal("promise category dropped")
		}
		if promise.Importance != 10 {
			t.Errorf("importance not clamped: got %d want 10", promise.Importance)
		}
	})

	t.Run("empty messages short-circuits", func(t *testing.T) {
		s := &Summarizer{Provider: &fakeProvider{Reply: "{}"}}
		got, err := s.ExtractMemories(context.Background(), "Xiami", nil)
		if err != nil {
			t.Fatalf("ExtractMemories: %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("want 0, got %d", len(got))
		}
	})

	t.Run("empty npc rejects", func(t *testing.T) {
		s := &Summarizer{Provider: &fakeProvider{Reply: "{}"}}
		if _, err := s.ExtractMemories(context.Background(), "",
			[]Message{{Role: "user", Content: "hi"}}); err == nil {
			t.Fatal("want error for empty NPC")
		}
	})

	t.Run("unconfigured provider", func(t *testing.T) {
		var s *Summarizer
		if _, err := s.ExtractMemories(context.Background(), "Xiami",
			[]Message{{Role: "user", Content: "hi"}}); err == nil {
			t.Fatal("want error for nil summarizer")
		}
	})
}

// TestAllCategoriesAsList ties down the prompt-template helper so a future
// renaming of category constants doesn't silently rewrite the LLM prompt.
func TestAllCategoriesAsList(t *testing.T) {
	got := AllCategoriesAsList()
	for _, c := range AllCategories {
		if !strings.Contains(got, c) {
			t.Errorf("%q missing from AllCategoriesAsList: %q", c, got)
		}
	}
}
