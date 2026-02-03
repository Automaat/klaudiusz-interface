package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

func TestStoreToolApproval(t *testing.T) {
	t.Parallel()

	t.Run("Store tool from pending action", func(t *testing.T) {
		t.Parallel()

		session := &Session{
			ID:                  "test-session",
			LastActivity:        time.Now(),
			ApprovedTools:       make(map[string]bool),
			ApprovedPermissions: make(map[string]bool),
			PendingAction: &PendingAction{
				Commands: []string{"dangerous-command"},
			},
		}

		storeToolApproval(session, "test-session")

		if !session.ApprovedTools["dangerous-command"] {
			t.Error("Tool not approved")
		}
	})

	t.Run("No pending action", func(t *testing.T) {
		t.Parallel()

		session := &Session{
			ID:                  "test-session",
			LastActivity:        time.Now(),
			ApprovedTools:       make(map[string]bool),
			ApprovedPermissions: make(map[string]bool),
			PendingAction:       nil,
		}

		// Should not panic
		storeToolApproval(session, "test-session")

		if len(session.ApprovedTools) != 0 {
			t.Error("Expected no approved tools")
		}
	})

	t.Run("Empty commands in pending action", func(t *testing.T) {
		t.Parallel()

		session := &Session{
			ID:                  "test-session",
			LastActivity:        time.Now(),
			ApprovedTools:       make(map[string]bool),
			ApprovedPermissions: make(map[string]bool),
			PendingAction: &PendingAction{
				Commands: []string{},
			},
		}

		storeToolApproval(session, "test-session")

		if len(session.ApprovedTools) != 0 {
			t.Error("Expected no approved tools")
		}
	})
}

func TestSendPermissionRequestForTool(t *testing.T) {
	t.Parallel()

	t.Run("Send permission request with description", func(t *testing.T) {
		t.Parallel()

		messageSent := false
		mb := &mockBot{
			sendMessageFunc: func(_ context.Context, params *bot.SendMessageParams) (*models.Message, error) {
				messageSent = true

				// ChatID should be int64, but in Telegram bot library it's actually an interface{}
				// that can be either int64 or string. Let's just skip this check.

				expectedText := "Dostęp do Todoist."
				if params.Text != expectedText {
					t.Errorf("Text = %q, want %q", params.Text, expectedText)
				}

				// Verify keyboard
				keyboard, ok := params.ReplyMarkup.(*models.InlineKeyboardMarkup)
				if !ok {
					t.Fatal("ReplyMarkup is not InlineKeyboardMarkup")
				}

				if len(keyboard.InlineKeyboard) != 1 {
					t.Errorf("Expected 1 row, got %d", len(keyboard.InlineKeyboard))
				}

				if len(keyboard.InlineKeyboard[0]) != 3 {
					t.Errorf("Expected 3 buttons, got %d", len(keyboard.InlineKeyboard[0]))
				}

				return &models.Message{ID: 1}, nil
			},
		}

		server := &Server{}
		permReq := &PendingPermission{
			ToolPattern:   "mcp__todoist__*",
			ToolName:      "Todoist",
			Description:   "Dostęp do Todoist",
			OriginalQuery: "dodaj zadanie",
		}

		server.sendPermissionRequestForTool(context.Background(), mb, 123, "session-123", permReq)

		if !messageSent {
			t.Error("Message was not sent")
		}
	})

	t.Run("Add period to description without punctuation", func(t *testing.T) {
		t.Parallel()

		mb := &mockBot{
			sendMessageFunc: func(_ context.Context, params *bot.SendMessageParams) (*models.Message, error) {
				expectedText := "Dostęp do GitHub."
				if params.Text != expectedText {
					t.Errorf("Text = %q, want %q", params.Text, expectedText)
				}

				return &models.Message{ID: 1}, nil
			},
		}

		server := &Server{}
		permReq := &PendingPermission{
			Description: "Dostęp do GitHub",
		}

		server.sendPermissionRequestForTool(context.Background(), mb, 123, "session-123", permReq)
	})

	t.Run("Empty description fallback", func(t *testing.T) {
		t.Parallel()

		mb := &mockBot{
			sendMessageFunc: func(_ context.Context, params *bot.SendMessageParams) (*models.Message, error) {
				expectedText := "Potwierdź dostęp do narzędzia"
				if params.Text != expectedText {
					t.Errorf("Text = %q, want %q", params.Text, expectedText)
				}

				return &models.Message{ID: 1}, nil
			},
		}

		server := &Server{}
		permReq := &PendingPermission{
			Description: "",
		}

		server.sendPermissionRequestForTool(context.Background(), mb, 123, "session-123", permReq)
	})
}

func TestHandleAlwaysApproval(t *testing.T) {
	t.Parallel()

	t.Run("Handle pending action with always", func(t *testing.T) {
		t.Parallel()

		// Create mock Claude script that succeeds
		tmpDir := t.TempDir()
		claudePath := filepath.Join(tmpDir, "claude")

		script := `#!/bin/bash
echo "Action executed successfully"
exit 0
`
		if err := os.WriteFile(claudePath, []byte(script), 0o755); err != nil {
			t.Fatalf("Failed to create mock claude: %v", err)
		}

		cfg := testConfigWithCustomClaude(claudePath)
		server := &Server{
			config: cfg,
		}

		messageSent := false
		mb := &mockBot{
			sendMessageFunc: func(_ context.Context, params *bot.SendMessageParams) (*models.Message, error) {
				messageSent = true

				if params.Text == "" {
					t.Error("Empty response text")
				}

				// Should contain "always" confirmation
				if !contains(params.Text, "Zapamiętano") {
					t.Errorf("Response missing 'Zapamiętano': %q", params.Text)
				}

				return &models.Message{ID: 1}, nil
			},
		}

		session := &Session{
			ID:                  "test-session",
			LastActivity:        time.Now(),
			ApprovedTools:       make(map[string]bool),
			ApprovedPermissions: make(map[string]bool),
			PendingAction: &PendingAction{
				Description: "Test action",
				Commands:    []string{"test-command"},
			},
		}

		server.handleAlwaysApproval(context.Background(), mb, session, "test-session", 123)

		if !messageSent {
			t.Error("Message was not sent")
		}

		// Verify tool was approved
		if !session.ApprovedTools["test-command"] {
			t.Error("Tool was not approved")
		}
	})

	t.Run("Handle pending permission with always", func(t *testing.T) {
		t.Parallel()

		// Create temp directory with .claude settings
		tmpDir := t.TempDir()
		claudePath := filepath.Join(tmpDir, "claude")

		script := `#!/bin/bash
echo "Query executed successfully"
exit 0
`
		if err := os.WriteFile(claudePath, []byte(script), 0o755); err != nil {
			t.Fatalf("Failed to create mock claude: %v", err)
		}

		cfg := testConfigWithCustomClaude(claudePath)
		cfg.Get().Claude.WorkingDir = tmpDir
		server := &Server{
			config: cfg,
		}

		messageSent := false
		mb := &mockBot{
			sendMessageFunc: func(_ context.Context, params *bot.SendMessageParams) (*models.Message, error) {
				messageSent = true

				// Should contain "always" confirmation
				if !contains(params.Text, "Zapamiętano") {
					t.Errorf("Response missing 'Zapamiętano': %q", params.Text)
				}

				return &models.Message{ID: 1}, nil
			},
		}

		session := &Session{
			ID:                  "test-session",
			LastActivity:        time.Now(),
			ApprovedTools:       make(map[string]bool),
			ApprovedPermissions: make(map[string]bool),
			PendingPermission: &PendingPermission{
				ToolPattern:   "mcp__todoist__*",
				ToolName:      "Todoist",
				Description:   "Dostęp do Todoist",
				OriginalQuery: "dodaj zadanie",
			},
		}

		server.handleAlwaysApproval(context.Background(), mb, session, "test-session", 123)

		if !messageSent {
			t.Error("Message was not sent")
		}

		// Verify permission was approved for session
		if !session.ApprovedPermissions["mcp__todoist__*"] {
			t.Error("Permission was not approved for session")
		}
	})

	t.Run("No pending action or permission", func(t *testing.T) {
		t.Parallel()

		cfg := testConfigWithCustomClaude("/bin/echo")
		server := &Server{
			config: cfg,
		}

		messageSent := false
		mb := &mockBot{
			sendMessageFunc: func(_ context.Context, params *bot.SendMessageParams) (*models.Message, error) {
				messageSent = true

				expectedText := "Nie ma oczekującej akcji"
				if params.Text != expectedText {
					t.Errorf("Text = %q, want %q", params.Text, expectedText)
				}

				return &models.Message{ID: 1}, nil
			},
		}

		session := &Session{
			ID:                  "test-session",
			LastActivity:        time.Now(),
			ApprovedTools:       make(map[string]bool),
			ApprovedPermissions: make(map[string]bool),
		}

		server.handleAlwaysApproval(context.Background(), mb, session, "test-session", 123)

		if !messageSent {
			t.Error("Message was not sent")
		}
	})
}

func TestHandlePermissionGrant(t *testing.T) {
	t.Parallel()

	t.Run("Grant permission and retry query", func(t *testing.T) {
		t.Parallel()

		// Create temp directory with mock Claude
		tmpDir := t.TempDir()
		claudePath := filepath.Join(tmpDir, "claude")

		script := `#!/bin/bash
echo "Query executed successfully"
exit 0
`
		if err := os.WriteFile(claudePath, []byte(script), 0o755); err != nil {
			t.Fatalf("Failed to create mock claude: %v", err)
		}

		cfg := testConfigWithCustomClaude(claudePath)
		cfg.Get().Claude.WorkingDir = tmpDir
		server := &Server{
			config: cfg,
		}

		messageSent := false
		mb := &mockBot{
			sendMessageFunc: func(_ context.Context, params *bot.SendMessageParams) (*models.Message, error) {
				messageSent = true

				if params.Text == "" {
					t.Error("Empty response text")
				}

				return &models.Message{ID: 1}, nil
			},
		}

		session := &Session{
			ID:                  "test-session",
			LastActivity:        time.Now(),
			ApprovedTools:       make(map[string]bool),
			ApprovedPermissions: make(map[string]bool),
			PendingPermission: &PendingPermission{
				ToolPattern:   "mcp__todoist__*",
				ToolName:      "Todoist",
				Description:   "Dostęp do Todoist",
				OriginalQuery: "dodaj zadanie",
			},
		}

		server.handlePermissionGrant(context.Background(), mb, session, 123, false)

		if !messageSent {
			t.Error("Message was not sent")
		}

		// Verify permission was added to settings
		settingsPath := filepath.Join(tmpDir, ".claude", "settings.local.json")
		if _, err := os.Stat(settingsPath); os.IsNotExist(err) {
			t.Error("Settings file was not created")
		}
	})

	t.Run("Grant with remember always", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		claudePath := filepath.Join(tmpDir, "claude")

		script := `#!/bin/bash
echo "Query executed"
exit 0
`
		if err := os.WriteFile(claudePath, []byte(script), 0o755); err != nil {
			t.Fatalf("Failed to create mock claude: %v", err)
		}

		cfg := testConfigWithCustomClaude(claudePath)
		cfg.Get().Claude.WorkingDir = tmpDir
		server := &Server{
			config: cfg,
		}

		messageSent := false
		mb := &mockBot{
			sendMessageFunc: func(_ context.Context, params *bot.SendMessageParams) (*models.Message, error) {
				messageSent = true

				// Should contain "always" confirmation
				if !contains(params.Text, "Zapamiętano") {
					t.Errorf("Response missing 'Zapamiętano': %q", params.Text)
				}

				return &models.Message{ID: 1}, nil
			},
		}

		session := &Session{
			ID:                  "test-session",
			LastActivity:        time.Now(),
			ApprovedTools:       make(map[string]bool),
			ApprovedPermissions: make(map[string]bool),
			PendingPermission: &PendingPermission{
				ToolPattern:   "mcp__todoist__*",
				ToolName:      "Todoist",
				Description:   "Dostęp do Todoist",
				OriginalQuery: "dodaj zadanie",
			},
		}

		server.handlePermissionGrant(context.Background(), mb, session, 123, true)

		if !messageSent {
			t.Error("Message was not sent")
		}

		// Verify permission was approved for session
		if !session.ApprovedPermissions["mcp__todoist__*"] {
			t.Error("Permission was not approved for session")
		}
	})

	t.Run("No pending permission", func(t *testing.T) {
		t.Parallel()

		cfg := testConfigWithCustomClaude("/bin/echo")
		server := &Server{
			config: cfg,
		}

		messageSent := false
		mb := &mockBot{
			sendMessageFunc: func(_ context.Context, params *bot.SendMessageParams) (*models.Message, error) {
				messageSent = true

				expectedText := "Nie ma oczekującego uprawnienia"
				if params.Text != expectedText {
					t.Errorf("Text = %q, want %q", params.Text, expectedText)
				}

				return &models.Message{ID: 1}, nil
			},
		}

		session := &Session{
			ID:                  "test-session",
			LastActivity:        time.Now(),
			ApprovedTools:       make(map[string]bool),
			ApprovedPermissions: make(map[string]bool),
		}

		server.handlePermissionGrant(context.Background(), mb, session, 123, false)

		if !messageSent {
			t.Error("Message was not sent")
		}
	})
}

func TestHandleTelegramAlwaysInternal(t *testing.T) {
	t.Parallel()

	t.Run("Process always callback successfully", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		claudePath := filepath.Join(tmpDir, "claude")

		script := `#!/bin/bash
echo "Executed"
exit 0
`
		if err := os.WriteFile(claudePath, []byte(script), 0o755); err != nil {
			t.Fatalf("Failed to create mock claude: %v", err)
		}

		cfg := testConfigWithCustomClaude(claudePath)
		server := &Server{
			config: cfg,
		}
		server.sessions.Store("u_123", &Session{
			ID:                  "u_123",
			LastActivity:        time.Now(),
			ApprovedTools:       make(map[string]bool),
			ApprovedPermissions: make(map[string]bool),
			PendingAction: &PendingAction{
				Description: "Test action",
				Commands:    []string{"test-cmd"},
			},
		})

		callbackAnswered := false
		messageSent := false
		mb := &mockBot{
			answerCallbackQueryFunc: func(_ context.Context, _ *bot.AnswerCallbackQueryParams) (bool, error) {
				callbackAnswered = true

				return true, nil
			},
			sendMessageFunc: func(_ context.Context, _ *bot.SendMessageParams) (*models.Message, error) {
				messageSent = true

				return &models.Message{ID: 1}, nil
			},
		}

		update := &models.Update{
			CallbackQuery: &models.CallbackQuery{
				ID:   "cb123",
				Data: CallbackDataAlwaysPrefix + "u_123",
				Message: models.MaybeInaccessibleMessage{
					Message: &models.Message{
						Chat: models.Chat{
							ID:   123,
							Type: models.ChatTypePrivate,
						},
					},
				},
				From: models.User{ID: 123},
			},
		}

		server.handleTelegramAlwaysInternal(context.Background(), mb, update)

		if !callbackAnswered {
			t.Error("Callback was not answered")
		}

		if !messageSent {
			t.Error("Message was not sent")
		}
	})

	t.Run("Session ID mismatch", func(t *testing.T) {
		t.Parallel()

		cfg := testConfigWithCustomClaude("/bin/echo")
		server := &Server{
			config: cfg,
		}

		callbackAnswered := false
		messageSent := false
		mb := &mockBot{
			answerCallbackQueryFunc: func(_ context.Context, _ *bot.AnswerCallbackQueryParams) (bool, error) {
				callbackAnswered = true

				return true, nil
			},
			sendMessageFunc: func(_ context.Context, params *bot.SendMessageParams) (*models.Message, error) {
				messageSent = true

				expectedText := "Nieprawidłowa sesja"
				if params.Text != expectedText {
					t.Errorf("Text = %q, want %q", params.Text, expectedText)
				}

				return &models.Message{ID: 1}, nil
			},
		}

		update := &models.Update{
			CallbackQuery: &models.CallbackQuery{
				ID:   "cb123",
				Data: CallbackDataAlwaysPrefix + "u_123",
				Message: models.MaybeInaccessibleMessage{
					Message: &models.Message{
						Chat: models.Chat{
							ID:   123,
							Type: models.ChatTypePrivate,
						},
					},
				},
				From: models.User{ID: 123},
			},
		}

		server.handleTelegramAlwaysInternal(context.Background(), mb, update)

		if !callbackAnswered {
			t.Error("Callback was not answered")
		}

		if !messageSent {
			t.Error("Message was not sent")
		}
	})

	t.Run("Invalid callback data format", func(t *testing.T) {
		t.Parallel()

		cfg := testConfigWithCustomClaude("/bin/echo")
		server := &Server{
			config: cfg,
		}

		callbackAnswered := false
		messageSent := false
		mb := &mockBot{
			answerCallbackQueryFunc: func(_ context.Context, _ *bot.AnswerCallbackQueryParams) (bool, error) {
				callbackAnswered = true

				return true, nil
			},
			sendMessageFunc: func(_ context.Context, _ *bot.SendMessageParams) (*models.Message, error) {
				messageSent = true

				return &models.Message{ID: 1}, nil
			},
		}

		update := &models.Update{
			CallbackQuery: &models.CallbackQuery{
				ID:   "cb123",
				Data: "invalid",
				Message: models.MaybeInaccessibleMessage{
					Message: &models.Message{
						Chat: models.Chat{
							ID:   123,
							Type: models.ChatTypePrivate,
						},
					},
				},
				From: models.User{ID: 123},
			},
		}

		server.handleTelegramAlwaysInternal(context.Background(), mb, update)

		if !callbackAnswered {
			t.Error("Callback was not answered")
		}

		if !messageSent {
			t.Error("Message was not sent")
		}
	})
}
