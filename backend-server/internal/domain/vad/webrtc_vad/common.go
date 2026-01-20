package webrtc_vad

import (
	"encoding/binary"
	"math"
	"sync"
	"time"

	log "backend-server/internal/infrastructure/logger"
)

const (
	// DefaultSampleRate WebRTC VAD 支持的采样率 (8000, 16000, 32000, 48000)
	DefaultSampleRate = 16000
	// DefaultMode VAD 敏感度模式 (0: 最不敏感, 3: 最敏感)。
	DefaultMode = 2
	// FrameDuration 帧持续时间 (ms)，WebRTC VAD 支持 10ms, 20ms, 30ms
	FrameDuration = 20
	// noiseFilterLogCooldown 控制 RMS 噪声过滤日志的最小间隔，避免刷屏
	noiseFilterLogCooldown = time.Second
	// noiseFilterLogMaxBurst 限制在冷却期内允许抑制的日志次数，超过则强制输出一次
	noiseFilterLogMaxBurst = 200
)

type noiseLogger struct {
	mu    sync.Mutex
	last  time.Time
	count int
}

func (l *noiseLogger) log(noiseFloor, rms float64) {
	if noiseFloor <= 0 {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	l.count++
	now := time.Now()
	shouldLog := l.last.IsZero() || now.Sub(l.last) >= noiseFilterLogCooldown || l.count >= noiseFilterLogMaxBurst
	if !shouldLog {
		return
	}

	suppressed := l.count - 1
	l.count = 0
	l.last = now

	if suppressed > 0 {
		log.Debugf("WebRTC VAD: RMS 噪声过滤 rms=%.6f < noise_floor=%.6f, 跳过 VAD 检测 (suppressed %d similar events)",
			rms, noiseFloor, suppressed)
		return
	}

	log.Debugf("WebRTC VAD: RMS 噪声过滤 rms=%.6f < noise_floor=%.6f, 跳过 VAD 检测",
		rms, noiseFloor)
}

func float32ToPCMBytes(samples []float32) []byte {
	pcmBytes := make([]byte, len(samples)*2)

	for i, sample := range samples {
		var intSample int16
		if sample > 1.0 {
			intSample = 32767
		} else if sample < -1.0 {
			intSample = -32768
		} else {
			intSample = int16(sample * 32767)
		}

		binary.LittleEndian.PutUint16(pcmBytes[i*2:], uint16(intSample))
	}

	return pcmBytes
}

func isValidSampleRate(sampleRate int) bool {
	switch sampleRate {
	case 8000, 16000, 32000, 48000:
		return true
	default:
		return false
	}
}

func calcRMS(data []float32) float64 {
	if len(data) == 0 {
		return 0
	}
	var sum float64
	for _, v := range data {
		sum += float64(v * v)
	}
	return math.Sqrt(sum / float64(len(data)))
}

func (w *WebRTCVAD) logNoiseFilter(rms float64) {
	w.noiseLogger.log(w.noiseFloor, rms)
}
