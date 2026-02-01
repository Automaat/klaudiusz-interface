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
	defaultMemoryDB   = "~/.klaudiusz/memory.db"
	trueString        = "true"
)

var (
	ClaudePath       string
	WorkingDir       string
	TelegramBotToken string
	TelegramEnabled  bool
	DeepgramAPIKey   string
	DeepgramLanguage string
	DeepgramModel    string
	VoiceEnabled     bool
	PhotoEnabled     bool
	MemoryDBPath     string
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
	TelegramEnabled = getEnvOrDefault("TELEGRAM_ENABLED", "false") == trueString
	DeepgramAPIKey = getEnvOrDefault("DEEPGRAM_API_KEY", "")
	DeepgramLanguage = getEnvOrDefault("DEEPGRAM_LANGUAGE", "pl")
	DeepgramModel = getEnvOrDefault("DEEPGRAM_MODEL", "nova-3")
	MemoryDBPath = getEnvOrDefault("MEMORY_DB_PATH", defaultMemoryDB)

	// Respect explicit VOICE_ENABLED setting, or auto-enable if API key present
	voiceEnabledEnv := getEnvOrDefault("VOICE_ENABLED", "")
	switch voiceEnabledEnv {
	case trueString:
		VoiceEnabled = true
	case "false":
		VoiceEnabled = false
	default:
		// Auto-enable if API key present
		VoiceEnabled = DeepgramAPIKey != ""
	}

	// Auto-enable photos (no API key needed)
	photoEnabledEnv := getEnvOrDefault("PHOTO_ENABLED", "")
	switch photoEnabledEnv {
	case trueString:
		PhotoEnabled = true
	case "false":
		PhotoEnabled = false
	default:
		PhotoEnabled = true // default enabled
	}
}

func getEnvOrDefault(key, defaultValue string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}

	return defaultValue
}
