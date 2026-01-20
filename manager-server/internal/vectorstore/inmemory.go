package vectorstore

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"sync"

	"manager-server/internal/models"
)

type storedChunk struct {
	chunk *models.KBChunk
	text  string
}

// InMemoryAdapter provides a lightweight vector store suitable for tests and local usage.
type InMemoryAdapter struct {
	mu    sync.RWMutex
	store map[uint64]map[uint64]storedChunk
}

// NewInMemoryAdapter constructs an adapter backed by memory.
func NewInMemoryAdapter() *InMemoryAdapter {
	return &InMemoryAdapter{
		store: make(map[uint64]map[uint64]storedChunk),
	}
}

// IndexChunks stores chunk payloads.
func (a *InMemoryAdapter) IndexChunks(ctx context.Context, knowledgeBaseID uint64, chunks []*models.KBChunk) error {
	if len(chunks) == 0 {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()

	kbMap, ok := a.store[knowledgeBaseID]
	if !ok {
		kbMap = make(map[uint64]storedChunk)
		a.store[knowledgeBaseID] = kbMap
	}
	for _, chunk := range chunks {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		text := chunk.Content
		kbMap[chunk.ID] = storedChunk{
			chunk: chunk,
			text:  text,
		}
	}
	return nil
}

// DeleteChunks removes indexed entries.
func (a *InMemoryAdapter) DeleteChunks(ctx context.Context, knowledgeBaseID uint64, chunkIDs []uint64) error {
	if len(chunkIDs) == 0 {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	kbMap, ok := a.store[knowledgeBaseID]
	if !ok {
		return nil
	}
	for _, id := range chunkIDs {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		delete(kbMap, id)
	}
	return nil
}

// Query returns chunks using a naive scoring heuristic.
func (a *InMemoryAdapter) Query(ctx context.Context, knowledgeBaseID uint64, query string, topK int) ([]SearchResult, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	kbMap, ok := a.store[knowledgeBaseID]
	if !ok || len(kbMap) == 0 {
		return nil, nil
	}
	if topK <= 0 {
		topK = 5
	}
	query = strings.ToLower(strings.TrimSpace(query))

	results := make([]SearchResult, 0, len(kbMap))
	for id, entry := range kbMap {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		score := 0.0
		textLower := strings.ToLower(entry.text)
		if strings.Contains(textLower, query) {
			score = 1.0
		} else if query != "" {
			// basic token overlap
			qTokens := strings.Fields(query)
			matches := 0
			for _, token := range qTokens {
				if strings.Contains(textLower, token) {
					matches++
				}
			}
			if matches > 0 {
				score = float64(matches) / float64(len(qTokens))
			}
		}
		var meta map[string]any
		if len(entry.chunk.Metadata) > 0 {
			if err := json.Unmarshal(entry.chunk.Metadata, &meta); err != nil {
				meta = nil
			}
		}

		results = append(results, SearchResult{
			ChunkID: id,
			Score:   score,
			Content: entry.text,
			Meta:    meta,
			Chunk:   entry.chunk,
		})
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].Score == results[j].Score {
			return results[i].ChunkID < results[j].ChunkID
		}
		return results[i].Score > results[j].Score
	})
	if len(results) > topK {
		results = results[:topK]
	}
	return results, nil
}
