package memory

import (
	"context"
	"sort"
	"strings"

	"github.com/cockroachdb/errors"
)

const maxFactsToLoad = 1000

// SimpleRetriever implements Retriever using keyword matching
type SimpleRetriever struct {
	storage Storage
}

// NewSimpleRetriever creates a new retriever
func NewSimpleRetriever(storage Storage) *SimpleRetriever {
	return &SimpleRetriever{
		storage: storage,
	}
}

// GetRelevantFacts retrieves facts relevant to the query
func (r *SimpleRetriever) GetRelevantFacts(
	ctx context.Context,
	query string,
	limit int,
) ([]Fact, error) {
	// Load all facts
	allFacts, err := r.storage.LoadFacts(ctx, Filter{Limit: maxFactsToLoad})
	if err != nil {
		return nil, errors.Wrap(err, "load facts")
	}

	if len(allFacts) == 0 {
		return nil, nil
	}

	// Score facts by relevance
	scored := r.scoreFactsByRelevance(query, allFacts)

	// Return top N
	if limit > 0 && limit < len(scored) {
		return scored[:limit], nil
	}

	return scored, nil
}

// scoredFact wraps Fact with relevance score
type scoredFact struct {
	fact  Fact
	score float64
}

// scoreFactsByRelevance scores facts based on keyword overlap with query
func (r *SimpleRetriever) scoreFactsByRelevance(query string, facts []Fact) []Fact {
	queryWords := r.tokenize(query)

	scored := make([]scoredFact, 0, len(facts))
	for _, fact := range facts {
		factWords := r.tokenize(fact.Text)
		overlap := r.calculateOverlap(queryWords, factWords)

		// Weight by confidence
		score := overlap * fact.Confidence

		scored = append(scored, scoredFact{
			fact:  fact,
			score: score,
		})
	}

	// Sort by score descending
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	// Extract facts
	result := make([]Fact, 0, len(scored))
	for _, sf := range scored {
		if sf.score > 0 { // Only include facts with non-zero score
			result = append(result, sf.fact)
		}
	}

	return result
}

// tokenize splits text into lowercase words
func (*SimpleRetriever) tokenize(text string) []string {
	text = strings.ToLower(text)

	words := strings.FieldsFunc(text, func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9') &&
			r != 'ą' && r != 'ć' && r != 'ę' && r != 'ł' && r != 'ń' &&
			r != 'ó' && r != 'ś' && r != 'ź' && r != 'ż'
	})

	return words
}

// calculateOverlap computes Jaccard similarity between word sets
func (*SimpleRetriever) calculateOverlap(words1, words2 []string) float64 {
	if len(words1) == 0 || len(words2) == 0 {
		return 0.0
	}

	set1 := make(map[string]bool)
	for _, w := range words1 {
		set1[w] = true
	}

	set2 := make(map[string]bool)
	for _, w := range words2 {
		set2[w] = true
	}

	// Count intersection
	intersection := 0

	for w := range set1 {
		if set2[w] {
			intersection++
		}
	}

	// Count union
	union := len(set1)
	for w := range set2 {
		if !set1[w] {
			union++
		}
	}

	if union == 0 {
		return 0.0
	}

	return float64(intersection) / float64(union)
}
