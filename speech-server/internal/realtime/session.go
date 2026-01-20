package realtime

import (
	"strings"
	"sync"
	"time"

	openairt "github.com/WqyJh/go-openai-realtime/v2"
	"github.com/google/uuid"
)

type sessionState struct {
	id          string
	createdAt   time.Time
	sessionCfg  openairt.SessionUnion
	clientRate  int
	outputRate  int
	outputVoice openairt.Voice
	outputSpeed float32
	// speaker controls
	speakerIntent      string
	speakerID          string
	speakerName        string
	speakerThreshold   float32
	speakerMinDuration float32
	mode               sessionMode
	bufferMu           sync.Mutex
	audioBuffer        []byte
	vad                *vadSessionState
	vadModel           string
	// per-session tuning
	skipNoiseFilter        bool
	customEnergyThreshold  float32
	customMinDuration      float32
	hasEnergyOverride      bool
	hasMinDurationOverride bool
	languageHintOverride   string
	languageWhitelist      []string
}

type sessionMode int

const (
	sessionModeTranscription sessionMode = iota
	sessionModeRealtime
)

func newSessionState(defaultClientRate int, mode sessionMode, defaultVoice openairt.Voice, defaultSpeed float32) *sessionState {
	sessionID := uuid.NewString()
	defaultVoice = normalizeVoice(defaultVoice)
	if defaultVoice == "" {
		defaultVoice = normalizeVoice(openairt.VoiceAlloy)
	}
	if defaultSpeed <= 0 {
		defaultSpeed = 1.0
	}

	state := &sessionState{
		id:          sessionID,
		createdAt:   time.Now(),
		clientRate:  defaultClientRate,
		outputRate:  defaultClientRate,
		outputVoice: defaultVoice,
		outputSpeed: defaultSpeed,
		mode:        mode,
	}

	switch mode {
	case sessionModeRealtime:
		state.sessionCfg = openairt.SessionUnion{
			Realtime: &openairt.RealtimeSession{
				Model: "sherpa-onnx-matcha-tts",
				Audio: &openairt.RealtimeSessionAudio{
					Input: &openairt.SessionAudioInput{
						Format: &openairt.AudioFormatUnion{
							PCM: &openairt.AudioFormatPCM{Rate: defaultClientRate},
						},
						Transcription: &openairt.AudioTranscription{
							Model:    "sherpa-onnx-sense-voice",
							Language: "auto",
						},
					},
					Output: &openairt.SessionAudioOutput{
						Format: &openairt.AudioFormatUnion{
							PCM: &openairt.AudioFormatPCM{Rate: defaultClientRate},
						},
						Voice: defaultVoice,
						Speed: defaultSpeed,
					},
				},
				OutputModalities: []openairt.Modality{openairt.ModalityAudio},
			},
		}
	default:
		state.sessionCfg = openairt.SessionUnion{
			Transcription: &openairt.TranscriptionSession{
				Audio: &openairt.TranscriptionSessionAudio{
					Input: &openairt.SessionAudioInput{
						Format: &openairt.AudioFormatUnion{
							PCM: &openairt.AudioFormatPCM{Rate: defaultClientRate},
						},
						Transcription: &openairt.AudioTranscription{
							Model:    "sherpa-onnx-sense-voice",
							Language: "auto",
						},
					},
				},
			},
		}
	}

	return state
}

func (s *sessionState) applyUpdate(update openairt.SessionUnion) {
	switch s.mode {
	case sessionModeRealtime:
		if s.sessionCfg.Realtime == nil {
			s.sessionCfg.Realtime = &openairt.RealtimeSession{
				OutputModalities: []openairt.Modality{openairt.ModalityAudio},
			}
		}
		realtime := s.sessionCfg.Realtime
		if realtime.Audio == nil {
			realtime.Audio = &openairt.RealtimeSessionAudio{}
		}
		if update.Realtime != nil {
			if update.Realtime.Model != "" {
				realtime.Model = update.Realtime.Model
			}
			if update.Realtime.Instructions != "" {
				realtime.Instructions = update.Realtime.Instructions
			}
			if update.Realtime.OutputModalities != nil {
				realtime.OutputModalities = update.Realtime.OutputModalities
			}
			if update.Realtime.Audio != nil {
				s.applyInputConfig(&realtime.Audio.Input, update.Realtime.Audio.Input)
				s.applyOutputConfig(&realtime.Audio.Output, update.Realtime.Audio.Output)
			} else {
				s.applyOutputConfig(&realtime.Audio.Output, nil)
			}
		} else {
			s.applyOutputConfig(&realtime.Audio.Output, nil)
		}

		if update.Transcription != nil && update.Transcription.Audio != nil {
			s.applyInputConfig(&realtime.Audio.Input, update.Transcription.Audio.Input)
		}

		s.sessionCfg.Transcription = nil

	default:
		if s.sessionCfg.Transcription == nil {
			s.sessionCfg.Transcription = &openairt.TranscriptionSession{}
		}
		trans := s.sessionCfg.Transcription
		if trans.Audio == nil {
			trans.Audio = &openairt.TranscriptionSessionAudio{}
		}

		if update.Transcription != nil && update.Transcription.Audio != nil {
			s.applyInputConfig(&trans.Audio.Input, update.Transcription.Audio.Input)
		}

		if update.Realtime != nil && update.Realtime.Audio != nil {
			s.applyVoicePreferences(update.Realtime.Audio.Output)
		}

		s.sessionCfg.Realtime = nil
	}
}

func (s *sessionState) applyInputConfig(dest **openairt.SessionAudioInput, src *openairt.SessionAudioInput) {
	if src == nil {
		return
	}
	if *dest == nil {
		*dest = &openairt.SessionAudioInput{}
	}
	if src.Format != nil {
		format := copyAudioFormat(src.Format)
		(*dest).Format = format
		if format != nil && format.PCM != nil && format.PCM.Rate > 0 {
			s.clientRate = format.PCM.Rate
		}
	}
	if src.Transcription != nil {
		tr := *src.Transcription
		(*dest).Transcription = &tr
	}
	if src.NoiseReduction != nil {
		nr := *src.NoiseReduction
		(*dest).NoiseReduction = &nr
	}
	if src.TurnDetection != nil {
		td := *src.TurnDetection
		(*dest).TurnDetection = &td
	}
}

func (s *sessionState) applyOutputConfig(dest **openairt.SessionAudioOutput, src *openairt.SessionAudioOutput) {
	if dest == nil {
		s.applyVoicePreferences(src)
		return
	}
	if *dest == nil {
		*dest = &openairt.SessionAudioOutput{}
	}
	if src != nil {
		if src.Format != nil {
			format := copyAudioFormat(src.Format)
			(*dest).Format = format
			if rate := pcmRateFromFormat(format); rate > 0 {
				s.outputRate = rate
			}
		}
		if src.Voice != "" {
			s.outputVoice = normalizeVoice(src.Voice)
		}
		if src.Speed > 0 {
			s.outputSpeed = src.Speed
		}
	} else if (*dest).Format != nil {
		if rate := pcmRateFromFormat((*dest).Format); rate > 0 {
			s.outputRate = rate
		}
	}

	if (*dest).Format == nil && s.outputRate > 0 {
		(*dest).Format = &openairt.AudioFormatUnion{
			PCM: &openairt.AudioFormatPCM{Rate: s.outputRate},
		}
	} else if (*dest).Format != nil && (*dest).Format.PCM != nil {
		if s.outputRate > 0 && (*dest).Format.PCM.Rate == 0 {
			(*dest).Format.PCM.Rate = s.outputRate
		}
	}
	(*dest).Voice = s.outputVoice
	if s.outputSpeed > 0 {
		(*dest).Speed = s.outputSpeed
	}
}

func (s *sessionState) applyVoicePreferences(output *openairt.SessionAudioOutput) {
	if output == nil {
		return
	}
	if output.Format != nil {
		if rate := pcmRateFromFormat(output.Format); rate > 0 {
			s.outputRate = rate
		}
	}
	if output.Voice != "" {
		s.outputVoice = normalizeVoice(output.Voice)
	}
	if output.Speed > 0 {
		s.outputSpeed = output.Speed
	}
}

func (s *sessionState) setVADModel(model string) bool {
	trimmed := strings.TrimSpace(model)
	if trimmed == "" {
		return false
	}
	if s.vadModel == trimmed {
		return false
	}
	s.vadModel = trimmed
	return true
}

func copyAudioFormat(src *openairt.AudioFormatUnion) *openairt.AudioFormatUnion {
	if src == nil {
		return nil
	}
	out := &openairt.AudioFormatUnion{}
	if src.PCM != nil {
		pcm := *src.PCM
		out.PCM = &pcm
	}
	if src.PCMU != nil {
		pcmu := *src.PCMU
		out.PCMU = &pcmu
	}
	if src.PCMA != nil {
		pcma := *src.PCMA
		out.PCMA = &pcma
	}
	return out
}

func pcmRateFromFormat(format *openairt.AudioFormatUnion) int {
	if format == nil || format.PCM == nil {
		return 0
	}
	if format.PCM.Rate <= 0 {
		return 0
	}
	return format.PCM.Rate
}

func (s *sessionState) takeAudioBuffer() []byte {
	s.bufferMu.Lock()
	defer s.bufferMu.Unlock()
	if len(s.audioBuffer) == 0 {
		return nil
	}
	data := make([]byte, len(s.audioBuffer))
	copy(data, s.audioBuffer)
	s.audioBuffer = s.audioBuffer[:0]
	return data
}

func (s *sessionState) clearAudioBuffer() {
	s.bufferMu.Lock()
	s.audioBuffer = s.audioBuffer[:0]
	s.bufferMu.Unlock()
}

func (s *sessionState) close() {
	if s.vad != nil {
		s.vad.close()
		s.vad = nil
	}
}

func (s *sessionState) currentServerVad() *openairt.ServerVad {
	switch s.mode {
	case sessionModeRealtime:
		if s.sessionCfg.Realtime == nil || s.sessionCfg.Realtime.Audio == nil || s.sessionCfg.Realtime.Audio.Input == nil || s.sessionCfg.Realtime.Audio.Input.TurnDetection == nil {
			return nil
		}
		return s.sessionCfg.Realtime.Audio.Input.TurnDetection.ServerVad
	default:
		if s.sessionCfg.Transcription == nil || s.sessionCfg.Transcription.Audio == nil || s.sessionCfg.Transcription.Audio.Input == nil || s.sessionCfg.Transcription.Audio.Input.TurnDetection == nil {
			return nil
		}
		return s.sessionCfg.Transcription.Audio.Input.TurnDetection.ServerVad
	}
}

func (s *sessionState) languageHint() string {
	var hints []string
	if hint := strings.TrimSpace(s.languageHintOverride); hint != "" {
		hints = append(hints, hint)
	}
	if base := strings.TrimSpace(s.sessionLanguage()); base != "" {
		hints = append(hints, base)
	}
	if len(s.languageWhitelist) > 0 {
		hints = append(hints, s.languageWhitelist...)
	}
	unique := dedupeLanguages(hints)
	if len(unique) == 0 {
		return ""
	}
	return strings.Join(unique, ",")
}

func (s *sessionState) sessionLanguage() string {
	switch s.mode {
	case sessionModeRealtime:
		if s.sessionCfg.Realtime != nil &&
			s.sessionCfg.Realtime.Audio != nil &&
			s.sessionCfg.Realtime.Audio.Input != nil &&
			s.sessionCfg.Realtime.Audio.Input.Transcription != nil {
			return strings.TrimSpace(s.sessionCfg.Realtime.Audio.Input.Transcription.Language)
		}
	default:
		if s.sessionCfg.Transcription != nil &&
			s.sessionCfg.Transcription.Audio != nil &&
			s.sessionCfg.Transcription.Audio.Input != nil &&
			s.sessionCfg.Transcription.Audio.Input.Transcription != nil {
			return strings.TrimSpace(s.sessionCfg.Transcription.Audio.Input.Transcription.Language)
		}
	}
	return ""
}

func (s *sessionState) noiseFilterConfig(defaultEnergy, defaultMin float32) (skip bool, energy float32, min float32) {
	skip = s.skipNoiseFilter
	energy = defaultEnergy
	min = defaultMin
	if s.hasEnergyOverride {
		energy = s.customEnergyThreshold
	}
	if s.hasMinDurationOverride {
		min = s.customMinDuration
	}
	if energy <= 0 {
		energy = 0.01
	}
	if min <= 0 {
		min = 0.15
	}
	return skip, energy, min
}

func (s *sessionState) applyMetadata(meta sessionMetadata) {
	if meta.SkipNoiseFilter != nil {
		s.skipNoiseFilter = *meta.SkipNoiseFilter
	}
	if meta.EnergyThreshold != nil {
		val := *meta.EnergyThreshold
		s.customEnergyThreshold = val
		s.hasEnergyOverride = val > 0
	}
	if meta.MinDuration != nil {
		val := *meta.MinDuration
		s.customMinDuration = val
		s.hasMinDurationOverride = val > 0
	}
	if meta.LanguageHint != nil {
		s.languageHintOverride = strings.TrimSpace(*meta.LanguageHint)
	}
	if meta.LanguageWhitelist != nil {
		s.languageWhitelist = normalizeLanguageList(*meta.LanguageWhitelist)
	}
	if meta.SpeakerIntent != nil {
		s.speakerIntent = strings.TrimSpace(*meta.SpeakerIntent)
	}
	if meta.SpeakerID != nil {
		s.speakerID = strings.TrimSpace(*meta.SpeakerID)
	}
	if meta.SpeakerName != nil {
		s.speakerName = strings.TrimSpace(*meta.SpeakerName)
	}
	if meta.SpeakerThreshold != nil && *meta.SpeakerThreshold > 0 {
		s.speakerThreshold = *meta.SpeakerThreshold
	}
	if meta.SpeakerMinDuration != nil && *meta.SpeakerMinDuration > 0 {
		s.speakerMinDuration = *meta.SpeakerMinDuration
	}
}

type sessionMetadata struct {
	SkipNoiseFilter    *bool
	EnergyThreshold    *float32
	MinDuration        *float32
	LanguageHint       *string
	LanguageWhitelist  *[]string
	SpeakerIntent      *string
	SpeakerID          *string
	SpeakerName        *string
	SpeakerThreshold   *float32
	SpeakerMinDuration *float32
}

func (s *sessionState) speakerThresholdOrDefault(defaultVal float32) float32 {
	if s.speakerThreshold > 0 {
		return s.speakerThreshold
	}
	return defaultVal
}

func (s *sessionState) speakerMinDurationOrDefault(defaultVal float32) float32 {
	if s.speakerMinDuration > 0 {
		return s.speakerMinDuration
	}
	return defaultVal
}

func normalizeLanguageList(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	return dedupeLanguages(items)
}

func dedupeLanguages(items []string) []string {
	seen := make(map[string]struct{})
	var result []string
	for _, raw := range items {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		key := strings.ToLower(trimmed)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}
