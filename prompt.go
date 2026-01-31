package main

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"
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
