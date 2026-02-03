package main

import (
	"log"
	"sync"
	"time"

	"github.com/Automaat/klaudiusz-interface/config"
	"github.com/Automaat/klaudiusz-interface/memory"
	"github.com/Automaat/klaudiusz-interface/scheduler"
)

type Server struct {
	sessions       sync.Map // map[string]*Session
	stopCh         chan struct{}
	deepgramClient *DeepgramClient
	memory         memory.MemoryService
	config         *config.Config
	scheduler      *scheduler.SchedulerManager
	schedulerMu    sync.RWMutex
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
		s.initMemoryService(c)
	}

	// Initialize scheduler
	if c.Scheduler.Enabled {
		s.initScheduler(c)
	}

	// Register config reload callback
	cfg.OnReload(s.reloadConfig)

	go s.cleanupSessions()

	return s
}

func (s *Server) initMemoryService(c *config.ConfigData) {
	storage, err := memory.NewSQLiteStorage(c.Memory.DBPath)
	if err != nil {
		log.Printf("WARNING: Memory storage init failed: %v", err)
		return
	}

	extractor, err := memory.NewClaudeExtractor(
		c.Claude.Path,
		c.Claude.WorkingDir,
		c.Memory.Extraction.Timeout,
		c.Memory.Extraction.MaxConversations,
	)
	if err != nil {
		log.Printf("WARNING: Memory extractor init failed: %v", err)
		return
	}

	retriever := memory.NewSimpleRetriever(storage)
	s.memory = memory.NewService(storage, extractor, retriever)

	// Start background extraction
	s.memory.StartBackgroundExtraction(c.Memory.Extraction.Interval)

	log.Printf("Memory service initialized (db=%s)", c.Memory.DBPath)
}

func (s *Server) initScheduler(c *config.ConfigData) {
	executor := scheduler.NewClaudeTaskExecutor(
		c.Claude.Path,
		c.Claude.WorkingDir,
	)

	s.scheduler = scheduler.NewSchedulerManager(c.Scheduler.Tasks, executor)
	s.scheduler.Start()

	log.Printf("[SCHEDULER] Initialized with %d tasks", len(c.Scheduler.Tasks))
}

func (s *Server) reloadConfig() {
	log.Println("[CONFIG] Reloading configuration")

	newCfg := s.config.Get()

	// Reload scheduler if config changed
	s.schedulerMu.Lock()

	if s.scheduler != nil {
		s.scheduler.Stop()
		s.scheduler = nil
	}

	s.schedulerMu.Unlock()

	if newCfg.Scheduler.Enabled {
		s.initScheduler(newCfg)
		log.Println("[CONFIG] Scheduler reloaded")
	}
}

func (s *Server) Close() {
	close(s.stopCh)

	s.schedulerMu.RLock()
	sched := s.scheduler
	s.schedulerMu.RUnlock()

	if sched != nil {
		sched.Stop()
	}

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
