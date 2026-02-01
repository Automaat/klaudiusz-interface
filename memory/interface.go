package memory

import (
	"context"
	"time"
)

// MemoryService is the main interface for memory operations
type MemoryService interface {
	// Remember stores a conversation turn
	Remember(ctx context.Context, turn ConversationTurn) error

	// Recall retrieves relevant context for a query
	Recall(ctx context.Context, query string, opts RecallOptions) (Context, error)

	// ExtractFacts extracts facts from recent conversations
	ExtractFacts(ctx context.Context) error

	// StartBackgroundExtraction starts periodic fact extraction
	StartBackgroundExtraction(interval time.Duration)

	// Close cleans up resources
	Close() error
}

// Storage abstracts the persistence layer
type Storage interface {
	SaveConversation(ctx context.Context, conv Conversation) error
	LoadConversations(ctx context.Context, filter Filter) ([]Conversation, error)
	SaveFact(ctx context.Context, fact Fact) error
	LoadFacts(ctx context.Context, filter Filter) ([]Fact, error)
	Close() error
}

// Extractor abstracts fact extraction (swappable LLM)
type Extractor interface {
	Extract(ctx context.Context, conversations []Conversation) ([]Fact, error)
}

// Retriever abstracts context assembly
type Retriever interface {
	GetRelevantFacts(ctx context.Context, query string, limit int) ([]Fact, error)
}
