package realtime

import (
	"encoding/binary"
	"math"
)

func float32ToPCM16(samples []float32) []byte {
	if len(samples) == 0 {
		return nil
	}
	data := make([]byte, len(samples)*2)
	for i, sample := range samples {
		v := sample
		if v > 1 {
			v = 1
		} else if v < -1 {
			v = -1
		}
		scaled := int16(math.Round(float64(v) * 32767.0))
		binary.LittleEndian.PutUint16(data[i*2:], uint16(scaled))
	}
	return data
}

func pcm16ToFloat32(data []byte) []float32 {
	if len(data)%2 != 0 {
		data = data[:len(data)-1]
	}

	samples := make([]float32, len(data)/2)
	for i := 0; i < len(samples); i++ {
		raw := int16(binary.LittleEndian.Uint16(data[i*2:]))
		samples[i] = float32(raw) / 32768.0
	}
	return samples
}

func resampleLinear(samples []float32, inputRate, outputRate int) []float32 {
	if inputRate <= 0 {
		inputRate = outputRate
	}
	if outputRate <= 0 {
		outputRate = inputRate
	}
	if inputRate == outputRate || len(samples) == 0 {
		out := make([]float32, len(samples))
		copy(out, samples)
		return out
	}

	ratio := float64(inputRate) / float64(outputRate)
	outLen := int(float64(len(samples)) / ratio)
	if outLen <= 0 {
		outLen = 1
	}
	result := make([]float32, outLen)

	for i := 0; i < outLen; i++ {
		srcIndex := float64(i) * ratio
		idx := int(srcIndex)
		if idx >= len(samples)-1 {
			result[i] = samples[len(samples)-1]
			continue
		}
		frac := srcIndex - float64(idx)
		result[i] = samples[idx]*(1-float32(frac)) + samples[idx+1]*float32(frac)
	}

	return result
}

func alignTTSSampleRate(samples []float32, currentRate, desiredRate int) ([]float32, int) {
	targetRate := desiredRate
	if targetRate <= 0 {
		targetRate = currentRate
	}
	if currentRate <= 0 {
		currentRate = targetRate
	}

	if len(samples) == 0 {
		return nil, targetRate
	}

	if currentRate == targetRate {
		out := make([]float32, len(samples))
		copy(out, samples)
		return out, targetRate
	}

	return resampleLinear(samples, currentRate, targetRate), targetRate
}

// Float32ToPCM16 is exported for callers that need PCM conversions without
// depending on the realtime internals.
func Float32ToPCM16(samples []float32) []byte {
	return float32ToPCM16(samples)
}

// ResamplePCM returns a new slice resampled to the desired rate using linear
// interpolation. When the sample rates already match, a copy of the input is
// returned.
func ResamplePCM(samples []float32, inputRate, outputRate int) []float32 {
	return resampleLinear(samples, inputRate, outputRate)
}

// calculateRMS computes the root mean square (RMS) energy of audio samples.
// RMS is a measure of the average audio power/volume.
func calculateRMS(samples []float32) float32 {
	if len(samples) == 0 {
		return 0
	}
	var sum float64
	for _, sample := range samples {
		sum += float64(sample) * float64(sample)
	}
	return float32(math.Sqrt(sum / float64(len(samples))))
}

// isSilentOrNoise checks if audio is likely silence or noise based on RMS energy.
// energyThreshold: minimum RMS energy level (typical range 0.001-0.05)
// Returns true if the audio should be filtered out.
func isSilentOrNoise(samples []float32, energyThreshold float32) bool {
	if len(samples) == 0 {
		return true
	}
	rms := calculateRMS(samples)
	return rms < energyThreshold
}

// AudioQualityMetrics contains metrics for evaluating audio quality.
type AudioQualityMetrics struct {
	RMS           float32 // Root mean square energy
	Peak          float32 // Peak amplitude
	DurationSec   float32 // Duration in seconds
	SampleRate    int     // Sample rate
	IsLikelyNoise bool    // Whether this is likely noise
}

// analyzeAudioQuality analyzes audio samples and returns quality metrics.
func analyzeAudioQuality(samples []float32, sampleRate int, energyThreshold, minDuration float32) AudioQualityMetrics {
	metrics := AudioQualityMetrics{
		SampleRate: sampleRate,
	}

	if len(samples) == 0 {
		metrics.IsLikelyNoise = true
		return metrics
	}

	// Calculate duration
	metrics.DurationSec = float32(len(samples)) / float32(sampleRate)

	// Calculate RMS energy
	metrics.RMS = calculateRMS(samples)

	// Find peak amplitude
	var peak float32
	for _, sample := range samples {
		abs := sample
		if abs < 0 {
			abs = -abs
		}
		if abs > peak {
			peak = abs
		}
	}
	metrics.Peak = peak

	metrics.IsLikelyNoise = isSilentOrNoise(samples, energyThreshold) || metrics.DurationSec < minDuration

	return metrics
}
