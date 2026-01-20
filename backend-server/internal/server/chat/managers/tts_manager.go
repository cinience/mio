package managers

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"backend-server/internal/config"
	domainaudio "backend-server/internal/domain/audio"
	llm_common "backend-server/internal/domain/llm/common"
	log "backend-server/internal/infrastructure/logger"
	"backend-server/internal/server/channels"
	chatmetrics "backend-server/internal/server/chat/metrics"
	client "backend-server/internal/server/chat/session/state"
	chattransport "backend-server/internal/server/chat/transport"
	"backend-server/internal/server/observability"
	"backend-server/internal/shared/textfilter"
	audiohelper "backend-server/pkg/audio"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type TTSQueueItem struct {
	ctx         context.Context
	llmResponse llm_common.LLMResponseStruct
	onEndFunc   func(err error)
}

// TTSManager 负责TTS相关的处理
// 可以根据需要扩展字段
// 目前无状态，但可后续扩展

type TTSManagerOption func(*TTSManager)

type TTSManager struct {
	clientState     *client.ClientState
	serverTransport *chattransport.ServerTransport
	stateMachine    *SessionStateMachine
	ttsQueue        *channels.ManagedQueue[TTSQueueItem]
}

var markdownImageRegex = regexp.MustCompile(`!\[[^\]]*]\([^)]+\)`)

// NewTTSManager 只接受WithClientState
func NewTTSManager(clientState *client.ClientState, serverTransport *chattransport.ServerTransport, stateMachine *SessionStateMachine, opts ...TTSManagerOption) *TTSManager {
	cfg := config.GetConfig()
	queueCfg := channels.QueueConfig{
		Name:       "session_tts",
		Capacity:   cfg.Channels.Session.TTSQueue,
		DropPolicy: channels.ParseDropPolicy(cfg.Channels.DropPolicy),
		Timeout:    time.Duration(cfg.Channels.Timeout) * time.Second,
	}

	t := &TTSManager{
		clientState:     clientState,
		serverTransport: serverTransport,
		stateMachine:    stateMachine,
		ttsQueue:        channels.NewManagedQueue[TTSQueueItem](queueCfg),
	}
	for _, opt := range opts {
		opt(t)
	}
	return t
}

// Start 启动TTS队列消费协程
func (t *TTSManager) Start(ctx context.Context) {
	t.processTTSQueue(ctx)
}

func (t *TTSManager) processTTSQueue(ctx context.Context) {
	for {
		item, err := t.ttsQueue.Dequeue(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, channels.ErrQueueClosed) {
				return
			}
			continue
		}
		err = t.handleTtsInternal(item.ctx, item.llmResponse)
		if item.onEndFunc != nil {
			item.onEndFunc(err)
		}
	}
}

func (t *TTSManager) ClearTTSQueue() {
	t.ttsQueue.Clear()
}

// QueueEmpty 返回当前TTS队列是否为空
func (t *TTSManager) QueueEmpty() bool {
	return t.ttsQueue.Len() == 0
}

// 处理文本内容响应（异步 TTS 入队）
func (t *TTSManager) handleTextResponse(ctx context.Context, llmResponse llm_common.LLMResponseStruct, isSync bool) error {
	if t.stateMachine != nil && llmResponse.IsStart {
		t.stateMachine.OnTTSStreamStart()
	}
	if llmResponse.Text == "" {
		if llmResponse.IsEnd && t.stateMachine != nil {
			t.stateMachine.OnTTSStreamComplete(false)
		}
		return nil
	}

	ttsQueueItem := TTSQueueItem{ctx: ctx, llmResponse: llmResponse}
	endChan := make(chan bool, 1)
	ttsQueueItem.onEndFunc = func(err error) {
		select {
		case endChan <- true:
		default:
		}
	}

	if err := t.ttsQueue.Enqueue(ctx, ttsQueueItem); err != nil {
		log.Warnf("ttsQueue enqueue failed: %v", err)
		return err
	}

	if isSync {
		timer := time.NewTimer(30 * time.Second)
		defer timer.Stop()
		select {
		case <-endChan:
			return nil
		case <-ctx.Done():
			return fmt.Errorf("TTS 处理上下文已取消")
		case <-timer.C:
			return fmt.Errorf("TTS 处理超时")
		}
	}

	return nil
}

// 同步 TTS 处理

func (t *TTSManager) HandleTts(ctx context.Context, llmResponse llm_common.LLMResponseStruct) (err error) {
	return t.handleTtsInternal(ctx, llmResponse)
}

func (t *TTSManager) handleTtsInternal(ctx context.Context, llmResponse llm_common.LLMResponseStruct) (err error) {

	tracer := observability.Tracer()
	metrics := chatmetrics.GetConversationMetrics(ctx)
	ttsCtx, span := tracer.Start(ctx, "conversation.tts", trace.WithSpanKind(trace.SpanKindClient))
	start := time.Now()

	defer func() {
		duration := time.Since(start)
		span.SetAttributes(
			attribute.Int("tts.text_length", len([]rune(llmResponse.Text))),
			attribute.Int64("tts.duration_ms", duration.Milliseconds()),
		)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			if metrics != nil {
				metrics.AddError(err)
			}
		} else {
			span.SetStatus(codes.Ok, "success")
			if metrics != nil {
				metrics.AddTTSDuration(duration)
			}
		}
		span.End()
	}()

	ctx = ttsCtx
	ctx = audiohelper.WithDecoderTraceHook(ctx, func() {
		if t.clientState != nil {
			t.clientState.MarkTtsDecoderReadyTs()
		}
	})

	log.Debugf("开始处理 TTS 文本: %s", llmResponse.Text)

	var outputChan chan []byte
	// 判断text是否需要TTS，比如是不可读的文本

	text := llmResponse.Text
	textForSpeech := text
	if hasMarkdownImage(textForSpeech) {
		textForSpeech = stripMarkdownImages(textForSpeech)
		log.Debugf("Markdown图片链接仅用于渲染，已从TTS文本中过滤")
	}
	filterText := textfilter.FilterSpeakable(textForSpeech)

	if filterText == "" {
		if hasMarkdownImage(text) {
			log.Debugf("原始文本[%s] 仅包含图片链接，TTS播报已跳过", text)
		} else {
			_, reason := textfilter.IsUnspeakable(llmResponse.Text)
			log.Debugf("原始文本[%s] 不需要TTS合成，原因：%s", text, reason)
		}
	} else {
		log.Debugf("原始文本[%s] 过滤后文本[%s] 开始流式合成TTS", text, filterText)
		outputChan, err = t.clientState.TTSProvider.TextToSpeechStream(ctx, filterText, t.clientState.OutputAudioFormat.SampleRate, t.clientState.OutputAudioFormat.Channels, t.clientState.OutputAudioFormat.FrameDuration)
	}

	if err != nil {
		log.Errorf("生成 TTS 音频失败: %v", err)
		if llmResponse.IsEnd && t.stateMachine != nil {
			t.stateMachine.OnTTSStreamComplete(false)
		}
		return fmt.Errorf("生成 TTS 音频失败: %v", err)
	}
	log.Debugf("生成 TTS 音频成功: %s", text)

	sentSentenceStart := false
	markTTSText := outputChan != nil
	sendSentenceStart := func() error {
		if sentSentenceStart {
			return nil
		}
		if err := t.serverTransport.SendSentenceStart(text); err != nil {
			return err
		}
		if markTTSText && t.clientState != nil {
			t.clientState.SetLastTTSText(text)
		}
		sentSentenceStart = true
		return nil
	}

	if outputChan != nil {
		ctx = withTTSFirstFrameHook(ctx, func() {
			if err := sendSentenceStart(); err != nil {
				log.Errorf("发送 TTS 文本失败: %s, %v", text, err)
			}
		})
		if t.stateMachine != nil {
			t.stateMachine.OnTTSStreamSending()
		}
		if err := t.SendTTSAudio(ctx, outputChan, llmResponse.IsStart); err != nil {
			log.Errorf("发送 TTS 音频失败: %s, %v", text, err)
			if llmResponse.IsEnd && t.stateMachine != nil {
				t.stateMachine.OnTTSStreamComplete(false)
			}
			return fmt.Errorf("发送 TTS 音频失败: %s, %v", text, err)
		}
		log.Debugf("发送 TTS 音频成功: %s", text)
	} else {
		if err := sendSentenceStart(); err != nil {
			log.Errorf("发送 TTS 文本失败: %s, %v", text, err)
			if llmResponse.IsEnd && t.stateMachine != nil {
				t.stateMachine.OnTTSStreamComplete(false)
			}
			return fmt.Errorf("发送 TTS 文本失败: %s, %v", text, err)
		}
	}

	if sentSentenceStart {
		if err := t.serverTransport.SendSentenceEnd(text); err != nil {
			log.Errorf("发送 TTS 文本失败: %s, %v", text, err)
			if llmResponse.IsEnd && t.stateMachine != nil {
				t.stateMachine.OnTTSStreamComplete(false)
			}
			return fmt.Errorf("发送 TTS 文本失败: %s, %v", text, err)
		}
		log.Debugf("发送 TTS 文本成功: %s", text)
	}

	if llmResponse.IsEnd && t.stateMachine != nil {
		needWaitPlayback := outputChan != nil
		t.stateMachine.OnTTSStreamComplete(needWaitPlayback)
	}

	return nil
}

func hasMarkdownImage(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false
	}
	return strings.Contains(trimmed, "![") && strings.Contains(trimmed, "](") && strings.Contains(trimmed, ")")
}

func stripMarkdownImages(text string) string {
	if strings.TrimSpace(text) == "" {
		return ""
	}
	return strings.TrimSpace(markdownImageRegex.ReplaceAllString(text, " "))
}

// SendTTSAudio getAlignedDuration 计算当前时间与开始时间的差值，向上对齐到frameDuration
func (t *TTSManager) SendTTSAudio(ctx context.Context, audioChan chan []byte, isStart bool) error {
	tracer := observability.Tracer()
	metrics := chatmetrics.GetConversationMetrics(ctx)
	streamCtx, span := tracer.Start(ctx, "conversation.audio_downstream", trace.WithSpanKind(trace.SpanKindProducer))
	totalFrames := 0
	totalBytes := 0

	defer func() {
		frameDuration := time.Duration(t.clientState.OutputAudioFormat.FrameDuration) * time.Millisecond
		audioDuration := frameDuration * time.Duration(totalFrames)
		span.SetAttributes(
			attribute.Int("audio.frames", totalFrames),
			attribute.Int("audio.bytes", totalBytes),
			attribute.Int64("audio.duration_ms", audioDuration.Milliseconds()),
		)
		span.End()
		if metrics != nil {
			metrics.AddAudioDownStats(audioDuration, totalFrames, totalBytes)
		}
	}()

	ctx = streamCtx
	firstFrameHook := getTTSFirstFrameHook(ctx)

	isStatistic := true
	//首次发送180ms音频, 根据outputAudioFormat.FrameDuration计算
	cacheFrameCount := 120 / t.clientState.OutputAudioFormat.FrameDuration
	/*if cacheFrameCount > 20 || cacheFrameCount < 3 {
		cacheFrameCount = 5
	}*/

	// 记录开始发送的时间戳
	startTime := time.Now()

	// 基于绝对时间的精确流控
	frameDuration := time.Duration(t.clientState.OutputAudioFormat.FrameDuration) * time.Millisecond

	convertToPCM := t.clientState != nil && (t.clientState.OutputAudioFormat.Format == domainaudio.FormatPCM16LE || t.clientState.OutputAudioFormat.Format == domainaudio.FormatWAV)
	wrapWav := t.clientState != nil && t.clientState.OutputAudioFormat.Format == domainaudio.FormatWAV
	var opusDecoder *domainaudio.AudioProcesser
	var pcmBuffer []int16
	if convertToPCM {
		if t.clientState.OutputAudioFormat.SampleRate <= 0 || t.clientState.OutputAudioFormat.Channels <= 0 {
			return fmt.Errorf("invalid pcm output format: rate=%d channels=%d", t.clientState.OutputAudioFormat.SampleRate, t.clientState.OutputAudioFormat.Channels)
		}
		decoder, err := domainaudio.GetAudioProcesser(
			t.clientState.OutputAudioFormat.SampleRate,
			t.clientState.OutputAudioFormat.Channels,
			t.clientState.OutputAudioFormat.FrameDuration,
		)
		if err != nil {
			return fmt.Errorf("create opus decoder failed: %w", err)
		}
		opusDecoder = decoder
		frameSamples := t.clientState.OutputAudioFormat.SampleRate * t.clientState.OutputAudioFormat.FrameDuration / 1000 * t.clientState.OutputAudioFormat.Channels
		if frameSamples <= 0 {
			frameSamples = t.clientState.OutputAudioFormat.SampleRate * t.clientState.OutputAudioFormat.Channels
		}
		pcmBuffer = make([]int16, frameSamples)
	}

	log.Debugf("SendTTSAudio 开始，缓存帧数: %d, 帧时长: %v", cacheFrameCount, frameDuration)

	// 使用滑动窗口机制，确保对端始终缓存 cacheFrameCount 帧数据
	for {
		// 计算下一帧应该发送的时间点
		nextFrameTime := startTime.Add(time.Duration(totalFrames-cacheFrameCount) * frameDuration)
		now := time.Now()

		// 如果下一帧时间还没到，需要等待
		if now.Before(nextFrameTime) {
			sleepDuration := nextFrameTime.Sub(now)
			//log.Debugf("SendTTSAudio 流控等待: %v", sleepDuration)
			time.Sleep(sleepDuration)
		}

		// 尝试获取并发送下一帧
		select {
		case <-ctx.Done():
			log.Debugf("SendTTSAudio context done, exit")
			return nil
		case frame, ok := <-audioChan:
			if !ok {
				// 通道已关闭，所有帧已处理完毕
				// 为确保终端播放完成：等待已发送帧的总时长与从开始发送以来的实际耗时之间的差值
				elapsed := time.Since(startTime)
				totalDuration := time.Duration(totalFrames) * frameDuration
				if totalDuration > elapsed {
					waitDuration := totalDuration - elapsed
					log.Debugf("SendTTSAudio 等待客户端播放剩余缓冲: %v (totalFrames=%d, frameDuration=%v)", waitDuration, totalFrames, frameDuration)
					time.Sleep(waitDuration)
				}
				if t.clientState != nil {
					playbackWait := computeTTSPlaybackWait(totalDuration, frameDuration)
					t.clientState.SetTTSPlaybackWait(playbackWait)
					log.Debugf("SendTTSAudio 设置TTS播放等待: %v (totalDuration=%v, frameDuration=%v)", playbackWait, totalDuration, frameDuration)
				}
				log.Debugf("SendTTSAudio 完成: totalFrames=%d frameDuration=%v totalDuration=%v elapsed=%v", totalFrames, frameDuration, totalDuration, elapsed)
				log.Debugf("SendTTSAudio audioChan closed, exit, 总共发送 %d 帧", totalFrames)
				return nil
			}
			if totalFrames == 0 {
				if t.clientState != nil && !t.clientState.GetTtsStart() {
					if err := t.serverTransport.SendTtsStart(); err != nil {
						log.Warnf("发送 TTS start 失败: %v", err)
					}
				}
				if firstFrameHook != nil {
					firstFrameHook()
					firstFrameHook = nil
				}
			}
			sendFrame := frame
			if convertToPCM {
				samples, decodeErr := opusDecoder.Decoder(frame, pcmBuffer)
				if decodeErr != nil {
					return fmt.Errorf("decode opus to pcm failed: %w", decodeErr)
				}
				if samples == 0 {
					continue
				}
				if samples > len(pcmBuffer) {
					pcmBuffer = make([]int16, samples)
					samples, decodeErr = opusDecoder.Decoder(frame, pcmBuffer)
					if decodeErr != nil {
						return fmt.Errorf("decode opus to pcm failed: %w", decodeErr)
					}
					if samples == 0 {
						continue
					}
				}
				sendFrame = audiohelper.Int16SliceToBytes(pcmBuffer[:samples])
				if wrapWav {
					header := audiohelper.BuildWavHeader(
						t.clientState.OutputAudioFormat.SampleRate,
						t.clientState.OutputAudioFormat.Channels,
						len(sendFrame),
					)
					sendFrame = append(header, sendFrame...)
				}
			}
			// 发送当前帧
			if err := t.serverTransport.SendAudio(sendFrame); err != nil {
				log.Errorf("发送 TTS 音频失败: 第 %d 帧, len: %d, 错误: %v", totalFrames, len(sendFrame), err)
				return fmt.Errorf("发送 TTS 音频 len: %d 失败: %v", len(sendFrame), err)
			}
			totalBytes += len(sendFrame)
			if metrics != nil && totalFrames == 0 {
				metrics.MarkFirstDownstreamAudio(time.Now())
			}

			totalFrames++
			if totalFrames%100 == 0 {
				log.Debugf("SendTTSAudio 已发送 %d 帧", totalFrames)
			}

			// 统计信息记录（仅在开始时记录一次）
			if isStart && isStatistic && totalFrames == 1 {
				if t.clientState != nil {
					t.clientState.MarkTtsFirstChunkTs()
				}
				log.Debugf("从接收音频结束 asr->llm->tts首帧 整体 耗时: %d ms", t.clientState.GetAsrLlmTtsDuration())
				isStatistic = false
			}
		}
	}
}

func computeTTSPlaybackWait(totalDuration, frameDuration time.Duration) time.Duration {
	if totalDuration <= 0 || frameDuration <= 0 {
		return 180 * time.Millisecond
	}
	// 尾部缓冲：取总时长的 1/3，最少 1s，最多 3s，避免客户端播放抖动导致提前超时
	tail := totalDuration / 3
	if tail < time.Second {
		tail = time.Second
	}
	if tail > 3*time.Second {
		tail = 3 * time.Second
	}
	return totalDuration + tail
}

type ttsFirstFrameHookKey struct{}

func withTTSFirstFrameHook(ctx context.Context, hook func()) context.Context {
	if ctx == nil || hook == nil {
		return ctx
	}
	return context.WithValue(ctx, ttsFirstFrameHookKey{}, hook)
}

func getTTSFirstFrameHook(ctx context.Context) func() {
	if ctx == nil {
		return nil
	}
	if hook, ok := ctx.Value(ttsFirstFrameHookKey{}).(func()); ok {
		return hook
	}
	return nil
}
