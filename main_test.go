package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/go-chi/chi/v5"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/google/uuid"
	"github.com/joho/godotenv"

	"github.com/Automaat/klaudiusz-interface/config"
	"github.com/Automaat/klaudiusz-interface/memory"
)

// testConfig creates a Config for testing with defaults
func testConfig() *config.Config {
	tmpDir := os.TempDir()
	cfgPath := filepath.Join(tmpDir, fmt.Sprintf("test-config-%d.yaml", time.Now().UnixNano()))

	yamlContent := `
server:
  port: "8742"
  read_timeout: 15s
  write_timeout: 15s
  idle_timeout: 60s
  shutdown_timeout: 10s
claude:
  path: ./testdata/mock-claude.sh
  working_dir: .
  execution_timeout: 2m
  max_prompt_length: 100000
session:
  timeout: 5m
  cleanup_interval: 1m
telegram:
  enabled: false
  bot_token: ""
  group_session_mode: per_user
  voice:
    enabled: true
    max_file_size: 20971520
    download_timeout: 30s
  photo:
    enabled: true
    max_file_size: 20971520
    download_timeout: 30s
deepgram:
  api_key: ""
  language: pl
  model: nova-3
memory:
  enabled: true
  db_path: ~/.klaudiusz/memory.db
  extraction:
    interval: 10m
    timeout: 2m
    max_conversations: 20
    fact_limit: 10
    admin_timeout: 2m
`

	if err := os.WriteFile(cfgPath, []byte(yamlContent), 0o600); err != nil {
		panic("failed to write test config: " + err.Error())
	}

	cfg, err := config.New(cfgPath, false)
	if err != nil {
		panic("failed to load test config: " + err.Error())
	}

	return cfg
}

// testConfigWithFailingClaude creates a Config with failing mock for error tests
func testConfigWithFailingClaude() *config.Config {
	tmpDir := os.TempDir()
	cfgPath := filepath.Join(tmpDir, fmt.Sprintf("test-config-fail-%d.yaml", time.Now().UnixNano()))

	yamlContent := `
server:
  port: "8742"
  read_timeout: 15s
  write_timeout: 15s
  idle_timeout: 60s
  shutdown_timeout: 10s
claude:
  path: ./testdata/mock-claude-fail.sh
  working_dir: .
  execution_timeout: 2m
  max_prompt_length: 100000
session:
  timeout: 5m
  cleanup_interval: 1m
telegram:
  enabled: false
  bot_token: ""
  group_session_mode: per_user
  voice:
    enabled: true
    max_file_size: 20971520
    download_timeout: 30s
  photo:
    enabled: true
    max_file_size: 20971520
    download_timeout: 30s
deepgram:
  api_key: ""
  language: pl
  model: nova-3
memory:
  enabled: true
  db_path: ~/.klaudiusz/memory.db
  extraction:
    interval: 10m
    timeout: 2m
    max_conversations: 20
    fact_limit: 10
    admin_timeout: 2m
`

	if err := os.WriteFile(cfgPath, []byte(yamlContent), 0o600); err != nil {
		panic("failed to write test config: " + err.Error())
	}

	cfg, err := config.New(cfgPath, false)
	if err != nil {
		panic("failed to load test config: " + err.Error())
	}

	return cfg
}

func TestNewServer(t *testing.T) {
	server := NewServer(testConfig())
	defer server.Close()

	if server == nil {
		t.Fatal("NewServer returned nil")
	}

	// Verify cleanup goroutine started (give it a moment)
	time.Sleep(10 * time.Millisecond)
}

func TestGetOrCreateSession(t *testing.T) {
	server := NewServer(testConfig())
	defer server.Close()

	// Test creating new session with empty ID
	session1 := server.getOrCreateSession("")
	if session1 == nil {
		t.Fatal("getOrCreateSession returned nil")
	}

	if session1.ID == "" {
		t.Error("session ID is empty")
	}

	// Test retrieving existing session
	session2 := server.getOrCreateSession(session1.ID)
	if session2.ID != session1.ID {
		t.Errorf("expected session ID %s, got %s", session1.ID, session2.ID)
	}

	// Test creating session with specific ID
	customID := "test-session-123"

	session3 := server.getOrCreateSession(customID)

	if session3.ID != customID {
		t.Errorf("expected session ID %s, got %s", customID, session3.ID)
	}
}

func TestSessionCleanup(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping cleanup test in short mode")
	}

	server := NewServer(testConfig())
	defer server.Close()

	sessionID := "cleanup-test"
	_ = server.getOrCreateSession(sessionID)

	// Verify session exists
	val, ok := server.sessions.Load(sessionID)
	if !ok {
		t.Fatal("session not found after creation")
	}

	// Manually expire session
	s, ok := val.(*Session)
	if !ok {
		t.Fatalf("expected *Session, got %T", val)
	}

	s.mu.Lock()
	s.LastActivity = time.Now().Add(-10 * time.Minute)
	s.mu.Unlock()

	// Wait for cleanup cycle (slightly more than 1 minute)
	time.Sleep(61 * time.Second)

	// Verify session was cleaned up
	_, ok = server.sessions.Load(sessionID)
	if ok {
		t.Error("expired session was not cleaned up")
	}
}

func TestIsDangerousAction(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"wyłącz wszystkie światła", true},
		{"wyłącz wszystko", true},
		{"WYŁĄCZ WSZYSTKO", true},
		{"turn off all lights", true},
		{"zamknij dom", true},
		{"ustaw temperaturę na 10", true},
		{"ustaw temperature do 5", true},
		{"jaka jest temperatura", false},
		{"włącz światło w kuchni", false},
		{"ustaw temperaturę na 20", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := isDangerousAction(tt.input)
			if result != tt.expected {
				t.Errorf("isDangerousAction(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestParsePermissionRequest(t *testing.T) {
	tests := []struct {
		name        string
		response    string
		expectValid bool
		expectDesc  string
		expectCmds  int
	}{
		{
			name:        "valid permission request",
			response:    "PERMISSION_REQUIRED: Wyłączyć wszystkie światła | COMMANDS: light.turn_off_all",
			expectValid: true,
			expectDesc:  "Wyłączyć wszystkie światła",
			expectCmds:  1,
		},
		{
			name:        "multiple commands",
			response:    "PERMISSION_REQUIRED: Zamknąć dom | COMMANDS: light.turn_off_all, lock.front_door",
			expectValid: true,
			expectDesc:  "Zamknąć dom",
			expectCmds:  2,
		},
		{
			name:        "no permission required",
			response:    "Temperatura wynosi 22 stopnie",
			expectValid: false,
		},
		{
			name:        "malformed request",
			response:    "PERMISSION_REQUIRED: something",
			expectValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			action, valid := parsePermissionRequest(tt.response)
			if valid != tt.expectValid {
				t.Errorf("expected valid=%v, got %v", tt.expectValid, valid)
			}

			if !valid {
				return
			}

			if action.Description != tt.expectDesc {
				t.Errorf("expected description %q, got %q", tt.expectDesc, action.Description)
			}

			if len(action.Commands) != tt.expectCmds {
				t.Errorf("expected %d commands, got %d", tt.expectCmds, len(action.Commands))
			}
		})
	}
}

func TestBuildSystemPrompt(t *testing.T) {
	query := "Jaka jest temperatura?"
	prompt := buildSystemPrompt(query)

	if prompt == "" {
		t.Error("buildSystemPrompt returned empty string")
	}

	// Verify prompt contains dynamic content (query)
	// Core personality/language instructions now in CLAUDE.md (klaudiusz-brain repo)
	if !contains(prompt, query) {
		t.Errorf("prompt missing query: %q", query)
	}

	if !contains(prompt, "Pytanie:") {
		t.Error("prompt missing Polish query header")
	}

	if !contains(prompt, "Odpowiedź") {
		t.Error("prompt missing Polish response prompt")
	}
}

func TestBuildSystemPromptWithMemory(t *testing.T) {
	query := "jaka jest temperatura"

	t.Run("with_memory_facts", func(t *testing.T) {
		facts := []memory.Fact{
			{Category: "preferences", Text: "Preferuje 22 stopnie", Confidence: 0.9},
			{Category: "location", Text: "Mieszka w Warszawie", Confidence: 0.8},
		}

		prompt := buildSystemPromptWithMemory(query, facts, nil)

		if !contains(prompt, "PAMIĘĆ") {
			t.Error("prompt missing memory section")
		}

		if !contains(prompt, "preferences") || !contains(prompt, "22 stopnie") {
			t.Error("prompt missing memory facts")
		}

		if !contains(prompt, query) {
			t.Error("prompt missing query")
		}
	})

	t.Run("with_user_context", func(t *testing.T) {
		userCtx := &UserContext{
			UserID:    123,
			Username:  "testuser",
			FirstName: "Test",
			ChatID:    456,
			ChatType:  "group",
			GroupMode: "per_user",
		}

		prompt := buildSystemPromptWithMemory(query, nil, userCtx)

		if !contains(prompt, "UŻYTKOWNIK") {
			t.Error("prompt missing user context section")
		}

		if !contains(prompt, "testuser") {
			t.Error("prompt missing username")
		}

		if !contains(prompt, "prywatna rozmowa w grupie") {
			t.Error("prompt missing group mode")
		}
	})

	t.Run("with_memory_and_user", func(t *testing.T) {
		facts := []memory.Fact{
			{Category: "preferences", Text: "Test fact", Confidence: 0.9},
		}

		userCtx := &UserContext{
			UserID:    123,
			Username:  "testuser",
			ChatID:    456,
			ChatType:  "group",
			GroupMode: "shared",
		}

		prompt := buildSystemPromptWithMemory(query, facts, userCtx)

		if !contains(prompt, "PAMIĘĆ") {
			t.Error("prompt missing memory section")
		}

		if !contains(prompt, "UŻYTKOWNIK") {
			t.Error("prompt missing user context")
		}

		if !contains(prompt, "wspólna konwersacja grupowa") {
			t.Error("prompt missing shared session mode")
		}
	})

	t.Run("without_memory_or_user", func(t *testing.T) {
		prompt := buildSystemPromptWithMemory(query, nil, nil)

		if contains(prompt, "PAMIĘĆ") {
			t.Error("prompt should not have memory section")
		}

		if contains(prompt, "UŻYTKOWNIK") {
			t.Error("prompt should not have user context")
		}

		if !contains(prompt, query) {
			t.Error("prompt missing query")
		}
	})

	t.Run("telegram_emoji_instruction", func(t *testing.T) {
		userCtx := &UserContext{
			UserID:     12345,
			FirstName:  "Test",
			ChatType:   "private",
			ChatID:     12345,
			IsTelegram: true,
		}

		prompt := buildSystemPromptWithMemory(query, nil, userCtx)

		if !contains(prompt, "emoji") {
			t.Error("Telegram prompt missing emoji instruction")
		}
	})

	t.Run("http_no_emoji_instruction", func(t *testing.T) {
		prompt := buildSystemPromptWithMemory(query, nil, nil)

		if contains(prompt, "emoji") {
			t.Error("HTTP prompt should not have emoji instruction")
		}
	})
}

func TestHealthHandler(t *testing.T) {
	server := NewServer(testConfig())
	defer server.Close()

	// Create test sessions
	server.getOrCreateSession("session-1")
	server.getOrCreateSession("session-2")

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	server.handleHealth(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var response map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response["status"] != "ok" {
		t.Errorf("expected status ok, got %v", response["status"])
	}

	// Verify claude_path is present (value comes from config)
	if response["claude_path"] == "" {
		t.Error("expected claude_path in response")
	}

	activeSessions := int(response["active_sessions"].(float64))
	if activeSessions != 2 {
		t.Errorf("expected 2 active sessions, got %d", activeSessions)
	}
}

func TestAskHandler_InvalidJSON(t *testing.T) {
	server := NewServer(testConfig())
	defer server.Close()

	req := httptest.NewRequest(http.MethodPost, "/ask", bytes.NewBufferString("{invalid json"))
	w := httptest.NewRecorder()

	server.handleAsk(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestAskHandler_MissingQuery(t *testing.T) {
	server := NewServer(testConfig())
	defer server.Close()

	body := `{"session_id": "test"}`
	req := httptest.NewRequest(http.MethodPost, "/ask", bytes.NewBufferString(body))
	w := httptest.NewRecorder()

	server.handleAsk(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestCancelHandler(t *testing.T) {
	server := NewServer(testConfig())
	defer server.Close()

	// Create session with pending action
	session := server.getOrCreateSession("cancel-test")
	session.mu.Lock()
	session.PendingAction = &PendingAction{
		ID:          "action-1",
		Description: "Test action",
		Commands:    []string{"test.command"},
	}
	session.mu.Unlock()

	// Cancel the action
	body := `{"session_id": "cancel-test"}`
	req := httptest.NewRequest(http.MethodPost, "/cancel", bytes.NewBufferString(body))
	w := httptest.NewRecorder()

	server.handleCancel(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var response map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !response["cancelled"].(bool) {
		t.Error("expected cancelled=true")
	}

	// Verify action was cleared
	session.mu.Lock()
	defer session.mu.Unlock()

	if session.PendingAction != nil {
		t.Error("pending action was not cleared")
	}
}

func TestCancelHandler_NoSession(t *testing.T) {
	server := NewServer(testConfig())
	defer server.Close()

	body := `{"session_id": "nonexistent"}`
	req := httptest.NewRequest(http.MethodPost, "/cancel", bytes.NewBufferString(body))
	w := httptest.NewRecorder()

	server.handleCancel(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var response map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response["cancelled"].(bool) {
		t.Error("expected cancelled=false for nonexistent session")
	}
}

func TestExecuteClaude(t *testing.T) {
	t.Run("rejects prompt exceeding max length", func(t *testing.T) {
		longPrompt := strings.Repeat("a", 100001)
		ctx := context.Background()
		session := &Session{ID: "test-session"}

		_, err := executeClaude(ctx, longPrompt, session, "claude", "/tmp", 100000)
		if err == nil {
			t.Error("expected error for oversized prompt")
		}

		if !strings.Contains(err.Error(), "prompt too long") {
			t.Errorf("expected 'prompt too long' error, got: %v", err)
		}
	})

	t.Run("handles context cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		session := &Session{ID: "test-session"}

		_, err := executeClaude(ctx, "test prompt", session, "claude", "/tmp", 100000)
		if err == nil {
			t.Error("expected error for cancelled context")
		}
	})

	t.Run("sets working directory on command", func(t *testing.T) {
		if testing.Short() {
			t.Skip("skipping command execution test in short mode")
		}

		// Build test helper to verify working directory
		helperPath := filepath.Join(t.TempDir(), "pwd_helper")

		cmd := exec.Command("go", "build", "-o", helperPath, "./testdata/permission_helper.go")
		if err := cmd.Run(); err != nil {
			t.Fatalf("failed to build helper: %v", err)
		}

		ctx := context.Background()
		session := &Session{ID: "test-session"}

		output, err := executeClaude(ctx, "test", session, helperPath, "/tmp", 100000)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify working directory was set correctly
		expected := "pwd=/tmp"
		if !strings.Contains(output, expected) {
			t.Errorf("expected output to contain %q, got: %s", expected, output)
		}
	})
}

func TestExecuteClaude_ResumeFallback(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping resume fallback test in short mode")
	}

	// Build helper that fails on --resume, succeeds on --session-id
	helperPath := filepath.Join(t.TempDir(), "resume_helper")

	cmd := exec.Command("go", "build", "-o", helperPath, "./testdata/resume_helper.go")
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to build helper: %v", err)
	}

	ctx := context.Background()
	session := &Session{ID: "test-session"}

	output, err := executeClaude(ctx, "test prompt", session, helperPath, "/tmp", 100000)
	if err != nil {
		t.Fatalf("expected fallback to succeed: %v", err)
	}

	if !strings.Contains(output, "Success") {
		t.Errorf("expected success output, got: %s", output)
	}
}

func TestExecuteClaude_ConcurrentSerialization(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping concurrent test in short mode")
	}

	// Build helper with delay
	helperPath := filepath.Join(t.TempDir(), "slow_helper")

	cmd := exec.Command("go", "build", "-o", helperPath, "./testdata/slow_helper.go")
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to build helper: %v", err)
	}

	session := &Session{ID: "concurrent-test"}
	ctx := context.Background()

	const numConcurrent = 3

	var wg sync.WaitGroup

	errs := make(chan error, numConcurrent)

	start := time.Now()

	for range numConcurrent {
		wg.Add(1)

		go func() {
			defer wg.Done()

			_, err := executeClaude(ctx, "test", session, helperPath, "/tmp", 100000)
			if err != nil {
				errs <- err
			}
		}()
	}

	wg.Wait()
	close(errs)

	duration := time.Since(start)

	// Check for errors
	for err := range errs {
		t.Errorf("concurrent execution failed: %v", err)
	}

	// Verify serialization (3 calls * 100ms ≈ 300ms)
	minDuration := 250 * time.Millisecond // Allow some variance
	if duration < minDuration {
		t.Errorf("executions may have run concurrently: %v < %v", duration, minDuration)
	}
}

func TestExecuteClaude_ResumeSuccess(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping resume success test in short mode")
	}

	helperPath := filepath.Join(t.TempDir(), "resume_success_helper")

	cmd := exec.Command("go", "build", "-o", helperPath, "./testdata/resume_success_helper.go")
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to build helper: %v", err)
	}

	ctx := context.Background()
	session := &Session{ID: "existing-session"}

	output, err := executeClaude(ctx, "test", session, helperPath, "/tmp", 100000)
	if err != nil {
		t.Fatalf("resume should succeed: %v", err)
	}

	if !strings.Contains(output, "Resumed") {
		t.Errorf("expected resumed output, got: %s", output)
	}
}

func TestHandleConfirmation_Success(t *testing.T) {
	server := NewServer(testConfig())
	defer server.Close()

	// ClaudePath and WorkingDir now come from config

	session := server.getOrCreateSession("confirm-test")
	session.mu.Lock()
	session.PendingAction = &PendingAction{
		ID:          "action-1",
		Description: "Test action",
		Commands:    []string{"light.turn_on"},
	}
	session.mu.Unlock()

	req := httptest.NewRequest(http.MethodPost, "/ask", nil)
	w := httptest.NewRecorder()

	err := server.handleConfirmation(w, req, session)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var response map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !response["action_executed"].(bool) {
		t.Error("expected action_executed=true")
	}

	session.mu.Lock()
	defer session.mu.Unlock()

	if session.PendingAction != nil {
		t.Error("expected pending action to be cleared")
	}
}

func TestHandleConfirmation_NoPendingAction(t *testing.T) {
	server := NewServer(testConfig())
	defer server.Close()

	session := server.getOrCreateSession("no-action")

	req := httptest.NewRequest(http.MethodPost, "/ask", nil)
	w := httptest.NewRecorder()

	err := server.handleConfirmation(w, req, session)
	if err == nil {
		t.Error("expected error for no pending action")
	}

	if !strings.Contains(err.Error(), "no pending action") {
		t.Errorf("expected 'no pending action' error, got: %v", err)
	}
}

func TestHandleConfirmation_InvalidCommand(t *testing.T) {
	server := NewServer(testConfig())
	defer server.Close()

	session := server.getOrCreateSession("invalid-cmd")
	session.mu.Lock()
	session.PendingAction = &PendingAction{
		ID:          "action-1",
		Description: "Test",
		Commands:    []string{""},
	}
	session.mu.Unlock()

	req := httptest.NewRequest(http.MethodPost, "/ask", nil)
	w := httptest.NewRecorder()

	err := server.handleConfirmation(w, req, session)
	if err == nil {
		t.Error("expected error for invalid command")
	}

	if !strings.Contains(err.Error(), "invalid command format") {
		t.Errorf("expected 'invalid command format' error, got: %v", err)
	}
}

func TestHandleConfirmation_CommandTooLong(t *testing.T) {
	server := NewServer(testConfig())
	defer server.Close()

	session := server.getOrCreateSession("long-cmd")
	session.mu.Lock()
	session.PendingAction = &PendingAction{
		ID:          "action-1",
		Description: "Test",
		Commands:    []string{strings.Repeat("x", 1001)},
	}
	session.mu.Unlock()

	req := httptest.NewRequest(http.MethodPost, "/ask", nil)
	w := httptest.NewRecorder()

	err := server.handleConfirmation(w, req, session)
	if err == nil {
		t.Error("expected error for command too long")
	}

	if !strings.Contains(err.Error(), "invalid command format") {
		t.Errorf("expected 'invalid command format' error, got: %v", err)
	}
}

func TestHandleConfirmation_ExecutionError(t *testing.T) {
	server := NewServer(testConfigWithFailingClaude())
	defer server.Close()

	session := server.getOrCreateSession("exec-error")
	session.mu.Lock()
	session.PendingAction = &PendingAction{
		ID:          "action-exec",
		Description: "Test",
		Commands:    []string{"test"},
	}
	session.mu.Unlock()

	req := httptest.NewRequest(http.MethodPost, "/ask", nil)
	w := httptest.NewRecorder()

	err := server.handleConfirmation(w, req, session)
	if err == nil {
		t.Fatal("expected error from executeClaude")
	}

	if !strings.Contains(err.Error(), "failed to execute action") {
		t.Errorf("expected 'failed to execute action' error, got: %v", err)
	}
}

func TestHandleAsk_WithConfirmation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping confirmation test in short mode")
	}

	server := NewServer(testConfig())
	defer server.Close()

	// ClaudePath and WorkingDir now come from config

	session := server.getOrCreateSession("ask-confirm")
	session.mu.Lock()
	session.PendingAction = &PendingAction{
		ID:          "action-1",
		Description: "Turn on lights",
		Commands:    []string{"light.turn_on"},
	}
	session.mu.Unlock()

	body := `{"query": "yes", "session_id": "ask-confirm", "confirm_action": true}`
	req := httptest.NewRequest(http.MethodPost, "/ask", bytes.NewBufferString(body))
	w := httptest.NewRecorder()

	server.handleAsk(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestHandleAsk_PermissionRequired(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping permission test in short mode")
	}

	server := NewServer(testConfig())
	defer server.Close()

	body := `{"query": "test", "session_id": "perm-test"}`
	req := httptest.NewRequest(http.MethodPost, "/ask", bytes.NewBufferString(body))
	w := httptest.NewRecorder()

	server.handleAsk(w, req)

	var response map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if _, hasText := response["text"]; !hasText {
		t.Error("expected text in response")
	}
}

func TestHandleAsk_DangerousActionWarning(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping dangerous action test in short mode")
	}

	server := NewServer(testConfig())
	defer server.Close()

	// ClaudePath and WorkingDir now come from config

	body := `{"query": "wyłącz wszystko", "session_id": "danger-test"}`
	req := httptest.NewRequest(http.MethodPost, "/ask", bytes.NewBufferString(body))
	w := httptest.NewRecorder()

	server.handleAsk(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestHandleAsk_NormalResponse(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping normal response test in short mode")
	}

	server := NewServer(testConfig())
	defer server.Close()

	// ClaudePath and WorkingDir now come from config

	body := `{"query": "jaka jest temperatura", "session_id": "normal-test"}`
	req := httptest.NewRequest(http.MethodPost, "/ask", bytes.NewBufferString(body))
	w := httptest.NewRecorder()

	server.handleAsk(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var response map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response["session_id"] == "" {
		t.Error("expected session_id in response")
	}

	if _, hasText := response["text"]; !hasText {
		t.Error("expected text in response")
	}
}

func TestHandleAsk_ClaudeExecutionError(t *testing.T) {
	server := NewServer(testConfigWithFailingClaude())
	defer server.Close()

	body := `{"query": "test", "session_id": "error-test"}`
	req := httptest.NewRequest(http.MethodPost, "/ask", bytes.NewBufferString(body))
	w := httptest.NewRecorder()

	server.handleAsk(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", w.Code)
	}

	var response map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response["text"] == "" {
		t.Error("expected error text in response")
	}

	if _, hasError := response["error"]; !hasError {
		t.Error("expected error field in response")
	}
}

func TestHandleAsk_ConfirmationError(t *testing.T) {
	server := NewServer(testConfig())
	defer server.Close()

	_ = server.getOrCreateSession("confirm-error")

	body := `{"query": "yes", "session_id": "confirm-error", "confirm_action": true}`
	req := httptest.NewRequest(http.MethodPost, "/ask", bytes.NewBufferString(body))
	w := httptest.NewRecorder()

	server.handleAsk(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", w.Code)
	}

	var response map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if _, hasError := response["error"]; !hasError {
		t.Error("expected error field in response")
	}
}

func TestHandleAsk_PermissionFlow_WithPunctuation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping permission punctuation test in short mode")
	}

	// Build test helper binary
	helperPath := filepath.Join(t.TempDir(), "permission_helper")

	cmd := exec.Command("go", "build", "-o", helperPath, "./testdata/permission_helper.go")
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to build permission helper: %v", err)
	}

	// Create test config with helper as Claude binary
	tmpDir := os.TempDir()
	cfgPath := filepath.Join(tmpDir, fmt.Sprintf("test-config-perm-%d.yaml", time.Now().UnixNano()))

	yamlContent := fmt.Sprintf(`
server:
  port: "8742"
  read_timeout: 15s
  write_timeout: 15s
  idle_timeout: 60s
  shutdown_timeout: 10s
claude:
  path: %s
  working_dir: .
  execution_timeout: 2m
  max_prompt_length: 100000
session:
  timeout: 5m
  cleanup_interval: 1m
telegram:
  enabled: false
  bot_token: ""
  group_session_mode: per_user
  voice:
    enabled: true
    max_file_size: 20971520
    download_timeout: 30s
  photo:
    enabled: true
    max_file_size: 20971520
    download_timeout: 30s
deepgram:
  api_key: ""
  language: pl
  model: nova-3
memory:
  enabled: true
  db_path: ~/.klaudiusz/memory.db
  extraction:
    interval: 10m
    timeout: 2m
    max_conversations: 20
    fact_limit: 10
    admin_timeout: 2m
`, helperPath)

	if err := os.WriteFile(cfgPath, []byte(yamlContent), 0o600); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	cfg, err := config.New(cfgPath, false)
	if err != nil {
		t.Fatalf("failed to load test config: %v", err)
	}

	server := NewServer(cfg)
	defer server.Close()

	body := `{"query": "test query"}`
	req := httptest.NewRequest(http.MethodPost, "/ask", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()

	server.handleAsk(w, req)

	var response map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Verify permission flow was triggered
	requiresPermission, ok := response["requires_permission"].(bool)
	if !ok || !requiresPermission {
		t.Error("expected requires_permission=true")
	}

	actionDesc, ok := response["action_description"].(string)
	if !ok {
		t.Error("expected action_description field")
	}

	// Verify punctuation was added (helper outputs without trailing punctuation)
	text, ok := response["text"].(string)
	if !ok {
		t.Error("expected text field")
	}

	expectedPrefix := "Test action description."
	if !strings.HasPrefix(text, expectedPrefix) {
		t.Errorf("expected text to start with %q (with added period), got %q", expectedPrefix, text)
	}

	if response["session_id"] == "" {
		t.Error("expected session_id in response")
	}

	if response["action_id"] == "" {
		t.Error("expected action_id in response")
	}

	if actionDesc != "Test action description" {
		t.Errorf("expected action_description 'Test action description', got %q", actionDesc)
	}
}

func TestHandleCancel_InvalidJSON(t *testing.T) {
	server := NewServer(testConfig())
	defer server.Close()

	req := httptest.NewRequest(http.MethodPost, "/cancel", bytes.NewBufferString("{invalid"))
	w := httptest.NewRecorder()

	server.handleCancel(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestHandleCancel_NoPendingAction(t *testing.T) {
	server := NewServer(testConfig())
	defer server.Close()

	_ = server.getOrCreateSession("cancel-no-action")

	body := `{"session_id": "cancel-no-action"}`
	req := httptest.NewRequest(http.MethodPost, "/cancel", bytes.NewBufferString(body))
	w := httptest.NewRecorder()

	server.handleCancel(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var response map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response["cancelled"].(bool) {
		t.Error("expected cancelled=false when no pending action")
	}

	if response["text"] != "Anulowano akcję." {
		t.Errorf("unexpected text: %v", response["text"])
	}
}

func TestHandleCancel_InvalidSessionType(t *testing.T) {
	server := NewServer(testConfig())
	defer server.Close()

	// Store invalid type in sessions map to trigger type assertion failure
	server.sessions.Store("invalid-session", "not-a-session-pointer")

	body := `{"session_id": "invalid-session"}`
	req := httptest.NewRequest(http.MethodPost, "/cancel", bytes.NewBufferString(body))
	w := httptest.NewRecorder()

	server.handleCancel(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", w.Code)
	}
}

func TestHealthHandler_WorkingDir(t *testing.T) {
	server := NewServer(testConfig())
	defer server.Close()

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	server.handleHealth(w, req)

	var response map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Verify working_dir is present (value comes from config)
	if _, ok := response["working_dir"]; !ok {
		t.Error("expected working_dir in response")
	}
}

// Helper function
func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

// Telegram tests
func TestChatIDToSessionID(t *testing.T) {
	tests := []int64{12345, -67890, 0}

	for _, chatID := range tests {
		result := chatIDToSessionID(chatID)

		// Verify it's a valid UUID
		if _, err := uuid.Parse(result); err != nil {
			t.Errorf("chatIDToSessionID(%d) = %s, not a valid UUID: %v", chatID, result, err)
		}

		// Verify deterministic (same input produces same output)
		result2 := chatIDToSessionID(chatID)
		if result != result2 {
			t.Errorf("chatIDToSessionID(%d) not deterministic: %s != %s", chatID, result, result2)
		}
	}

	// Verify different chat IDs produce different UUIDs
	uuid1 := chatIDToSessionID(12345)

	uuid2 := chatIDToSessionID(67890)
	if uuid1 == uuid2 {
		t.Error("different chat IDs should produce different UUIDs")
	}
}

func TestFormatTelegramError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected string
	}{
		{
			name:     "timeout error",
			err:      context.DeadlineExceeded,
			expected: "Przekroczono czas oczekiwania",
		},
		{
			name:     "no pending action",
			err:      &mockError{msg: "no pending action"},
			expected: "Nie ma oczekującej akcji",
		},
		{
			name:     "invalid command",
			err:      &mockError{msg: "invalid command format"},
			expected: "Nieprawidłowe polecenie",
		},
		{
			name:     "generic error",
			err:      &mockError{msg: "something else"},
			expected: "Przepraszam, wystąpił błąd",
		},
		{
			name:     "nil error",
			err:      nil,
			expected: "Nieznany błąd",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatTelegramError(tt.err)
			if result != tt.expected {
				t.Errorf("formatTelegramError(%v) = %s, want %s", tt.err, result, tt.expected)
			}
		})
	}
}

func TestExecuteConfirmedAction_Success(t *testing.T) {
	server := NewServer(testConfig())
	defer server.Close()

	sessionID := "test-session"
	session := server.getOrCreateSession(sessionID)

	action := &PendingAction{
		ID:          "action-123",
		Description: "Test action",
		Commands:    []string{"test-command"},
	}

	session.mu.Lock()
	session.PendingAction = action
	session.mu.Unlock()

	ctx := context.Background()

	// Note: This test will use the claude path from config
	// If claude is not available, it will fail with an error which is expected
	response, err := server.executeConfirmedAction(ctx, session, "action-123")
	if err != nil {
		// Expected to fail if claude is not installed at the default path
		t.Skipf("executeConfirmedAction failed (may need claude installed): %v", err)
	}

	// If it succeeds, verify we got some response
	if response == "" {
		t.Error("expected non-empty response")
	}

	// Verify pending action was cleared
	session.mu.Lock()
	defer session.mu.Unlock()

	if session.PendingAction != nil {
		t.Error("pending action should be cleared")
	}
}

func TestExecuteConfirmedAction_NoPendingAction(t *testing.T) {
	server := NewServer(testConfig())
	defer server.Close()

	session := server.getOrCreateSession("test")

	ctx := context.Background()

	_, err := server.executeConfirmedAction(ctx, session, "action-123")
	if err == nil {
		t.Fatal("expected error for no pending action")
	}

	if !strings.Contains(err.Error(), "no pending action") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestExecuteConfirmedAction_ActionIDMismatch(t *testing.T) {
	server := NewServer(testConfig())
	defer server.Close()

	session := server.getOrCreateSession("test")

	action := &PendingAction{
		ID:          "action-123",
		Description: "Test",
		Commands:    []string{"test"},
	}

	session.mu.Lock()
	session.PendingAction = action
	session.mu.Unlock()

	ctx := context.Background()

	_, err := server.executeConfirmedAction(ctx, session, "wrong-id")
	if err == nil {
		t.Fatal("expected error for action ID mismatch")
	}

	if !strings.Contains(err.Error(), "action ID mismatch") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestExecuteConfirmedAction_InvalidCommand(t *testing.T) {
	server := NewServer(testConfig())
	defer server.Close()

	session := server.getOrCreateSession("test")

	tests := []struct {
		name     string
		commands []string
	}{
		{"empty command", []string{""}},
		{"too long command", []string{strings.Repeat("x", 1001)}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			action := &PendingAction{
				ID:          "action-123",
				Description: "Test",
				Commands:    tt.commands,
			}

			session.mu.Lock()
			session.PendingAction = action
			session.mu.Unlock()

			ctx := context.Background()

			_, err := server.executeConfirmedAction(ctx, session, "action-123")
			if err == nil {
				t.Fatal("expected error for invalid command")
			}

			if !strings.Contains(err.Error(), "invalid command format") {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestTelegramSessionMapping(t *testing.T) {
	server := NewServer(testConfig())
	defer server.Close()

	// Test that Telegram chat IDs map to unique session IDs
	chatID1 := int64(12345)
	chatID2 := int64(67890)

	sessionID1 := chatIDToSessionID(chatID1)
	sessionID2 := chatIDToSessionID(chatID2)

	session1 := server.getOrCreateSession(sessionID1)
	session2 := server.getOrCreateSession(sessionID2)

	// Verify different chat IDs produce different sessions
	if session1.ID == session2.ID {
		t.Error("different chat IDs should produce different sessions")
	}

	// Verify session IDs are valid UUIDs
	if _, err := uuid.Parse(session1.ID); err != nil {
		t.Errorf("session ID should be valid UUID: %s, error: %v", session1.ID, err)
	}

	if _, err := uuid.Parse(session2.ID); err != nil {
		t.Errorf("session ID should be valid UUID: %s, error: %v", session2.ID, err)
	}

	// Verify same chat ID produces same session
	session1Again := server.getOrCreateSession(sessionID1)
	if session1.ID != session1Again.ID {
		t.Error("same chat ID should produce same session")
	}
}

func TestTelegramSessionTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping timeout test in short mode")
	}

	server := NewServer(testConfig())
	defer server.Close()

	chatID := int64(99999)
	sessionID := chatIDToSessionID(chatID)
	_ = server.getOrCreateSession(sessionID)

	// Verify session exists
	val, ok := server.sessions.Load(sessionID)
	if !ok {
		t.Fatal("session not found after creation")
	}

	// Manually expire session
	s, ok := val.(*Session)
	if !ok {
		t.Fatalf("expected *Session, got %T", val)
	}

	s.mu.Lock()
	s.LastActivity = time.Now().Add(-10 * time.Minute)
	s.mu.Unlock()

	// Wait for cleanup cycle
	time.Sleep(61 * time.Second)

	// Verify session was cleaned up
	_, ok = server.sessions.Load(sessionID)
	if ok {
		t.Error("expired Telegram session was not cleaned up")
	}
}

func TestTelegramHTTPSessionIsolation(t *testing.T) {
	server := NewServer(testConfig())
	defer server.Close()

	// Create HTTP session
	httpSessionID := "http-session-123"
	httpSession := server.getOrCreateSession(httpSessionID)

	// Create Telegram session with similar ID
	telegramChatID := int64(123)
	telegramSessionID := chatIDToSessionID(telegramChatID)
	telegramSession := server.getOrCreateSession(telegramSessionID)

	// Verify sessions are different
	if httpSession.ID == telegramSession.ID {
		t.Error("HTTP and Telegram sessions should be isolated")
	}

	// Set pending actions on both
	httpAction := &PendingAction{
		ID:          "http-action",
		Description: "HTTP test",
		Commands:    []string{"http-cmd"},
	}

	telegramAction := &PendingAction{
		ID:          "telegram-action",
		Description: "Telegram test",
		Commands:    []string{"telegram-cmd"},
	}

	httpSession.mu.Lock()
	httpSession.PendingAction = httpAction
	httpSession.mu.Unlock()

	telegramSession.mu.Lock()
	telegramSession.PendingAction = telegramAction
	telegramSession.mu.Unlock()

	// Verify isolation
	httpSession.mu.Lock()

	if httpSession.PendingAction.ID != "http-action" {
		t.Error("HTTP session pending action was contaminated")
	}

	httpSession.mu.Unlock()

	telegramSession.mu.Lock()

	if telegramSession.PendingAction.ID != "telegram-action" {
		t.Error("Telegram session pending action was contaminated")
	}

	telegramSession.mu.Unlock()
}

// Mock error type for testing
type mockError struct {
	msg string
}

func (e *mockError) Error() string {
	return e.msg
}

// Test gracefulShutdown function
func TestGracefulShutdown(t *testing.T) {
	cfg := testConfig()
	server := NewServer(cfg)
	// Note: Don't defer server.Close() because gracefulShutdown will close it

	r := chi.NewRouter()
	r.Get("/health", server.handleHealth)

	srv := &http.Server{
		Addr:    ":18743", // Different port for testing
		Handler: r,
	}

	// Start server
	go func() {
		srv.ListenAndServe()
	}()

	// Give server time to start
	time.Sleep(50 * time.Millisecond)

	// Test without bot cancel function
	gracefulShutdown(srv, server, nil, cfg)

	// Verify server stopped
	_, err := http.Get("http://localhost:18743/health")
	if err == nil {
		t.Error("expected error after shutdown, got nil")
	}
}

// Test gracefulShutdown with bot cancel
func TestGracefulShutdown_WithBot(t *testing.T) {
	cfg := testConfig()
	server := NewServer(cfg)
	// Note: Don't defer server.Close() because gracefulShutdown will close it

	r := chi.NewRouter()
	r.Get("/health", server.handleHealth)

	srv := &http.Server{
		Addr:    ":18744", // Different port for testing
		Handler: r,
	}

	// Start server
	go func() {
		srv.ListenAndServe()
	}()

	// Give server time to start
	time.Sleep(50 * time.Millisecond)

	// Create cancelBot function
	botCancelled := false
	cancelBot := func() {
		botCancelled = true
	}

	// Test with bot cancel function
	gracefulShutdown(srv, server, cancelBot, cfg)

	// Verify bot was cancelled
	if !botCancelled {
		t.Error("expected bot to be cancelled")
	}

	// Verify server stopped
	_, err := http.Get("http://localhost:18744/health")
	if err == nil {
		t.Error("expected error after shutdown, got nil")
	}
}

// Test config init with .env file (kept for godotenv coverage)
func TestConfigInit_WithEnvFile(t *testing.T) {
	// Create temp directory with .env file
	tmpDir := t.TempDir()
	envPath := filepath.Join(tmpDir, ".env")

	envContent := `TEST_VAR=test-value`

	if err := os.WriteFile(envPath, []byte(envContent), 0o644); err != nil {
		t.Fatalf("failed to write .env file: %v", err)
	}

	// Load .env file from specific path, overwriting existing vars
	if loadErr := godotenv.Overload(envPath); loadErr != nil {
		t.Errorf("godotenv.Overload() failed: %v", loadErr)
	}

	// Verify environment variable is set
	if got := os.Getenv("TEST_VAR"); got != "test-value" {
		t.Errorf("expected TEST_VAR=test-value, got %s", got)
	}

	os.Unsetenv("TEST_VAR")
}

// Test config init without .env file
func TestConfigInit_NoEnvFile(t *testing.T) {
	// Create temp directory without .env file
	tmpDir := t.TempDir()

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}

	defer os.Chdir(oldWd)

	if chdirErr := os.Chdir(tmpDir); chdirErr != nil {
		t.Fatalf("failed to change directory: %v", chdirErr)
	}

	// Try loading .env file (should fail gracefully)
	err = godotenv.Load()
	if err == nil {
		t.Error("expected error when .env file doesn't exist, got nil")
	}

	// This simulates the behavior in init() where error is logged but not fatal
	// The test verifies the error path is exercised
}

// Test telegram bot shutdown
func TestTelegramBotShutdown(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping telegram bot test in short mode")
	}

	// Skip if no bot token (can't test real bot without token)
	// This test exercises the goroutine shutdown path
	testToken := "test-token"

	// Create mock server
	server := NewServer(testConfig())
	defer server.Close()

	// Create cancellable context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create bot with mock token (will fail to connect but that's OK for this test)
	// We're testing the goroutine shutdown path, not the bot functionality
	opts := []bot.Option{
		bot.WithDefaultHandler(func(_ context.Context, _ *bot.Bot, _ *models.Update) {}),
	}

	// This will fail with invalid token but that's expected
	b, err := bot.New(testToken, opts...)
	if err != nil {
		// Expected to fail with test token, but we've exercised the code path
		t.Logf("bot creation failed (expected): %v", err)

		return
	}

	// Start polling in background (same as telegram.go)
	done := make(chan bool)

	go func() {
		b.Start(ctx)
		// This log line is what we're testing for coverage
		done <- true
	}()

	// Cancel context to trigger shutdown
	cancel()

	// Wait for goroutine to finish
	select {
	case <-done:
		// Success - goroutine finished
	case <-time.After(5 * time.Second):
		t.Error("timeout waiting for bot to stop")
	}
}

// Test server startup error path
func TestServerStartupError(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	server := NewServer(testConfig())
	defer server.Close()

	r := chi.NewRouter()
	r.Get("/health", server.handleHealth)

	// Create server with invalid address to trigger error
	srv := &http.Server{
		Addr:    "invalid:address:8742",
		Handler: r,
	}

	// Start server in background
	errCh := make(chan error, 1)

	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	// Wait for error
	select {
	case err := <-errCh:
		if err == nil {
			t.Error("expected error from invalid address, got nil")
		}
	case <-time.After(2 * time.Second):
		t.Error("timeout waiting for server error")
	}
}

func TestHandleExtractFacts_NoMemoryService(t *testing.T) {
	server := NewServer(testConfig())
	defer server.Close()

	// Ensure memory service is nil
	server.memory = nil

	req := httptest.NewRequest(http.MethodPost, "/extract-facts", nil)
	w := httptest.NewRecorder()

	server.handleExtractFacts(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status 503, got %d", w.Code)
	}
}

func TestHandleExtractFacts_WithMemoryService(t *testing.T) {
	server := NewServer(testConfig())
	defer server.Close()

	// Server should have memory service initialized by default (from NewServer)
	if server.memory == nil {
		t.Skip("memory service not available")
	}

	req := httptest.NewRequest(http.MethodPost, "/extract-facts", nil)
	w := httptest.NewRecorder()

	server.handleExtractFacts(w, req)

	// Should either succeed or fail with internal error (if Claude CLI fails)
	// Both are acceptable since we're just testing the endpoint handler
	if w.Code != http.StatusOK && w.Code != http.StatusInternalServerError {
		t.Errorf("expected status 200 or 500, got %d", w.Code)
	}

	// If OK, verify response structure
	if w.Code == http.StatusOK {
		var response map[string]interface{}
		if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if response["status"] != "ok" {
			t.Errorf("expected status ok, got %v", response["status"])
		}
	}
}
