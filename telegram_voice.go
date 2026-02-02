package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/go-telegram/bot"
)

func downloadTelegramFile(
	ctx context.Context,
	b TelegramBot,
	fileID string,
	tempFilePrefix string,
	maxFileSize int64,
	downloadTimeout time.Duration,
	httpClient *http.Client,
	botToken string,
) (path string, cleanup func(), err error) {
	// Get file info
	file, err := b.GetFile(ctx, &bot.GetFileParams{FileID: fileID})
	if err != nil {
		return "", nil, errors.Wrap(err, "failed to get file info")
	}

	// Check file size
	if file.FileSize > maxFileSize {
		return "", nil, errors.Newf("file too large: %d bytes", file.FileSize)
	}

	// Create secure temp file with O_EXCL and 0600 perms
	tempFile, err := os.CreateTemp(os.TempDir(), tempFilePrefix)
	if err != nil {
		return "", nil, errors.Wrap(err, "failed to create temp file")
	}

	tempPath := tempFile.Name()

	// Build download URL
	downloadURL := fmt.Sprintf(
		"https://api.telegram.org/file/bot%s/%s",
		botToken,
		file.FilePath,
	)

	// Download with timeout
	downloadCtx, cancel := context.WithTimeout(ctx, downloadTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(downloadCtx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return "", nil, errors.Wrap(err, "failed to create download request")
	}

	client := httpClient
	if client == nil {
		client = http.DefaultClient
	}

	resp, err := client.Do(req)
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

	defer func() {
		if cerr := tempFile.Close(); cerr != nil {
			fmt.Printf("WARNING: Failed to close temp file: %v\n", cerr)
		}
	}()

	// Limit read to maxFileSize to protect against malicious server
	limitedReader := io.LimitReader(resp.Body, maxFileSize+1)

	n, err := io.Copy(tempFile, limitedReader)
	if err != nil {
		_ = os.Remove(tempPath)
		return "", nil, errors.Wrap(err, "failed to write temp file")
	}

	if n > maxFileSize {
		_ = os.Remove(tempPath)

		return "", nil, errors.Newf(
			"downloaded file too large: %d bytes (limit %d)",
			n,
			maxFileSize,
		)
	}

	cleanupFunc := func() {
		if err := os.Remove(tempPath); err != nil {
			// Log but don't fail on cleanup error
			fmt.Printf("WARNING: Failed to cleanup temp file %s: %v\n", tempPath, err)
		}
	}

	return tempPath, cleanupFunc, nil
}

func downloadVoiceMessage(
	ctx context.Context,
	b TelegramBot,
	fileID string,
	botToken string,
) (path string, cleanup func(), err error) {
	return downloadVoiceMessageWithConfig(ctx, b, fileID, 20*1024*1024, 30*time.Second, botToken)
}

func downloadVoiceMessageWithConfig(
	ctx context.Context,
	b TelegramBot,
	fileID string,
	maxFileSize int64,
	downloadTimeout time.Duration,
	botToken string,
) (path string, cleanup func(), err error) {
	return downloadTelegramFile(
		ctx,
		b,
		fileID,
		"telegram-voice-*.oga",
		maxFileSize,
		downloadTimeout,
		nil, // Use default HTTP client
		botToken,
	)
}
