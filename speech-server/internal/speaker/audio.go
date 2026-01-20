package speaker

import "math"

// resampleLinear copies realtime.resampleLinear logic (small helper).
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

type audioMetrics struct {
	RMS         float32
	DurationSec float32
	Peak        float32
}

func analyze(samples []float32, sampleRate int) audioMetrics {
	m := audioMetrics{}
	if len(samples) == 0 || sampleRate <= 0 {
		return m
	}
	m.DurationSec = float32(len(samples)) / float32(sampleRate)
	var sum float64
	var peak float32
	for _, s := range samples {
		sum += float64(s) * float64(s)
		if s < 0 {
			s = -s
		}
		if s > peak {
			peak = s
		}
	}
	m.RMS = float32(math.Sqrt(sum / float64(len(samples))))
	m.Peak = peak
	return m
}
