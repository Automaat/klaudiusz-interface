package main

import (
	"context"
	"strings"

	"github.com/cockroachdb/errors"
	restapi "github.com/deepgram/deepgram-go-sdk/v3/pkg/api/listen/v1/rest"
	interfaces "github.com/deepgram/deepgram-go-sdk/v3/pkg/client/interfaces"
	restclient "github.com/deepgram/deepgram-go-sdk/v3/pkg/client/listen/v1/rest"
)

// DeepgramClient wraps the Deepgram client
type DeepgramClient struct {
	client *restapi.Client
}

func initDeepgramClient(apiKey, model, language string) (*DeepgramClient, error) {
	if apiKey == "" {
		return nil, errors.New("deepgram API key not set")
	}

	restClient := restclient.New(apiKey, &interfaces.ClientOptions{})
	if restClient == nil {
		return nil, errors.New("failed to create Deepgram rest client")
	}

	client := restapi.New(restClient)
	if client == nil {
		return nil, errors.New("failed to create Deepgram client")
	}

	return &DeepgramClient{client: client}, nil
}

func transcribeAudioFile(
	ctx context.Context,
	client *DeepgramClient,
	filePath string,
	model string,
	language string,
) (string, error) {
	if client == nil || client.client == nil {
		return "", errors.New("deepgram client is nil")
	}

	options := &interfaces.PreRecordedTranscriptionOptions{
		Language:    language,
		Model:       model,
		Punctuate:   true,
		SmartFormat: true,
	}

	res, err := client.client.FromFile(ctx, filePath, options)
	if err != nil {
		return "", errors.Wrap(err, "deepgram transcription failed")
	}

	if res == nil || len(res.Results.Channels) == 0 ||
		len(res.Results.Channels[0].Alternatives) == 0 {
		return "", errors.New("empty transcript from deepgram")
	}

	transcript := strings.TrimSpace(res.Results.Channels[0].Alternatives[0].Transcript)
	if transcript == "" {
		return "", errors.New("empty transcript text")
	}

	return transcript, nil
}

func formatDeepgramError(err error) string {
	if err == nil {
		return "Przepraszam, wystąpił błąd z transkrypcją"
	}

	errStr := err.Error()

	switch {
	case strings.Contains(errStr, "timeout") || strings.Contains(errStr, "deadline exceeded"):
		return "Nie mogę teraz przetworzyć nagrania"
	case strings.Contains(errStr, "empty transcript"):
		return "Nie słyszałem nic na nagraniu"
	case strings.Contains(errStr, "rate limit"):
		return "Przekroczono limit transkrypcji"
	case strings.Contains(errStr, "invalid audio") || strings.Contains(errStr, "unsupported format"):
		return "Nie mogę odczytać pliku audio"
	case strings.Contains(errStr, "file too large"):
		return "Nagranie jest za duże"
	case strings.Contains(errStr, "download"):
		return "Nie mogę pobrać nagrania"
	default:
		return "Przepraszam, wystąpił błąd z transkrypcją"
	}
}
