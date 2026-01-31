package main

import (
	"fmt"
	"strings"
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
)

// chatIDToSessionID converts Telegram chat ID to session ID with tg- prefix
func chatIDToSessionID(chatID int64) string {
	return fmt.Sprintf("tg-%d", chatID)
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
