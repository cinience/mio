package session

import (
	"context"
	"strings"
	"sync"
	"time"

	manager_api "backend-server/internal/adapters/manager"
	log "backend-server/internal/infrastructure/logger"
	audiohelper "backend-server/pkg/audio"

	"github.com/cloudwego/eino/schema"
	"github.com/godeps/opus"
)

const (
	managerUploadTextLimit        = 4000
	managerUploadTimeout          = 5 * time.Second
	managerUserAudioWindow        = 12 * time.Second
	managerAssistantAudioWindow   = 20 * time.Second
	managerMaxOpusFrameDurationMs = 120
	defaultAudioSampleRate        = 16000
)

type managerChatUploader struct {
	session *ChatSession
	svc     manager_api.ManagerAPIService

	mu sync.Mutex

	upstreamSampleMark int64

	downstreamPCM      []float32
	downstreamScratch  []float32
	downstreamDecoder  *opus.Decoder
	downstreamRate     int
	downstreamChannels int
}

func newManagerChatUploader(session *ChatSession) *managerChatUploader {
	if session == nil || session.clientState == nil {
		return nil
	}
	svc := session.GetManagerAPIService()
	if svc == nil {
		return nil
	}

	return &managerChatUploader{
		session: session,
		svc:     svc,
	}
}

func (u *managerChatUploader) RecordDownstreamAudio(frame []byte) {
	if u == nil || len(frame) == 0 || u.svc == nil {
		return
	}

	u.mu.Lock()
	defer u.mu.Unlock()

	if err := u.ensureDecoder(); err != nil {
		log.Debugf("manager uploader skip downstream audio: %v", err)
		return
	}

	maxFrameSamples := u.maxFrameSamples()
	if cap(u.downstreamScratch) < maxFrameSamples*u.downstreamChannels {
		u.downstreamScratch = make([]float32, maxFrameSamples*u.downstreamChannels)
	}
	pcmBuf := u.downstreamScratch[:maxFrameSamples*u.downstreamChannels]

	samples, err := u.downstreamDecoder.DecodeFloat32(frame, pcmBuf)
	if err != nil {
		log.Warnf("manager uploader decode downstream audio failed: %v", err)
		return
	}

	total := samples * u.downstreamChannels
	u.downstreamPCM = append(u.downstreamPCM, pcmBuf[:total]...)
	u.enforceDownstreamLimit()
}

func (u *managerChatUploader) OnMessageLogged(msg *schema.Message) {
	if u == nil || msg == nil || u.svc == nil {
		return
	}

	var audioPayload []byte
	role := msg.Role

	switch role {
	case schema.User:
		audioPayload = u.captureUserAudio()
	case schema.Assistant:
		audioPayload = u.captureAssistantAudio()
	default:
		return
	}

	content := strings.TrimSpace(u.renderMessageContent(msg))
	if content == "" {
		return
	}

	chatType := u.chatTypeForRole(role)
	u.dispatchUpload(chatType, content, audioPayload)
}

func (u *managerChatUploader) captureUserAudio() []byte {
	state := u.session.clientState
	if state == nil || state.AsrAudioBuffer == nil {
		return nil
	}

	u.mu.Lock()
	samples, mark := state.AsrAudioBuffer.GetSince(u.upstreamSampleMark)
	u.upstreamSampleMark = mark
	u.mu.Unlock()
	if len(samples) == 0 {
		return nil
	}

	sr := state.InputAudioFormat.SampleRate
	if sr <= 0 {
		sr = defaultAudioSampleRate
	}
	channels := state.InputAudioFormat.Channels
	if channels <= 0 {
		channels = 1
	}

	trimmed := trimLatestSamples(samples, sr*channels, managerUserAudioWindow)
	if len(trimmed) == 0 {
		return nil
	}

	wavData, err := audiohelper.Float32ToWav(trimmed, sr, channels)
	if err != nil {
		log.Warnf("manager uploader convert upstream audio failed: %v", err)
		return nil
	}
	return wavData
}

func (u *managerChatUploader) captureAssistantAudio() []byte {
	u.mu.Lock()
	if len(u.downstreamPCM) == 0 {
		u.mu.Unlock()
		return nil
	}

	sr := u.downstreamRate
	if sr <= 0 {
		sr = defaultAudioSampleRate
	}
	ch := u.downstreamChannels
	if ch <= 0 {
		ch = 1
	}

	samples := append([]float32(nil), u.downstreamPCM...)
	u.downstreamPCM = u.downstreamPCM[:0]
	u.mu.Unlock()

	trimmed := trimLatestSamples(samples, sr*ch, managerAssistantAudioWindow)
	if len(trimmed) == 0 {
		return nil
	}

	wavData, err := audiohelper.Float32ToWav(trimmed, sr, ch)
	if err != nil {
		log.Warnf("manager uploader convert downstream audio failed: %v", err)
		return nil
	}
	return wavData
}

func (u *managerChatUploader) renderMessageContent(msg *schema.Message) string {
	if msg == nil {
		return ""
	}
	if text := strings.TrimSpace(msg.Content); text != "" {
		return u.truncate(text)
	}

	for _, part := range msg.MultiContent {
		if strings.TrimSpace(part.Text) != "" {
			return u.truncate(part.Text)
		}
	}
	for _, part := range msg.UserInputMultiContent {
		if strings.TrimSpace(part.Text) != "" {
			return u.truncate(part.Text)
		}
	}
	for _, part := range msg.AssistantGenMultiContent {
		if strings.TrimSpace(part.Text) != "" {
			return u.truncate(part.Text)
		}
	}
	return ""
}

func (u *managerChatUploader) truncate(text string) string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) <= managerUploadTextLimit {
		return string(runes)
	}
	return string(runes[:managerUploadTextLimit])
}

func (u *managerChatUploader) chatTypeForRole(role schema.RoleType) string {
	switch role {
	case schema.User:
		return "user"
	case schema.Assistant:
		return "assistant"
	default:
		return strings.ToLower(string(role))
	}
}

func (u *managerChatUploader) dispatchUpload(chatType, content string, audio []byte) {
	deviceID := strings.TrimSpace(u.session.clientState.DeviceID)
	sessionID := strings.TrimSpace(u.session.clientState.SessionID)
	if deviceID == "" || sessionID == "" || content == "" {
		return
	}

	svc := u.svc
	if svc == nil {
		return
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), managerUploadTimeout)
		defer cancel()

		if err := svc.ReportChat(ctx, deviceID, sessionID, chatType, content, audio); err != nil {
			log.Warnf("report chat history failed: device=%s session=%s err=%v", deviceID, sessionID, err)
		}
	}()
}

func (u *managerChatUploader) ensureDecoder() error {
	if u.downstreamDecoder != nil {
		return nil
	}

	rate := u.session.clientState.OutputAudioFormat.SampleRate
	if rate <= 0 {
		rate = defaultAudioSampleRate
	}
	ch := u.session.clientState.OutputAudioFormat.Channels
	if ch <= 0 {
		ch = 1
	}

	decoder, err := opus.NewDecoder(rate, ch)
	if err != nil {
		return err
	}

	u.downstreamDecoder = decoder
	u.downstreamRate = rate
	u.downstreamChannels = ch
	return nil
}

func (u *managerChatUploader) maxFrameSamples() int {
	rate := u.downstreamRate
	if rate <= 0 {
		rate = defaultAudioSampleRate
	}
	return (rate * managerMaxOpusFrameDurationMs) / 1000
}

func (u *managerChatUploader) enforceDownstreamLimit() {
	limit := u.maxDownstreamSamples()
	if limit <= 0 || len(u.downstreamPCM) <= limit {
		return
	}
	start := len(u.downstreamPCM) - limit
	trimmed := append([]float32(nil), u.downstreamPCM[start:]...)
	u.downstreamPCM = trimmed
}

func (u *managerChatUploader) maxDownstreamSamples() int {
	if u.downstreamRate <= 0 {
		return 0
	}
	durationSamples := float64(u.downstreamRate*u.downstreamChannels) * managerAssistantAudioWindow.Seconds()
	if durationSamples <= 0 {
		return 0
	}
	return int(durationSamples)
}

func trimLatestSamples(samples []float32, samplesPerSecond int, window time.Duration) []float32 {
	if len(samples) == 0 || samplesPerSecond <= 0 {
		return nil
	}
	limit := int(float64(samplesPerSecond) * window.Seconds())
	if limit <= 0 || len(samples) <= limit {
		return samples
	}
	return samples[len(samples)-limit:]
}
