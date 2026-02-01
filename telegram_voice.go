package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/go-telegram/bot"
	"github.com/google/uuid"
)

const (
	maxVoiceFileSize     = 20 * 1024 * 1024 // 20MB
	voiceDownloadTimeout = 30 * time.Second
)

func downloadVoiceMessage(
	ctx context.Context,
	b TelegramBot,
	fileID string,
) (path string, cleanup func(), err error) {
	// Get file info
	file, err := b.GetFile(ctx, &bot.GetFileParams{FileID: fileID})
	if err != nil {
		return "", nil, errors.Wrap(err, "failed to get file info")
	}

	// Check file size
	if file.FileSize > maxVoiceFileSize {
		return "", nil, errors.Newf("file too large: %d bytes", file.FileSize)
	}

	// Build download URL
	downloadURL := fmt.Sprintf(
		"https://api.telegram.org/file/bot%s/%s",
		TelegramBotToken,
		file.FilePath,
	)

	// Create temp file
	tempPath := filepath.Join(
		os.TempDir(),
		fmt.Sprintf("telegram-voice-%s.oga", uuid.New().String()),
	)

	// Download with timeout
	downloadCtx, cancel := context.WithTimeout(ctx, voiceDownloadTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(downloadCtx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return "", nil, errors.Wrap(err, "failed to create download request")
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", nil, errors.Wrap(err, "download failed")
	}

	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			fmt.Printf("WARNING: Failed to close response body: %v\n", cerr)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return "", nil, errors.Newf("download failed with status: %d", resp.StatusCode)
	}

	// Write to temp file
	out, err := os.Create(tempPath) // #nosec G304 - tempPath is generated internally
	if err != nil {
		return "", nil, errors.Wrap(err, "failed to create temp file")
	}

	defer func() {
		if cerr := out.Close(); cerr != nil {
			fmt.Printf("WARNING: Failed to close temp file: %v\n", cerr)
		}
	}()

	if _, err := io.Copy(out, resp.Body); err != nil {
		_ = os.Remove(tempPath) // Best effort cleanup
		return "", nil, errors.Wrap(err, "failed to write temp file")
	}

	cleanupFunc := func() {
		if err := os.Remove(tempPath); err != nil {
			// Log but don't fail on cleanup error
			fmt.Printf("WARNING: Failed to cleanup temp file %s: %v\n", tempPath, err)
		}
	}

	return tempPath, cleanupFunc, nil
}

const (
	errCannotDownload = "Nie mogę pobrać nagrania"
	errFileTooLarge   = "Nagranie jest za duże"
)

func formatVoiceDownloadError(err error) string {
	if err == nil {
		return errCannotDownload
	}

	errStr := err.Error()

	switch {
	case containsAny(errStr, "timeout", "deadline exceeded"):
		return errCannotDownload
	case containsAny(errStr, "file too large"):
		return errFileTooLarge
	default:
		return errCannotDownload
	}
}

func containsAny(s string, substrs ...string) bool {
	for _, substr := range substrs {
		if len(substr) > 0 && len(s) >= len(substr) {
			for i := 0; i <= len(s)-len(substr); i++ {
				if s[i:i+len(substr)] == substr {
					return true
				}
			}
		}
	}

	return false
}
