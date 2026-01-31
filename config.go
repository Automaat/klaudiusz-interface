package main

import (
	"log"
	"os"
	"time"

	"github.com/joho/godotenv"
)

const (
	Port              = "8742"
	ReadTimeout       = 15 * time.Second
	WriteTimeout      = 15 * time.Second
	IdleTimeout       = 60 * time.Second
	ShutdownTimeout   = 10 * time.Second
	defaultClaudePath = "/Users/marcin.skalski@konghq.com/.local/bin/claude"
	defaultWorkingDir = "/Users/marcin.skalski@konghq.com/sideprojects/klaudiusz-brain"
)

var (
	ClaudePath       string
	WorkingDir       string
	TelegramBotToken string
	TelegramEnabled  bool
)

func init() {
	// Load .env file if present (optional for local development)
	if err := godotenv.Load(); err != nil {
		log.Printf("No .env file loaded (optional): %v", err)
	} else {
		log.Printf("Loaded .env file")
	}

	// Initialize config variables after loading .env
	ClaudePath = getEnvOrDefault("CLAUDE_PATH", defaultClaudePath)
	WorkingDir = getEnvOrDefault("WORKING_DIR", defaultWorkingDir)
	TelegramBotToken = getEnvOrDefault("TELEGRAM_BOT_TOKEN", "")
	TelegramEnabled = getEnvOrDefault("TELEGRAM_ENABLED", "false") == "true"
}

func getEnvOrDefault(key, defaultValue string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}

	return defaultValue
}
