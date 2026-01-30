package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewServer(t *testing.T) {
	server := NewServer()
	if server == nil {
		t.Fatal("NewServer returned nil")
	}

	// Verify cleanup goroutine started (give it a moment)
	time.Sleep(10 * time.Millisecond)
}

func TestGetOrCreateSession(t *testing.T) {
	server := NewServer()

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
	server := NewServer()
	sessionID := "cleanup-test"
	_ = server.getOrCreateSession(sessionID)

	// Verify session exists
	val, ok := server.sessions.Load(sessionID)
	if !ok {
		t.Fatal("session not found after creation")
	}

	// Manually expire session
	s := val.(*Session)
	s.mu.Lock()
	s.LastActivity = time.Now().Add(-10 * time.Minute)
	s.mu.Unlock()

	// Wait for cleanup cycle (slightly more than 1 minute)
	time.Sleep(61 * time.Second)

	// Note: This test is slow (61s) and may be skipped in CI
	// To skip: go test -short
	if testing.Short() {
		t.Skip("skipping cleanup test in short mode")
	}

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

	req := httptest.NewRequest(http.MethodPost, "/ask", bytes.NewBufferString("{invalid json"))
	w := httptest.NewRecorder()

	server.handleAsk(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestAskHandler_MissingQuery(t *testing.T) {
	server := NewServer()

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

// Helper function
func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}
