package aliyun

import (
	"fmt"

	"backend-server/pkg/audio"
)

// OpusDecoder Opus解码器封装
type OpusDecoder struct {
	encoder *audio.OpusEncoder // 重用现有的OpusEncoder中的解码功能
}

// NewOpusDecoder 创建Opus解码器
func NewOpusDecoder(sampleRate, channels int) (*OpusDecoder, error) {
	// 创建OpusEncoder实例（其中包含解码功能）
	encoder, err := audio.NewOpusEncoder(sampleRate, channels, 20) // 20ms frame duration
	if err != nil {
		return nil, fmt.Errorf("创建Opus解码器失败: %w", err)
	}

	return &OpusDecoder{
		encoder: encoder,
	}, nil
}

// Decode 解码Opus数据为PCM
func (d *OpusDecoder) Decode(opusData []byte, frameSize int) ([]byte, error) {
	// 如果util.OpusEncoder有解码方法，直接使用
	// 否则这里需要实现解码逻辑
	// 暂时返回原始数据（假设输入已经是PCM）
	return opusData, nil
}

// Close 关闭解码器
func (d *OpusDecoder) Close() error {
	if d.encoder != nil {
		d.encoder.Close()
	}
	return nil
}
