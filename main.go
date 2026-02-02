// Package main provides HTTP API wrapper for Claude Code CLI
package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/Automaat/klaudiusz-interface/config"
	"github.com/cockroachdb/errors"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func main() {
	// Parse command-line flags
	configPath := flag.String("config", "", "path to config file")
	flag.Parse()

	// Find and load config
	cfgFile, err := config.FindConfigFile(*configPath)
	if err != nil {
		log.Fatalf("Config error: %v", err)
	}

	cfg, err := config.New(cfgFile, true) // Enable live reload
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	defer cfg.Close()

	if cfgFile != "" {
		log.Printf("Loaded config from: %s", cfgFile)
	} else {
		log.Printf("Using default configuration (no config file found)")
	}

	// Create server with config
	server := NewServer(cfg)
	defer server.Close()

	// Get initial config snapshot
	c := cfg.Get()

	// Initialize Telegram bot if enabled
	var cancelBot context.CancelFunc

	if c.Telegram.Enabled {
		if c.Telegram.BotToken == "" {
			log.Fatal("telegram.enabled=true but bot_token not set")
		}

		cancel, err := initTelegramBot(server, c.Telegram.BotToken)
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
	r.Post("/admin/extract-facts", server.handleExtractFacts)

	srv := &http.Server{
		Addr:         ":" + c.Server.Port,
		Handler:      r,
		ReadTimeout:  c.Server.ReadTimeout,
		WriteTimeout: c.Server.WriteTimeout,
		IdleTimeout:  c.Server.IdleTimeout,
	}

	log.Printf("Starting Claude HA Brain server on port %s", c.Server.Port)
	log.Printf("Claude CLI: %s", c.Claude.Path)
	log.Printf("Working directory: %s", c.Claude.WorkingDir)
	log.Printf("Session timeout: %.0f minutes", c.Session.Timeout.Minutes())

	runServer(srv, server, cancelBot, cfg)
}

// runServer starts the HTTP server and handles graceful shutdown
func runServer(srv *http.Server, server *Server, cancelBot context.CancelFunc, cfg *config.Config) {
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

	gracefulShutdown(srv, server, cancelBot, cfg)
}

// gracefulShutdown stops the telegram bot and HTTP server gracefully
func gracefulShutdown(srv *http.Server, server *Server, cancelBot context.CancelFunc, cfg *config.Config) {
	log.Printf("Shutting down gracefully...")

	// Stop telegram bot first
	if cancelBot != nil {
		log.Printf("Stopping Telegram bot")
		cancelBot()
	}

	// Close server resources
	server.Close()

	// Shutdown HTTP server with timeout
	c := cfg.Get()
	ctx, cancel := context.WithTimeout(context.Background(), c.Server.ShutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("Server shutdown error: %v", err)
	}

	log.Printf("Server stopped")
}
