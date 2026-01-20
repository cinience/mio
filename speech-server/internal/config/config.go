package config

import (
	"time"

	openairt "github.com/WqyJh/go-openai-realtime/v2"

	"speech-server/internal/asr"
	"speech-server/internal/logger"
	"speech-server/internal/tts"
)

// Config captures the runtime configuration for the speech server.
type Config struct {
	Addr       string
	ConfigPath string

	ASRProvider string
	ASRModels   map[string]asr.Config

	TTSProvider string
	TTSModels   map[string]TTSModel

	VADEnabled  bool
	VADProvider string
	VADModels   map[string]asr.VADModel
	VADPool     VADPoolConfig

	Log LogConfig

	// Audio filtering configuration
	AudioEnergyThreshold float32 // Minimum RMS energy threshold (0.001-0.1, default 0.01)
	AudioMinDuration     float32 // Minimum audio duration in seconds (default 0.3)

	Speaker SpeakerConfig

	ModelBootstrap ModelBootstrapConfig
}

// TTSModel aggregates the configuration for a single TTS provider.
type TTSModel struct {
	Config       tts.Config
	VoiceMap     map[openairt.Voice]int
	DefaultVoice openairt.Voice
	DefaultSpeed float32
}

// VADPoolConfig configures the detector pool.
type VADPoolConfig struct {
	MinSize        int
	MaxSize        int
	AcquireTimeout time.Duration
}

// ModelBootstrapConfig controls automatic model downloads.
type ModelBootstrapConfig struct {
	AutoDownload bool
	GitRepo      string
	GitRef       string
	TargetDir    string
}

// LogConfig wraps structured logging configuration.
type LogConfig = logger.Config

// LogFileConfig controls on-disk log rotation.
type LogFileConfig = logger.FileConfig

// NewDefault returns a Config seeded with reasonable defaults.
func NewDefault() Config {
	return Config{
		Addr:      ":8009",
		ASRModels: make(map[string]asr.Config),
		TTSModels: make(map[string]TTSModel),
		VADModels: make(map[string]asr.VADModel),
		VADPool:   VADPoolConfig{},
		Speaker: SpeakerConfig{
			Enabled:         true,
			ModelPath:       "./models/speaker/3dspeaker_speech_campplus_sv_zh_en_16k-common_advanced.onnx",
			SampleRate:      16000,
			NumThreads:      0,
			Provider:        "cpu",
			Threshold:       0.65,
			MinDuration:     0.4,
			EnergyThreshold: 0.003,
			DataDir:         "./data/speaker",
			VectorDB: SpeakerVectorDBConfig{
				Provider: "chromem",
				Chromem: SpeakerChromemConfig{
					Path:     "./data/speaker_index",
					Compress: false,
				},
			},
		},
		Log: LogConfig{
			Level:  "info",
			Stdout: true,
			File: LogFileConfig{
				Enabled:    true,
				Path:       "./logs",
				Name:       "speech-server.log",
				MaxSizeMB:  100,
				MaxBackups: 5,
				MaxAgeDays: 30,
				Compress:   true,
			},
		},
		ModelBootstrap: ModelBootstrapConfig{
			AutoDownload: true,
			GitRepo:      defaultModelGitRepo,
			TargetDir:    "./models",
		},
	}
}

// SpeakerConfig controls speaker embedding and storage.
type SpeakerConfig struct {
	Enabled         bool
	ModelPath       string
	SampleRate      int
	NumThreads      int
	Provider        string
	Threshold       float32
	MinDuration     float32
	EnergyThreshold float32
	DataDir         string
	VectorDB        SpeakerVectorDBConfig
}

type SpeakerVectorDBConfig struct {
	Provider string
	Chromem  SpeakerChromemConfig
}

type SpeakerChromemConfig struct {
	Path     string
	Compress bool
}
