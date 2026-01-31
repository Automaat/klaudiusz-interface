# klaudiusz-interface

Go HTTP server wrapping Claude Code CLI for Home Assistant voice integration.

## Project Structure

```
.
├── *.go           - Main source files (handlers, server, session, config, types)
├── main_test.go   - Comprehensive test suite (82.4% coverage)
├── testdata/      - Test fixtures and helpers
├── .mise.toml     - Tool management and task definitions
├── .golangci.yml  - Extensive linter configuration
├── go.mod/sum     - Dependency management
├── brain-plan.md  - Full implementation roadmap
└── .github/workflows/ - CI/CD (lint, test, codecov)
```

## Tech Stack

**Language:** Go 1.25.6
**Router:** Chi v5.2.4 (minimal HTTP routing)
**Error Handling:** cockroachdb/errors (structured error wrapping)
**Testing:** Go stdlib with race detector
**Linting:** golangci-lint 2.8.0 (80+ enabled rules)
**Dependency Mgmt:** mise
**CI/CD:** GitHub Actions with codecov (80% target)

## Architecture

```
Voice → Whisper STT → HA Conversation → HTTP (port 8742) → Claude Code CLI → Response → Piper TTS
```

**Key Components:**
- Session manager (5min timeout, auto-cleanup)
- Permission system (dangerous action verification)
- Health check endpoint
- launchd service (Mac background daemon)

## Development Workflow

### Adding New Endpoint

1. Define handler in `handlers.go`
2. Add route in `server.go` via Chi router
3. Update `types.go` if new request/response structs needed
4. Write comprehensive tests in `main_test.go`
5. Run `mise run check` (fmt, lint, test)
6. Verify coverage stays ≥80%

### Modifying Session Logic

1. Read `session.go` to understand timeout/cleanup
2. Update session manager logic
3. Add/update tests for timeout scenarios
4. Test race conditions with `-race` flag
5. Verify no goroutine leaks

### Testing Changes

```bash
# Run tests with race detector and coverage
mise run test

# View coverage report
go tool cover -html=coverage.out

# Run specific test
go test -v -run TestHandleAsk

# Run linter
mise run lint
```

## Home Assistant Integration

### Testing HA REST Commands

**Setup:**
1. Start server locally: `mise run run`
2. Server listens on `localhost:8742`

**Manual test via curl:**
```bash
# Health check
curl http://localhost:8742/health

# Ask endpoint
curl -X POST http://localhost:8742/ask \
  -H "Content-Type: application/json" \
  -d '{"prompt": "what time is it?", "session_id": "test-session"}'

# Cancel endpoint
curl -X POST http://localhost:8742/cancel \
  -H "Content-Type: application/json" \
  -d '{"session_id": "test-session"}'
```

**Expected responses:**
- Health: `{"status": "ok"}`
- Ask: `{"response": "...", "session_id": "test-session"}`
- Cancel: `{"status": "cancelled"}`

**HA Integration (after deployment):**
1. Update `hosts/homelab/home-assistant/claude-brain.nix` with Mac IP
2. Test via HA Developer Tools > Services > `rest_command.ask_claude`
3. Test voice flow: "Hey Jarvis, ask Claude what time is it"
4. Check HA logs: `journalctl -u home-assistant -f`
5. Check server logs: `log stream --predicate 'subsystem == "com.mskalski.claude-ha-brain"'`

**Debugging Whisper → Piper TTS Flow:**
- Whisper logs: Check HA Whisper addon logs
- Intent routing: HA Developer Tools > States > search for conversation entity
- Claude server: `log show --predicate 'subsystem == "com.mskalski.claude-ha-brain"' --last 1h`
- Piper TTS: Check HA Piper addon logs, validate Polish language pack

**Common issues:**
- Network timeout: Check firewall, verify Mac IP reachable from HA
- Session not found: Check 5min timeout hasn't expired
- Polish TTS sounds wrong: Remove code symbols/brackets from response text

## Quality Gates

Before committing:

- [ ] `mise run fmt` - Format code with gofmt/gofumpt/goimports
- [ ] `mise run lint` - golangci-lint passes (all 80+ rules)
- [ ] `mise run test` - Tests pass with race detector
- [ ] Coverage ≥80% (enforced by codecov)
- [ ] Session timeout/cleanup tested
- [ ] HA voice response text TTS-friendly (no symbols/brackets)

**Commands:**
```bash
# Run all checks at once
mise run check

# Individual steps
mise run fmt
mise run lint
mise run test

# View coverage
go tool cover -html=coverage.out
```

## Common Commands

```bash
# Build binary
mise run build

# Run server locally (port 8742)
mise run run

# Run tests with coverage
mise run test

# Run linter
mise run lint

# Format code
mise run fmt

# Clean artifacts
mise run clean

# All checks (fmt + lint + test)
mise run check
```

## Output Templates

### JSON API Response (Success)

```json
{
  "response": "The current time is 3:45 PM.",
  "session_id": "ha-session-12345"
}
```

**Fields:**
- `response` (string): Claude's response text (TTS-friendly, no symbols)
- `session_id` (string): Session identifier for follow-up questions

### JSON API Response (Error)

```json
{
  "error": "session not found"
}
```

**Common errors:**
- `"session not found"` - Session expired (>5min) or invalid ID
- `"permission denied"` - Dangerous action requires confirmation
- `"internal server error"` - Claude CLI hang/timeout/crash

### Structured Log Messages

**Session creation:**
```
[INFO] Created session session_id=ha-session-12345 timeout=5m
```

**Permission check:**
```
[WARN] Permission denied session_id=ha-session-12345 action=delete_file reason=requires_confirmation
```

**Claude CLI interaction:**
```
[DEBUG] Executing Claude CLI session_id=ha-session-12345 prompt="what time is it?"
[INFO] Claude response received session_id=ha-session-12345 duration=1.2s
```

**Session cleanup:**
```
[INFO] Session expired session_id=ha-session-12345 age=5m2s
```

### Error Response (Voice-Friendly)

**Good (TTS-friendly):**
- "Nie mogę wykonać tej operacji bez potwierdzenia" (I can't perform this operation without confirmation)
- "Sesja wygasła, zacznij od nowa" (Session expired, start over)
- "Wystąpił problem z połączeniem" (Connection problem occurred)

**Bad (breaks TTS):**
```
ERROR: session_id="xyz" not found (timeout=5m)
```

**Rule:** Response text goes directly to Piper Polish TTS - avoid:
- Code syntax (`session_id=`, brackets, quotes)
- English error codes
- Technical jargon
- Special characters that sound bad when spoken

## Anti-Patterns

**AVOID:**

- ❌ Using `fmt.Errorf` instead of `errors.New/Wrap/Wrapf`
  - **REASON:** forbidigo linter enforces cockroachdb/errors for structured error handling
  - **FIX:** Always use `errors.New()`, `errors.Wrap()`, or `errors.Wrapf()`

- ❌ Not testing session timeout/cleanup logic
  - **REASON:** 5min timeout critical for resource management, goroutine leaks cause memory issues
  - **FIX:** Add tests that verify sessions are cleaned up after timeout, no goroutine leaks with `-race`

- ❌ Including symbols/code/brackets in HA voice responses
  - **REASON:** Response text goes to Polish TTS (Piper) - technical syntax sounds terrible when spoken
  - **FIX:** Use plain Polish text, test by reading response aloud, avoid English/code

- ❌ Forgetting to reload launchd after code changes
  - **REASON:** Server runs as background service - old binary keeps running until reloaded
  - **FIX:** After deploying: `launchctl unload ~/Library/LaunchAgents/com.mskalski.claude-ha-brain.plist && launchctl load ~/Library/LaunchAgents/com.mskalski.claude-ha-brain.plist`

- ❌ Skipping race detector in tests
  - **REASON:** Session manager uses goroutines, race conditions can cause crashes
  - **FIX:** Always run `go test -race`, CI enforces this

- ❌ Ignoring golangci-lint warnings
  - **REASON:** 80+ enabled rules catch bugs, security issues, bad practices
  - **FIX:** Fix linter errors properly (research solution if unclear), never use `//nolint` directives

## Permission Validation

Decision matrix for dangerous actions requiring voice confirmation:

| Action Type | Risk Level | Requires Confirmation | Example |
|-------------|------------|----------------------|---------|
| File deletion | HIGH | Yes | `rm -rf`, delete files |
| System commands | HIGH | Yes | `sudo`, `shutdown`, `reboot` |
| Network changes | MEDIUM | Yes | Firewall rules, port forwarding |
| File reads | LOW | No | `cat`, `ls`, read logs |
| Device queries | LOW | No | HA state queries, sensor data |

**Implementation:** `safety.go` contains validation logic.

**Workflow:**
1. Parse prompt for dangerous keywords
2. If detected → return permission denied error
3. User must explicitly confirm via voice
4. Retry with confirmation flag set

## Few-Shot Testing Examples

### Session Timeout Test

```go
func TestSessionTimeout(t *testing.T) {
    sm := NewSessionManager(100 * time.Millisecond) // Short timeout for testing

    // Create session
    id := sm.CreateSession()

    // Verify session exists
    if !sm.SessionExists(id) {
        t.Fatal("session should exist")
    }

    // Wait for timeout
    time.Sleep(150 * time.Millisecond)

    // Verify cleanup
    if sm.SessionExists(id) {
        t.Fatal("session should be cleaned up")
    }
}
```

### HA Integration Test Fixture

```go
// testdata/ha_request.json
{
  "prompt": "jaka jest temperatura w salonie?",
  "session_id": "ha-test-session"
}

// Test
func TestHandleAsk_HAIntegration(t *testing.T) {
    body, _ := os.ReadFile("testdata/ha_request.json")
    req := httptest.NewRequest("POST", "/ask", bytes.NewReader(body))
    req.Header.Set("Content-Type", "application/json")

    rec := httptest.NewRecorder()
    handler.ServeHTTP(rec, req)

    // Verify TTS-friendly response
    var resp Response
    json.Unmarshal(rec.Body.Bytes(), &resp)

    // Should not contain technical symbols
    if strings.Contains(resp.Response, "{") || strings.Contains(resp.Response, "[") {
        t.Error("response not TTS-friendly")
    }
}
```

### Permission Denial Test

```go
func TestDangerousAction_Denied(t *testing.T) {
    prompts := []string{
        "delete all my files",
        "sudo shutdown now",
        "rm -rf /",
    }

    for _, prompt := range prompts {
        req := Request{Prompt: prompt}

        if !RequiresPermission(req) {
            t.Errorf("should deny: %s", prompt)
        }
    }
}
```

## launchd Deployment

**Service file:** `~/Library/LaunchAgents/com.mskalski.claude-ha-brain.plist`

**After code changes:**
```bash
# Build new binary
mise run build

# Copy to deployment location (if not already there)
cp klaudiusz-interface ~/bin/

# Reload service
launchctl unload ~/Library/LaunchAgents/com.mskalski.claude-ha-brain.plist
launchctl load ~/Library/LaunchAgents/com.mskalski.claude-ha-brain.plist

# Verify running
launchctl list | grep claude-ha-brain

# Check logs
log stream --predicate 'subsystem == "com.mskalski.claude-ha-brain"'
```

**Check service status:**
```bash
# List service
launchctl list com.mskalski.claude-ha-brain

# View recent logs
log show --predicate 'subsystem == "com.mskalski.claude-ha-brain"' --last 1h

# Monitor live logs
log stream --predicate 'subsystem == "com.mskalski.claude-ha-brain"'
```

**Common issues:**
- Service not starting: Check plist syntax, verify binary path
- Port 8742 in use: `lsof -i :8742`, kill conflicting process
- Logs not appearing: Verify subsystem name matches plist

## Extensibility

To add sections as project evolves:

1. Add heading in appropriate location (e.g., ## New Feature)
2. Follow section structure: overview → commands → examples
3. Keep concrete and actionable (no placeholders/TODOs)
4. Include examples where helpful (test fixtures, API calls, logs)

**Suggested additions when needed:**
- Authentication/authorization section when HA auth implemented
- Rate limiting section if abuse becomes issue
- Metrics/observability section if Prometheus added
- Multi-language support section if non-Polish TTS needed

See `.claude/skills/claude-md-gen/customization-guide.md` for:
- Adding new project types
- Creating custom patterns
- Updating as requirements change
