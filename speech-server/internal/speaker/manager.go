package speaker

import (
	"context"
	"errors"
	"strings"
	"time"
)

// Manager orchestrates embedding and storage.
type Manager struct {
	cfg      Config
	embedder *Embedder
	store    Store
}

func NewManager(cfg Config) (*Manager, error) {
	store, err := NewStore(cfg.VectorDB)
	if err != nil {
		return nil, err
	}
	embedder, err := NewEmbedder(EmbedderConfig{
		ModelPath:       cfg.ModelPath,
		SampleRate:      cfg.SampleRate,
		NumThreads:      cfg.NumThreads,
		Provider:        cfg.Provider,
		EnergyThreshold: cfg.EnergyThreshold,
		MinDuration:     cfg.MinDuration,
	})
	if err != nil {
		store.Close()
		return nil, err
	}
	return &Manager{
		cfg:      cfg,
		embedder: embedder,
		store:    store,
	}, nil
}

func (m *Manager) Close() {
	if m.embedder != nil {
		m.embedder.Close()
	}
	if m.store != nil {
		_ = m.store.Close()
	}
}

func (m *Manager) Register(ctx context.Context, in RegisterInput) (SpeakerInfo, error) {
	if m == nil || m.embedder == nil || m.store == nil {
		return SpeakerInfo{}, errors.New("speaker manager not initialized")
	}
	if strings.TrimSpace(in.SpeakerID) == "" {
		return SpeakerInfo{}, errors.New("speaker_id is required")
	}
	if strings.TrimSpace(in.SpeakerName) == "" {
		return SpeakerInfo{}, errors.New("speaker_name is required")
	}

	embedding, err := m.embedder.Extract(in.Samples, in.SampleRate)
	if err != nil {
		return SpeakerInfo{}, err
	}

	meta := map[string]string{
		"uid":          in.UID,
		"agent_id":     in.AgentID,
		"speaker_id":   in.SpeakerID,
		"speaker_name": in.SpeakerName,
	}
	if _, err := m.store.Insert(ctx, embedding, meta); err != nil {
		return SpeakerInfo{}, err
	}
	info := SpeakerInfo{
		ID:          in.SpeakerID,
		Name:        in.SpeakerName,
		SampleCount: 0,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	// use store list to return accurate data
	for _, s := range m.store.List() {
		if s.ID == in.SpeakerID {
			info = s
			break
		}
	}
	return info, nil
}

func (m *Manager) Identify(ctx context.Context, in IdentifyInput) (*IdentifyResult, error) {
	if m == nil || m.embedder == nil || m.store == nil {
		return nil, errors.New("speaker manager not initialized")
	}
	embedding, err := m.embedder.Extract(in.Samples, in.SampleRate)
	if err != nil {
		return nil, err
	}
	where := map[string]string{}
	if in.UID != "" {
		where["uid"] = in.UID
	}
	if in.AgentID != "" {
		where["agent_id"] = in.AgentID
	}
	if strings.TrimSpace(in.SpeakerID) != "" {
		where["speaker_id"] = strings.TrimSpace(in.SpeakerID)
	}
	if strings.TrimSpace(in.SpeakerName) != "" {
		where["speaker_name"] = strings.TrimSpace(in.SpeakerName)
	}
	topK := in.TopK
	if topK <= 0 {
		topK = 1
	}
	results, err := m.store.Search(ctx, embedding, topK, where)
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, nil
	}
	threshold := m.cfg.Threshold
	if in.Threshold > 0 {
		threshold = in.Threshold
	}
	if results[0].Similarity < threshold {
		return nil, nil
	}
	return &IdentifyResult{
		SpeakerID:   results[0].SpeakerID,
		SpeakerName: results[0].SpeakerName,
		Confidence:  results[0].Similarity,
	}, nil
}

func (m *Manager) Verify(ctx context.Context, in VerifyInput) (*VerifyResult, error) {
	if strings.TrimSpace(in.SpeakerID) == "" {
		return nil, errors.New("speaker_id is required")
	}
	res, err := m.Identify(ctx, IdentifyInput{
		UID:        in.UID,
		AgentID:    in.AgentID,
		SpeakerID:  in.SpeakerID,
		Samples:    in.Samples,
		SampleRate: in.SampleRate,
		Threshold:  in.Threshold,
		TopK:       1,
	})
	if err != nil {
		return nil, err
	}
	if res == nil {
		return &VerifyResult{SpeakerID: in.SpeakerID, Verified: false, Confidence: 0}, nil
	}
	return &VerifyResult{
		SpeakerID:   res.SpeakerID,
		SpeakerName: res.SpeakerName,
		Confidence:  res.Confidence,
		Verified:    true,
	}, nil
}

func (m *Manager) DeleteSpeaker(ctx context.Context, speakerID string) error {
	if m == nil || m.store == nil {
		return errors.New("speaker manager not initialized")
	}
	return m.store.DeleteSpeaker(ctx, speakerID)
}

func (m *Manager) List() []SpeakerInfo {
	if m == nil || m.store == nil {
		return nil
	}
	return m.store.List()
}

func (m *Manager) Stats() Stats {
	if m == nil || m.store == nil {
		return Stats{}
	}
	return m.store.Stats()
}
