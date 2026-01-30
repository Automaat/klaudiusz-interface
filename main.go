// Package main provides HTTP API wrapper for Claude Code CLI
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
)

const (
	port                   = "8742"
	readTimeout            = 15 * time.Second
	writeTimeout           = 15 * time.Second
	idleTimeout            = 60 * time.Second
	ClaudePath             = "/Users/marcin.skalski@konghq.com/.local/bin/claude"
	WorkingDir             = "/Users/marcin.skalski@konghq.com/sideprojects/klaudiusz-smart-home"
	SessionTimeout         = 5 * time.Minute
	permissionRegexMatches = 3
	claudeExecutionTimeout = 30 * time.Second
)

// Pre-compile for performance
var dangerousPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)wyłącz wszystk`),
	regexp.MustCompile(`(?i)turn off all`),
	regexp.MustCompile(`(?i)zamknij dom`),
	regexp.MustCompile(`(?i)ustaw temperatur[ęe] (na|do) (1[0-5]|[0-9])($|\s)`),
}

type PendingAction struct {
	ID          string   `json:"id"`
	Description string   `json:"description"`
	Commands    []string `json:"commands"`
}

type Session struct {
	ID            string
	LastActivity  time.Time
	PendingAction *PendingAction
	mu            sync.Mutex
}

type Server struct {
	sessions sync.Map // map[string]*Session
}

func NewServer() *Server {
	s := &Server{}
	go s.cleanupSessions()

	return s
}

func (s *Server) cleanupSessions() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		now := time.Now()

		s.sessions.Range(func(key, value interface{}) bool {
			session, ok := value.(*Session)
			if !ok {
				return true
			}

			session.mu.Lock()
			expired := now.Sub(session.LastActivity) > SessionTimeout
			session.mu.Unlock()

			if expired {
				log.Printf("Session %s expired", session.ID)
				s.sessions.Delete(key)
			}

			return true
		})
	}
}

func (s *Server) getOrCreateSession(sessionID string) *Session {
	if sessionID == "" {
		sessionID = uuid.New().String()
	}

	val, _ := s.sessions.LoadOrStore(sessionID, &Session{
		ID:           sessionID,
		LastActivity: time.Now(),
	})

	session, ok := val.(*Session)
	if !ok {
		log.Printf("Failed to cast session for ID: %s", sessionID)

		return &Session{
			ID:           sessionID,
			LastActivity: time.Now(),
		}
	}

	session.mu.Lock()
	session.LastActivity = time.Now()
	session.mu.Unlock()

	return session
}

func isDangerousAction(text string) bool {
	for _, pattern := range dangerousPatterns {
		if pattern.MatchString(text) {
			return true
		}
	}

	return false
}

func executeClaude(ctx context.Context, prompt string, sessionID string) (string, error) {
	args := []string{
		"-p",
		"--working-directory", WorkingDir,
	}

	if sessionID != "" {
		args = append(args, "--session-id", sessionID)
	}

	args = append(args, prompt)

	cmd := exec.CommandContext(ctx, ClaudePath, args...)

	var stdout, stderr bytes.Buffer

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", errors.Wrapf(err, "claude execution failed: %s", stderr.String())
	}

	return strings.TrimSpace(stdout.String()), nil
}

func parsePermissionRequest(response string) (*PendingAction, bool) {
	if !strings.Contains(response, "PERMISSION_REQUIRED:") {
		return nil, false
	}

	re := regexp.MustCompile(`PERMISSION_REQUIRED: (.+?) \| COMMANDS: (.+)`)

	matches := re.FindStringSubmatch(response)

	if len(matches) != permissionRegexMatches {
		return nil, false
	}

	description := strings.TrimSpace(matches[1])
	commandsStr := matches[2]

	commands := strings.Split(commandsStr, ",")
	for i := range commands {
		commands[i] = strings.TrimSpace(commands[i])
	}

	return &PendingAction{
		ID:          uuid.New().String(),
		Description: description,
		Commands:    commands,
	}, true
}

func buildSystemPrompt(query string) string {
	return fmt.Sprintf(`JĘZYK: Odpowiadaj TYLKO po polsku.
FORMAT: Zwięzłe odpowiedzi dla głosowego wyjścia (max 2-3 zdania).
KONTEKST: Jesteś polskim asystentem domowym Klaudiusz.

NARZĘDZIA:
- Masz dostęp do Home Assistant przez ha-mcp
- Możesz sprawdzać temperaturę, światła, sensory
- Możesz kontrolować urządzenia

BEZPIECZEŃSTWO:
- Dla niebezpiecznych akcji użyj: "PERMISSION_REQUIRED: [opis] | COMMANDS: [lista]"
- Przykład: "PERMISSION_REQUIRED: Wyłączyć wszystkie światła | COMMANDS: light.turn_off_all"

Pytanie: %s

Odpowiedź (po polsku, zwięźle):`, query)
}

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

	executePrompt := fmt.Sprintf(`WYKONAJ: %s
Użyj narzędzi ha-mcp aby wykonać powyższe komendy.
Odpowiedz krótko "Wykonano" gdy zakończysz.`, strings.Join(action.Commands, ", "))

	ctx, cancel := context.WithTimeout(r.Context(), claudeExecutionTimeout)
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

	ctx, cancel := context.WithTimeout(r.Context(), claudeExecutionTimeout)
	defer cancel()

	response, err := executeClaude(ctx, systemPrompt, session.ID)
	if err != nil {
		log.Printf("Claude error: %v", err)

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

		confirmMsg := action.Description +
			". Powiedz 'Tak' aby potwierdzić lub 'Nie' aby anulować."
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

func main() {
	server := NewServer()

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Post("/ask", server.handleAsk)
	r.Post("/cancel", server.handleCancel)
	r.Get("/health", server.handleHealth)

	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      r,
		ReadTimeout:  readTimeout,
		WriteTimeout: writeTimeout,
		IdleTimeout:  idleTimeout,
	}

	log.Printf("Starting Claude HA Brain server on port %s", port)
	log.Printf("Claude CLI: %s", ClaudePath)
	log.Printf("Working directory: %s", WorkingDir)
	log.Printf("Session timeout: %.0f minutes", SessionTimeout.Minutes())

	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
