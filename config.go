package main

import (
	"os"
	"time"
)

const (
	Port              = "8742"
	ReadTimeout       = 15 * time.Second
	WriteTimeout      = 15 * time.Second
	IdleTimeout       = 60 * time.Second
	defaultClaudePath = "/Users/marcin.skalski@konghq.com/.local/bin/claude"
	defaultWorkingDir = "/Users/marcin.skalski@konghq.com/sideprojects/klaudiusz-brain"
)

var (
	ClaudePath = getEnvOrDefault("CLAUDE_PATH", defaultClaudePath)
	WorkingDir = getEnvOrDefault("WORKING_DIR", defaultWorkingDir)
)

func getEnvOrDefault(key, defaultValue string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}

	return defaultValue
}
