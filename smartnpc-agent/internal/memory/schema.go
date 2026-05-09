package memory

import (
	"context"
	"database/sql"
	"fmt"
)

// schemaStatements is the canonical DDL applied at open time. Statements are
// idempotent (CREATE TABLE IF NOT EXISTS / CREATE INDEX IF NOT EXISTS) so
// running them on every open is safe and cheap.
var schemaStatements = []string{
	`CREATE TABLE IF NOT EXISTS conversations (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		npc_name TEXT NOT NULL,
		game_date TEXT,
		started_at TEXT NOT NULL,
		ended_at TEXT
	)`,
	`CREATE TABLE IF NOT EXISTS messages (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		conversation_id INTEGER NOT NULL,
		role TEXT NOT NULL,
		content TEXT NOT NULL,
		timestamp TEXT NOT NULL,
		tool_calls TEXT,
		FOREIGN KEY(conversation_id) REFERENCES conversations(id)
	)`,
	`CREATE TABLE IF NOT EXISTS memories (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		npc_name TEXT NOT NULL,
		category TEXT NOT NULL,
		content TEXT NOT NULL,
		importance INTEGER NOT NULL DEFAULT 5,
		created_at TEXT NOT NULL,
		access_count INTEGER NOT NULL DEFAULT 0,
		expired INTEGER NOT NULL DEFAULT 0
	)`,
	`CREATE TABLE IF NOT EXISTS summaries (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		npc_name TEXT NOT NULL,
		conversation_id INTEGER NOT NULL,
		summary TEXT NOT NULL,
		key_topics TEXT,
		emotional_tone TEXT,
		created_at TEXT NOT NULL,
		FOREIGN KEY(conversation_id) REFERENCES conversations(id)
	)`,
	`CREATE INDEX IF NOT EXISTS idx_memories_npc ON memories(npc_name, expired)`,
	`CREATE INDEX IF NOT EXISTS idx_messages_conv ON messages(conversation_id)`,
	`CREATE INDEX IF NOT EXISTS idx_summaries_npc ON summaries(npc_name)`,
	`CREATE INDEX IF NOT EXISTS idx_conversations_npc ON conversations(npc_name)`,
}

// pragmaStatements tunes the SQLite session for typical SmartNPC workloads:
// WAL for concurrent readers + writer, NORMAL sync for performance vs. crash
// safety, and foreign keys for referential integrity.
var pragmaStatements = []string{
	`PRAGMA journal_mode = WAL`,
	`PRAGMA synchronous = NORMAL`,
	`PRAGMA foreign_keys = ON`,
	`PRAGMA busy_timeout = 5000`,
}

// applySchema runs all DDL + pragmas in a single context-aware transaction.
// modernc.org/sqlite is happy with WAL inside a transaction so long as the
// PRAGMA executes before any writes.
func applySchema(ctx context.Context, db *sql.DB) error {
	for _, p := range pragmaStatements {
		if _, err := db.ExecContext(ctx, p); err != nil {
			return fmt.Errorf("apply pragma %q: %w", p, err)
		}
	}
	for _, s := range schemaStatements {
		if _, err := db.ExecContext(ctx, s); err != nil {
			return fmt.Errorf("apply schema %q: %w", truncateForErr(s), err)
		}
	}
	return nil
}

// truncateForErr keeps long DDL strings out of error messages without losing
// the discriminating prefix.
func truncateForErr(s string) string {
	const max = 60
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
