package speaker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	chromem "github.com/philippgille/chromem-go"
)

// Store abstracts embedding persistence/search.
type Store interface {
	Insert(ctx context.Context, embedding []float32, meta map[string]string) (string, error)
	Search(ctx context.Context, embedding []float32, topK int, where map[string]string) ([]SearchResult, error)
	DeleteSpeaker(ctx context.Context, speakerID string) error
	List() []SpeakerInfo
	Stats() Stats
	Close() error
}

type SearchResult struct {
	SpeakerID   string
	SpeakerName string
	Similarity  float32
}

// NewStore constructs a store based on config.
func NewStore(cfg VectorDBConfig) (Store, error) {
	provider := cfg.Provider
	if provider == "" {
		provider = "chromem"
	}
	switch provider {
	case "chromem":
		return newChromemStore(cfg.Chromem)
	case "memory":
		return newMemoryStore(), nil
	default:
		return nil, fmt.Errorf("unsupported speaker vector db provider %q", provider)
	}
}

// metadata persisted alongside embeddings for list/stats/delete.
type speakerMeta struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	SampleCount int       `json:"sample_count"`
	DocIDs      []string  `json:"doc_ids"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type chromemStore struct {
	db       *chromem.DB
	coll     *chromem.Collection
	metaPath string

	mu   sync.RWMutex
	meta map[string]*speakerMeta
}

func newChromemStore(cfg ChromemConfig) (*chromemStore, error) {
	path := cfg.Path
	if strings.TrimSpace(path) == "" {
		path = "./data/speaker_index"
	}
	db, err := chromem.NewPersistentDB(path, cfg.Compress)
	if err != nil {
		return nil, fmt.Errorf("init chromem: %w", err)
	}
	embed := func(ctx context.Context, text string) ([]float32, error) {
		return nil, errors.New("embedding by text not supported")
	}
	coll, err := db.GetOrCreateCollection("speaker", nil, embed)
	if err != nil {
		return nil, fmt.Errorf("create collection: %w", err)
	}
	store := &chromemStore{
		db:       db,
		coll:     coll,
		metaPath: filepath.Join(path, "metadata.json"),
		meta:     make(map[string]*speakerMeta),
	}
	if err := store.loadMeta(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *chromemStore) loadMeta() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.metaPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read metadata: %w", err)
	}
	var items []*speakerMeta
	if err := json.Unmarshal(data, &items); err != nil {
		return fmt.Errorf("decode metadata: %w", err)
	}
	for _, item := range items {
		if item == nil || item.ID == "" {
			continue
		}
		s.meta[item.ID] = item
	}
	return nil
}

func (s *chromemStore) persistMetaLocked() {
	items := make([]*speakerMeta, 0, len(s.meta))
	for _, m := range s.meta {
		items = append(items, m)
	}
	data, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(s.metaPath), 0o755)
	_ = os.WriteFile(s.metaPath, data, 0o644)
}

func (s *chromemStore) Insert(ctx context.Context, embedding []float32, meta map[string]string) (string, error) {
	if s.coll == nil {
		return "", errors.New("collection not initialized")
	}
	speakerID := meta["speaker_id"]
	speakerName := meta["speaker_name"]
	if speakerID == "" {
		return "", errors.New("speaker_id missing in metadata")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.meta[speakerID]
	if !ok {
		entry = &speakerMeta{
			ID:          speakerID,
			Name:        speakerName,
			SampleCount: 0,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		}
		s.meta[speakerID] = entry
	}
	docID := fmt.Sprintf("%s-%d-%s", speakerID, entry.SampleCount, uuid.NewString())
	entry.SampleCount++
	entry.Name = speakerName
	entry.DocIDs = append(entry.DocIDs, docID)
	entry.UpdatedAt = time.Now()

	metadata := map[string]string{}
	for k, v := range meta {
		if v != "" {
			metadata[k] = v
		}
	}

	doc := chromem.Document{
		ID:        docID,
		Metadata:  metadata,
		Embedding: embedding,
	}
	if err := s.coll.AddDocuments(ctx, []chromem.Document{doc}, 1); err != nil {
		// rollback count on failure
		entry.SampleCount--
		entry.DocIDs = entry.DocIDs[:len(entry.DocIDs)-1]
		return "", fmt.Errorf("add document: %w", err)
	}
	s.persistMetaLocked()
	return docID, nil
}

func (s *chromemStore) Search(ctx context.Context, embedding []float32, topK int, where map[string]string) ([]SearchResult, error) {
	if s.coll == nil {
		return nil, errors.New("collection not initialized")
	}
	if topK <= 0 {
		topK = 1
	}
	count := s.coll.Count()
	if count == 0 {
		return nil, nil
	}
	if topK > count {
		topK = count
	}
	results, err := s.coll.QueryEmbedding(ctx, embedding, topK, where, nil)
	if err != nil {
		return nil, err
	}
	out := make([]SearchResult, 0, len(results))
	for _, r := range results {
		out = append(out, SearchResult{
			SpeakerID:   r.Metadata["speaker_id"],
			SpeakerName: r.Metadata["speaker_name"],
			Similarity:  r.Similarity,
		})
	}
	return out, nil
}

func (s *chromemStore) DeleteSpeaker(ctx context.Context, speakerID string) error {
	if speakerID == "" {
		return errors.New("speaker_id required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.meta[speakerID]
	if !ok {
		return nil
	}
	if len(entry.DocIDs) > 0 && s.coll != nil {
		if err := s.coll.Delete(ctx, nil, nil, entry.DocIDs...); err != nil {
			return err
		}
	}
	delete(s.meta, speakerID)
	s.persistMetaLocked()
	return nil
}

func (s *chromemStore) List() []SpeakerInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]SpeakerInfo, 0, len(s.meta))
	for _, m := range s.meta {
		items = append(items, SpeakerInfo{
			ID:          m.ID,
			Name:        m.Name,
			SampleCount: m.SampleCount,
			CreatedAt:   m.CreatedAt,
			UpdatedAt:   m.UpdatedAt,
		})
	}
	return items
}

func (s *chromemStore) Stats() Stats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st := Stats{}
	for _, m := range s.meta {
		st.TotalSpeakers++
		st.TotalSamples += m.SampleCount
	}
	return st
}

func (s *chromemStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.persistMetaLocked()
	return nil
}

type memoryStore struct {
	mu   sync.RWMutex
	meta map[string]*speakerMeta
}

func newMemoryStore() *memoryStore {
	return &memoryStore{meta: make(map[string]*speakerMeta)}
}

func (m *memoryStore) Insert(_ context.Context, _ []float32, meta map[string]string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id := meta["speaker_id"]
	name := meta["speaker_name"]
	entry, ok := m.meta[id]
	if !ok {
		entry = &speakerMeta{
			ID:        id,
			Name:      name,
			CreatedAt: time.Now(),
		}
		m.meta[id] = entry
	}
	entry.SampleCount++
	entry.UpdatedAt = time.Now()
	docID := fmt.Sprintf("%s-%d-%s", id, entry.SampleCount, uuid.NewString())
	entry.DocIDs = append(entry.DocIDs, docID)
	return docID, nil
}

func (m *memoryStore) Search(_ context.Context, _ []float32, _ int, _ map[string]string) ([]SearchResult, error) {
	return nil, nil
}

func (m *memoryStore) DeleteSpeaker(_ context.Context, speakerID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.meta, speakerID)
	return nil
}

func (m *memoryStore) List() []SpeakerInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	items := make([]SpeakerInfo, 0, len(m.meta))
	for _, s := range m.meta {
		items = append(items, SpeakerInfo{
			ID:          s.ID,
			Name:        s.Name,
			SampleCount: s.SampleCount,
			CreatedAt:   s.CreatedAt,
			UpdatedAt:   s.UpdatedAt,
		})
	}
	return items
}

func (m *memoryStore) Stats() Stats {
	m.mu.RLock()
	defer m.mu.RUnlock()
	st := Stats{}
	for _, s := range m.meta {
		st.TotalSpeakers++
		st.TotalSamples += s.SampleCount
	}
	return st
}

func (m *memoryStore) Close() error { return nil }
