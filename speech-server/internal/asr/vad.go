package asr

import (
	"context"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	sherpa "github.com/k2-fsa/sherpa-onnx-go/sherpa_onnx"
)

const (
	VADProviderSilero = "silero"
	VADProviderTen    = "ten"

	defaultVADSampleRate   = 16000
	defaultVADThreads      = 1
	defaultVADThreshold    = 0.5
	defaultVADMinSilence   = 0.5
	defaultVADMinSpeech    = 0.1
	defaultVADMaxSpeech    = 20.0
	defaultVADWindowSize   = 512
	defaultVADBufferWindow = 5.0

	defaultVADPoolTimeout = 3 * time.Second
	vadAcquireRetryDelay  = 100 * time.Millisecond
)

// VADModel captures the configuration necessary to instantiate a sherpa-onnx VAD detector.
type VADModel struct {
	Name              string
	Provider          string
	ModelPath         string
	Threshold         float32
	MinSilenceSeconds float32
	MinSpeechSeconds  float32
	MaxSpeechSeconds  float32
	WindowSize        int
	SampleRate        int
	NumThreads        int
	Device            string
	Debug             int
	BufferSeconds     float32
}

// Clone returns a deep copy of the model configuration.
func (m VADModel) Clone() VADModel {
	return m
}

// Validate normalises the configuration and returns an error when required fields are missing.
func (m *VADModel) Validate() error {
	if m.Provider == "" {
		m.Provider = VADProviderSilero
	}
	switch m.Provider {
	case VADProviderSilero, VADProviderTen:
	default:
		return fmt.Errorf("unsupported VAD provider %q", m.Provider)
	}

	if m.ModelPath == "" {
		return fmt.Errorf("vad model path is required for provider %s", m.Provider)
	}
	m.ModelPath = filepath.Clean(m.ModelPath)

	if m.SampleRate <= 0 {
		m.SampleRate = defaultVADSampleRate
	}
	if m.NumThreads <= 0 {
		m.NumThreads = defaultVADThreads
	}
	if m.BufferSeconds <= 0 {
		m.BufferSeconds = defaultVADBufferWindow
	}
	if m.WindowSize <= 0 {
		m.WindowSize = defaultVADWindowSize
	}
	if m.Provider == VADProviderSilero {
		m.WindowSize = selectSileroWindowSize(m.SampleRate, m.WindowSize)
	}

	if m.MinSilenceSeconds <= 0 {
		m.MinSilenceSeconds = defaultVADMinSilence
	}
	if m.MinSpeechSeconds <= 0 {
		m.MinSpeechSeconds = defaultVADMinSpeech
	}
	if m.MaxSpeechSeconds <= 0 {
		m.MaxSpeechSeconds = defaultVADMaxSpeech
	}
	if m.MaxSpeechSeconds < m.MinSpeechSeconds {
		m.MaxSpeechSeconds = m.MinSpeechSeconds + 1.0
	}
	if m.Threshold <= 0 {
		m.Threshold = defaultVADThreshold
	} else if m.Threshold > 1 {
		m.Threshold = 1
	}
	return nil
}

// ApplyOverrides returns a copy of the model with the supplied overrides applied.
func (m VADModel) ApplyOverrides(o VADOverrides) VADModel {
	if o.Threshold != nil {
		m.Threshold = clampThreshold(*o.Threshold)
	}
	if o.MinSilenceSeconds != nil {
		m.MinSilenceSeconds = float32(math.Max(0.05, float64(*o.MinSilenceSeconds)))
	}
	if o.MinSpeechSeconds != nil {
		m.MinSpeechSeconds = float32(math.Max(0.01, float64(*o.MinSpeechSeconds)))
	}
	if o.MaxSpeechSeconds != nil {
		m.MaxSpeechSeconds = float32(math.Max(float64(m.MinSpeechSeconds)+0.01, float64(*o.MaxSpeechSeconds)))
	}
	return m
}

func selectSileroWindowSize(sampleRate, current int) int {
	if current > 0 {
		return current
	}
	switch {
	case sampleRate >= 16000:
		return 512
	case sampleRate >= 8000:
		return 256
	default:
		return 128
	}
}

func clampThreshold(v float32) float32 {
	switch {
	case v < 0:
		return 0
	case v > 1:
		return 1
	default:
		return v
	}
}

// VADOverrides carries optional overrides extracted from session turn-detection hints.
type VADOverrides struct {
	Threshold         *float32
	MinSilenceSeconds *float32
	MinSpeechSeconds  *float32
	MaxSpeechSeconds  *float32
}

// VADSegment represents a chunk of detected speech.
type VADSegment struct {
	StartSamples int
	Samples      []float32
}

// VADDetector exposes the minimal interface required by the realtime server.
type VADDetector interface {
	Feed(samples []float32) ([]VADSegment, error)
	Flush() ([]VADSegment, error)
	Close()
	SampleRate() int
}

type sherpaVADDetector struct {
	cfg      VADModel
	vad      *sherpa.VoiceActivityDetector
	vadMu    sync.Mutex
	closed   bool
	shutdown sync.Once
}

var (
	// ErrVADAcquireTimeout is returned when the pool cannot hand out a detector in
	// the configured timeout.
	ErrVADAcquireTimeout = errors.New("vad detector acquisition timed out")
)

// VADFactory builds VAD detectors for configured models.
type VADFactory struct {
	models map[string]VADModel
}

// NewVADFactory validates all supplied models and returns a factory.
func NewVADFactory(models map[string]VADModel) (*VADFactory, error) {
	copied := make(map[string]VADModel, len(models))
	for name, model := range models {
		if strings.TrimSpace(name) == "" {
			return nil, fmt.Errorf("vad model name cannot be empty")
		}
		cloned := model.Clone()
		if err := cloned.Validate(); err != nil {
			return nil, fmt.Errorf("vad model %s: %w", name, err)
		}
		cloned.Name = name
		copied[name] = cloned
	}
	return &VADFactory{models: copied}, nil
}

// Names returns the list of configured VAD identifiers.
func (f *VADFactory) Names() []string {
	names := make([]string, 0, len(f.models))
	for name := range f.models {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// Model returns a copy of the base configuration for the supplied model name.
func (f *VADFactory) Model(name string) (VADModel, bool) {
	model, ok := f.models[name]
	if !ok {
		return VADModel{}, false
	}
	return model.Clone(), true
}

// Create instantiates a new detector using the named model and optional overrides.
func (f *VADFactory) Create(name string, overrides VADOverrides) (VADDetector, VADModel, error) {
	base, ok := f.Model(name)
	if !ok {
		return nil, VADModel{}, fmt.Errorf("vad provider %q not found", name)
	}
	cfg := base.ApplyOverrides(overrides)

	if err := cfg.Validate(); err != nil {
		return nil, VADModel{}, err
	}

	detector, err := newSherpaVADDetector(cfg)
	if err != nil {
		return nil, VADModel{}, err
	}
	return detector, cfg, nil
}

func newSherpaVADDetector(cfg VADModel) (*sherpaVADDetector, error) {
	var (
		config sherpa.VadModelConfig
	)
	switch cfg.Provider {
	case VADProviderSilero:
		config.SileroVad = sherpa.SileroVadModelConfig{
			Model:              cfg.ModelPath,
			Threshold:          cfg.Threshold,
			MinSilenceDuration: cfg.MinSilenceSeconds,
			MinSpeechDuration:  cfg.MinSpeechSeconds,
			WindowSize:         cfg.WindowSize,
			MaxSpeechDuration:  cfg.MaxSpeechSeconds,
		}
	case VADProviderTen:
		config.TenVad = sherpa.TenVadModelConfig{
			Model:              cfg.ModelPath,
			Threshold:          cfg.Threshold,
			MinSilenceDuration: cfg.MinSilenceSeconds,
			MinSpeechDuration:  cfg.MinSpeechSeconds,
			WindowSize:         cfg.WindowSize,
			MaxSpeechDuration:  cfg.MaxSpeechSeconds,
		}
	default:
		return nil, fmt.Errorf("unsupported VAD provider %q", cfg.Provider)
	}

	config.SampleRate = cfg.SampleRate
	config.NumThreads = cfg.NumThreads
	config.Provider = cfg.Device
	if strings.TrimSpace(config.Provider) == "" {
		config.Provider = "cpu"
	}
	config.Debug = cfg.Debug

	instance := sherpa.NewVoiceActivityDetector(&config, cfg.BufferSeconds)
	if instance == nil {
		return nil, fmt.Errorf("failed to create sherpa voice activity detector for %s", cfg.Name)
	}

	return &sherpaVADDetector{
		cfg: cfg,
		vad: instance,
	}, nil
}

func (s *sherpaVADDetector) Feed(samples []float32) ([]VADSegment, error) {
	if len(samples) == 0 {
		return nil, nil
	}
	s.vadMu.Lock()
	defer s.vadMu.Unlock()
	if s.closed {
		return nil, fmt.Errorf("vad detector closed")
	}

	s.vad.AcceptWaveform(samples)
	return s.collectLocked(), nil
}

func (s *sherpaVADDetector) Flush() ([]VADSegment, error) {
	s.vadMu.Lock()
	defer s.vadMu.Unlock()
	if s.closed {
		return nil, nil
	}
	s.vad.Flush()
	return s.collectLocked(), nil
}

func (s *sherpaVADDetector) collectLocked() []VADSegment {
	var segments []VADSegment
	for !s.vad.IsEmpty() {
		segment := s.vad.Front()
		s.vad.Pop()
		if segment == nil || len(segment.Samples) == 0 {
			continue
		}
		copySamples := make([]float32, len(segment.Samples))
		copy(copySamples, segment.Samples)
		segments = append(segments, VADSegment{
			StartSamples: segment.Start,
			Samples:      copySamples,
		})
	}
	return segments
}

func (s *sherpaVADDetector) Close() {
	s.shutdown.Do(func() {
		s.vadMu.Lock()
		defer s.vadMu.Unlock()
		if s.vad != nil {
			sherpa.DeleteVoiceActivityDetector(s.vad)
			s.vad = nil
		}
		s.closed = true
	})
}

func (s *sherpaVADDetector) SampleRate() int {
	return s.cfg.SampleRate
}

// VADLease represents a detector borrowed from the pool.
type VADLease struct {
	detector VADDetector
	pool     *VADPool
	key      string
	released atomic.Bool
}

// Detector exposes the underlying detector.
func (l *VADLease) Detector() VADDetector {
	if l == nil {
		return nil
	}
	return l.detector
}

// Release returns the detector to the originating pool.
func (l *VADLease) Release() {
	if l == nil || l.pool == nil || l.detector == nil {
		return
	}
	if l.released.Swap(true) {
		return
	}
	l.pool.release(l.key, l.detector)
	l.detector = nil
	l.pool = nil
	l.key = ""
}

// VADPool manages a bounded set of VAD detectors that can be shared across
// realtime sessions to avoid repeatedly constructing expensive ONNX graphs.
type VADPool struct {
	factory  *VADFactory
	provider string
	minSize  int
	maxSize  int
	timeout  time.Duration

	mu    sync.Mutex
	idle  map[string][]VADDetector
	total int
}

// VADPoolOption configures pool creation.
type VADPoolOption struct {
	Factory  *VADFactory
	Provider string
	MinSize  int
	MaxSize  int
	Timeout  time.Duration
}

// NewVADPool creates a new pool with optional warm-up.
func NewVADPool(opt VADPoolOption) (*VADPool, error) {
	if opt.Factory == nil {
		return nil, fmt.Errorf("vad pool requires factory")
	}
	if strings.TrimSpace(opt.Provider) == "" {
		return nil, fmt.Errorf("vad pool provider is required")
	}
	minSize := opt.MinSize
	maxSize := opt.MaxSize
	if maxSize <= 0 {
		maxSize = 4
	}
	if minSize < 0 {
		minSize = 0
	}
	if minSize > maxSize {
		minSize = maxSize
	}
	pool := &VADPool{
		factory:  opt.Factory,
		provider: strings.TrimSpace(opt.Provider),
		minSize:  minSize,
		maxSize:  maxSize,
		timeout:  opt.Timeout,
		idle:     make(map[string][]VADDetector),
	}

	if pool.timeout <= 0 {
		pool.timeout = defaultVADPoolTimeout
	}

	if pool.minSize > 0 {
		key := pool.configKey(VADOverrides{})
		for i := 0; i < pool.minSize && pool.total < pool.maxSize; i++ {
			det, _, err := pool.factory.Create(pool.provider, VADOverrides{})
			if err != nil {
				return nil, err
			}
			pool.idle[key] = append(pool.idle[key], det)
			pool.total++
		}
	}

	return pool, nil
}

// Acquire obtains a detector configured with the supplied overrides. The
// detector must be returned via the lease's Release method.
func (p *VADPool) Acquire(ctx context.Context, overrides VADOverrides) (*VADLease, VADModel, error) {
	if p == nil {
		return nil, VADModel{}, fmt.Errorf("vad pool is nil")
	}
	key := p.configKey(overrides)
	deadline := time.Now().Add(p.timeout)

forLoop:
	for {
		p.mu.Lock()
		if list := p.idle[key]; len(list) > 0 {
			var det VADDetector
			det, p.idle[key] = list[len(list)-1], list[:len(list)-1]
			p.mu.Unlock()
			cfg, ok := p.factory.Model(p.provider)
			if !ok {
				cfg = VADModel{SampleRate: defaultVADSampleRate, Threshold: defaultVADThreshold, MinSilenceSeconds: defaultVADMinSilence}
			}
			cfg = cfg.ApplyOverrides(overrides)
			return &VADLease{detector: det, pool: p, key: key}, cfg, nil
		}
		if p.total < p.maxSize {
			p.total++
			p.mu.Unlock()
			det, cfg, err := p.factory.Create(p.provider, overrides)
			if err != nil {
				p.mu.Lock()
				p.total--
				p.mu.Unlock()
				return nil, VADModel{}, err
			}
			return &VADLease{detector: det, pool: p, key: key}, cfg, nil
		}
		p.mu.Unlock()

		if ctx != nil {
			select {
			case <-ctx.Done():
				return nil, VADModel{}, ctx.Err()
			default:
			}
		}

		if time.Now().After(deadline) {
			break forLoop
		}
		delay := time.Until(deadline)
		if delay > vadAcquireRetryDelay {
			delay = vadAcquireRetryDelay
		}
		time.Sleep(delay)
	}

	return nil, VADModel{}, ErrVADAcquireTimeout
}

func (p *VADPool) release(key string, det VADDetector) {
	if det == nil {
		return
	}
	p.mu.Lock()
	if p.idle == nil {
		p.idle = make(map[string][]VADDetector)
	}
	if len(p.idle[key]) >= p.maxSize {
		p.total--
		p.mu.Unlock()
		det.Close()
		return
	}
	p.idle[key] = append(p.idle[key], det)
	p.mu.Unlock()
}

func (p *VADPool) configKey(overrides VADOverrides) string {
	threshold := -1.0
	minSilence := -1.0
	minSpeech := -1.0
	maxSpeech := -1.0
	if overrides.Threshold != nil {
		threshold = float64(*overrides.Threshold)
	}
	if overrides.MinSilenceSeconds != nil {
		minSilence = float64(*overrides.MinSilenceSeconds)
	}
	if overrides.MinSpeechSeconds != nil {
		minSpeech = float64(*overrides.MinSpeechSeconds)
	}
	if overrides.MaxSpeechSeconds != nil {
		maxSpeech = float64(*overrides.MaxSpeechSeconds)
	}
	return fmt.Sprintf("%s:%.3f:%.3f:%.3f:%.3f", p.provider, threshold, minSilence, minSpeech, maxSpeech)
}

// Close releases all idle detectors held by the pool.
func (p *VADPool) Close() {
	if p == nil {
		return
	}
	p.mu.Lock()
	idle := p.idle
	p.idle = make(map[string][]VADDetector)
	p.total = 0
	p.mu.Unlock()
	for _, detectors := range idle {
		for _, det := range detectors {
			det.Close()
		}
	}
}
