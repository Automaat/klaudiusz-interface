package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
)

func (*Server) handleConfirmation(
	w http.ResponseWriter,
	r *http.Request,
	session *Session,
) error {
	session.mu.Lock()
	action := session.PendingAction
	session.PendingAction = nil
	session.mu.Unlock()

	if action == nil {
		return errors.New("no pending action")
	}

	// Validate commands format
	for _, cmd := range action.Commands {
		if cmd == "" || len(cmd) > 1000 {
			return errors.Newf("invalid command format: %q", cmd)
		}
	}

	executePrompt := fmt.Sprintf(`WYKONAJ: %s
Użyj narzędzi ha-mcp aby wykonać powyższe komendy.
Odpowiedz krótko "Wykonano" gdy zakończysz.`, strings.Join(action.Commands, ", "))

	ctx, cancel := context.WithTimeout(r.Context(), ClaudeExecutionTimeout)
	defer cancel()

	response, err := executeClaude(ctx, executePrompt, session.ID)
	if err != nil {
		return errors.Wrap(err, "failed to execute action")
	}

	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"text":            response,
		"language":        "pl",
		"session_id":      session.ID,
		"action_executed": true,
	}); err != nil {
		return errors.Wrap(err, "failed to encode response")
	}

	return nil
}

func (s *Server) handleAsk(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Query         string `json:"query"`
		SessionID     string `json:"session_id"`
		ConfirmAction bool   `json:"confirm_action"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, errors.Wrap(err, "invalid JSON").Error(), http.StatusBadRequest)
		return
	}

	if req.Query == "" {
		http.Error(w, "missing query", http.StatusBadRequest)
		return
	}

	session := s.getOrCreateSession(req.SessionID)

	w.Header().Set("Content-Type", "application/json")

	// Handle confirmation
	if req.ConfirmAction {
		if err := s.handleConfirmation(w, r, session); err != nil {
			log.Printf("Confirmation error: %v", err)
			w.WriteHeader(http.StatusInternalServerError)

			if encErr := json.NewEncoder(w).Encode(map[string]interface{}{
				"text":  "Przepraszam, nie mogę wykonać akcji.",
				"error": err.Error(),
			}); encErr != nil {
				log.Printf("Failed to encode error response: %v", encErr)
			}
		}

		return
	}

	// Execute query
	systemPrompt := buildSystemPrompt(req.Query)

	ctx, cancel := context.WithTimeout(r.Context(), ClaudeExecutionTimeout)
	defer cancel()

	response, err := executeClaude(ctx, systemPrompt, session.ID)
	if err != nil {
		log.Printf("Claude error: %v", err)
		w.WriteHeader(http.StatusInternalServerError)

		if encErr := json.NewEncoder(w).Encode(map[string]interface{}{
			"text":  "Przepraszam, nie mogę teraz odpowiedzieć.",
			"error": err.Error(),
		}); encErr != nil {
			log.Printf("Failed to encode error response: %v", encErr)
		}

		return
	}

	// Check permission required
	if action, needsPermission := parsePermissionRequest(response); needsPermission {
		session.mu.Lock()
		session.PendingAction = action
		session.mu.Unlock()

		// Check if description ends with punctuation
		confirmMsg := action.Description

		lastChar := confirmMsg[len(confirmMsg)-1]
		if lastChar != '.' && lastChar != '!' && lastChar != '?' {
			confirmMsg += "."
		}

		confirmMsg += " Powiedz 'Tak' aby potwierdzić lub 'Nie' aby anulować."
		if encErr := json.NewEncoder(w).Encode(map[string]interface{}{
			"text":                confirmMsg,
			"language":            "pl",
			"session_id":          session.ID,
			"requires_permission": true,
			"action_id":           action.ID,
			"action_description":  action.Description,
		}); encErr != nil {
			log.Printf("Failed to encode permission response: %v", encErr)
		}

		return
	}

	// Normal response
	if isDangerousAction(req.Query) {
		log.Printf("WARNING: Dangerous query not flagged: %s", req.Query)
	}

	if encErr := json.NewEncoder(w).Encode(map[string]interface{}{
		"text":       response,
		"language":   "pl",
		"session_id": session.ID,
		"timestamp":  time.Now().Format(time.RFC3339),
	}); encErr != nil {
		log.Printf("Failed to encode response: %v", encErr)
	}
}

func (s *Server) handleCancel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SessionID string `json:"session_id"`
	}

	w.Header().Set("Content-Type", "application/json")

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, errors.Wrap(err, "invalid JSON").Error(), http.StatusBadRequest)
		return
	}

	val, ok := s.sessions.Load(req.SessionID)
	if !ok {
		if encErr := json.NewEncoder(w).Encode(map[string]interface{}{
			"text":      "Nie ma oczekującej akcji.",
			"cancelled": false,
		}); encErr != nil {
			log.Printf("Failed to encode response: %v", encErr)
		}

		return
	}

	session, ok := val.(*Session)
	if !ok {
		log.Printf("Failed to cast session for ID: %s", req.SessionID)
		http.Error(w, "internal server error", http.StatusInternalServerError)

		return
	}

	session.mu.Lock()
	hasPending := session.PendingAction != nil
	session.PendingAction = nil
	session.mu.Unlock()

	if encErr := json.NewEncoder(w).Encode(map[string]interface{}{
		"text":      "Anulowano akcję.",
		"cancelled": hasPending,
	}); encErr != nil {
		log.Printf("Failed to encode response: %v", encErr)
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	activeSessions := 0

	s.sessions.Range(func(_, _ interface{}) bool {
		activeSessions++
		return true
	})

	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"status":          "ok",
		"claude_path":     ClaudePath,
		"active_sessions": activeSessions,
		"working_dir":     WorkingDir,
	}); err != nil {
		log.Printf("Failed to encode health response: %v", err)
	}
}
