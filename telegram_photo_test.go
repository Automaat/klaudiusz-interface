package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
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
	// Save original token and restore after test
	oldToken := TelegramBotToken

	defer func() { TelegramBotToken = oldToken }()

	t.Run("successful download", func(t *testing.T) {
		expectedData := []byte("mock image data")

		// Mock HTTP server
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(expectedData)
		}))
		defer server.Close()

		// Mock bot that returns path pointing to our mock server
		mockBot := &mockBot{
			getFileFunc: func(_ context.Context, _ *bot.GetFileParams) (*models.File, error) {
				return &models.File{
					FileID:   "test-photo-id",
					FilePath: "photos/test.jpg",
					FileSize: int64(len(expectedData)),
				}, nil
			},
		}

		// Set token to construct URL that points to mock server
		TelegramBotToken = "test-token"

		// Create HTTP client that redirects Telegram API to our mock server
		mockClient := &http.Client{
			Transport: &mockTransport{
				mockServer: server,
			},
		}

		ctx := context.Background()

		filePath, cleanup, err := downloadPhotoMessageWithClient(
			ctx, mockBot, "test-photo-id", mockClient,
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		defer cleanup()

		if filePath == "" {
			t.Error("expected non-empty file path")
		}

		// Verify file contains expected data
		data, err := os.ReadFile(filePath)
		if err != nil {
			t.Fatalf("failed to read downloaded file: %v", err)
		}

		if string(data) != string(expectedData) {
			t.Errorf("expected %q, got %q", expectedData, data)
		}

		// Verify cleanup works
		cleanup()

		if _, err := os.Stat(filePath); !os.IsNotExist(err) {
			t.Error("cleanup should have removed temp file")
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

		_, _, err := downloadPhotoMessage(ctx, mockBot, "large-photo-id", maxPhotoFileSize, photoDownloadTimeout, "test-token")
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

		_, _, err := downloadPhotoMessage(ctx, mockBot, "error-photo-id", maxPhotoFileSize, photoDownloadTimeout, "test-token")
		if err == nil {
			t.Error("expected error when GetFile fails")
		}
	})

	t.Run("http status error", func(t *testing.T) {
		// This tests the HTTP error path by simulating GetFile success but URL doesn't exist
		mockBot := &mockBot{
			getFileFunc: func(_ context.Context, _ *bot.GetFileParams) (*models.File, error) {
				return &models.File{
					FileID:   "test-photo-id",
					FilePath: "photos/test.jpg",
					FileSize: 100,
				}, nil
			},
		}

		TelegramBotToken = "test-token"
		ctx := context.Background()

		_, _, err := downloadPhotoMessage(ctx, mockBot, "test-photo-id", maxPhotoFileSize, photoDownloadTimeout, "test-token")
		// Should fail because the Telegram API URL is not accessible in test
		if err == nil {
			t.Error("expected HTTP error")
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

		TelegramBotToken = "test-token"
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		_, _, err := downloadPhotoMessage(ctx, mockBot, "test-photo-id", maxPhotoFileSize, photoDownloadTimeout, "test-token")
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

// mockTransport redirects all HTTP requests to a test server.
type mockTransport struct {
	mockServer *httptest.Server
}

func (m *mockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Redirect to mock server
	req.URL.Scheme = "http"
	req.URL.Host = m.mockServer.URL[7:] // Strip "http://"

	return http.DefaultTransport.RoundTrip(req)
}
