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

func (s *Server) executeConfirmedAction(
	ctx context.Context,
	session *Session,
	actionID string,
) (string, error) {
	cfg := s.config.Get()

	session.mu.Lock()
	action := session.PendingAction

	// Validate before clearing
	if action == nil {
		session.mu.Unlock()
		return "", errors.New("no pending action")
	}

	// Skip ID check for HTTP flow (actionID == "")
	if actionID != "" && action.ID != actionID {
		session.mu.Unlock()
		return "", errors.Newf("action ID mismatch: expected %s, got %s", action.ID, actionID)
	}

	// Only clear after validation succeeds
	session.PendingAction = nil
	session.mu.Unlock()

	// Validate commands format
	for _, cmd := range action.Commands {
		if cmd == "" || len(cmd) > 1000 {
			return "", errors.Newf("invalid command format: %q", cmd)
		}
	}

	executePrompt := fmt.Sprintf(`WYKONAJ: %s
Użyj narzędzi ha-mcp aby wykonać powyższe komendy.
Odpowiedz krótko "Wykonano" gdy zakończysz.`, strings.Join(action.Commands, ", "))

	execCtx, cancel := context.WithTimeout(ctx, cfg.Claude.ExecutionTimeout)
	defer cancel()

	response, err := executeClaude(
		execCtx,
		executePrompt,
		session,
		cfg.Claude.Path,
		cfg.Claude.WorkingDir,
		cfg.Claude.MaxPromptLength,
	)
	if err != nil {
		return "", errors.Wrap(err, "failed to execute action")
	}

	return response, nil
}

func (s *Server) handleConfirmation(
	w http.ResponseWriter,
	r *http.Request,
	session *Session,
) error {
	// HTTP flow: pass empty actionID to skip ID check
	response, err := s.executeConfirmedAction(r.Context(), session, "")
	if err != nil {
		return err
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

func buildConfirmationMessage(action *PendingAction) string {
	confirmMsg := strings.TrimSpace(action.Description)
	if confirmMsg == "" {
		confirmMsg = "Potwierdź wykonanie tej akcji"
	} else {
		lastChar := confirmMsg[len(confirmMsg)-1]
		if lastChar != '.' && lastChar != '!' && lastChar != '?' {
			confirmMsg += "."
		}
	}

	return confirmMsg + " Powiedz 'Tak' aby potwierdzić lub 'Nie' aby anulować."
}

func (*Server) respondPermissionRequired(
	w http.ResponseWriter,
	session *Session,
	action *PendingAction,
) {
	session.mu.Lock()
	session.PendingAction = action
	session.mu.Unlock()

	confirmMsg := buildConfirmationMessage(action)
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
}

func (*Server) respondNormal(
	w http.ResponseWriter,
	session *Session,
	response string,
) {
	if encErr := json.NewEncoder(w).Encode(map[string]interface{}{
		"text":       response,
		"language":   "pl",
		"session_id": session.ID,
		"timestamp":  time.Now().Format(time.RFC3339),
	}); encErr != nil {
		log.Printf("Failed to encode response: %v", encErr)
	}
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
	cfg := s.config.Get()

	ctx, cancel := context.WithTimeout(r.Context(), cfg.Claude.ExecutionTimeout)
	defer cancel()

	response, err := executeClaude(
		ctx,
		buildSystemPrompt(req.Query),
		session,
		cfg.Claude.Path,
		cfg.Claude.WorkingDir,
		cfg.Claude.MaxPromptLength,
	)
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
		s.respondPermissionRequired(w, session, action)
		return
	}

	// Normal response
	if isDangerousAction(req.Query) {
		log.Printf("WARNING: Dangerous query not flagged: %s", req.Query)
	}

	s.respondNormal(w, session, response)
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

	cfg := s.config.Get()

	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"status":          "ok",
		"claude_path":     cfg.Claude.Path,
		"active_sessions": activeSessions,
		"working_dir":     cfg.Claude.WorkingDir,
	}); err != nil {
		log.Printf("Failed to encode health response: %v", err)
	}
}

func (s *Server) handleExtractFacts(w http.ResponseWriter, r *http.Request) {
	if s.memory == nil {
		http.Error(w, "memory service not initialized", http.StatusServiceUnavailable)

		return
	}

	cfg := s.config.Get()

	ctx, cancel := context.WithTimeout(r.Context(), cfg.Memory.Extraction.AdminTimeout)
	defer cancel()

	if err := s.memory.ExtractFacts(ctx); err != nil {
		log.Printf("Manual fact extraction error: %v", err)

		http.Error(
			w,
			errors.Wrap(err, "fact extraction failed").Error(),
			http.StatusInternalServerError,
		)

		return
	}

	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "ok",
		"message": "fact extraction completed",
	}); err != nil {
		log.Printf("Failed to encode extraction response: %v", err)
	}
}
