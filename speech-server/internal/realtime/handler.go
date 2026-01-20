package realtime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	openairt "github.com/WqyJh/go-openai-realtime/v2"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"speech-server/internal/logger"
)

type connectionHandler struct {
	conn    *websocket.Conn
	ctx     context.Context
	cancel  context.CancelFunc
	server  *Server
	session *sessionState
	remote  string
}

const (
	maxRealtimeMessageSize = 10 * 1024 * 1024 // 10MB
	realtimeReadTimeout    = 15 * time.Second
)

func (h *connectionHandler) run() {
	defer func() {
		if err := h.conn.Close(); err != nil {
			logger.Debugf("realtime: session %s close error: %v", h.session.id, err)
		}
	}()
	defer logger.Infof("realtime: session %s closed (remote=%s)", h.session.id, h.remote)
	defer h.session.close()

	logger.Infof("realtime: session %s connected (remote=%s)", h.session.id, h.remote)

	if err := h.sendSessionCreated(); err != nil {
		logger.Errorf("realtime: send session created error: %v", err)
		return
	}

	h.conn.SetReadLimit(maxRealtimeMessageSize)
	if err := h.conn.SetReadDeadline(time.Now().Add(realtimeReadTimeout)); err != nil {
		logger.Debugf("realtime: session %s set read deadline error: %v", h.session.id, err)
	}
	h.conn.SetPongHandler(func(string) error {
		if err := h.conn.SetReadDeadline(time.Now().Add(realtimeReadTimeout)); err != nil {
			logger.Debugf("realtime: session %s pong deadline error: %v", h.session.id, err)
			return err
		}
		return nil
	})

	for {
		select {
		case <-h.ctx.Done():
			return
		default:
		}

		_, data, err := h.conn.ReadMessage()
		if err != nil {
			return
		}

		if err := h.handleMessage(data); err != nil {
			logger.Warnf("realtime: session=%s remote=%s handle message error: %v", h.session.id, h.remote, err)
			if sendErr := h.sendError("", err); sendErr != nil {
				logger.Errorf("realtime: session=%s remote=%s failed to send error: %v", h.session.id, h.remote, sendErr)
			}
		}
	}
}

func (h *connectionHandler) handleMessage(data []byte) error {
	var envelope struct {
		Type openairt.ClientEventType `json:"type"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return fmt.Errorf("invalid event: %w", err)
	}

	switch envelope.Type {
	case openairt.ClientEventTypeSessionUpdate:
		if model := extractSessionVADModel(data); model != "" {
			if h.session.setVADModel(model) {
				logger.Infof("realtime: session %s requested VAD model override: %s", h.session.id, model)
			}
		}
		var evt openairt.SessionUpdateEvent
		if err := json.Unmarshal(data, &evt); err != nil {
			return fmt.Errorf("decode session.update: %w", err)
		}
		meta := extractSessionMetadata(data)
		return h.handleSessionUpdate(evt, meta)
	case openairt.ClientEventTypeInputAudioBufferAppend:
		var evt openairt.InputAudioBufferAppendEvent
		if err := json.Unmarshal(data, &evt); err != nil {
			return fmt.Errorf("decode input_audio_buffer.append: %w", err)
		}
		return h.handleAudioAppend(evt)
	case openairt.ClientEventTypeInputAudioBufferCommit:
		return h.handleCommit()
	case openairt.ClientEventTypeInputAudioBufferClear:
		return h.handleClear()
	case openairt.ClientEventTypeResponseCreate:
		var evt openairt.ResponseCreateEvent
		if err := json.Unmarshal(data, &evt); err != nil {
			return fmt.Errorf("decode response.create: %w", err)
		}
		return h.handleResponseCreate(evt)
	default:
		return fmt.Errorf("unsupported event type: %s", envelope.Type)
	}
}

func (h *connectionHandler) sendSessionCreated() error {
	event := openairt.SessionCreatedEvent{
		ServerEventBase: openairt.ServerEventBase{
			Type:    openairt.ServerEventTypeSessionCreated,
			EventID: uuid.NewString(),
		},
		Session: h.session.sessionCfg,
	}
	return h.sendEvent(event)
}

func (h *connectionHandler) handleSessionUpdate(evt openairt.SessionUpdateEvent, meta sessionMetadata) error {
	h.session.applyUpdate(evt.Session)
	h.session.applyMetadata(meta)

	if h.session.clientRate <= 0 {
		h.session.clientRate = h.server.defaultClientRate
	}

	if err := h.configureVAD(); err != nil {
		return err
	}

	vadModel := strings.TrimSpace(h.session.vadModel)
	lang := ""
	switch h.session.mode {
	case sessionModeRealtime:
		if h.session.sessionCfg.Realtime != nil &&
			h.session.sessionCfg.Realtime.Audio != nil &&
			h.session.sessionCfg.Realtime.Audio.Input != nil &&
			h.session.sessionCfg.Realtime.Audio.Input.Transcription != nil {
			lang = h.session.sessionCfg.Realtime.Audio.Input.Transcription.Language
		}
		logger.Infof("realtime: session %s session.update applied: client_rate=%dHz output_rate=%dHz vad_model=%s voice=%s speed=%.2f language=%s",
			h.session.id, h.session.clientRate, h.session.outputRate, vadModel, h.session.outputVoice, h.session.outputSpeed, lang)
	default:
		if h.session.sessionCfg.Transcription != nil &&
			h.session.sessionCfg.Transcription.Audio != nil &&
			h.session.sessionCfg.Transcription.Audio.Input != nil &&
			h.session.sessionCfg.Transcription.Audio.Input.Transcription != nil {
			lang = h.session.sessionCfg.Transcription.Audio.Input.Transcription.Language
		}
		logger.Infof("realtime: session %s session.update applied: client_rate=%dHz vad_model=%s language=%s",
			h.session.id, h.session.clientRate, vadModel, lang)
	}

	resp := openairt.SessionUpdatedEvent{
		ServerEventBase: openairt.ServerEventBase{
			Type:    openairt.ServerEventTypeSessionUpdated,
			EventID: uuid.NewString(),
		},
		Session: h.session.sessionCfg,
	}
	return h.sendEvent(resp)
}
