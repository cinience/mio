package realtime

import (
	"context"
	"encoding/base64"
	"fmt"
	"math"
	"strings"
	"time"

	openairt "github.com/WqyJh/go-openai-realtime/v2"

	"speech-server/internal/asr"
	"speech-server/internal/logger"
)

func (h *connectionHandler) configureVAD() error {
	if h.server.vadFactory == nil || !h.server.vadEnabled {
		h.disableVAD()
		return nil
	}

	serverVad := h.session.currentServerVad()
	if serverVad == nil {
		h.disableVAD()
		return nil
	}

	modelName := strings.TrimSpace(h.session.vadModel)
	if modelName == "" {
		modelName = strings.TrimSpace(h.server.vadProvider)
	}
	if modelName == "" {
		names := h.server.vadFactory.Names()
		if len(names) == 0 {
			h.disableVAD()
			return nil
		}
		modelName = names[0]
	}
	if strings.TrimSpace(h.session.vadModel) != modelName {
		h.session.vadModel = modelName
	}

	var overrides asr.VADOverrides
	if serverVad.Threshold > 0 {
		value := float32(serverVad.Threshold)
		overrides.Threshold = &value
	}
	if serverVad.SilenceDurationMs > 0 {
		seconds := float32(float64(serverVad.SilenceDurationMs) / 1000.0)
		overrides.MinSilenceSeconds = &seconds
	}

	current := h.session.vad
	needNew := current == nil || !current.enabled || current.model != modelName

	if !needNew {
		if overrides.Threshold != nil && math.Abs(float64(*overrides.Threshold-current.threshold)) > 0.0001 {
			needNew = true
		}
		if overrides.MinSilenceSeconds != nil && math.Abs(float64(*overrides.MinSilenceSeconds-current.silenceSeconds)) > 0.0001 {
			needNew = true
		}
	}

	if needNew {
		if current != nil {
			current.close()
		}
		var (
			lease    *asr.VADLease
			detector asr.VADDetector
			cfg      asr.VADModel
			err      error
		)
		if h.server.vadPool != nil {
			ctx := h.ctx
			if ctx == nil {
				ctx = context.Background()
			}
			lease, cfg, err = h.server.vadPool.Acquire(ctx, overrides)
			if err != nil {
				return fmt.Errorf("acquire vad detector: %w", err)
			}
			detector = lease.Detector()
		} else {
			detector, cfg, err = h.server.vadFactory.Create(modelName, overrides)
			if err != nil {
				return fmt.Errorf("create vad detector: %w", err)
			}
		}
		h.session.vad = &vadSessionState{
			enabled:        true,
			model:          modelName,
			detector:       detector,
			lease:          lease,
			sampleRate:     cfg.SampleRate,
			threshold:      cfg.Threshold,
			silenceSeconds: cfg.MinSilenceSeconds,
		}
		logger.Infof("realtime: session %s VAD initialised with model=%s sample_rate=%dHz threshold=%.2f silence=%.2fs",
			h.session.id, modelName, cfg.SampleRate, cfg.Threshold, cfg.MinSilenceSeconds)
	} else {
		if overrides.Threshold != nil {
			h.session.vad.threshold = *overrides.Threshold
		}
		if overrides.MinSilenceSeconds != nil {
			h.session.vad.silenceSeconds = *overrides.MinSilenceSeconds
		}
		h.session.vad.sampleRate = h.session.vad.detector.SampleRate()
		logger.Infof("realtime: session %s VAD parameters updated model=%s threshold=%.2f silence=%.2fs",
			h.session.id, h.session.vad.model, h.session.vad.threshold, h.session.vad.silenceSeconds)
	}

	state := h.session.vad
	state.enabled = true
	state.createResponse = serverVad.CreateResponse
	state.interruptResponse = serverVad.InterruptResponse
	state.setPrefixPadding(time.Duration(serverVad.PrefixPaddingMs) * time.Millisecond)

	return nil
}

func (h *connectionHandler) disableVAD() {
	if h.session.vad != nil {
		h.session.vad.close()
		h.session.vad = nil
	}
}

func (h *connectionHandler) handleAudioAppend(evt openairt.InputAudioBufferAppendEvent) error {
	raw, err := base64.StdEncoding.DecodeString(evt.Audio)
	if err != nil {
		return fmt.Errorf("invalid audio base64: %w", err)
	}

	var total int
	h.session.bufferMu.Lock()
	h.session.audioBuffer = append(h.session.audioBuffer, raw...)
	total = len(h.session.audioBuffer)
	h.session.bufferMu.Unlock()
	rate := h.session.clientRate
	if rate <= 0 {
		rate = h.server.defaultClientRate
	}
	duration := float64(total/2) / float64(rate)
	logger.Infof("realtime: session %s buffered chunk=%dB total=%dB (~%.2fs)", h.session.id, len(raw), total, duration)
	return h.processVADChunk(raw)
}

func (h *connectionHandler) handleCommit() error {
	data := h.session.takeAudioBuffer()
	if len(data) == 0 {
		logger.Warnf("realtime: session %s commit requested with empty buffer", h.session.id)
		return h.sendError("", fmt.Errorf("audio buffer empty"))
	}
	rate := h.session.clientRate
	if rate <= 0 {
		rate = h.server.defaultClientRate
	}
	if h.session.vad != nil {
		h.session.vad.resetHistory()
	}
	return h.processAudioSegment(data, rate, "manual")
}

func (h *connectionHandler) processVADChunk(raw []byte) error {
	state := h.session.vad
	if state == nil || !state.enabled || state.detector == nil {
		return nil
	}
	if len(raw) == 0 {
		return nil
	}
	clientRate := h.session.clientRate
	if clientRate <= 0 {
		clientRate = h.server.defaultClientRate
	}
	sourceRate := state.detector.SampleRate()
	if sourceRate <= 0 {
		sourceRate = state.sampleRate
	}
	if sourceRate <= 0 {
		sourceRate = defaultVADSampleRate
	}
	floatSamples := pcm16ToFloat32(raw)
	resampled := resampleLinear(floatSamples, clientRate, sourceRate)
	segments, err := state.detector.Feed(resampled)
	if err != nil {
		return fmt.Errorf("vad feed failed: %w", err)
	}
	segmentDetected := false
	for _, segment := range segments {
		combined := state.combineWithPrefix(segment.Samples)
		if err := h.emitAutoCommittedAudio(combined, sourceRate); err != nil {
			return err
		}
		segmentDetected = true
	}
	if segmentDetected {
		state.resetHistory()
	}
	if !segmentDetected {
		state.pushHistory(resampled)
	}
	return nil
}

func (h *connectionHandler) emitAutoCommittedAudio(samples []float32, sourceRate int) error {
	if len(samples) == 0 {
		return nil
	}
	clientRate := h.session.clientRate
	if clientRate <= 0 {
		clientRate = h.server.defaultClientRate
	}
	pcm := resampleLinear(samples, sourceRate, clientRate)
	raw := float32ToPCM16(pcm)
	if err := h.processAudioSegment(raw, clientRate, "vad"); err != nil {
		return err
	}
	h.session.clearAudioBuffer()
	return nil
}
