package service

import (
	"context"
	"sync"
	"time"

	"manager-server/internal/models"
)

// IngestionQuotaConfig defines rate limits for ingestion operations.
type IngestionQuotaConfig struct {
	MaxDocumentsPerDay int
	MaxBytesPerDay     int64
	MaxConcurrentJobs  int
}

// IngestionRetryConfig represents retry policy thresholds.
type IngestionRetryConfig struct {
	MaxAttempts    int
	BackoffSeconds []int
}

// ConnectorFieldOption represents a select choice in connector forms.
type ConnectorFieldOption struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// ConnectorFieldDefinition describes a dynamic field rendered in the UI.
type ConnectorFieldDefinition struct {
	Key         string                 `json:"key"`
	Label       string                 `json:"label"`
	Type        string                 `json:"type"`
	Required    bool                   `json:"required"`
	Placeholder string                 `json:"placeholder,omitempty"`
	Help        string                 `json:"help,omitempty"`
	Default     any                    `json:"default,omitempty"`
	Options     []ConnectorFieldOption `json:"options,omitempty"`
	Advanced    bool                   `json:"advanced,omitempty"`
}

// ConnectorDefinition exposes connector configuration to the frontend.
type ConnectorDefinition struct {
	Name             string                     `json:"name"`
	DisplayName      string                     `json:"displayName"`
	Description      string                     `json:"description,omitempty"`
	Categories       []string                   `json:"categories,omitempty"`
	Icon             string                     `json:"icon,omitempty"`
	SupportsOAuth    bool                       `json:"supportsOAuth,omitempty"`
	OAuthProvider    string                     `json:"oauthProvider,omitempty"`
	SupportsWebhook  bool                       `json:"supportsWebhook,omitempty"`
	DefaultParams    map[string]any             `json:"defaultParams,omitempty"`
	DefaultMetadata  map[string]any             `json:"defaultMetadata,omitempty"`
	ParamsSchema     []ConnectorFieldDefinition `json:"paramsSchema,omitempty"`
	CredentialSchema []ConnectorFieldDefinition `json:"credentialSchema,omitempty"`
	MetadataSchema   []ConnectorFieldDefinition `json:"metadataSchema,omitempty"`
}

// IngestionQuotaStatus reports current quota usage.
type IngestionQuotaStatus struct {
	MaxDocumentsPerDay int   `json:"maxDocumentsPerDay"`
	DocumentsUsedToday int64 `json:"documentsUsedToday"`
	MaxBytesPerDay     int64 `json:"maxBytesPerDay"`
	BytesUsedToday     int64 `json:"bytesUsedToday"`
	MaxConcurrentJobs  int   `json:"maxConcurrentJobs"`
	ActiveJobs         int64 `json:"activeJobs"`
}

// IngestionRetryStatus echoes retry policy to the frontend.
type IngestionRetryStatus struct {
	MaxAttempts    int   `json:"maxAttempts"`
	BackoffSeconds []int `json:"backoffSeconds"`
}

// IngestionPolicy groups quota and retry configuration.
type IngestionPolicy struct {
	Quota IngestionQuotaStatus `json:"quota"`
	Retry IngestionRetryStatus `json:"retry"`
}

// IngestionOverview aggregates connector definitions with policy information.
type IngestionOverview struct {
	Connectors []ConnectorDefinition `json:"connectors"`
	Policy     IngestionPolicy       `json:"policy"`
}

// KBJobEvent conveys real-time job updates to SSE subscribers.
type KBJobEvent struct {
	ID              uint64     `json:"id"`
	KnowledgeBaseID uint64     `json:"knowledgeBaseId"`
	DocumentID      *uint64    `json:"documentId,omitempty"`
	JobType         string     `json:"jobType"`
	Status          string     `json:"status"`
	Progress        int        `json:"progress"`
	RetryCount      int        `json:"retryCount"`
	MaxRetries      int        `json:"maxRetries"`
	ErrorType       string     `json:"errorType,omitempty"`
	ErrorMessage    string     `json:"errorMessage,omitempty"`
	Connector       string     `json:"connector,omitempty"`
	StartedAt       *time.Time `json:"startedAt,omitempty"`
	CompletedAt     *time.Time `json:"completedAt,omitempty"`
	NextRetryAt     *time.Time `json:"nextRetryAt,omitempty"`
	UpdatedAt       time.Time  `json:"updatedAt"`
}

// KBJobWebhookLog represents stored webhook callbacks for a job.
type KBJobWebhookLog struct {
	ID              uint64         `json:"id"`
	JobID           uint64         `json:"jobId"`
	KnowledgeBaseID uint64         `json:"knowledgeBaseId"`
	Status          string         `json:"status"`
	Progress        *int           `json:"progress,omitempty"`
	ErrorMessage    string         `json:"errorMessage,omitempty"`
	ErrorType       string         `json:"errorType,omitempty"`
	Payload         map[string]any `json:"payload,omitempty"`
	RemoteAddr      string         `json:"remoteAddr"`
	ReceivedAt      time.Time      `json:"receivedAt"`
}

// KBJobEventHub fan-outs job updates to SSE listeners while implementing KBEventSink.
type KBJobEventHub struct {
	mu        sync.RWMutex
	listeners map[int]*jobEventListener
	nextID    int
}

type jobEventListener struct {
	kbID uint64
	ch   chan KBJobEvent
}

// JobEventSubscription exposes a read-only channel with a cancellation hook.
type JobEventSubscription struct {
	Events <-chan KBJobEvent
	cancel func()
	once   sync.Once
}

// NewJobEventSubscription creates a subscription wrapper.
func NewJobEventSubscription(ch <-chan KBJobEvent, cancel func()) *JobEventSubscription {
	return &JobEventSubscription{
		Events: ch,
		cancel: cancel,
	}
}

// Close releases the subscription.
func (s *JobEventSubscription) Close() {
	s.once.Do(func() {
		if s.cancel != nil {
			s.cancel()
		}
	})
}

// NewKBJobEventHub constructs a hub ready for subscriptions.
func NewKBJobEventHub() *KBJobEventHub {
	return &KBJobEventHub{
		listeners: make(map[int]*jobEventListener),
	}
}

// Subscribe attaches a listener for the given knowledge base id (0 for all).
func (h *KBJobEventHub) Subscribe(kbID uint64) *JobEventSubscription {
	if h == nil {
		return nil
	}
	ch := make(chan KBJobEvent, 32)
	h.mu.Lock()
	id := h.nextID
	h.nextID++
	h.listeners[id] = &jobEventListener{
		kbID: kbID,
		ch:   ch,
	}
	h.mu.Unlock()

	cancel := func() {
		h.mu.Lock()
		if listener, ok := h.listeners[id]; ok {
			delete(h.listeners, id)
			close(listener.ch)
		}
		h.mu.Unlock()
	}

	return NewJobEventSubscription(ch, cancel)
}

// AgentsCapabilitiesChanged satisfies KBEventSink but is not used for SSE.
func (h *KBJobEventHub) AgentsCapabilitiesChanged(context.Context, []uint64) {}

// JobStatusChanged broadcasts job updates to all interested listeners.
func (h *KBJobEventHub) JobStatusChanged(_ context.Context, job *models.KBJob) {
	if h == nil || job == nil {
		return
	}
	event := KBJobEvent{
		ID:              job.ID,
		KnowledgeBaseID: job.KnowledgeBaseID,
		DocumentID:      job.DocumentID,
		JobType:         job.JobType,
		Status:          job.Status,
		Progress:        job.Progress,
		RetryCount:      job.RetryCount,
		MaxRetries:      job.MaxRetries,
		ErrorType:       job.ErrorType,
		ErrorMessage:    job.ErrorMessage,
		Connector:       job.Connector,
		StartedAt:       job.StartedAt,
		CompletedAt:     job.CompletedAt,
		NextRetryAt:     job.NextRetryAt,
		UpdatedAt:       time.Now(),
	}

	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, listener := range h.listeners {
		if listener == nil {
			continue
		}
		if listener.kbID != 0 && listener.kbID != job.KnowledgeBaseID {
			continue
		}
		select {
		case listener.ch <- event:
		default:
			// drop event if the consumer is slow to avoid blocking the runner
		}
	}
}

// KBEventFanout multiplexes KB events to multiple sinks.
type KBEventFanout struct {
	sinks []KBEventSink
}

// NewKBEventFanout constructs a fan-out sink, filtering nils.
func NewKBEventFanout(sinks ...KBEventSink) KBEventSink {
	filtered := make([]KBEventSink, 0, len(sinks))
	for _, sink := range sinks {
		if sink != nil {
			filtered = append(filtered, sink)
		}
	}
	return &KBEventFanout{sinks: filtered}
}

func (f *KBEventFanout) AgentsCapabilitiesChanged(ctx context.Context, agentIDs []uint64) {
	if f == nil {
		return
	}
	for _, sink := range f.sinks {
		sink.AgentsCapabilitiesChanged(ctx, agentIDs)
	}
}

func (f *KBEventFanout) JobStatusChanged(ctx context.Context, job *models.KBJob) {
	if f == nil {
		return
	}
	for _, sink := range f.sinks {
		sink.JobStatusChanged(ctx, job)
	}
}
