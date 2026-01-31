// Package main provides HTTP API wrapper for Claude Code CLI
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/cockroachdb/errors"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func main() {
	server := NewServer()

	// Initialize Telegram bot if enabled
	var cancelBot context.CancelFunc

	if TelegramEnabled {
		if TelegramBotToken == "" {
			log.Fatal("TELEGRAM_ENABLED=true but TELEGRAM_BOT_TOKEN not set")
		}

		cancel, err := initTelegramBot(server)
		if err != nil {
			log.Fatalf("Failed to init Telegram bot: %v", err)
		}

		cancelBot = cancel

		log.Printf("Telegram bot initialized")
	}

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Post("/ask", server.handleAsk)
	r.Post("/cancel", server.handleCancel)
	r.Get("/health", server.handleHealth)

	srv := &http.Server{
		Addr:         ":" + Port,
		Handler:      r,
		ReadTimeout:  ReadTimeout,
		WriteTimeout: WriteTimeout,
		IdleTimeout:  IdleTimeout,
	}

	log.Printf("Starting Claude HA Brain server on port %s", Port)
	log.Printf("Claude CLI: %s", ClaudePath)
	log.Printf("Working directory: %s", WorkingDir)
	log.Printf("Session timeout: %.0f minutes", SessionTimeout.Minutes())

	runServer(srv, cancelBot)
}

// runServer starts the HTTP server and handles graceful shutdown
func runServer(srv *http.Server, cancelBot context.CancelFunc) {
	// Start server in background
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("Server failed: %v", err)
		}
	}()

	// Wait for interrupt signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh

	gracefulShutdown(srv, cancelBot)
}

// gracefulShutdown stops the telegram bot and HTTP server gracefully
func gracefulShutdown(srv *http.Server, cancelBot context.CancelFunc) {
	log.Printf("Shutting down gracefully...")

	// Stop telegram bot first
	if cancelBot != nil {
		log.Printf("Stopping Telegram bot")
		cancelBot()
	}

	// Shutdown HTTP server with timeout
	ctx, cancel := context.WithTimeout(context.Background(), ShutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("Server shutdown error: %v", err)
	}

	log.Printf("Server stopped")
}
