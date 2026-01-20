package providers

import (
	"context"

	"backend-server/internal/adapters/manager"
	"backend-server/internal/adapters/manager/types"
	"backend-server/internal/infrastructure/logger"
	"backend-server/internal/app/service"

	"github.com/cloudwego/eino/schema"
)

// ManagerAPIProvider delegates long-term memory operations to manager-server via manager-api service
type ManagerAPIProvider struct {
	service manager_api.ManagerAPIService
}

// NewManagerAPIProvider creates a provider backed by manager-server APIs
func NewManagerAPIProvider(svc manager_api.ManagerAPIService) (LongTermMemoryProvider, error) {
	if svc == nil {
		svc = service.DefaultRegistry().ManagerAPIService()
	}
	if svc == nil {
		logger.Warn("manager-api service is not initialized, using null long memory provider")
		return &NullProvider{}, nil
	}

	return &ManagerAPIProvider{service: svc}, nil
}

// GetName returns provider identifier
func (p *ManagerAPIProvider) GetName() string {
	return "manager_api"
}

// IsEnabled checks whether provider is usable
func (p *ManagerAPIProvider) IsEnabled() bool {
	return p.service != nil
}

// IsHealthy proxies health status from underlying service
func (p *ManagerAPIProvider) IsHealthy() bool {
	if p.service == nil {
		return false
	}
	return p.service.IsHealthy()
}

// Close releases resources (noop)
func (p *ManagerAPIProvider) Close() error {
	return nil
}

// StoreMessage stores a single message
func (p *ManagerAPIProvider) StoreMessage(ctx context.Context, deviceID string, role schema.RoleType, content string) error {
	return p.StoreBatch(ctx, deviceID, []schema.Message{{Role: role, Content: content}})
}

// StoreBatch stores a batch of messages
func (p *ManagerAPIProvider) StoreBatch(ctx context.Context, deviceID string, messages []schema.Message) error {
	if p.service == nil {
		return nil
	}
	if len(messages) == 0 {
		return nil
	}

	payload := make([]types.LongMemoryMessage, 0, len(messages))
	for _, msg := range messages {
		payload = append(payload, types.LongMemoryMessage{
			Role:    string(msg.Role),
			Content: msg.Content,
		})
	}

	return p.service.StoreLongTermMessages(ctx, deviceID, payload)
}

// GetUserProfile retrieves aggregated profile text
func (p *ManagerAPIProvider) GetUserProfile(ctx context.Context, deviceID string) (string, error) {
	if p.service == nil {
		return "", nil
	}
	return p.service.GetLongTermUserProfile(ctx, deviceID)
}

// UpdateUserProfile updates profile content
func (p *ManagerAPIProvider) UpdateUserProfile(ctx context.Context, deviceID, content, topic, subTopic string) error {
	if p.service == nil {
		return nil
	}

	req := &types.LongMemoryProfileRequest{
		Content:  content,
		Topic:    topic,
		SubTopic: subTopic,
	}
	return p.service.UpdateLongTermUserProfile(ctx, deviceID, req)
}

// GetLongTermContext retrieves long memory context
func (p *ManagerAPIProvider) GetLongTermContext(ctx context.Context, deviceID string, maxTokens int) (string, error) {
	if p.service == nil {
		return "", nil
	}
	return p.service.GetLongTermContext(ctx, deviceID, maxTokens)
}

// GetUserProfiles retrieves structured profiles
func (p *ManagerAPIProvider) GetUserProfiles(ctx context.Context, deviceID string, topics []string) ([]UserProfile, error) {
	if p.service == nil {
		return []UserProfile{}, nil
	}

	profiles, err := p.service.GetLongTermUserProfiles(ctx, deviceID, topics)
	if err != nil {
		return nil, err
	}

	result := make([]UserProfile, 0, len(profiles))
	for _, profile := range profiles {
		result = append(result, UserProfile{
			ID:         profile.ID,
			Content:    profile.Content,
			Topic:      profile.Topic,
			SubTopic:   profile.SubTopic,
			CreatedAt:  profile.CreatedAt,
			UpdatedAt:  profile.UpdatedAt,
			Attributes: profile.Attributes,
		})
	}

	return result, nil
}

// GetUserEvents retrieves user events
func (p *ManagerAPIProvider) GetUserEvents(ctx context.Context, deviceID string, limit int) ([]UserEvent, error) {
	if p.service == nil {
		return []UserEvent{}, nil
	}

	events, err := p.service.GetLongTermUserEvents(ctx, deviceID, limit)
	if err != nil {
		return nil, err
	}

	result := make([]UserEvent, 0, len(events))
	for _, event := range events {
		result = append(result, UserEvent{
			ID:         event.ID,
			CreatedAt:  event.CreatedAt,
			Timestamp:  event.Timestamp,
			Content:    event.Content,
			EventData:  event.EventData,
			Similarity: event.Similarity,
		})
	}

	return result, nil
}
