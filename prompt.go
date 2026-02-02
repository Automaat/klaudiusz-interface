package main

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"

	"github.com/Automaat/klaudiusz-interface/memory"
)

func parsePermissionRequest(response string) (*PendingAction, bool) {
	if !strings.Contains(response, "PERMISSION_REQUIRED:") {
		return nil, false
	}

	re := regexp.MustCompile(`PERMISSION_REQUIRED: (.+?) \| COMMANDS: (.+)`)

	matches := re.FindStringSubmatch(response)

	if len(matches) != PermissionRegexSubmatchCount {
		return nil, false
	}

	description := strings.TrimSpace(matches[1])
	commandsStr := matches[2]

	commandsSplit := strings.Split(commandsStr, ",")

	commands := make([]string, 0, len(commandsSplit))
	for _, cmd := range commandsSplit {
		if trimmed := strings.TrimSpace(cmd); trimmed != "" {
			commands = append(commands, trimmed)
		}
	}

	return &PendingAction{
		ID:          uuid.New().String(),
		Description: description,
		Commands:    commands,
	}, true
}

func buildSystemPrompt(query string) string {
	return buildSystemPromptWithMemory(query, nil, nil)
}

// buildSystemPromptWithMemory creates context prompt with dynamic sections.
// Core personality/language/format instructions come from CLAUDE.md in working directory.
func buildSystemPromptWithMemory(query string, facts []memory.Fact, userCtx *UserContext) string {
	var parts []string

	// Add memory facts if available
	if len(facts) > 0 {
		var sb strings.Builder

		sb.WriteString("PAMIĘĆ (istotne fakty z przeszłych rozmów):")

		for _, fact := range facts {
			sb.WriteString(fmt.Sprintf("\n- [%s] %s", fact.Category, fact.Text))
		}

		parts = append(parts, sb.String())
	}

	// Add user context for group chats
	if userCtx != nil && userCtx.IsGroupChat() {
		mode := "prywatna rozmowa w grupie"
		if userCtx.GroupMode == "shared" {
			mode = "wspólna konwersacja grupowa"
		}

		userSection := fmt.Sprintf(
			"UŻYTKOWNIK:\n- Rozmawia: %s\n- Tryb: %s",
			userCtx.DisplayName(),
			mode,
		)

		parts = append(parts, userSection)
	}

	// Add current query with emoji and HTML formatting instruction for Telegram
	responseInstr := "Odpowiedź (po polsku, zwięźle"
	if userCtx != nil && userCtx.IsTelegram {
		responseInstr += ", używaj emoji i formatowania HTML: <b>pogrubienie</b>, <i>kursywa</i>"
	}

	responseInstr += "):"

	parts = append(parts, fmt.Sprintf("Pytanie: %s\n\n%s", query, responseInstr))

	return strings.Join(parts, "\n\n")
}
