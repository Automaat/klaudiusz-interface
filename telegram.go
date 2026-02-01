package main

import (
	"context"
	"fmt"
	"log"

	"github.com/cockroachdb/errors"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

func initTelegramBot(s *Server) (context.CancelFunc, error) {
	if TelegramBotToken == "" {
		return nil, errors.New("TELEGRAM_BOT_TOKEN not set")
	}

	opts := []bot.Option{
		bot.WithDefaultHandler(s.handleTelegramMessage),
		bot.WithCallbackQueryDataHandler("confirm:", bot.MatchTypePrefix, s.handleTelegramCallback),
		bot.WithCallbackQueryDataHandler("cancel:", bot.MatchTypePrefix, s.handleTelegramCancel),
	}

	b, err := bot.New(TelegramBotToken, opts...)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create bot")
	}

	// Create cancellable context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())

	// Start polling in background
	go func() {
		log.Printf("Telegram bot polling started")
		b.Start(ctx)
		log.Printf("Telegram bot stopped")
	}()

	return cancel, nil
}

func (s *Server) handleTelegramMessage(ctx context.Context, b *bot.Bot, update *models.Update) {
	s.handleTelegramMessageInternal(ctx, wrapBot(b), update)
}

func (s *Server) handleTelegramMessageInternal(
	ctx context.Context,
	b TelegramBot,
	update *models.Update,
) {
	if update.Message == nil {
		return
	}

	chatID := update.Message.Chat.ID
	sessionID := chatIDToSessionID(chatID)

	var text string

	// Handle voice messages if enabled
	switch {
	case update.Message.Voice != nil && VoiceEnabled:
		transcribed, err := s.handleVoiceMessage(ctx, b, update.Message.Voice.FileID)
		if err != nil {
			s.sendTelegramResponse(ctx, b, chatID, formatDeepgramError(err))
			return
		}

		text = transcribed
	case update.Message.Photo != nil && PhotoEnabled:
		// Select largest photo
		if len(update.Message.Photo) == 0 {
			return
		}

		largest := selectLargestPhoto(update.Message.Photo)
		if largest == nil {
			return
		}

		// Download photo
		imagePath, cleanup, err := downloadPhotoMessage(ctx, b, largest.FileID)
		if err != nil {
			s.sendTelegramResponse(ctx, b, chatID, formatPhotoError(err))
			return
		}
		defer cleanup()

		// Build prompt with caption
		caption := update.Message.Caption
		if caption != "" {
			text = fmt.Sprintf("%s\n\nObraz: %s", caption, imagePath)
		} else {
			text = "Opisz ten obraz: " + imagePath
		}
	case update.Message.Text != "":
		text = update.Message.Text
	default:
		return
	}

	log.Printf("Telegram message from chat_id=%d, session_id=%s: %s", chatID, sessionID, text)

	session := s.getOrCreateSession(sessionID)

	// Build system prompt
	systemPrompt := buildSystemPrompt(text)

	// Execute Claude with timeout
	execCtx, cancel := context.WithTimeout(ctx, ClaudeExecutionTimeout)
	defer cancel()

	response, err := executeClaude(execCtx, systemPrompt, session)
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

func (s *Server) handleVoiceMessage(
	ctx context.Context,
	b TelegramBot,
	fileID string,
) (string, error) {
	if s.deepgramClient == nil {
		return "", errors.New("deepgram client not initialized")
	}

	// Download voice file
	filePath, cleanup, err := downloadVoiceMessage(ctx, b, fileID)
	if err != nil {
		return "", errors.Wrap(err, "voice download failed")
	}
	defer cleanup()

	// Transcribe
	transcript, err := transcribeAudioFile(ctx, s.deepgramClient, filePath)
	if err != nil {
		return "", errors.Wrap(err, "transcription failed")
	}

	return transcript, nil
}
