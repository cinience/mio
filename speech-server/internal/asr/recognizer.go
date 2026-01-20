package asr

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"

	sherpa "github.com/k2-fsa/sherpa-onnx-go/sherpa_onnx"

	"speech-server/internal/logger"
)

const (
	defaultSampleRate     = 16000
	defaultNumThreads     = 1
	defaultProvider       = "cpu"
	defaultDebugLevel     = 1
	defaultDecodingMethod = "greedy_search"
	defaultMaxActivePaths = 4
)

// Config captures the subset of Sherpa options required for sense-voice.
type Config struct {
	ModelDir       string
	ModelType      string
	ModelVariant   string
	SampleRate     int
	NumThreads     int
	Provider       string
	Debug          int
	DecodingMethod string
	MaxActivePaths int
	EnableEndpoint bool
	Language       string
	UseITN         bool
	LanguageGuard  LanguageGuardConfig
}

// LanguageGuardConfig defines heuristics to drop mismatched language results.
type LanguageGuardConfig struct {
	Enabled               bool
	DefaultHint           string
	MaxMismatchDuration   float64
	MaxMismatchCharacters int
}

// Result represents a single transcription with optional metadata emitted by
// sherpa-onnx (language tag, emotion, event classification, timings, etc.).
type Result struct {
	Text       string
	Tokens     []string
	Timestamps []float32
	Durations  []float32
	Language   string
	Emotion    string
	Event      string
}

// Recognizer wraps sherpa.OnlineRecognizer with simple helpers.
type Recognizer struct {
	cfg        Config
	mode       recognizerMode
	onlineCfg  *sherpa.OnlineRecognizerConfig
	online     *sherpa.OnlineRecognizer
	offlineCfg *sherpa.OfflineRecognizerConfig
	offline    *sherpa.OfflineRecognizer
	mu         sync.Mutex
}

type recognizerMode int

const (
	modeOnline recognizerMode = iota
	modeOffline
)

func (m recognizerMode) String() string {
	switch m {
	case modeOffline:
		return "offline"
	case modeOnline:
		return "online"
	default:
		return "unknown"
	}
}

// NewRecognizer builds a sherpa recognizer using the supplied configuration.
func NewRecognizer(cfg Config) (*Recognizer, error) {
	if cfg.ModelDir == "" {
		return nil, fmt.Errorf("model directory is required")
	}
	if cfg.SampleRate == 0 {
		cfg.SampleRate = defaultSampleRate
	}
	if cfg.NumThreads == 0 {
		cfg.NumThreads = defaultNumThreads
	}
	if cfg.Provider == "" {
		cfg.Provider = defaultProvider
	}
	if cfg.DecodingMethod == "" {
		cfg.DecodingMethod = defaultDecodingMethod
	}
	if cfg.MaxActivePaths == 0 {
		cfg.MaxActivePaths = defaultMaxActivePaths
	}
	cfg.Debug = clampDebug(cfg.Debug)
	if cfg.Language == "" {
		cfg.Language = "auto"
	}

	modelType := detectModelType(cfg.ModelType, cfg.ModelDir)

	switch modelType {
	case "sense_voice":
		offlineCfg, err := buildSenseVoiceConfig(cfg)
		if err != nil {
			return nil, err
		}
		logger.Infof("asr sense voice model: %s", offlineCfg.ModelConfig.SenseVoice.Model)
		offline := sherpa.NewOfflineRecognizer(offlineCfg)
		if offline == nil {
			return nil, fmt.Errorf("failed to create sherpa offline recognizer")
		}
		return &Recognizer{
			cfg:        cfg,
			mode:       modeOffline,
			offlineCfg: offlineCfg,
			offline:    offline,
		}, nil
	default:
		onlineCfg, err := buildOnlineRecognizerConfig(cfg, modelType)
		if err != nil {
			return nil, err
		}
		logger.Infof("asr config tokens file: %s", onlineCfg.ModelConfig.Tokens)

		recog := sherpa.NewOnlineRecognizer(onlineCfg)
		if recog == nil {
			return nil, fmt.Errorf("failed to create sherpa online recognizer")
		}

		return &Recognizer{
			cfg:       cfg,
			mode:      modeOnline,
			onlineCfg: onlineCfg,
			online:    recog,
		}, nil
	}
}

// Recognize runs a single utterance through the recognizer.
func (r *Recognizer) Recognize(samples []float32) (Result, error) {
	return r.RecognizeWithHint(samples, "")
}

// RecognizeWithHint runs recognition with an optional language hint the caller
// expects. When the result language mismatches the hint, a short utterance can
// be treated as noise based on heuristics.
func (r *Recognizer) RecognizeWithHint(samples []float32, languageHint string) (Result, error) {
	var empty Result

	if len(samples) == 0 {
		return empty, nil
	}

	start := time.Now()
	var recognized Result

	switch r.mode {
	case modeOffline:
		stream := sherpa.NewOfflineStream(r.offline)
		if stream == nil {
			return empty, fmt.Errorf("failed to create offline stream")
		}
		defer sherpa.DeleteOfflineStream(stream)

		stream.AcceptWaveform(r.cfg.SampleRate, samples)

		r.mu.Lock()
		r.offline.Decode(stream)
		result := stream.GetResult()
		r.mu.Unlock()

		if result == nil {
			return empty, nil
		}

		recognized = Result{
			Text:       result.Text,
			Tokens:     append([]string(nil), result.Tokens...),
			Timestamps: append([]float32(nil), result.Timestamps...),
			Durations:  append([]float32(nil), result.Durations...),
			Language:   result.Lang,
			Emotion:    result.Emotion,
			Event:      result.Event,
		}
	default:
		stream := sherpa.NewOnlineStream(r.online)
		if stream == nil {
			return empty, fmt.Errorf("failed to create sherpa stream")
		}
		defer sherpa.DeleteOnlineStream(stream)

		stream.AcceptWaveform(r.cfg.SampleRate, samples)
		stream.InputFinished()

		r.mu.Lock()
		for r.online.IsReady(stream) {
			r.online.Decode(stream)
		}
		text := r.online.GetResult(stream).Text
		r.online.Reset(stream)
		r.mu.Unlock()

		recognized = Result{Text: text}
	}

	recognized = r.applyLanguageHint(recognized, languageHint, len(samples))
	r.logResult(recognized, len(samples), time.Since(start))
	return recognized, nil
}

// SampleRate returns the target sample rate for the recognizer.
func (r *Recognizer) SampleRate() int {
	return r.cfg.SampleRate
}

func (r *Recognizer) logResult(res Result, sampleCount int, elapsed time.Duration) {
	if sampleCount <= 0 {
		sampleCount = 0
	}
	rate := r.SampleRate()
	var durationSec float64
	if rate > 0 {
		durationSec = float64(sampleCount) / float64(rate)
	}
	logger.Infof("asr recognize result: mode=%s duration=%.2fs elapsed=%s text=%q lang=%s emotion=%s event=%s tokens=%d", r.mode.String(), durationSec, elapsed, res.Text, res.Language, res.Emotion, res.Event, len(res.Tokens))
}

func (r *Recognizer) applyLanguageHint(res Result, hint string, sampleCount int) Result {
	guard := r.cfg.LanguageGuard
	// 根据动态语言提示或配置默认提示拼接出候选列表
	// Gather hints from session or fallback to config defaults.
	hints := parseLanguageHints(hint)
	if len(hints) == 0 && guard.Enabled {
		hints = parseLanguageHints(guard.DefaultHint)
	}
	if len(hints) == 0 {
		return res
	}
	if res.Text == "" {
		return res
	}
	recognizedLang := strings.TrimSpace(res.Language)
	for _, candidate := range hints {
		if strings.EqualFold(recognizedLang, candidate) {
			return res
		}
	}
	rate := r.SampleRate()
	var durationSec float64
	if rate > 0 {
		durationSec = float64(sampleCount) / float64(rate)
	}
	maxDuration := guard.MaxMismatchDuration
	if maxDuration <= 0 {
		maxDuration = 0.8
	}
	maxChars := guard.MaxMismatchCharacters
	if maxChars <= 0 {
		maxChars = 4
	}
	if durationSec <= maxDuration && len([]rune(res.Text)) <= maxChars {
		logger.Warnf("asr result rejected by language hint: expected=%s got=%s duration=%.2fs text=%q", strings.Join(hints, ","), res.Language, durationSec, res.Text)
		return Result{}
	}
	return res
}

// parseLanguageHints 将逗号/分号/空格分隔的语言码解析成去重的提示列表。
// parseLanguageHints parses comma/semicolon/space separated hints into a unique list.
func parseLanguageHints(raw string) []string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}
	parts := strings.FieldsFunc(trimmed, func(r rune) bool {
		return r == ',' || r == ';' || unicode.IsSpace(r)
	})
	var hints []string
	seen := make(map[string]struct{})
	for _, p := range parts {
		candidate := strings.TrimSpace(p)
		if candidate == "" {
			continue
		}
		key := strings.ToLower(candidate)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		hints = append(hints, candidate)
	}
	return hints
}

// Close releases native resources.
func (r *Recognizer) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.online != nil {
		sherpa.DeleteOnlineRecognizer(r.online)
		r.online = nil
	}
	if r.offline != nil {
		sherpa.DeleteOfflineRecognizer(r.offline)
		r.offline = nil
	}
}

func clampDebug(level int) int {
	switch {
	case level < 0:
		return 0
	case level > 3:
		return 3
	default:
		return level
	}
}

func buildOnlineRecognizerConfig(cfg Config, modelType string) (*sherpa.OnlineRecognizerConfig, error) {
	recogCfg := &sherpa.OnlineRecognizerConfig{}
	recogCfg.FeatConfig = sherpa.FeatureConfig{SampleRate: cfg.SampleRate, FeatureDim: 80}
	recogCfg.ModelConfig.NumThreads = cfg.NumThreads
	recogCfg.ModelConfig.Provider = cfg.Provider
	recogCfg.ModelConfig.Debug = cfg.Debug
	recogCfg.DecodingMethod = cfg.DecodingMethod
	recogCfg.MaxActivePaths = cfg.MaxActivePaths
	if cfg.EnableEndpoint {
		recogCfg.EnableEndpoint = 1
	}

	switch modelType {
	case "paraformer":
		recogCfg.ModelConfig.Paraformer.Encoder = filepath.Join(cfg.ModelDir, "encoder.int8.onnx")
		recogCfg.ModelConfig.Paraformer.Decoder = filepath.Join(cfg.ModelDir, "decoder.int8.onnx")
		recogCfg.ModelConfig.Tokens = filepath.Join(cfg.ModelDir, "tokens.txt")
		recogCfg.ModelConfig.ModelType = "paraformer"
	case "zipformer":
		recogCfg.ModelConfig.Transducer.Encoder = filepath.Join(cfg.ModelDir, "exp", "encoder-epoch-12-avg-4-chunk-16-left-128.onnx")
		recogCfg.ModelConfig.Transducer.Decoder = filepath.Join(cfg.ModelDir, "exp", "decoder-epoch-12-avg-4-chunk-16-left-128.onnx")
		recogCfg.ModelConfig.Transducer.Joiner = filepath.Join(cfg.ModelDir, "exp", "joiner-epoch-12-avg-4-chunk-16-left-128.onnx")
		recogCfg.ModelConfig.Tokens = filepath.Join(cfg.ModelDir, "tokens.txt")
		recogCfg.ModelConfig.ModelType = "zipformer2"
	default:
		return nil, fmt.Errorf("unsupported model type %q", modelType)
	}

	if recogCfg.ModelConfig.Tokens == "" && recogCfg.ModelConfig.TokensBuf == "" {
		return nil, fmt.Errorf("token file not configured for model")
	}

	return recogCfg, nil
}

func detectModelType(modelType, modelDir string) string {
	if modelType != "" {
		return strings.ToLower(modelType)
	}

	dir := strings.ToLower(modelDir)
	switch {
	case strings.Contains(dir, "paraformer"):
		return "paraformer"
	case strings.Contains(dir, "zipformer"):
		return "zipformer"
	case strings.Contains(dir, "sense-voice"):
		return "sense_voice"
	default:
		return "paraformer"
	}
}

func buildSenseVoiceConfig(cfg Config) (*sherpa.OfflineRecognizerConfig, error) {
	modelPath, err := resolveSenseVoiceModelPath(cfg.ModelDir, cfg.ModelVariant)
	if err != nil {
		return nil, err
	}
	tokensPath := filepath.Join(cfg.ModelDir, "tokens.txt")
	decodingMethod := strings.TrimSpace(cfg.DecodingMethod)
	if decodingMethod == "" {
		decodingMethod = defaultDecodingMethod
	}
	if decodingMethod != defaultDecodingMethod {
		logger.Warnf("sense-voice only supports greedy_search decoding; requested %s will be downgraded", decodingMethod)
		decodingMethod = defaultDecodingMethod
	}
	maxPaths := cfg.MaxActivePaths
	if maxPaths <= 0 {
		maxPaths = defaultMaxActivePaths
	}

	recogCfg := &sherpa.OfflineRecognizerConfig{
		FeatConfig: sherpa.FeatureConfig{
			SampleRate: cfg.SampleRate,
			FeatureDim: 80,
		},
		ModelConfig: sherpa.OfflineModelConfig{
			SenseVoice: sherpa.OfflineSenseVoiceModelConfig{
				Model:                       modelPath,
				Language:                    cfg.Language,
				UseInverseTextNormalization: boolToInt(cfg.UseITN),
			},
			Tokens:     tokensPath,
			NumThreads: cfg.NumThreads,
			Debug:      cfg.Debug,
			Provider:   cfg.Provider,
			ModelType:  "sense_voice_ctc",
		},
		DecodingMethod: decodingMethod,
		MaxActivePaths: maxPaths,
	}
	return recogCfg, nil
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func resolveSenseVoiceModelPath(dir, variant string) (string, error) {
	if strings.TrimSpace(dir) == "" {
		return "", fmt.Errorf("empty model directory")
	}
	int8Path := filepath.Join(dir, "model.int8.onnx")
	fp32Path := filepath.Join(dir, "model.onnx")
	if path, ok := pickSenseVoicePath(variant, int8Path, fp32Path); ok {
		return path, nil
	}
	return "", fmt.Errorf("sense voice model not found in %s (checked %s and %s)", dir, int8Path, fp32Path)
}

func pickSenseVoicePath(variant, int8Path, fp32Path string) (string, bool) {
	preference := strings.ToLower(strings.TrimSpace(variant))
	switch preference {
	case "int8":
		if fileExists(int8Path) {
			return int8Path, true
		}
		return "", false
	case "fp32":
		if fileExists(fp32Path) {
			return fp32Path, true
		}
		return "", false
	}
	if fileExists(int8Path) {
		return int8Path, true
	}
	if fileExists(fp32Path) {
		return fp32Path, true
	}
	return "", false
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
