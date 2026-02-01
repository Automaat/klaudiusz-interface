package memory

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewSQLiteStorage(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	storage, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStorage failed: %v", err)
	}
	defer storage.Close()

	if storage.db == nil {
		t.Fatal("database connection is nil")
	}
}

func TestNewSQLiteStorage_TildeExpansion(t *testing.T) {
	storage, err := NewSQLiteStorage("~/test-claude-memory.db")
	if err != nil {
		t.Fatalf("NewSQLiteStorage with ~ failed: %v", err)
	}
	defer storage.Close()

	// Cleanup
	home, _ := os.UserHomeDir()
	_ = os.Remove(filepath.Join(home, "test-claude-memory.db"))
}

func TestNewSQLiteStorage_DirectoryCreation(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "nested", "dir", "test.db")

	storage, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStorage with nested dir failed: %v", err)
	}
	defer storage.Close()

	// Verify directory was created
	dir := filepath.Dir(dbPath)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Error("directory was not created")
	}
}

func TestSaveAndLoadConversations(t *testing.T) {
	storage := newTestStorage(t)
	defer storage.Close()

	ctx := context.Background()
	now := time.Now()

	conv := Conversation{
		SessionID: "test-session",
		Timestamp: now,
		Query:     "Co tam?",
		Response:  "Wszystko dobrze!",
		Metadata:  map[string]string{"source": "telegram"},
	}

	// Save conversation
	if err := storage.SaveConversation(ctx, conv); err != nil {
		t.Fatalf("SaveConversation failed: %v", err)
	}

	// Load conversations
	conversations, err := storage.LoadConversations(ctx, Filter{
		SessionID: "test-session",
	})
	if err != nil {
		t.Fatalf("LoadConversations failed: %v", err)
	}

	if len(conversations) != 1 {
		t.Fatalf("expected 1 conversation, got %d", len(conversations))
	}

	loaded := conversations[0]
	if loaded.Query != conv.Query {
		t.Errorf("expected query %q, got %q", conv.Query, loaded.Query)
	}

	if loaded.Response != conv.Response {
		t.Errorf("expected response %q, got %q", conv.Response, loaded.Response)
	}

	if loaded.Metadata["source"] != "telegram" {
		t.Errorf("metadata not preserved")
	}
}

func TestSaveConversation_EmptyMetadata(t *testing.T) {
	storage := newTestStorage(t)
	defer storage.Close()

	ctx := context.Background()

	conv := Conversation{
		SessionID: "test-session",
		Timestamp: time.Now(),
		Query:     "Test query",
		Response:  "Test response",
		Metadata:  nil, // Empty metadata
	}

	if err := storage.SaveConversation(ctx, conv); err != nil {
		t.Fatalf("SaveConversation with nil metadata failed: %v", err)
	}

	// Load and verify
	conversations, err := storage.LoadConversations(ctx, Filter{Limit: 1})
	if err != nil {
		t.Fatalf("LoadConversations failed: %v", err)
	}

	if len(conversations) != 1 {
		t.Fatalf("expected 1 conversation, got %d", len(conversations))
	}

	if conversations[0].Metadata == nil {
		conversations[0].Metadata = make(map[string]string)
	}
}

func TestLoadConversations_FilterBySessionID(t *testing.T) {
	storage := newTestStorage(t)
	defer storage.Close()

	ctx := context.Background()

	// Save conversations from different sessions
	sessions := []string{"session-1", "session-2", "session-3"}
	for i, sid := range sessions {
		conv := Conversation{
			SessionID: sid,
			Timestamp: time.Now().Add(time.Duration(i) * time.Second),
			Query:     "Query " + sid,
			Response:  "Response " + sid,
		}
		if err := storage.SaveConversation(ctx, conv); err != nil {
			t.Fatalf("SaveConversation failed: %v", err)
		}
	}

	// Load only session-2
	conversations, err := storage.LoadConversations(ctx, Filter{
		SessionID: "session-2",
	})
	if err != nil {
		t.Fatalf("LoadConversations failed: %v", err)
	}

	if len(conversations) != 1 {
		t.Fatalf("expected 1 conversation, got %d", len(conversations))
	}

	if conversations[0].SessionID != "session-2" {
		t.Errorf("expected session-2, got %s", conversations[0].SessionID)
	}
}

func TestLoadConversations_FilterBySince(t *testing.T) {
	storage := newTestStorage(t)
	defer storage.Close()

	ctx := context.Background()
	now := time.Now()

	// Save conversations at different times
	for i := 0; i < 3; i++ {
		conv := Conversation{
			SessionID: "test",
			Timestamp: now.Add(time.Duration(i-2) * time.Hour), // -2h, -1h, 0h
			Query:     "Query",
			Response:  "Response",
		}
		if err := storage.SaveConversation(ctx, conv); err != nil {
			t.Fatalf("SaveConversation failed: %v", err)
		}
	}

	// Load only conversations from last hour
	conversations, err := storage.LoadConversations(ctx, Filter{
		Since: now.Add(-90 * time.Minute),
	})
	if err != nil {
		t.Fatalf("LoadConversations failed: %v", err)
	}

	// Should get last 2 conversations
	if len(conversations) < 1 {
		t.Errorf("expected at least 1 conversation, got %d", len(conversations))
	}
}

func TestLoadConversations_Limit(t *testing.T) {
	storage := newTestStorage(t)
	defer storage.Close()

	ctx := context.Background()

	// Save 5 conversations
	for i := 0; i < 5; i++ {
		conv := Conversation{
			SessionID: "test",
			Timestamp: time.Now().Add(time.Duration(i) * time.Second),
			Query:     "Query",
			Response:  "Response",
		}
		if err := storage.SaveConversation(ctx, conv); err != nil {
			t.Fatalf("SaveConversation failed: %v", err)
		}
	}

	// Load with limit
	conversations, err := storage.LoadConversations(ctx, Filter{
		Limit: 3,
	})
	if err != nil {
		t.Fatalf("LoadConversations failed: %v", err)
	}

	if len(conversations) != 3 {
		t.Fatalf("expected 3 conversations, got %d", len(conversations))
	}
}

func TestSaveAndLoadFacts(t *testing.T) {
	storage := newTestStorage(t)
	defer storage.Close()

	ctx := context.Background()
	now := time.Now()

	fact := Fact{
		Category:   CategoryPreference,
		Text:       "Użytkownik lubi kawę z mlekiem",
		Confidence: 0.95,
		SourceIDs:  []int64{1, 2, 3},
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	// Save fact
	if err := storage.SaveFact(ctx, fact); err != nil {
		t.Fatalf("SaveFact failed: %v", err)
	}

	// Load facts
	facts, err := storage.LoadFacts(ctx, Filter{})
	if err != nil {
		t.Fatalf("LoadFacts failed: %v", err)
	}

	if len(facts) != 1 {
		t.Fatalf("expected 1 fact, got %d", len(facts))
	}

	loaded := facts[0]
	if loaded.Text != fact.Text {
		t.Errorf("expected text %q, got %q", fact.Text, loaded.Text)
	}

	if loaded.Confidence != fact.Confidence {
		t.Errorf("expected confidence %f, got %f", fact.Confidence, loaded.Confidence)
	}

	if len(loaded.SourceIDs) != 3 {
		t.Errorf("expected 3 source IDs, got %d", len(loaded.SourceIDs))
	}
}

func TestLoadFacts_FilterByCategory(t *testing.T) {
	storage := newTestStorage(t)
	defer storage.Close()

	ctx := context.Background()
	now := time.Now()

	// Save facts in different categories
	categories := []FactCategory{
		CategoryPreference,
		CategoryHAPattern,
		CategoryKnowledge,
	}

	for _, cat := range categories {
		fact := Fact{
			Category:   cat,
			Text:       "Fact for " + string(cat),
			Confidence: 0.9,
			SourceIDs:  []int64{1},
			CreatedAt:  now,
			UpdatedAt:  now,
		}
		if err := storage.SaveFact(ctx, fact); err != nil {
			t.Fatalf("SaveFact failed: %v", err)
		}
	}

	// Load only preferences
	facts, err := storage.LoadFacts(ctx, Filter{
		Categories: []FactCategory{CategoryPreference},
	})
	if err != nil {
		t.Fatalf("LoadFacts failed: %v", err)
	}

	if len(facts) != 1 {
		t.Fatalf("expected 1 fact, got %d", len(facts))
	}

	if facts[0].Category != CategoryPreference {
		t.Errorf("expected preference category, got %s", facts[0].Category)
	}
}

func TestLoadFacts_FilterByMultipleCategories(t *testing.T) {
	storage := newTestStorage(t)
	defer storage.Close()

	ctx := context.Background()
	now := time.Now()

	// Save facts in different categories
	categories := []FactCategory{
		CategoryPreference,
		CategoryHAPattern,
		CategoryKnowledge,
		CategorySchedule,
	}

	for _, cat := range categories {
		fact := Fact{
			Category:   cat,
			Text:       "Fact for " + string(cat),
			Confidence: 0.9,
			SourceIDs:  []int64{1},
			CreatedAt:  now,
			UpdatedAt:  now,
		}
		if err := storage.SaveFact(ctx, fact); err != nil {
			t.Fatalf("SaveFact failed: %v", err)
		}
	}

	// Load preferences and schedules
	facts, err := storage.LoadFacts(ctx, Filter{
		Categories: []FactCategory{CategoryPreference, CategorySchedule},
	})
	if err != nil {
		t.Fatalf("LoadFacts failed: %v", err)
	}

	if len(facts) != 2 {
		t.Fatalf("expected 2 facts, got %d", len(facts))
	}
}

func TestLoadFacts_Limit(t *testing.T) {
	storage := newTestStorage(t)
	defer storage.Close()

	ctx := context.Background()
	now := time.Now()

	// Save 5 facts
	for i := 0; i < 5; i++ {
		fact := Fact{
			Category:   CategoryKnowledge,
			Text:       "Fact",
			Confidence: 0.9,
			SourceIDs:  []int64{1},
			CreatedAt:  now,
			UpdatedAt:  now.Add(time.Duration(i) * time.Second),
		}
		if err := storage.SaveFact(ctx, fact); err != nil {
			t.Fatalf("SaveFact failed: %v", err)
		}
	}

	// Load with limit
	facts, err := storage.LoadFacts(ctx, Filter{
		Limit: 3,
	})
	if err != nil {
		t.Fatalf("LoadFacts failed: %v", err)
	}

	if len(facts) != 3 {
		t.Fatalf("expected 3 facts, got %d", len(facts))
	}
}

func TestMarkExtracted(t *testing.T) {
	storage := newTestStorage(t)
	defer storage.Close()

	ctx := context.Background()

	// Save conversations
	for i := 0; i < 3; i++ {
		conv := Conversation{
			SessionID: "test",
			Timestamp: time.Now(),
			Query:     "Query",
			Response:  "Response",
		}
		if err := storage.SaveConversation(ctx, conv); err != nil {
			t.Fatalf("SaveConversation failed: %v", err)
		}
	}

	// Load conversations
	convs, err := storage.LoadConversations(ctx, Filter{})
	if err != nil {
		t.Fatalf("LoadConversations failed: %v", err)
	}

	if len(convs) != 3 {
		t.Fatalf("expected 3 conversations, got %d", len(convs))
	}

	// Verify not extracted initially
	for _, conv := range convs {
		if conv.ExtractedAt != nil {
			t.Error("conversation should not be marked as extracted initially")
		}
	}

	// Mark first 2 as extracted
	extractedAt := time.Now()
	convIDs := []int64{convs[0].ID, convs[1].ID}

	if err = storage.MarkExtracted(ctx, convIDs, extractedAt); err != nil {
		t.Fatalf("MarkExtracted failed: %v", err)
	}

	// Load and verify
	convs, err = storage.LoadConversations(ctx, Filter{})
	if err != nil {
		t.Fatalf("LoadConversations failed: %v", err)
	}

	extracted := 0

	for _, conv := range convs {
		if conv.ExtractedAt != nil {
			extracted++
			// Verify timestamp is close to what we set (within 1 second)
			if conv.ExtractedAt.Sub(extractedAt).Abs() > time.Second {
				t.Errorf(
					"extracted_at mismatch: expected ~%v, got %v",
					extractedAt,
					conv.ExtractedAt,
				)
			}
		}
	}

	if extracted != 2 {
		t.Errorf("expected 2 conversations marked as extracted, got %d", extracted)
	}
}

func TestMarkExtracted_EmptyList(t *testing.T) {
	storage := newTestStorage(t)
	defer storage.Close()

	ctx := context.Background()

	// Should not error with empty list
	if err := storage.MarkExtracted(ctx, []int64{}, time.Now()); err != nil {
		t.Errorf("MarkExtracted with empty list should not error: %v", err)
	}
}

func TestLoadConversations_FilterByNotExtractedSince(t *testing.T) {
	storage := newTestStorage(t)
	defer storage.Close()

	ctx := context.Background()

	// Save 3 conversations
	for i := 0; i < 3; i++ {
		conv := Conversation{
			SessionID: "test",
			Timestamp: time.Now(),
			Query:     "Query",
			Response:  "Response",
		}
		if err := storage.SaveConversation(ctx, conv); err != nil {
			t.Fatalf("SaveConversation failed: %v", err)
		}
	}

	// Load conversations
	convs, err := storage.LoadConversations(ctx, Filter{})
	if err != nil {
		t.Fatalf("LoadConversations failed: %v", err)
	}

	// Mark first conversation as extracted 2 hours ago
	oldExtractedAt := time.Now().Add(-2 * time.Hour)
	if err = storage.MarkExtracted(ctx, []int64{convs[0].ID}, oldExtractedAt); err != nil {
		t.Fatalf("MarkExtracted failed: %v", err)
	}

	// Mark second conversation as extracted now
	recentExtractedAt := time.Now()
	if err = storage.MarkExtracted(ctx, []int64{convs[1].ID}, recentExtractedAt); err != nil {
		t.Fatalf("MarkExtracted failed: %v", err)
	}

	// Third conversation not marked

	// Load conversations not extracted since 1 hour ago
	// Should get: conversation 1 (extracted 2h ago) and conversation 3 (never extracted)
	// Should NOT get: conversation 2 (extracted recently)
	filtered, err := storage.LoadConversations(ctx, Filter{
		NotExtractedSince: time.Now().Add(-1 * time.Hour),
	})
	if err != nil {
		t.Fatalf("LoadConversations failed: %v", err)
	}

	if len(filtered) != 2 {
		t.Fatalf(
			"expected 2 conversations (old extraction + never extracted), got %d",
			len(filtered),
		)
	}

	// Verify the right conversations were returned
	hasOldExtraction := false
	hasNeverExtracted := false

	for _, conv := range filtered {
		if conv.ID == convs[0].ID {
			hasOldExtraction = true
		}

		if conv.ID == convs[2].ID {
			hasNeverExtracted = true
		}

		if conv.ID == convs[1].ID {
			t.Error("should not return recently extracted conversation")
		}
	}

	if !hasOldExtraction {
		t.Error("should return conversation with old extraction")
	}

	if !hasNeverExtracted {
		t.Error("should return never-extracted conversation")
	}
}

func TestStorageClose(t *testing.T) {
	storage := newTestStorage(t)

	if err := storage.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Verify database is closed (subsequent operations should fail)
	ctx := context.Background()

	err := storage.SaveConversation(ctx, Conversation{})
	if err == nil {
		t.Error("expected error after Close, got nil")
	}
}

// newTestStorage creates a temporary SQLite storage for testing
func newTestStorage(t *testing.T) *SQLiteStorage {
	t.Helper()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	storage, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStorage failed: %v", err)
	}

	return storage
}
