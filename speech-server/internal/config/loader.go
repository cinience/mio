package config

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	openairt "github.com/WqyJh/go-openai-realtime/v2"
	mapstructure "github.com/go-viper/mapstructure/v2"
	"github.com/spf13/viper"

	"speech-server/config"
	"speech-server/internal/asr"
	"speech-server/internal/tts"
)

type fileConfig struct {
	Server         *fileServerConfig         `yaml:"server" mapstructure:"server"`
	ASR            *fileASRConfig            `yaml:"asr" mapstructure:"asr"`
	TTS            *fileTTSConfig            `yaml:"tts" mapstructure:"tts"`
	VAD            *fileVADConfig            `yaml:"vad" mapstructure:"vad"`
	Models         *fileModelsConfig         `yaml:"models" mapstructure:"models"`
	AudioFiltering *fileAudioFilteringConfig `yaml:"audio_filtering" mapstructure:"audio_filtering"`
	Speaker        *fileSpeakerConfig        `yaml:"speaker" mapstructure:"speaker"`
	Log            *fileLogConfig            `yaml:"log" mapstructure:"log"`
}

type fileServerConfig struct {
	Addr string `yaml:"addr" mapstructure:"addr"`
}

type fileASRConfig struct {
	Provider      string                  `yaml:"provider" mapstructure:"provider"`
	Models        map[string]fileASRModel `yaml:"models" mapstructure:"models"`
	LanguageGuard *fileASRLanguageGuard   `yaml:"language_guard" mapstructure:"language_guard"`
}

type fileASRModel struct {
	ModelDir       string                `yaml:"model_dir" mapstructure:"model_dir"`
	ModelType      string                `yaml:"model_type" mapstructure:"model_type"`
	ModelVariant   string                `yaml:"model_variant" mapstructure:"model_variant"`
	DecodingMethod string                `yaml:"decoding_method" mapstructure:"decoding_method"`
	Threads        int                   `yaml:"threads" mapstructure:"threads"`
	Provider       string                `yaml:"provider" mapstructure:"provider"`
	Debug          int                   `yaml:"debug_level" mapstructure:"debug_level"`
	SampleRate     int                   `yaml:"sample_rate" mapstructure:"sample_rate"`
	MaxActivePaths int                   `yaml:"max_active_paths" mapstructure:"max_active_paths"`
	EnableEndpoint *bool                 `yaml:"enable_endpoint" mapstructure:"enable_endpoint"`
	Language       string                `yaml:"language" mapstructure:"language"`
	UseITN         *bool                 `yaml:"use_itn" mapstructure:"use_itn"`
	LanguageGuard  *fileASRLanguageGuard `yaml:"language_guard" mapstructure:"language_guard"`
}

type fileASRLanguageGuard struct {
	Enabled               bool    `yaml:"enabled" mapstructure:"enabled"`
	DefaultHint           string  `yaml:"default_hint" mapstructure:"default_hint"`
	MaxMismatchDuration   float64 `yaml:"max_mismatch_duration" mapstructure:"max_mismatch_duration"`
	MaxMismatchCharacters int     `yaml:"max_mismatch_characters" mapstructure:"max_mismatch_characters"`
}

type fileTTSConfig struct {
	Provider string                  `yaml:"provider" mapstructure:"provider"`
	Models   map[string]fileTTSModel `yaml:"models" mapstructure:"models"`
}

type fileTTSModel struct {
	Engine                    string                        `yaml:"engine" mapstructure:"engine"`
	AcousticModel             string                        `yaml:"acoustic_model" mapstructure:"acoustic_model"`
	VocoderModel              string                        `yaml:"vocoder_model" mapstructure:"vocoder_model"`
	Lexicon                   string                        `yaml:"lexicon" mapstructure:"lexicon"`
	Tokens                    string                        `yaml:"tokens" mapstructure:"tokens"`
	EspeakDir                 string                        `yaml:"espeak_dir" mapstructure:"espeak_dir"`
	CN2ANEnabled              bool                          `yaml:"cn2an_enabled" mapstructure:"cn2an_enabled"`
	Threads                   int                           `yaml:"threads" mapstructure:"threads"`
	Provider                  string                        `yaml:"provider" mapstructure:"provider"`
	Debug                     int                           `yaml:"debug_level" mapstructure:"debug_level"`
	NoiseScale                float64                       `yaml:"noise_scale" mapstructure:"noise_scale"`
	LengthScale               float64                       `yaml:"length_scale" mapstructure:"length_scale"`
	VoiceMap                  string                        `yaml:"voice_map" mapstructure:"voice_map"`
	DefaultVoice              string                        `yaml:"default_voice" mapstructure:"default_voice"`
	DefaultSpeed              float64                       `yaml:"default_speed" mapstructure:"default_speed"`
	KokoroModel               string                        `yaml:"kokoro_model" mapstructure:"kokoro_model"`
	KokoroVoices              string                        `yaml:"kokoro_voices" mapstructure:"kokoro_voices"`
	KokoroTokens              string                        `yaml:"kokoro_tokens" mapstructure:"kokoro_tokens"`
	KokoroDataDir             string                        `yaml:"kokoro_data_dir" mapstructure:"kokoro_data_dir"`
	KokoroLexicon             string                        `yaml:"kokoro_lexicon" mapstructure:"kokoro_lexicon"`
	KokoroLang                string                        `yaml:"kokoro_lang" mapstructure:"kokoro_lang"`
	ZipvoiceTokens            string                        `yaml:"zipvoice_tokens" mapstructure:"zipvoice_tokens"`
	ZipvoiceTextModel         string                        `yaml:"zipvoice_text_model" mapstructure:"zipvoice_text_model"`
	ZipvoiceFlowMatchingModel string                        `yaml:"zipvoice_flow_matching_model" mapstructure:"zipvoice_flow_matching_model"`
	ZipvoiceDataDir           string                        `yaml:"zipvoice_data_dir" mapstructure:"zipvoice_data_dir"`
	ZipvoicePinyinDict        string                        `yaml:"zipvoice_pinyin_dict" mapstructure:"zipvoice_pinyin_dict"`
	ZipvoiceVocoder           string                        `yaml:"zipvoice_vocoder" mapstructure:"zipvoice_vocoder"`
	ZipvoiceFeatScale         float64                       `yaml:"zipvoice_feat_scale" mapstructure:"zipvoice_feat_scale"`
	ZipvoiceTShift            float64                       `yaml:"zipvoice_t_shift" mapstructure:"zipvoice_t_shift"`
	ZipvoiceTargetRms         float64                       `yaml:"zipvoice_target_rms" mapstructure:"zipvoice_target_rms"`
	ZipvoiceGuidanceScale     float64                       `yaml:"zipvoice_guidance_scale" mapstructure:"zipvoice_guidance_scale"`
	ZipvoiceNumSteps          int                           `yaml:"zipvoice_num_steps" mapstructure:"zipvoice_num_steps"`
	ZipvoicePromptAudio       string                        `yaml:"zipvoice_prompt_audio" mapstructure:"zipvoice_prompt_audio"`
	ZipvoicePromptText        string                        `yaml:"zipvoice_prompt_text" mapstructure:"zipvoice_prompt_text"`
	ZipvoicePrompts           map[string]fileZipvoicePrompt `yaml:"zipvoice_prompts" mapstructure:"zipvoice_prompts"`
}

type fileZipvoicePrompt struct {
	PromptAudio string `yaml:"audio" mapstructure:"audio"`
	PromptText  string `yaml:"text" mapstructure:"text"`
	NumSteps    int    `yaml:"num_steps" mapstructure:"num_steps"`
}

type fileVADConfig struct {
	Enabled  bool                    `yaml:"enabled" mapstructure:"enabled"`
	Provider string                  `yaml:"provider" mapstructure:"provider"`
	Models   map[string]fileVADModel `yaml:"models" mapstructure:"models"`
	Pool     *fileVADPoolConfig      `yaml:"pool" mapstructure:"pool"`
}

type fileVADModel struct {
	Provider      string  `yaml:"provider" mapstructure:"provider"`
	Model         string  `yaml:"model" mapstructure:"model"`
	ModelPath     string  `yaml:"model_path" mapstructure:"model_path"`
	ModelFile     string  `yaml:"model_file" mapstructure:"model_file"`
	ModelDir      string  `yaml:"model_dir" mapstructure:"model_dir"`
	Threshold     float64 `yaml:"threshold" mapstructure:"threshold"`
	MinSilence    float64 `yaml:"min_silence" mapstructure:"min_silence"`
	MinSpeech     float64 `yaml:"min_speech" mapstructure:"min_speech"`
	MaxSpeech     float64 `yaml:"max_speech" mapstructure:"max_speech"`
	WindowSize    int     `yaml:"window_size" mapstructure:"window_size"`
	SampleRate    int     `yaml:"sample_rate" mapstructure:"sample_rate"`
	Threads       int     `yaml:"threads" mapstructure:"threads"`
	Device        string  `yaml:"device" mapstructure:"device"`
	Debug         int     `yaml:"debug_level" mapstructure:"debug_level"`
	BufferSeconds float64 `yaml:"buffer_seconds" mapstructure:"buffer_seconds"`
}

type fileVADPoolConfig struct {
	MinSize        int `yaml:"min_size" mapstructure:"min_size"`
	MaxSize        int `yaml:"max_size" mapstructure:"max_size"`
	AcquireTimeout int `yaml:"acquire_timeout_ms" mapstructure:"acquire_timeout_ms"`
}

type fileModelsConfig struct {
	AutoDownload *bool  `yaml:"auto_download" mapstructure:"auto_download"`
	GitRepo      string `yaml:"git_repo" mapstructure:"git_repo"`
	GitRef       string `yaml:"git_ref" mapstructure:"git_ref"`
	TargetDir    string `yaml:"target_dir" mapstructure:"target_dir"`
}

type fileAudioFilteringConfig struct {
	EnergyThreshold float64 `yaml:"energy_threshold" mapstructure:"energy_threshold"`
	MinDuration     float64 `yaml:"min_duration" mapstructure:"min_duration"`
}

type fileSpeakerConfig struct {
	Enabled         *bool                     `yaml:"enabled" mapstructure:"enabled"`
	ModelPath       string                    `yaml:"model_path" mapstructure:"model_path"`
	SampleRate      int                       `yaml:"sample_rate" mapstructure:"sample_rate"`
	NumThreads      int                       `yaml:"num_threads" mapstructure:"num_threads"`
	Provider        string                    `yaml:"provider" mapstructure:"provider"`
	Threshold       float64                   `yaml:"threshold" mapstructure:"threshold"`
	MinDuration     float64                   `yaml:"min_duration" mapstructure:"min_duration"`
	EnergyThreshold float64                   `yaml:"energy_threshold" mapstructure:"energy_threshold"`
	DataDir         string                    `yaml:"data_dir" mapstructure:"data_dir"`
	VectorDB        fileSpeakerVectorDBConfig `yaml:"vector_db" mapstructure:"vector_db"`
}

type fileSpeakerVectorDBConfig struct {
	Provider string                   `yaml:"provider" mapstructure:"provider"`
	Chromem  fileSpeakerChromemConfig `yaml:"chromem" mapstructure:"chromem"`
}

type fileSpeakerChromemConfig struct {
	Path     string `yaml:"path" mapstructure:"path"`
	Compress *bool  `yaml:"compress" mapstructure:"compress"`
}

type fileLogConfig struct {
	Level  string             `yaml:"level" mapstructure:"level"`
	Stdout *bool              `yaml:"stdout" mapstructure:"stdout"`
	File   *fileLogFileConfig `yaml:"file" mapstructure:"file"`
}

type fileLogFileConfig struct {
	Enabled    *bool  `yaml:"enabled" mapstructure:"enabled"`
	Path       string `yaml:"path" mapstructure:"path"`
	Name       string `yaml:"name" mapstructure:"name"`
	MaxSizeMB  int    `yaml:"max_size_mb" mapstructure:"max_size_mb"`
	MaxBackups int    `yaml:"max_backups" mapstructure:"max_backups"`
	MaxAgeDays int    `yaml:"max_age_days" mapstructure:"max_age_days"`
	Compress   *bool  `yaml:"compress" mapstructure:"compress"`
}

const (
	defaultModelGitRepo = "https://www.modelscope.cn/gomodels/sherpa-small.git"
	envPrefix           = "SPEECH_"
)

var envKeySanitizer = strings.NewReplacer("-", "_", ".", "_", "/", "_")

func loadConfigFile(configPath string) (fileConfig, string, error) {
	v := viper.New()
	v.SetConfigType("yaml")
	if err := v.ReadConfig(bytes.NewReader(config.Default)); err != nil {
		return fileConfig{}, "", fmt.Errorf("load embedded config: %w", err)
	}
	baseDir := "."
	if strings.TrimSpace(configPath) != "" && fileExists(configPath) {
		absPath, err := filepath.Abs(configPath)
		if err != nil {
			return fileConfig{}, "", fmt.Errorf("resolve config path: %w", err)
		}
		v.SetConfigFile(absPath)
		if err := v.MergeInConfig(); err != nil {
			return fileConfig{}, "", fmt.Errorf("merge config %s: %w", absPath, err)
		}
		baseDir = filepath.Dir(absPath)
	}
	v.SetEnvPrefix(strings.TrimSuffix(envPrefix, "_"))
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()
	var fc fileConfig
	if err := v.Unmarshal(&fc, func(dc *mapstructure.DecoderConfig) {
		dc.TagName = "mapstructure"
		dc.ZeroFields = true
	}); err != nil {
		return fileConfig{}, "", fmt.Errorf("unmarshal config: %w", err)
	}
	return fc, baseDir, nil
}

func writeDefaultConfig(path string) error {
	if strings.TrimSpace(path) == "" || len(config.Default) == 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	if err := os.WriteFile(path, config.Default, 0o644); err != nil {
		return fmt.Errorf("write default config: %w", err)
	}
	return nil
}

func mergeFileConfig(cfg *Config, fc *fileConfig, baseDir string) error {
	if cfg == nil || fc == nil {
		return nil
	}

	if fc.Server != nil {
		if strings.TrimSpace(fc.Server.Addr) != "" {
			cfg.Addr = fc.Server.Addr
		}
	}

	if fc.Log != nil {
		if lvl := strings.TrimSpace(fc.Log.Level); lvl != "" {
			cfg.Log.Level = lvl
		}
		if fc.Log.Stdout != nil {
			cfg.Log.Stdout = *fc.Log.Stdout
		}
		if fc.Log.File != nil {
			if fc.Log.File.Enabled != nil {
				cfg.Log.File.Enabled = *fc.Log.File.Enabled
			}
			if strings.TrimSpace(fc.Log.File.Path) != "" {
				cfg.Log.File.Path = strings.TrimSpace(fc.Log.File.Path)
			}
			if strings.TrimSpace(fc.Log.File.Name) != "" {
				cfg.Log.File.Name = strings.TrimSpace(fc.Log.File.Name)
			}
			if fc.Log.File.MaxSizeMB > 0 {
				cfg.Log.File.MaxSizeMB = fc.Log.File.MaxSizeMB
			}
			if fc.Log.File.MaxBackups > 0 {
				cfg.Log.File.MaxBackups = fc.Log.File.MaxBackups
			}
			if fc.Log.File.MaxAgeDays > 0 {
				cfg.Log.File.MaxAgeDays = fc.Log.File.MaxAgeDays
			}
			if fc.Log.File.Compress != nil {
				cfg.Log.File.Compress = *fc.Log.File.Compress
			}
		}
	}

	if fc.ASR != nil {
		if cfg.ASRModels == nil {
			cfg.ASRModels = make(map[string]asr.Config)
		}
		for name, model := range fc.ASR.Models {
			cfg.ASRModels[name] = buildASRModel(model, baseDir, fc.ASR.LanguageGuard)
		}
		if strings.TrimSpace(fc.ASR.Provider) != "" {
			cfg.ASRProvider = fc.ASR.Provider
		}
	}

	if fc.TTS != nil {
		if cfg.TTSModels == nil {
			cfg.TTSModels = make(map[string]TTSModel)
		}
		for name, model := range fc.TTS.Models {
			m, err := buildTTSModel(model, baseDir)
			if err != nil {
				return fmt.Errorf("tts model %q: %w", name, err)
			}
			cfg.TTSModels[name] = m
		}
		if strings.TrimSpace(fc.TTS.Provider) != "" {
			cfg.TTSProvider = fc.TTS.Provider
		}
	}

	if fc.VAD != nil {
		cfg.VADEnabled = fc.VAD.Enabled
		if cfg.VADModels == nil {
			cfg.VADModels = make(map[string]asr.VADModel)
		}
		for name, model := range fc.VAD.Models {
			built, err := buildVADModel(model, baseDir)
			if err != nil {
				return fmt.Errorf("vad model %q: %w", name, err)
			}
			built.Name = name
			cfg.VADModels[name] = built
		}
		if strings.TrimSpace(fc.VAD.Provider) != "" {
			cfg.VADProvider = strings.TrimSpace(fc.VAD.Provider)
		}
		if fc.VAD.Pool != nil {
			cfg.VADPool.MinSize = fc.VAD.Pool.MinSize
			cfg.VADPool.MaxSize = fc.VAD.Pool.MaxSize
			if fc.VAD.Pool.AcquireTimeout > 0 {
				cfg.VADPool.AcquireTimeout = time.Duration(fc.VAD.Pool.AcquireTimeout) * time.Millisecond
			}
		}
	}

	if fc.Models != nil {
		if fc.Models.AutoDownload != nil {
			cfg.ModelBootstrap.AutoDownload = *fc.Models.AutoDownload
		}
		if strings.TrimSpace(fc.Models.GitRepo) != "" {
			cfg.ModelBootstrap.GitRepo = strings.TrimSpace(fc.Models.GitRepo)
		}
		if strings.TrimSpace(fc.Models.GitRef) != "" {
			cfg.ModelBootstrap.GitRef = strings.TrimSpace(fc.Models.GitRef)
		}
		if strings.TrimSpace(fc.Models.TargetDir) != "" {
			cfg.ModelBootstrap.TargetDir = strings.TrimSpace(fc.Models.TargetDir)
		}
	}

	if fc.AudioFiltering != nil {
		if fc.AudioFiltering.EnergyThreshold > 0 {
			cfg.AudioEnergyThreshold = float32(fc.AudioFiltering.EnergyThreshold)
		}
		if fc.AudioFiltering.MinDuration > 0 {
			cfg.AudioMinDuration = float32(fc.AudioFiltering.MinDuration)
		}
	}

	if fc.Speaker != nil {
		if fc.Speaker.Enabled != nil {
			cfg.Speaker.Enabled = *fc.Speaker.Enabled
		}
		if strings.TrimSpace(fc.Speaker.ModelPath) != "" {
			cfg.Speaker.ModelPath = resolvePath(baseDir, fc.Speaker.ModelPath)
		}
		if fc.Speaker.SampleRate > 0 {
			cfg.Speaker.SampleRate = fc.Speaker.SampleRate
		}
		if fc.Speaker.NumThreads > 0 {
			cfg.Speaker.NumThreads = fc.Speaker.NumThreads
		}
		if strings.TrimSpace(fc.Speaker.Provider) != "" {
			cfg.Speaker.Provider = strings.TrimSpace(fc.Speaker.Provider)
		}
		if fc.Speaker.Threshold > 0 {
			cfg.Speaker.Threshold = float32(fc.Speaker.Threshold)
		}
		if fc.Speaker.MinDuration > 0 {
			cfg.Speaker.MinDuration = float32(fc.Speaker.MinDuration)
		}
		if fc.Speaker.EnergyThreshold > 0 {
			cfg.Speaker.EnergyThreshold = float32(fc.Speaker.EnergyThreshold)
		}
		if strings.TrimSpace(fc.Speaker.DataDir) != "" {
			cfg.Speaker.DataDir = resolvePath(baseDir, fc.Speaker.DataDir)
		}
		if strings.TrimSpace(fc.Speaker.VectorDB.Provider) != "" {
			cfg.Speaker.VectorDB.Provider = strings.TrimSpace(fc.Speaker.VectorDB.Provider)
		}
		if strings.TrimSpace(fc.Speaker.VectorDB.Chromem.Path) != "" {
			cfg.Speaker.VectorDB.Chromem.Path = resolvePath(baseDir, fc.Speaker.VectorDB.Chromem.Path)
		}
		if fc.Speaker.VectorDB.Chromem.Compress != nil {
			cfg.Speaker.VectorDB.Chromem.Compress = *fc.Speaker.VectorDB.Chromem.Compress
		}
	}

	ensureModelDefaults(cfg)

	return nil
}

func buildASRModel(model fileASRModel, baseDir string, inheritedGuard *fileASRLanguageGuard) asr.Config {
	threads := model.Threads
	if threads <= 0 {
		threads = runtime.NumCPU()
		if threads < 1 {
			threads = 1
		}
	}

	cfg := asr.Config{
		ModelDir:       resolvePath(baseDir, model.ModelDir),
		ModelType:      strings.TrimSpace(model.ModelType),
		ModelVariant:   strings.TrimSpace(model.ModelVariant),
		NumThreads:     threads,
		Provider:       strings.TrimSpace(model.Provider),
		Debug:          model.Debug,
		SampleRate:     model.SampleRate,
		MaxActivePaths: model.MaxActivePaths,
		EnableEndpoint: true,
		DecodingMethod: strings.TrimSpace(model.DecodingMethod),
		Language:       strings.TrimSpace(model.Language),
		UseITN:         true,
		LanguageGuard: asr.LanguageGuardConfig{
			MaxMismatchDuration:   1.0,
			MaxMismatchCharacters: 8,
		},
	}
	if model.EnableEndpoint != nil {
		cfg.EnableEndpoint = *model.EnableEndpoint
	}
	cfg.Provider = detectDefaultASRProvider(cfg.Provider)
	guard := inheritedGuard
	if model.LanguageGuard != nil {
		guard = model.LanguageGuard
	}
	if guard != nil {
		cfg.LanguageGuard.Enabled = guard.Enabled
		cfg.LanguageGuard.DefaultHint = strings.TrimSpace(guard.DefaultHint)
		if guard.MaxMismatchDuration > 0 {
			cfg.LanguageGuard.MaxMismatchDuration = guard.MaxMismatchDuration
		}
		if guard.MaxMismatchCharacters > 0 {
			cfg.LanguageGuard.MaxMismatchCharacters = guard.MaxMismatchCharacters
		}
	}
	if cfg.Debug < 0 {
		cfg.Debug = 0
	}
	if cfg.SampleRate <= 0 {
		cfg.SampleRate = 16000
	}
	if cfg.MaxActivePaths <= 0 {
		cfg.MaxActivePaths = 8
	}
	if model.UseITN != nil {
		cfg.UseITN = *model.UseITN
	}
	return cfg
}

func buildTTSModel(model fileTTSModel, baseDir string) (TTSModel, error) {
	voiceMap := parseVoiceMap(model.VoiceMap)
	defaultVoice := normalizeVoice(openairt.Voice(model.DefaultVoice))
	if defaultVoice == "" {
		defaultVoice = normalizeVoice(openairt.VoiceAlloy)
	}
	defaultSpeed := float32(model.DefaultSpeed)
	if defaultSpeed <= 0 {
		defaultSpeed = 1.0
	}

	threads := model.Threads
	if threads <= 0 {
		threads = runtime.NumCPU()
		if threads < 1 {
			threads = 1
		}
	}

	engine := strings.TrimSpace(model.Engine)
	if engine == "" {
		engine = "matcha"
	}

	tCfg := tts.Config{
		Engine:      engine,
		NumThreads:  threads,
		Provider:    strings.TrimSpace(model.Provider),
		DebugLevel:  model.Debug,
		EnableCN2AN: model.CN2ANEnabled,
		LengthScale: func() float32 {
			if model.LengthScale == 0 {
				return 1.0
			}
			return float32(model.LengthScale)
		}(),
	}

	switch strings.ToLower(engine) {
	case "kokoro":
		tCfg.KokoroModel = resolvePath(baseDir, model.KokoroModel)
		tCfg.KokoroVoices = resolvePath(baseDir, model.KokoroVoices)
		tCfg.KokoroTokens = resolvePath(baseDir, model.KokoroTokens)
		tCfg.KokoroDataDir = resolvePath(baseDir, model.KokoroDataDir)
		tCfg.KokoroLexicon = resolvePath(baseDir, model.KokoroLexicon)
		tCfg.KokoroLang = strings.TrimSpace(model.KokoroLang)
	case "matcha":
		tCfg.AcousticModel = resolvePath(baseDir, model.AcousticModel)
		tCfg.Vocoder = resolvePath(baseDir, model.VocoderModel)
		tCfg.Lexicon = resolvePath(baseDir, model.Lexicon)
		tCfg.Tokens = resolvePath(baseDir, model.Tokens)
		tCfg.DataDir = resolvePath(baseDir, model.EspeakDir)
		tCfg.NoiseScale = float32(model.NoiseScale)
	case "zipvoice":
		tCfg.ZipvoiceTokens = resolvePath(baseDir, model.ZipvoiceTokens)
		tCfg.ZipvoiceTextModel = resolvePath(baseDir, model.ZipvoiceTextModel)
		tCfg.ZipvoiceFlowMatchingModel = resolvePath(baseDir, model.ZipvoiceFlowMatchingModel)
		tCfg.ZipvoiceDataDir = resolvePath(baseDir, model.ZipvoiceDataDir)
		tCfg.ZipvoicePinyinDict = resolvePath(baseDir, model.ZipvoicePinyinDict)
		tCfg.ZipvoiceVocoder = resolvePath(baseDir, model.ZipvoiceVocoder)
		tCfg.ZipvoiceFeatScale = float32(model.ZipvoiceFeatScale)
		tCfg.ZipvoiceTShift = float32(model.ZipvoiceTShift)
		tCfg.ZipvoiceTargetRms = float32(model.ZipvoiceTargetRms)
		tCfg.ZipvoiceGuidanceScale = float32(model.ZipvoiceGuidanceScale)
		tCfg.ZipvoiceNumSteps = model.ZipvoiceNumSteps
		tCfg.ZipvoicePromptAudio = resolvePath(baseDir, model.ZipvoicePromptAudio)
		tCfg.ZipvoicePromptText = strings.TrimSpace(model.ZipvoicePromptText)

		if len(model.ZipvoicePrompts) > 0 {
			prompts := make(map[int]tts.ZipvoicePromptConfig, len(model.ZipvoicePrompts))
			for key, prompt := range model.ZipvoicePrompts {
				key = strings.TrimSpace(key)
				if key == "" {
					continue
				}

				speakerID, err := strconv.Atoi(key)
				if err != nil {
					voiceKey := normalizeVoice(openairt.Voice(key))
					if voiceKey == "" {
						return TTSModel{}, fmt.Errorf("invalid zipvoice prompt key %q", key)
					}
					val, ok := voiceMap[voiceKey]
					if !ok {
						return TTSModel{}, fmt.Errorf("zipvoice prompt voice %q not found in voice_map", key)
					}
					speakerID = val
				}

				if _, exists := prompts[speakerID]; exists {
					return TTSModel{}, fmt.Errorf("zipvoice prompt duplicated for speaker id %d", speakerID)
				}

				prompts[speakerID] = tts.ZipvoicePromptConfig{
					PromptAudio: resolvePath(baseDir, prompt.PromptAudio),
					PromptText:  strings.TrimSpace(prompt.PromptText),
					NumSteps:    prompt.NumSteps,
				}
			}
			tCfg.ZipvoicePrompts = prompts
		}
	default:
		return TTSModel{}, fmt.Errorf("unsupported tts engine %q", engine)
	}

	if tCfg.NumThreads <= 0 {
		tCfg.NumThreads = 1
	}
	if tCfg.Provider == "" {
		tCfg.Provider = "cpu"
	}
	if tCfg.DebugLevel < 0 {
		tCfg.DebugLevel = 0
	}
	if strings.ToLower(engine) == "matcha" {
		if tCfg.NoiseScale == 0 {
			tCfg.NoiseScale = 0.667
		}
	}

	return TTSModel{
		Config:       tCfg,
		VoiceMap:     voiceMap,
		DefaultVoice: defaultVoice,
		DefaultSpeed: defaultSpeed,
	}, nil
}

func buildVADModel(model fileVADModel, baseDir string) (asr.VADModel, error) {
	provider := strings.TrimSpace(model.Provider)
	if provider == "" {
		provider = asr.VADProviderSilero
	}

	modelPath := firstNonEmpty(
		model.Model,
		model.ModelPath,
		model.ModelFile,
		model.ModelDir,
	)
	modelPath = strings.TrimSpace(modelPath)
	if modelPath == "" {
		return asr.VADModel{}, errors.New("vad model path is required")
	}

	threads := model.Threads
	if threads <= 0 {
		threads = runtime.NumCPU()
		if threads < 1 {
			threads = 1
		}
	}

	cfg := asr.VADModel{
		Provider:          provider,
		ModelPath:         resolvePath(baseDir, modelPath),
		Threshold:         float32(model.Threshold),
		MinSilenceSeconds: float32(model.MinSilence),
		MinSpeechSeconds:  float32(model.MinSpeech),
		MaxSpeechSeconds:  float32(model.MaxSpeech),
		WindowSize:        model.WindowSize,
		SampleRate:        model.SampleRate,
		NumThreads:        threads,
		Device:            strings.TrimSpace(model.Device),
		Debug:             model.Debug,
		BufferSeconds:     float32(model.BufferSeconds),
	}
	return cfg, nil
}

func ensureModelDefaults(cfg *Config) {
	if cfg == nil {
		return
	}
	if strings.TrimSpace(cfg.ModelBootstrap.TargetDir) == "" {
		cfg.ModelBootstrap.TargetDir = "./models"
	}
	if strings.TrimSpace(cfg.ModelBootstrap.GitRepo) == "" {
		cfg.ModelBootstrap.GitRepo = defaultModelGitRepo
	}
	if cfg.Speaker.SampleRate <= 0 {
		cfg.Speaker.SampleRate = 16000
	}
	if cfg.Speaker.Threshold <= 0 {
		cfg.Speaker.Threshold = 0.65
	}
	if cfg.Speaker.MinDuration <= 0 {
		cfg.Speaker.MinDuration = 0.4
	}
	if cfg.Speaker.EnergyThreshold <= 0 {
		cfg.Speaker.EnergyThreshold = 0.003
	}
	if strings.TrimSpace(cfg.Speaker.VectorDB.Provider) == "" {
		cfg.Speaker.VectorDB.Provider = "chromem"
	}
	if strings.TrimSpace(cfg.Speaker.VectorDB.Chromem.Path) == "" {
		cfg.Speaker.VectorDB.Chromem.Path = "./data/speaker_index"
	}
	if strings.TrimSpace(cfg.Speaker.ModelPath) == "" {
		cfg.Speaker.ModelPath = "./models/speaker/3dspeaker_speech_campplus_sv_zh_en_16k-common_advanced.onnx"
	}
	if strings.TrimSpace(cfg.Speaker.Provider) == "" {
		cfg.Speaker.Provider = "cpu"
	}
	if strings.TrimSpace(cfg.Speaker.DataDir) == "" {
		cfg.Speaker.DataDir = "./data/speaker"
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func detectDefaultASRProvider(explicit string) string {
	trimmed := strings.TrimSpace(explicit)
	if trimmed != "" {
		return trimmed
	}
	if hasCUDAHardware() {
		return "cuda"
	}
	if runtime.GOOS == "darwin" && runtime.GOARCH == "arm64" {
		return "coreml"
	}
	return "cpu"
}

func hasCUDAHardware() bool {
	if devs := strings.TrimSpace(os.Getenv("CUDA_VISIBLE_DEVICES")); devs != "" && devs != "-1" {
		return true
	}
	if _, err := os.Stat("/dev/nvidiactl"); err == nil {
		return true
	}
	if _, err := os.Stat("/proc/driver/nvidia/version"); err == nil {
		return true
	}
	return false
}

func resolvePath(baseDir, path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return ""
	}

	effectiveBase := baseDir
	if strings.HasPrefix(trimmed, "./") {
		trimmed = strings.TrimPrefix(trimmed, "./")
		if wd, err := os.Getwd(); err == nil {
			effectiveBase = wd
		}
	}

	if filepath.IsAbs(trimmed) {
		return filepath.Clean(trimmed)
	}
	if effectiveBase == "" {
		return filepath.Clean(trimmed)
	}
	return filepath.Clean(filepath.Join(effectiveBase, trimmed))
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func parseVoiceMap(raw string) map[openairt.Voice]int {
	result := make(map[openairt.Voice]int)
	if raw == "" {
		return result
	}
	pairs := strings.Split(raw, ",")
	for _, pair := range pairs {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		parts := strings.SplitN(pair, ":", 2)
		if len(parts) != 2 {
			log.Printf("invalid voice mapping entry %q", pair)
			continue
		}
		name := strings.TrimSpace(parts[0])
		if name == "" {
			continue
		}
		idStr := strings.TrimSpace(parts[1])
		id, err := strconv.Atoi(idStr)
		if err != nil {
			log.Printf("invalid speaker id %q for voice %s: %v", idStr, name, err)
			continue
		}
		voice := normalizeVoice(openairt.Voice(name))
		if voice == "" {
			continue
		}
		result[voice] = id
	}
	return result
}

func applyConfigOverrides(cfg *Config, addrOverride string) {
	if strings.TrimSpace(addrOverride) != "" {
		cfg.Addr = addrOverride
	}
}

func ensurePrimaryProviders(cfg *Config) error {
	if len(cfg.ASRModels) == 0 {
		return errors.New("no ASR models configured")
	}
	if strings.TrimSpace(cfg.ASRProvider) == "" {
		if len(cfg.ASRModels) == 1 {
			for name := range cfg.ASRModels {
				cfg.ASRProvider = name
			}
		} else {
			return errors.New("asr.provider is required when multiple models are defined")
		}
	}
	if _, ok := cfg.ASRModels[cfg.ASRProvider]; !ok {
		return fmt.Errorf("asr provider %q not found", cfg.ASRProvider)
	}

	if strings.TrimSpace(cfg.TTSProvider) == "" {
		if len(cfg.TTSModels) == 1 {
			for name, model := range cfg.TTSModels {
				if hasUsableTTSConfig(model.Config) {
					cfg.TTSProvider = name
					break
				}
			}
		}
	} else {
		cfg.TTSProvider = strings.TrimSpace(cfg.TTSProvider)
	}

	if cfg.TTSProvider != "" {
		if _, ok := cfg.TTSModels[cfg.TTSProvider]; !ok {
			return fmt.Errorf("tts provider %q not found", cfg.TTSProvider)
		}
	}

	if cfg.VADEnabled {
		if len(cfg.VADModels) == 0 {
			return errors.New("vad enabled but no models configured")
		}
		cfg.VADProvider = strings.TrimSpace(cfg.VADProvider)
		if cfg.VADProvider == "" {
			for name := range cfg.VADModels {
				cfg.VADProvider = name
				break
			}
		}
		if cfg.VADProvider != "" {
			if _, ok := cfg.VADModels[cfg.VADProvider]; !ok {
				return fmt.Errorf("vad provider %q not found", cfg.VADProvider)
			}
		}
	}
	return nil
}

func validateConfig(cfg *Config) error {
	model, ok := cfg.ASRModels[cfg.ASRProvider]
	if !ok {
		return fmt.Errorf("asr provider %q not found", cfg.ASRProvider)
	}
	if strings.TrimSpace(model.ModelDir) == "" {
		return errors.New("asr model directory is required")
	}
	if !fileExists(model.ModelDir) && !cfg.ModelBootstrap.AutoDownload {
		return fmt.Errorf("asr model directory %q not found", model.ModelDir)
	}

	if cfg.TTSProvider != "" {
		modelCfg, ok := cfg.TTSModels[cfg.TTSProvider]
		if !ok {
			return fmt.Errorf("tts provider %q not found", cfg.TTSProvider)
		}
		tCfg := modelCfg.Config
		engine := strings.ToLower(strings.TrimSpace(tCfg.Engine))
		switch engine {
		case "", "matcha":
			if strings.TrimSpace(tCfg.AcousticModel) == "" ||
				strings.TrimSpace(tCfg.Vocoder) == "" ||
				strings.TrimSpace(tCfg.Lexicon) == "" ||
				strings.TrimSpace(tCfg.Tokens) == "" {
				return errors.New("matcha tts provider requires acoustic_model, vocoder_model, lexicon, and tokens")
			}
		case "kokoro":
			if strings.TrimSpace(tCfg.KokoroModel) == "" ||
				strings.TrimSpace(tCfg.KokoroVoices) == "" ||
				strings.TrimSpace(tCfg.KokoroTokens) == "" {
				return errors.New("kokoro tts provider requires kokoro_model, kokoro_voices, and kokoro_tokens")
			}
		case "zipvoice":
			if strings.TrimSpace(tCfg.ZipvoiceTokens) == "" ||
				strings.TrimSpace(tCfg.ZipvoiceTextModel) == "" ||
				strings.TrimSpace(tCfg.ZipvoiceFlowMatchingModel) == "" ||
				strings.TrimSpace(tCfg.ZipvoiceDataDir) == "" ||
				strings.TrimSpace(tCfg.ZipvoiceVocoder) == "" {
				return errors.New("zipvoice tts provider requires zipvoice_tokens, zipvoice_text_model, zipvoice_flow_matching_model, zipvoice_data_dir, and zipvoice_vocoder")
			}
			if strings.TrimSpace(tCfg.ZipvoicePromptAudio) != "" &&
				strings.TrimSpace(tCfg.ZipvoicePromptText) == "" {
				return errors.New("zipvoice prompt_text is required when prompt_audio is set")
			}
			for id, prompt := range tCfg.ZipvoicePrompts {
				if strings.TrimSpace(prompt.PromptAudio) == "" {
					return fmt.Errorf("zipvoice prompt %d is missing audio path", id)
				}
				if strings.TrimSpace(prompt.PromptText) == "" {
					return fmt.Errorf("zipvoice prompt %d is missing prompt text", id)
				}
			}
		default:
			return fmt.Errorf("unsupported tts engine %q", engine)
		}
	}

	if cfg.VADEnabled {
		modelCfg, ok := cfg.VADModels[cfg.VADProvider]
		if !ok {
			return fmt.Errorf("vad provider %q not found", cfg.VADProvider)
		}
		if strings.TrimSpace(modelCfg.ModelPath) == "" {
			return errors.New("vad model path is required")
		}
		if !fileExists(modelCfg.ModelPath) && !cfg.ModelBootstrap.AutoDownload {
			return fmt.Errorf("vad model path %q not found", modelCfg.ModelPath)
		}
		if cfg.VADPool.MaxSize <= 0 {
			cfg.VADPool.MaxSize = 4
		}
		if cfg.VADPool.MinSize < 0 {
			cfg.VADPool.MinSize = 0
		}
		if cfg.VADPool.MinSize > cfg.VADPool.MaxSize {
			cfg.VADPool.MinSize = cfg.VADPool.MaxSize
		}
		if cfg.VADPool.AcquireTimeout <= 0 {
			cfg.VADPool.AcquireTimeout = 3 * time.Second
		}
	}

	if cfg.ModelBootstrap.AutoDownload && strings.TrimSpace(cfg.ModelBootstrap.GitRepo) == "" {
		return errors.New("models.git_repo is required when auto_download is true")
	}

	if cfg.Speaker.Enabled {
		if cfg.Speaker.SampleRate <= 0 {
			cfg.Speaker.SampleRate = 16000
		}
		if cfg.Speaker.MinDuration <= 0 {
			cfg.Speaker.MinDuration = 0.4
		}
		if cfg.Speaker.EnergyThreshold <= 0 {
			cfg.Speaker.EnergyThreshold = 0.003
		}
		if cfg.Speaker.Threshold <= 0 {
			cfg.Speaker.Threshold = 0.65
		}
		if strings.TrimSpace(cfg.Speaker.ModelPath) == "" {
			log.Printf("speaker enabled but model_path is empty, feature will be disabled at runtime")
		} else if !fileExists(cfg.Speaker.ModelPath) && !cfg.ModelBootstrap.AutoDownload {
			log.Printf("speaker model %q not found; will attempt to start without speaker", cfg.Speaker.ModelPath)
		}
		if strings.TrimSpace(cfg.Speaker.VectorDB.Provider) == "" {
			cfg.Speaker.VectorDB.Provider = "chromem"
		}
		if strings.TrimSpace(cfg.Speaker.VectorDB.Chromem.Path) == "" {
			cfg.Speaker.VectorDB.Chromem.Path = "./data/speaker_index"
		}
	}

	return nil
}

func hasUsableTTSConfig(cfg tts.Config) bool {
	switch strings.ToLower(strings.TrimSpace(cfg.Engine)) {
	case "kokoro":
		return strings.TrimSpace(cfg.KokoroModel) != "" &&
			strings.TrimSpace(cfg.KokoroVoices) != "" &&
			strings.TrimSpace(cfg.KokoroTokens) != ""
	case "zipvoice":
		return strings.TrimSpace(cfg.ZipvoiceTokens) != "" &&
			strings.TrimSpace(cfg.ZipvoiceTextModel) != "" &&
			strings.TrimSpace(cfg.ZipvoiceFlowMatchingModel) != "" &&
			strings.TrimSpace(cfg.ZipvoiceDataDir) != "" &&
			strings.TrimSpace(cfg.ZipvoiceVocoder) != ""
	case "matcha", "":
		return strings.TrimSpace(cfg.AcousticModel) != "" &&
			strings.TrimSpace(cfg.Vocoder) != "" &&
			strings.TrimSpace(cfg.Lexicon) != "" &&
			strings.TrimSpace(cfg.Tokens) != ""
	default:
		return false
	}
}

func applyEnvOverrides(cfg *Config) {
	if cfg == nil {
		return
	}
	if v, ok := lookupEnvString(envPrefix + "SERVER_ADDR"); ok {
		cfg.Addr = v
	}
	if v, ok := lookupEnvString(envPrefix + "ASR_PROVIDER"); ok {
		cfg.ASRProvider = v
	}
	for name, model := range cfg.ASRModels {
		prefix := envPrefix + "ASR_MODELS_" + sanitizeEnvSegment(name) + "_"
		if v, ok := lookupEnvString(prefix + "MODEL_DIR"); ok {
			model.ModelDir = v
		}
		if v, ok := lookupEnvString(prefix + "MODEL_TYPE"); ok {
			model.ModelType = v
		}
		if n, ok := lookupEnvInt(prefix + "THREADS"); ok {
			model.NumThreads = n
		}
		if v, ok := lookupEnvString(prefix + "PROVIDER"); ok {
			model.Provider = v
		}
		if n, ok := lookupEnvInt(prefix + "DEBUG_LEVEL"); ok {
			model.Debug = n
		}
		if n, ok := lookupEnvInt(prefix + "SAMPLE_RATE"); ok {
			model.SampleRate = n
		}
		if n, ok := lookupEnvInt(prefix + "MAX_ACTIVE_PATHS"); ok {
			model.MaxActivePaths = n
		}
		if b, ok := lookupEnvBool(prefix + "ENABLE_ENDPOINT"); ok {
			model.EnableEndpoint = b
		}
		cfg.ASRModels[name] = model
	}
	if v, ok := lookupEnvString(envPrefix + "TTS_PROVIDER"); ok {
		cfg.TTSProvider = v
	}
	for name, model := range cfg.TTSModels {
		prefix := envPrefix + "TTS_MODELS_" + sanitizeEnvSegment(name) + "_"
		tCfg := model.Config
		if v, ok := lookupEnvString(prefix + "ENGINE"); ok {
			tCfg.Engine = v
		}
		if v, ok := lookupEnvString(prefix + "ACOUSTIC_MODEL"); ok {
			tCfg.AcousticModel = v
		}
		if v, ok := lookupEnvString(prefix + "VOCODER_MODEL"); ok {
			tCfg.Vocoder = v
		}
		if v, ok := lookupEnvString(prefix + "LEXICON"); ok {
			tCfg.Lexicon = v
		}
		if v, ok := lookupEnvString(prefix + "TOKENS"); ok {
			tCfg.Tokens = v
		}
		if v, ok := lookupEnvString(prefix + "ESPEAK_DIR"); ok {
			tCfg.DataDir = v
		}
		if v, ok := lookupEnvString(prefix + "KOKORO_MODEL"); ok {
			tCfg.KokoroModel = v
		}
		if v, ok := lookupEnvString(prefix + "KOKORO_VOICES"); ok {
			tCfg.KokoroVoices = v
		}
		if v, ok := lookupEnvString(prefix + "KOKORO_TOKENS"); ok {
			tCfg.KokoroTokens = v
		}
		if v, ok := lookupEnvString(prefix + "KOKORO_DATA_DIR"); ok {
			tCfg.KokoroDataDir = v
		}
		if v, ok := lookupEnvString(prefix + "KOKORO_LEXICON"); ok {
			tCfg.KokoroLexicon = v
		}
		if v, ok := lookupEnvString(prefix + "KOKORO_LANG"); ok {
			tCfg.KokoroLang = v
		}
		if v, ok := lookupEnvString(prefix + "ZIPVOICE_TOKENS"); ok {
			tCfg.ZipvoiceTokens = v
		}
		if v, ok := lookupEnvString(prefix + "ZIPVOICE_TEXT_MODEL"); ok {
			tCfg.ZipvoiceTextModel = v
		}
		if v, ok := lookupEnvString(prefix + "ZIPVOICE_FLOW_MATCHING_MODEL"); ok {
			tCfg.ZipvoiceFlowMatchingModel = v
		}
		if v, ok := lookupEnvString(prefix + "ZIPVOICE_DATA_DIR"); ok {
			tCfg.ZipvoiceDataDir = v
		}
		if v, ok := lookupEnvString(prefix + "ZIPVOICE_PINYIN_DICT"); ok {
			tCfg.ZipvoicePinyinDict = v
		}
		if v, ok := lookupEnvString(prefix + "ZIPVOICE_VOCODER"); ok {
			tCfg.ZipvoiceVocoder = v
		}
		if f, ok := lookupEnvFloat(prefix + "ZIPVOICE_FEAT_SCALE"); ok {
			tCfg.ZipvoiceFeatScale = float32(f)
		}
		if f, ok := lookupEnvFloat(prefix + "ZIPVOICE_T_SHIFT"); ok {
			tCfg.ZipvoiceTShift = float32(f)
		}
		if f, ok := lookupEnvFloat(prefix + "ZIPVOICE_TARGET_RMS"); ok {
			tCfg.ZipvoiceTargetRms = float32(f)
		}
		if f, ok := lookupEnvFloat(prefix + "ZIPVOICE_GUIDANCE_SCALE"); ok {
			tCfg.ZipvoiceGuidanceScale = float32(f)
		}
		if n, ok := lookupEnvInt(prefix + "ZIPVOICE_NUM_STEPS"); ok {
			tCfg.ZipvoiceNumSteps = n
		}
		if v, ok := lookupEnvString(prefix + "ZIPVOICE_PROMPT_AUDIO"); ok {
			tCfg.ZipvoicePromptAudio = v
		}
		if v, ok := lookupEnvString(prefix + "ZIPVOICE_PROMPT_TEXT"); ok {
			tCfg.ZipvoicePromptText = v
		}
		if n, ok := lookupEnvInt(prefix + "THREADS"); ok {
			tCfg.NumThreads = n
		}
		if v, ok := lookupEnvString(prefix + "PROVIDER"); ok {
			tCfg.Provider = v
		}
		if n, ok := lookupEnvInt(prefix + "DEBUG_LEVEL"); ok {
			tCfg.DebugLevel = n
		}
		if f, ok := lookupEnvFloat(prefix + "NOISE_SCALE"); ok {
			tCfg.NoiseScale = float32(f)
		}
		if f, ok := lookupEnvFloat(prefix + "LENGTH_SCALE"); ok {
			tCfg.LengthScale = float32(f)
		}
		model.Config = tCfg
		if v, ok := lookupEnvString(prefix + "VOICE_MAP"); ok {
			model.VoiceMap = parseVoiceMap(v)
		}
		if v, ok := lookupEnvString(prefix + "DEFAULT_VOICE"); ok {
			model.DefaultVoice = normalizeVoice(openairt.Voice(v))
		}
		if f, ok := lookupEnvFloat(prefix + "DEFAULT_SPEED"); ok && f > 0 {
			model.DefaultSpeed = float32(f)
		}
		cfg.TTSModels[name] = model
	}
	if b, ok := lookupEnvBool(envPrefix + "VAD_ENABLED"); ok {
		cfg.VADEnabled = b
	}
	if v, ok := lookupEnvString(envPrefix + "VAD_PROVIDER"); ok {
		cfg.VADProvider = v
	}
	for name, model := range cfg.VADModels {
		prefix := envPrefix + "VAD_MODELS_" + sanitizeEnvSegment(name) + "_"
		if v, ok := lookupEnvString(prefix + "PROVIDER"); ok {
			model.Provider = v
		}
		if v, ok := lookupEnvString(prefix + "MODEL"); ok {
			model.ModelPath = resolvePath(".", v)
		}
		if v, ok := lookupEnvString(prefix + "MODEL_PATH"); ok {
			model.ModelPath = resolvePath(".", v)
		}
		if f, ok := lookupEnvFloat(prefix + "THRESHOLD"); ok {
			model.Threshold = float32(f)
		}
		if f, ok := lookupEnvFloat(prefix + "MIN_SILENCE"); ok {
			model.MinSilenceSeconds = float32(f)
		}
		if f, ok := lookupEnvFloat(prefix + "MIN_SPEECH"); ok {
			model.MinSpeechSeconds = float32(f)
		}
		if f, ok := lookupEnvFloat(prefix + "MAX_SPEECH"); ok {
			model.MaxSpeechSeconds = float32(f)
		}
		if n, ok := lookupEnvInt(prefix + "WINDOW_SIZE"); ok {
			model.WindowSize = n
		}
		if n, ok := lookupEnvInt(prefix + "SAMPLE_RATE"); ok {
			model.SampleRate = n
		}
		if n, ok := lookupEnvInt(prefix + "THREADS"); ok {
			model.NumThreads = n
		}
		if v, ok := lookupEnvString(prefix + "DEVICE"); ok {
			model.Device = v
		}
		if f, ok := lookupEnvFloat(prefix + "BUFFER_SECONDS"); ok {
			model.BufferSeconds = float32(f)
		}
		cfg.VADModels[name] = model
	}
	if n, ok := lookupEnvInt(envPrefix + "VAD_POOL_MIN_SIZE"); ok {
		cfg.VADPool.MinSize = n
	}
	if n, ok := lookupEnvInt(envPrefix + "VAD_POOL_MAX_SIZE"); ok {
		cfg.VADPool.MaxSize = n
	}
	if n, ok := lookupEnvInt(envPrefix + "VAD_POOL_ACQUIRE_TIMEOUT_MS"); ok {
		cfg.VADPool.AcquireTimeout = time.Duration(n) * time.Millisecond
	}
	if b, ok := lookupEnvBool(envPrefix + "MODELS_AUTO_DOWNLOAD"); ok {
		cfg.ModelBootstrap.AutoDownload = b
	}
	if v, ok := lookupEnvString(envPrefix + "MODELS_GIT_REPO"); ok {
		cfg.ModelBootstrap.GitRepo = v
	}
	if v, ok := lookupEnvString(envPrefix + "MODELS_GIT_REF"); ok {
		cfg.ModelBootstrap.GitRef = v
	}
	if v, ok := lookupEnvString(envPrefix + "MODELS_TARGET_DIR"); ok {
		cfg.ModelBootstrap.TargetDir = v
	}
	if v, ok := lookupEnvString(envPrefix + "LOG_LEVEL"); ok {
		cfg.Log.Level = v
	}
	if b, ok := lookupEnvBool(envPrefix + "LOG_STDOUT"); ok {
		cfg.Log.Stdout = b
	}
	if b, ok := lookupEnvBool(envPrefix + "LOG_FILE_ENABLED"); ok {
		cfg.Log.File.Enabled = b
	}
	if v, ok := lookupEnvString(envPrefix + "LOG_FILE_PATH"); ok {
		cfg.Log.File.Path = v
	}
	if v, ok := lookupEnvString(envPrefix + "LOG_FILE_NAME"); ok {
		cfg.Log.File.Name = v
	}
	if n, ok := lookupEnvInt(envPrefix + "LOG_FILE_MAX_SIZE_MB"); ok {
		cfg.Log.File.MaxSizeMB = n
	}
	if n, ok := lookupEnvInt(envPrefix + "LOG_FILE_MAX_BACKUPS"); ok {
		cfg.Log.File.MaxBackups = n
	}
	if n, ok := lookupEnvInt(envPrefix + "LOG_FILE_MAX_AGE_DAYS"); ok {
		cfg.Log.File.MaxAgeDays = n
	}
	if b, ok := lookupEnvBool(envPrefix + "LOG_FILE_COMPRESS"); ok {
		cfg.Log.File.Compress = b
	}
	if b, ok := lookupEnvBool(envPrefix + "SPEAKER_ENABLED"); ok {
		cfg.Speaker.Enabled = b
	}
	if v, ok := lookupEnvString(envPrefix + "SPEAKER_MODEL_PATH"); ok {
		cfg.Speaker.ModelPath = v
	}
	if n, ok := lookupEnvInt(envPrefix + "SPEAKER_SAMPLE_RATE"); ok && n > 0 {
		cfg.Speaker.SampleRate = n
	}
	if n, ok := lookupEnvInt(envPrefix + "SPEAKER_NUM_THREADS"); ok && n > 0 {
		cfg.Speaker.NumThreads = n
	}
	if v, ok := lookupEnvString(envPrefix + "SPEAKER_PROVIDER"); ok {
		cfg.Speaker.Provider = v
	}
	if f, ok := lookupEnvFloat(envPrefix + "SPEAKER_THRESHOLD"); ok && f > 0 {
		cfg.Speaker.Threshold = float32(f)
	}
	if f, ok := lookupEnvFloat(envPrefix + "SPEAKER_MIN_DURATION"); ok && f > 0 {
		cfg.Speaker.MinDuration = float32(f)
	}
	if f, ok := lookupEnvFloat(envPrefix + "SPEAKER_ENERGY_THRESHOLD"); ok && f > 0 {
		cfg.Speaker.EnergyThreshold = float32(f)
	}
	if v, ok := lookupEnvString(envPrefix + "SPEAKER_DATA_DIR"); ok {
		cfg.Speaker.DataDir = v
	}
	if v, ok := lookupEnvString(envPrefix + "SPEAKER_VECTOR_DB_PROVIDER"); ok {
		cfg.Speaker.VectorDB.Provider = v
	}
	if v, ok := lookupEnvString(envPrefix + "SPEAKER_VECTOR_DB_CHROMEM_PATH"); ok {
		cfg.Speaker.VectorDB.Chromem.Path = v
	}
	if b, ok := lookupEnvBool(envPrefix + "SPEAKER_VECTOR_DB_CHROMEM_COMPRESS"); ok {
		cfg.Speaker.VectorDB.Chromem.Compress = b
	}
	ensureModelDefaults(cfg)
}

func lookupEnvString(key string) (string, bool) {
	v, ok := os.LookupEnv(key)
	if !ok {
		return "", false
	}
	v = strings.TrimSpace(v)
	if v == "" {
		return "", false
	}
	return v, true
}

func lookupEnvInt(key string) (int, bool) {
	val, ok := lookupEnvString(key)
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(val)
	if err != nil {
		log.Printf("invalid integer value %q for %s", val, key)
		return 0, false
	}
	return n, true
}

func lookupEnvFloat(key string) (float64, bool) {
	val, ok := lookupEnvString(key)
	if !ok {
		return 0, false
	}
	f, err := strconv.ParseFloat(val, 64)
	if err != nil {
		log.Printf("invalid float value %q for %s", val, key)
		return 0, false
	}
	return f, true
}

func lookupEnvBool(key string) (bool, bool) {
	val, ok := lookupEnvString(key)
	if !ok {
		return false, false
	}
	b, err := strconv.ParseBool(val)
	if err != nil {
		log.Printf("invalid boolean value %q for %s", val, key)
		return false, false
	}
	return b, true
}

func sanitizeEnvSegment(seg string) string {
	seg = envKeySanitizer.Replace(seg)
	seg = strings.ToUpper(seg)
	var b strings.Builder
	for _, r := range seg {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return strings.Trim(b.String(), "_")
}

func normalizeVoice(v openairt.Voice) openairt.Voice {
	trimmed := strings.TrimSpace(string(v))
	if trimmed == "" {
		return ""
	}
	return openairt.Voice(strings.ToLower(trimmed))
}
