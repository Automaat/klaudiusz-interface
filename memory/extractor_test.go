package memory

import (
	"context"
	"fmt"
	"testing"
)

func TestNewClaudeExtractor(t *testing.T) {
	extractor := NewClaudeExtractor("/usr/bin/claude", "/tmp")

	if extractor == nil {
		t.Fatal("NewClaudeExtractor returned nil")
	}

	if extractor.claudePath == "" {
		t.Error("claudePath not set")
	}

	if extractor.workingDir != "/tmp" {
		t.Errorf("expected workingDir /tmp, got %s", extractor.workingDir)
	}
}

func TestNewClaudeExtractor_RelativePath(t *testing.T) {
	extractor := NewClaudeExtractor("claude", "/tmp")

	// Should convert to absolute path
	if extractor.claudePath == "claude" {
		t.Error("relative path should be converted to absolute")
	}
}

func TestExtract_EmptyConversations(t *testing.T) {
	extractor := NewClaudeExtractor("/usr/bin/claude", "/tmp")
	ctx := context.Background()

	facts, err := extractor.Extract(ctx, []Conversation{})
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	if facts != nil {
		t.Error("expected nil facts for empty conversations")
	}
}

func TestBuildExtractionPrompt(t *testing.T) {
	extractor := NewClaudeExtractor("/usr/bin/claude", "/tmp")

	conversations := []Conversation{
		{
			ID:       1,
			Query:    "Lubię kawę",
			Response: "Świetnie!",
		},
		{
			ID:       2,
			Query:    "Włącz światła o 18:00",
			Response: "Ustawiam automatyzację",
		},
	}

	prompt := extractor.buildExtractionPrompt(conversations)

	// Verify prompt contains conversation data
	if prompt == "" {
		t.Fatal("prompt is empty")
	}

	// Should include conversation IDs
	if !contains(prompt, "ID: 1") || !contains(prompt, "ID: 2") {
		t.Error("prompt missing conversation IDs")
	}

	// Should include queries
	if !contains(prompt, "Lubię kawę") {
		t.Error("prompt missing query text")
	}

	// Should include category descriptions
	if !contains(prompt, "preference") {
		t.Error("prompt missing category: preference")
	}

	if !contains(prompt, "ha_pattern") {
		t.Error("prompt missing category: ha_pattern")
	}

	// Should include JSON format example
	if !contains(prompt, "confidence") {
		t.Error("prompt missing JSON structure")
	}

	// Should include rules
	if !contains(prompt, "0.7") {
		t.Error("prompt missing confidence threshold")
	}
}

func TestParseFactsJSON_ValidJSON(t *testing.T) {
	extractor := NewClaudeExtractor("/usr/bin/claude", "/tmp")

	output := `Here are the extracted facts:
[
  {
    "category": "preference",
    "text": "Użytkownik lubi kawę z mlekiem",
    "confidence": 0.95,
    "source_ids": [1, 2]
  },
  {
    "category": "ha_pattern",
    "text": "Światła włączają się o 18:00",
    "confidence": 0.90,
    "source_ids": [3]
  }
]
That's all!`

	facts, err := extractor.parseFactsJSON(output, nil)
	if err != nil {
		t.Fatalf("parseFactsJSON failed: %v", err)
	}

	if len(facts) != 2 {
		t.Fatalf("expected 2 facts, got %d", len(facts))
	}

	// Verify first fact
	if facts[0].Category != CategoryPreference {
		t.Errorf("expected category preference, got %s", facts[0].Category)
	}

	if facts[0].Text != "Użytkownik lubi kawę z mlekiem" {
		t.Errorf("unexpected text: %s", facts[0].Text)
	}

	if facts[0].Confidence != 0.95 {
		t.Errorf("expected confidence 0.95, got %f", facts[0].Confidence)
	}

	if len(facts[0].SourceIDs) != 2 {
		t.Errorf("expected 2 source IDs, got %d", len(facts[0].SourceIDs))
	}

	// Verify timestamps are set
	if facts[0].CreatedAt.IsZero() {
		t.Error("CreatedAt not set")
	}

	if facts[0].UpdatedAt.IsZero() {
		t.Error("UpdatedAt not set")
	}
}

func TestParseFactsJSON_EmptyArray(t *testing.T) {
	extractor := NewClaudeExtractor("/usr/bin/claude", "/tmp")

	output := "No high-confidence facts found.\n[]"

	facts, err := extractor.parseFactsJSON(output, nil)
	if err != nil {
		t.Fatalf("parseFactsJSON failed: %v", err)
	}

	if len(facts) != 0 {
		t.Errorf("expected 0 facts, got %d", len(facts))
	}
}

func TestParseFactsJSON_NoJSON(t *testing.T) {
	extractor := NewClaudeExtractor("/usr/bin/claude", "/tmp")

	output := "I could not extract any facts from these conversations."

	facts, err := extractor.parseFactsJSON(output, nil)
	if err != nil {
		t.Fatalf("parseFactsJSON should not error on missing JSON: %v", err)
	}

	if facts != nil {
		t.Error("expected nil facts when no JSON found")
	}
}

func TestParseFactsJSON_InvalidJSON(t *testing.T) {
	extractor := NewClaudeExtractor("/usr/bin/claude", "/tmp")

	output := "Here are facts: [{invalid json}]"

	_, err := extractor.parseFactsJSON(output, nil)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestParseFactsJSON_FilterLowConfidence(t *testing.T) {
	extractor := NewClaudeExtractor("/usr/bin/claude", "/tmp")

	output := `[
  {
    "category": "preference",
    "text": "Low confidence fact",
    "confidence": 0.5,
    "source_ids": [1]
  },
  {
    "category": "preference",
    "text": "High confidence fact",
    "confidence": 0.9,
    "source_ids": [2]
  }
]`

	facts, err := extractor.parseFactsJSON(output, nil)
	if err != nil {
		t.Fatalf("parseFactsJSON failed: %v", err)
	}

	// Should only include high confidence fact
	if len(facts) != 1 {
		t.Fatalf("expected 1 fact (low confidence filtered), got %d", len(facts))
	}

	if facts[0].Confidence != 0.9 {
		t.Errorf("expected high confidence fact, got %f", facts[0].Confidence)
	}
}

func TestParseFactsJSON_FilterInvalidCategory(t *testing.T) {
	extractor := NewClaudeExtractor("/usr/bin/claude", "/tmp")

	output := `[
  {
    "category": "invalid_category",
    "text": "Invalid category fact",
    "confidence": 0.95,
    "source_ids": [1]
  },
  {
    "category": "preference",
    "text": "Valid category fact",
    "confidence": 0.95,
    "source_ids": [2]
  }
]`

	facts, err := extractor.parseFactsJSON(output, nil)
	if err != nil {
		t.Fatalf("parseFactsJSON failed: %v", err)
	}

	// Should only include valid category
	if len(facts) != 1 {
		t.Fatalf("expected 1 fact (invalid category filtered), got %d", len(facts))
	}

	if facts[0].Category != CategoryPreference {
		t.Errorf("expected preference category, got %s", facts[0].Category)
	}
}

func TestParseFactsJSON_FilterHighConfidence(t *testing.T) {
	extractor := NewClaudeExtractor("/usr/bin/claude", "/tmp")

	output := `[
  {
    "category": "preference",
    "text": "Too high confidence",
    "confidence": 1.5,
    "source_ids": [1]
  },
  {
    "category": "preference",
    "text": "Valid confidence",
    "confidence": 0.95,
    "source_ids": [2]
  }
]`

	facts, err := extractor.parseFactsJSON(output, nil)
	if err != nil {
		t.Fatalf("parseFactsJSON failed: %v", err)
	}

	// Should filter out >1.0 confidence
	if len(facts) != 1 {
		t.Fatalf("expected 1 fact (>1.0 confidence filtered), got %d", len(facts))
	}

	if facts[0].Confidence != 0.95 {
		t.Errorf("expected 0.95 confidence, got %f", facts[0].Confidence)
	}
}

func TestParseFactsJSON_AllCategories(t *testing.T) {
	extractor := NewClaudeExtractor("/usr/bin/claude", "/tmp")

	output := `[
  {
    "category": "preference",
    "text": "Preference fact",
    "confidence": 0.9,
    "source_ids": [1]
  },
  {
    "category": "ha_pattern",
    "text": "HA pattern fact",
    "confidence": 0.9,
    "source_ids": [2]
  },
  {
    "category": "knowledge",
    "text": "Knowledge fact",
    "confidence": 0.9,
    "source_ids": [3]
  },
  {
    "category": "schedule",
    "text": "Schedule fact",
    "confidence": 0.9,
    "source_ids": [4]
  }
]`

	facts, err := extractor.parseFactsJSON(output, nil)
	if err != nil {
		t.Fatalf("parseFactsJSON failed: %v", err)
	}

	if len(facts) != 4 {
		t.Fatalf("expected 4 facts (all categories), got %d", len(facts))
	}

	// Verify all categories present
	categories := make(map[FactCategory]bool)
	for _, fact := range facts {
		categories[fact.Category] = true
	}

	expectedCategories := []FactCategory{
		CategoryPreference,
		CategoryHAPattern,
		CategoryKnowledge,
		CategorySchedule,
	}

	for _, cat := range expectedCategories {
		if !categories[cat] {
			t.Errorf("missing category: %s", cat)
		}
	}
}

func TestParseFactsJSON_ConfidenceEdgeCases(t *testing.T) {
	extractor := NewClaudeExtractor("/usr/bin/claude", "/tmp")

	tests := []struct {
		confidence float64
		shouldPass bool
	}{
		{0.7, false},  // Boundary: should be filtered (<=0.7)
		{0.71, true},  // Just above threshold
		{1.0, true},   // Max valid
		{1.01, false}, // Above max
		{0.0, false},  // Zero
		{0.5, false},  // Low
	}

	for _, tc := range tests {
		output := `[
  {
    "category": "preference",
    "text": "Test fact",
    "confidence": ` + formatFloat(tc.confidence) + `,
    "source_ids": [1]
  }
]`

		facts, err := extractor.parseFactsJSON(output, nil)
		if err != nil {
			t.Fatalf("parseFactsJSON failed for confidence %f: %v", tc.confidence, err)
		}

		if tc.shouldPass && len(facts) != 1 {
			t.Errorf("confidence %f: expected 1 fact, got %d", tc.confidence, len(facts))
		}

		if !tc.shouldPass && len(facts) != 0 {
			t.Errorf("confidence %f: expected 0 facts (filtered), got %d", tc.confidence, len(facts))
		}
	}
}

// Helper functions

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}

	return false
}

func formatFloat(f float64) string {
	return fmt.Sprintf("%g", f)
}
