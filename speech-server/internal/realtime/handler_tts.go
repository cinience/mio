package realtime

import (
	"encoding/base64"
	"fmt"
	"strings"

	openairt "github.com/WqyJh/go-openai-realtime/v2"
	"github.com/google/uuid"

	"speech-server/internal/logger"
)

func (h *connectionHandler) handleResponseCreate(evt openairt.ResponseCreateEvent) error {
	if h.server.tts == nil {
		return fmt.Errorf("response.create is unavailable: TTS not configured")
	}

	text := strings.TrimSpace(extractResponseText(evt.Response))
	if text == "" {
		return fmt.Errorf("response.create requires non-empty input text or instructions")
	}

	ttsInput := strings.ReplaceAll(text, "\"", " ")
	if h.server.normalizer != nil {
		normalized := h.server.normalizer.Normalize(ttsInput)
		if normalized != ttsInput {
			logger.Debugf("realtime: session %s normalized TTS text: %q -> %q", h.session.id, ttsInput, normalized)
		}
		ttsInput = normalized
	}

	if evt.Response.Audio != nil && evt.Response.Audio.Output != nil {
		if evt.Response.Audio.Output.Voice != "" {
			h.session.outputVoice = normalizeVoice(evt.Response.Audio.Output.Voice)
		}
		if evt.Response.Audio.Output.Format != nil &&
			evt.Response.Audio.Output.Format.PCM != nil &&
			evt.Response.Audio.Output.Format.PCM.Rate > 0 {
			if h.session.mode == sessionModeRealtime &&
				h.session.sessionCfg.Realtime != nil &&
				h.session.sessionCfg.Realtime.Audio != nil {
				desired := &openairt.SessionAudioOutput{
					Voice:  h.session.outputVoice,
					Speed:  h.session.outputSpeed,
					Format: copyAudioFormat(evt.Response.Audio.Output.Format),
				}
				h.session.applyOutputConfig(&h.session.sessionCfg.Realtime.Audio.Output, desired)
			}
		}
	}

	speakerID := 0
	if h.server.voiceMap != nil {
		if id, ok := h.server.voiceMap[normalizeVoice(h.session.outputVoice)]; ok {
			speakerID = id
		}
	}

	speed := h.session.outputSpeed
	if speed <= 0 {
		speed = 1.0
	}

	samples, sampleRate, err := h.server.tts.Generate(ttsInput, speakerID, speed)
	if err != nil {
		return fmt.Errorf("tts generation failed: %w", err)
	}
	if len(samples) == 0 {
		return fmt.Errorf("tts generated empty audio")
	}

	targetRate := h.session.outputRate
	if targetRate <= 0 {
		targetRate = sampleRate
	}
	alignedSamples, outputRate := alignTTSSampleRate(samples, sampleRate, targetRate)
	h.session.outputRate = outputRate

	if h.session.mode == sessionModeRealtime && h.session.sessionCfg.Realtime != nil && h.session.sessionCfg.Realtime.Audio != nil {
		desired := &openairt.SessionAudioOutput{
			Voice: normalizeVoice(h.session.outputVoice),
			Speed: speed,
			Format: &openairt.AudioFormatUnion{
				PCM: &openairt.AudioFormatPCM{Rate: outputRate},
			},
		}
		h.session.applyOutputConfig(&h.session.sessionCfg.Realtime.Audio.Output, desired)
	} else {
		h.session.outputVoice = normalizeVoice(h.session.outputVoice)
	}

	audioPCM := float32ToPCM16(alignedSamples)
	audioB64 := base64.StdEncoding.EncodeToString(audioPCM)

	responseID := uuid.NewString()
	itemID := uuid.NewString()

	assistantInProgress := openairt.MessageItemUnion{
		Assistant: &openairt.MessageItemAssistant{
			ID:     itemID,
			Status: openairt.ItemStatusInProgress,
		},
	}

	responseCreated := openairt.ResponseCreatedEvent{
		ServerEventBase: openairt.ServerEventBase{
			Type:    openairt.ServerEventTypeResponseCreated,
			EventID: uuid.NewString(),
		},
		Response: openairt.Response{
			ID:               responseID,
			Status:           openairt.ResponseStatusInProgress,
			Output:           []openairt.MessageItemUnion{assistantInProgress},
			OutputModalities: []openairt.Modality{openairt.ModalityAudio},
		},
	}
	if err := h.sendEvent(responseCreated); err != nil {
		return err
	}

	if err := h.sendEvent(openairt.ResponseOutputItemAddedEvent{
		ServerEventBase: openairt.ServerEventBase{
			Type:    openairt.ServerEventTypeResponseOutputItemAdded,
			EventID: uuid.NewString(),
		},
		ResponseID:  responseID,
		OutputIndex: 0,
		Item:        assistantInProgress,
	}); err != nil {
		return err
	}

	if err := h.sendEvent(openairt.ConversationItemAddedEvent{
		ServerEventBase: openairt.ServerEventBase{
			Type:    openairt.ServerEventTypeConversationItemAdded,
			EventID: uuid.NewString(),
		},
		Item: assistantInProgress,
	}); err != nil {
		return err
	}

	audioPart := openairt.MessageContentOutput{
		Type: openairt.MessageContentTypeOutputAudio,
	}
	if err := h.sendEvent(openairt.ResponseContentPartAddedEvent{
		ServerEventBase: openairt.ServerEventBase{
			Type:    openairt.ServerEventTypeResponseContentPartAdded,
			EventID: uuid.NewString(),
		},
		ResponseID:   responseID,
		ItemID:       itemID,
		OutputIndex:  0,
		ContentIndex: 0,
		Part:         audioPart,
	}); err != nil {
		return err
	}

	textPart := openairt.MessageContentOutput{
		Type: openairt.MessageContentTypeOutputText,
	}
	if err := h.sendEvent(openairt.ResponseContentPartAddedEvent{
		ServerEventBase: openairt.ServerEventBase{
			Type:    openairt.ServerEventTypeResponseContentPartAdded,
			EventID: uuid.NewString(),
		},
		ResponseID:   responseID,
		ItemID:       itemID,
		OutputIndex:  0,
		ContentIndex: 1,
		Part:         textPart,
	}); err != nil {
		return err
	}

	if err := h.sendEvent(openairt.ResponseOutputAudioDeltaEvent{
		ServerEventBase: openairt.ServerEventBase{
			Type:    openairt.ServerEventTypeResponseOutputAudioDelta,
			EventID: uuid.NewString(),
		},
		ResponseID:   responseID,
		ItemID:       itemID,
		OutputIndex:  0,
		ContentIndex: 0,
		Delta:        audioB64,
	}); err != nil {
		return err
	}

	if err := h.sendEvent(openairt.ResponseOutputAudioTranscriptDeltaEvent{
		ServerEventBase: openairt.ServerEventBase{
			Type:    openairt.ServerEventTypeResponseOutputAudioTranscriptDelta,
			EventID: uuid.NewString(),
		},
		ResponseID:   responseID,
		ItemID:       itemID,
		OutputIndex:  0,
		ContentIndex: 0,
		Delta:        text,
	}); err != nil {
		return err
	}

	if err := h.sendEvent(openairt.ResponseOutputTextDeltaEvent{
		ServerEventBase: openairt.ServerEventBase{
			Type:    openairt.ServerEventTypeResponseOutputTextDelta,
			EventID: uuid.NewString(),
		},
		ResponseID:   responseID,
		ItemID:       itemID,
		OutputIndex:  0,
		ContentIndex: 1,
		Delta:        text,
	}); err != nil {
		return err
	}

	if err := h.sendEvent(openairt.ResponseOutputAudioTranscriptDoneEvent{
		ServerEventBase: openairt.ServerEventBase{
			Type:    openairt.ServerEventTypeResponseOutputAudioTranscriptDone,
			EventID: uuid.NewString(),
		},
		ResponseID:   responseID,
		ItemID:       itemID,
		OutputIndex:  0,
		ContentIndex: 0,
		Transcript:   text,
	}); err != nil {
		return err
	}

	if err := h.sendEvent(openairt.ResponseOutputAudioDoneEvent{
		ServerEventBase: openairt.ServerEventBase{
			Type:    openairt.ServerEventTypeResponseOutputAudioDone,
			EventID: uuid.NewString(),
		},
		ResponseID:   responseID,
		ItemID:       itemID,
		OutputIndex:  0,
		ContentIndex: 0,
	}); err != nil {
		return err
	}

	if err := h.sendEvent(openairt.ResponseContentPartDoneEvent{
		ServerEventBase: openairt.ServerEventBase{
			Type:    openairt.ServerEventTypeResponseContentPartDone,
			EventID: uuid.NewString(),
		},
		ResponseID:   responseID,
		ItemID:       itemID,
		OutputIndex:  0,
		ContentIndex: 0,
		Part: openairt.MessageContentOutput{
			Type:       openairt.MessageContentTypeOutputAudio,
			Audio:      audioB64,
			Transcript: text,
		},
	}); err != nil {
		return err
	}

	if err := h.sendEvent(openairt.ResponseOutputTextDoneEvent{
		ServerEventBase: openairt.ServerEventBase{
			Type:    openairt.ServerEventTypeResponseOutputTextDone,
			EventID: uuid.NewString(),
		},
		ResponseID:   responseID,
		ItemID:       itemID,
		OutputIndex:  0,
		ContentIndex: 1,
		Text:         text,
	}); err != nil {
		return err
	}

	if err := h.sendEvent(openairt.ResponseContentPartDoneEvent{
		ServerEventBase: openairt.ServerEventBase{
			Type:    openairt.ServerEventTypeResponseContentPartDone,
			EventID: uuid.NewString(),
		},
		ResponseID:   responseID,
		ItemID:       itemID,
		OutputIndex:  0,
		ContentIndex: 1,
		Part: openairt.MessageContentOutput{
			Type: openairt.MessageContentTypeOutputText,
			Text: text,
		},
	}); err != nil {
		return err
	}

	assistantCompleted := openairt.MessageItemUnion{
		Assistant: &openairt.MessageItemAssistant{
			ID:     itemID,
			Status: openairt.ItemStatusCompleted,
			Content: []openairt.MessageContentOutput{
				{
					Type:       openairt.MessageContentTypeOutputAudio,
					Audio:      audioB64,
					Transcript: text,
				},
				{
					Type: openairt.MessageContentTypeOutputText,
					Text: text,
				},
			},
		},
	}

	if err := h.sendEvent(openairt.ResponseOutputItemDoneEvent{
		ServerEventBase: openairt.ServerEventBase{
			Type:    openairt.ServerEventTypeResponseOutputItemDone,
			EventID: uuid.NewString(),
		},
		ResponseID:  responseID,
		OutputIndex: 0,
		Item:        assistantCompleted,
	}); err != nil {
		return err
	}

	if err := h.sendEvent(openairt.ResponseDoneEvent{
		ServerEventBase: openairt.ServerEventBase{
			Type:    openairt.ServerEventTypeResponseDone,
			EventID: uuid.NewString(),
		},
		Response: openairt.Response{
			ID:               responseID,
			Status:           openairt.ResponseStatusCompleted,
			Output:           []openairt.MessageItemUnion{assistantCompleted},
			OutputModalities: []openairt.Modality{openairt.ModalityAudio},
		},
	}); err != nil {
		return err
	}

	if err := h.sendEvent(openairt.ConversationItemDoneEvent{
		ServerEventBase: openairt.ServerEventBase{
			Type:    openairt.ServerEventTypeConversationItemDone,
			EventID: uuid.NewString(),
		},
		Item: assistantCompleted,
	}); err != nil {
		return err
	}

	logger.Infof("realtime: session %s generated response voice=%s speaker=%d rate=%dHz text=%q",
		h.session.id, h.session.outputVoice, speakerID, outputRate, text)
	return nil
}
