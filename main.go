package main

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

const Port = "8742"

func main() {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Post("/ask", handleAsk)
	r.Post("/cancel", handleCancel)
	r.Get("/health", handleHealth)

	log.Printf("Starting server on port %s", Port)
	if err := http.ListenAndServe(":"+Port, r); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

func handleAsk(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]string{"status": "stub"})
}

func handleCancel(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]string{"status": "stub"})
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
