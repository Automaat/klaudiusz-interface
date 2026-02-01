package memory

import (
	"context"
	"log"
	"time"

	"github.com/cockroachdb/errors"
)

const (
	maxConversationsToExtract = 20
	extractionTimeout         = 2 * time.Minute
)

// Service implements MemoryService interface
type Service struct {
	storage   Storage
	extractor Extractor
	retriever Retriever

	extractionTicker *time.Ticker
	stopCh           chan struct{}
}

// NewService creates a new memory service
func NewService(storage Storage, extractor Extractor, retriever Retriever) *Service {
	return &Service{
		storage:   storage,
		extractor: extractor,
		retriever: retriever,
		stopCh:    make(chan struct{}),
	}
}

// Remember stores a conversation turn
func (s *Service) Remember(ctx context.Context, turn ConversationTurn) error {
	conv := Conversation{
		SessionID: turn.SessionID,
		Timestamp: turn.Timestamp,
		Query:     turn.Query,
		Response:  turn.Response,
	}

	return errors.Wrap(s.storage.SaveConversation(ctx, conv), "save conversation")
}

// Recall retrieves relevant context for a query
func (s *Service) Recall(ctx context.Context, query string, opts RecallOptions) (Context, error) {
	var result Context

	if opts.IncludeFacts {
		facts, err := s.retriever.GetRelevantFacts(ctx, query, opts.FactLimit)
		if err != nil {
			return result, errors.Wrap(err, "retrieve facts")
		}

		result.Facts = facts
	}

	if opts.IncludeConversations {
		convs, err := s.storage.LoadConversations(ctx, Filter{
			Limit: opts.ConversationLimit,
		})
		if err != nil {
			return result, errors.Wrap(err, "load conversations")
		}

		result.Conversations = convs
	}

	return result, nil
}

// ExtractFacts extracts facts from recent conversations
func (s *Service) ExtractFacts(ctx context.Context) error {
	// Load unprocessed conversations (last hour, max 20)
	convs, err := s.storage.LoadConversations(ctx, Filter{
		Since: time.Now().Add(-1 * time.Hour),
		Limit: maxConversationsToExtract,
	})
	if err != nil {
		return errors.Wrap(err, "load conversations")
	}

	if len(convs) == 0 {
		return nil // nothing to extract
	}

	// Extract facts
	facts, err := s.extractor.Extract(ctx, convs)
	if err != nil {
		return errors.Wrap(err, "extract facts")
	}

	// Save facts
	for _, fact := range facts {
		if err := s.storage.SaveFact(ctx, fact); err != nil {
			return errors.Wrapf(err, "save fact: %s", fact.Text)
		}
	}

	log.Printf("Extracted %d facts from %d conversations", len(facts), len(convs))

	return nil
}

// StartBackgroundExtraction starts periodic fact extraction
func (s *Service) StartBackgroundExtraction(interval time.Duration) {
	s.extractionTicker = time.NewTicker(interval)

	go func() {
		for {
			select {
			case <-s.extractionTicker.C:
				ctx, cancel := context.WithTimeout(context.Background(), extractionTimeout)
				if err := s.ExtractFacts(ctx); err != nil {
					log.Printf("Background extraction error: %v", err)
				}

				cancel()
			case <-s.stopCh:
				return
			}
		}
	}()
}

// Close stops background tasks and closes storage
func (s *Service) Close() error {
	if s.extractionTicker != nil {
		s.extractionTicker.Stop()
	}

	close(s.stopCh)

	return errors.Wrap(s.storage.Close(), "close storage")
}
