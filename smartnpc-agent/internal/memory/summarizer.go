package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/smartnpc/smartnpc-agent/internal/llm"
)

// Summarizer turns raw conversation transcripts into the condensed forms the
// retriever needs (summaries + extracted memories). It is intentionally a
// thin wrapper around an llm.Provider so the agent can pick whichever LLM
// (cloud, local, decision-stage) is best for the job.
//
// All methods accept context.Context for cancellation and obey the provider's
// timeout. The caller is responsible for persisting the returned values via
// Store; Summarizer never touches SQLite directly.
type Summarizer struct {
	// Provider is the LLM used for both summary and memory extraction.
	Provider llm.Provider
	// Model overrides the default model name baked into the provider. Empty
	// keeps the provider default.
	Model string
	// Temperature for both prompts. 0 keeps extraction deterministic.
	Temperature float64
	// MaxTokens caps the response length. Defaults to 600 when zero.
	MaxTokens int
}

// summarizerOutput is the JSON shape we ask the LLM to emit for the
// summary prompt.
type summarizerOutput struct {
	Summary       string   `json:"summary"`
	KeyTopics     []string `json:"key_topics"`
	EmotionalTone string   `json:"emotional_tone"`
}

// memoryExtraction is the JSON shape used by ExtractMemories.
type memoryExtraction struct {
	Memories []struct {
		Category   string `json:"category"`
		Content    string `json:"content"`
		Importance int    `json:"importance"`
	} `json:"memories"`
}

// SummarizeConversation asks the LLM to produce a 1-3 sentence prose summary
// of the conversation along with a small set of topic tags and an overall
// emotional tone. The transcript is encoded inline into the user prompt.
//
// Returns (summary, key_topics, emotional_tone, error). On any LLM error or
// JSON parse failure the function returns the wrapped error and zero values
// for the other returns; the caller may then decide to retry or skip.
func (s *Summarizer) SummarizeConversation(ctx context.Context, messages []Message) (string, []string, string, error) {
	if s == nil || s.Provider == nil {
		return "", nil, "", fmt.Errorf("Summarizer: provider not configured")
	}
	if len(messages) == 0 {
		return "", nil, "", nil
	}

	transcript := renderTranscript(messages)

	maxTokens := s.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 600
	}

	req := llm.ChatRequest{
		Model: s.Model,
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: summarizerSystemPrompt},
			{Role: llm.RoleUser, Content: "Conversation transcript:\n" + transcript +
				"\n\nReturn ONLY a JSON object matching this shape:\n" +
				`{"summary":"...","key_topics":["..."],"emotional_tone":"..."}`},
		},
		Temperature: s.Temperature,
		MaxTokens:   maxTokens,
	}
	resp, err := s.Provider.Chat(ctx, req)
	if err != nil {
		return "", nil, "", fmt.Errorf("Summarizer.Summarize: chat: %w", err)
	}
	out, err := parseSummarizerOutput(resp.Content)
	if err != nil {
		return "", nil, "", err
	}
	return out.Summary, out.KeyTopics, out.EmotionalTone, nil
}

// ExtractMemories asks the LLM to scan the transcript and return a list of
// durable memories worth persisting. Each memory carries a category from
// AllCategories and an importance score in 1..10. Out-of-range values are
// clamped before return.
func (s *Summarizer) ExtractMemories(ctx context.Context, npcName string, messages []Message) ([]Memory, error) {
	if s == nil || s.Provider == nil {
		return nil, fmt.Errorf("Summarizer: provider not configured")
	}
	if npcName == "" {
		return nil, fmt.Errorf("ExtractMemories: npcName is required")
	}
	if len(messages) == 0 {
		return nil, nil
	}

	transcript := renderTranscript(messages)

	maxTokens := s.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 800
	}

	req := llm.ChatRequest{
		Model: s.Model,
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: extractorSystemPrompt},
			{Role: llm.RoleUser, Content: "NPC: " + npcName +
				"\n\nConversation transcript:\n" + transcript +
				"\n\nReturn ONLY a JSON object matching:\n" +
				`{"memories":[{"category":"fact|preference|event|relationship|promise","content":"...","importance":1-10}]}`},
		},
		Temperature: s.Temperature,
		MaxTokens:   maxTokens,
	}
	resp, err := s.Provider.Chat(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("Summarizer.Extract: chat: %w", err)
	}

	parsed, err := parseMemoryExtraction(resp.Content)
	if err != nil {
		return nil, err
	}
	out := make([]Memory, 0, len(parsed.Memories))
	for _, m := range parsed.Memories {
		cat := strings.ToLower(strings.TrimSpace(m.Category))
		if !isValidCategory(cat) {
			continue
		}
		content := strings.TrimSpace(m.Content)
		if content == "" {
			continue
		}
		out = append(out, Memory{
			NPCName:    npcName,
			Category:   cat,
			Content:    content,
			Importance: clamp(m.Importance, 1, 10),
		})
	}
	return out, nil
}

// renderTranscript flattens a slice of Messages into a plain-text dialogue
// the LLM can read. We intentionally omit tool_calls JSON to keep the prompt
// small; the summary should focus on what was said, not on bridge plumbing.
func renderTranscript(messages []Message) string {
	var sb strings.Builder
	for _, m := range messages {
		role := strings.ToLower(strings.TrimSpace(m.Role))
		if role == "system" {
			continue
		}
		if role == "" {
			role = "unknown"
		}
		content := strings.TrimSpace(m.Content)
		if content == "" {
			continue
		}
		sb.WriteString(role)
		sb.WriteString(": ")
		sb.WriteString(content)
		sb.WriteString("\n")
	}
	return sb.String()
}

// parseSummarizerOutput tolerates code-fenced JSON ("```json\n{...}\n```")
// and stray prose by extracting the first {...} block.
func parseSummarizerOutput(raw string) (summarizerOutput, error) {
	body := extractJSONObject(raw)
	if body == "" {
		return summarizerOutput{}, fmt.Errorf("summarizer: no JSON object in response: %q", truncateForErr(raw))
	}
	var out summarizerOutput
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		return summarizerOutput{}, fmt.Errorf("summarizer: decode JSON: %w", err)
	}
	return out, nil
}

func parseMemoryExtraction(raw string) (memoryExtraction, error) {
	body := extractJSONObject(raw)
	if body == "" {
		return memoryExtraction{}, fmt.Errorf("extractor: no JSON object in response: %q", truncateForErr(raw))
	}
	var out memoryExtraction
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		return memoryExtraction{}, fmt.Errorf("extractor: decode JSON: %w", err)
	}
	return out, nil
}

// extractJSONObject finds the first balanced {...} substring. Naïve enough
// for LLM responses we control via prompt; production code should swap in a
// streaming JSON scanner.
func extractJSONObject(s string) string {
	start := strings.Index(s, "{")
	if start < 0 {
		return ""
	}
	depth := 0
	inString := false
	escape := false
	for i := start; i < len(s); i++ {
		c := s[i]
		if escape {
			escape = false
			continue
		}
		if c == '\\' {
			escape = true
			continue
		}
		if c == '"' {
			inString = !inString
			continue
		}
		if inString {
			continue
		}
		switch c {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return ""
}

func isValidCategory(c string) bool {
	for _, v := range AllCategories {
		if v == c {
			return true
		}
	}
	return false
}

const summarizerSystemPrompt = `You are a memory summarizer for a Stardew Valley AI NPC.
Read the conversation between the player and the NPC, then output a short JSON object with three fields:
  - summary: 1-3 sentences in the third person, focused on what was said and decided.
  - key_topics: an array of 1-5 short tags (lower case, English).
  - emotional_tone: one short phrase (e.g. "warm", "tense", "playful").
Output ONLY the JSON object, no preamble, no code fence.`

const extractorSystemPrompt = `You are a memory extractor for a Stardew Valley AI NPC. Your job is to find facts the NPC should remember next time they talk to the player.
Output ONLY a JSON object with a "memories" array. Each memory has:
  - category: one of "fact", "preference", "event", "relationship", "promise".
  - content: a short, concrete sentence in the NPC's voice (≤ 25 words, English).
  - importance: integer 1..10. 1=trivia, 5=worth remembering, 8+=relationship-defining.
Skip greetings, small talk, and anything the NPC could not credibly remember.
If nothing is worth remembering, output {"memories": []}.`

// AllCategoriesAsList is a convenience wrapper used by prompt templates.
// Kept here so changes to AllCategories propagate without editing prompt
// strings.
func AllCategoriesAsList() string {
	return strings.Join(AllCategories, ", ")
}
