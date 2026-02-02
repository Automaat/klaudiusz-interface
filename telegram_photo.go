package main

import (
	"context"
	"net/http"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/go-telegram/bot/models"
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
	botToken string,
) (path string, cleanup func(), err error) {
	return downloadPhotoMessageWithClient(ctx, b, fileID, nil, botToken)
}

func downloadPhotoMessageWithClient(
	ctx context.Context,
	b TelegramBot,
	fileID string,
	httpClient *http.Client,
	botToken string,
) (path string, cleanup func(), err error) {
	return downloadTelegramFile(
		ctx,
		b,
		fileID,
		"telegram-photo-*.jpg",
		20*1024*1024,  // 20MB default
		30*1000000000, // 30s default
		httpClient,
		botToken,
	)
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
