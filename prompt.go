package main

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/Automaat/klaudiusz-interface/memory"
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
	return buildSystemPromptWithMemory(query, nil)
}

func buildSystemPromptWithMemory(query string, facts []memory.Fact) string {
	var sb strings.Builder

	sb.WriteString("JĘZYK: Odpowiadaj TYLKO po polsku.\n")
	sb.WriteString("FORMAT: Zwięzłe odpowiedzi dla głosowego wyjścia (max 2-3 zdania).\n")
	sb.WriteString("KONTEKST: Jesteś polskim asystentem domowym Klaudiusz.\n\n")

	sb.WriteString("NARZĘDZIA:\n")
	sb.WriteString("- Masz dostęp do Home Assistant przez ha-mcp\n")
	sb.WriteString("- Możesz sprawdzać temperaturę, światła, sensory\n")
	sb.WriteString("- Możesz kontrolować urządzenia\n\n")

	sb.WriteString("BEZPIECZEŃSTWO:\n")
	sb.WriteString("- Dla niebezpiecznych akcji użyj: \"PERMISSION_REQUIRED: [opis] | COMMANDS: [lista]\"\n")
	sb.WriteString("- Przykład: \"PERMISSION_REQUIRED: Wyłączyć wszystkie światła | COMMANDS: light.turn_off_all\"\n\n")

	// Inject memory facts
	if len(facts) > 0 {
		sb.WriteString("PAMIĘĆ (informacje o użytkowniku):\n")
		for _, fact := range facts {
			sb.WriteString(fmt.Sprintf("- [%s] %s\n", fact.Category, fact.Text))
		}
		sb.WriteString("\n")
	}

	sb.WriteString(fmt.Sprintf("Pytanie: %s\n\n", query))
	sb.WriteString("Odpowiedź (po polsku, zwięźle):")

	return sb.String()
}
