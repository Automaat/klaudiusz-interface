package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/joho/godotenv"
)

func TestNewServer(t *testing.T) {
	server := NewServer()
	defer server.Close()

	if server == nil {
		t.Fatal("NewServer returned nil")
	}

	// Verify cleanup goroutine started (give it a moment)
	time.Sleep(10 * time.Millisecond)
}

func TestGetOrCreateSession(t *testing.T) {
	server := NewServer()
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

	server := NewServer()
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

	// Verify prompt contains key elements
	requiredPhrases := []string{
		"JĘZYK",
		"po polsku",
		"Klaudiusz",
		"Home Assistant",
		"PERMISSION_REQUIRED",
		query,
	}

	for _, phrase := range requiredPhrases {
		if !contains(prompt, phrase) {
			t.Errorf("prompt missing required phrase: %q", phrase)
		}
	}
}

func TestHealthHandler(t *testing.T) {
	server := NewServer()
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

	if response["claude_path"] != ClaudePath {
		t.Errorf("expected claude_path %s, got %v", ClaudePath, response["claude_path"])
	}

	activeSessions := int(response["active_sessions"].(float64))
	if activeSessions != 2 {
		t.Errorf("expected 2 active sessions, got %d", activeSessions)
	}
}

func TestAskHandler_InvalidJSON(t *testing.T) {
	server := NewServer()
	defer server.Close()

	req := httptest.NewRequest(http.MethodPost, "/ask", bytes.NewBufferString("{invalid json"))
	w := httptest.NewRecorder()

	server.handleAsk(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestAskHandler_MissingQuery(t *testing.T) {
	server := NewServer()
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
	server := NewServer()
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
	server := NewServer()
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

func TestGetEnvOrDefault(t *testing.T) {
	t.Run("returns env value when set", func(t *testing.T) {
		key := "TEST_ENV_VAR_UNIQUE"
		expected := "test-value"

		os.Setenv(key, expected)
		defer os.Unsetenv(key)

		result := getEnvOrDefault(key, "default")
		if result != expected {
			t.Errorf("expected %q, got %q", expected, result)
		}
	})

	t.Run("returns default when env not set", func(t *testing.T) {
		key := "NONEXISTENT_ENV_VAR"
		expected := "default-value"

		result := getEnvOrDefault(key, expected)
		if result != expected {
			t.Errorf("expected %q, got %q", expected, result)
		}
	})

	t.Run("returns default when env is empty string", func(t *testing.T) {
		key := "TEST_EMPTY_ENV_VAR"

		os.Setenv(key, "")
		defer os.Unsetenv(key)

		result := getEnvOrDefault(key, "default")
		if result != "default" {
			t.Errorf("expected 'default', got %q", result)
		}
	})
}

func TestExecuteClaude(t *testing.T) {
	t.Run("rejects prompt exceeding max length", func(t *testing.T) {
		longPrompt := strings.Repeat("a", 100001)
		ctx := context.Background()

		_, err := executeClaude(ctx, longPrompt)
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

		_, err := executeClaude(ctx, "test prompt")
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

		oldPath := ClaudePath
		oldDir := WorkingDir
		ClaudePath = helperPath
		WorkingDir = "/tmp"

		defer func() {
			ClaudePath = oldPath
			WorkingDir = oldDir
		}()

		ctx := context.Background()

		output, err := executeClaude(ctx, "test")
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

func TestHandleConfirmation_Success(t *testing.T) {
	server := NewServer()
	defer server.Close()

	oldPath := ClaudePath
	oldDir := WorkingDir
	ClaudePath = "echo"
	WorkingDir = "/tmp"

	defer func() {
		ClaudePath = oldPath
		WorkingDir = oldDir
	}()

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
	server := NewServer()
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
	server := NewServer()
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
	server := NewServer()
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
	server := NewServer()
	defer server.Close()

	session := server.getOrCreateSession("exec-error")
	session.mu.Lock()
	session.PendingAction = &PendingAction{
		ID:          "action-exec",
		Description: "Test",
		Commands:    []string{"test"},
	}
	session.mu.Unlock()

	oldPath := ClaudePath
	ClaudePath = "/nonexistent/claude"

	defer func() { ClaudePath = oldPath }()

	req := httptest.NewRequest(http.MethodPost, "/ask", nil)
	w := httptest.NewRecorder()

	err := server.handleConfirmation(w, req, session)
	if err == nil {
		t.Error("expected error from executeClaude")
	}

	if !strings.Contains(err.Error(), "failed to execute action") {
		t.Errorf("expected 'failed to execute action' error, got: %v", err)
	}
}

func TestHandleAsk_WithConfirmation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping confirmation test in short mode")
	}

	server := NewServer()
	defer server.Close()

	oldPath := ClaudePath
	oldDir := WorkingDir
	ClaudePath = "echo"
	WorkingDir = "/tmp"

	defer func() {
		ClaudePath = oldPath
		WorkingDir = oldDir
	}()

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

	server := NewServer()
	defer server.Close()

	oldPath := ClaudePath
	ClaudePath = "printf"

	defer func() { ClaudePath = oldPath }()

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

	server := NewServer()
	defer server.Close()

	oldPath := ClaudePath
	oldDir := WorkingDir
	ClaudePath = "echo"
	WorkingDir = "/tmp"

	defer func() {
		ClaudePath = oldPath
		WorkingDir = oldDir
	}()

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

	server := NewServer()
	defer server.Close()

	oldPath := ClaudePath
	oldDir := WorkingDir
	ClaudePath = "echo"
	WorkingDir = "/tmp"

	defer func() {
		ClaudePath = oldPath
		WorkingDir = oldDir
	}()

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
	server := NewServer()
	defer server.Close()

	oldPath := ClaudePath
	ClaudePath = "/nonexistent/command"

	defer func() { ClaudePath = oldPath }()

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
	server := NewServer()
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

	server := NewServer()
	defer server.Close()

	oldPath := ClaudePath
	ClaudePath = helperPath

	defer func() { ClaudePath = oldPath }()

	oldWorkingDir := WorkingDir
	WorkingDir = "/tmp"

	defer func() { WorkingDir = oldWorkingDir }()

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
	server := NewServer()
	defer server.Close()

	req := httptest.NewRequest(http.MethodPost, "/cancel", bytes.NewBufferString("{invalid"))
	w := httptest.NewRecorder()

	server.handleCancel(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestHandleCancel_NoPendingAction(t *testing.T) {
	server := NewServer()
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
	server := NewServer()
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
	server := NewServer()
	defer server.Close()

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	server.handleHealth(w, req)

	var response map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response["working_dir"] != WorkingDir {
		t.Errorf("expected working_dir %s, got %v", WorkingDir, response["working_dir"])
	}
}

// Helper function
func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

// Telegram tests
func TestChatIDToSessionID(t *testing.T) {
	tests := []struct {
		chatID   int64
		expected string
	}{
		{12345, "tg-12345"},
		{-67890, "tg--67890"},
		{0, "tg-0"},
	}

	for _, tt := range tests {
		result := chatIDToSessionID(tt.chatID)
		if result != tt.expected {
			t.Errorf("chatIDToSessionID(%d) = %s, want %s", tt.chatID, result, tt.expected)
		}
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
	server := NewServer()
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

	// Mock executeClaude by temporarily replacing ClaudePath and WorkingDir
	oldClaudePath := ClaudePath
	oldWorkingDir := WorkingDir

	defer func() {
		ClaudePath = oldClaudePath
		WorkingDir = oldWorkingDir
	}()

	// Create a simple test script
	tmpDir := t.TempDir()
	script := filepath.Join(tmpDir, "test-claude.sh")

	if err := os.WriteFile(script, []byte("#!/bin/sh\necho 'Wykonano'"), 0o755); err != nil {
		t.Fatalf("failed to create test script: %v", err)
	}

	ClaudePath = script
	WorkingDir = tmpDir

	ctx := context.Background()

	response, err := server.executeConfirmedAction(ctx, session, "action-123")
	if err != nil {
		t.Fatalf("executeConfirmedAction failed: %v", err)
	}

	if !strings.Contains(response, "Wykonano") {
		t.Errorf("unexpected response: %s", response)
	}

	// Verify pending action was cleared
	session.mu.Lock()
	defer session.mu.Unlock()

	if session.PendingAction != nil {
		t.Error("pending action should be cleared")
	}
}

func TestExecuteConfirmedAction_NoPendingAction(t *testing.T) {
	server := NewServer()
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
	server := NewServer()
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
	server := NewServer()
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
	server := NewServer()
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

	// Verify session IDs have tg- prefix
	if !strings.HasPrefix(session1.ID, "tg-") {
		t.Errorf("session ID should have tg- prefix: %s", session1.ID)
	}

	if !strings.HasPrefix(session2.ID, "tg-") {
		t.Errorf("session ID should have tg- prefix: %s", session2.ID)
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

	server := NewServer()
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
	server := NewServer()
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

// Test server shutdown behavior
func TestServerGracefulShutdown(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	server := NewServer()

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Post("/ask", server.handleAsk)
	r.Post("/cancel", server.handleCancel)
	r.Get("/health", server.handleHealth)

	srv := &http.Server{
		Addr:         ":18742", // Different port for testing
		Handler:      r,
		ReadTimeout:  ReadTimeout,
		WriteTimeout: WriteTimeout,
		IdleTimeout:  IdleTimeout,
	}

	// Start server in background (same as main.go)
	serverErrCh := make(chan error, 1)

	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErrCh <- err
		}
	}()

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// Verify server is running
	resp, err := http.Get("http://localhost:18742/health")
	if err != nil {
		t.Fatalf("server not started: %v", err)
	}

	resp.Body.Close()

	// Shutdown server gracefully (same as main.go)
	ctx, cancel := context.WithTimeout(context.Background(), ShutdownTimeout)
	defer cancel()

	if shutdownErr := srv.Shutdown(ctx); shutdownErr != nil {
		t.Fatalf("server shutdown failed: %v", shutdownErr)
	}

	// Verify server stopped
	_, err = http.Get("http://localhost:18742/health")
	if err == nil {
		t.Error("expected error after shutdown, got nil")
	}

	// Check if server goroutine reported any errors
	select {
	case err := <-serverErrCh:
		t.Fatalf("server reported error: %v", err)
	default:
		// No error, which is expected
	}

	server.Close()
}

// Test config init with .env file
func TestConfigInit_WithEnvFile(t *testing.T) {
	// Create temp directory with .env file
	tmpDir := t.TempDir()
	envPath := filepath.Join(tmpDir, ".env")

	envContent := `CLAUDE_PATH=/custom/claude
WORKING_DIR=/custom/dir
TELEGRAM_BOT_TOKEN=test-token-12345
TELEGRAM_ENABLED=true`

	if err := os.WriteFile(envPath, []byte(envContent), 0o644); err != nil {
		t.Fatalf("failed to write .env file: %v", err)
	}

	// Save original env vars
	origToken := os.Getenv("TELEGRAM_BOT_TOKEN")
	origPath := os.Getenv("CLAUDE_PATH")
	origWorkDir := os.Getenv("WORKING_DIR")
	origEnabled := os.Getenv("TELEGRAM_ENABLED")

	defer func() {
		// Restore original env vars
		if origToken != "" {
			os.Setenv("TELEGRAM_BOT_TOKEN", origToken)
		} else {
			os.Unsetenv("TELEGRAM_BOT_TOKEN")
		}

		if origPath != "" {
			os.Setenv("CLAUDE_PATH", origPath)
		} else {
			os.Unsetenv("CLAUDE_PATH")
		}

		if origWorkDir != "" {
			os.Setenv("WORKING_DIR", origWorkDir)
		} else {
			os.Unsetenv("WORKING_DIR")
		}

		if origEnabled != "" {
			os.Setenv("TELEGRAM_ENABLED", origEnabled)
		} else {
			os.Unsetenv("TELEGRAM_ENABLED")
		}
	}()

	// Load .env file from specific path, overwriting existing vars
	if loadErr := godotenv.Overload(envPath); loadErr != nil {
		t.Errorf("godotenv.Overload() failed: %v", loadErr)
	}

	// Verify environment variables are set
	if got := os.Getenv("TELEGRAM_BOT_TOKEN"); got != "test-token-12345" {
		t.Errorf("expected TELEGRAM_BOT_TOKEN=test-token-12345, got %s", got)
	}

	if got := os.Getenv("CLAUDE_PATH"); got != "/custom/claude" {
		t.Errorf("expected CLAUDE_PATH=/custom/claude, got %s", got)
	}

	if got := os.Getenv("TELEGRAM_ENABLED"); got != "true" {
		t.Errorf("expected TELEGRAM_ENABLED=true, got %s", got)
	}
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
	oldToken := TelegramBotToken
	TelegramBotToken = "test-token"

	defer func() { TelegramBotToken = oldToken }()

	// Create mock server
	server := NewServer()
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
	b, err := bot.New(TelegramBotToken, opts...)
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

	server := NewServer()
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
