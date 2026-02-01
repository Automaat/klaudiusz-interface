package main

import (
	"context"
	"strings"
	"testing"

	"github.com/cockroachdb/errors"
)

func TestInitDeepgramClient(t *testing.T) {
	t.Run("fails without API key", func(t *testing.T) {
		oldKey := DeepgramAPIKey
		DeepgramAPIKey = ""

		defer func() { DeepgramAPIKey = oldKey }()

		_, err := initDeepgramClient()
		if err == nil {
			t.Error("expected error when API key not set")
		}
	})

	t.Run("succeeds with API key", func(t *testing.T) {
		oldKey := DeepgramAPIKey
		DeepgramAPIKey = "test-key"

		defer func() { DeepgramAPIKey = oldKey }()

		client, err := initDeepgramClient()
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		if client == nil {
			t.Error("expected client, got nil")
		}
	})
}

func TestFormatDeepgramError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected string
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: "Przepraszam, wystąpił błąd z transkrypcją",
		},
		{
			name:     "timeout error",
			err:      errors.New("context deadline exceeded"),
			expected: "Nie mogę teraz przetworzyć nagrania",
		},
		{
			name:     "empty transcript",
			err:      errors.New("empty transcript from deepgram"),
			expected: "Nie słyszałem nic na nagraniu",
		},
		{
			name:     "rate limit",
			err:      errors.New("rate limit exceeded"),
			expected: "Przekroczono limit transkrypcji",
		},
		{
			name:     "invalid audio",
			err:      errors.New("invalid audio format"),
			expected: "Nie mogę odczytać pliku audio",
		},
		{
			name:     "file too large",
			err:      errors.New("file too large"),
			expected: "Nagranie jest za duże",
		},
		{
			name:     "download error",
			err:      errors.New("download failed"),
			expected: "Nie mogę pobrać nagrania",
		},
		{
			name:     "generic error",
			err:      errors.New("some other error"),
			expected: "Przepraszam, wystąpił błąd z transkrypcją",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatDeepgramError(tt.err)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestTranscribeAudioFile_NilClient(t *testing.T) {
	ctx := context.Background()

	_, err := transcribeAudioFile(ctx, nil, "/tmp/test.oga")
	if err == nil {
		t.Error("expected error with nil client")
	}

	if !strings.Contains(err.Error(), "nil") {
		t.Errorf("expected nil error message, got: %v", err)
	}
}
