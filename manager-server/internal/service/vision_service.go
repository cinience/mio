package service

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"manager-server/internal/models"
	"manager-server/internal/repository"
	"manager-server/internal/security"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// VisionService handles vision sources, rules, and events.
type VisionService interface {
	CreateSource(ctx context.Context, agentID string, req *models.VisionSourceCreateDTO) (*models.AgentVisionSource, error)
	UpdateSource(ctx context.Context, sourceID string, req *models.VisionSourceUpdateDTO) (*models.AgentVisionSource, error)
	DeleteSource(ctx context.Context, sourceID string) error
	ListSources(ctx context.Context, agentID string) ([]*models.AgentVisionSource, error)
	ListSourcesForSync(ctx context.Context, agentID string) ([]*models.AgentVisionSource, error)

	CreateRule(ctx context.Context, agentID string, req *models.VisionRuleCreateDTO) (*models.AgentVisionRule, error)
	UpdateRule(ctx context.Context, ruleID string, req *models.VisionRuleUpdateDTO) (*models.AgentVisionRule, error)
	DeleteRule(ctx context.Context, ruleID string) error
	ListRules(ctx context.Context, agentID string) ([]*models.AgentVisionRule, error)

	QueryEvents(ctx context.Context, agentID string, req *models.VisionEventQueryDTO) ([]*models.VisionEventListItem, int64, error)
	GetEvent(ctx context.Context, eventID string) (*models.VisionEventDetail, error)
	CreateEvent(ctx context.Context, req *models.VisionEventCreateDTO) (*models.AgentVisionEvent, error)
}

type visionService struct {
	sourceRepo repository.VisionSourceRepository
	ruleRepo   repository.VisionRuleRepository
	eventRepo  repository.VisionEventRepository
	mediaRepo  repository.MediaRepository
	crypto     *security.Crypto
}

func NewVisionService(
	sourceRepo repository.VisionSourceRepository,
	ruleRepo repository.VisionRuleRepository,
	eventRepo repository.VisionEventRepository,
	mediaRepo repository.MediaRepository,
	crypto *security.Crypto,
) VisionService {
	return &visionService{
		sourceRepo: sourceRepo,
		ruleRepo:   ruleRepo,
		eventRepo:  eventRepo,
		mediaRepo:  mediaRepo,
		crypto:     crypto,
	}
}

func (s *visionService) CreateSource(ctx context.Context, agentID string, req *models.VisionSourceCreateDTO) (*models.AgentVisionSource, error) {
	if strings.TrimSpace(agentID) == "" {
		return nil, errors.New("agentID不能为空")
	}
	if req == nil {
		return nil, errors.New("请求体不能为空")
	}
	protocol := normalizeVisionProtocol(req.Protocol)
	if protocol == "" {
		return nil, errors.New("协议不能为空")
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	password := req.Password
	if strings.TrimSpace(password) == security.MaskedValue {
		password = ""
	}
	encryptedPassword, err := s.encryptPassword(password)
	if err != nil {
		return nil, err
	}
	source := &models.AgentVisionSource{
		ID:        uuid.NewString(),
		AgentID:   agentID,
		Name:      strings.TrimSpace(req.Name),
		Protocol:  protocol,
		SourceURI: strings.TrimSpace(req.SourceURI),
		Username:  strings.TrimSpace(req.Username),
		Password:  encryptedPassword,
		DeviceID:  strings.TrimSpace(req.DeviceID),
		ChannelID: strings.TrimSpace(req.ChannelID),
		Zones:     req.Zones,
		Enabled:   enabled,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if strings.TrimSpace(source.Name) == "" {
		return nil, errors.New("来源名称不能为空")
	}
	if err := validateVisionSourceConfig(source.Protocol, source.SourceURI, source.DeviceID, source.ChannelID); err != nil {
		return nil, err
	}
	if err := s.sourceRepo.Create(ctx, source); err != nil {
		return nil, err
	}
	return s.maskSource(source), nil
}

func (s *visionService) UpdateSource(ctx context.Context, sourceID string, req *models.VisionSourceUpdateDTO) (*models.AgentVisionSource, error) {
	source, err := s.sourceRepo.FindByID(ctx, sourceID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("来源不存在")
		}
		return nil, err
	}
	if req == nil {
		return source, nil
	}
	protocol := normalizeVisionProtocol(source.Protocol)
	if protocol != source.Protocol {
		source.Protocol = protocol
	}
	if req.Name != nil {
		source.Name = strings.TrimSpace(*req.Name)
	}
	if req.Protocol != nil {
		protocol = normalizeVisionProtocol(*req.Protocol)
		source.Protocol = protocol
	}
	if req.SourceURI != nil {
		source.SourceURI = strings.TrimSpace(*req.SourceURI)
	}
	if req.Username != nil {
		source.Username = strings.TrimSpace(*req.Username)
	}
	if req.Password != nil {
		if strings.TrimSpace(*req.Password) != security.MaskedValue {
			encryptedPassword, err := s.encryptPassword(*req.Password)
			if err != nil {
				return nil, err
			}
			source.Password = encryptedPassword
		}
	}
	if req.DeviceID != nil {
		source.DeviceID = strings.TrimSpace(*req.DeviceID)
	}
	if req.ChannelID != nil {
		source.ChannelID = strings.TrimSpace(*req.ChannelID)
	}
	if req.Zones != nil {
		source.Zones = req.Zones
	}
	if req.Enabled != nil {
		source.Enabled = *req.Enabled
	}
	if strings.TrimSpace(source.Name) == "" {
		return nil, errors.New("来源名称不能为空")
	}
	if err := validateVisionSourceConfig(protocol, source.SourceURI, source.DeviceID, source.ChannelID); err != nil {
		return nil, err
	}
	source.UpdatedAt = time.Now()
	if err := s.sourceRepo.Update(ctx, source); err != nil {
		return nil, err
	}
	return s.maskSource(source), nil
}

func (s *visionService) DeleteSource(ctx context.Context, sourceID string) error {
	return s.sourceRepo.Delete(ctx, sourceID)
}

func (s *visionService) ListSources(ctx context.Context, agentID string) ([]*models.AgentVisionSource, error) {
	sources, err := s.sourceRepo.FindByAgentID(ctx, agentID)
	if err != nil {
		return nil, err
	}
	for _, source := range sources {
		s.maskSource(source)
	}
	return sources, nil
}

func (s *visionService) ListSourcesForSync(ctx context.Context, agentID string) ([]*models.AgentVisionSource, error) {
	sources, err := s.sourceRepo.FindByAgentID(ctx, agentID)
	if err != nil {
		return nil, err
	}
	for _, source := range sources {
		if decrypted, err := s.decryptPassword(source.Password); err == nil {
			source.Password = decrypted
		}
	}
	return sources, nil
}

func (s *visionService) CreateRule(ctx context.Context, agentID string, req *models.VisionRuleCreateDTO) (*models.AgentVisionRule, error) {
	if strings.TrimSpace(agentID) == "" {
		return nil, errors.New("agentID不能为空")
	}
	if req == nil {
		return nil, errors.New("请求体不能为空")
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	decisionMode := strings.TrimSpace(req.DecisionMode)
	if decisionMode == "" {
		decisionMode = "model"
	}
	rule := &models.AgentVisionRule{
		ID:                  uuid.NewString(),
		AgentID:             agentID,
		Name:                strings.TrimSpace(req.Name),
		ConditionText:       strings.TrimSpace(req.ConditionText),
		SampleIntervalMs:    req.SampleIntervalMs,
		MaxInflight:         req.MaxInflight,
		PromptTemplate:      strings.TrimSpace(req.PromptTemplate),
		VllmModelID:         strings.TrimSpace(req.VllmModelID),
		DecisionMode:        decisionMode,
		ConfidenceThreshold: req.ConfidenceThreshold,
		MinHitCount:         req.MinHitCount,
		MinHitSeconds:       req.MinHitSeconds,
		CooldownSeconds:     req.CooldownSeconds,
		Frequency:           req.Frequency,
		MotionFilterEnabled: req.MotionFilterEnabled,
		VisionUseImgCount:   req.VisionUseImgCount,
		AutoSelectSource:    req.AutoSelectSource,
		Schedule:            req.Schedule,
		Action:              req.Action,
		Enabled:             enabled,
		CreatedAt:           time.Now(),
		UpdatedAt:           time.Now(),
	}
	if err := s.ruleRepo.Create(ctx, rule); err != nil {
		return nil, err
	}
	return rule, nil
}

func (s *visionService) UpdateRule(ctx context.Context, ruleID string, req *models.VisionRuleUpdateDTO) (*models.AgentVisionRule, error) {
	rule, err := s.ruleRepo.FindByID(ctx, ruleID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("规则不存在")
		}
		return nil, err
	}
	if req == nil {
		return rule, nil
	}
	if req.Name != nil {
		rule.Name = strings.TrimSpace(*req.Name)
	}
	if req.ConditionText != nil {
		rule.ConditionText = strings.TrimSpace(*req.ConditionText)
	}
	if req.SampleIntervalMs != nil {
		rule.SampleIntervalMs = *req.SampleIntervalMs
	}
	if req.MaxInflight != nil {
		rule.MaxInflight = *req.MaxInflight
	}
	if req.PromptTemplate != nil {
		rule.PromptTemplate = strings.TrimSpace(*req.PromptTemplate)
	}
	if req.VllmModelID != nil {
		rule.VllmModelID = strings.TrimSpace(*req.VllmModelID)
	}
	if req.DecisionMode != nil {
		rule.DecisionMode = strings.TrimSpace(*req.DecisionMode)
	}
	if req.ConfidenceThreshold != nil {
		rule.ConfidenceThreshold = req.ConfidenceThreshold
	}
	if req.MinHitCount != nil {
		rule.MinHitCount = *req.MinHitCount
	}
	if req.MinHitSeconds != nil {
		rule.MinHitSeconds = *req.MinHitSeconds
	}
	if req.CooldownSeconds != nil {
		rule.CooldownSeconds = *req.CooldownSeconds
	}
	if req.Frequency != nil {
		rule.Frequency = req.Frequency
	}
	if req.MotionFilterEnabled != nil {
		rule.MotionFilterEnabled = *req.MotionFilterEnabled
	}
	if req.VisionUseImgCount != nil {
		rule.VisionUseImgCount = *req.VisionUseImgCount
	}
	if req.AutoSelectSource != nil {
		rule.AutoSelectSource = *req.AutoSelectSource
	}
	if req.Schedule != nil {
		rule.Schedule = req.Schedule
	}
	if req.Action != nil {
		rule.Action = req.Action
	}
	if req.Enabled != nil {
		rule.Enabled = *req.Enabled
	}
	rule.UpdatedAt = time.Now()
	if err := s.ruleRepo.Update(ctx, rule); err != nil {
		return nil, err
	}
	return rule, nil
}

func (s *visionService) DeleteRule(ctx context.Context, ruleID string) error {
	return s.ruleRepo.Delete(ctx, ruleID)
}

func (s *visionService) ListRules(ctx context.Context, agentID string) ([]*models.AgentVisionRule, error) {
	return s.ruleRepo.FindByAgentID(ctx, agentID)
}

func (s *visionService) CreateEvent(ctx context.Context, req *models.VisionEventCreateDTO) (*models.AgentVisionEvent, error) {
	if req == nil {
		return nil, errors.New("请求体不能为空")
	}
	if strings.TrimSpace(req.AgentID) == "" {
		return nil, errors.New("agentId不能为空")
	}
	event := &models.AgentVisionEvent{
		ID:              uuid.NewString(),
		AgentID:         req.AgentID,
		SourceID:        req.SourceID,
		RuleID:          req.RuleID,
		SnapshotMediaID: req.SnapshotMediaID,
		Summary:         req.Summary,
		Confidence:      req.Confidence,
		Labels:          req.Labels,
		RawResponse:     req.RawResponse,
		ActionResult:    req.ActionResult,
		CreatedAt:       time.Now(),
	}
	if err := s.eventRepo.Create(ctx, event); err != nil {
		return nil, err
	}
	return event, nil
}

func (s *visionService) QueryEvents(ctx context.Context, agentID string, req *models.VisionEventQueryDTO) ([]*models.VisionEventListItem, int64, error) {
	events, total, err := s.eventRepo.Query(ctx, agentID, req)
	if err != nil {
		return nil, 0, err
	}
	sources, _ := s.sourceRepo.FindByAgentID(ctx, agentID)
	rules, _ := s.ruleRepo.FindByAgentID(ctx, agentID)
	sourceMap := make(map[string]*models.AgentVisionSource)
	for _, source := range sources {
		sourceMap[source.ID] = source
	}
	ruleMap := make(map[string]*models.AgentVisionRule)
	for _, rule := range rules {
		ruleMap[rule.ID] = rule
	}
	items := make([]*models.VisionEventListItem, 0, len(events))
	for _, event := range events {
		item := &models.VisionEventListItem{
			ID:              event.ID,
			AgentID:         event.AgentID,
			SourceID:        event.SourceID,
			RuleID:          event.RuleID,
			SnapshotMediaID: event.SnapshotMediaID,
			Summary:         event.Summary,
			Confidence:      event.Confidence,
			Labels:          event.Labels,
			ActionResult:    event.ActionResult,
			CreatedAt:       event.CreatedAt,
		}
		item.SnapshotURL = s.resolveSnapshotURL(ctx, event.SnapshotMediaID)
		if source := sourceMap[event.SourceID]; source != nil {
			item.SourceName = source.Name
		}
		if rule := ruleMap[event.RuleID]; rule != nil {
			item.RuleName = rule.Name
		}
		items = append(items, item)
	}
	return items, total, nil
}

func (s *visionService) GetEvent(ctx context.Context, eventID string) (*models.VisionEventDetail, error) {
	event, err := s.eventRepo.FindByID(ctx, eventID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("事件不存在")
		}
		return nil, err
	}
	detail := &models.VisionEventDetail{
		ID:              event.ID,
		AgentID:         event.AgentID,
		SourceID:        event.SourceID,
		RuleID:          event.RuleID,
		SnapshotMediaID: event.SnapshotMediaID,
		Summary:         event.Summary,
		Confidence:      event.Confidence,
		Labels:          event.Labels,
		RawResponse:     event.RawResponse,
		ActionResult:    event.ActionResult,
		CreatedAt:       event.CreatedAt,
	}
	detail.SnapshotURL = s.resolveSnapshotURL(ctx, event.SnapshotMediaID)
	if rule, err := s.ruleRepo.FindByID(ctx, event.RuleID); err == nil {
		detail.Prompt = rule.PromptTemplate
	}
	return detail, nil
}

func (s *visionService) resolveSnapshotURL(ctx context.Context, mediaID string) string {
	if s.mediaRepo == nil {
		return ""
	}
	trimmed := strings.TrimSpace(mediaID)
	if trimmed == "" {
		return ""
	}
	parsed, err := strconv.ParseUint(trimmed, 10, 64)
	if err != nil || parsed == 0 {
		return ""
	}
	asset, err := s.mediaRepo.GetByID(ctx, parsed)
	if err != nil || asset == nil {
		return ""
	}
	return asset.StorageURI
}

func (s *visionService) encryptPassword(value string) (string, error) {
	if s.crypto == nil {
		return value, nil
	}
	return s.crypto.Encrypt(value)
}

func (s *visionService) decryptPassword(value string) (string, error) {
	if s.crypto == nil {
		return value, nil
	}
	return s.crypto.Decrypt(value)
}

func (s *visionService) maskSource(source *models.AgentVisionSource) *models.AgentVisionSource {
	if source == nil {
		return nil
	}
	source.Password = security.MaskSecret(source.Password)
	return source
}

func normalizeVisionProtocol(value string) string {
	protocol := strings.ToLower(strings.TrimSpace(value))
	if protocol == "httpflv" {
		return "http-flv"
	}
	return protocol
}

func validateVisionSourceConfig(protocol, sourceURI, deviceID, channelID string) error {
	switch protocol {
	case "rtsp", "rtmp", "hls", "http-flv":
		if strings.TrimSpace(sourceURI) == "" {
			return errors.New("来源地址不能为空")
		}
	case "onvif":
		if strings.TrimSpace(sourceURI) == "" {
			return errors.New("ONVIF 设备地址不能为空")
		}
	case "gb28181":
		if strings.TrimSpace(deviceID) == "" {
			return errors.New("GB28181 设备ID不能为空")
		}
		if strings.TrimSpace(channelID) == "" {
			return errors.New("GB28181 通道ID不能为空")
		}
	case "webrtc":
		if strings.TrimSpace(channelID) == "" {
			return errors.New("WebRTC 通道ID不能为空")
		}
	default:
		return errors.New("不支持的协议类型")
	}
	return nil
}

// decodeJSON helper for config mapping
