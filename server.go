package main

import (
	"log"
	"sync"
	"time"
)

type Server struct {
	sessions       sync.Map // map[string]*Session
	stopCh         chan struct{}
	deepgramClient *DeepgramClient
}

func NewServer() *Server {
	s := &Server{
		stopCh: make(chan struct{}),
	}

	// Initialize Deepgram client if voice enabled
	if VoiceEnabled {
		client, err := initDeepgramClient()
		if err != nil {
			log.Printf("WARNING: Deepgram init failed: %v", err)
		} else {
			s.deepgramClient = client

			log.Printf(
				"Deepgram client initialized (model=%s, language=%s)",
				DeepgramModel,
				DeepgramLanguage,
			)
		}
	}

	go s.cleanupSessions()

	return s
}

func (s *Server) Close() {
	close(s.stopCh)
}

func (s *Server) cleanupSessions() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			now := time.Now()

			s.sessions.Range(func(key, value interface{}) bool {
				session, ok := value.(*Session)
				if !ok {
					return true
				}

				session.mu.Lock()
				expired := now.Sub(session.LastActivity) > SessionTimeout
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
