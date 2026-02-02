package main

import (
	"fmt"
	"sync"
	"time"
)

const (
	SessionTimeout               = 5 * time.Minute
	PermissionRegexSubmatchCount = 3
	ClaudeExecutionTimeout       = 2 * time.Minute
)

type PendingAction struct {
	ID          string   `json:"id"`
	Description string   `json:"description"`
	Commands    []string `json:"commands"`
}

type UserContext struct {
	UserID    int64
	FirstName string
	LastName  string
	Username  string
	ChatType  string
	ChatID    int64
	GroupMode string
}

func (uc *UserContext) DisplayName() string {
	if uc.Username != "" {
		return "@" + uc.Username
	}

	name := uc.FirstName
	if uc.LastName != "" {
		name += " " + uc.LastName
	}

	if name == "" {
		return fmt.Sprintf("User %d", uc.UserID)
	}

	return name
}

func (uc *UserContext) IsGroupChat() bool {
	return uc.ChatType == "group" || uc.ChatType == "supergroup"
}

type Session struct {
	ID            string
	LastActivity  time.Time
	PendingAction *PendingAction
	UserContext   *UserContext
	mu            sync.Mutex
	execMu        sync.Mutex // Prevents concurrent Claude CLI executions on same session
}
