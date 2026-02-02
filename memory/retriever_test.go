package memory

import (
	"context"
	"testing"
	"time"
)

func TestNewSimpleRetriever(t *testing.T) {
	storage := newTestStorage(t)
	defer storage.Close()

	retriever := NewSimpleRetriever(storage, 10)
	if retriever == nil {
		t.Fatal("NewSimpleRetriever returned nil")
	}

	if retriever.storage != storage {
		t.Error("storage not set correctly")
	}
}

func TestGetRelevantFacts_EmptyStorage(t *testing.T) {
	storage := newTestStorage(t)
	defer storage.Close()

	retriever := NewSimpleRetriever(storage, 10)
	ctx := context.Background()

	facts, err := retriever.GetRelevantFacts(ctx, "test query", 5)
	if err != nil {
		t.Fatalf("GetRelevantFacts failed: %v", err)
	}

	if facts != nil {
		t.Errorf("expected nil facts for empty storage, got %d", len(facts))
	}
}

func TestGetRelevantFacts_KeywordMatching(t *testing.T) {
	storage := newTestStorage(t)
	defer storage.Close()

	ctx := context.Background()
	now := time.Now()

	// Save facts with different keywords
	facts := []Fact{
		{
			Category:   CategoryPreference,
			Text:       "Użytkownik lubi kawę z mlekiem",
			Confidence: 0.95,
			SourceIDs:  []int64{1},
			CreatedAt:  now,
			UpdatedAt:  now,
		},
		{
			Category:   CategoryHAPattern,
			Text:       "Światła włączają się o 18:00",
			Confidence: 0.90,
			SourceIDs:  []int64{2},
			CreatedAt:  now,
			UpdatedAt:  now,
		},
		{
			Category:   CategoryKnowledge,
			Text:       "Użytkownik pracuje z domu",
			Confidence: 0.85,
			SourceIDs:  []int64{3},
			CreatedAt:  now,
			UpdatedAt:  now,
		},
	}

	for _, fact := range facts {
		if err := storage.SaveFact(ctx, fact); err != nil {
			t.Fatalf("SaveFact failed: %v", err)
		}
	}

	retriever := NewSimpleRetriever(storage, 10)

	// Query about coffee should match first fact
	results, err := retriever.GetRelevantFacts(ctx, "użytkownik lubi kawę", 5)
	if err != nil {
		t.Fatalf("GetRelevantFacts failed: %v", err)
	}

	if len(results) == 0 {
		t.Fatal("expected at least 1 result")
	}

	// Should have coffee fact in results
	found := false

	for _, r := range results {
		if r.Text == facts[0].Text {
			found = true
			break
		}
	}

	if !found {
		t.Errorf("expected coffee fact in results, got: %+v", results)
	}
}

func TestGetRelevantFacts_ConfidenceWeighting(t *testing.T) {
	storage := newTestStorage(t)
	defer storage.Close()

	ctx := context.Background()
	now := time.Now()

	// Save facts with same keywords but different confidence
	facts := []Fact{
		{
			Category:   CategoryPreference,
			Text:       "test low confidence",
			Confidence: 0.75,
			SourceIDs:  []int64{1},
			CreatedAt:  now,
			UpdatedAt:  now,
		},
		{
			Category:   CategoryPreference,
			Text:       "test high confidence",
			Confidence: 0.95,
			SourceIDs:  []int64{2},
			CreatedAt:  now,
			UpdatedAt:  now,
		},
	}

	for _, fact := range facts {
		if err := storage.SaveFact(ctx, fact); err != nil {
			t.Fatalf("SaveFact failed: %v", err)
		}
	}

	retriever := NewSimpleRetriever(storage, 10)

	// Query with "test" should rank high confidence first
	results, err := retriever.GetRelevantFacts(ctx, "test", 5)
	if err != nil {
		t.Fatalf("GetRelevantFacts failed: %v", err)
	}

	if len(results) < 2 {
		t.Fatal("expected 2 results")
	}

	// Higher confidence fact should be first
	if results[0].Confidence < results[1].Confidence {
		t.Error("facts not sorted by score (confidence weighted)")
	}
}

func TestGetRelevantFacts_Limit(t *testing.T) {
	storage := newTestStorage(t)
	defer storage.Close()

	ctx := context.Background()
	now := time.Now()

	// Save 5 facts with same keyword
	for i := range 5 {
		fact := Fact{
			Category:   CategoryKnowledge,
			Text:       "test fact number",
			Confidence: 0.9,
			SourceIDs:  []int64{int64(i)},
			CreatedAt:  now,
			UpdatedAt:  now,
		}
		if err := storage.SaveFact(ctx, fact); err != nil {
			t.Fatalf("SaveFact failed: %v", err)
		}
	}

	retriever := NewSimpleRetriever(storage, 10)

	// Request only 3 facts
	results, err := retriever.GetRelevantFacts(ctx, "test", 3)
	if err != nil {
		t.Fatalf("GetRelevantFacts failed: %v", err)
	}

	if len(results) != 3 {
		t.Errorf("expected 3 results, got %d", len(results))
	}
}

func TestGetRelevantFacts_NoLimit(t *testing.T) {
	storage := newTestStorage(t)
	defer storage.Close()

	ctx := context.Background()
	now := time.Now()

	// Save 3 facts
	for i := range 3 {
		fact := Fact{
			Category:   CategoryKnowledge,
			Text:       "test fact",
			Confidence: 0.9,
			SourceIDs:  []int64{int64(i)},
			CreatedAt:  now,
			UpdatedAt:  now,
		}
		if err := storage.SaveFact(ctx, fact); err != nil {
			t.Fatalf("SaveFact failed: %v", err)
		}
	}

	retriever := NewSimpleRetriever(storage, 10)

	// Request all facts (limit = 0)
	results, err := retriever.GetRelevantFacts(ctx, "test", 0)
	if err != nil {
		t.Fatalf("GetRelevantFacts failed: %v", err)
	}

	if len(results) != 3 {
		t.Errorf("expected 3 results, got %d", len(results))
	}
}

func TestTokenize(t *testing.T) {
	retriever := &SimpleRetriever{}

	tests := []struct {
		input    string
		expected []string
	}{
		{
			input:    "Użytkownik lubi kawę",
			expected: []string{"użytkownik", "lubi", "kawę"},
		},
		{
			input:    "Hello World 123",
			expected: []string{"hello", "world", "123"},
		},
		{
			input:    "test, with! punctuation?",
			expected: []string{"test", "with", "punctuation"},
		},
		{
			input:    "ąćęłńóśźż", // Polish characters
			expected: []string{"ąćęłńóśźż"},
		},
		{
			input:    "",
			expected: []string{},
		},
	}

	for _, tc := range tests {
		result := retriever.tokenize(tc.input)
		if len(result) != len(tc.expected) {
			t.Errorf(
				"tokenize(%q): expected %d words, got %d",
				tc.input,
				len(tc.expected),
				len(result),
			)

			continue
		}

		for i, word := range tc.expected {
			if result[i] != word {
				t.Errorf("tokenize(%q): expected word[%d]=%q, got %q", tc.input, i, word, result[i])
			}
		}
	}
}

func TestCalculateOverlap(t *testing.T) {
	retriever := &SimpleRetriever{}

	tests := []struct {
		words1   []string
		words2   []string
		expected float64
	}{
		{
			words1:   []string{"test", "word"},
			words2:   []string{"test", "word"},
			expected: 1.0, // Perfect match
		},
		{
			words1:   []string{"test"},
			words2:   []string{"other"},
			expected: 0.0, // No overlap
		},
		{
			words1:   []string{"test", "word"},
			words2:   []string{"test"},
			expected: 0.5, // 1 intersection, 2 union
		},
		{
			words1:   []string{},
			words2:   []string{"test"},
			expected: 0.0, // Empty set
		},
		{
			words1:   []string{"test"},
			words2:   []string{},
			expected: 0.0, // Empty set
		},
		{
			words1:   []string{"a", "b", "c"},
			words2:   []string{"b", "c", "d"},
			expected: 0.5, // 2 intersection, 4 union
		},
	}

	for _, tc := range tests {
		result := retriever.calculateOverlap(tc.words1, tc.words2)
		if result != tc.expected {
			t.Errorf("calculateOverlap(%v, %v): expected %f, got %f",
				tc.words1, tc.words2, tc.expected, result)
		}
	}
}

func TestScoreFactsByRelevance_ZeroScoreFiltered(t *testing.T) {
	retriever := &SimpleRetriever{}

	facts := []Fact{
		{
			Text:       "completely irrelevant xyz",
			Confidence: 0.9,
		},
		{
			Text:       "test fact matching",
			Confidence: 0.9,
		},
	}

	// Query that only matches second fact
	results := retriever.scoreFactsByRelevance("test matching", facts)

	// Should only include fact with non-zero score
	for _, fact := range results {
		if fact.Text == "completely irrelevant xyz" {
			t.Error("zero-score fact should be filtered out")
		}
	}
}

func TestGetRelevantFacts_PolishCharacters(t *testing.T) {
	storage := newTestStorage(t)
	defer storage.Close()

	ctx := context.Background()
	now := time.Now()

	fact := Fact{
		Category:   CategoryPreference,
		Text:       "Użytkownik lubi herbatę z cytryną",
		Confidence: 0.95,
		SourceIDs:  []int64{1},
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	if err := storage.SaveFact(ctx, fact); err != nil {
		t.Fatalf("SaveFact failed: %v", err)
	}

	retriever := NewSimpleRetriever(storage, 10)

	// Query with Polish characters - use words from the fact
	results, err := retriever.GetRelevantFacts(ctx, "użytkownik lubi herbatę", 5)
	if err != nil {
		t.Fatalf("GetRelevantFacts failed: %v", err)
	}

	if len(results) == 0 {
		t.Fatal("expected at least 1 result for Polish query")
	}

	if results[0].Text != fact.Text {
		t.Errorf("expected fact about tea, got: %s", results[0].Text)
	}
}
