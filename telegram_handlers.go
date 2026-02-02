package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

func (*Server) sendPermissionRequest(
	ctx context.Context,
	b TelegramBot,
	chatID int64,
	sessionID string,
	action *PendingAction,
) {
	// Build confirmation message with guard
	confirmMsg := strings.TrimSpace(action.Description)
	if confirmMsg == "" {
		confirmMsg = DefaultConfirmationMessage
	} else {
		lastChar := confirmMsg[len(confirmMsg)-1]
		if lastChar != '.' && lastChar != '!' && lastChar != '?' {
			confirmMsg += "."
		}
	}

	// Build inline keyboard
	keyboard := &models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{
			{
				{
					Text: "✅ Tak",
					CallbackData: fmt.Sprintf(
						"%s%s:%s",
						CallbackDataConfirmPrefix,
						sessionID,
						action.ID,
					),
				},
				{
					Text: "❌ Nie",
					CallbackData: fmt.Sprintf(
						"%s%s",
						CallbackDataCancelPrefix,
						sessionID,
					),
				},
			},
		},
	}

	if _, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:      chatID,
		Text:        confirmMsg,
		ReplyMarkup: keyboard,
	}); err != nil {
		log.Printf("Failed to send permission request to chat_id=%d: %v", chatID, err)
	}
}

func (*Server) sendTelegramResponse(
	ctx context.Context,
	b TelegramBot,
	chatID int64,
	text string,
) {
	if _, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: chatID,
		Text:   text,
	}); err != nil {
		log.Printf("Failed to send message to chat_id=%d: %v", chatID, err)
	}
}

// answerCallbackQuery answers callback query and logs errors
func answerCallbackQuery(ctx context.Context, b TelegramBot, callbackID string) {
	if _, err := b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
		CallbackQueryID: callbackID,
	}); err != nil {
		log.Printf("Failed to answer callback query: %v", err)
	}
}

// handleCallbackError sends error message and answers callback
func (s *Server) handleCallbackError(
	ctx context.Context,
	b TelegramBot,
	chatID int64,
	callbackID string,
	message string,
) {
	s.sendTelegramResponse(ctx, b, chatID, message)
	answerCallbackQuery(ctx, b, callbackID)
}

func (s *Server) handleTelegramCallback(ctx context.Context, b *bot.Bot, update *models.Update) {
	s.handleTelegramCallbackInternal(ctx, wrapBot(b), update)
}

func (s *Server) handleTelegramCallbackInternal(
	ctx context.Context,
	b TelegramBot,
	update *models.Update,
) {
	if update.CallbackQuery == nil {
		return
	}

	callbackData := update.CallbackQuery.Data
	callbackID := update.CallbackQuery.ID
	chatID := update.CallbackQuery.Message.Message.Chat.ID
	chatType := string(update.CallbackQuery.Message.Message.Chat.Type)

	var userID int64
	if update.CallbackQuery.From.ID != 0 {
		userID = update.CallbackQuery.From.ID
	} else {
		userID = chatID
	}

	// Parse callback data: "confirm:<session_id>:<action_id>"
	parts := strings.SplitN(callbackData, ":", CallbackDataConfirmParts)
	if len(parts) != CallbackDataConfirmParts {
		log.Printf("Invalid callback data format: %s", callbackData)
		s.handleCallbackError(ctx, b, chatID, callbackID, "Nieprawidłowe żądanie")

		return
	}

	sessionID := parts[1]
	actionID := parts[2]

	// Validate session matches chat and user
	expectedSessionID := sessionIDFromContext(chatID, userID, chatType)
	if sessionID != expectedSessionID {
		log.Printf(
			"Session mismatch: callback=%s, expected=%s (user=%d, chat=%d)",
			sessionID,
			expectedSessionID,
			userID,
			chatID,
		)
		s.handleCallbackError(ctx, b, chatID, callbackID, "Nieprawidłowa sesja")

		return
	}

	// Load session
	val, ok := s.sessions.Load(sessionID)
	if !ok {
		log.Printf("Session not found: %s", sessionID)
		s.handleCallbackError(ctx, b, chatID, callbackID, "Sesja wygasła")

		return
	}

	session, ok := val.(*Session)
	if !ok {
		log.Printf("Failed to cast session: %s", sessionID)
		s.handleCallbackError(ctx, b, chatID, callbackID, "Błąd wewnętrzny")

		return
	}

	// Execute confirmed action (Telegram passes actionID for validation)
	response, err := s.executeConfirmedAction(ctx, session, actionID)
	if err != nil {
		log.Printf("Failed to execute action for session %s: %v", sessionID, err)
		s.handleCallbackError(ctx, b, chatID, callbackID, formatTelegramError(err))

		return
	}

	// Answer callback query to remove loading state
	answerCallbackQuery(ctx, b, callbackID)

	// Send success response
	s.sendTelegramResponse(ctx, b, chatID, response)
}

func (s *Server) handleTelegramCancel(ctx context.Context, b *bot.Bot, update *models.Update) {
	s.handleTelegramCancelInternal(ctx, wrapBot(b), update)
}

func (s *Server) handleTelegramCancelInternal(
	ctx context.Context,
	b TelegramBot,
	update *models.Update,
) {
	if update.CallbackQuery == nil {
		return
	}

	callbackData := update.CallbackQuery.Data
	chatID := update.CallbackQuery.Message.Message.Chat.ID
	chatType := string(update.CallbackQuery.Message.Message.Chat.Type)

	var userID int64
	if update.CallbackQuery.From.ID != 0 {
		userID = update.CallbackQuery.From.ID
	} else {
		userID = chatID
	}

	// Answer callback early (before any returns)
	defer func() {
		if _, err := b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
			CallbackQueryID: update.CallbackQuery.ID,
		}); err != nil {
			log.Printf("Failed to answer cancel callback: %v", err)
		}
	}()

	// Parse callback data: "cancel:<session_id>"
	parts := strings.SplitN(callbackData, ":", CallbackDataCancelParts)
	if len(parts) != CallbackDataCancelParts {
		log.Printf("Invalid cancel callback data: %s", callbackData)
		return
	}

	sessionID := parts[1]

	// Validate session matches chat and user
	expectedSessionID := sessionIDFromContext(chatID, userID, chatType)
	if sessionID != expectedSessionID {
		log.Printf(
			"Cancel session mismatch: callback=%s, expected=%s (user=%d, chat=%d)",
			sessionID,
			expectedSessionID,
			userID,
			chatID,
		)

		return
	}

	// Load and clear pending action
	val, ok := s.sessions.Load(sessionID)
	if ok {
		session, ok := val.(*Session)
		if ok {
			session.mu.Lock()
			session.PendingAction = nil
			session.mu.Unlock()
		}
	}

	s.sendTelegramResponse(ctx, b, chatID, "Anulowano akcję")
}
