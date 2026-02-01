package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/cockroachdb/errors"
	_ "github.com/mattn/go-sqlite3"
)

// SQLiteStorage implements Storage interface using SQLite
type SQLiteStorage struct {
	db *sql.DB
}

// NewSQLiteStorage creates a new SQLite storage instance
func NewSQLiteStorage(dbPath string) (*SQLiteStorage, error) {
	// Expand ~ to home directory
	if strings.HasPrefix(dbPath, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, errors.Wrap(err, "get home directory")
		}
		dbPath = filepath.Join(home, dbPath[2:])
	}

	// Ensure directory exists
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, errors.Wrapf(err, "create directory: %s", dir)
	}

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, errors.Wrap(err, "open database")
	}

	storage := &SQLiteStorage{db: db}
	if err := storage.migrate(); err != nil {
		db.Close()
		return nil, errors.Wrap(err, "migrate database")
	}

	return storage, nil
}

// migrate creates tables if they don't exist
func (s *SQLiteStorage) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS conversations (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		session_id TEXT NOT NULL,
		timestamp DATETIME NOT NULL,
		query TEXT NOT NULL,
		response TEXT NOT NULL,
		metadata TEXT
	);

	CREATE INDEX IF NOT EXISTS idx_conversations_session ON conversations(session_id);
	CREATE INDEX IF NOT EXISTS idx_conversations_timestamp ON conversations(timestamp);

	CREATE TABLE IF NOT EXISTS facts (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		category TEXT NOT NULL,
		text TEXT NOT NULL,
		confidence REAL NOT NULL,
		source_ids TEXT NOT NULL,
		created_at DATETIME NOT NULL,
		updated_at DATETIME NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_facts_category ON facts(category);
	CREATE INDEX IF NOT EXISTS idx_facts_updated ON facts(updated_at);
	`

	_, err := s.db.Exec(schema)
	return errors.Wrap(err, "execute schema")
}

// SaveConversation stores a conversation
func (s *SQLiteStorage) SaveConversation(ctx context.Context, conv Conversation) error {
	metadata := ""
	if len(conv.Metadata) > 0 {
		b, err := json.Marshal(conv.Metadata)
		if err != nil {
			return errors.Wrap(err, "marshal metadata")
		}
		metadata = string(b)
	}

	query := `INSERT INTO conversations (session_id, timestamp, query, response, metadata)
	          VALUES (?, ?, ?, ?, ?)`

	_, err := s.db.ExecContext(ctx, query,
		conv.SessionID,
		conv.Timestamp,
		conv.Query,
		conv.Response,
		metadata,
	)

	return errors.Wrap(err, "insert conversation")
}

// LoadConversations retrieves conversations matching filter
func (s *SQLiteStorage) LoadConversations(ctx context.Context, filter Filter) ([]Conversation, error) {
	query := "SELECT id, session_id, timestamp, query, response, metadata FROM conversations WHERE 1=1"
	args := []interface{}{}

	if filter.SessionID != "" {
		query += " AND session_id = ?"
		args = append(args, filter.SessionID)
	}

	if !filter.Since.IsZero() {
		query += " AND timestamp >= ?"
		args = append(args, filter.Since)
	}

	query += " ORDER BY timestamp DESC"

	if filter.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filter.Limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, errors.Wrap(err, "query conversations")
	}
	defer rows.Close()

	var conversations []Conversation
	for rows.Next() {
		var conv Conversation
		var metadataStr sql.NullString

		if err := rows.Scan(&conv.ID, &conv.SessionID, &conv.Timestamp, &conv.Query, &conv.Response, &metadataStr); err != nil {
			return nil, errors.Wrap(err, "scan conversation")
		}

		if metadataStr.Valid && metadataStr.String != "" {
			if err := json.Unmarshal([]byte(metadataStr.String), &conv.Metadata); err != nil {
				return nil, errors.Wrap(err, "unmarshal metadata")
			}
		}

		conversations = append(conversations, conv)
	}

	return conversations, errors.Wrap(rows.Err(), "iterate conversations")
}

// SaveFact stores a fact
func (s *SQLiteStorage) SaveFact(ctx context.Context, fact Fact) error {
	sourceIDs, err := json.Marshal(fact.SourceIDs)
	if err != nil {
		return errors.Wrap(err, "marshal source_ids")
	}

	query := `INSERT INTO facts (category, text, confidence, source_ids, created_at, updated_at)
	          VALUES (?, ?, ?, ?, ?, ?)`

	_, err = s.db.ExecContext(ctx, query,
		fact.Category,
		fact.Text,
		fact.Confidence,
		string(sourceIDs),
		fact.CreatedAt,
		fact.UpdatedAt,
	)

	return errors.Wrap(err, "insert fact")
}

// LoadFacts retrieves facts matching filter
func (s *SQLiteStorage) LoadFacts(ctx context.Context, filter Filter) ([]Fact, error) {
	query := "SELECT id, category, text, confidence, source_ids, created_at, updated_at FROM facts WHERE 1=1"
	args := []interface{}{}

	if len(filter.Categories) > 0 {
		placeholders := make([]string, len(filter.Categories))
		for i, cat := range filter.Categories {
			placeholders[i] = "?"
			args = append(args, cat)
		}
		query += " AND category IN (" + strings.Join(placeholders, ",") + ")"
	}

	query += " ORDER BY updated_at DESC"

	if filter.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filter.Limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, errors.Wrap(err, "query facts")
	}
	defer rows.Close()

	var facts []Fact
	for rows.Next() {
		var fact Fact
		var sourceIDsStr string

		if err := rows.Scan(&fact.ID, &fact.Category, &fact.Text, &fact.Confidence, &sourceIDsStr, &fact.CreatedAt, &fact.UpdatedAt); err != nil {
			return nil, errors.Wrap(err, "scan fact")
		}

		if err := json.Unmarshal([]byte(sourceIDsStr), &fact.SourceIDs); err != nil {
			return nil, errors.Wrap(err, "unmarshal source_ids")
		}

		facts = append(facts, fact)
	}

	return facts, errors.Wrap(rows.Err(), "iterate facts")
}

// Close closes the database connection
func (s *SQLiteStorage) Close() error {
	return errors.Wrap(s.db.Close(), "close database")
}
