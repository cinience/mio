//go:build sherpa_onnx
// +build sherpa_onnx

package sherpa_vad

import (
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"backend-server/internal/domain/vad/inter"
	log "backend-server/internal/infrastructure/logger"
	"backend-server/pkg/confighelper"

	sherpa "github.com/k2-fsa/sherpa-onnx-go/sherpa_onnx"
)

const (
	defaultSampleRate         = 16000
	defaultNumThreads         = 1
	defaultProvider           = "cpu"
	defaultDebugLevel         = 0
	defaultThreshold          = 0.5
	defaultMinSilenceDuration = 0.5
	defaultMinSpeechDuration  = 0.25
	defaultMaxSpeechDuration  = 0.0
	defaultWindowSize         = 512
	defaultBufferSizeSeconds  = 5.0
)

// SherpaConfig 描述 Sherpa VAD 需要的关键参数
type SherpaConfig struct {
	ModelPath          string
	SampleRate         int
	NumThreads         int
	Provider           string
	Debug              int
	Threshold          float32
	MinSilenceDuration float32
	MinSpeechDuration  float32
	MaxSpeechDuration  float32
	WindowSize         int
	BufferSizeSeconds  float32
}

// SherpaVAD 基于 sherpa-onnx 的 VAD 实现
type SherpaVAD struct {
	cfg        SherpaConfig
	detector   *sherpa.VoiceActivityDetector
	buffer     *sherpa.CircularBuffer
	windowSize int
	mu         sync.Mutex
	closed     bool
	poolKey    string
}

//go:embed silero_vad.onnx
var embeddedSileroModel []byte

var (
	// embeddedModelOnce guards extraction of the embedded model to disk.
	embeddedModelOnce sync.Once
	// embeddedModelPath stores the temp file location holding the embedded model.
	embeddedModelPath string
	// embeddedModelErr persists any failure encountered during extraction.
	embeddedModelErr error
	// embeddedLogOnce ensures we only log the fallback message once.
	embeddedLogOnce sync.Once

	poolMu  sync.RWMutex
	poolMap = make(map[string]*sync.Pool)
)

// ensureModelPath resolves the ONNX model location. If the provided path points
// to a readable file it is returned as-is. Otherwise we fall back to the
// embedded Silero VAD model (written to a temp file on first use).
func ensureModelPath(path string) (string, error) {
	clean := filepath.Clean(path)
	if clean != "." && clean != "" {
		if info, err := os.Stat(clean); err == nil && !info.IsDir() {
			return clean, nil
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("stat model path %q: %w", clean, err)
		}
	}

	if len(embeddedSileroModel) == 0 {
		return "", fmt.Errorf("model path %q not found and embedded model unavailable", clean)
	}

	embeddedModelOnce.Do(func() {
		f, err := os.CreateTemp("", "sherpa-silero-*.onnx")
		if err != nil {
			embeddedModelErr = fmt.Errorf("create temp model: %w", err)
			return
		}
		if _, err = f.Write(embeddedSileroModel); err != nil {
			f.Close()
			_ = os.Remove(f.Name())
			embeddedModelErr = fmt.Errorf("write temp model: %w", err)
			return
		}
		if err = f.Close(); err != nil {
			_ = os.Remove(f.Name())
			embeddedModelErr = fmt.Errorf("close temp model: %w", err)
			return
		}
		embeddedModelPath = f.Name()
	})

	if embeddedModelErr != nil {
		return "", embeddedModelErr
	}

	embeddedLogOnce.Do(func() {
		log.Infof("sherpa_vad: using embedded Silero VAD model at %s", embeddedModelPath)
	})

	return embeddedModelPath, nil
}

// AcquireVAD 创建一个新的 Sherpa VAD 实例
func AcquireVAD(cfg map[string]interface{}) (inter.VAD, error) {
	parsed, err := parseConfig(cfg)
	if err != nil {
		return nil, err
	}
	// Resolve the model path now so downstream code receives a concrete file.
	parsed.ModelPath, err = ensureModelPath(parsed.ModelPath)
	if err != nil {
		return nil, fmt.Errorf("resolve VAD model: %w", err)
	}

	modelCfg := &sherpa.VadModelConfig{}
	modelCfg.SampleRate = parsed.SampleRate
	modelCfg.NumThreads = parsed.NumThreads
	modelCfg.Provider = parsed.Provider
	modelCfg.Debug = parsed.Debug

	modelCfg.SileroVad.Model = parsed.ModelPath
	modelCfg.SileroVad.Threshold = parsed.Threshold
	modelCfg.SileroVad.MinSilenceDuration = parsed.MinSilenceDuration
	modelCfg.SileroVad.MinSpeechDuration = parsed.MinSpeechDuration
	modelCfg.SileroVad.MaxSpeechDuration = parsed.MaxSpeechDuration
	modelCfg.SileroVad.WindowSize = parsed.WindowSize

	// 仅使用 Silero VAD，TenVad 保持默认空配置
	detector := sherpa.NewVoiceActivityDetector(modelCfg, parsed.BufferSizeSeconds)
	if detector == nil {
		return nil, fmt.Errorf("failed to initialize sherpa voice activity detector. modelCfg:%+v", modelCfg)
	}

	windowSize := parsed.WindowSize
	if windowSize <= 0 {
		windowSize = defaultWindowSize
	}

	bufferCapacity := int(parsed.BufferSizeSeconds * float32(parsed.SampleRate))
	if bufferCapacity <= 0 {
		bufferCapacity = parsed.SampleRate * 10
	}
	if bufferCapacity < windowSize*2 {
		bufferCapacity = windowSize * 2
	}

	poolKey := poolKeyForConfig(parsed)
	if pooled := takeFromPool(poolKey); pooled != nil {
		pooled.cfg = parsed
		pooled.windowSize = windowSize
		return pooled, nil
	}

	cbuf := sherpa.NewCircularBuffer(bufferCapacity)
	if cbuf == nil {
		sherpa.DeleteVoiceActivityDetector(detector)
		return nil, fmt.Errorf("failed to initialize sherpa circular buffer")
	}

	vad := &SherpaVAD{
		cfg:        parsed,
		detector:   detector,
		buffer:     cbuf,
		windowSize: windowSize,
		poolKey:    poolKey,
	}
	return vad, nil
}

// ReleaseVAD 释放 Sherpa VAD 资源
func ReleaseVAD(v inter.VAD) error {
	sv, ok := v.(*SherpaVAD)
	if !ok {
		return fmt.Errorf("invalid VAD type for sherpa release")
	}
	if sv == nil {
		return fmt.Errorf("invalid VAD instance")
	}
	if sv.returnToPool() {
		return nil
	}
	return sv.Close()
}

// IsVAD 检测语音活动
func (s *SherpaVAD) IsVAD(pcmData []float32) (bool, error) {
	return s.detect(pcmData)
}

// IsVADExt 提供带采样率信息的语音检测
func (s *SherpaVAD) IsVADExt(pcmData []float32, sampleRate int, _ int) (bool, error) {
	if sampleRate > 0 && sampleRate != s.cfg.SampleRate {
		return false, fmt.Errorf("sherpa_vad: sample rate mismatch, got %d expect %d", sampleRate, s.cfg.SampleRate)
	}
	return s.detect(pcmData)
}

// Reset 重置内部 detector
func (s *SherpaVAD) Reset() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed || s.detector == nil {
		return nil
	}
	s.detector.Reset()
	if s.buffer != nil {
		s.buffer.Reset()
	}
	return nil
}

// Close 释放 detector 资源
func (s *SherpaVAD) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}

	if s.detector != nil {
		sherpa.DeleteVoiceActivityDetector(s.detector)
		s.detector = nil
	}
	if s.buffer != nil {
		sherpa.DeleteCircularBuffer(s.buffer)
		s.buffer = nil
	}

	s.closed = true
	return nil
}

func (s *SherpaVAD) detect(pcmData []float32) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed || s.detector == nil {
		return false, fmt.Errorf("sherpa_vad: detector not available")
	}

	if s.buffer == nil {
		return false, fmt.Errorf("sherpa_vad: circular buffer not available")
	}

	if len(pcmData) > 0 {
		s.buffer.Push(pcmData)
	}

	detected := false
	window := s.windowSize
	if window <= 0 {
		window = defaultWindowSize
	}

	for s.buffer.Size() >= window {
		head := s.buffer.Head()
		chunk := s.buffer.Get(head, window)
		s.buffer.Pop(window)

		s.detector.AcceptWaveform(chunk)
		if s.detector.IsSpeech() {
			detected = true
		}

		for !s.detector.IsEmpty() {
			segment := s.detector.Front()
			s.detector.Pop()
			if len(segment.Samples) > 0 {
				detected = true
			}
		}
	}

	if s.detector.IsSpeech() {
		detected = true
	}

	return detected, nil
}

func parseConfig(cfg map[string]interface{}) (SherpaConfig, error) {
	helper := confighelper.New(cfg)
	parsed := SherpaConfig{
		ModelPath:          helper.GetString("model_path"),
		SampleRate:         helper.GetInt("sample_rate", defaultSampleRate),
		NumThreads:         helper.GetInt("num_threads", defaultNumThreads),
		Provider:           helper.GetString("provider", defaultProvider),
		Debug:              helper.GetInt("debug", defaultDebugLevel),
		Threshold:          float32(helper.GetFloat64("threshold", defaultThreshold)),
		MinSilenceDuration: float32(helper.GetFloat64("min_silence_duration", defaultMinSilenceDuration)),
		MinSpeechDuration:  float32(helper.GetFloat64("min_speech_duration", defaultMinSpeechDuration)),
		MaxSpeechDuration:  float32(helper.GetFloat64("max_speech_duration", defaultMaxSpeechDuration)),
		WindowSize:         helper.GetInt("window_size", defaultWindowSize),
		BufferSizeSeconds:  float32(helper.GetFloat64("buffer_size_seconds", defaultBufferSizeSeconds)),
	}

	if parsed.SampleRate <= 0 {
		return SherpaConfig{}, fmt.Errorf("sample_rate must be positive, got %d", parsed.SampleRate)
	}
	if parsed.SampleRate != 8000 && parsed.SampleRate != 16000 {
		log.Warnf("sherpa_vad: unusual sample rate %d, Silero VAD typically uses 8000 or 16000", parsed.SampleRate)
	}
	if parsed.NumThreads <= 0 {
		parsed.NumThreads = defaultNumThreads
	}
	if parsed.BufferSizeSeconds <= 0 {
		parsed.BufferSizeSeconds = defaultBufferSizeSeconds
	}
	if parsed.WindowSize <= 0 {
		parsed.WindowSize = defaultWindowSize
	} else if parsed.WindowSize != 512 && parsed.WindowSize != 1024 {
		log.Warnf("sherpa_vad: unusual window size %d, Silero VAD typically uses 512 or 1024", parsed.WindowSize)
	}
	if parsed.Threshold < 0 || parsed.Threshold > 1 {
		return SherpaConfig{}, fmt.Errorf("threshold must be within [0,1], got %f", parsed.Threshold)
	}
	if parsed.MinSpeechDuration < 0 {
		return SherpaConfig{}, fmt.Errorf("min_speech_duration must be non-negative, got %f", parsed.MinSpeechDuration)
	}
	if parsed.MinSilenceDuration < 0 {
		return SherpaConfig{}, fmt.Errorf("min_silence_duration must be non-negative, got %f", parsed.MinSilenceDuration)
	}
	if parsed.MaxSpeechDuration < 0 {
		return SherpaConfig{}, fmt.Errorf("max_speech_duration must be >= 0, got %f", parsed.MaxSpeechDuration)
	}
	if parsed.MaxSpeechDuration > 0 && parsed.MaxSpeechDuration < parsed.MinSpeechDuration {
		return SherpaConfig{}, fmt.Errorf("max_speech_duration (%f) must be >= min_speech_duration (%f)", parsed.MaxSpeechDuration, parsed.MinSpeechDuration)
	}

	return parsed, nil
}

func poolKeyForConfig(cfg SherpaConfig) string {
	return fmt.Sprintf("%s|%d|%d|%d|%0.4f|%0.4f|%0.4f|%0.4f|%0.4f|%d|%s",
		cfg.ModelPath,
		cfg.SampleRate,
		cfg.NumThreads,
		cfg.Debug,
		cfg.Threshold,
		cfg.MinSilenceDuration,
		cfg.MinSpeechDuration,
		cfg.MaxSpeechDuration,
		cfg.BufferSizeSeconds,
		cfg.WindowSize,
		cfg.Provider,
	)
}

func takeFromPool(key string) *SherpaVAD {
	if key == "" {
		return nil
	}
	poolMu.RLock()
	pool := poolMap[key]
	poolMu.RUnlock()
	if pool == nil {
		return nil
	}
	if v := pool.Get(); v != nil {
		if sv, ok := v.(*SherpaVAD); ok {
			sv.poolKey = key
			sv.closed = false
			return sv
		}
	}
	return nil
}

func getOrCreatePool(key string) *sync.Pool {
	if key == "" {
		return nil
	}
	poolMu.RLock()
	pool := poolMap[key]
	poolMu.RUnlock()
	if pool != nil {
		return pool
	}
	poolMu.Lock()
	defer poolMu.Unlock()
	if pool = poolMap[key]; pool != nil {
		return pool
	}
	pool = &sync.Pool{}
	poolMap[key] = pool
	return pool
}

func (s *SherpaVAD) returnToPool() bool {
	if s == nil || s.poolKey == "" {
		return false
	}
	if s.closed {
		return false
	}
	if err := s.Reset(); err != nil {
		return false
	}
	pool := getOrCreatePool(s.poolKey)
	if pool == nil {
		return false
	}
	pool.Put(s)
	return true
}
