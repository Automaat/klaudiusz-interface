package main

import (
	"log"
	"sync"
	"time"

	"github.com/Automaat/klaudiusz-interface/config"
	"github.com/Automaat/klaudiusz-interface/memory"
)

type Server struct {
	sessions       sync.Map // map[string]*Session
	stopCh         chan struct{}
	deepgramClient *DeepgramClient
	memory         memory.MemoryService
	config         *config.Config
}

func NewServer(cfg *config.Config) *Server {
	s := &Server{
		stopCh: make(chan struct{}),
		config: cfg,
	}

	// Capture config snapshot
	c := cfg.Get()

	// Initialize Deepgram client if voice enabled
	if c.Telegram.Enabled && c.Telegram.Voice.Enabled {
		client, err := initDeepgramClient(c.Deepgram.APIKey)
		if err != nil {
			log.Printf("WARNING: Deepgram init failed: %v", err)
		} else {
			s.deepgramClient = client

			log.Printf(
				"Deepgram client initialized (model=%s, language=%s)",
				c.Deepgram.Model,
				c.Deepgram.Language,
			)
		}
	}

	// Initialize memory service
	if c.Memory.Enabled {
		storage, err := memory.NewSQLiteStorage(c.Memory.DBPath)
		if err != nil {
			log.Printf("WARNING: Memory storage init failed: %v", err)
		} else {
			extractor := memory.NewClaudeExtractor(
				c.Claude.Path,
				c.Claude.WorkingDir,
				c.Memory.Extraction.Timeout,
				c.Memory.Extraction.MaxConversations,
			)
			retriever := memory.NewSimpleRetriever(storage)
			s.memory = memory.NewService(storage, extractor, retriever)

			// Start background extraction
			s.memory.StartBackgroundExtraction(c.Memory.Extraction.Interval)

			log.Printf("Memory service initialized (db=%s)", c.Memory.DBPath)
		}
	}

	go s.cleanupSessions()

	return s
}

func (s *Server) Close() {
	close(s.stopCh)

	if s.memory != nil {
		if err := s.memory.Close(); err != nil {
			log.Printf("Error closing memory service: %v", err)
		}
	}
}

func (s *Server) cleanupSessions() {
	// Get initial cleanup interval from config
	c := s.config.Get()
	// Ticker interval set at startup - changing session.cleanup_interval requires restart
	ticker := time.NewTicker(c.Session.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			// Use current config for timeout check (hot-reloadable)
			cfg := s.config.Get()
			now := time.Now()

			s.sessions.Range(func(key, value interface{}) bool {
				session, ok := value.(*Session)
				if !ok {
					return true
				}

				session.mu.Lock()
				expired := now.Sub(session.LastActivity) > cfg.Session.Timeout
				session.mu.Unlock()

				if expired {
					log.Printf("Session %s expired", session.ID)
					s.sessions.Delete(key)
				}

				return true
			})
		}
	}
}
