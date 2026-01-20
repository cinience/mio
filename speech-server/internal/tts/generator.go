package tts

import (
	"fmt"
	"strings"
	"sync"
	"unicode"

	sherpa "github.com/k2-fsa/sherpa-onnx-go/sherpa_onnx"
)

const (
	defaultProvider    = "cpu"
	defaultNumThreads  = 1
	defaultNoiseScale  = 0.667
	defaultLengthScale = 1.0
)

// Config describes the resources required to run a sherpa-onnx Matcha TTS model.
type Config struct {
	Engine string

	// Matcha configuration
	AcousticModel string
	Vocoder       string
	Lexicon       string
	Tokens        string
	DataDir       string
	NoiseScale    float32
	EnableCN2AN   bool

	// Shared
	LengthScale float32

	NumThreads int
	Provider   string
	DebugLevel int

	// Kokoro configuration
	KokoroModel   string
	KokoroVoices  string
	KokoroTokens  string
	KokoroDataDir string
	KokoroLexicon string
	KokoroLang    string

	// ZipVoice configuration
	ZipvoiceTokens            string
	ZipvoiceTextModel         string
	ZipvoiceFlowMatchingModel string
	ZipvoiceDataDir           string
	ZipvoicePinyinDict        string
	ZipvoiceVocoder           string
	ZipvoiceFeatScale         float32
	ZipvoiceTShift            float32
	ZipvoiceTargetRms         float32
	ZipvoiceGuidanceScale     float32
	ZipvoiceNumSteps          int
	ZipvoicePromptAudio       string
	ZipvoicePromptText        string
	ZipvoicePrompts           map[int]ZipvoicePromptConfig
}

// ZipvoicePromptConfig describes a voice-specific prompt used to condition the ZipVoice model.
type ZipvoicePromptConfig struct {
	PromptAudio string
	PromptText  string
	NumSteps    int
}

// Generator wraps sherpa.OnlineTts for thread-safe synthesis calls.
type Generator struct {
	cfg             Config
	tts             *sherpa.OfflineTts
	mu              sync.Mutex
	zipvoiceDefault *zipvoiceRuntime
	zipvoiceByID    map[int]*zipvoiceRuntime
}

type zipvoiceRuntime struct {
	promptSamples []float32
	promptRate    int
	promptText    string
	numSteps      int
}

// NewGenerator builds a Matcha TTS generator using the supplied configuration.
func NewGenerator(cfg Config) (*Generator, error) {
	engine := strings.TrimSpace(cfg.Engine)
	if engine == "" {
		engine = "matcha"
	}

	if cfg.Provider == "" {
		cfg.Provider = defaultProvider
	}
	if cfg.NumThreads <= 0 {
		cfg.NumThreads = defaultNumThreads
	}
	if cfg.LengthScale == 0 {
		cfg.LengthScale = defaultLengthScale
	}

	var ttsCfg *sherpa.OfflineTtsConfig

	switch strings.ToLower(engine) {
	case "kokoro":
		if cfg.KokoroModel == "" {
			return nil, fmt.Errorf("tts kokoro model path is required")
		}
		if cfg.KokoroVoices == "" {
			return nil, fmt.Errorf("tts kokoro voices path is required")
		}
		if cfg.KokoroTokens == "" {
			return nil, fmt.Errorf("tts kokoro tokens path is required")
		}
		if cfg.KokoroDataDir == "" {
			return nil, fmt.Errorf("tts kokoro data directory is required")
		}

		ttsCfg = &sherpa.OfflineTtsConfig{
			Model: sherpa.OfflineTtsModelConfig{
				Kokoro: sherpa.OfflineTtsKokoroModelConfig{
					Model:       cfg.KokoroModel,
					Voices:      cfg.KokoroVoices,
					Tokens:      cfg.KokoroTokens,
					DataDir:     cfg.KokoroDataDir,
					Lexicon:     cfg.KokoroLexicon,
					LengthScale: cfg.LengthScale,
				},
				NumThreads: cfg.NumThreads,
				Provider:   cfg.Provider,
				Debug:      cfg.DebugLevel,
			},
		}
	case "matcha":
		if cfg.AcousticModel == "" {
			return nil, fmt.Errorf("tts acoustic model path is required")
		}
		if cfg.Vocoder == "" {
			return nil, fmt.Errorf("tts vocoder path is required")
		}
		if cfg.Lexicon == "" {
			return nil, fmt.Errorf("tts lexicon path is required")
		}
		if cfg.Tokens == "" {
			return nil, fmt.Errorf("tts tokens path is required")
		}
		if cfg.NoiseScale == 0 {
			cfg.NoiseScale = defaultNoiseScale
		}

		ttsCfg = &sherpa.OfflineTtsConfig{
			Model: sherpa.OfflineTtsModelConfig{
				Matcha: sherpa.OfflineTtsMatchaModelConfig{
					AcousticModel: cfg.AcousticModel,
					Vocoder:       cfg.Vocoder,
					Lexicon:       cfg.Lexicon,
					Tokens:        cfg.Tokens,
					DataDir:       cfg.DataDir,
					NoiseScale:    cfg.NoiseScale,
					LengthScale:   cfg.LengthScale,
				},
				NumThreads: cfg.NumThreads,
				Provider:   cfg.Provider,
				Debug:      cfg.DebugLevel,
			},
		}
	case "zipvoice":
		if cfg.ZipvoiceTokens == "" {
			return nil, fmt.Errorf("tts zipvoice tokens path is required")
		}
		if cfg.ZipvoiceTextModel == "" {
			return nil, fmt.Errorf("tts zipvoice text model path is required")
		}
		if cfg.ZipvoiceFlowMatchingModel == "" {
			return nil, fmt.Errorf("tts zipvoice flow-matching model path is required")
		}
		if cfg.ZipvoiceDataDir == "" {
			return nil, fmt.Errorf("tts zipvoice data directory is required")
		}
		if cfg.ZipvoiceVocoder == "" {
			return nil, fmt.Errorf("tts zipvoice vocoder path is required")
		}
		if cfg.ZipvoiceFeatScale == 0 {
			cfg.ZipvoiceFeatScale = 0.1
		}
		if cfg.ZipvoiceTShift == 0 {
			cfg.ZipvoiceTShift = 0.5
		}
		if cfg.ZipvoiceTargetRms == 0 {
			cfg.ZipvoiceTargetRms = 0.1
		}
		if cfg.ZipvoiceGuidanceScale == 0 {
			cfg.ZipvoiceGuidanceScale = 1.0
		}
		if cfg.ZipvoiceNumSteps <= 0 {
			cfg.ZipvoiceNumSteps = 4
		}

		ttsCfg = &sherpa.OfflineTtsConfig{
			Model: sherpa.OfflineTtsModelConfig{
				Zipvoice: sherpa.OfflineTtsZipvoiceModelConfig{
					Tokens:            cfg.ZipvoiceTokens,
					Encoder:           cfg.ZipvoiceTextModel,
					Decoder:           cfg.ZipvoiceFlowMatchingModel,
					DataDir:           cfg.ZipvoiceDataDir,
					Lexicon:           cfg.ZipvoicePinyinDict,
					Vocoder:           cfg.ZipvoiceVocoder,
					FeatScale:         cfg.ZipvoiceFeatScale,
					TShift:            cfg.ZipvoiceTShift,
					TargetRms:         cfg.ZipvoiceTargetRms,
					GuidanceScale:     cfg.ZipvoiceGuidanceScale,
				},
				NumThreads: cfg.NumThreads,
				Provider:   cfg.Provider,
				Debug:      cfg.DebugLevel,
			},
		}
	default:
		return nil, fmt.Errorf("unsupported tts engine %q", engine)
	}

	engineInstance := sherpa.NewOfflineTts(ttsCfg)
	if engineInstance == nil {
		return nil, fmt.Errorf("failed to initialize sherpa-onnx offline tts")
	}

	cfg.Engine = engine

	gen := &Generator{
		cfg:          cfg,
		tts:          engineInstance,
		zipvoiceByID: make(map[int]*zipvoiceRuntime),
	}

	if strings.ToLower(engine) == "zipvoice" {
		loader := newZipvoiceLoader(cfg)

		defaultRuntime, err := loader.loadRuntime(cfg.ZipvoicePromptAudio, cfg.ZipvoicePromptText, cfg.ZipvoiceNumSteps)
		if err != nil {
			return nil, err
		}
		gen.zipvoiceDefault = defaultRuntime

		for id, promptCfg := range cfg.ZipvoicePrompts {
			rt, err := loader.loadRuntime(promptCfg.PromptAudio, promptCfg.PromptText, promptCfg.NumSteps)
			if err != nil {
				return nil, fmt.Errorf("zipvoice prompt %d: %w", id, err)
			}
			gen.zipvoiceByID[id] = rt
		}
	}

	return gen, nil
}

// Generate returns synthesized speech samples for the given text.
func (g *Generator) Generate(text string, speakerID int, speed float32) ([]float32, int, error) {
	if g == nil || g.tts == nil {
		return nil, 0, fmt.Errorf("tts generator is not initialized")
	}
	if text == "" {
		return nil, 0, nil
	}
	if speed <= 0 {
		speed = 1.0
	}

	switch g.Engine() {
	case "zipvoice":
		return g.generateZipvoice(text, speakerID, speed)
	default:
		return g.generateStandard(text, speakerID, speed)
	}
}

// Engine returns the configured synthesis engine identifier.
func (g *Generator) Engine() string {
	if g == nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(g.cfg.Engine))
}

// Close releases the underlying sherpa resources.
func (g *Generator) Close() {
	if g == nil || g.tts == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()

	sherpa.DeleteOfflineTts(g.tts)
	g.tts = nil
}

func (g *Generator) generateStandard(text string, speakerID int, speed float32) ([]float32, int, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	audio := g.tts.Generate(text, speakerID, speed)
	if audio == nil {
		return nil, 0, fmt.Errorf("tts generation failed")
	}
	return audio.Samples, audio.SampleRate, nil
}

func (g *Generator) generateZipvoice(text string, speakerID int, speed float32) ([]float32, int, error) {
	// zipvoice 字母会异常，先临时过滤
	cleanText := sanitizeZipvoiceText(text)
	if cleanText == "" {
		return nil, 0, fmt.Errorf("zipvoice input becomes empty after sanitization")
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	rt := g.zipvoiceByID[speakerID]
	if rt == nil {
		rt = g.zipvoiceDefault
	}

	if rt != nil && len(rt.promptSamples) > 0 && rt.promptRate > 0 && rt.numSteps > 0 {
		audio := g.tts.GenerateWithZipvoice(
			cleanText,
			rt.promptText,
			rt.promptSamples,
			rt.promptRate,
			speed,
			rt.numSteps,
		)
		if audio == nil {
			return nil, 0, fmt.Errorf("zipvoice generation failed with prompt")
		}
		return audio.Samples, audio.SampleRate, nil
	}

	audio := g.tts.Generate(cleanText, speakerID, speed)
	if audio == nil {
		return nil, 0, fmt.Errorf("zipvoice generation failed")
	}
	return audio.Samples, audio.SampleRate, nil
}

func sanitizeZipvoiceText(text string) string {
	var b strings.Builder
	prevSpace := false
	for _, r := range text {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z':
			if !prevSpace {
				b.WriteRune(' ')
				prevSpace = true
			}
		case unicode.IsSpace(r):
			if !prevSpace {
				b.WriteRune(' ')
				prevSpace = true
			}
		default:
			b.WriteRune(r)
			prevSpace = false
		}
	}
	return strings.TrimSpace(b.String())
}

type zipvoiceLoader struct {
	defaultSteps int
}

func newZipvoiceLoader(cfg Config) zipvoiceLoader {
	steps := cfg.ZipvoiceNumSteps
	if steps <= 0 {
		steps = 4
	}
	return zipvoiceLoader{defaultSteps: steps}
}

func (l zipvoiceLoader) loadRuntime(audioPath, promptText string, numSteps int) (*zipvoiceRuntime, error) {
	audioPath = strings.TrimSpace(audioPath)
	promptText = strings.TrimSpace(promptText)

	if audioPath == "" {
		if promptText != "" {
			return nil, fmt.Errorf("zipvoice prompt text provided without audio")
		}
		return nil, nil
	}
	if promptText == "" {
		return nil, fmt.Errorf("zipvoice prompt text is required when prompt audio is provided")
	}

	wave := sherpa.ReadWave(audioPath)
	if wave == nil || len(wave.Samples) == 0 || wave.SampleRate <= 0 {
		return nil, fmt.Errorf("failed to load prompt audio %q", audioPath)
	}

	rt := &zipvoiceRuntime{
		promptSamples: append([]float32(nil), wave.Samples...),
		promptRate:    wave.SampleRate,
		promptText:    promptText,
		numSteps:      numSteps,
	}
	if rt.numSteps <= 0 {
		rt.numSteps = l.defaultSteps
	}
	if rt.numSteps <= 0 {
		rt.numSteps = 4
	}
	return rt, nil
}
