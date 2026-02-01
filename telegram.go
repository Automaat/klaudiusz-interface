package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/Automaat/klaudiusz-interface/memory"
)

const memoryFactLimit = 10

// extractMessageText extracts text from various message types
func (s *Server) extractMessageText(
	ctx context.Context,
	b TelegramBot,
	msg *models.Message,
	chatID int64,
) (string, bool) {
	switch {
	case msg.Voice != nil && VoiceEnabled:
		transcribed, err := s.handleVoiceMessage(ctx, b, msg.Voice.FileID)
		if err != nil {
			s.sendTelegramResponse(ctx, b, chatID, formatDeepgramError(err))
			return "", false
		}

		return transcribed, true
	case msg.Photo != nil && PhotoEnabled:
		if len(msg.Photo) == 0 {
			return "", false
		}

		largest := selectLargestPhoto(msg.Photo)
		if largest == nil {
			return "", false
		}

		imagePath, cleanup, err := downloadPhotoMessage(ctx, b, largest.FileID)
		if err != nil {
			s.sendTelegramResponse(ctx, b, chatID, formatPhotoError(err))
			return "", false
		}
		defer cleanup()

		caption := msg.Caption
		if caption != "" {
			return fmt.Sprintf("%s\n\nObraz: %s", caption, imagePath), true
		}

		return "Opisz ten obraz: " + imagePath, true
	case msg.Text != "":
		return msg.Text, true
	default:
		return "", false
	}
}

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

	text, ok := s.extractMessageText(ctx, b, update.Message, chatID)
	if !ok {
		return
	}

	log.Printf("Telegram message from chat_id=%d, session_id=%s: %s", chatID, sessionID, text)

	session := s.getOrCreateSession(sessionID)

	// Recall relevant context from memory
	var memoryCtx memory.Context

	if s.memory != nil {
		var err error

		memoryCtx, err = s.memory.Recall(ctx, text, memory.RecallOptions{
			IncludeFacts: true,
			FactLimit:    memoryFactLimit,
		})
		if err != nil {
			log.Printf("Memory recall error for chat_id=%d: %v", chatID, err)
			// Continue without memory (graceful degradation)
		}
	}

	// Build system prompt with memory context
	systemPrompt := buildSystemPromptWithMemory(text, memoryCtx.Facts)

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

	// Remember this conversation
	if s.memory != nil {
		if err := s.memory.Remember(ctx, memory.ConversationTurn{
			SessionID: sessionID,
			Timestamp: time.Now(),
			Query:     text,
			Response:  response,
		}); err != nil {
			log.Printf("Memory remember error for chat_id=%d: %v", chatID, err)
			// Continue (don't fail request)
		}
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
