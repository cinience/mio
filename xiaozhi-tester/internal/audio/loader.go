package audio

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-audio/wav"
)

// Asset holds normalized PCM16 mono data ready for Opus encoding.
type Asset struct {
	Path              string
	Samples           []int16
	SampleRate        int
	Channels          int
	DurationSeconds   float64
	SourceSampleRate  int
	SourceNumChannels int
}

// LoadAsset reads a WAV or raw 16‑bit PCM file and converts it to mono PCM16.
func LoadAsset(path string, targetSampleRate int) (*Asset, error) {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".wav":
		return loadWAV(path, targetSampleRate)
	case ".pcm", ".raw":
		return loadRawPCM(path, targetSampleRate)
	default:
		return nil, fmt.Errorf("unsupported audio format %q (only .wav and .pcm)", ext)
	}
}

func loadWAV(path string, targetSampleRate int) (*Asset, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open wav: %w", err)
	}
	defer f.Close()

	decoder := wav.NewDecoder(f)
	if !decoder.IsValidFile() {
		return nil, fmt.Errorf("invalid WAV file")
	}

	buf, err := decoder.FullPCMBuffer()
	if err != nil {
		return nil, fmt.Errorf("read wav samples: %w", err)
	}
	if buf == nil || len(buf.Data) == 0 {
		return nil, fmt.Errorf("wav file has no audio samples")
	}

	srcSampleRate := int(buf.Format.SampleRate)
	srcChannels := int(buf.Format.NumChannels)
	if srcChannels <= 0 {
		srcChannels = 1
	}

	bitDepth := buf.SourceBitDepth
	if bitDepth == 0 {
		bitDepth = 16
	}
	scale := math.Pow(2, float64(bitDepth-1)) - 1
	if scale <= 0 {
		scale = 32767
	}

	frameCount := len(buf.Data) / srcChannels
	mono := make([]float64, frameCount)
	idx := 0
	for i := 0; i < frameCount; i++ {
		sum := 0.0
		for c := 0; c < srcChannels; c++ {
			sum += float64(buf.Data[idx])
			idx++
		}
		mono[i] = (sum / float64(srcChannels)) / scale
	}

	resampled := resampleLinear(mono, srcSampleRate, targetSampleRate)
	if len(resampled) == 0 {
		return nil, fmt.Errorf("no samples after resampling")
	}

	samples := make([]int16, len(resampled))
	for i, v := range resampled {
		switch {
		case v > 1:
			v = 1
		case v < -1:
			v = -1
		}
		samples[i] = int16(math.Round(v * 32767))
	}

	duration := float64(len(samples)) / float64(targetSampleRate)
	return &Asset{
		Path:              path,
		Samples:           samples,
		SampleRate:        targetSampleRate,
		Channels:          1,
		DurationSeconds:   duration,
		SourceSampleRate:  srcSampleRate,
		SourceNumChannels: srcChannels,
	}, nil
}

func loadRawPCM(path string, targetSampleRate int) (*Asset, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read raw pcm: %w", err)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("raw pcm file empty")
	}

	if len(data)%2 != 0 {
		data = data[:len(data)-1]
	}

	samples := make([]int16, len(data)/2)
	for i := 0; i < len(samples); i++ {
		samples[i] = int16(binary.LittleEndian.Uint16(data[i*2 : i*2+2]))
	}

	duration := float64(len(samples)) / float64(targetSampleRate)
	return &Asset{
		Path:              path,
		Samples:           samples,
		SampleRate:        targetSampleRate,
		Channels:          1,
		DurationSeconds:   duration,
		SourceSampleRate:  targetSampleRate,
		SourceNumChannels: 1,
	}, nil
}

func resampleLinear(input []float64, srcRate, dstRate int) []float64 {
	if len(input) == 0 {
		return nil
	}
	if srcRate <= 0 || dstRate <= 0 {
		out := make([]float64, len(input))
		copy(out, input)
		return out
	}
	if srcRate == dstRate {
		out := make([]float64, len(input))
		copy(out, input)
		return out
	}
	if len(input) == 1 {
		return []float64{input[0]}
	}

	duration := float64(len(input)-1) / float64(srcRate)
	outLen := int(math.Round(duration*float64(dstRate))) + 1
	if outLen <= 0 {
		outLen = 1
	}

	out := make([]float64, outLen)
	for i := 0; i < outLen; i++ {
		srcPos := float64(i) * float64(srcRate) / float64(dstRate)
		idx := int(math.Floor(srcPos))
		if idx >= len(input)-1 {
			out[i] = input[len(input)-1]
			continue
		}
		frac := srcPos - float64(idx)
		out[i] = input[idx]*(1-frac) + input[idx+1]*frac
	}
	return out
}
