// Package main provides HTTP API wrapper for Claude Code CLI
package main

import (
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func main() {
	server := NewServer()

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

	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
