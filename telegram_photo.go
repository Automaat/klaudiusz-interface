package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/go-telegram/bot/models"
	"github.com/google/uuid"
)

const (
	maxPhotoFileSize     = 20 * 1024 * 1024 // 20MB
	photoDownloadTimeout = 30 * time.Second
)

// selectLargestPhoto returns the largest photo from the array.
// Telegram orders photos by size, so we return the last element.
func selectLargestPhoto(photos []models.PhotoSize) *models.PhotoSize {
	if len(photos) == 0 {
		return nil
	}

	return &photos[len(photos)-1]
}

func downloadPhotoMessage(
	ctx context.Context,
	b TelegramBot,
	fileID string,
) (path string, cleanup func(), err error) {
	tempPath := filepath.Join(
		os.TempDir(),
		fmt.Sprintf("telegram-photo-%s.jpg", uuid.New().String()),
	)

	return downloadTelegramFile(ctx, b, fileID, tempPath, maxPhotoFileSize, photoDownloadTimeout)
}

// formatPhotoError converts photo download errors to Polish TTS-friendly messages.
func formatPhotoError(err error) string {
	errMsg := err.Error()

	if errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, context.Canceled) {
		return "Nie mogę teraz pobrać zdjęcia"
	}

	if strings.Contains(errMsg, "file too large") ||
		strings.Contains(errMsg, "downloaded file too large") {
		return "Zdjęcie jest za duże"
	}

	if strings.Contains(errMsg, "download failed") {
		return "Nie mogę pobrać zdjęcia"
	}

	return "Przepraszam, wystąpił błąd ze zdjęciem"
}
