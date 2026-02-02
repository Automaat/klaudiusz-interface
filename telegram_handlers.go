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

	// Build inline keyboard (callback data max 64 bytes - session ID only)
	keyboard := &models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{
			{
				{
					Text: "✅ Tak",
					CallbackData: fmt.Sprintf(
						"%s%s",
						CallbackDataConfirmPrefix,
						sessionID,
					),
				},
				{
					Text: "⏩ Zawsze",
					CallbackData: fmt.Sprintf(
						"%s%s",
						CallbackDataAlwaysPrefix,
						sessionID,
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
		ParseMode:   models.ParseModeHTML,
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
		ChatID:    chatID,
		Text:      text,
		ParseMode: models.ParseModeHTML,
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

	cfg := s.config.Get()

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

	// Parse callback data: "confirm:<session_id>"
	parts := strings.SplitN(callbackData, ":", CallbackDataConfirmParts)
	if len(parts) != CallbackDataConfirmParts {
		log.Printf("Invalid callback data format: %s", callbackData)
		s.handleCallbackError(ctx, b, chatID, callbackID, "Nieprawidłowe żądanie")

		return
	}

	sessionID := parts[1]

	// Validate session matches chat and user
	expectedSessionID := sessionIDFromContext(
		chatID,
		userID,
		chatType,
		cfg.Telegram.GroupSessionMode,
	)
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

	// Answer callback immediately (Telegram 5s timeout)
	answerCallbackQuery(ctx, b, callbackID)

	// Check what type of pending item exists
	session.mu.Lock()
	hasPendingAction := session.PendingAction != nil
	hasPendingPermission := session.PendingPermission != nil
	session.mu.Unlock()

	if hasPendingAction {
		// Execute dangerous action
		response, err := s.executeConfirmedAction(ctx, session, "")
		if err != nil {
			log.Printf("Failed to execute action for session %s: %v", sessionID, err)
			s.sendTelegramResponse(ctx, b, chatID, formatTelegramError(err))

			return
		}

		s.sendTelegramResponse(ctx, b, chatID, response)

		return
	}

	if hasPendingPermission {
		// Grant tool permission and retry
		s.handlePermissionGrant(ctx, b, session, chatID, false)

		return
	}

	s.sendTelegramResponse(ctx, b, chatID, "Nie ma oczekującej akcji")
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

	cfg := s.config.Get()

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
	expectedSessionID := sessionIDFromContext(
		chatID,
		userID,
		chatType,
		cfg.Telegram.GroupSessionMode,
	)
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

	// Load and clear pending action/permission
	val, ok := s.sessions.Load(sessionID)
	if ok {
		session, ok := val.(*Session)
		if ok {
			session.mu.Lock()
			session.PendingAction = nil
			session.PendingPermission = nil
			session.mu.Unlock()
		}
	}

	s.sendTelegramResponse(ctx, b, chatID, "Anulowano akcję")
}

func (s *Server) handleAlwaysApproval(
	ctx context.Context,
	b TelegramBot,
	session *Session,
	sessionID string,
	chatID int64,
) {
	// Check what type of pending item exists
	session.mu.Lock()
	hasPendingAction := session.PendingAction != nil
	hasPendingPermission := session.PendingPermission != nil
	session.mu.Unlock()

	if hasPendingAction {
		// Store tool approval before execution
		session.mu.Lock()

		if session.PendingAction != nil && len(session.PendingAction.Commands) > 0 {
			toolName := session.PendingAction.Commands[0]
			session.ApprovedTools[toolName] = true
			log.Printf("Tool approved for session %s: %s", sessionID, toolName)
		}

		session.mu.Unlock()

		// Execute dangerous action
		response, err := s.executeConfirmedAction(ctx, session, "")
		if err != nil {
			log.Printf("Failed to execute always action for session %s: %v", sessionID, err)
			s.sendTelegramResponse(ctx, b, chatID, formatTelegramError(err))

			return
		}

		s.sendTelegramResponse(
			ctx,
			b,
			chatID,
			response+"\n\nZapamiętano i będę wykonywać automatycznie.",
		)

		return
	}

	if hasPendingPermission {
		// Grant tool permission with "always" flag and retry
		s.handlePermissionGrant(ctx, b, session, chatID, true)

		return
	}

	s.sendTelegramResponse(ctx, b, chatID, "Nie ma oczekującej akcji")
}

func (s *Server) handlePermissionGrant(
	ctx context.Context,
	b TelegramBot,
	session *Session,
	chatID int64,
	rememberAlways bool,
) {
	cfg := s.config.Get()

	// Get permission details
	session.mu.Lock()
	permReq := session.PendingPermission
	session.PendingPermission = nil // Clear immediately
	session.mu.Unlock()

	if permReq == nil {
		s.sendTelegramResponse(ctx, b, chatID, "Nie ma oczekującego uprawnienia")

		return
	}

	// Update settings.local.json
	if err := addPermissionToSettings(cfg.Claude.WorkingDir, permReq.ToolPattern); err != nil {
		log.Printf("Failed to add permission for chat_id=%d: %v", chatID, err)
		s.sendTelegramResponse(ctx, b, chatID, "Nie mogę zaktualizować uprawnień")

		return
	}

	// Remember for this session if "Always" clicked
	if rememberAlways {
		session.mu.Lock()
		session.ApprovedPermissions[permReq.ToolPattern] = true
		log.Printf("Permission auto-approved for session %s: %s", session.ID, permReq.ToolPattern)
		session.mu.Unlock()
	}

	// Retry original query
	retryCtx, retryCancel := context.WithTimeout(ctx, cfg.Claude.ExecutionTimeout)
	defer retryCancel()

	// Build system prompt (reuse memory context if available)
	systemPrompt := buildSystemPromptWithMemory(permReq.OriginalQuery, nil, session.UserContext)

	retryResponse, err := executeClaude(
		retryCtx,
		systemPrompt,
		session,
		cfg.Claude.Path,
		cfg.Claude.WorkingDir,
		cfg.Claude.MaxPromptLength,
	)
	if err != nil {
		log.Printf("Retry after permission grant failed for chat_id=%d: %v", chatID, err)
		s.sendTelegramResponse(ctx, b, chatID, formatTelegramError(err))

		return
	}

	responseMsg := retryResponse
	if rememberAlways {
		responseMsg += "\n\nZapamiętano i będę wykonywać automatycznie."
	}

	s.sendTelegramResponse(ctx, b, chatID, responseMsg)
}

func (*Server) sendPermissionRequestForTool(
	ctx context.Context,
	b TelegramBot,
	chatID int64,
	sessionID string,
	permReq *PendingPermission,
) {
	// Build confirmation message
	confirmMsg := strings.TrimSpace(permReq.Description)
	if confirmMsg == "" {
		confirmMsg = "Potwierdź dostęp do narzędzia"
	} else {
		lastChar := confirmMsg[len(confirmMsg)-1]
		if lastChar != '.' && lastChar != '!' && lastChar != '?' {
			confirmMsg += "."
		}
	}

	// Build inline keyboard (same as dangerous actions)
	keyboard := &models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{
			{
				{
					Text: "✅ Tak",
					CallbackData: fmt.Sprintf(
						"%s%s",
						CallbackDataConfirmPrefix,
						sessionID,
					),
				},
				{
					Text: "⏩ Zawsze",
					CallbackData: fmt.Sprintf(
						"%s%s",
						CallbackDataAlwaysPrefix,
						sessionID,
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
		ParseMode:   models.ParseModeHTML,
	}); err != nil {
		log.Printf("Failed to send permission request to chat_id=%d: %v", chatID, err)
	}
}

func (s *Server) handleTelegramAlways(ctx context.Context, b *bot.Bot, update *models.Update) {
	s.handleTelegramAlwaysInternal(ctx, wrapBot(b), update)
}

func (s *Server) handleTelegramAlwaysInternal(
	ctx context.Context,
	b TelegramBot,
	update *models.Update,
) {
	if update.CallbackQuery == nil {
		return
	}

	cfg := s.config.Get()

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

	// Parse callback data: "always:<session_id>"
	parts := strings.SplitN(callbackData, ":", CallbackDataAlwaysParts)
	if len(parts) != CallbackDataAlwaysParts {
		log.Printf("Invalid always callback data format: %s", callbackData)
		s.handleCallbackError(ctx, b, chatID, callbackID, "Nieprawidłowe żądanie")

		return
	}

	sessionID := parts[1]

	// Validate session matches chat and user
	expectedSessionID := sessionIDFromContext(
		chatID,
		userID,
		chatType,
		cfg.Telegram.GroupSessionMode,
	)
	if sessionID != expectedSessionID {
		log.Printf(
			"Always session mismatch: callback=%s, expected=%s (user=%d, chat=%d)",
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
		log.Printf("Session not found for always: %s", sessionID)
		s.handleCallbackError(ctx, b, chatID, callbackID, "Sesja wygasła")

		return
	}

	session, ok := val.(*Session)
	if !ok {
		log.Printf("Failed to cast session for always: %s", sessionID)
		s.handleCallbackError(ctx, b, chatID, callbackID, "Błąd wewnętrzny")

		return
	}

	// Store tool approval before execution
	session.mu.Lock()

	if session.PendingAction != nil && len(session.PendingAction.Commands) > 0 {
		toolName := session.PendingAction.Commands[0]
		session.ApprovedTools[toolName] = true
		log.Printf("Tool approved for session %s: %s", sessionID, toolName)
	}

	session.mu.Unlock()

	// Answer callback immediately (Telegram 5s timeout)
	answerCallbackQuery(ctx, b, callbackID)

	// Handle based on pending item type
	s.handleAlwaysApproval(ctx, b, session, sessionID, chatID)
}
