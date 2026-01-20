package realtime

import (
	"time"

	"speech-server/internal/asr"
)

const defaultVADSampleRate = 16000

type vadSessionState struct {
	enabled           bool
	model             string
	detector          asr.VADDetector
	lease             *asr.VADLease
	sampleRate        int
	prefixPadding     time.Duration
	prefixSamples     int
	prefixBuffer      []float32
	threshold         float32
	silenceSeconds    float32
	createResponse    bool
	interruptResponse bool
}

func (v *vadSessionState) close() {
	if v == nil {
		return
	}
	if v.lease != nil {
		v.lease.Release()
	} else if v.detector != nil {
		v.detector.Close()
	}
	v.detector = nil
	v.lease = nil
	v.prefixBuffer = nil
}

func (v *vadSessionState) setPrefixPadding(d time.Duration) {
	v.prefixPadding = d
	if d <= 0 || v.sampleRate <= 0 {
		v.prefixSamples = 0
		v.prefixBuffer = nil
		return
	}
	samples := int(float64(v.sampleRate) * d.Seconds())
	if samples < 0 {
		samples = 0
	}
	v.prefixSamples = samples
	if len(v.prefixBuffer) > samples {
		trimmed := make([]float32, samples)
		copy(trimmed, v.prefixBuffer[len(v.prefixBuffer)-samples:])
		v.prefixBuffer = trimmed
	}
}

func (v *vadSessionState) refreshPrefixFrom(samples []float32) {
	if v.prefixSamples <= 0 {
		v.prefixBuffer = nil
		return
	}
	if len(samples) <= v.prefixSamples {
		v.prefixBuffer = append([]float32(nil), samples...)
		return
	}
	start := len(samples) - v.prefixSamples
	v.prefixBuffer = append([]float32(nil), samples[start:]...)
}

func (v *vadSessionState) combineWithPrefix(segment []float32) []float32 {
	var combined []float32
	if len(v.prefixBuffer) > 0 {
		combined = append(combined, v.prefixBuffer...)
	}
	if len(segment) > 0 {
		combined = append(combined, segment...)
	}
	v.refreshPrefixFrom(combined)
	return combined
}

func (v *vadSessionState) pushHistory(samples []float32) {
	if v.prefixSamples <= 0 || len(samples) == 0 {
		return
	}
	combined := append(v.prefixBuffer, samples...)
	if len(combined) > v.prefixSamples {
		combined = combined[len(combined)-v.prefixSamples:]
	}
	v.prefixBuffer = append([]float32(nil), combined...)
}

func (v *vadSessionState) resetHistory() {
	v.prefixBuffer = nil
}
