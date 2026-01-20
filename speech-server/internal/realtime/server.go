package realtime

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	openairt "github.com/WqyJh/go-openai-realtime/v2"
	"github.com/gorilla/websocket"

	"speech-server/internal/asr"
	"speech-server/internal/logger"
	"speech-server/internal/speaker"
	"speech-server/internal/tts"
)

const defaultClientSampleRate = 16000

// Normalizer represents the subset of functionality required to normalise text
// before it is passed to the TTS engine.
type Normalizer interface {
	Normalize(string) string
}

// Options contains the dependencies required to construct a realtime Server.
type Options struct {
	Recognizer           *asr.Recognizer
	DefaultClientRate    int
	TTS                  *tts.Generator
	VoiceMap             map[openairt.Voice]int
	DefaultVoice         openairt.Voice
	DefaultTTSSpeed      float32
	Normalizer           Normalizer
	VADEnabled           bool
	VADProvider          string
	VADFactory           *asr.VADFactory
	VADPool              *asr.VADPool
	Upgrader             *websocket.Upgrader
	AudioEnergyThreshold float32 // Minimum RMS energy to consider audio as speech (0.001-0.1, default 0.01)
	AudioMinDuration     float32 // Minimum audio duration in seconds (default 0.3)
	Speaker              *speaker.Manager
	SpeakerConfig        speaker.Config
}

// Server exposes the realtime websocket endpoint that powers speech streaming.
type Server struct {
	recog                *asr.Recognizer
	defaultClientRate    int
	tts                  *tts.Generator
	voiceMap             map[openairt.Voice]int
	defaultVoice         openairt.Voice
	defaultTTSSpeed      float32
	normalizer           Normalizer
	vadEnabled           bool
	vadProvider          string
	vadFactory           *asr.VADFactory
	vadPool              *asr.VADPool
	speaker              *speaker.Manager
	speakerCfg           speaker.Config
	upgrader             websocket.Upgrader
	audioEnergyThreshold float32 // Minimum RMS energy threshold for audio filtering
	audioMinDuration     float32 // Minimum audio duration for recognition
}

// NewServer builds a realtime Server from the supplied dependencies.
func NewServer(opts Options) (*Server, error) {
	if opts.Recognizer == nil {
		return nil, fmt.Errorf("realtime: recognizer is required")
	}

	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}
	if opts.Upgrader != nil {
		upgrader = *opts.Upgrader
	}

	voice := normalizeVoice(opts.DefaultVoice)
	if voice == "" {
		voice = normalizeVoice(openairt.VoiceAlloy)
	}

	defaultRate := opts.DefaultClientRate
	if defaultRate <= 0 {
		defaultRate = defaultClientSampleRate
	}

	defaultSpeed := opts.DefaultTTSSpeed
	if defaultSpeed <= 0 {
		defaultSpeed = 1.0
	}

	voiceMap := opts.VoiceMap
	if len(voiceMap) == 0 {
		voiceMap = map[openairt.Voice]int{
			voice: 0,
		}
	}

	s := &Server{
		recog:                opts.Recognizer,
		defaultClientRate:    defaultRate,
		tts:                  opts.TTS,
		voiceMap:             voiceMap,
		defaultVoice:         voice,
		defaultTTSSpeed:      defaultSpeed,
		normalizer:           opts.Normalizer,
		vadEnabled:           opts.VADEnabled && opts.VADFactory != nil,
		vadProvider:          strings.TrimSpace(opts.VADProvider),
		vadFactory:           opts.VADFactory,
		vadPool:              opts.VADPool,
		speaker:              opts.Speaker,
		speakerCfg:           opts.SpeakerConfig,
		upgrader:             upgrader,
		audioEnergyThreshold: opts.AudioEnergyThreshold,
		audioMinDuration:     opts.AudioMinDuration,
	}

	return s, nil
}

// Handle upgrades the incoming HTTP connection to a websocket and begins
// processing realtime events.
func (s *Server) Handle(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.Errorf("realtime: upgrade error: %v", err)
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mode := determineSessionMode(r)
	if mode == sessionModeRealtime && s.tts == nil {
		logger.Warnf("realtime: text-to-voice requested but TTS not configured; falling back to transcription mode")
		mode = sessionModeTranscription
	}

	session := newSessionState(s.defaultClientRate, mode, s.defaultVoice, s.defaultTTSSpeed)
	if s.vadProvider != "" {
		session.setVADModel(s.vadProvider)
	}

	handler := &connectionHandler{
		conn:    conn,
		ctx:     ctx,
		cancel:  cancel,
		server:  s,
		session: session,
		remote:  conn.RemoteAddr().String(),
	}

	handler.run()
}

func determineSessionMode(r *http.Request) sessionMode {
	query := r.URL.Query()
	if model := query.Get("model"); model != "" {
		return sessionModeRealtime
	}
	intent := strings.ToLower(query.Get("intent"))
	switch intent {
	case "", "transcription":
		return sessionModeTranscription
	default:
		return sessionModeRealtime
	}
}

func normalizeVoice(v openairt.Voice) openairt.Voice {
	if strings.TrimSpace(string(v)) == "" {
		return ""
	}
	return openairt.Voice(strings.ToLower(strings.TrimSpace(string(v))))
}

// NormalizeVoice is an exported helper for consumers that need to align voice
// identifiers with the realtime server expectations.
func NormalizeVoice(v openairt.Voice) openairt.Voice {
	return normalizeVoice(v)
}
