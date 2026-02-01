package main

import (
	"sync"
	"time"
)

const (
	SessionTimeout               = 5 * time.Minute
	PermissionRegexSubmatchCount = 3
	ClaudeExecutionTimeout       = 90 * time.Second
)

type PendingAction struct {
	ID          string   `json:"id"`
	Description string   `json:"description"`
	Commands    []string `json:"commands"`
}

type Session struct {
	ID                string
	LastActivity      time.Time
	PendingAction     *PendingAction
	ClaudeInitialized bool // Track if session created in Claude CLI
	mu                sync.Mutex
}
