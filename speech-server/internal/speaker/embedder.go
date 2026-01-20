package speaker

import (
	"fmt"
	"math"

	sherpa "github.com/k2-fsa/sherpa-onnx-go/sherpa_onnx"
)

// Embedder wraps sherpa speaker embedding extractor.
type Embedder struct {
	extractor       *sherpa.SpeakerEmbeddingExtractor
	sampleRate      int
	energyThreshold float32
	minDuration     float32
}

type EmbedderConfig struct {
	ModelPath       string
	SampleRate      int
	NumThreads      int
	Provider        string
	EnergyThreshold float32
	MinDuration     float32
}

func NewEmbedder(cfg EmbedderConfig) (*Embedder, error) {
	if cfg.SampleRate <= 0 {
		cfg.SampleRate = 16000
	}
	extractorCfg := &sherpa.SpeakerEmbeddingExtractorConfig{
		Model:      cfg.ModelPath,
		NumThreads: cfg.NumThreads,
		Debug:      0,
		Provider:   cfg.Provider,
	}
	extractor := sherpa.NewSpeakerEmbeddingExtractor(extractorCfg)
	if extractor == nil {
		return nil, fmt.Errorf("create speaker embedding extractor failed")
	}

	return &Embedder{
		extractor:       extractor,
		sampleRate:      cfg.SampleRate,
		energyThreshold: cfg.EnergyThreshold,
		minDuration:     cfg.MinDuration,
	}, nil
}

func (e *Embedder) Close() {
	if e == nil || e.extractor == nil {
		return
	}
	sherpa.DeleteSpeakerEmbeddingExtractor(e.extractor)
	e.extractor = nil
}

// Extract normalizes audio and returns an embedding.
func (e *Embedder) Extract(samples []float32, sampleRate int) ([]float32, error) {
	if e == nil || e.extractor == nil {
		return nil, fmt.Errorf("embedder not initialized")
	}
	if sampleRate <= 0 {
		sampleRate = e.sampleRate
	}
	if sampleRate != e.sampleRate {
		samples = resampleLinear(samples, sampleRate, e.sampleRate)
		sampleRate = e.sampleRate
	}

	metrics := analyze(samples, sampleRate)
	if metrics.DurationSec < e.minDuration {
		return nil, fmt.Errorf("audio too short: %.3fs < %.3fs", metrics.DurationSec, e.minDuration)
	}
	if metrics.RMS < e.energyThreshold {
		return nil, fmt.Errorf("audio energy too low: rms=%.4f threshold=%.4f", metrics.RMS, e.energyThreshold)
	}

	stream := e.extractor.CreateStream()
	defer sherpa.DeleteOnlineStream(stream)
	stream.AcceptWaveform(sampleRate, samples)
	stream.InputFinished()

	if !e.extractor.IsReady(stream) {
		return nil, fmt.Errorf("insufficient audio data for embedding")
	}

	embedding := e.extractor.Compute(stream)
	if len(embedding) == 0 {
		return nil, fmt.Errorf("empty embedding")
	}
	return normalizeL2(embedding), nil
}

func normalizeL2(vec []float32) []float32 {
	var sum float64
	for _, v := range vec {
		sum += float64(v) * float64(v)
	}
	if sum == 0 {
		return vec
	}
	norm := float32(1.0 / math.Sqrt(sum))
	out := make([]float32, len(vec))
	for i, v := range vec {
		out[i] = v * norm
	}
	return out
}
