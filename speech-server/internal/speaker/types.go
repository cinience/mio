package speaker

import "time"

// Config captures speaker runtime settings.
type Config struct {
	Enabled         bool
	ModelPath       string
	SampleRate      int
	NumThreads      int
	Provider        string
	Threshold       float32
	MinDuration     float32
	EnergyThreshold float32
	DataDir         string
	VectorDB        VectorDBConfig
}

type VectorDBConfig struct {
	Provider string
	Chromem  ChromemConfig
}

type ChromemConfig struct {
	Path     string
	Compress bool
}

// RegisterInput bundles audio and metadata for registration.
type RegisterInput struct {
	UID         string
	AgentID     string
	SpeakerID   string
	SpeakerName string
	Samples     []float32
	SampleRate  int
}

// IdentifyInput carries audio and optional filters.
type IdentifyInput struct {
	UID         string
	AgentID     string
	SpeakerID   string
	SpeakerName string
	Samples     []float32
	SampleRate  int
	Threshold   float32
	TopK        int
}

// VerifyInput verifies against a concrete speaker id.
type VerifyInput struct {
	UID        string
	AgentID    string
	SpeakerID  string
	Samples    []float32
	SampleRate int
	Threshold  float32
}

type IdentifyResult struct {
	SpeakerID   string  `json:"speaker_id,omitempty"`
	SpeakerName string  `json:"speaker_name,omitempty"`
	Confidence  float32 `json:"confidence"`
}

type VerifyResult struct {
	SpeakerID   string  `json:"speaker_id,omitempty"`
	SpeakerName string  `json:"speaker_name,omitempty"`
	Confidence  float32 `json:"confidence"`
	Verified    bool    `json:"verified"`
}

type SpeakerInfo struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	SampleCount int       `json:"sample_count"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Stats struct {
	TotalSpeakers int `json:"total_speakers"`
	TotalSamples  int `json:"total_samples"`
}
