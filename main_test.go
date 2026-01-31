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
