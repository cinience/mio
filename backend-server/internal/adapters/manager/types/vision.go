package types

// VisionAgentSource represents a visual stream source for vision agents.
type VisionAgentSource struct {
	ID        string      `json:"id"`
	Name      string      `json:"name"`
	Protocol  string      `json:"protocol"`
	SourceURI string      `json:"sourceUri"`
	Username  string      `json:"username,omitempty"`
	Password  string      `json:"password,omitempty"`
	DeviceID  string      `json:"deviceId,omitempty"`
	ChannelID string      `json:"channelId,omitempty"`
	Zones     interface{} `json:"zones,omitempty"`
	Enabled   bool        `json:"enabled"`
}

// VisionAgentRule represents a visual detection rule.
type VisionAgentRule struct {
	ID                  string      `json:"id"`
	Name                string      `json:"name"`
	ConditionText       string      `json:"conditionText,omitempty"`
	SampleIntervalMs    int         `json:"sampleIntervalMs,omitempty"`
	MaxInflight         int         `json:"maxInflight,omitempty"`
	PromptTemplate      string      `json:"promptTemplate,omitempty"`
	VllmModelID         string      `json:"vllmModelId,omitempty"`
	DecisionMode        string      `json:"decisionMode,omitempty"`
	ConfidenceThreshold float64     `json:"confidenceThreshold,omitempty"`
	MinHitCount         int         `json:"minHitCount,omitempty"`
	MinHitSeconds       int         `json:"minHitSeconds,omitempty"`
	CooldownSeconds     int         `json:"cooldownSeconds,omitempty"`
	FrequencyFilter     interface{} `json:"frequencyFilter,omitempty"`
	MotionFilterEnabled bool        `json:"motionFilterEnabled,omitempty"`
	VisionUseImgCount   int         `json:"visionUseImgCount,omitempty"`
	AutoSelectSource    bool        `json:"autoSelectSource,omitempty"`
	Schedule            interface{} `json:"schedule,omitempty"`
	Action              interface{} `json:"action,omitempty"`
	Enabled             bool        `json:"enabled,omitempty"`
}

// VisionAgentConfig aggregates a vision agent's sources and rules.
type VisionAgentConfig struct {
	AgentID      string              `json:"agentId"`
	AgentName    string              `json:"agentName"`
	AgentType    string              `json:"agentType"`
	SystemPrompt string              `json:"systemPrompt,omitempty"`
	Sources      []VisionAgentSource `json:"sources"`
	Rules        []VisionAgentRule   `json:"rules"`
}

// VisionEventCreateRequest represents the payload for reporting a vision event.
type VisionEventCreateRequest struct {
	AgentID         string                 `json:"agentId"`
	SourceID        string                 `json:"sourceId"`
	RuleID          string                 `json:"ruleId"`
	SnapshotMediaID string                 `json:"snapshotMediaId,omitempty"`
	Summary         string                 `json:"summary,omitempty"`
	Confidence      float64                `json:"confidence,omitempty"`
	Labels          string                 `json:"labels,omitempty"`
	RawResponse     string                 `json:"rawResponse,omitempty"`
	ActionResult    string                 `json:"actionResult,omitempty"`
	Extra           map[string]interface{} `json:"extra,omitempty"`
}
