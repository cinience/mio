package types

// StreamingResult 流式识别结果
type StreamingResult struct {
	Text    string // 识别的文本
	IsFinal bool   // 是否为最终结果
	Error   error  // 错误信息
}
