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
	maxPhotoFileSize     = 20 * 1024 * 1024 // 20MB
	photoDownloadTimeout = 30 * time.Second
)

func TestSelectLargestPhoto(t *testing.T) {
	t.Run("empty array", func(t *testing.T) {
		photos := []models.PhotoSize{}
		result := selectLargestPhoto(photos)

		if result != nil {
			t.Error("expected nil for empty array")
		}
	})

	t.Run("single photo", func(t *testing.T) {
		photos := []models.PhotoSize{
			{FileID: "photo1", Width: 100, Height: 100},
		}
		result := selectLargestPhoto(photos)

		if result == nil {
			t.Fatal("expected non-nil result")
		}

		if result.FileID != "photo1" {
			t.Errorf("expected photo1, got %s", result.FileID)
		}
	})

	t.Run("multiple photos", func(t *testing.T) {
		photos := []models.PhotoSize{
			{FileID: "photo1", Width: 100, Height: 100},
			{FileID: "photo2", Width: 320, Height: 320},
			{FileID: "photo3", Width: 800, Height: 800},
		}
		result := selectLargestPhoto(photos)

		if result == nil {
			t.Fatal("expected non-nil result")
		}

		if result.FileID != "photo3" {
			t.Errorf("expected largest photo (photo3), got %s", result.FileID)
		}
	})
}

func TestDownloadPhotoMessage(t *testing.T) {
	t.Run("successful download", func(t *testing.T) {
		mockClient := testhelpers.NewMockHTTPClient(
			func(req *http.Request) (*http.Response, error) {
				if !strings.Contains(req.URL.String(), "api.telegram.org/file/bot") {
					t.Errorf("unexpected URL: %s", req.URL.String())
				}

				return testhelpers.MockSuccessResponse([]byte("fake-photo-data"))
			},
		)

		mockBot := &mockBot{
			getFileFunc: func(_ context.Context, _ *bot.GetFileParams) (*models.File, error) {
				return &models.File{
					FileID:   "photo-id",
					FilePath: "photos/test.jpg",
					FileSize: 100,
				}, nil
			},
		}

		ctx := context.Background()

		path, cleanup, err := downloadTelegramFile(
			ctx, mockBot, "photo-id", "telegram-photo-*.jpg",
			maxPhotoFileSize, photoDownloadTimeout,
			mockClient,
			"test-token",
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		defer cleanup()

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
					FileID:   "large-photo-id",
					FilePath: "photos/large.jpg",
					FileSize: maxPhotoFileSize + 1,
				}, nil
			},
		}

		ctx := context.Background()

		_, _, err := downloadPhotoMessage(
			ctx,
			mockBot,
			"large-photo-id",
			maxPhotoFileSize,
			photoDownloadTimeout,
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

		_, _, err := downloadPhotoMessage(
			ctx,
			mockBot,
			"error-photo-id",
			maxPhotoFileSize,
			photoDownloadTimeout,
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
					FileID:   "test-photo-id",
					FilePath: "photos/test.jpg",
					FileSize: 100,
				}, nil
			},
		}

		ctx := context.Background()

		_, _, err := downloadTelegramFile(
			ctx, mockBot, "test-photo-id", "telegram-photo-*.jpg",
			maxPhotoFileSize, photoDownloadTimeout,
			mockClient,
			"test-token",
		)
		if err == nil {
			t.Error("expected HTTP 404 error")
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
					FileID:   "test-photo-id",
					FilePath: "photos/test.jpg",
					FileSize: 100,
				}, nil
			},
		}

		ctx := context.Background()

		_, _, err := downloadTelegramFile(
			ctx, mockBot, "test-photo-id", "telegram-photo-*.jpg",
			maxPhotoFileSize, photoDownloadTimeout,
			mockClient,
			"test-token",
		)
		if err == nil {
			t.Error("expected HTTP 500 error")
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
					FileID:   "test-photo-id",
					FilePath: "photos/test.jpg",
					FileSize: 100,
				}, nil
			},
		}

		ctx := context.Background()

		_, _, err := downloadTelegramFile(
			ctx, mockBot, "test-photo-id", "telegram-photo-*.jpg",
			maxPhotoFileSize, photoDownloadTimeout,
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
					FileID:   "test-photo-id",
					FilePath: "photos/test.jpg",
					FileSize: 100,
				}, nil
			},
		}

		ctx := context.Background()

		_, _, err := downloadTelegramFile(
			ctx, mockBot, "test-photo-id", "telegram-photo-*.jpg",
			maxPhotoFileSize, photoDownloadTimeout,
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
					FileID:   "test-photo-id",
					FilePath: "photos/test.jpg",
					FileSize: 100,
				}, nil
			},
		}

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		_, _, err := downloadPhotoMessage(
			ctx,
			mockBot,
			"test-photo-id",
			maxPhotoFileSize,
			photoDownloadTimeout,
			"test-token",
		)
		if err == nil {
			t.Error("expected error from cancelled context")
		}
	})
}

func TestFormatPhotoError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected string
	}{
		{
			name:     "context deadline exceeded",
			err:      context.DeadlineExceeded,
			expected: "Nie mogę teraz pobrać zdjęcia",
		},
		{
			name:     "context canceled",
			err:      context.Canceled,
			expected: "Nie mogę teraz pobrać zdjęcia",
		},
		{
			name:     "file too large",
			err:      errors.New("file too large: 25000000 bytes"),
			expected: "Zdjęcie jest za duże",
		},
		{
			name:     "downloaded file too large",
			err:      errors.New("downloaded file too large: 25000000 bytes (limit 20971520)"),
			expected: "Zdjęcie jest za duże",
		},
		{
			name:     "download failed",
			err:      errors.New("download failed with status: 404"),
			expected: "Nie mogę pobrać zdjęcia",
		},
		{
			name:     "generic error",
			err:      errors.New("something went wrong"),
			expected: "Przepraszam, wystąpił błąd ze zdjęciem",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatPhotoError(tt.err)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}
