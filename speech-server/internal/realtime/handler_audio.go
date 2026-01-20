package realtime

import (
	"encoding/base64"
	"fmt"
	"strings"
	"time"
	"unicode"

	openairt "github.com/WqyJh/go-openai-realtime/v2"
	"github.com/google/uuid"

	"speech-server/internal/asr"
	"speech-server/internal/logger"
	"speech-server/internal/speaker"
)

func (h *connectionHandler) processAudioSegment(raw []byte, sampleRate int, origin string) error {
	if len(raw) == 0 {
		return nil
	}
	if sampleRate <= 0 {
		sampleRate = h.server.defaultClientRate
	}
	pcm := pcm16ToFloat32(raw)
	metrics := analyzeAudioQuality(pcm, sampleRate, h.server.audioEnergyThreshold, h.server.audioMinDuration)

	skipNoise, energyThreshold, minDuration := h.session.noiseFilterConfig(h.server.audioEnergyThreshold, h.server.audioMinDuration)
	if !skipNoise {
		if metrics.IsLikelyNoise {
			if origin == "manual" {
				logger.Warnf("realtime: session %s audio low energy manual commit suppressed (%s): duration=%.2fs rms=%.4f peak=%.4f (threshold=%.4f, min_duration=%.2fs) - dropping",
					h.session.id, origin, metrics.DurationSec, metrics.RMS, metrics.Peak, energyThreshold, minDuration)
			} else {
				logger.Infof("realtime: session %s audio filtered out (%s): duration=%.2fs rms=%.4f peak=%.4f (threshold=%.4f, min_duration=%.2fs) - likely noise/silence",
					h.session.id, origin, metrics.DurationSec, metrics.RMS, metrics.Peak, energyThreshold, minDuration)
			}

			return h.emitSilenceSkipped()
		}
		logger.Infof("realtime: session %s audio quality (%s): duration=%.2fs rms=%.4f peak=%.4f - proceeding with recognition",
			h.session.id, origin, metrics.DurationSec, metrics.RMS, metrics.Peak)
	} else {
		logger.Debugf("realtime: session %s noise gate skipped by session metadata (%s)", h.session.id, origin)
	}

	// speaker pipeline (non-blocking of ASR when disabled)
	h.runSpeakerPipeline(pcm, sampleRate, metrics)

	modelRate := h.server.recog.SampleRate()
	resampled := resampleLinear(pcm, sampleRate, modelRate)

	languageHint := h.session.languageHint()
	result, err := h.server.recog.RecognizeWithHint(resampled, languageHint)
	if err != nil {
		return fmt.Errorf("asr failed: %w", err)
	}
	text := result.Text
	if hint := languageHint; result.Language == "" && hint != "" {
		result.Language = hint
	}
	duration := float64(len(raw)/2) / float64(sampleRate)
	logger.Infof("realtime: session %s transcript ready (%s): samples=%d (~%.2fs) text=%q lang=%s emotion=%s event=%s",
		h.session.id, origin, len(raw)/2, duration, text, result.Language, result.Emotion, result.Event)
	if shouldTreatTranscriptAsEmpty(text) {
		logger.Infof("realtime: session %s transcript treated as empty (raw=%q)", h.session.id, text)
		text = ""
	}

	itemID := uuid.NewString()

	committed := openairt.InputAudioBufferCommittedEvent{
		ServerEventBase: openairt.ServerEventBase{
			Type:    openairt.ServerEventTypeInputAudioBufferCommitted,
			EventID: uuid.NewString(),
		},
		ItemID: itemID,
	}
	if err := h.sendEvent(committed); err != nil {
		return err
	}

	audioBase64 := base64.StdEncoding.EncodeToString(raw)
	content := []openairt.MessageContentInput{
		{
			Type:       openairt.MessageContentTypeInputAudio,
			Audio:      audioBase64,
			Transcript: text,
		},
	}
	if meta := buildMetadataText(result); meta != "" {
		content = append(content, openairt.MessageContentInput{
			Type: openairt.MessageContentTypeInputText,
			Text: meta,
		})
	}

	item := openairt.MessageItemUnion{
		User: &openairt.MessageItemUser{
			ID:      itemID,
			Status:  openairt.ItemStatusCompleted,
			Content: content,
		},
	}

	added := openairt.ConversationItemAddedEvent{
		ServerEventBase: openairt.ServerEventBase{
			Type:    openairt.ServerEventTypeConversationItemAdded,
			EventID: uuid.NewString(),
		},
		Item: item,
	}
	if err := h.sendEvent(added); err != nil {
		return err
	}

	done := openairt.ConversationItemDoneEvent{
		ServerEventBase: openairt.ServerEventBase{
			Type:    openairt.ServerEventTypeConversationItemDone,
			EventID: uuid.NewString(),
		},
		Item: item,
	}
	if err := h.sendEvent(done); err != nil {
		return err
	}

	transcribed := openairt.ConversationItemInputAudioTranscriptionCompletedEvent{
		ServerEventBase: openairt.ServerEventBase{
			Type:    openairt.ServerEventTypeConversationItemInputAudioTranscriptionCompleted,
			EventID: uuid.NewString(),
		},
		ItemID:       itemID,
		ContentIndex: 0,
		Transcript:   text,
	}
	if err := h.sendEvent(transcribed); err != nil {
		return err
	}

	cleared := openairt.InputAudioBufferClearedEvent{
		ServerEventBase: openairt.ServerEventBase{
			Type:    openairt.ServerEventTypeInputAudioBufferCleared,
			EventID: uuid.NewString(),
		},
	}
	return h.sendEvent(cleared)
}

func shouldTreatTranscriptAsEmpty(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return true
	}
	cleaned := strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r) {
			return -1
		}
		return r
	}, trimmed)
	if cleaned == "" {
		return true
	}
	return strings.EqualFold(cleaned, "yeah")
}

func (h *connectionHandler) runSpeakerPipeline(pcm []float32, sampleRate int, metrics AudioQualityMetrics) {
	if h.server.speaker == nil {
		return
	}
	intent := strings.ToLower(strings.TrimSpace(h.session.speakerIntent))
	if intent == "" {
		return
	}
	minDur := h.session.speakerMinDurationOrDefault(h.server.speakerCfg.MinDuration)
	if minDur <= 0 {
		minDur = 0.3
	}
	if metrics.DurationSec < minDur {
		_ = h.emitSpeakerError(intent, fmt.Sprintf("audio too short for speaker (%0.2fs < %0.2fs)", metrics.DurationSec, minDur))
		return
	}
	threshold := h.session.speakerThresholdOrDefault(h.server.speakerCfg.Threshold)
	audioMs := int64(metrics.DurationSec * 1000)

	start := time.Now()
	switch intent {
	case "register":
		if strings.TrimSpace(h.session.speakerID) == "" || strings.TrimSpace(h.session.speakerName) == "" {
			_ = h.emitSpeakerError(intent, "speaker_id and speaker_name are required for registration")
			return
		}
		_, err := h.server.speaker.Register(h.ctx, speaker.RegisterInput{
			SpeakerID:   h.session.speakerID,
			SpeakerName: h.session.speakerName,
			Samples:     pcm,
			SampleRate:  sampleRate,
		})
		if err != nil {
			_ = h.emitSpeakerError(intent, err.Error())
			return
		}
		_ = h.emitSpeakerMatch(intent, true, h.session.speakerID, h.session.speakerName, 1.0, start, audioMs)
	case "identify", "identify_speaker":
		res, err := h.server.speaker.Identify(h.ctx, speaker.IdentifyInput{
			SpeakerID:   h.session.speakerID,
			SpeakerName: h.session.speakerName,
			Samples:     pcm,
			SampleRate:  sampleRate,
			Threshold:   threshold,
			TopK:        1,
		})
		if err != nil {
			_ = h.emitSpeakerError(intent, err.Error())
			return
		}
		if res == nil {
			_ = h.emitSpeakerMatch(intent, false, "", "", 0, start, audioMs)
			return
		}
		_ = h.emitSpeakerMatch(intent, true, res.SpeakerID, res.SpeakerName, res.Confidence, start, audioMs)
	case "verify":
		if strings.TrimSpace(h.session.speakerID) == "" {
			_ = h.emitSpeakerError(intent, "speaker_id is required for verify")
			return
		}
		res, err := h.server.speaker.Verify(h.ctx, speaker.VerifyInput{
			SpeakerID:  h.session.speakerID,
			Samples:    pcm,
			SampleRate: sampleRate,
			Threshold:  threshold,
		})
		if err != nil {
			_ = h.emitSpeakerError(intent, err.Error())
			return
		}
		_ = h.emitSpeakerMatch(intent, res.Verified, res.SpeakerID, res.SpeakerName, res.Confidence, start, audioMs)
	default:
		_ = h.emitSpeakerError(intent, "unsupported speaker intent")
	}
}

func (h *connectionHandler) emitSpeakerMatch(intent string, matched bool, id, name string, confidence float32, start time.Time, audioMs int64) error {
	event := speakerMatchEvent{
		Type:            "speaker.match",
		EventID:         uuid.NewString(),
		Intent:          intent,
		Matched:         matched,
		SpeakerID:       id,
		SpeakerName:     name,
		Confidence:      confidence,
		LatencyMs:       time.Since(start).Milliseconds(),
		AudioDurationMs: audioMs,
	}
	return h.sendEvent(event)
}

func (h *connectionHandler) emitSpeakerError(intent, msg string) error {
	event := speakerErrorEvent{
		Type:    "speaker.error",
		EventID: uuid.NewString(),
		Intent:  intent,
		Error:   msg,
	}
	return h.sendEvent(event)
}

type speakerMatchEvent struct {
	Type            string  `json:"type"`
	EventID         string  `json:"event_id"`
	Intent          string  `json:"intent,omitempty"`
	Matched         bool    `json:"matched"`
	SpeakerID       string  `json:"speaker_id,omitempty"`
	SpeakerName     string  `json:"speaker_name,omitempty"`
	Confidence      float32 `json:"confidence,omitempty"`
	LatencyMs       int64   `json:"latency_ms,omitempty"`
	AudioDurationMs int64   `json:"audio_duration_ms,omitempty"`
}

type speakerErrorEvent struct {
	Type    string `json:"type"`
	EventID string `json:"event_id"`
	Intent  string `json:"intent,omitempty"`
	Error   string `json:"error"`
}

func buildMetadataText(res asr.Result) string {
	var parts []string
	if lang := strings.TrimSpace(res.Language); lang != "" {
		parts = append(parts, "language="+lang)
	}
	if emotion := strings.TrimSpace(res.Emotion); emotion != "" {
		parts = append(parts, "emotion="+emotion)
	}
	if event := strings.TrimSpace(res.Event); event != "" {
		parts = append(parts, "event="+event)
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, ", ")
}

func (h *connectionHandler) handleClear() error {
	h.session.bufferMu.Lock()
	h.session.audioBuffer = h.session.audioBuffer[:0]
	h.session.bufferMu.Unlock()
	logger.Infof("realtime: session %s buffer cleared on request", h.session.id)

	cleared := openairt.InputAudioBufferClearedEvent{
		ServerEventBase: openairt.ServerEventBase{
			Type:    openairt.ServerEventTypeInputAudioBufferCleared,
			EventID: uuid.NewString(),
		},
	}
	return h.sendEvent(cleared)
}

func (h *connectionHandler) emitSilenceSkipped() error {
	cleared := openairt.InputAudioBufferClearedEvent{
		ServerEventBase: openairt.ServerEventBase{
			Type:    openairt.ServerEventTypeInputAudioBufferCleared,
			EventID: uuid.NewString(),
		},
	}
	return h.sendEvent(cleared)
}
