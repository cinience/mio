package types

// SwitchAgentVoiceRequest represents the payload for updating an agent's TTS voice.
type SwitchAgentVoiceRequest struct {
	TTSModelID string `json:"ttsModelId,omitempty"`
	TTSVoiceID string `json:"ttsVoiceId"`
}

// VoiceOption represents a single selectable voice entry.
type VoiceOption struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	TTSVoice  string `json:"ttsVoice"`
	Languages string `json:"languages"`
	VoiceDemo string `json:"voiceDemo"`
}

// AgentVoiceOptions captures available voice options and current selection for an agent.
type AgentVoiceOptions struct {
	TTSModelID string         `json:"ttsModelId"`
	TTSVoiceID string         `json:"ttsVoiceId"`
	Voices     []*VoiceOption `json:"voices"`
}
