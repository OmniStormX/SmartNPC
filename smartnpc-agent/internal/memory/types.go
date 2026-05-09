// Package memory implements the SmartNPC long-term memory system.
//
// The store is intentionally agent-side: the MCP server stays stateless and
// every NPC's persistent state (conversations, summaries, extracted facts)
// lives in a single SQLite database under the agent process. The package is
// split into:
//
//   - types.go      — wire structs and category constants.
//   - schema.go     — DDL + migrations.
//   - store.go      — Store interface and SQLite implementation.
//   - retriever.go  — assembles ContextBundle from the raw rows.
//   - summarizer.go — LLM-driven summary / memory extraction.
//
// All exported symbols use English; in-line code comments use English so
// downstream tooling (godoc, IDE hovers) stays useful.
package memory

import "time"

// Category enumerates the five memory categories the system tracks. A memory
// row's category is stored as plain text so future categories can be added
// without a schema migration; the constants below are the canonical values.
const (
	// CategoryFact captures stable facts about the player or world.
	CategoryFact = "fact"
	// CategoryPreference records likes / dislikes.
	CategoryPreference = "preference"
	// CategoryEvent records discrete events that happened in-game.
	CategoryEvent = "event"
	// CategoryRelationship captures relational stance (always retrieved).
	CategoryRelationship = "relationship"
	// CategoryPromise tracks open commitments the NPC made to the player.
	CategoryPromise = "promise"
)

// AllCategories is the canonical list of memory categories. Useful for
// validation and prompt rendering.
var AllCategories = []string{
	CategoryFact,
	CategoryPreference,
	CategoryEvent,
	CategoryRelationship,
	CategoryPromise,
}

// Memory is a single persisted fact / preference / event / relationship /
// promise. Importance is a 1-10 score used for ranking; AccessCount is bumped
// every time the row is included in a ContextBundle so frequently-recalled
// memories surface first.
type Memory struct {
	ID          int64     `json:"id"`
	NPCName     string    `json:"npc_name"`
	Category    string    `json:"category"`
	Content     string    `json:"content"`
	Importance  int       `json:"importance"`
	CreatedAt   time.Time `json:"created_at"`
	AccessCount int       `json:"access_count"`
	Expired     bool      `json:"expired"`
}

// Summary is the LLM-generated condensation of a finished conversation.
// KeyTopics is stored as a JSON-encoded []string in the DB but exposed here
// as a typed slice for ergonomics.
type Summary struct {
	ID             int64     `json:"id"`
	NPCName        string    `json:"npc_name"`
	ConversationID int64     `json:"conversation_id"`
	Summary        string    `json:"summary"`
	KeyTopics      []string  `json:"key_topics"`
	EmotionalTone  string    `json:"emotional_tone"`
	CreatedAt      time.Time `json:"created_at"`
}

// Message is a single turn appended to a conversation transcript. ToolCalls
// is the raw JSON of any tool invocations the assistant requested; storing
// the JSON verbatim avoids round-tripping the structured arguments map and
// keeps the schema free of further nested tables.
type Message struct {
	ID             int64     `json:"id"`
	ConversationID int64     `json:"conversation_id"`
	Role           string    `json:"role"`
	Content        string    `json:"content"`
	Timestamp      time.Time `json:"timestamp"`
	ToolCalls      string    `json:"tool_calls,omitempty"`
}

// Conversation tracks a single chat session. EndedAt is the zero value while
// the conversation is still in progress.
type Conversation struct {
	ID        int64     `json:"id"`
	NPCName   string    `json:"npc_name"`
	GameDate  string    `json:"game_date"`
	StartedAt time.Time `json:"started_at"`
	EndedAt   time.Time `json:"ended_at,omitempty"`
}

// ContextBundle is the digest fed into an NPC system prompt before each turn.
// It packages the last few summaries, the highest-priority memories, the
// always-include relationship facts and the lifetime conversation count so
// the LLM can ground itself in what it "remembers" about the player.
type ContextBundle struct {
	RecentSummary      []Summary `json:"recent_summary"`
	KeyMemories        []Memory  `json:"key_memories"`
	RelationshipFacts  []Memory  `json:"relationship_facts"`
	TotalConversations int       `json:"total_conversations"`
}

// MemoryQuery filters the memory table on read. Empty fields mean "no
// filter"; Limit <= 0 means "no limit".
type MemoryQuery struct {
	Categories    []string
	MinImportance int
	Limit         int
	Search        string
	IncludeExpired bool
}
