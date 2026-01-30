# Claude Code Headless MCP Server - Home Assistant Brain

## Overview

Run Claude Code as headless HTTP server on Mac, integrated with HA as AI brain. HA becomes interface for voice I/O, Claude handles reasoning, device control, personal assistant tasks.

**Architecture:**
```
Voice → Whisper STT → HA Conversation → HTTP → Claude Code MCP Server (Mac) → Response → Piper TTS
```

**Key decisions:**
- HTTP wrapper (not stdio) for HA rest_command integration
- Mac launchd service (always-on, auto-restart)
- Session-based conversations using `--session-id` (5 min timeout)
- Permission system for dangerous actions (voice confirmation required)
- HA state awareness via existing ha-mcp tools
- Polish responses via prompt engineering

## Critical Files

**New project (Mac):**
1. `~/sideprojects/claude-ha-brain/` - Go HTTP server wrapping Claude Code CLI
2. `~/Library/LaunchAgents/com.mskalski.claude-ha-brain.plist` - launchd auto-start service

**Port:** 8742 (non-obvious, unlikely conflicts)

**klaudiusz-smart-home repo:**
3. `hosts/homelab/home-assistant/claude-brain.nix` - HA REST command integration (NEW)
4. `hosts/homelab/home-assistant/default.nix` - Import claude-brain.nix
5. `hosts/homelab/home-assistant/intents.nix` - Add AskClaude intent
6. `custom_sentences/pl/intents.yaml` - Polish voice patterns
7. `secrets/secrets.yaml` - Mac IP address, authentication token

## Implementation Steps

**Technology:** Go (lightweight, single binary, fast, stdlib-only)
**Port:** 8742 (non-obvious, unlikely conflicts)

### Phase 1: Server Development (Mac)

#### 1.1 Project Setup

```bash
mkdir ~/sideprojects/claude-ha-brain
cd ~/sideprojects/claude-ha-brain

# Initialize Go module
go mod init claude-ha-brain

# No dependencies needed - stdlib only!
# Create main.go
touch main.go
```

#### 1.2 Core Server Implementation

**File:** `main.go`

```go
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

	"github.com/google/uuid"
)

const (
	ClaudePath     = "/Users/marcin.skalski@konghq.com/.local/bin/claude"
	WorkingDir     = "/Users/marcin.skalski@konghq.com/sideprojects/klaudiusz-smart-home"
	SessionTimeout = 5 * time.Minute
	Port           = "8742"
)

type PendingAction struct {
	ID          string   `json:"id"`
	Description string   `json:"description"`
	Commands    []string `json:"commands"`
}

type Session struct {
	ID            string
	LastActivity  time.Time
	PendingAction *PendingAction
}

type Server struct {
	sessions sync.Map
	mu       sync.RWMutex
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
			session := value.(*Session)
			if now.Sub(session.LastActivity) > SessionTimeout {
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
	session.LastActivity = time.Now()
	return session
}

func executeClaude(ctx context.Context, prompt string, sessionID string) (string, error) {
	args := []string{
		"-p", // Non-interactive headless mode
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
		return "", fmt.Errorf("claude failed: %v, stderr: %s", err, stderr.String())
	}

	return strings.TrimSpace(stdout.String()), nil
}

var dangerousPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)wyłącz wszystk`),
	regexp.MustCompile(`(?i)turn off all`),
	regexp.MustCompile(`(?i)zamknij dom`),
	regexp.MustCompile(`(?i)ustaw temperatur[ęe] (na|do) (1[0-5]|[0-9])`),
}

func isDangerousAction(text string) bool {
	for _, pattern := range dangerousPatterns {
		if pattern.MatchString(text) {
			return true
		}
	}
	return false
}

func (s *Server) handleAsk(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Query         string `json:"query"`
		SessionID     string `json:"session_id"`
		ConfirmAction bool   `json:"confirm_action"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if req.Query == "" {
		http.Error(w, "Missing query", http.StatusBadRequest)
		return
	}

	session := s.getOrCreateSession(req.SessionID)

	// Handle action confirmation
	if req.ConfirmAction && session.PendingAction != nil {
		action := session.PendingAction
		session.PendingAction = nil

		executePrompt := fmt.Sprintf(`
WYKONAJ: %s

Użyj narzędzi ha-mcp aby wykonać powyższe komendy.
Odpowiedz krótko "Wykonano" gdy zakończysz.
`, strings.Join(action.Commands, ", "))

		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()

		response, err := executeClaude(ctx, executePrompt, session.ID)
		if err != nil {
			log.Printf("Claude execution error: %v", err)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"text":  "Przepraszam, nie mogę wykonać akcji.",
				"error": err.Error(),
			})
			return
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"text":           response,
			"language":       "pl",
			"session_id":     session.ID,
			"action_executed": true,
		})
		return
	}

	// Build system prompt
	systemPrompt := fmt.Sprintf(`
JĘZYK: Odpowiadaj TYLKO po polsku.
FORMAT: Zwięzłe odpowiedzi dla głosowego wyjścia (max 2-3 zdania).
KONTEKST: Jesteś polskim asystentem domowym Klaudiusz.

NARZĘDZIA:
- Masz dostęp do Home Assistant przez ha-mcp (kontrola urządzeń, odczyt stanu)
- Możesz sprawdzać temperaturę, światła, sensory
- Możesz kontrolować urządzenia

BEZPIECZEŃSTWO:
- Dla niebezpiecznych akcji (wyłącz wszystko, drastyczna zmiana temp) użyj formatu:
  "PERMISSION_REQUIRED: [opis akcji] | COMMANDS: [lista komend]"
- Przykład: "PERMISSION_REQUIRED: Wyłączyć wszystkie światła i ogrzewanie | COMMANDS: light.turn_off_all, climate.set_temperature"

Pytanie użytkownika: %s

Odpowiedź (po polsku, zwięźle):
`, req.Query)

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	response, err := executeClaude(ctx, systemPrompt, session.ID)
	if err != nil {
		log.Printf("Claude execution error: %v", err)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"text":  "Przepraszam, nie mogę teraz odpowiedzieć.",
			"error": err.Error(),
		})
		return
	}

	// Check if permission required
	if strings.Contains(response, "PERMISSION_REQUIRED:") {
		re := regexp.MustCompile(`PERMISSION_REQUIRED: (.+?) \| COMMANDS: (.+)`)
		matches := re.FindStringSubmatch(response)
		if len(matches) == 3 {
			description := strings.TrimSpace(matches[1])
			commandsStr := matches[2]
			commands := strings.Split(commandsStr, ",")
			for i := range commands {
				commands[i] = strings.TrimSpace(commands[i])
			}

			actionID := uuid.New().String()
			session.PendingAction = &PendingAction{
				ID:          actionID,
				Description: description,
				Commands:    commands,
			}

			json.NewEncoder(w).Encode(map[string]interface{}{
				"text":               fmt.Sprintf("%s. Powiedz 'Tak' aby potwierdzić lub 'Nie' aby anulować.", description),
				"language":           "pl",
				"session_id":         session.ID,
				"requires_permission": true,
				"action_id":          actionID,
				"action_description": description,
			})
			return
		}
	}

	// Check if dangerous but Claude didn't flag
	if isDangerousAction(req.Query) && !strings.Contains(response, "PERMISSION_REQUIRED:") {
		log.Printf("WARNING: Query flagged as dangerous: %s", req.Query)
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"text":       response,
		"language":   "pl",
		"session_id": session.ID,
		"timestamp":  time.Now().Format(time.RFC3339),
	})
}

func (s *Server) handleCancel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SessionID string `json:"session_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
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
	if session.PendingAction != nil {
		session.PendingAction = nil
		json.NewEncoder(w).Encode(map[string]interface{}{
			"text":      "Anulowano akcję.",
			"cancelled": true,
		})
	} else {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"text":      "Nie ma oczekującej akcji.",
			"cancelled": false,
		})
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	activeSessions := 0
	s.sessions.Range(func(key, value interface{}) bool {
		activeSessions++
		return true
	})

	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":          "ok",
		"claude_path":     ClaudePath,
		"active_sessions": activeSessions,
	})
}

func main() {
	server := NewServer()

	http.HandleFunc("/ask", server.handleAsk)
	http.HandleFunc("/cancel", server.handleCancel)
	http.HandleFunc("/health", server.handleHealth)

	log.Printf("Claude HA Brain server starting on port %s", Port)
	log.Printf("Claude CLI: %s", ClaudePath)
	log.Printf("Working directory: %s", WorkingDir)
	log.Printf("Session timeout: %.0f minutes", SessionTimeout.Minutes())

	if err := http.ListenAndServe(":"+Port, nil); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
```

**Dependencies:** Add to `go.mod`:
```bash
go get github.com/google/uuid
```

#### 1.3 Build and Test

```bash
# Build binary
go build -o claude-ha-brain

# Run server
./claude-ha-brain

# In another terminal - test basic query
curl -X POST http://localhost:8742/ask \
  -H "Content-Type: application/json" \
  -d '{"query": "Co to jest fotosynteza?"}'

# Should return: {"text": "[Polish explanation]", "language": "pl", "session_id": "..."}

# Test health endpoint
curl http://localhost:8742/health
# Should return: {"status": "ok", "claude_path": "...", "active_sessions": 0}
```

### Phase 2: launchd Service (Mac Auto-Start)

#### 2.1 Get Mac IP Address

```bash
# Find Mac LAN IP
ipconfig getifaddr en0  # WiFi
# or
ipconfig getifaddr en1  # Ethernet

# Example: 192.168.0.150
# Save this for HA configuration
```

#### 2.2 Create launchd Plist

**File:** `~/Library/LaunchAgents/com.mskalski.claude-ha-brain.plist`

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
  "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>com.mskalski.claude-ha-brain</string>

  <key>ProgramArguments</key>
  <array>
    <string>/Users/marcin.skalski@konghq.com/sideprojects/claude-ha-brain/claude-ha-brain</string>
  </array>

  <key>WorkingDirectory</key>
  <string>/Users/marcin.skalski@konghq.com/sideprojects/claude-ha-brain</string>

  <key>EnvironmentVariables</key>
  <dict>
    <key>PATH</key>
    <string>/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin:/Users/marcin.skalski@konghq.com/.local/bin</string>
  </dict>

  <key>RunAtLoad</key>
  <true/>

  <key>KeepAlive</key>
  <true/>

  <key>StandardOutPath</key>
  <string>/Users/marcin.skalski@konghq.com/Library/Logs/claude-ha-brain.log</string>

  <key>StandardErrorPath</key>
  <string>/Users/marcin.skalski@konghq.com/Library/Logs/claude-ha-brain.error.log</string>
</dict>
</plist>
```

#### 2.3 Load and Start Service

```bash
# Load service
launchctl load ~/Library/LaunchAgents/com.mskalski.claude-ha-brain.plist

# Check status
launchctl list | grep claude-ha-brain

# Verify listening on port 8742
lsof -i :8742

# Monitor logs
tail -f ~/Library/Logs/claude-ha-brain.log
```

### Phase 3: Home Assistant Integration (NixOS)

#### 3.1 Create HA REST Integration

**File:** `hosts/homelab/home-assistant/claude-brain.nix`

```nix
{ config, lib, ... }:

{
  services.home-assistant.config = {

    # Input helpers for session management
    input_text = {
      claude_session = {
        name = "Claude Session ID";
        max = 100;
        initial = "";
      };
      claude_response = {
        name = "Claude Response Buffer";
        max = 500;
        initial = "";
      };
      claude_pending_action = {
        name = "Claude Pending Action";
        max = 500;
        initial = "";
      };
    };

    input_boolean.claude_awaiting_confirmation = {
      name = "Claude Awaiting Confirmation";
      initial = false;
    };

    # Shell commands for Claude server calls
    shell_command = {
      claude_ask = ''
        response=$(curl -s -X POST http://192.168.0.150:8742/ask \
          -H "Content-Type: application/json" \
          -d '{"query":"{{ query }}", "session_id":"{{ states('input_text.claude_session') }}"}')

        text=$(echo "$response" | jq -r '.text')
        session_id=$(echo "$response" | jq -r '.session_id')
        requires_permission=$(echo "$response" | jq -r '.requires_permission // false')
        action_desc=$(echo "$response" | jq -r '.action_description // ""')

        # Store session ID
        curl -s -X POST http://homelab:8123/api/states/input_text.claude_session \
          -H "Authorization: Bearer ${HA_TOKEN}" \
          -H "Content-Type: application/json" \
          -d "{\"state\":\"$session_id\"}"

        # Store response
        curl -s -X POST http://homelab:8123/api/states/input_text.claude_response \
          -H "Authorization: Bearer ${HA_TOKEN}" \
          -H "Content-Type: application/json" \
          -d "{\"state\":\"$text\"}"

        # If permission required, set flag
        if [ "$requires_permission" = "true" ]; then
          curl -s -X POST http://homelab:8123/api/states/input_text.claude_pending_action \
            -H "Authorization: Bearer ${HA_TOKEN}" \
            -H "Content-Type: application/json" \
            -d "{\"state\":\"$action_desc\"}"

          curl -s -X POST http://homelab:8123/api/states/input_boolean.claude_awaiting_confirmation \
            -H "Authorization: Bearer ${HA_TOKEN}" \
            -H "Content-Type: application/json" \
            -d "{\"state\":\"on\"}"
        fi
      '';

      claude_confirm = ''
        curl -s -X POST http://192.168.0.150:8742/ask \
          -H "Content-Type: application/json" \
          -d '{"query":"wykonaj", "session_id":"{{ states('input_text.claude_session') }}", "confirm_action":true}' \
          | jq -r '.text' | \
        xargs -I {} curl -s -X POST http://homelab:8123/api/states/input_text.claude_response \
          -H "Authorization: Bearer ${HA_TOKEN}" \
          -H "Content-Type: application/json" \
          -d "{\"state\":\"{}\"}"

        # Clear confirmation flag
        curl -s -X POST http://homelab:8123/api/states/input_boolean.claude_awaiting_confirmation \
          -H "Authorization: Bearer ${HA_TOKEN}" \
          -H "Content-Type: application/json" \
          -d "{\"state\":\"off\"}"
      '';

      claude_cancel = ''
        curl -s -X POST http://192.168.0.150:8742/cancel \
          -H "Content-Type: application/json" \
          -d '{"session_id":"{{ states('input_text.claude_session') }}"}' \
          | jq -r '.text' | \
        xargs -I {} curl -s -X POST http://homelab:8123/api/states/input_text.claude_response \
          -H "Authorization: Bearer ${HA_TOKEN}" \
          -H "Content-Type: application/json" \
          -d "{\"state\":\"{}\"}"

        # Clear confirmation flag
        curl -s -X POST http://homelab:8123/api/states/input_boolean.claude_awaiting_confirmation \
          -H "Authorization: Bearer ${HA_TOKEN}" \
          -H "Content-Type: application/json" \
          -d "{\"state\":\"off\"}"
      '';
    };

    # Template sensor for last response
    template = [
      {
        sensor = [{
          name = "Claude Last Response";
          unique_id = "claude_last_response";
          state = "{{ states('input_text.claude_response') }}";
          attributes = {
            session_id = "{{ states('input_text.claude_session') }}";
            awaiting_confirmation = "{{ is_state('input_boolean.claude_awaiting_confirmation', 'on') }}";
            pending_action = "{{ states('input_text.claude_pending_action') }}";
          };
        }];
      }
    ];
  };
}
```

#### 3.2 Import in default.nix

**File:** `hosts/homelab/home-assistant/default.nix`

Add to imports (around line 10-15):
```nix
imports = [
  ./automations.nix
  ./intents.nix
  ./claude-brain.nix  # ADD THIS
];
```

#### 3.3 Add Intent Handlers

**File:** `hosts/homelab/home-assistant/intents.nix`

Add to intent_script attribute set (end of file):

```nix
# Claude AI conversation
AskClaude = {
  action = [
    # Call Claude server with session support
    {
      action = "shell_command.claude_ask";
      data.query = "{{ query }}";
    }
    # Wait for response processing
    { delay.seconds = 6; }
  ];
  speech.text = "{{ states('input_text.claude_response') }}";
};

# Confirm dangerous action
ConfirmClaude = {
  action = [
    {
      condition = "state";
      entity_id = "input_boolean.claude_awaiting_confirmation";
      state = "on";
    }
    {
      action = "shell_command.claude_confirm";
    }
    { delay.seconds = 5; }
  ];
  speech.text = "{{ states('input_text.claude_response') }}";
};

# Cancel dangerous action
CancelClaude = {
  action = [
    {
      condition = "state";
      entity_id = "input_boolean.claude_awaiting_confirmation";
      state = "on";
    }
    {
      action = "shell_command.claude_cancel";
    }
    { delay.seconds = 2; }
  ];
  speech.text = "{{ states('input_text.claude_response') }}";
};
```

### Phase 4: Add Polish Voice Patterns

**File:** `custom_sentences/pl/intents.yaml`

Add at end:

```yaml
language: "pl"
intents:
  AskClaude:
    data:
      - required_keywords: [claude, zapytaj, powiedz, wyjaśnij, zrób, wykonaj]
        sentences:
          - "claude {query}"
          - "zapytaj claude {query}"
          - "powiedz mi {query}"
          - "wyjaśnij {query}"
          - "co to jest {query}"
          - "claude zrób {query}"
          - "claude wykonaj {query}"
          - "{query}"  # Fallback - any query goes to Claude if no intent matches

  ConfirmClaude:
    data:
      - required_keywords: [tak, potwierdź, zgoda, wykonaj, ok, dobrze]
        sentences:
          - "tak"
          - "potwierdź"
          - "zgoda"
          - "wykonaj"
          - "ok"
          - "dobrze"
          - "tak zrób to"

  CancelClaude:
    data:
      - required_keywords: [nie, anuluj, stop, nie rób, rezygnuj]
        sentences:
          - "nie"
          - "anuluj"
          - "stop"
          - "nie rób tego"
          - "rezygnuję"
          - "przerwij"
```

## Conversation Flow Examples

### Simple Query (No Permission Needed)
```
User: "Claude, jaka jest pogoda jutro?"
→ Claude queries HA weather sensor via ha-mcp
→ Response: "Jutro będzie słonecznie, 18 stopni"
→ Session ID stored for follow-ups
```

### Follow-up Query (Using Session)
```
User: "A pojutrze?"
→ Claude uses session context (knows "weather" topic)
→ Response: "Pojutrze deszcz, 14 stopni"
```

### Dangerous Action (Requires Permission)
```
User: "Claude, wyłącz wszystko i idę spać"
→ Claude detects dangerous action (multiple devices)
→ Response: "Wyłączyć wszystkie światła, ogrzewanie i telewizor? Powiedz 'Tak' aby potwierdzić."
→ Sets input_boolean.claude_awaiting_confirmation = on
→ Stores pending action in session

User: "Tak"
→ Triggers ConfirmClaude intent
→ Claude executes stored commands via ha-mcp
→ Response: "Wykonano. Dobranoc!"
```

### Cancel Dangerous Action
```
User: "Claude, wyłącz ogrzewanie w całym domu"
→ Response: "Wyłączyć ogrzewanie we wszystkich pomieszczeniach? Powiedz 'Tak' lub 'Nie'."

User: "Nie"
→ Triggers CancelClaude intent
→ Response: "Anulowano akcję"
→ Clears awaiting_confirmation flag
```

## Verification

### Mac Server Tests

```bash
# Health check
curl http://localhost:8742/health

# Ask query
curl -X POST http://localhost:8742/ask \
  -H "Content-Type: application/json" \
  -d '{"query": "Jaka jest stolica Polski?"}'

# Should return: {"text": "Warszawa", "language": "pl"}
```

### HA Integration Tests

```bash
# From homelab - basic query
ssh homelab "curl -X POST http://192.168.0.150:8742/ask \
  -H 'Content-Type: application/json' \
  -d '{\"query\": \"Jaka jest pogoda?\"}'"

# Should receive: {"text": "[Polish weather response]", "session_id": "...", "language": "pl"}

# Session persistence test
ssh homelab "curl -X POST http://192.168.0.150:8742/ask \
  -H 'Content-Type: application/json' \
  -d '{\"query\": \"A pojutrze?\", \"session_id\": \"[session_id_from_above]\"}'"

# Should receive follow-up response using context

# Permission test
ssh homelab "curl -X POST http://192.168.0.150:8742/ask \
  -H 'Content-Type: application/json' \
  -d '{\"query\": \"Wyłącz wszystko\"}'"

# Should receive: {"text": "...", "requires_permission": true, "action_description": "..."}
```

### Service Persistence Tests

```bash
# Kill process
pkill -f claude-ha-brain

# Wait 10s, check restarted
lsof -i :8742

# Reboot Mac, check auto-start after login
```

### Voice Integration Tests

**Test 1: Simple Query**
```
Voice: "Claude, jaka jest temperatura w salonie?"
Expected: Claude queries HA sensor, responds in Polish
Check: input_text.claude_session has UUID
```

**Test 2: Follow-up (Session Context)**
```
Voice: "A w sypialni?"
Expected: Claude knows "temperature" from previous query
Check: Same session ID as Test 1
```

**Test 3: Dangerous Action (Permission Required)**
```
Voice: "Claude, wyłącz wszystkie światła"
Expected: "Wyłączyć wszystkie światła? Powiedz Tak lub Nie"
Check: input_boolean.claude_awaiting_confirmation = on
```

**Test 4: Confirmation**
```
Voice: "Tak"
Expected: Lights turn off, "Wykonano"
Check: input_boolean.claude_awaiting_confirmation = off
```

**Test 5: Cancellation**
```
Voice: "Claude, wyłącz ogrzewanie"
Expected: "Wyłączyć ogrzewanie we wszystkich pomieszczeniach?"
Voice: "Nie"
Expected: "Anulowano akcję"
Check: Heating unchanged
```

**Test 6: Session Timeout**
```
Voice: "Claude, która godzina?"
Wait 6 minutes (session timeout)
Voice: "A jutro?"
Expected: Claude doesn't have context (new session)
```

## Architecture Decisions Made

1. **Session Management:** Using `--session-id` with 5 min timeout for multi-turn conversations
2. **Permission System:** Implemented with voice confirmation for dangerous actions
3. **HA State Awareness:** Claude queries HA via existing ha-mcp tools
4. **Response Storage:** Using shell_command + input_text helpers (simpler than REST sensor)
5. **Confirmation Flow:** input_boolean flag + separate Confirm/Cancel intents

## Unresolved Questions

1. **Mac IP stability** - Need to check if DHCP or static. If DHCP:
   - Option A: Configure router DHCP reservation
   - Option B: Set static IP in Mac network settings
   - Option C: Use mDNS (mac-mini.local) - may have latency issues

2. **HA Token in shell_command** - Where to store HA long-lived access token?
   - Option A: sops-encrypted secret
   - Option B: Environment variable in HA service
   - Option C: Hardcode in shell_command (less secure)
   - **Recommended:** Option A (sops-nix secret)

3. **Session cleanup on HA restart** - Sessions stored in Mac server RAM, lost on restart. Acceptable?
   - Could add persistence via file/Redis if needed
   - For now: acceptable, sessions timeout after 5 min anyway

4. **Dangerous action detection** - Should HA also validate dangerous actions client-side?
   - Currently: only Claude server detects via regex
   - Could add HA-side validation in automation condition
   - **Recommended:** Trust Claude for now, add HA validation if false positives

5. **Multi-user sessions** - How to handle multiple users? Currently single session per household.
   - Could add user detection via voice recognition
   - Or: session per room/device
   - For now: single shared session acceptable

## Future Enhancements (Post-MVP)

**After 1-2 weeks of stable operation, consider:**

1. **Memory persistence** - Add MCP memory server for long-term context
   - Remember preferences ("I like 22°C")
   - Learn routines ("Usually watch TV at 8pm")

2. **Todoist integration** - Task management via voice
   - "Dodaj do Todoist: kupić mleko"
   - "Co mam dzisiaj do zrobienia?"

3. **Calendar integration** - Schedule awareness
   - "Jakie mam spotkania dzisiaj?"
   - "Ustaw przypomnienie na 15:00"

4. **Proactive suggestions** - Claude monitors and suggests
   - "Temperatura wysoka, otworzyć okna?"
   - "Wychodzisz? Wyłączyć światła?"

5. **Multi-user sessions** - Per-user context via voice recognition
   - Separate session IDs for different household members
   - Personalized responses

6. **Authentication** - Bearer token for Mac server security
   - Prevent unauthorized LAN access
   - Rate limiting per client

## Timeline

- **Day 1:** Phase 1-2 (Mac server + launchd) - 4h
- **Day 2:** Phase 3-4 (HA integration + testing) - 4h
- **Day 3:** Voice integration + refinement - 3h
- **Total:** ~11h over 3 days

## References

- [Claude Code Headless Mode](https://code.claude.com/docs/en/headless)
- [Model Context Protocol](https://modelcontextprotocol.io/)
- [HA REST Command](https://www.home-assistant.io/integrations/rest_command/)
- [HA Shell Command](https://www.home-assistant.io/integrations/shell_command/)
- [launchd Tutorial](https://www.launchd.info/)
