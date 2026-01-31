package main

import (
	"context"
	"log"
	"os"
	"os/signal"

	"github.com/cockroachdb/errors"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

func initTelegramBot(s *Server) (*bot.Bot, context.CancelFunc, error) {
	if TelegramBotToken == "" {
		return nil, nil, errors.New("TELEGRAM_BOT_TOKEN not set")
	}

	opts := []bot.Option{
		bot.WithDefaultHandler(s.handleTelegramMessage),
		bot.WithCallbackQueryDataHandler("confirm:", bot.MatchTypePrefix, s.handleTelegramCallback),
		bot.WithCallbackQueryDataHandler("cancel:", bot.MatchTypePrefix, s.handleTelegramCancel),
	}

	b, err := bot.New(TelegramBotToken, opts...)
	if err != nil {
		return nil, nil, errors.Wrap(err, "failed to create bot")
	}

	// Create cancellable context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())

	// Start polling in background
	go func() {
		log.Printf("Telegram bot polling started")
		b.Start(ctx)
	}()

	// Handle graceful shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt)
		<-sigCh
		log.Printf("Stopping Telegram bot")
		cancel()
	}()

	return b, cancel, nil
}

func (s *Server) handleTelegramMessage(ctx context.Context, b *bot.Bot, update *models.Update) {
	s.handleTelegramMessageInternal(ctx, wrapBot(b), update)
}

func (s *Server) handleTelegramMessageInternal(ctx context.Context, b TelegramBot, update *models.Update) {
	if update.Message == nil || update.Message.Text == "" {
		return
	}

	chatID := update.Message.Chat.ID
	text := update.Message.Text
	sessionID := chatIDToSessionID(chatID)

	log.Printf("Telegram message from chat_id=%d, session_id=%s: %s", chatID, sessionID, text)

	session := s.getOrCreateSession(sessionID)

	// Build system prompt
	systemPrompt := buildSystemPrompt(text)

	// Execute Claude with timeout
	execCtx, cancel := context.WithTimeout(ctx, ClaudeExecutionTimeout)
	defer cancel()

	response, err := executeClaude(execCtx, systemPrompt)
	if err != nil {
		log.Printf("Claude error for chat_id=%d: %v", chatID, err)
		s.sendTelegramResponse(ctx, b, chatID, formatTelegramError(err))

		return
	}

	// Check if permission required
	if action, needsPermission := parsePermissionRequest(response); needsPermission {
		session.mu.Lock()
		session.PendingAction = action
		session.mu.Unlock()

		s.sendPermissionRequest(ctx, b, chatID, sessionID, action)

		return
	}

	// Normal response
	if isDangerousAction(text) {
		log.Printf("WARNING: Dangerous query not flagged for chat_id=%d: %s", chatID, text)
	}

	s.sendTelegramResponse(ctx, b, chatID, response)
}
