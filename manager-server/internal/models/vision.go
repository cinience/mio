package models

import (
	"time"

	"gorm.io/datatypes"
)

// AgentVisionSource 视觉来源实体
// 存储摄像头/流媒体来源配置
// Table: ai_agent_vision_source
type AgentVisionSource struct {
	ID        string         `gorm:"primarykey;column:id;type:varchar(36)" json:"id"`
	AgentID   string         `gorm:"column:agent_id;type:varchar(32);index" json:"agentId"`
	Name      string         `gorm:"column:name;type:varchar(64)" json:"name"`
	Protocol  string         `gorm:"column:protocol;type:varchar(32)" json:"protocol"`
	SourceURI string         `gorm:"column:source_uri;type:text" json:"sourceUri"`
	Username  string         `gorm:"column:username;type:varchar(64)" json:"username"`
	Password  string         `gorm:"column:password;type:text" json:"password"`
	DeviceID  string         `gorm:"column:device_id;type:varchar(64)" json:"deviceId"`
	ChannelID string         `gorm:"column:channel_id;type:varchar(64)" json:"channelId"`
	Zones     datatypes.JSON `gorm:"column:zones_json;type:jsonb" json:"zones"`
	Enabled   bool           `gorm:"column:enabled;default:true" json:"enabled"`
	CreatedAt time.Time      `gorm:"column:created_at" json:"createdAt"`
	UpdatedAt time.Time      `gorm:"column:updated_at" json:"updatedAt"`
}

// TableName 指定表名
func (AgentVisionSource) TableName() string {
	return "ai_agent_vision_source"
}

// AgentVisionRule 视觉规则实体
// Table: ai_agent_vision_rule
type AgentVisionRule struct {
	ID                  string         `gorm:"primarykey;column:id;type:varchar(36)" json:"id"`
	AgentID             string         `gorm:"column:agent_id;type:varchar(32);index" json:"agentId"`
	Name                string         `gorm:"column:name;type:varchar(64)" json:"name"`
	ConditionText       string         `gorm:"column:condition_text;type:text" json:"conditionText"`
	SampleIntervalMs    int            `gorm:"column:sample_interval_ms" json:"sampleIntervalMs"`
	MaxInflight         int            `gorm:"column:max_inflight" json:"maxInflight"`
	PromptTemplate      string         `gorm:"column:prompt_template;type:text" json:"promptTemplate"`
	VllmModelID         string         `gorm:"column:vllm_model_id;type:varchar(32)" json:"vllmModelId"`
	DecisionMode        string         `gorm:"column:decision_mode;type:varchar(16);default:'model'" json:"decisionMode"`
	ConfidenceThreshold *float64       `gorm:"column:confidence_threshold" json:"confidenceThreshold"`
	MinHitCount         int            `gorm:"column:min_hit_count" json:"minHitCount"`
	MinHitSeconds       int            `gorm:"column:min_hit_seconds" json:"minHitSeconds"`
	CooldownSeconds     int            `gorm:"column:cooldown_seconds" json:"cooldownSeconds"`
	Frequency           datatypes.JSON `gorm:"column:frequency_json;type:jsonb" json:"frequencyFilter"`
	MotionFilterEnabled bool           `gorm:"column:motion_filter_enabled;default:false" json:"motionFilterEnabled"`
	VisionUseImgCount   int            `gorm:"column:vision_use_img_count;default:1" json:"visionUseImgCount"`
	AutoSelectSource    bool           `gorm:"column:auto_select_source;default:false" json:"autoSelectSource"`
	Schedule            datatypes.JSON `gorm:"column:schedule_json;type:jsonb" json:"schedule"`
	Action              datatypes.JSON `gorm:"column:action_json;type:jsonb" json:"action"`
	Enabled             bool           `gorm:"column:enabled;default:true" json:"enabled"`
	CreatedAt           time.Time      `gorm:"column:created_at" json:"createdAt"`
	UpdatedAt           time.Time      `gorm:"column:updated_at" json:"updatedAt"`
}

// TableName 指定表名
func (AgentVisionRule) TableName() string {
	return "ai_agent_vision_rule"
}

// AgentVisionEvent 视觉事件实体
// Table: ai_agent_vision_event
type AgentVisionEvent struct {
	ID              string    `gorm:"primarykey;column:id;type:varchar(36)" json:"id"`
	AgentID         string    `gorm:"column:agent_id;type:varchar(32);index" json:"agentId"`
	SourceID        string    `gorm:"column:source_id;type:varchar(36);index" json:"sourceId"`
	RuleID          string    `gorm:"column:rule_id;type:varchar(36);index" json:"ruleId"`
	SnapshotMediaID string    `gorm:"column:snapshot_media_id;type:varchar(64)" json:"snapshotMediaId"`
	Summary         string    `gorm:"column:summary;type:text" json:"summary"`
	Confidence      float64   `gorm:"column:confidence" json:"confidence"`
	Labels          string    `gorm:"column:labels;type:text" json:"labels"`
	RawResponse     string    `gorm:"column:raw_response;type:text" json:"rawResponse"`
	ActionResult    string    `gorm:"column:action_result;type:text" json:"actionResult"`
	CreatedAt       time.Time `gorm:"column:created_at" json:"createdAt"`
}

// TableName 指定表名
func (AgentVisionEvent) TableName() string {
	return "ai_agent_vision_event"
}

// VisionSourceCreateDTO 视觉来源创建请求
type VisionSourceCreateDTO struct {
	Name      string         `json:"name" binding:"required"`
	Protocol  string         `json:"protocol" binding:"required"`
	SourceURI string         `json:"sourceUri"`
	Username  string         `json:"username"`
	Password  string         `json:"password"`
	DeviceID  string         `json:"deviceId"`
	ChannelID string         `json:"channelId"`
	Zones     datatypes.JSON `json:"zones"`
	Enabled   *bool          `json:"enabled"`
}

// VisionSourceUpdateDTO 视觉来源更新请求
type VisionSourceUpdateDTO struct {
	Name      *string        `json:"name"`
	Protocol  *string        `json:"protocol"`
	SourceURI *string        `json:"sourceUri"`
	Username  *string        `json:"username"`
	Password  *string        `json:"password"`
	DeviceID  *string        `json:"deviceId"`
	ChannelID *string        `json:"channelId"`
	Zones     datatypes.JSON `json:"zones"`
	Enabled   *bool          `json:"enabled"`
}

// VisionRuleCreateDTO 视觉规则创建请求
type VisionRuleCreateDTO struct {
	Name                string         `json:"name" binding:"required"`
	ConditionText       string         `json:"conditionText"`
	SampleIntervalMs    int            `json:"sampleIntervalMs"`
	MaxInflight         int            `json:"maxInflight"`
	PromptTemplate      string         `json:"promptTemplate"`
	VllmModelID         string         `json:"vllmModelId"`
	DecisionMode        string         `json:"decisionMode"`
	ConfidenceThreshold *float64       `json:"confidenceThreshold"`
	MinHitCount         int            `json:"minHitCount"`
	MinHitSeconds       int            `json:"minHitSeconds"`
	CooldownSeconds     int            `json:"cooldownSeconds"`
	Frequency           datatypes.JSON `json:"frequencyFilter"`
	MotionFilterEnabled bool           `json:"motionFilterEnabled"`
	VisionUseImgCount   int            `json:"visionUseImgCount"`
	AutoSelectSource    bool           `json:"autoSelectSource"`
	Schedule            datatypes.JSON `json:"schedule"`
	Action              datatypes.JSON `json:"action"`
	Enabled             *bool          `json:"enabled"`
}

// VisionRuleUpdateDTO 视觉规则更新请求
type VisionRuleUpdateDTO struct {
	Name                *string        `json:"name"`
	ConditionText       *string        `json:"conditionText"`
	SampleIntervalMs    *int           `json:"sampleIntervalMs"`
	MaxInflight         *int           `json:"maxInflight"`
	PromptTemplate      *string        `json:"promptTemplate"`
	VllmModelID         *string        `json:"vllmModelId"`
	DecisionMode        *string        `json:"decisionMode"`
	ConfidenceThreshold *float64       `json:"confidenceThreshold"`
	MinHitCount         *int           `json:"minHitCount"`
	MinHitSeconds       *int           `json:"minHitSeconds"`
	CooldownSeconds     *int           `json:"cooldownSeconds"`
	Frequency           datatypes.JSON `json:"frequencyFilter"`
	MotionFilterEnabled *bool          `json:"motionFilterEnabled"`
	VisionUseImgCount   *int           `json:"visionUseImgCount"`
	AutoSelectSource    *bool          `json:"autoSelectSource"`
	Schedule            datatypes.JSON `json:"schedule"`
	Action              datatypes.JSON `json:"action"`
	Enabled             *bool          `json:"enabled"`
}

// VisionEventQueryDTO 事件查询请求
type VisionEventQueryDTO struct {
	SourceID        string         `json:"sourceId"`
	RuleID          string         `json:"ruleId"`
	Keyword         string         `json:"keyword"`
	TimeRange       datatypes.JSON `json:"timeRange"`
	ConfidenceRange datatypes.JSON `json:"confidenceRange"`
	Page            int            `json:"page"`
	Limit           int            `json:"limit"`
}

// VisionEventCreateDTO 事件上报请求（后端回写）
type VisionEventCreateDTO struct {
	AgentID         string         `json:"agentId" binding:"required"`
	SourceID        string         `json:"sourceId" binding:"required"`
	RuleID          string         `json:"ruleId" binding:"required"`
	SnapshotMediaID string         `json:"snapshotMediaId"`
	Summary         string         `json:"summary"`
	Confidence      float64        `json:"confidence"`
	Labels          string         `json:"labels"`
	RawResponse     string         `json:"rawResponse"`
	ActionResult    string         `json:"actionResult"`
	Extra           datatypes.JSON `json:"extra"`
}

// VisionEventListItem 事件列表输出
type VisionEventListItem struct {
	ID              string    `json:"id"`
	AgentID         string    `json:"agentId"`
	SourceID        string    `json:"sourceId"`
	SourceName      string    `json:"sourceName,omitempty"`
	RuleID          string    `json:"ruleId"`
	RuleName        string    `json:"ruleName,omitempty"`
	SnapshotMediaID string    `json:"snapshotMediaId"`
	SnapshotURL     string    `json:"snapshotUrl,omitempty"`
	Summary         string    `json:"summary"`
	Confidence      float64   `json:"confidence"`
	Labels          string    `json:"labels"`
	ActionResult    string    `json:"actionResult"`
	CreatedAt       time.Time `json:"createdAt"`
}

// VisionEventDetail 事件详情输出
type VisionEventDetail struct {
	ID              string         `json:"id"`
	AgentID         string         `json:"agentId"`
	SourceID        string         `json:"sourceId"`
	RuleID          string         `json:"ruleId"`
	SnapshotMediaID string         `json:"snapshotMediaId"`
	SnapshotURL     string         `json:"snapshotUrl,omitempty"`
	Summary         string         `json:"summary"`
	Confidence      float64        `json:"confidence"`
	Labels          string         `json:"labels"`
	Prompt          string         `json:"prompt,omitempty"`
	RawResponse     string         `json:"rawResponse"`
	ActionResult    string         `json:"actionResult"`
	HitTrace        datatypes.JSON `json:"hitTrace,omitempty"`
	CreatedAt       time.Time      `json:"createdAt"`
}

// VisionAgentConfig 视觉智能体配置（供后台服务拉取）
type VisionAgentConfig struct {
	AgentID      string               `json:"agentId"`
	AgentName    string               `json:"agentName"`
	AgentType    string               `json:"agentType"`
	SystemPrompt string               `json:"systemPrompt"`
	Sources      []*AgentVisionSource `json:"sources"`
	Rules        []*AgentVisionRule   `json:"rules"`
}
