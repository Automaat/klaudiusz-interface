package memory

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestNewService(t *testing.T) {
	storage := newTestStorage(t)
	defer storage.Close()

	extractor := &mockExtractor{}
	retriever := NewSimpleRetriever(storage)

	service := NewService(storage, extractor, retriever)
	if service == nil {
		t.Fatal("NewService returned nil")
	}

	if service.storage != storage {
		t.Error("storage not set")
	}

	if service.extractor != extractor {
		t.Error("extractor not set")
	}

	if service.retriever != retriever {
		t.Error("retriever not set")
	}
}

func TestRemember(t *testing.T) {
	storage := newTestStorage(t)
	defer storage.Close()

	service := NewService(storage, &mockExtractor{}, NewSimpleRetriever(storage))
	ctx := context.Background()

	turn := ConversationTurn{
		SessionID: "test-session",
		Timestamp: time.Now(),
		Query:     "Co tam?",
		Response:  "Wszystko dobrze!",
	}

	if err := service.Remember(ctx, turn); err != nil {
		t.Fatalf("Remember failed: %v", err)
	}

	// Verify conversation was saved
	convs, err := storage.LoadConversations(ctx, Filter{
		SessionID: "test-session",
	})
	if err != nil {
		t.Fatalf("LoadConversations failed: %v", err)
	}

	if len(convs) != 1 {
		t.Fatalf("expected 1 conversation, got %d", len(convs))
	}

	if convs[0].Query != turn.Query {
		t.Errorf("expected query %q, got %q", turn.Query, convs[0].Query)
	}
}

func TestRecall_WithFacts(t *testing.T) {
	storage := newTestStorage(t)
	defer storage.Close()

	ctx := context.Background()
	now := time.Now()

	// Save a fact
	fact := Fact{
		Category:   CategoryPreference,
		Text:       "Użytkownik lubi kawę",
		Confidence: 0.95,
		SourceIDs:  []int64{1},
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := storage.SaveFact(ctx, fact); err != nil {
		t.Fatalf("SaveFact failed: %v", err)
	}

	service := NewService(storage, &mockExtractor{}, NewSimpleRetriever(storage))

	// Recall with facts - use query that matches fact keywords
	context, err := service.Recall(ctx, "użytkownik lubi kawę", RecallOptions{
		IncludeFacts: true,
		FactLimit:    5,
	})
	if err != nil {
		t.Fatalf("Recall failed: %v", err)
	}

	if len(context.Facts) == 0 {
		t.Error("expected facts in context")
	}
}

func TestRecall_WithConversations(t *testing.T) {
	storage := newTestStorage(t)
	defer storage.Close()

	ctx := context.Background()

	// Save a conversation
	conv := Conversation{
		SessionID: "test",
		Timestamp: time.Now(),
		Query:     "Test query",
		Response:  "Test response",
	}
	if err := storage.SaveConversation(ctx, conv); err != nil {
		t.Fatalf("SaveConversation failed: %v", err)
	}

	service := NewService(storage, &mockExtractor{}, NewSimpleRetriever(storage))

	// Recall with conversations
	context, err := service.Recall(ctx, "query", RecallOptions{
		IncludeConversations: true,
		ConversationLimit:    10,
	})
	if err != nil {
		t.Fatalf("Recall failed: %v", err)
	}

	if len(context.Conversations) == 0 {
		t.Error("expected conversations in context")
	}
}

func TestRecall_WithBoth(t *testing.T) {
	storage := newTestStorage(t)
	defer storage.Close()

	ctx := context.Background()
	now := time.Now()

	// Save conversation
	conv := Conversation{
		SessionID: "test",
		Timestamp: now,
		Query:     "Test",
		Response:  "Response",
	}
	if err := storage.SaveConversation(ctx, conv); err != nil {
		t.Fatalf("SaveConversation failed: %v", err)
	}

	// Save fact
	fact := Fact{
		Category:   CategoryKnowledge,
		Text:       "Test fact",
		Confidence: 0.9,
		SourceIDs:  []int64{1},
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := storage.SaveFact(ctx, fact); err != nil {
		t.Fatalf("SaveFact failed: %v", err)
	}

	service := NewService(storage, &mockExtractor{}, NewSimpleRetriever(storage))

	// Recall both
	context, err := service.Recall(ctx, "test", RecallOptions{
		IncludeFacts:         true,
		IncludeConversations: true,
		FactLimit:            5,
		ConversationLimit:    10,
	})
	if err != nil {
		t.Fatalf("Recall failed: %v", err)
	}

	if len(context.Facts) == 0 {
		t.Error("expected facts")
	}

	if len(context.Conversations) == 0 {
		t.Error("expected conversations")
	}
}

func TestRecall_Empty(t *testing.T) {
	storage := newTestStorage(t)
	defer storage.Close()

	service := NewService(storage, &mockExtractor{}, NewSimpleRetriever(storage))

	// Recall without requesting anything
	context, err := service.Recall(context.Background(), "query", RecallOptions{})
	if err != nil {
		t.Fatalf("Recall failed: %v", err)
	}

	if len(context.Facts) != 0 {
		t.Error("expected no facts")
	}

	if len(context.Conversations) != 0 {
		t.Error("expected no conversations")
	}
}

func TestExtractFacts_NoConversations(t *testing.T) {
	storage := newTestStorage(t)
	defer storage.Close()

	extractor := &mockExtractor{}
	service := NewService(storage, extractor, NewSimpleRetriever(storage))

	ctx := context.Background()

	// Extract with no conversations
	if err := service.ExtractFacts(ctx); err != nil {
		t.Fatalf("ExtractFacts failed: %v", err)
	}

	// Extractor should not be called
	if extractor.wasCalled() {
		t.Error("extractor should not be called when no conversations")
	}
}

func TestExtractFacts_Success(t *testing.T) {
	storage := newTestStorage(t)
	defer storage.Close()

	ctx := context.Background()

	// Save a conversation
	conv := Conversation{
		SessionID: "test",
		Timestamp: time.Now(),
		Query:     "Lubię kawę",
		Response:  "Świetnie!",
	}
	if err := storage.SaveConversation(ctx, conv); err != nil {
		t.Fatalf("SaveConversation failed: %v", err)
	}

	now := time.Now()
	extractedFacts := []Fact{
		{
			Category:   CategoryPreference,
			Text:       "Użytkownik lubi kawę",
			Confidence: 0.95,
			SourceIDs:  []int64{1},
			CreatedAt:  now,
			UpdatedAt:  now,
		},
	}

	extractor := &mockExtractor{
		facts: extractedFacts,
	}

	service := NewService(storage, extractor, NewSimpleRetriever(storage))

	// Extract facts
	if err := service.ExtractFacts(ctx); err != nil {
		t.Fatalf("ExtractFacts failed: %v", err)
	}

	// Verify extractor was called
	if !extractor.wasCalled() {
		t.Error("extractor should be called")
	}

	// Verify facts were saved
	facts, err := storage.LoadFacts(ctx, Filter{})
	if err != nil {
		t.Fatalf("LoadFacts failed: %v", err)
	}

	if len(facts) != 1 {
		t.Fatalf("expected 1 fact, got %d", len(facts))
	}

	// Verify conversation marked as extracted
	convs, err := storage.LoadConversations(ctx, Filter{})
	if err != nil {
		t.Fatalf("LoadConversations failed: %v", err)
	}

	if len(convs) != 1 {
		t.Fatalf("expected 1 conversation, got %d", len(convs))
	}

	if convs[0].ExtractedAt == nil {
		t.Error("conversation should be marked as extracted")
	}
}

func TestExtractFacts_PreventsDuplicates(t *testing.T) {
	storage := newTestStorage(t)
	defer storage.Close()

	ctx := context.Background()

	// Save a conversation
	conv := Conversation{
		SessionID: "test",
		Timestamp: time.Now(),
		Query:     "Lubię kawę",
		Response:  "Świetnie!",
	}
	if err := storage.SaveConversation(ctx, conv); err != nil {
		t.Fatalf("SaveConversation failed: %v", err)
	}

	now := time.Now()
	extractedFacts := []Fact{
		{
			Category:   CategoryPreference,
			Text:       "Użytkownik lubi kawę",
			Confidence: 0.95,
			SourceIDs:  []int64{1},
			CreatedAt:  now,
			UpdatedAt:  now,
		},
	}

	extractor := &mockExtractor{
		facts: extractedFacts,
	}

	service := NewService(storage, extractor, NewSimpleRetriever(storage))

	// First extraction
	if err := service.ExtractFacts(ctx); err != nil {
		t.Fatalf("ExtractFacts failed: %v", err)
	}

	// Verify 1 fact saved
	facts, err := storage.LoadFacts(ctx, Filter{})
	if err != nil {
		t.Fatalf("LoadFacts failed: %v", err)
	}

	if len(facts) != 1 {
		t.Fatalf("expected 1 fact after first extraction, got %d", len(facts))
	}

	// Reset extractor call tracking
	extractor.mu.Lock()
	extractor.called = false
	extractor.mu.Unlock()

	// Second extraction - should skip already-extracted conversation
	if err = service.ExtractFacts(ctx); err != nil {
		t.Fatalf("Second ExtractFacts failed: %v", err)
	}

	// Extractor should NOT be called (no unprocessed conversations)
	if extractor.wasCalled() {
		t.Error("extractor should not be called for already-extracted conversation")
	}

	// Verify still only 1 fact (no duplicates)
	facts, err = storage.LoadFacts(ctx, Filter{})
	if err != nil {
		t.Fatalf("LoadFacts failed: %v", err)
	}

	if len(facts) != 1 {
		t.Fatalf("expected 1 fact (no duplicates), got %d", len(facts))
	}
}

func TestExtractFacts_ReextractsAfter24Hours(t *testing.T) {
	storage := newTestStorage(t)
	defer storage.Close()

	ctx := context.Background()

	// Save a conversation
	conv := Conversation{
		SessionID: "test",
		Timestamp: time.Now(),
		Query:     "Lubię kawę",
		Response:  "Świetnie!",
	}
	if err := storage.SaveConversation(ctx, conv); err != nil {
		t.Fatalf("SaveConversation failed: %v", err)
	}

	// Manually mark as extracted 25 hours ago
	convs, err := storage.LoadConversations(ctx, Filter{})
	if err != nil {
		t.Fatalf("LoadConversations failed: %v", err)
	}

	oldExtractedAt := time.Now().Add(-25 * time.Hour)
	if err = storage.MarkExtracted(ctx, []int64{convs[0].ID}, oldExtractedAt); err != nil {
		t.Fatalf("MarkExtracted failed: %v", err)
	}

	now := time.Now()
	extractedFacts := []Fact{
		{
			Category:   CategoryPreference,
			Text:       "Użytkownik lubi kawę (updated)",
			Confidence: 0.98,
			SourceIDs:  []int64{1},
			CreatedAt:  now,
			UpdatedAt:  now,
		},
	}

	extractor := &mockExtractor{
		facts: extractedFacts,
	}

	service := NewService(storage, extractor, NewSimpleRetriever(storage))

	// Extract - should reprocess conversation extracted >24h ago
	if err = service.ExtractFacts(ctx); err != nil {
		t.Fatalf("ExtractFacts failed: %v", err)
	}

	// Extractor SHOULD be called (re-extraction after 24h)
	if !extractor.wasCalled() {
		t.Error("extractor should be called for conversation extracted >24h ago")
	}

	// Verify fact was saved
	facts, err := storage.LoadFacts(ctx, Filter{})
	if err != nil {
		t.Fatalf("LoadFacts failed: %v", err)
	}

	if len(facts) != 1 {
		t.Fatalf("expected 1 fact, got %d", len(facts))
	}
}

func TestStartBackgroundExtraction(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping background extraction test in short mode")
	}

	storage := newTestStorage(t)
	defer storage.Close()

	ctx := context.Background()

	// Save a conversation so extractor has something to extract
	conv := Conversation{
		SessionID: "test",
		Timestamp: time.Now(),
		Query:     "Test query",
		Response:  "Test response",
	}
	if err := storage.SaveConversation(ctx, conv); err != nil {
		t.Fatalf("SaveConversation failed: %v", err)
	}

	now := time.Now()
	extractedFacts := []Fact{
		{
			Category:   CategoryKnowledge,
			Text:       "Test fact",
			Confidence: 0.9,
			SourceIDs:  []int64{1},
			CreatedAt:  now,
			UpdatedAt:  now,
		},
	}

	extractor := &mockExtractor{
		facts: extractedFacts,
	}

	service := NewService(storage, extractor, NewSimpleRetriever(storage))

	// Start with short interval
	service.StartBackgroundExtraction(100 * time.Millisecond)

	// Wait for at least one extraction cycle
	time.Sleep(250 * time.Millisecond)

	// Extractor should have been called at least once
	if !extractor.wasCalled() {
		t.Error("background extraction should have run")
	}

	// Stop service
	if err := service.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
}

func TestClose(t *testing.T) {
	storage := newTestStorage(t)

	extractor := &mockExtractor{}
	service := NewService(storage, extractor, NewSimpleRetriever(storage))

	// Start background extraction
	service.StartBackgroundExtraction(1 * time.Second)

	// Close should stop ticker and close storage
	if err := service.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Verify storage is closed (subsequent operations fail)
	ctx := context.Background()

	err := storage.SaveConversation(ctx, Conversation{})
	if err == nil {
		t.Error("expected error after Close")
	}
}

func TestClose_NoTicker(t *testing.T) {
	storage := newTestStorage(t)

	service := NewService(storage, &mockExtractor{}, NewSimpleRetriever(storage))

	// Close without starting background extraction
	if err := service.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
}

// mockExtractor is a test double for Extractor
type mockExtractor struct {
	facts  []Fact
	called bool
	err    error
	mu     sync.Mutex
}

func (m *mockExtractor) Extract(_ context.Context, _ []Conversation) ([]Fact, error) {
	m.mu.Lock()
	m.called = true
	m.mu.Unlock()

	return m.facts, m.err
}

func (m *mockExtractor) wasCalled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.called
}
