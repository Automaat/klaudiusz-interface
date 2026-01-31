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
	b *bot.Bot,
	chatID int64,
	sessionID string,
	action *PendingAction,
) {
	// Build confirmation message
	confirmMsg := action.Description

	lastChar := confirmMsg[len(confirmMsg)-1]
	if lastChar != '.' && lastChar != '!' && lastChar != '?' {
		confirmMsg += "."
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
	b *bot.Bot,
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

func (s *Server) handleTelegramCallback(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update.CallbackQuery == nil {
		return
	}

	callbackData := update.CallbackQuery.Data
	chatID := update.CallbackQuery.Message.Message.Chat.ID

	// Parse callback data: "confirm:<session_id>:<action_id>"
	parts := strings.SplitN(callbackData, ":", CallbackDataConfirmParts)
	if len(parts) != CallbackDataConfirmParts {
		log.Printf("Invalid callback data format: %s", callbackData)
		s.sendTelegramResponse(ctx, b, chatID, "Nieprawidłowe żądanie")

		return
	}

	sessionID := parts[1]
	actionID := parts[2]

	// Load session
	val, ok := s.sessions.Load(sessionID)
	if !ok {
		log.Printf("Session not found: %s", sessionID)
		s.sendTelegramResponse(ctx, b, chatID, "Sesja wygasła")

		return
	}

	session, ok := val.(*Session)
	if !ok {
		log.Printf("Failed to cast session: %s", sessionID)
		s.sendTelegramResponse(ctx, b, chatID, "Błąd wewnętrzny")

		return
	}

	// Execute confirmed action
	response, err := s.executeConfirmedAction(ctx, session, actionID)
	if err != nil {
		log.Printf("Failed to execute action for session %s: %v", sessionID, err)
		s.sendTelegramResponse(ctx, b, chatID, formatTelegramError(err))

		return
	}

	// Answer callback query to remove loading state
	if _, err := b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
		CallbackQueryID: update.CallbackQuery.ID,
	}); err != nil {
		log.Printf("Failed to answer callback query: %v", err)
	}

	// Send success response
	s.sendTelegramResponse(ctx, b, chatID, response)
}

func (s *Server) handleTelegramCancel(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update.CallbackQuery == nil {
		return
	}

	callbackData := update.CallbackQuery.Data
	chatID := update.CallbackQuery.Message.Message.Chat.ID

	// Parse callback data: "cancel:<session_id>"
	parts := strings.SplitN(callbackData, ":", CallbackDataCancelParts)
	if len(parts) != CallbackDataCancelParts {
		log.Printf("Invalid cancel callback data: %s", callbackData)
		return
	}

	sessionID := parts[1]

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

	// Answer callback query
	if _, err := b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
		CallbackQueryID: update.CallbackQuery.ID,
	}); err != nil {
		log.Printf("Failed to answer cancel callback: %v", err)
	}

	s.sendTelegramResponse(ctx, b, chatID, "Anulowano akcję")
}
