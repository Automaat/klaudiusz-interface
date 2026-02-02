package main

import (
	"context"
	"testing"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

const (
	maxVoiceFileSize     = 20 * 1024 * 1024 // 20MB
	voiceDownloadTimeout = 30 * time.Second
)

func TestDownloadVoiceMessage(t *testing.T) {
	t.Run("successful download", func(t *testing.T) {
		// Skip - needs HTTP mocking infrastructure (see issue #29)
		t.Skip("Test requires HTTP mocking infrastructure")
	})

	t.Run("file too large", func(t *testing.T) {
		mockBot := &mockBot{
			getFileFunc: func(_ context.Context, _ *bot.GetFileParams) (*models.File, error) {
				return &models.File{
					FileID:   "large-file-id",
					FilePath: "voice/large.oga",
					FileSize: maxVoiceFileSize + 1,
				}, nil
			},
		}

		ctx := context.Background()

		_, _, err := downloadVoiceMessage(
			ctx,
			mockBot,
			"large-file-id",
			maxVoiceFileSize,
			voiceDownloadTimeout,
			"test-token",
		)
		if err == nil {
			t.Error("expected error for large file")
		}

		if err.Error() != "file too large: 20971521 bytes" {
			t.Errorf("expected 'file too large' error, got: %v", err)
		}
	})

	t.Run("get file error", func(t *testing.T) {
		mockBot := &mockBot{
			getFileFunc: func(_ context.Context, _ *bot.GetFileParams) (*models.File, error) {
				return nil, errors.New("API error")
			},
		}

		ctx := context.Background()

		_, _, err := downloadVoiceMessage(
			ctx,
			mockBot,
			"error-file-id",
			maxVoiceFileSize,
			voiceDownloadTimeout,
			"test-token",
		)
		if err == nil {
			t.Error("expected error when GetFile fails")
		}
	})

	t.Run("http status error", func(t *testing.T) {
		// This tests the HTTP error path by simulating GetFile success but URL doesn't exist
		mockBot := &mockBot{
			getFileFunc: func(_ context.Context, _ *bot.GetFileParams) (*models.File, error) {
				return &models.File{
					FileID:   "test-file-id",
					FilePath: "voice/test.oga",
					FileSize: 100,
				}, nil
			},
		}

		ctx := context.Background()

		_, _, err := downloadVoiceMessage(
			ctx,
			mockBot,
			"test-file-id",
			maxVoiceFileSize,
			voiceDownloadTimeout,
			"test-token",
		)
		// Should fail because the Telegram API URL is not accessible in test
		if err == nil {
			t.Error("expected HTTP error")
		}
	})

	t.Run("context cancellation", func(t *testing.T) {
		mockBot := &mockBot{
			getFileFunc: func(_ context.Context, _ *bot.GetFileParams) (*models.File, error) {
				return &models.File{
					FileID:   "test-file-id",
					FilePath: "voice/test.oga",
					FileSize: 100,
				}, nil
			},
		}

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		_, _, err := downloadVoiceMessage(
			ctx,
			mockBot,
			"test-file-id",
			maxVoiceFileSize,
			voiceDownloadTimeout,
			"test-token",
		)
		if err == nil {
			t.Error("expected error from cancelled context")
		}
	})
}
