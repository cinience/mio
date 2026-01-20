//go:build !webrtc_vad_cgo
// +build !webrtc_vad_cgo

package webrtc_vad

import (
	"fmt"
	"sync"
	"time"

	"backend-server/internal/domain/vad/inter"
	log "backend-server/internal/infrastructure/logger"

	"github.com/godeps/go-webrtcvad"
)

// WebRTCVAD WebRTC VAD 实现，现在实现了 Resource 接口
type WebRTCVAD struct {
	vadInst        webrtcvad.VadInst
	sampleRate     int          // 采样率
	mode           int          // VAD 模式
	frameSize      int          // 每帧采样数
	frameSizeBytes int          // 每帧字节数
	noiseFloor     float64      // RMS 能量阈值，低于此值视为静音
	initialized    bool         // 是否已初始化
	lastUsed       time.Time    // 最后使用时间
	createdAt      time.Time    // 创建时间
	mu             sync.RWMutex // 读写锁
	noiseLogger    noiseLogger
}

func (w *WebRTCVAD) hasVADInstance() bool {
	return w.vadInst != (webrtcvad.VadInst{})
}

var vadCreateMu sync.Mutex
var vadProcessMu sync.Mutex

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
	if w.initialized && w.hasVADInstance() {
		return nil
	}

	// go-webrtcvad 基于 wazero，在并发创建实例时会触发运行时崩溃；
	// 使用全局锁串行化底层 VAD 的创建与初始化过程。
	vadCreateMu.Lock()
	defer vadCreateMu.Unlock()

	// 计算帧大小
	w.frameSize = w.sampleRate / 1000 * FrameDuration
	w.frameSizeBytes = w.frameSize * 2 // 16-bit PCM

	// 创建 VAD 实例
	w.vadInst = webrtcvad.Create()
	if !w.hasVADInstance() {
		return fmt.Errorf("failed to create WebRTC VAD instance")
	}

	if err := webrtcvad.Init(w.vadInst); err != nil {
		webrtcvad.Free(w.vadInst)
		w.vadInst = webrtcvad.VadInst{}
		return fmt.Errorf("failed to initialize WebRTC VAD: %w", err)
	}

	if err := webrtcvad.SetMode(w.vadInst, w.mode); err != nil {
		webrtcvad.Free(w.vadInst)
		w.vadInst = webrtcvad.VadInst{}
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

	// 如果配置了噪声底限，先进行 RMS 能量检测
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
		return false, fmt.Errorf("unsupported sample rate: %d", sampleRate)
	}

	if frameSize <= 0 {
		frameSize = sampleRate / 1000 * FrameDuration
	}
	if frameSize <= 0 {
		return false, fmt.Errorf("invalid frame size: %d", frameSize)
	}
	frameSizeBytes := frameSize * 2

	// 将 float32 数据转换为 16-bit PCM 字节数组
	pcmBytes := float32ToPCMBytes(pcmData)
	if len(pcmBytes) < frameSizeBytes {
		return false, nil
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.initialized || !w.hasVADInstance() {
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

		vadProcessMu.Lock()
		isActive, err = webrtcvad.Process(w.vadInst, sampleRate, frameData, frameSize)
		vadProcessMu.Unlock()
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

	// 使用多数投票模式：只有当超过一半的帧判定为“有声”时才返回 true。
	// 旧逻辑使用 activityCount >= frameCount/2，在 frameCount == 1 时阈值为 0，
	// 会导致纯静音帧被误判为有声。因此改为 (frameCount+1)/2 以确保单帧场景阈值为 1。
	threshold := (frameCount + 1) / 2
	result := activityCount >= threshold
	log.Debugf("WebRTC VAD: frames=%d active=%d threshold=%d mode=%d sample_rate=%d input_len=%d result=%v",
		frameCount, activityCount, threshold, w.mode, sampleRate, len(pcmData), result)
	return result, nil
}

func (w *WebRTCVAD) IsVADExt(pcmData []float32, sampleRate int, frameSize int) (bool, error) {
	// 计算帧时长
	frameDuration := frameSize * 1000 / sampleRate

	// WebRTC VAD 原生支持的帧时长：10ms, 20ms, 30ms
	if frameDuration == 10 || frameDuration == 20 || frameDuration == 30 {
		// 标准帧长，直接处理
		return w.isVad(pcmData, sampleRate, frameSize)
	}

	// 非标准帧长：采用通用的多帧处理策略（借鉴 v0 设计）
	// 自动分割成多个标准帧（默认使用 30ms）并使用多数投票策略
	return w.processMultiFrame(pcmData, sampleRate, frameDuration)
}

// processMultiFrame 处理非标准帧长的音频数据
// 借鉴 v0 版本的通用设计：自动分割成多个标准帧并使用多数投票策略
// 这样可以自动支持任意长度的音频帧（60ms, 90ms, 120ms 等）
func (w *WebRTCVAD) processMultiFrame(pcmData []float32, sampleRate int, frameDuration int) (bool, error) {
	if len(pcmData) == 0 {
		return false, nil
	}

	// 如果配置了噪声底限，先进行 RMS 能量检测
	if w.noiseFloor > 0 {
		rms := calcRMS(pcmData)
		if rms < w.noiseFloor {
			w.logNoiseFilter(rms)
			return false, nil
		}
	}

	// 选择子帧时长：优先使用 30ms，如果音频太短则使用 20ms 或 10ms
	subFrameDuration := 30
	subFrameSize := sampleRate * subFrameDuration / 1000

	if len(pcmData) < subFrameSize {
		// 音频太短，尝试使用 20ms
		subFrameDuration = 20
		subFrameSize = sampleRate * subFrameDuration / 1000

		if len(pcmData) < subFrameSize {
			// 还是太短，使用 10ms
			subFrameDuration = 10
			subFrameSize = sampleRate * subFrameDuration / 1000

			if len(pcmData) < subFrameSize {
				// 数据不足一个最小帧，返回 false
				return false, nil
			}
		}
	}

	// 将 float32 数据转换为 int16 PCM 数据
	pcmBytes := float32ToPCMBytes(pcmData)
	frameSizeBytes := subFrameSize * 2 // 16-bit = 2 bytes

	// 循环处理每一帧，统计有语音的帧数
	activityCount := 0
	frameCount := 0

	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.initialized || !w.hasVADInstance() {
		return false, fmt.Errorf("VAD not initialized")
	}

	for i := 0; i+frameSizeBytes <= len(pcmBytes); i += frameSizeBytes {
		frameData := pcmBytes[i : i+frameSizeBytes]

		// 注意：webrtcvad.Process 的第4个参数是帧的样本数，不是字节数
		vadProcessMu.Lock()
		isActive, err := webrtcvad.Process(w.vadInst, sampleRate, frameData, subFrameSize)
		vadProcessMu.Unlock()

		if err != nil {
			return false, fmt.Errorf("WebRTC VAD process error: %w", err)
		}

		if isActive {
			activityCount++
		}
		frameCount++
	}

	// 多数投票策略：超过一半的帧检测到语音即认为有语音
	// 这种策略比 OR 策略更稳健，可以过滤偶然的噪声
	threshold := (frameCount + 1) / 2
	hasVoice := activityCount >= threshold // 使用 (frameCount+1)/2 处理奇数情况

	log.Debugf("WebRTC VAD multi-frame: frame_duration=%dms sub_frame=%dms frames=%d active=%d threshold=%d mode=%d sample_rate=%d input_len=%d result=%v",
		frameDuration, subFrameDuration, frameCount, activityCount, threshold, w.mode, sampleRate, len(pcmData), hasVoice)

	return hasVoice, nil
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
	if w.hasVADInstance() {
		webrtcvad.Free(w.vadInst)
		w.vadInst = webrtcvad.VadInst{}
	}
	w.initialized = false
}

// IsValid 检查资源是否有效 (实现 Resource 接口)
func (w *WebRTCVAD) IsValid() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()

	return w.initialized && w.hasVADInstance()
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

	if w.initialized && w.hasVADInstance() {
		return webrtcvad.SetMode(w.vadInst, mode)
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
