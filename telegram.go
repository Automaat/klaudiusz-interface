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

// extractMessageText extracts text from various message types
func (s *Server) extractMessageText(
	ctx context.Context,
	b TelegramBot,
	msg *models.Message,
	chatID int64,
) (string, bool) {
	cfg := s.config.Get()

	switch {
	case msg.Voice != nil && cfg.Telegram.Voice.Enabled:
		transcribed, err := s.handleVoiceMessage(ctx, b, msg.Voice.FileID)
		if err != nil {
			s.sendTelegramResponse(ctx, b, chatID, formatDeepgramError(err))
			return "", false
		}

		return transcribed, true
	case msg.Photo != nil && cfg.Telegram.Photo.Enabled:
		if len(msg.Photo) == 0 {
			return "", false
		}

		largest := selectLargestPhoto(msg.Photo)
		if largest == nil {
			return "", false
		}

		imagePath, cleanup, err := downloadPhotoMessage(
			ctx,
			b,
			largest.FileID,
			cfg.Telegram.Photo.MaxFileSize,
			cfg.Telegram.Photo.DownloadTimeout,
			cfg.Telegram.BotToken,
		)
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

func initTelegramBot(s *Server, botToken string) (context.CancelFunc, error) {
	if botToken == "" {
		return nil, errors.New("telegram bot token not set")
	}

	opts := []bot.Option{
		bot.WithDefaultHandler(s.handleTelegramMessage),
		bot.WithCallbackQueryDataHandler("confirm:", bot.MatchTypePrefix, s.handleTelegramCallback),
		bot.WithCallbackQueryDataHandler("always:", bot.MatchTypePrefix, s.handleTelegramAlways),
		bot.WithCallbackQueryDataHandler("cancel:", bot.MatchTypePrefix, s.handleTelegramCancel),
	}

	b, err := bot.New(botToken, opts...)
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

// extractUserContextFromMessage extracts user context from Telegram message
func extractUserContextFromMessage(
	msg *models.Message,
	chatID int64,
	chatType string,
) *UserContext {
	var userID int64

	var firstName, lastName, username string

	if msg.From != nil {
		userID = msg.From.ID
		firstName = msg.From.FirstName
		lastName = msg.From.LastName
		username = msg.From.Username
	} else {
		// Channel post or anonymous admin
		userID = chatID
		firstName = "Channel"
	}

	return &UserContext{
		UserID:    userID,
		FirstName: firstName,
		LastName:  lastName,
		Username:  username,
		ChatType:  chatType,
		ChatID:    chatID,
		GroupMode: "", // Will be set by caller with config value
	}
}

// recallMemoryContext retrieves relevant facts from memory
func (s *Server) recallMemoryContext(
	ctx context.Context,
	text string,
	chatID int64,
	factLimit int,
) memory.Context {
	var memoryCtx memory.Context

	if s.memory != nil {
		var err error

		memoryCtx, err = s.memory.Recall(ctx, text, memory.RecallOptions{
			IncludeFacts: true,
			FactLimit:    factLimit,
		})
		if err != nil {
			log.Printf("Memory recall error for chat_id=%d: %v", chatID, err)
			// Continue without memory (graceful degradation)
		}
	}

	return memoryCtx
}

// rememberConversation stores conversation turn in memory
func (s *Server) rememberConversation(
	ctx context.Context,
	sessionID string,
	text string,
	response string,
	chatID int64,
) {
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
}

// handleApprovedAction auto-executes actions with session-scoped approval
func (s *Server) handleApprovedAction(
	ctx context.Context,
	b TelegramBot,
	session *Session,
	action *PendingAction,
	chatID int64,
) bool {
	session.mu.Lock()

	toolName := ""
	if len(action.Commands) > 0 {
		toolName = action.Commands[0]
	}

	alreadyApproved := toolName != "" && session.ApprovedTools[toolName]
	session.mu.Unlock()

	if !alreadyApproved {
		return false
	}

	// Auto-execute without confirmation
	session.mu.Lock()
	session.PendingAction = action
	session.mu.Unlock()

	execResponse, execErr := s.executeConfirmedAction(ctx, session, "")
	if execErr != nil {
		log.Printf("Auto-execute error for chat_id=%d: %v", chatID, execErr)
		s.sendTelegramResponse(ctx, b, chatID, formatTelegramError(execErr))

		return true
	}

	s.sendTelegramResponse(ctx, b, chatID, execResponse)

	return true
}

func (s *Server) handleTelegramMessageInternal(
	ctx context.Context,
	b TelegramBot,
	update *models.Update,
) {
	if update.Message == nil {
		return
	}

	cfg := s.config.Get()

	chatID := update.Message.Chat.ID
	chatType := string(update.Message.Chat.Type)

	// Extract user context
	userCtx := extractUserContextFromMessage(update.Message, chatID, chatType)
	userCtx.GroupMode = cfg.Telegram.GroupSessionMode // Set from config
	userCtx.IsTelegram = true                         // Mark as Telegram channel for emoji formatting
	sessionID := sessionIDFromContext(
		chatID,
		userCtx.UserID,
		chatType,
		cfg.Telegram.GroupSessionMode,
	)

	text, ok := s.extractMessageText(ctx, b, update.Message, chatID)
	if !ok {
		return
	}

	log.Printf(
		"Telegram message from user_id=%d (%s), chat_id=%d (%s), session_id=%s: %s",
		userCtx.UserID,
		userCtx.DisplayName(),
		chatID,
		chatType,
		sessionID,
		text,
	)

	session := s.getOrCreateSessionWithContext(sessionID, userCtx)

	// Recall relevant context from memory
	memoryCtx := s.recallMemoryContext(ctx, text, chatID, cfg.Memory.Extraction.FactLimit)

	// Build system prompt with memory context
	systemPrompt := buildSystemPromptWithMemory(text, memoryCtx.Facts, userCtx)

	// Execute Claude with timeout
	execCtx, cancel := context.WithTimeout(ctx, cfg.Claude.ExecutionTimeout)
	defer cancel()

	response, err := executeClaude(
		execCtx,
		systemPrompt,
		session,
		cfg.Claude.Path,
		cfg.Claude.WorkingDir,
		cfg.Claude.MaxPromptLength,
	)
	if err != nil {
		log.Printf("Claude error for chat_id=%d: %v", chatID, err)
		s.sendTelegramResponse(ctx, b, chatID, formatTelegramError(err))

		return
	}

	// Check if permission required
	if action, needsPermission := parsePermissionRequest(response); needsPermission {
		// Check and handle session-scoped approval
		if s.handleApprovedAction(ctx, b, session, action, chatID) {
			return
		}

		// No approval - show permission dialog with "Always" button
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
	s.rememberConversation(ctx, sessionID, text, response, chatID)

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

	cfg := s.config.Get()

	// Download voice file
	filePath, cleanup, err := downloadVoiceMessageWithConfig(
		ctx,
		b,
		fileID,
		cfg.Telegram.Voice.MaxFileSize,
		cfg.Telegram.Voice.DownloadTimeout,
		cfg.Telegram.BotToken,
	)
	if err != nil {
		return "", errors.Wrap(err, "voice download failed")
	}
	defer cleanup()

	// Transcribe
	transcript, err := transcribeAudioFile(
		ctx,
		s.deepgramClient,
		filePath,
		cfg.Deepgram.Model,
		cfg.Deepgram.Language,
	)
	if err != nil {
		return "", errors.Wrap(err, "transcription failed")
	}

	return transcript, nil
}
