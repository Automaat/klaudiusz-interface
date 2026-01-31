# klaudiusz-interface Implementation Plan

## Overview

Replace stub handlers in main.go with full Claude Code HTTP wrapper implementation from brain-plan.md. Chi router + strict CI already configured.

## Current State

- **Repo:** github.com/Automaat/klaudiusz-interface
- **main.go:** 68 lines, chi router skeleton, 3 stub handlers
- **CI:** 92 linters (golangci-lint v2.2.0), race detector, format checks, coverage tracking
- **Deps:** chi v5.2.4, google/uuid v1.6.0, cockroachdb/errors v1.12.0

## Target Implementation

HTTP wrapper for Claude Code CLI with session management, permission system, dangerous action detection.

**Endpoints:**
- `POST /ask` - Query Claude with optional confirmation flow
- `POST /cancel` - Cancel pending dangerous action
- `GET /health` - Server status + active sessions

**Key features:**
- Session management: sync.Map, 5min timeout, cleanup goroutine
- Claude execution: `claude -p --working-directory --session-id`
- Permission system: dangerous action regex → confirmation → execution
- Polish language: prompts, responses, dangerous patterns

## Critical Files

### To Modify
- `main.go` - Replace stubs with full implementation (~375 lines)

### To Create
- `main_test.go` - Test suite (~250 lines, 11 tests, >80% coverage)

### Reference
- `brain-plan.md` - Complete code examples, flow diagrams, test scenarios
- `.golangci.yml` - Linter rules (key: forbidigo bans fmt.Errorf, funlen 100 lines max)

## Implementation Steps

### 1. Add Constants & Types (lines 14-50)

```go
const (
    ClaudePath     = "/Users/marcin.skalski@konghq.com/.local/bin/claude"
    WorkingDir     = "/Users/marcin.skalski@konghq.com/sideprojects/klaudiusz-smart-home"
    SessionTimeout = 5 * time.Minute
)

// Pre-compile for performance
var dangerousPatterns = []*regexp.Regexp{
    regexp.MustCompile(`(?i)wyłącz wszystk`),
    regexp.MustCompile(`(?i)turn off all`),
    regexp.MustCompile(`(?i)zamknij dom`),
    regexp.MustCompile(`(?i)ustaw temperatur[ęe] (na|do) (1[0-5]|[0-9])`),
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
    mu            sync.Mutex  // Protect PendingAction
}

type Server struct {
    sessions sync.Map  // map[string]*Session
}
```

### 2. Server Constructor & Session Management (lines 51-110)

```go
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
            session := value.(*Session)
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

    session := val.(*Session)
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
```

### 3. Claude Execution (lines 111-135)

```go
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
```

**Critical:** Use `errors.Wrapf` not `fmt.Errorf` (forbidigo linter rule)

### 4. Helper Functions (lines 136-220)

**Extract to satisfy funlen linter (100 line max):**

```go
func parsePermissionRequest(response string) (*PendingAction, bool) {
    if !strings.Contains(response, "PERMISSION_REQUIRED:") {
        return nil, false
    }

    re := regexp.MustCompile(`PERMISSION_REQUIRED: (.+?) \| COMMANDS: (.+)`)
    matches := re.FindStringSubmatch(response)
    if len(matches) != 3 {
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

func (s *Server) handleConfirmation(w http.ResponseWriter, r *http.Request, session *Session) error {
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

    ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
    defer cancel()

    response, err := executeClaude(ctx, executePrompt, session.ID)
    if err != nil {
        return errors.Wrap(err, "failed to execute action")
    }

    return json.NewEncoder(w).Encode(map[string]interface{}{
        "text":            response,
        "language":        "pl",
        "session_id":      session.ID,
        "action_executed": true,
    })
}
```

### 5. Replace handleAsk Stub (lines 221-290)

```go
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
            json.NewEncoder(w).Encode(map[string]interface{}{
                "text":  "Przepraszam, nie mogę wykonać akcji.",
                "error": err.Error(),
            })
        }
        return
    }

    // Execute query
    systemPrompt := buildSystemPrompt(req.Query)
    ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
    defer cancel()

    response, err := executeClaude(ctx, systemPrompt, session.ID)
    if err != nil {
        log.Printf("Claude error: %v", err)
        json.NewEncoder(w).Encode(map[string]interface{}{
            "text":  "Przepraszam, nie mogę teraz odpowiedzieć.",
            "error": err.Error(),
        })
        return
    }

    // Check permission required
    if action, needsPermission := parsePermissionRequest(response); needsPermission {
        session.mu.Lock()
        session.PendingAction = action
        session.mu.Unlock()

        json.NewEncoder(w).Encode(map[string]interface{}{
            "text":                fmt.Sprintf("%s. Powiedz 'Tak' aby potwierdzić lub 'Nie' aby anulować.", action.Description),
            "language":            "pl",
            "session_id":          session.ID,
            "requires_permission": true,
            "action_id":           action.ID,
            "action_description":  action.Description,
        })
        return
    }

    // Normal response
    if isDangerousAction(req.Query) {
        log.Printf("WARNING: Dangerous query not flagged: %s", req.Query)
    }

    json.NewEncoder(w).Encode(map[string]interface{}{
        "text":       response,
        "language":   "pl",
        "session_id": session.ID,
        "timestamp":  time.Now().Format(time.RFC3339),
    })
}
```

### 6. Replace handleCancel Stub (lines 291-320)

```go
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
        json.NewEncoder(w).Encode(map[string]interface{}{
            "text":      "Nie ma oczekującej akcji.",
            "cancelled": false,
        })
        return
    }

    session := val.(*Session)
    session.mu.Lock()
    hasPending := session.PendingAction != nil
    session.PendingAction = nil
    session.mu.Unlock()

    json.NewEncoder(w).Encode(map[string]interface{}{
        "text":      "Anulowano akcję.",
        "cancelled": hasPending,
    })
}
```

### 7. Update handleHealth (lines 321-340)

```go
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
```

### 8. Update main() (lines 341-365)

```go
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
```

### 9. Update Imports

```go
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
```

### 10. Create Test Suite (main_test.go)

**11 tests, >80% coverage:**

1. `TestNewServer` - Server creation
2. `TestGetOrCreateSession` - Session CRUD
3. `TestSessionCleanup` - Timeout cleanup (reduce timeout for test)
4. `TestIsDangerousAction` - Regex patterns (8 test cases)
5. `TestParsePermissionRequest` - Permission parsing
6. `TestBuildSystemPrompt` - Prompt builder
7. `TestHealthHandler` - Health endpoint
8. `TestAskHandler_InvalidJSON` - Validation
9. `TestAskHandler_MissingQuery` - Validation
10. `TestCancelHandler` - Cancel flow
11. Integration test with real Claude CLI (optional, may skip in CI)

**Race detector:** All tests run with `-race` via `mise run test`

## Linter Compliance

**Critical rules:**

1. **forbidigo:** No `fmt.Errorf` → use `errors.New/Wrap/Wrapf`
2. **funlen:** Max 100 lines/50 statements → extract helpers
3. **err113:** Wrap all errors with context
4. **contextcheck:** All exec/HTTP use context
5. **ireturn:** No interface returns (map[string]interface{} inline OK)

**Validation:** `mise run fmt && mise run lint && mise run test`

## Manual Testing

```bash
# Start server
go run main.go

# Health check
curl http://localhost:8742/health
# Expected: {"status":"ok","claude_path":"...","active_sessions":0}

# Ask query (requires claude CLI)
curl -X POST http://localhost:8742/ask \
  -H "Content-Type: application/json" \
  -d '{"query":"Jaka jest stolica Polski?"}'
# Expected: {"text":"Warszawa","language":"pl","session_id":"..."}

# Dangerous action
curl -X POST http://localhost:8742/ask \
  -H "Content-Type: application/json" \
  -d '{"query":"wyłącz wszystko"}'
# Expected: {"requires_permission":true,"action_description":"..."}

# Session timeout (wait 6 min, check logs)
# Expected: "Session xyz expired"
```

## Verification Checklist

- [ ] `mise run fmt` - no changes
- [ ] `mise run lint` - exit 0
- [ ] `mise run test` - all pass, >80% coverage, no races
- [ ] Manual health test - returns ok
- [ ] Manual ask test - returns response with session_id
- [ ] Session cleanup - logs expiration after 6 min
- [ ] Dangerous patterns - all 4 regex match correctly
- [ ] CI passes - all 4 jobs (test, lint, format, build)

## Dependencies

**No new dependencies** - all already in go.mod:
- cockroachdb/errors v1.12.0 (error handling)
- google/uuid v1.6.0 (session IDs)
- go-chi/chi/v5 v5.2.4 (router)

## Implementation Flow

1. Add constants, types, Server struct
2. Implement session management
3. Implement executeClaude + helpers
4. Replace handler stubs
5. Update main()
6. Create test suite
7. Run quality gates: fmt, lint, test
8. Manual integration testing
9. Commit with `-s -S`, create PR
