package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
)

// ClaudeExtractor implements Extractor using Claude CLI
type ClaudeExtractor struct {
	claudePath string
	workingDir string
}

// NewClaudeExtractor creates a new Claude-based extractor
func NewClaudeExtractor(claudePath, workingDir string) *ClaudeExtractor {
	// Validate and clean claudePath (security: prevent command injection)
	// Claude path must be absolute and executable
	if !filepath.IsAbs(claudePath) {
		claudePath, _ = filepath.Abs(claudePath)
	}

	claudePath = filepath.Clean(claudePath)

	return &ClaudeExtractor{
		claudePath: claudePath,
		workingDir: workingDir,
	}
}

// extractedFact represents the JSON structure from Claude
type extractedFact struct {
	Category   string  `json:"category"`
	Text       string  `json:"text"`
	Confidence float64 `json:"confidence"`
	SourceIDs  []int64 `json:"source_ids"`
}

// Extract analyzes conversations and extracts facts
func (e *ClaudeExtractor) Extract(
	ctx context.Context,
	conversations []Conversation,
) ([]Fact, error) {
	if len(conversations) == 0 {
		return nil, nil
	}

	prompt := e.buildExtractionPrompt(conversations)

	output, err := e.callClaude(ctx, prompt)
	if err != nil {
		return nil, errors.Wrap(err, "call claude")
	}

	return e.parseFactsJSON(output, conversations)
}

// buildExtractionPrompt creates the prompt for fact extraction
func (*ClaudeExtractor) buildExtractionPrompt(conversations []Conversation) string {
	var sb strings.Builder

	sb.WriteString("Analyze these conversations and extract facts about the user:\n\n")
	sb.WriteString("Conversations:\n")

	for _, conv := range conversations {
		sb.WriteString(fmt.Sprintf("ID: %d\n", conv.ID))
		sb.WriteString(fmt.Sprintf("Query: %s\n", conv.Query))
		sb.WriteString(fmt.Sprintf("Response: %s\n", conv.Response))
		sb.WriteString("\n")
	}

	sb.WriteString("\nExtract facts in these categories:\n")
	sb.WriteString("1. \"preference\" - User preferences (coffee, temperature, etc)\n")
	sb.WriteString(
		"2. \"ha_pattern\" - Automation patterns (lights, schedules)\n",
	)
	sb.WriteString("3. \"knowledge\" - General knowledge (habits, routines)\n")
	sb.WriteString("4. \"schedule\" - Time-based patterns (daily routines, schedules)\n\n")

	sb.WriteString("Output ONLY a JSON array with this structure:\n")
	sb.WriteString("[\n")
	sb.WriteString("  {\n")
	sb.WriteString("    \"category\": \"preference\",\n")
	sb.WriteString("    \"text\": \"User likes coffee with milk\",\n")
	sb.WriteString("    \"confidence\": 0.95,\n")
	sb.WriteString("    \"source_ids\": [1, 3]\n")
	sb.WriteString("  }\n")
	sb.WriteString("]\n\n")

	sb.WriteString("Rules:\n")
	sb.WriteString("- Only include facts with confidence > 0.7\n")
	sb.WriteString("- Use Polish language for fact text if the conversation was in Polish\n")
	sb.WriteString("- Be specific and concise\n")
	sb.WriteString("- Include source_ids (conversation IDs) that support the fact\n")
	sb.WriteString("- Return empty array [] if no high-confidence facts found\n")

	return sb.String()
}

// callClaude executes Claude CLI with the prompt
func (e *ClaudeExtractor) callClaude(ctx context.Context, prompt string) (string, error) {
	// Validate claudePath is safe (already cleaned in constructor, but re-validate for security)
	cleanPath := filepath.Clean(e.claudePath)
	if !filepath.IsAbs(cleanPath) {
		return "", errors.New("claude path must be absolute")
	}

	// Verify executable exists and is accessible
	if _, err := os.Stat(cleanPath); err != nil {
		return "", errors.Wrap(err, "claude executable not found")
	}

	// Use LookPath for additional validation (standard security practice)
	execPath, err := exec.LookPath(cleanPath)
	if err != nil {
		return "", errors.Wrap(err, "claude executable not in PATH or not executable")
	}

	// #nosec G204 -- execPath from config, validated with Clean/IsAbs/Stat/LookPath
	cmd := exec.CommandContext(ctx, execPath, "chat", "--no-tty")
	cmd.Dir = e.workingDir
	cmd.Stdin = strings.NewReader(prompt)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", errors.Wrapf(err, "execute claude: %s", string(output))
	}

	return string(output), nil
}

// parseFactsJSON parses Claude's JSON output into Facts
func (*ClaudeExtractor) parseFactsJSON(
	output string,
	_ []Conversation,
) ([]Fact, error) {
	// Extract JSON array from output (Claude may include text before/after)
	start := strings.Index(output, "[")
	end := strings.LastIndex(output, "]")

	if start == -1 || end == -1 || start >= end {
		// No facts found or invalid JSON
		return nil, nil
	}

	jsonStr := output[start : end+1]

	var extracted []extractedFact
	if err := json.Unmarshal([]byte(jsonStr), &extracted); err != nil {
		return nil, errors.Wrapf(err, "unmarshal facts: %s", jsonStr)
	}

	now := time.Now()
	facts := make([]Fact, 0, len(extracted))

	for _, ef := range extracted {
		fact := Fact{
			Category:   FactCategory(ef.Category),
			Text:       ef.Text,
			Confidence: ef.Confidence,
			SourceIDs:  ef.SourceIDs,
			CreatedAt:  now,
			UpdatedAt:  now,
		}

		// Validate category
		if fact.Category != CategoryPreference &&
			fact.Category != CategoryHAPattern &&
			fact.Category != CategoryKnowledge &&
			fact.Category != CategorySchedule {
			continue // Skip invalid category
		}

		// Validate confidence
		if fact.Confidence <= 0.7 || fact.Confidence > 1.0 {
			continue // Skip low confidence
		}

		facts = append(facts, fact)
	}

	return facts, nil
}
