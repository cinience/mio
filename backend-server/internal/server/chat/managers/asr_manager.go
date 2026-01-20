package managers

// VAD 优化说明:
// 1. 通过 noise_floor (RMS 能量阈值) 在进入 WebRTC VAD 之前先行过滤静音/低噪声帧。
// 2. 结合 min_active_frames 输入，只有连续多帧判定为有声才触发首帧回放，避免瞬时噪声误判。
// 3. 支持全局配置与设备级覆盖，调整敏感度时统一参考日志中输出的噪声阈值与帧计数。

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"backend-server/internal/config"
	"backend-server/internal/domain/audio"
	log "backend-server/internal/infrastructure/logger"
	chatmetrics "backend-server/internal/server/chat/metrics"
	client "backend-server/internal/server/chat/session/state"
	chattransport "backend-server/internal/server/chat/transport"
	chatvad "backend-server/internal/server/chat/vad"
)

type ASRManagerOption func(*ASRManager)

const (
	asrProviderChannelSize = 10
	minASRRestartGap       = 300 * time.Millisecond
	audioStatsLogInterval  = 10 * time.Second
)

type ASRManager struct {
	clientState        *client.ClientState
	serverTransport    *chattransport.ServerTransport
	interruptHandler   func(reason string)
	pendingListenStart atomic.Bool
	restartInFlight    atomic.Bool
	lastRestartAtMs    atomic.Int64
}

func NewASRManager(clientState *client.ClientState, serverTransport *chattransport.ServerTransport, opts ...ASRManagerOption) *ASRManager {
	asr := &ASRManager{
		clientState:     clientState,
		serverTransport: serverTransport,
	}
	for _, opt := range opts {
		opt(asr)
	}
	return asr
}

// SetInterruptHandler 注册实时打断回调（用于 VAD/ASR 检测到新语音时取消下游 LLM/TTS）
func (a *ASRManager) SetInterruptHandler(fn func(reason string)) {
	a.interruptHandler = fn
}

// MarkListenStartPending marks that we should trigger a listen start after ASR has a result.
func (a *ASRManager) MarkListenStartPending() bool {
	return !a.pendingListenStart.Swap(true)
}

// ConsumeListenStartPending returns true once for a pending listen start request.
func (a *ASRManager) ConsumeListenStartPending() bool {
	return a.pendingListenStart.Swap(false)
}

// ProcessVadAudio 启动VAD音频处理
func (a *ASRManager) ProcessVadAudio(ctx context.Context, onClose func()) {
	state := a.clientState
	go func() {
		audioFormat := state.InputAudioFormat
		audioProcesser, err := audio.GetAudioProcesser(audioFormat.SampleRate, audioFormat.Channels, audioFormat.FrameDuration)
		if err != nil {
			log.Errorf("获取解码器失败: %v", err)
			return
		}
		frameSize := state.AsrAudioBuffer.FrameSize()
		if frameSize == 0 {
			frameSize = audioFormat.SampleRate * audioFormat.FrameDuration / 1000
		}

		appCfg := config.GetConfig()
		vadCfg := chatvad.BuildVADConfig(state, appCfg, frameSize)
		vadSvc := chatvad.NewVADService(state, &state.Vad, state.AsrAudioBuffer, vadCfg, audioProcesser)
		var voiceDurationMs int64
		lastLogAt := time.Now()
		var lastConsumedFrames uint64
		var lastConsumedBytes uint64

		for {
			select {
			case audioFrame, ok := <-state.OpusAudioBuffer:
				//log.Debugf("processAsrAudio 收到音频数据, len: %d", len(audioFrame))
				if !ok {
					log.Debugf("processAsrAudio 音频通道已关闭")
					return
				}
				if len(audioFrame) > 0 {
					state.AudioStats.RecordConsume(len(audioFrame))
				}

				clientHadVoice := state.GetClientHaveVoice()
				var pcmData []float32
				var rawHaveVoice bool
				var confirmedHaveVoice bool
				var skipped bool
				var err error
				if audioFormat.Format == audio.FormatPCM16LE {
					pcmData, rawHaveVoice, confirmedHaveVoice, skipped, err = vadSvc.EvaluatePCM(audioFrame)
				} else {
					pcmData, rawHaveVoice, confirmedHaveVoice, skipped, err = vadSvc.Evaluate(audioFrame)
				}
				if err != nil {
					log.Errorf("VAD 处理失败: %v", err)
				}
				if pcmData == nil {
					continue
				}

				haveVoice := confirmedHaveVoice
				if skipped {
					haveVoice = true
				}

				if haveVoice {
					if confirmedHaveVoice && !clientHadVoice && state.ListenMode == "realtime" {
						status := state.GetStatus()
						if status == client.ClientStatusTTSStart || status == client.ClientStatusLLMStart ||
							status == client.ClientStatusListenStop || status == client.ClientStatusInit {
							if a.MarkListenStartPending() {
								log.Infof("realtime VAD 标记 listen start: device=%s status=%s mode=%s", state.DeviceID, status, state.ListenMode)
							}
						}
					}
					//log.Infof("检测到语音, len: %d", len(pcmData))
					state.SetClientHaveVoice(true)
					state.SetClientHaveVoiceLastTime(time.Now().UnixMilli())
					state.Vad.ResetIdleDuration()
					voiceDurationMs += int64(audioFormat.FrameDuration)
				} else {
					state.Vad.AddIdleDuration(int64(audioFormat.FrameDuration))
					idleDuration := state.Vad.GetIdleDuration()
					//log.Infof("空闲时间: %dms", idleDuration)
					if idleDuration > state.GetMaxIdleDuration() {
						log.Infof("超出空闲时长: %dms, 断开连接", idleDuration)
						//断开连接
						onClose()
						return
					}
					if !clientHadVoice {
						continue
					}
					voiceDurationMs = 0
				}

				shouldSend := clientHadVoice
				if !clientHadVoice && (rawHaveVoice || skipped) {
					shouldSend = true
				}

				if shouldSend {
					state.Asr.AddAudioData(pcmData)
				}

				now := time.Now()
				if now.Sub(lastLogAt) >= audioStatsLogInterval {
					stats := state.AudioStats.Snapshot()
					elapsed := now.Sub(lastLogAt).Seconds()
					consumeRate := 0.0
					if elapsed > 0 {
						consumeRate = float64(stats.ConsumedBytes-lastConsumedBytes) / elapsed
					}
					consumedFramesDelta := stats.ConsumedFrames - lastConsumedFrames
					queueLen := 0
					queueCap := 0
					if state.OpusAudioBuffer != nil {
						queueLen = len(state.OpusAudioBuffer)
						queueCap = cap(state.OpusAudioBuffer)
					}
					log.Infof("音频消费统计: device=%s session=%s mode=%s queue=%d/%d recv_frames=%d recv_bytes=%d dropped_frames=%d dropped_bytes=%d consumed_frames=%d consumed_bytes=%d consumed_frames_delta=%d consume_rate=%.1fB/s",
						state.DeviceID,
						state.SessionID,
						state.ListenMode,
						queueLen,
						queueCap,
						stats.RecvFrames,
						stats.RecvBytes,
						stats.DroppedFrames,
						stats.DroppedBytes,
						stats.ConsumedFrames,
						stats.ConsumedBytes,
						consumedFramesDelta,
						consumeRate,
					)
					lastLogAt = now
					lastConsumedFrames = stats.ConsumedFrames
					lastConsumedBytes = stats.ConsumedBytes
				}

				if state.ListenMode == "realtime" && appCfg.Chat.RealtimeMode == 1 && appCfg.Chat.RealtimeInterruptEnabled {
					minVoiceMs := appCfg.Chat.RealtimeMinVoiceMs
					if minVoiceMs <= 0 {
						minVoiceMs = 120
					}
					status := state.GetStatus()
					if (status == client.ClientStatusLLMStart || status == client.ClientStatusTTSStart) && voiceDurationMs >= int64(minVoiceMs) {
						if a.interruptHandler != nil {
							log.Infof("realtime VAD 打断触发, device=%s voice_ms=%d threshold=%d status=%s", state.DeviceID, voiceDurationMs, minVoiceMs, status)
							a.interruptHandler("vad-interrupt")
						}
						voiceDurationMs = 0
					}
				}

				//已经有语音了, 但本次没有检测到语音, 则需要判断是否已经停止说话
				lastHaveVoiceTime := state.GetClientHaveVoiceLastTime()
				if state.GetClientHaveVoice() && lastHaveVoiceTime > 0 && !haveVoice {
					idleDuration := state.Vad.GetIdleDuration()
					if state.IsSilence(idleDuration) { //从有声音到 静默的判断
						log.Infof("停止说话, idleDuration: %dms (threshold=%dms)", idleDuration, state.VoiceStatus.SilenceThresholdTime)
						state.OnVoiceSilence()
						continue
					}
				}

			case <-ctx.Done():
				return
			}
		}
	}()
}

// RestartAsrRecognition 重启ASR识别
func (a *ASRManager) RestartAsrRecognition(ctx context.Context) error {
	state := a.clientState
	if state == nil {
		return nil
	}
	if state.ListenMode == "realtime" && state.IsAsrFinalizing() {
		log.Debugf("realtime 收尾中，跳过ASR重启")
		return nil
	}
	if !a.restartInFlight.CompareAndSwap(false, true) {
		return nil
	}
	defer a.restartInFlight.Store(false)
	if last := a.lastRestartAtMs.Load(); last > 0 {
		if delta := time.Since(time.UnixMilli(last)); delta < minASRRestartGap {
			wait := minASRRestartGap - delta
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
	}
	a.lastRestartAtMs.Store(time.Now().UnixMilli())

	log.Debugf("重启ASR识别开始")
	if m := chatmetrics.GetConversationMetrics(ctx); m != nil {
		m.IncAsrRestartCount()
	}

	// 取消当前ASR上下文
	if state.Asr.Cancel != nil {
		state.Asr.Cancel()
	}

	state.VoiceStatus.Reset()
	state.AsrAudioBuffer.ClearAsrAudioData()

	// 等待一小段时间让资源清理
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// 重新创建ASR上下文
	state.Asr.Ctx, state.Asr.Cancel = context.WithCancel(ctx)
	if state.Asr.AsrAudioChannel == nil {
		ringSize := state.Asr.RingBufferFrames
		if ringSize <= 0 {
			ringSize = client.DefaultRingBufferFrames
		}
		state.Asr.AsrAudioChannel = client.NewRingAudioChannel(ringSize)
	}
	providerAudioChannel := make(chan []float32, asrProviderChannelSize)
	a.bridgeRingToChannel(state.Asr.Ctx, state.Asr.AsrAudioChannel, providerAudioChannel)

	// 重新启动流式识别
	asrResultChannel, err := state.AsrProvider.StreamingRecognize(state.Asr.Ctx, providerAudioChannel)
	if err != nil {
		log.Errorf("重启ASR流式识别失败: %v", err)
		if state.Asr.Cancel != nil {
			state.Asr.Cancel()
		}
		return fmt.Errorf("重启ASR流式识别失败: %v", err)
	}

	state.AsrResultChannel = asrResultChannel
	log.Debugf("重启ASR识别成功")
	return nil
}

func (a *ASRManager) bridgeRingToChannel(
	ctx context.Context,
	ring *client.RingAudioChannel,
	out chan<- []float32,
) {
	if ring == nil {
		close(out)
		return
	}
	go func() {
		defer close(out)
		for {
			frame, ok := ring.ReadContext(ctx)
			if !ok {
				return
			}
			select {
			case <-ctx.Done():
				return
			case out <- frame:
			}
		}
	}()
}
