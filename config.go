package main

import (
	"log"

	"github.com/joho/godotenv"
)

func init() {
	// Load .env file if present (optional for local development)
	if err := godotenv.Load(); err != nil {
		log.Printf("No .env file loaded (optional): %v", err)
	} else {
		log.Printf("Loaded .env file")
	}
}
