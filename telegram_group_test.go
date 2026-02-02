package main

import (
	"context"
	"testing"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/stretchr/testify/assert"
)

func TestSessionIDGeneration_PrivateChat(t *testing.T) {
	chatID := int64(123456789)
	userID := int64(987654321)

	// Private chats should always use chatID only
	sessionID := sessionIDFromContext(chatID, userID, "private", "per_user")
	expectedSessionID := chatIDToSessionID(chatID)

	assert.Equal(t, expectedSessionID, sessionID, "private chat should use chatID only")
}

func TestSessionIDGeneration_GroupPerUser(t *testing.T) {
	chatID := int64(123456789)
	userID1 := int64(111)
	userID2 := int64(222)

	sessionID1 := sessionIDFromContext(chatID, userID1, "group", "per_user")
	sessionID2 := sessionIDFromContext(chatID, userID2, "group", "per_user")

	// Different users should get different sessions
	assert.NotEqual(t, sessionID1, sessionID2, "different users should have different sessions")

	// Same user should get same session
	sessionID1Again := sessionIDFromContext(chatID, userID1, "group", "per_user")
	assert.Equal(t, sessionID1, sessionID1Again, "same user should get same session")
}

func TestSessionIDGeneration_GroupShared(t *testing.T) {
	chatID := int64(123456789)
	userID1 := int64(111)
	userID2 := int64(222)

	sessionID1 := sessionIDFromContext(chatID, userID1, "group", "shared")
	sessionID2 := sessionIDFromContext(chatID, userID2, "group", "shared")

	// Different users should get SAME session in shared mode
	assert.Equal(
		t,
		sessionID1,
		sessionID2,
		"different users should share same session in shared mode",
	)

	// Should match chat-based session
	expectedSessionID := chatIDToSessionID(chatID)
	assert.Equal(t, expectedSessionID, sessionID1, "shared mode should use chatID")
}

func TestSessionIDGeneration_Supergroup(t *testing.T) {
	chatID := int64(123456789)
	userID := int64(111)

	// Supergroups should behave like groups
	sessionIDGroup := sessionIDFromContext(chatID, userID, "group", "per_user")
	sessionIDSupergroup := sessionIDFromContext(chatID, userID, "supergroup", "per_user")

	assert.Equal(
		t,
		sessionIDGroup,
		sessionIDSupergroup,
		"group and supergroup should use same logic",
	)
}

func TestUserContext_DisplayName(t *testing.T) {
	tests := []struct {
		name     string
		ctx      UserContext
		expected string
	}{
		{
			name: "username only",
			ctx: UserContext{
				UserID:   123,
				Username: "testuser",
			},
			expected: "@testuser",
		},
		{
			name: "first name only",
			ctx: UserContext{
				UserID:    123,
				FirstName: "John",
			},
			expected: "John",
		},
		{
			name: "first and last name",
			ctx: UserContext{
				UserID:    123,
				FirstName: "John",
				LastName:  "Doe",
			},
			expected: "John Doe",
		},
		{
			name: "username takes priority",
			ctx: UserContext{
				UserID:    123,
				Username:  "jdoe",
				FirstName: "John",
				LastName:  "Doe",
			},
			expected: "@jdoe",
		},
		{
			name: "no name info",
			ctx: UserContext{
				UserID: 123,
			},
			expected: "User 123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.ctx.DisplayName()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestUserContext_IsGroupChat(t *testing.T) {
	tests := []struct {
		name     string
		chatType string
		expected bool
	}{
		{"private chat", "private", false},
		{"group chat", "group", true},
		{"supergroup chat", "supergroup", true},
		{"channel", "channel", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := UserContext{ChatType: tt.chatType}
			result := ctx.IsGroupChat()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestUserChatIDToSessionID_Deterministic(t *testing.T) {
	chatID := int64(123456789)
	userID := int64(987654321)

	// Same inputs should produce same output
	sessionID1 := userChatIDToSessionID(chatID, userID)
	sessionID2 := userChatIDToSessionID(chatID, userID)

	assert.Equal(t, sessionID1, sessionID2, "session ID generation should be deterministic")

	// Different inputs should produce different outputs
	sessionID3 := userChatIDToSessionID(chatID+1, userID)
	assert.NotEqual(t, sessionID1, sessionID3, "different chatID should produce different session")

	sessionID4 := userChatIDToSessionID(chatID, userID+1)
	assert.NotEqual(t, sessionID1, sessionID4, "different userID should produce different session")
}

func TestChatIDToSessionID_Deterministic(t *testing.T) {
	chatID := int64(123456789)

	// Same input should produce same output
	sessionID1 := chatIDToSessionID(chatID)
	sessionID2 := chatIDToSessionID(chatID)

	assert.Equal(t, sessionID1, sessionID2, "session ID generation should be deterministic")

	// Different input should produce different output
	sessionID3 := chatIDToSessionID(chatID + 1)
	assert.NotEqual(t, sessionID1, sessionID3, "different chatID should produce different session")
}

func TestHandleTelegramCallback_UserValidation(t *testing.T) {
	t.Run("prevents callback hijacking in group per_user mode", func(t *testing.T) {
		server := NewServer(testConfig())
		defer server.Close()

		chatID := int64(12345)
		userID1 := int64(111)
		userID2 := int64(222)

		// User 1 triggers permission request
		sessionID1 := sessionIDFromContext(chatID, userID1, "group", "per_user")
		session1 := server.getOrCreateSessionWithContext(sessionID1, &UserContext{
			UserID:     userID1,
			ChatID:     chatID,
			ChatType:   "group",
			IsTelegram: true,
		})
		session1.mu.Lock()
		session1.PendingAction = &PendingAction{
			ID:          "action-123",
			Description: "Test",
			Commands:    []string{"test.command"},
		}
		session1.mu.Unlock()

		var sentMessage string

		mockB := &mockBot{
			sendMessageFunc: func(_ context.Context, params *bot.SendMessageParams) (*models.Message, error) {
				sentMessage = params.Text
				return &models.Message{ID: 1}, nil
			},
			answerCallbackQueryFunc: func(_ context.Context, _ *bot.AnswerCallbackQueryParams) (bool, error) {
				return true, nil
			},
		}

		// User 2 attempts to confirm User 1's action
		update := &models.Update{
			CallbackQuery: &models.CallbackQuery{
				ID:   "callback-123",
				Data: "confirm:" + sessionID1 + ":action-123",
				From: models.User{ID: userID2, FirstName: "UserTwo"},
				Message: models.MaybeInaccessibleMessage{
					Message: &models.Message{
						Chat: models.Chat{ID: chatID, Type: "group"},
					},
				},
			},
		}

		ctx := context.Background()

		server.handleTelegramCallbackInternal(ctx, mockB, update)

		// Should reject with session mismatch
		assert.Equal(t, "Nieprawidłowa sesja", sentMessage, "should reject callback hijacking")

		// Verify pending action still exists
		session1.mu.Lock()
		defer session1.mu.Unlock()

		assert.NotNil(t, session1.PendingAction, "pending action should still exist")
	})

	t.Run("allows same user to confirm their own action in group", func(t *testing.T) {
		server := NewServer(testConfig())
		defer server.Close()

		chatID := int64(12345)
		userID := int64(111)

		sessionID := sessionIDFromContext(chatID, userID, "group", "per_user")
		session := server.getOrCreateSessionWithContext(sessionID, &UserContext{
			UserID:     userID,
			ChatID:     chatID,
			ChatType:   "group",
			IsTelegram: true,
		})
		session.mu.Lock()
		session.PendingAction = &PendingAction{
			ID:          "action-123",
			Description: "Test",
			Commands:    []string{"test.command"},
		}
		session.mu.Unlock()

		// Mock Claude

		// ClaudePath and WorkingDir now come from config

		var answeredCallback bool

		mockB := &mockBot{
			sendMessageFunc: func(_ context.Context, _ *bot.SendMessageParams) (*models.Message, error) {
				return &models.Message{ID: 1}, nil
			},
			answerCallbackQueryFunc: func(_ context.Context, _ *bot.AnswerCallbackQueryParams) (bool, error) {
				answeredCallback = true
				return true, nil
			},
		}

		// Same user confirms their own action
		update := &models.Update{
			CallbackQuery: &models.CallbackQuery{
				ID:   "callback-123",
				Data: "confirm:" + sessionID + ":action-123",
				From: models.User{ID: userID, FirstName: "UserOne"},
				Message: models.MaybeInaccessibleMessage{
					Message: &models.Message{
						Chat: models.Chat{ID: chatID, Type: "group"},
					},
				},
			},
		}

		ctx := context.Background()

		server.handleTelegramCallbackInternal(ctx, mockB, update)

		assert.True(t, answeredCallback, "callback should be answered")
	})
}

func TestHandleTelegramMessage_UserContext(t *testing.T) {
	t.Run("extracts user context from message", func(t *testing.T) {
		server := NewServer(testConfig())
		defer server.Close()

		chatID := int64(12345)
		userID := int64(111)

		// Mock Claude

		// ClaudePath and WorkingDir now come from config

		mockB := &mockBot{
			sendMessageFunc: func(_ context.Context, _ *bot.SendMessageParams) (*models.Message, error) {
				return &models.Message{ID: 1}, nil
			},
		}

		update := &models.Update{
			Message: &models.Message{
				Text: "test message",
				Chat: models.Chat{ID: chatID, Type: "group"},
				From: &models.User{
					ID:        userID,
					FirstName: "John",
					LastName:  "Doe",
					Username:  "johndoe",
				},
			},
		}

		ctx := context.Background()

		server.handleTelegramMessageInternal(ctx, mockB, update)

		// Verify session created with user context
		sessionID := sessionIDFromContext(chatID, userID, "group", "per_user")
		val, ok := server.sessions.Load(sessionID)
		assert.True(t, ok, "session should be created")

		session, ok := val.(*Session)
		assert.True(t, ok, "should be valid session")

		assert.NotNil(t, session.UserContext, "user context should be set")
		assert.Equal(t, userID, session.UserContext.UserID)
		assert.Equal(t, "John", session.UserContext.FirstName)
		assert.Equal(t, "Doe", session.UserContext.LastName)
		assert.Equal(t, "johndoe", session.UserContext.Username)
		assert.Equal(t, "group", session.UserContext.ChatType)
	})

	t.Run("handles channel post with nil From", func(t *testing.T) {
		server := NewServer(testConfig())
		defer server.Close()

		chatID := int64(12345)

		// Mock Claude

		// ClaudePath and WorkingDir now come from config

		mockB := &mockBot{
			sendMessageFunc: func(_ context.Context, _ *bot.SendMessageParams) (*models.Message, error) {
				return &models.Message{ID: 1}, nil
			},
		}

		update := &models.Update{
			Message: &models.Message{
				Text: "test message",
				Chat: models.Chat{ID: chatID, Type: "channel"},
				From: nil, // Channel post
			},
		}

		ctx := context.Background()

		server.handleTelegramMessageInternal(ctx, mockB, update)

		// Should use chatID as userID fallback
		sessionID := sessionIDFromContext(chatID, chatID, "channel", "per_user")
		val, ok := server.sessions.Load(sessionID)
		assert.True(t, ok, "session should be created")

		session, ok := val.(*Session)
		assert.True(t, ok, "should be valid session")

		assert.NotNil(t, session.UserContext, "user context should be set")
		assert.Equal(t, chatID, session.UserContext.UserID, "should use chatID as fallback")
		assert.Equal(t, "Channel", session.UserContext.FirstName, "should use Channel as name")
	})
}
