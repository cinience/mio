package state

import (
	"backend-server/internal/domain/asr"
	asr_types "backend-server/internal/domain/asr/types"
	log "backend-server/internal/infrastructure/logger"
	"bytes"
	"context"
	"fmt"
	"sync"
)

type Asr struct {
	lock sync.RWMutex
	// ASR 提供者
	Ctx              context.Context
	Cancel           context.CancelFunc
	AsrProvider      asr.AsrProvider
	AsrEnd           chan bool
	AsrAudioChannel  *RingAudioChannel              //流式音频输入的环形缓冲区
	AsrResultChannel chan asr_types.StreamingResult //流式输出asr识别到的结果片断
	AsrResult        bytes.Buffer                   //保存此次识别到的最终文本
	Statue           int                            //0:初始化 1:识别中 2:识别结束
	AutoEnd          bool                           //auto_end是指使用asr自动判断结束，不再使用vad模块
	RingBufferFrames int                            //环形缓冲区容量（帧数）
}

func (a *Asr) Reset() {
	a.AsrResult.Reset()
}

func (a *Asr) RetireAsrResult(ctx context.Context) (string, error) {
	return a.RetireAsrResultWithHandler(ctx, nil)
}

func (a *Asr) RetireAsrResultWithHandler(ctx context.Context, handler func(result asr_types.StreamingResult) error) (string, error) {
	defer func() {
		a.Reset()
	}()

	for {
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("RetireAsrResult ctx Done")
		case result, ok := <-a.AsrResultChannel:
			log.Debugf("asr result: %s, ok: %+v, isFinal: %+v, error: %+v", result.Text, ok, result.IsFinal, result.Error)
			if !ok {
				log.Debugf("asr result channel closed")
				return "", fmt.Errorf("asr result channel closed before final result")
			}
			if result.Error != nil {
				return "", result.Error
			}
			a.AsrResult.WriteString(result.Text)
			currentText := a.AsrResult.String()
			if handler != nil {
				// 通过 handler 传递当前累计的识别文本，便于实时模式回调
				streamingResult := asr_types.StreamingResult{
					Text:    currentText,
					IsFinal: result.IsFinal,
					Error:   result.Error,
				}
				if err := handler(streamingResult); err != nil {
					return "", err
				}
			}
			if a.AutoEnd || result.IsFinal {
				return currentText, nil
			}
		}
	}
}

func (a *Asr) Stop() {
	a.lock.Lock()
	defer a.lock.Unlock()
	if a.AsrAudioChannel != nil {
		log.Debugf("停止asr")
		a.AsrAudioChannel.Close() //关闭环形缓冲区
		a.AsrAudioChannel = nil   //释放引用，等待下次初始化
	}
}

func (a *Asr) AddAudioData(pcmFrameData []float32) error {
	a.lock.Lock()
	defer a.lock.Unlock()
	if a.AsrAudioChannel != nil {
		a.AsrAudioChannel.Write(pcmFrameData)
	}
	return nil
}

type AsrAudioBuffer struct {
	buffer          []float32
	capacity        int
	start           int
	length          int
	frameSize       int
	audioBufferLock sync.RWMutex
	totalSamples    int64
}

const defaultAsrBufferDurationMs = 2000

// Configure resets the buffer to operate with the provided frame size and max frame count.
func (a *AsrAudioBuffer) Configure(frameSize, maxFrameCount int) {
	if frameSize <= 0 {
		frameSize = 1
	}
	if maxFrameCount <= 0 {
		maxFrameCount = 1
	}

	capacity := frameSize * maxFrameCount

	a.audioBufferLock.Lock()
	defer a.audioBufferLock.Unlock()

	if cap(a.buffer) != capacity {
		a.buffer = make([]float32, capacity)
	} else {
		// reuse existing buffer; ensure full length available for copy operations
		a.buffer = a.buffer[:capacity]
	}

	a.capacity = capacity
	a.frameSize = frameSize
	a.start = 0
	a.length = 0
	a.totalSamples = 0
}

func (a *AsrAudioBuffer) AddAsrAudioData(pcmFrameData []float32) {
	if len(pcmFrameData) == 0 {
		return
	}

	a.audioBufferLock.Lock()
	defer a.audioBufferLock.Unlock()

	if a.capacity == 0 {
		return
	}

	data := pcmFrameData
	if len(data) > a.capacity {
		data = data[len(data)-a.capacity:]
	}

	if a.length+len(data) > a.capacity {
		drop := a.length + len(data) - a.capacity
		a.start = (a.start + drop) % a.capacity
		a.length -= drop
	}

	writePos := (a.start + a.length) % a.capacity
	first := len(data)
	if writePos+first > a.capacity {
		first = a.capacity - writePos
	}

	copy(a.buffer[writePos:writePos+first], data[:first])
	if first < len(data) {
		copy(a.buffer[:len(data)-first], data[first:])
	}

	a.length += len(data)
	a.totalSamples += int64(len(data))
}

func (a *AsrAudioBuffer) GetAsrDataSize() int {
	a.audioBufferLock.RLock()
	defer a.audioBufferLock.RUnlock()
	return a.length
}

func (a *AsrAudioBuffer) GetFrameCount() int {
	a.audioBufferLock.RLock()
	defer a.audioBufferLock.RUnlock()
	if a.frameSize == 0 {
		return 0
	}
	return a.length / a.frameSize
}

func (a *AsrAudioBuffer) FrameSize() int {
	a.audioBufferLock.RLock()
	defer a.audioBufferLock.RUnlock()
	return a.frameSize
}

// CopyLatest writes the newest frameCount frames into dst (growing dst as needed) and returns the slice used.
func (a *AsrAudioBuffer) CopyLatest(frameCount int, dst []float32) []float32 {
	if frameCount <= 0 {
		return dst[:0]
	}

	a.audioBufferLock.RLock()
	defer a.audioBufferLock.RUnlock()

	if a.length == 0 || a.frameSize == 0 {
		return dst[:0]
	}

	required := frameCount * a.frameSize
	if required > a.length {
		required = a.length
	}

	if cap(dst) < required {
		dst = make([]float32, required)
	} else {
		dst = dst[:required]
	}

	if required == 0 {
		return dst[:0]
	}

	start := (a.start + a.length - required) % a.capacity
	first := required
	if start+first > a.capacity {
		first = a.capacity - start
	}

	copy(dst[:first], a.buffer[start:start+first])
	if first < required {
		copy(dst[first:], a.buffer[:required-first])
	}
	return dst
}

// Drain copies all buffered samples into dst, clears the buffer, and returns the copied slice.
func (a *AsrAudioBuffer) Drain(dst []float32) []float32 {
	a.audioBufferLock.Lock()
	defer a.audioBufferLock.Unlock()

	if a.length == 0 {
		return dst[:0]
	}

	if cap(dst) < a.length {
		dst = make([]float32, a.length)
	} else {
		dst = dst[:a.length]
	}

	first := a.length
	if a.start+first > a.capacity {
		first = a.capacity - a.start
	}

	copy(dst[:first], a.buffer[a.start:a.start+first])
	if first < a.length {
		copy(dst[first:], a.buffer[:a.length-first])
	}

	a.start = 0
	a.length = 0
	return dst
}

func (a *AsrAudioBuffer) RemoveAsrAudioData(frameCount int) {
	if frameCount <= 0 {
		return
	}

	a.audioBufferLock.Lock()
	defer a.audioBufferLock.Unlock()

	if a.length == 0 || a.frameSize == 0 {
		return
	}

	remove := frameCount * a.frameSize
	if remove >= a.length {
		a.start = 0
		a.length = 0
		return
	}

	a.start = (a.start + remove) % a.capacity
	a.length -= remove
}

func (a *AsrAudioBuffer) ClearAsrAudioData() {
	a.audioBufferLock.Lock()
	defer a.audioBufferLock.Unlock()
	a.start = 0
	a.length = 0
}

// GetSince returns the samples appended after the given mark along with the updated mark.
func (a *AsrAudioBuffer) GetSince(mark int64) ([]float32, int64) {
	a.audioBufferLock.RLock()
	defer a.audioBufferLock.RUnlock()

	if a.length == 0 {
		return nil, a.totalSamples
	}

	startTotal := a.totalSamples - int64(a.length)
	if startTotal < 0 {
		startTotal = 0
	}
	if mark < startTotal {
		mark = startTotal
	}
	if mark > a.totalSamples {
		mark = a.totalSamples
	}

	skip := int(mark - startTotal)
	if skip >= a.length {
		return nil, a.totalSamples
	}

	count := a.length - skip
	result := make([]float32, count)
	idx := (a.start + skip) % a.capacity
	first := count
	if idx+first > a.capacity {
		first = a.capacity - idx
	}
	copy(result, a.buffer[idx:idx+first])
	if first < count {
		copy(result[first:], a.buffer[:count-first])
	}

	return result, a.totalSamples
}
