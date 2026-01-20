package vad

import (
	"fmt"
	"math"

	"backend-server/constants"
	"backend-server/internal/domain/audio"
	audiohelper "backend-server/pkg/audio"
	vadinter "backend-server/internal/domain/vad/inter"
	log "backend-server/internal/infrastructure/logger"
	vadregistry "backend-server/internal/registry/vad"
	client "backend-server/internal/server/chat/session/state"
)

// VADService 提供统一的 VAD 调用入口，manager 仅需关心 Evaluate 的返回值即可。
type VADService interface {
	Evaluate(opusFrame []byte) (pcm []float32, raw bool, confirmed bool, skipped bool, err error)
	EvaluatePCM(pcmBytes []byte) (pcm []float32, raw bool, confirmed bool, skipped bool, err error)
}

type vadService struct {
	state          *client.ClientState
	pipeline       *vadPipeline
	audioProcesser *audio.AudioProcesser
	frameSize      int
	cfg            VADConfig
	vadUnavailable bool
	pcmBuffer      []float32

	lastLogRaw       bool
	lastLogConfirmed bool
	lastLogBypassed  bool
	lastLogSkip      bool
	logStateInit     bool
}

func NewVADService(state *client.ClientState, runtime vadRuntime, buffer *client.AsrAudioBuffer, cfg VADConfig, processer *audio.AudioProcesser) VADService {
	pipe := newVADPipeline(runtime, buffer, vadPipelineConfig{
		Provider:        cfg.Provider,
		Config:          cfg.ProviderConfig,
		NoiseFloor:      cfg.NoiseFloor,
		MinActiveFrames: cfg.MinActiveFrames,
		FrameDurationMs: cfg.FrameDurationMs,
		FrameSize:       cfg.FrameSize,
		SampleRate:      cfg.SampleRate,
	})

	return &vadService{
		state:          state,
		pipeline:       pipe,
		audioProcesser: processer,
		frameSize:      cfg.FrameSize,
		cfg:            cfg,
	}
}

func (v *vadService) Evaluate(opusFrame []byte) ([]float32, bool, bool, bool, error) {
	if len(opusFrame) == 0 {
		return nil, false, false, false, nil
	}

	if v.state.GetClientVoiceStop() {
		log.Infof("客户端停止说话, 跳过音频数据")
		return nil, false, false, true, nil
	}

	if v.pcmBuffer == nil || len(v.pcmBuffer) < v.frameSize {
		v.pcmBuffer = make([]float32, v.frameSize)
	}

	n, err := v.audioProcesser.DecoderFloat32(opusFrame, v.pcmBuffer)
	// Make a copy so数据在送入 ASR 时不会被复用覆盖
	if err != nil {
		return nil, false, false, false, fmt.Errorf("解码失败: %w", err)
	}
	if n == 0 {
		return nil, false, false, false, nil
	}

	pcmFrame := make([]float32, n)
	copy(pcmFrame, v.pcmBuffer[:n])

	return v.evaluateDecodedPCM(pcmFrame)
}

func (v *vadService) EvaluatePCM(pcmBytes []byte) ([]float32, bool, bool, bool, error) {
	if len(pcmBytes) == 0 {
		return nil, false, false, false, nil
	}

	if v.state.GetClientVoiceStop() {
		log.Infof("客户端停止说话, 跳过音频数据")
		return nil, false, false, true, nil
	}

	pcmFrame := audiohelper.PCM16BytesToFloat32(pcmBytes)
	if len(pcmFrame) == 0 {
		return nil, false, false, false, nil
	}

	return v.evaluateDecodedPCM(pcmFrame)
}

func (v *vadService) evaluateDecodedPCM(pcmFrame []float32) ([]float32, bool, bool, bool, error) {
	clientHadVoice := v.state.GetClientHaveVoice()
	skipVad := v.state.Asr.AutoEnd || v.state.ListenMode == "manual" || v.vadUnavailable

	if skipVad {
		v.pipeline.ResetActivation()
		return pcmFrame, true, true, true, nil
	}

	evalResult, ready, errEval := v.pipeline.EvaluateFrame(pcmFrame, clientHadVoice)
	if errEval != nil {
		v.vadUnavailable = true
		return pcmFrame, true, true, true, fmt.Errorf("VAD检测失败: %w", errEval)
	}
	if !ready {
		return nil, false, false, false, nil
	}

	if v.pipeline.Disabled() && !v.vadUnavailable {
		v.vadUnavailable = true
	}
	if evalResult.Bypassed {
		clientHadVoice = true
	}

	if evalResult.Confirmed && !clientHadVoice {
		buffered := v.pipeline.DrainBufferedAudio(nil)
		if len(buffered) > 0 {
			if evalResult.ShouldFlush {
				pcmFrame = buffered
			} else {
				pcmFrame = append(buffered, pcmFrame...)
			}
		}
	}

	v.maybeLogVADResult(evalResult, false)

	if !clientHadVoice && !evalResult.Confirmed && !evalResult.Raw {
		return nil, evalResult.Raw, false, false, nil
	}

	return pcmFrame, evalResult.Raw, evalResult.Confirmed, false, nil
}

func (v *vadService) maybeLogVADResult(eval vadEvaluation, skip bool) {
	shouldLog := !v.logStateInit || eval.Confirmed || eval.Bypassed
	if !shouldLog {
		if eval.Raw != v.lastLogRaw || eval.Confirmed != v.lastLogConfirmed || eval.Bypassed != v.lastLogBypassed || skip != v.lastLogSkip {
			shouldLog = true
		}
	}

	if !shouldLog {
		return
	}

	v.lastLogRaw = eval.Raw
	v.lastLogConfirmed = eval.Confirmed
	v.lastLogBypassed = eval.Bypassed
	v.lastLogSkip = skip
	v.logStateInit = true

	log.Debugf("VAD 检测完成: provider=%s raw_result=%v confirmed=%v skip=%v rms=%.6f noise_floor=%.6f active_streak=%d/%d buffer_frames=%d vad_frames=%d frame_size=%d",
		v.cfg.Provider,
		eval.Raw,
		eval.Confirmed,
		skip,
		eval.FrameRMS,
		v.cfg.NoiseFloor,
		eval.ActiveStreak,
		eval.MinActiveFrames,
		eval.BufferFrames,
		eval.WindowFrames,
		v.frameSize,
	)
}

type vadRuntime interface {
	IsInitialized() bool
	Init(provider string, config map[string]interface{}) error
	GetProvider() vadinter.VAD
}

type vadPipelineConfig struct {
	Provider        string
	Config          map[string]interface{}
	NoiseFloor      float64
	MinActiveFrames int
	FrameDurationMs int
	FrameSize       int
	SampleRate      int
}

type vadPipeline struct {
	runtime          vadRuntime
	buffer           *client.AsrAudioBuffer
	strategy         *vadStrategy
	providerConfig   map[string]interface{}
	providerName     string
	frameSize        int
	sampleRate       int
	windowFrameCount int
	scratch          []float32
	disabled         bool
}

type vadEvaluation struct {
	Raw             bool
	Confirmed       bool
	ShouldFlush     bool
	Bypassed        bool
	FrameRMS        float64
	ActiveStreak    int
	MinActiveFrames int
	BufferFrames    int
	WindowFrames    int
}

func newVADPipeline(runtime vadRuntime, buffer *client.AsrAudioBuffer, cfg vadPipelineConfig) *vadPipeline {
	strategy := newVADStrategy(cfg.Provider, cfg.NoiseFloor, cfg.MinActiveFrames)
	windowFrames := 1
	if cfg.FrameDurationMs > 0 {
		switch cfg.Provider {
		case constants.VadTypeSherpaVad, constants.VadTypeSileroVad:
			windowFrames = int(math.Ceil(60.0 / float64(cfg.FrameDurationMs)))
			if windowFrames <= 0 {
				windowFrames = 1
			}
		}
	}

	return &vadPipeline{
		runtime:          runtime,
		buffer:           buffer,
		strategy:         strategy,
		providerConfig:   cfg.Config,
		providerName:     cfg.Provider,
		frameSize:        cfg.FrameSize,
		sampleRate:       cfg.SampleRate,
		windowFrameCount: windowFrames,
	}
}

func (p *vadPipeline) Disabled() bool {
	return p.disabled
}

func (p *vadPipeline) EvaluateFrame(pcm []float32, clientHaveVoice bool) (vadEvaluation, bool, error) {
	result := vadEvaluation{
		MinActiveFrames: p.strategy.minActiveFrames,
		ActiveStreak:    p.strategy.activeStreak,
		WindowFrames:    p.windowFrameCount,
		BufferFrames:    p.buffer.GetFrameCount(),
	}

	if p.disabled {
		result.Bypassed = true
		result.Raw = true
		result.Confirmed = true
		return result, true, nil
	}

	if len(pcm) == 0 {
		return result, false, nil
	}

	p.buffer.AddAsrAudioData(pcm)

	var aggregated []float32
	aggregatedReady := false
	if !p.strategy.caps.Incremental && p.windowFrameCount > 0 {
		if p.buffer.GetAsrDataSize() >= p.windowFrameCount*p.frameSize {
			p.scratch = p.buffer.CopyLatest(p.windowFrameCount, p.scratch)
			aggregated = p.scratch
			aggregatedReady = true
		}
	}

	var ready bool
	var frameRMS float64
	var vadInput []float32
	vadInput, frameRMS, ready = p.strategy.prepareInput(p.buffer, aggregated, aggregatedReady)
	result.FrameRMS = frameRMS
	result.BufferFrames = p.buffer.GetFrameCount()
	result.ActiveStreak = p.strategy.activeStreak

	if !ready {
		return result, false, nil
	}

	if !p.runtime.IsInitialized() {
		if err := p.runtime.Init(p.providerName, p.cloneConfig()); err != nil {
			p.disabled = true
			return result, false, err
		}
	}

	vadProvider := p.runtime.GetProvider()
	if vadProvider == nil {
		result.Bypassed = true
		result.Raw = true
		result.Confirmed = true
		p.strategy.confirmActivation(true, true)
		result.ActiveStreak = p.strategy.activeStreak
		return result, true, nil
	}

	if p.strategy.shouldResetProvider() {
		if err := vadProvider.Reset(); err != nil {
			log.Warnf("重置VAD失败: %v", err)
		}
	}

	// 直接调用 VAD provider，不再在外层做噪声过滤
	// WebRTC VAD 在内部实现了 RMS 噪声过滤，Sherpa VAD 使用深度学习模型自带噪声抑制
	rawHaveVoice, err := vadProvider.IsVADExt(vadInput, p.sampleRate, p.frameSize)
	if err != nil {
		return result, true, err
	}
	result.Raw = rawHaveVoice

	result.ShouldFlush = result.Raw && !clientHaveVoice
	result.Confirmed = p.strategy.confirmActivation(result.Raw, clientHaveVoice)
	result.ActiveStreak = p.strategy.activeStreak
	result.BufferFrames = p.buffer.GetFrameCount()
	return result, true, nil
}

func (p *vadPipeline) DrainBufferedAudio(dst []float32) []float32 {
	if p.buffer == nil {
		return nil
	}
	return p.buffer.Drain(dst)
}

func (p *vadPipeline) ResetActivation() {
	p.strategy.resetStreak()
}

func (p *vadPipeline) cloneConfig() map[string]interface{} {
	if p.providerConfig == nil {
		return nil
	}
	copied := make(map[string]interface{}, len(p.providerConfig))
	for k, v := range p.providerConfig {
		copied[k] = v
	}
	return copied
}

type vadStrategy struct {
	provider        string
	minActiveFrames int
	activeStreak    int
	caps            vadregistry.ProviderCapabilities
	consumedSamples int64
}

func newVADStrategy(provider string, _ float64, minActiveFrames int) *vadStrategy {
	if minActiveFrames < 1 {
		minActiveFrames = 1
	}
	caps := vadregistry.GetCapabilities(provider)
	if caps.RequiresWindow && minActiveFrames < 1 {
		minActiveFrames = 1
	}
	return &vadStrategy{
		provider:        provider,
		minActiveFrames: minActiveFrames,
		caps:            caps,
	}
}

func (s *vadStrategy) confirmActivation(rawHaveVoice bool, clientHaveVoice bool) bool {
	if clientHaveVoice {
		if rawHaveVoice {
			if s.activeStreak < s.minActiveFrames {
				s.activeStreak = s.minActiveFrames
			}
			return true
		}
		s.activeStreak = 0
		return false
	}

	if rawHaveVoice {
		s.activeStreak++
	} else {
		s.activeStreak = 0
	}

	return s.activeStreak >= s.minActiveFrames
}

func (s *vadStrategy) resetStreak() {
	s.activeStreak = 0
}

func (s *vadStrategy) shouldResetProvider() bool {
	if s.caps.ResetEachCall {
		return true
	}
	return s.provider == constants.VadTypeWebRTCVad
}

func (s *vadStrategy) prepareInput(buffer *client.AsrAudioBuffer, aggregated []float32, aggregatedReady bool) ([]float32, float64, bool) {
	if s.caps.Incremental {
		data, newMark := buffer.GetSince(s.consumedSamples)
		s.consumedSamples = newMark
		if len(data) == 0 {
			return nil, 0, false
		}
		return data, calcRMS(data), true
	}

	if aggregatedReady && len(aggregated) > 0 {
		return aggregated, calcRMS(aggregated), true
	}
	return nil, 0, false
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
