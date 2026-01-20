//go:build webrtc_vad_cgo
// +build webrtc_vad_cgo

package webrtc_vad

import (
	"fmt"
	"sync"
	"time"

	"backend-server/internal/domain/vad/inter"
	log "backend-server/internal/infrastructure/logger"

	"github.com/baabaaox/go-webrtcvad"
)

// WebRTCVAD WebRTC VAD 实现，现在实现了 Resource 接口
type WebRTCVAD struct {
	webrtcVad      webrtcvad.VadInst // 注意：baabaaox 版本使用 VadInst 类型
	sampleRate     int               // 采样率
	mode           int               // VAD 模式
	frameSize      int               // 每帧采样数
	frameSizeBytes int               // 每帧字节数
	noiseFloor     float64           // RMS 能量阈值，低于此值视为静音
	initialized    bool              // 是否已初始化
	lastUsed       time.Time         // 最后使用时间
	createdAt      time.Time         // 创建时间
	mu             sync.RWMutex      // 读写锁
	noiseLogger    noiseLogger
}

// NewWebRTCVAD 创建新的 WebRTC VAD 实例
func NewWebRTCVAD() inter.VAD {
	return &WebRTCVAD{
		sampleRate: DefaultSampleRate,
		mode:       DefaultMode,
		lastUsed:   time.Now(),
		createdAt:  time.Now(),
	}
}

// NewWebRTCVADWithConfig 使用指定配置创建 WebRTC VAD 实例
func NewWebRTCVADWithConfig(sampleRate, mode int) (inter.VAD, error) {
	if !isValidSampleRate(sampleRate) {
		return nil, fmt.Errorf("unsupported sample rate: %d, supported rates: 8000, 16000, 32000, 48000", sampleRate)
	}
	if mode < 0 || mode > 3 {
		return nil, fmt.Errorf("invalid VAD mode: %d, must be 0-3", mode)
	}

	vad := &WebRTCVAD{
		sampleRate: sampleRate,
		mode:       mode,
		lastUsed:   time.Now(),
		createdAt:  time.Now(),
	}

	err := vad.init()
	if err != nil {
		return nil, err
	}

	return vad, nil
}

// init 初始化 WebRTC VAD
func (w *WebRTCVAD) init() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	return w.initLocked()
}

func (w *WebRTCVAD) initLocked() error {
	if w.initialized && w.webrtcVad != nil {
		return nil
	}

	// 计算帧大小
	w.frameSize = w.sampleRate / 1000 * FrameDuration
	w.frameSizeBytes = w.frameSize * 2 // 16-bit PCM

	if w.webrtcVad != nil {
		webrtcvad.Free(w.webrtcVad)
	}

	// 创建 VAD 实例（使用 baabaaox API）
	w.webrtcVad = webrtcvad.Create()
	if w.webrtcVad == nil {
		return fmt.Errorf("failed to create WebRTC VAD instance")
	}

	// 初始化 VAD 实例
	if err := webrtcvad.Init(w.webrtcVad); err != nil {
		webrtcvad.Free(w.webrtcVad)
		w.webrtcVad = nil
		return fmt.Errorf("failed to init WebRTC VAD: %w", err)
	}

	// 设置 VAD 模式
	if err := webrtcvad.SetMode(w.webrtcVad, w.mode); err != nil {
		webrtcvad.Free(w.webrtcVad)
		w.webrtcVad = nil
		return fmt.Errorf("failed to set WebRTC VAD mode: %w", err)
	}

	w.initialized = true
	w.lastUsed = time.Now()
	if w.createdAt.IsZero() {
		w.createdAt = w.lastUsed
	}
	return nil
}

func (w *WebRTCVAD) IsVAD(pcmData []float32) (bool, error) {
	return w.isVad(pcmData, w.sampleRate, w.frameSize)
}

// IsVAD 检测音频数据中的语音活动
func (w *WebRTCVAD) isVad(pcmData []float32, sampleRate int, frameSize int) (bool, error) {
	if len(pcmData) == 0 {
		return false, nil
	}

	if w.noiseFloor > 0 {
		rms := calcRMS(pcmData)
		if rms < w.noiseFloor {
			w.logNoiseFilter(rms)
			return false, nil
		}
	}

	if sampleRate <= 0 {
		sampleRate = w.sampleRate
	}
	if !isValidSampleRate(sampleRate) {
		return false, fmt.Errorf("unsupported sample rate: %d, supported rates: 8000, 16000, 32000, 48000", sampleRate)
	}

	if frameSize <= 0 {
		frameSize = sampleRate / 1000 * FrameDuration
	}
	if frameSize <= 0 {
		return false, fmt.Errorf("invalid frame size: %d", frameSize)
	}
	frameSizeBytes := frameSize * 2

	pcmBytes := float32ToPCMBytes(pcmData)
	if len(pcmBytes) < frameSizeBytes {
		return false, nil
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.initialized || w.webrtcVad == nil {
		if err := w.initLocked(); err != nil {
			return false, err
		}
	}

	w.lastUsed = time.Now()

	activityCount := 0
	var isActive bool
	var err error

	for offset := 0; offset+frameSizeBytes <= len(pcmBytes); offset += frameSizeBytes {
		frameData := pcmBytes[offset : offset+frameSizeBytes]
		isActive, err = webrtcvad.Process(w.webrtcVad, sampleRate, frameData, frameSize)
		if err != nil {
			return false, fmt.Errorf("WebRTC VAD process error: %w", err)
		}
		if isActive {
			activityCount++
		}
	}

	frameCount := len(pcmBytes) / frameSizeBytes
	if frameCount == 0 {
		return false, nil
	}

	threshold := (frameCount + 1) / 2
	result := activityCount >= threshold
	log.Debugf("WebRTC VAD: frames=%d active=%d threshold=%d mode=%d sample_rate=%d input_len=%d result=%v",
		frameCount, activityCount, threshold, w.mode, sampleRate, len(pcmData), result)

	return result, nil
}

func (w *WebRTCVAD) IsVADExt(pcmData []float32, sampleRate int, frameSize int) (bool, error) {
	return w.isVad(pcmData, sampleRate, frameSize)
}

// Reset 重置检测器状态
func (w *WebRTCVAD) Reset() error {
	return nil
}

// Close 关闭并释放资源 (实现 Resource 接口)
func (w *WebRTCVAD) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.closeLocked()
	return nil
}

func (w *WebRTCVAD) closeLocked() {
	if w.webrtcVad != nil {
		webrtcvad.Free(w.webrtcVad)
		w.webrtcVad = nil
	}
	w.initialized = false
}

// IsValid 检查资源是否有效 (实现 Resource 接口)
func (w *WebRTCVAD) IsValid() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()

	return w.initialized && w.webrtcVad != nil
}

// CreatedAt returns the time when the VAD instance was first initialized.
func (w *WebRTCVAD) CreatedAt() time.Time {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.createdAt
}

// SetMode 设置 VAD 敏感度模式
func (w *WebRTCVAD) SetMode(mode int) error {
	if mode < 0 || mode > 3 {
		return fmt.Errorf("invalid VAD mode: %d, must be 0-3", mode)
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	w.mode = mode

	if w.initialized {
		// 使用 baabaaox API: SetMode(vadInst, mode)
		return webrtcvad.SetMode(w.webrtcVad, mode)
	}

	return nil
}

// SetSampleRate 设置采样率
func (w *WebRTCVAD) SetSampleRate(sampleRate int) error {
	if !isValidSampleRate(sampleRate) {
		return fmt.Errorf("unsupported sample rate: %d, supported rates: 8000, 16000, 32000, 48000", sampleRate)
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	// 如果已经初始化，需要重新初始化
	if w.initialized {
		w.closeLocked()
	}

	w.sampleRate = sampleRate
	w.frameSize = sampleRate / 1000 * FrameDuration
	w.frameSizeBytes = w.frameSize * 2
	return nil
}

// GetSampleRate 获取当前采样率
func (w *WebRTCVAD) GetSampleRate() int {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.sampleRate
}

// GetMode 获取当前 VAD 模式
func (w *WebRTCVAD) GetMode() int {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.mode
}

// GetLastUsed 获取最后使用时间
func (w *WebRTCVAD) GetLastUsed() time.Time {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.lastUsed
}
