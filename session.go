package main

import (
	"log"
	"time"

	"github.com/google/uuid"
)

func (s *Server) getOrCreateSession(sessionID string) *Session {
	if sessionID == "" {
		sessionID = uuid.New().String()
	}

	val, _ := s.sessions.LoadOrStore(sessionID, &Session{
		ID:           sessionID,
		LastActivity: time.Now(),
	})

	session, ok := val.(*Session)
	if !ok {
		log.Printf("Failed to cast session for ID: %s", sessionID)

		return &Session{
			ID:           sessionID,
			LastActivity: time.Now(),
		}
	}

	session.mu.Lock()
	session.LastActivity = time.Now()
	session.mu.Unlock()

	return session
}

func (s *Server) getOrCreateSessionWithContext(sessionID string, userCtx *UserContext) *Session {
	if sessionID == "" {
		sessionID = uuid.New().String()
	}

	val, _ := s.sessions.LoadOrStore(sessionID, &Session{
		ID:           sessionID,
		LastActivity: time.Now(),
		UserContext:  userCtx,
	})

	session, ok := val.(*Session)
	if !ok {
		log.Printf("Failed to cast session for ID: %s", sessionID)

		return &Session{
			ID:           sessionID,
			LastActivity: time.Now(),
			UserContext:  userCtx,
		}
	}

	session.mu.Lock()
	session.LastActivity = time.Now()
	session.UserContext = userCtx
	session.mu.Unlock()

	return session
}
