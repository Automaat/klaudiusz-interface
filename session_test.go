package main

import (
	"testing"
	"time"
)

func TestGetOrCreateSessionWithContext(t *testing.T) {
	t.Parallel()

	t.Run("Create new session with context", func(t *testing.T) {
		t.Parallel()

		server := &Server{}
		userCtx := &UserContext{
			UserID:   123,
			Username: "testuser",
		}

		session := server.getOrCreateSessionWithContext("", userCtx)

		if session == nil {
			t.Fatal("Expected session, got nil")
		}

		if session.UserContext == nil {
			t.Fatal("UserContext should not be nil")
		}

		if session.UserContext.UserID != 123 {
			t.Errorf("UserID = %d, want 123", session.UserContext.UserID)
		}

		if session.UserContext.Username != "testuser" {
			t.Errorf("Username = %q, want %q", session.UserContext.Username, "testuser")
		}
	})

	t.Run("Create with specific ID and context", func(t *testing.T) {
		t.Parallel()

		server := &Server{}
		sessionID := "ctx-session-123"
		userCtx := &UserContext{
			UserID:   456,
			Username: "anotheruser",
		}

		session := server.getOrCreateSessionWithContext(sessionID, userCtx)

		if session.ID != sessionID {
			t.Errorf("Session ID = %q, want %q", session.ID, sessionID)
		}

		if session.UserContext.UserID != 456 {
			t.Errorf("UserID = %d, want 456", session.UserContext.UserID)
		}
	})

	t.Run("Update context on existing session", func(t *testing.T) {
		t.Parallel()

		server := &Server{}
		sessionID := "update-ctx-test"

		// Create with first context
		ctx1 := &UserContext{UserID: 100, Username: "user1"}
		session1 := server.getOrCreateSessionWithContext(sessionID, ctx1)

		// Update with new context
		ctx2 := &UserContext{UserID: 200, Username: "user2"}
		session2 := server.getOrCreateSessionWithContext(sessionID, ctx2)

		if session1 != session2 {
			t.Error("Should return same session instance")
		}

		// Context should be updated
		if session2.UserContext.UserID != 200 {
			t.Errorf("UserID = %d, want 200", session2.UserContext.UserID)
		}

		if session2.UserContext.Username != "user2" {
			t.Errorf("Username = %q, want %q", session2.UserContext.Username, "user2")
		}
	})

	t.Run("Initialize maps properly", func(t *testing.T) {
		t.Parallel()

		server := &Server{}
		userCtx := &UserContext{UserID: 789}

		session := server.getOrCreateSessionWithContext("", userCtx)

		if session.ApprovedTools == nil {
			t.Error("ApprovedTools should be initialized")
		}

		if session.ApprovedPermissions == nil {
			t.Error("ApprovedPermissions should be initialized")
		}

		// Should be able to write to maps without panic
		session.ApprovedTools["test"] = true
		session.ApprovedPermissions["test"] = true
	})

	t.Run("Handle type cast failure with context", func(t *testing.T) {
		t.Parallel()

		server := &Server{}
		sessionID := "ctx-cast-fail"
		userCtx := &UserContext{UserID: 999}

		// Store wrong type
		server.sessions.Store(sessionID, "wrong-type")

		// Should return new session without panicking
		session := server.getOrCreateSessionWithContext(sessionID, userCtx)

		if session == nil {
			t.Fatal("Expected session, got nil")
		}

		if session.ID != sessionID {
			t.Errorf("Session ID = %q, want %q", session.ID, sessionID)
		}

		if session.UserContext == nil {
			t.Fatal("UserContext should not be nil")
		}

		if session.UserContext.UserID != 999 {
			t.Errorf("UserID = %d, want 999", session.UserContext.UserID)
		}
	})

	t.Run("Update LastActivity with context", func(t *testing.T) {
		t.Parallel()

		server := &Server{}
		sessionID := "ctx-activity-test"
		userCtx := &UserContext{UserID: 111}

		// Create session
		session1 := server.getOrCreateSessionWithContext(sessionID, userCtx)
		firstActivity := session1.LastActivity

		// Wait a bit
		time.Sleep(10 * time.Millisecond)

		// Access again
		session2 := server.getOrCreateSessionWithContext(sessionID, userCtx)

		if !session2.LastActivity.After(firstActivity) {
			t.Error("LastActivity should be updated")
		}
	})
}
