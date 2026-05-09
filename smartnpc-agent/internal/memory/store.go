package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite" // register the pure-Go SQLite driver
)

// Store is the persistence façade used by the chat agent. All methods are
// safe for concurrent use; the underlying *sql.DB serialises writes via
// SQLite's own locking.
type Store interface {
	// StartConversation creates a new conversation row and returns its id.
	StartConversation(npcName, gameDate string) (int64, error)
	// EndConversation marks the given conversation as finished. Idempotent —
	// repeated calls overwrite the ended_at timestamp.
	EndConversation(convID int64) error
	// AppendMessage stores one transcript turn. Timestamp on the input
	// struct is honoured if non-zero, otherwise time.Now() is used.
	AppendMessage(convID int64, msg Message) error

	// StoreMemory persists a memory row. Importance is clamped to [1,10].
	// Returns the inserted id via the Memory's ID field on success.
	StoreMemory(m Memory) error
	// GetMemories runs a filtered lookup. Empty Categories ⇒ all categories.
	// Results are sorted importance DESC, access_count DESC, created_at DESC.
	GetMemories(npcName string, opts MemoryQuery) ([]Memory, error)
	// ExpireMemory soft-deletes a memory by id.
	ExpireMemory(id int64) error
	// TouchMemory bumps a memory's access_count by 1. Best-effort: missing
	// rows do not return an error.
	TouchMemory(id int64) error

	// StoreSummary persists a conversation summary. KeyTopics is JSON-encoded
	// at storage time and decoded on read.
	StoreSummary(s Summary) error
	// GetRecentSummaries returns the newest `limit` summaries for an NPC,
	// ordered created_at DESC.
	GetRecentSummaries(npcName string, limit int) ([]Summary, error)

	// GetContextBundle assembles the prompt-ready memory digest for an NPC.
	// currentMessage is reserved for future relevance scoring; the current
	// implementation ignores it.
	GetContextBundle(npcName, currentMessage string) (*ContextBundle, error)

	// Close releases the underlying database handle.
	Close() error
}

// sqliteStore is the default Store implementation backed by SQLite via
// modernc.org/sqlite (pure Go, no CGO).
type sqliteStore struct {
	db *sql.DB
	// mu protects against the rare modernc race where a concurrent writer
	// can panic during schema bootstrap on Windows. Once schema is applied
	// we let SQLite handle locking; the mutex stays cheap.
	mu sync.Mutex
}

// Open creates (or opens) a SQLite memory store at the given path. Use
// ":memory:" for an ephemeral in-process database, useful in tests. The
// schema is applied automatically.
func Open(path string) (Store, error) {
	if path == "" {
		return nil, errors.New("memory.Open: empty path")
	}
	// modernc.org/sqlite uses the driver name "sqlite". DSN is the path.
	// For file-backed DBs we enable shared-cache + WAL via PRAGMA in
	// applySchema; for ":memory:" SQLite ignores most of those.
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("memory.Open: %w", err)
	}
	// Single connection for ":memory:" so all callers see the same in-memory
	// schema; otherwise the connection pool may hand out a fresh blank DB.
	if path == ":memory:" {
		db.SetMaxOpenConns(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := applySchema(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &sqliteStore{db: db}, nil
}

// Close shuts down the underlying database handle.
func (s *sqliteStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil
	}
	err := s.db.Close()
	s.db = nil
	return err
}

// --- Conversations ---

func (s *sqliteStore) StartConversation(npcName, gameDate string) (int64, error) {
	if npcName == "" {
		return 0, errors.New("StartConversation: npcName is required")
	}
	startedAt := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := s.db.Exec(
		`INSERT INTO conversations (npc_name, game_date, started_at) VALUES (?, ?, ?)`,
		npcName, gameDate, startedAt,
	)
	if err != nil {
		return 0, fmt.Errorf("StartConversation: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("StartConversation: last insert id: %w", err)
	}
	return id, nil
}

func (s *sqliteStore) EndConversation(convID int64) error {
	if convID <= 0 {
		return errors.New("EndConversation: invalid convID")
	}
	endedAt := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.Exec(`UPDATE conversations SET ended_at = ? WHERE id = ?`, endedAt, convID)
	if err != nil {
		return fmt.Errorf("EndConversation: %w", err)
	}
	return nil
}

// --- Messages ---

func (s *sqliteStore) AppendMessage(convID int64, msg Message) error {
	if convID <= 0 {
		return errors.New("AppendMessage: invalid convID")
	}
	if msg.Role == "" {
		return errors.New("AppendMessage: role is required")
	}
	ts := msg.Timestamp
	if ts.IsZero() {
		ts = time.Now().UTC()
	}
	_, err := s.db.Exec(
		`INSERT INTO messages (conversation_id, role, content, timestamp, tool_calls) VALUES (?, ?, ?, ?, ?)`,
		convID, msg.Role, msg.Content, ts.UTC().Format(time.RFC3339Nano), nullableString(msg.ToolCalls),
	)
	if err != nil {
		return fmt.Errorf("AppendMessage: %w", err)
	}
	return nil
}

// --- Memories ---

func (s *sqliteStore) StoreMemory(m Memory) error {
	if m.NPCName == "" {
		return errors.New("StoreMemory: NPCName is required")
	}
	if m.Category == "" {
		return errors.New("StoreMemory: Category is required")
	}
	if m.Content == "" {
		return errors.New("StoreMemory: Content is required")
	}
	importance := clamp(m.Importance, 1, 10)
	if m.Importance == 0 {
		importance = 5
	}
	createdAt := m.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	_, err := s.db.Exec(
		`INSERT INTO memories (npc_name, category, content, importance, created_at, access_count, expired)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		m.NPCName,
		m.Category,
		m.Content,
		importance,
		createdAt.UTC().Format(time.RFC3339Nano),
		m.AccessCount,
		boolToInt(m.Expired),
	)
	if err != nil {
		return fmt.Errorf("StoreMemory: %w", err)
	}
	return nil
}

func (s *sqliteStore) GetMemories(npcName string, opts MemoryQuery) ([]Memory, error) {
	if npcName == "" {
		return nil, errors.New("GetMemories: npcName is required")
	}
	var (
		conds = []string{"npc_name = ?"}
		args  = []any{npcName}
	)
	if !opts.IncludeExpired {
		conds = append(conds, "expired = 0")
	}
	if len(opts.Categories) > 0 {
		placeholders := make([]string, 0, len(opts.Categories))
		for _, c := range opts.Categories {
			placeholders = append(placeholders, "?")
			args = append(args, c)
		}
		conds = append(conds, "category IN ("+strings.Join(placeholders, ",")+")")
	}
	if opts.MinImportance > 0 {
		conds = append(conds, "importance >= ?")
		args = append(args, opts.MinImportance)
	}
	if opts.Search != "" {
		conds = append(conds, "LOWER(content) LIKE ?")
		args = append(args, "%"+strings.ToLower(opts.Search)+"%")
	}
	q := `SELECT id, npc_name, category, content, importance, created_at, access_count, expired
	      FROM memories WHERE ` + strings.Join(conds, " AND ") +
		` ORDER BY importance DESC, access_count DESC, created_at DESC`
	if opts.Limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", opts.Limit)
	}

	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("GetMemories: %w", err)
	}
	defer rows.Close()

	var out []Memory
	for rows.Next() {
		var (
			m         Memory
			createdAt string
			expired   int
		)
		if err := rows.Scan(&m.ID, &m.NPCName, &m.Category, &m.Content,
			&m.Importance, &createdAt, &m.AccessCount, &expired); err != nil {
			return nil, fmt.Errorf("GetMemories: scan: %w", err)
		}
		m.CreatedAt = parseTime(createdAt)
		m.Expired = expired != 0
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("GetMemories: rows: %w", err)
	}
	return out, nil
}

func (s *sqliteStore) ExpireMemory(id int64) error {
	if id <= 0 {
		return errors.New("ExpireMemory: invalid id")
	}
	_, err := s.db.Exec(`UPDATE memories SET expired = 1 WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("ExpireMemory: %w", err)
	}
	return nil
}

func (s *sqliteStore) TouchMemory(id int64) error {
	if id <= 0 {
		return errors.New("TouchMemory: invalid id")
	}
	_, err := s.db.Exec(`UPDATE memories SET access_count = access_count + 1 WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("TouchMemory: %w", err)
	}
	return nil
}

// --- Summaries ---

func (s *sqliteStore) StoreSummary(sum Summary) error {
	if sum.NPCName == "" {
		return errors.New("StoreSummary: NPCName is required")
	}
	if sum.ConversationID <= 0 {
		return errors.New("StoreSummary: ConversationID is required")
	}
	if sum.Summary == "" {
		return errors.New("StoreSummary: Summary is required")
	}
	createdAt := sum.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	topics, err := json.Marshal(sum.KeyTopics)
	if err != nil {
		return fmt.Errorf("StoreSummary: marshal topics: %w", err)
	}
	_, err = s.db.Exec(
		`INSERT INTO summaries (npc_name, conversation_id, summary, key_topics, emotional_tone, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		sum.NPCName, sum.ConversationID, sum.Summary,
		string(topics), sum.EmotionalTone,
		createdAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("StoreSummary: %w", err)
	}
	return nil
}

func (s *sqliteStore) GetRecentSummaries(npcName string, limit int) ([]Summary, error) {
	if npcName == "" {
		return nil, errors.New("GetRecentSummaries: npcName is required")
	}
	if limit <= 0 {
		limit = 3
	}
	rows, err := s.db.Query(
		`SELECT id, npc_name, conversation_id, summary, key_topics, emotional_tone, created_at
		 FROM summaries WHERE npc_name = ? ORDER BY created_at DESC LIMIT ?`,
		npcName, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("GetRecentSummaries: %w", err)
	}
	defer rows.Close()

	var out []Summary
	for rows.Next() {
		var (
			s         Summary
			topicsRaw sql.NullString
			tone      sql.NullString
			createdAt string
		)
		if err := rows.Scan(&s.ID, &s.NPCName, &s.ConversationID, &s.Summary,
			&topicsRaw, &tone, &createdAt); err != nil {
			return nil, fmt.Errorf("GetRecentSummaries: scan: %w", err)
		}
		s.CreatedAt = parseTime(createdAt)
		if tone.Valid {
			s.EmotionalTone = tone.String
		}
		if topicsRaw.Valid && topicsRaw.String != "" {
			_ = json.Unmarshal([]byte(topicsRaw.String), &s.KeyTopics)
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("GetRecentSummaries: rows: %w", err)
	}
	return out, nil
}

// GetContextBundle is implemented in retriever.go so the assembly logic and
// tests stay together. The Store interface keeps the method on the surface.
func (s *sqliteStore) GetContextBundle(npcName, currentMessage string) (*ContextBundle, error) {
	return assembleContextBundle(s, npcName, currentMessage)
}

// --- helpers ---

func clamp(v, lo, hi int) int {
	switch {
	case v < lo:
		return lo
	case v > hi:
		return hi
	default:
		return v
	}
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// parseTime parses an RFC3339(Nano) timestamp; returns zero on failure to
// keep callers tolerant of dirty data.
func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	return time.Time{}
}

// countConversations returns the lifetime conversation count for an NPC.
// Exposed package-internally so retriever.go can fold it into the bundle.
func countConversations(s *sqliteStore, npcName string) (int, error) {
	row := s.db.QueryRow(`SELECT COUNT(*) FROM conversations WHERE npc_name = ?`, npcName)
	var n int
	if err := row.Scan(&n); err != nil {
		return 0, fmt.Errorf("countConversations: %w", err)
	}
	return n, nil
}
