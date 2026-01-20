package audio

import (
	"fmt"
	"sync"

	"github.com/godeps/opus"
)

// OpusEncoder Opus编码器包装器
type OpusEncoder struct {
	encoder       *opus.Encoder
	sampleRate    int
	channels      int
	frameDuration int
	frameSize     int    // 每帧的样本数
	opusBuffer    []byte // 编码输出缓冲区
	mutex         sync.Mutex
}

// NewOpusEncoder 创建新的Opus编码器
// sampleRate: 采样率 (e.g., 24000)
// channels: 声道数 (1 for mono, 2 for stereo)
// frameDurationMs: 帧时长，毫秒 (10, 20, 40, or 60)
func NewOpusEncoder(sampleRate, channels, frameDurationMs int) (*OpusEncoder, error) {
	enc, err := opus.NewEncoder(sampleRate, channels, opus.AppAudio)
	if err != nil {
		return nil, fmt.Errorf("创建Opus编码器失败: %v", err)
	}

	frameSize := sampleRate * frameDurationMs / 1000
	opusBuffer := make([]byte, 4000) // 足够大的缓冲区

	return &OpusEncoder{
		encoder:       enc,
		sampleRate:    sampleRate,
		channels:      channels,
		frameDuration: frameDurationMs,
		frameSize:     frameSize,
		opusBuffer:    opusBuffer,
	}, nil
}

// Encode 编码PCM数据为Opus格式
// pcmData: PCM音频数据 (16-bit little-endian)
// 返回编码后的Opus帧，如果没有足够数据则返回nil
func (e *OpusEncoder) Encode(pcmData []byte) ([]byte, error) {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	// 将字节转换为int16切片
	pcmSamples := BytesToInt16Slice(pcmData)

	// 检查数据长度是否足够一帧
	expectedSamples := e.frameSize * e.channels
	if len(pcmSamples) < expectedSamples {
		// 不足一帧，用零填充
		paddedSamples := make([]int16, expectedSamples)
		copy(paddedSamples, pcmSamples)
		pcmSamples = paddedSamples
	} else if len(pcmSamples) > expectedSamples {
		// 超过一帧，截取一帧
		pcmSamples = pcmSamples[:expectedSamples]
	}

	// 编码
	n, err := e.encoder.Encode(pcmSamples, e.opusBuffer)
	if err != nil {
		return nil, fmt.Errorf("Opus编码失败: %v", err)
	}

	if n == 0 {
		return nil, nil
	}

	// 复制编码后的数据到新切片
	result := make([]byte, n)
	copy(result, e.opusBuffer[:n])

	return result, nil
}

// SetBitrate 设置比特率
func (e *OpusEncoder) SetBitrate(bitrate int) error {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	return e.encoder.SetBitrate(bitrate)
}

// Close 关闭编码器
func (e *OpusEncoder) Close() error {
	// opus.Encoder 没有 Close 方法，只需要清理资源
	e.encoder = nil
	e.opusBuffer = nil
	return nil
}

// GetFrameSize 获取每帧的样本数
func (e *OpusEncoder) GetFrameSize() int {
	return e.frameSize
}

// GetFrameDuration 获取帧时长(毫秒)
func (e *OpusEncoder) GetFrameDuration() int {
	return e.frameDuration
}

// GetFrameBytes 获取每帧的字节数 (16-bit PCM)
func (e *OpusEncoder) GetFrameBytes() int {
	return e.frameSize * e.channels * 2
}

// BytesToInt16Slice 将字节切片转换为int16切片 (little-endian)
func BytesToInt16Slice(data []byte) []int16 {
	if len(data)%2 != 0 {
		// 奇数长度，添加一个零字节
		data = append(data, 0)
	}

	result := make([]int16, len(data)/2)
	for i := 0; i < len(result); i++ {
		// Little-endian: 低字节在前
		result[i] = int16(data[i*2]) | int16(data[i*2+1])<<8
	}
	return result
}

// 注意：Int16SliceToBytes 函数已在 voice.go 中定义，这里不重复定义
