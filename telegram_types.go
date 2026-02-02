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
	// CallbackDataAlwaysPrefix format: "always:<session_id>:<action_id>"
	CallbackDataAlwaysPrefix = "always:"
	// CallbackDataConfirmParts is number of parts in confirm callback data
	CallbackDataConfirmParts = 3
	// CallbackDataCancelParts is number of parts in cancel callback data
	CallbackDataCancelParts = 2
	// CallbackDataAlwaysParts is number of parts in always callback data
	CallbackDataAlwaysParts = 3
	// DefaultConfirmationMessage is the default permission request message
	DefaultConfirmationMessage = "Potwierdź wykonanie tej akcji"
	// ChatTypePrivate represents private chat type
	ChatTypePrivate = "private"
	// GroupModeShared represents shared group session mode
	GroupModeShared = "shared"
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

// userChatIDToSessionID creates per-user session ID for groups
func userChatIDToSessionID(chatID int64, userID int64) string {
	identifier := fmt.Sprintf("telegram-chat-%d-user-%d", chatID, userID)
	return uuid.NewSHA1(telegramNamespace, []byte(identifier)).String()
}

// sessionIDFromContext generates session ID based on chat type and config
func sessionIDFromContext(
	chatID int64,
	userID int64,
	chatType string,
	groupSessionMode string,
) string {
	if chatType == ChatTypePrivate {
		return chatIDToSessionID(chatID)
	}

	if groupSessionMode == GroupModeShared {
		return chatIDToSessionID(chatID)
	}

	return userChatIDToSessionID(chatID, userID)
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
