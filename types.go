package main

import (
	"fmt"
	"sync"
	"time"
)

const (
	PermissionRegexSubmatchCount = 3
)

type PendingAction struct {
	ID          string   `json:"id"`
	Description string   `json:"description"`
	Commands    []string `json:"commands"`
}

type PendingPermission struct {
	ID            string `json:"id"`
	ToolPattern   string `json:"tool_pattern"`   // e.g., "mcp__todoist__*"
	ToolName      string `json:"tool_name"`      // e.g., "Todoist"
	OriginalQuery string `json:"original_query"` // Query that triggered this
	Description   string `json:"description"`    // Polish description
}

type UserContext struct {
	UserID     int64
	FirstName  string
	LastName   string
	Username   string
	ChatType   string
	ChatID     int64
	GroupMode  string
	IsTelegram bool // Indicates Telegram channel (requires emoji formatting)
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
	ID                  string
	LastActivity        time.Time
	PendingAction       *PendingAction
	PendingPermission   *PendingPermission // MCP/tool permission request
	UserContext         *UserContext
	ApprovedTools       map[string]bool // Session-scoped tool approvals (cleared on timeout)
	ApprovedPermissions map[string]bool // Session-scoped permission grants (cleared on timeout)
	mu                  sync.Mutex
	execMu              sync.Mutex // Prevents concurrent Claude CLI executions on same session
}
