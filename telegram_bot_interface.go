package main

import (
	"context"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// TelegramBot defines the interface for Telegram bot operations needed for testing
type TelegramBot interface {
	SendMessage(ctx context.Context, params *bot.SendMessageParams) (*models.Message, error)
	AnswerCallbackQuery(ctx context.Context, params *bot.AnswerCallbackQueryParams) (bool, error)
}

// realBot wraps *bot.Bot to implement TelegramBot interface
type realBot struct {
	bot *bot.Bot
}

func (r *realBot) SendMessage(ctx context.Context, params *bot.SendMessageParams) (*models.Message, error) {
	return r.bot.SendMessage(ctx, params)
}

func (r *realBot) AnswerCallbackQuery(ctx context.Context, params *bot.AnswerCallbackQueryParams) (bool, error) {
	return r.bot.AnswerCallbackQuery(ctx, params)
}

// wrapBot wraps a *bot.Bot to implement the TelegramBot interface
func wrapBot(b *bot.Bot) TelegramBot {
	return &realBot{bot: b}
}
