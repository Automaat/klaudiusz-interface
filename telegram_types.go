package main

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
)

const (
	// CallbackDataConfirmPrefix format: "confirm:<session_id>:<action_id>"
	CallbackDataConfirmPrefix = "confirm:"
	// CallbackDataCancelPrefix format: "cancel:<session_id>"
	CallbackDataCancelPrefix = "cancel:"
	// CallbackDataConfirmParts is number of parts in confirm callback data
	CallbackDataConfirmParts = 3
	// CallbackDataCancelParts is number of parts in cancel callback data
	CallbackDataCancelParts = 2
	// DefaultConfirmationMessage is the default permission request message
	DefaultConfirmationMessage = "Potwierdź wykonanie tej akcji"
)

// telegramNamespace is UUID namespace for Telegram chat IDs
// Generated deterministically from DNS namespace and "telegram.org" label.
var telegramNamespace = uuid.NewSHA1(uuid.NameSpaceDNS, []byte("telegram.org"))

// chatIDToSessionID converts Telegram chat ID to deterministic UUID
func chatIDToSessionID(chatID int64) string {
	// Create deterministic UUID v5 from chat ID
	chatIDStr := fmt.Sprintf("telegram-chat-%d", chatID)
	return uuid.NewSHA1(telegramNamespace, []byte(chatIDStr)).String()
}

// formatTelegramError converts error to Polish TTS-friendly message
func formatTelegramError(err error) string {
	if err == nil {
		return "Nieznany błąd"
	}

	errMsg := err.Error()

	// Convert common technical errors to Polish
	if strings.Contains(errMsg, "timeout") ||
		strings.Contains(errMsg, "context deadline exceeded") {
		return "Przekroczono czas oczekiwania"
	}

	if strings.Contains(errMsg, "no pending action") {
		return "Nie ma oczekującej akcji"
	}

	if strings.Contains(errMsg, "invalid command") {
		return "Nieprawidłowe polecenie"
	}

	return "Przepraszam, wystąpił błąd"
}
