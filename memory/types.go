package memory

import "time"

// ConversationTurn represents a single Q&A exchange
type ConversationTurn struct {
	SessionID string
	Timestamp time.Time
	Query     string
	Response  string
}

// Conversation represents a stored conversation in the database
type Conversation struct {
	ID        int64
	SessionID string
	Timestamp time.Time
	Query     string
	Response  string
	Metadata  map[string]string
}

// FactCategory classifies different types of extracted facts
type FactCategory string

const (
	CategoryPreference FactCategory = "preference" // coffee, temperature
	CategoryHAPattern  FactCategory = "ha_pattern" // automation behaviors
	CategoryKnowledge  FactCategory = "knowledge"  // general facts
	CategorySchedule   FactCategory = "schedule"   // time-based
)

// Fact represents an extracted piece of user information
type Fact struct {
	ID         int64
	Category   FactCategory
	Text       string
	Confidence float64 // 0.0-1.0
	SourceIDs  []int64 // conversation IDs
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// Context represents memory context to inject into prompts
type Context struct {
	Facts         []Fact
	Conversations []Conversation
}

// RecallOptions configures what to retrieve
type RecallOptions struct {
	IncludeFacts         bool
	IncludeConversations bool
	FactLimit            int
	ConversationLimit    int
}

// Filter configures database queries
type Filter struct {
	SessionID  string
	Since      time.Time
	Categories []FactCategory
	Limit      int
}
