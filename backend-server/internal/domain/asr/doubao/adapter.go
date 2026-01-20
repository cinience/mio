package doubao

import (
	"backend-server/internal/domain/asr/types"
	"context"
)

// DoubaoV2Adapter 适配器，实现现有的AsrProvider接口
type DoubaoV2Adapter struct {
	engine *DoubaoV2ASR
}

// Process 实现一次性处理整段音频，返回完整识别结果
func (d *DoubaoV2Adapter) Process(pcmData []float32) (string, error) {
	return "", nil
}

// StreamingRecognize 实现流式识别接口
func (d *DoubaoV2Adapter) StreamingRecognize(ctx context.Context, audioStream <-chan []float32) (chan types.StreamingResult, error) {
	return d.engine.StreamingRecognize(ctx, audioStream)
}
