package main

import (
	"context"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/Automaat/klaudiusz-interface/internal/testhelpers"
)

const (
	maxVoiceFileSize     = 20 * 1024 * 1024 // 20MB
	voiceDownloadTimeout = 30 * time.Second
)

func TestDownloadVoiceMessage(t *testing.T) {
	t.Run("successful download", func(t *testing.T) {
		mockClient := testhelpers.NewMockHTTPClient(
			func(req *http.Request) (*http.Response, error) {
				if !strings.Contains(req.URL.String(), "api.telegram.org/file/bot") {
					t.Errorf("unexpected URL: %s", req.URL.String())
				}

				return testhelpers.MockSuccessResponse([]byte("fake-voice-data"))
			},
		)

		mockBot := &mockBot{
			getFileFunc: func(_ context.Context, _ *bot.GetFileParams) (*models.File, error) {
				return &models.File{
					FileID:   "voice-id",
					FilePath: "voice/test.oga",
					FileSize: 100,
				}, nil
			},
		}

		ctx := context.Background()

		path, cleanup, err := downloadTelegramFile(
			ctx, mockBot, "voice-id", "telegram-voice-*.oga",
			maxVoiceFileSize, voiceDownloadTimeout,
			mockClient,
			"test-token",
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify temp file exists
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Error("temp file not created")
		}

		// Verify cleanup
		cleanup()

		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Error("temp file not cleaned up")
		}
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

	t.Run("http 404 error", func(t *testing.T) {
		mockClient := testhelpers.NewMockHTTPClient(func(_ *http.Request) (*http.Response, error) {
			return testhelpers.MockStatusResponse(404)
		})

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

		_, _, err := downloadTelegramFile(
			ctx, mockBot, "test-file-id", "telegram-voice-*.oga",
			maxVoiceFileSize, voiceDownloadTimeout,
			mockClient,
			"test-token",
		)
		if err == nil {
			t.Fatal("expected HTTP 404 error")
		}

		if !strings.Contains(err.Error(), "404") {
			t.Errorf("expected 404 in error, got: %v", err)
		}
	})

	t.Run("http 500 error", func(t *testing.T) {
		mockClient := testhelpers.NewMockHTTPClient(func(_ *http.Request) (*http.Response, error) {
			return testhelpers.MockStatusResponse(500)
		})

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

		_, _, err := downloadTelegramFile(
			ctx, mockBot, "test-file-id", "telegram-voice-*.oga",
			maxVoiceFileSize, voiceDownloadTimeout,
			mockClient,
			"test-token",
		)
		if err == nil {
			t.Fatal("expected HTTP 500 error")
		}

		if !strings.Contains(err.Error(), "500") {
			t.Errorf("expected 500 in error, got: %v", err)
		}
	})

	t.Run("http timeout", func(t *testing.T) {
		mockClient := testhelpers.NewMockHTTPClient(func(_ *http.Request) (*http.Response, error) {
			return testhelpers.MockTimeoutError()
		})

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

		_, _, err := downloadTelegramFile(
			ctx, mockBot, "test-file-id", "telegram-voice-*.oga",
			maxVoiceFileSize, voiceDownloadTimeout,
			mockClient,
			"test-token",
		)
		if err == nil {
			t.Error("expected timeout error")
		}
	})

	t.Run("network error", func(t *testing.T) {
		mockClient := testhelpers.NewMockHTTPClient(func(_ *http.Request) (*http.Response, error) {
			return testhelpers.MockNetworkError()
		})

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

		_, _, err := downloadTelegramFile(
			ctx, mockBot, "test-file-id", "telegram-voice-*.oga",
			maxVoiceFileSize, voiceDownloadTimeout,
			mockClient,
			"test-token",
		)
		if err == nil {
			t.Error("expected network error")
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
