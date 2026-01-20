package google_genai

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"google.golang.org/genai"

	"backend-server/constants"
	log "backend-server/internal/infrastructure/logger"
	"backend-server/internal/registry/tts"
	"backend-server/pkg/audio"
	"backend-server/pkg/confighelper"
)

const (
	defaultTtsSampleRate      = 24000
	defaultTtsFrameDuration   = 20
	defaultGeminiAPIVersion   = "v1alpha"
	defaultVertexAPIVersion   = "v1beta1"
	defaultAudioMimeType      = "audio/pcm"
	defaultOutputChannelDepth = 10
)

type providerConfig struct {
	APIKey        string
	Project       string
	Location      string
	Model         string
	BaseURL       string
	APIVersion    string
	Voice         string
	LanguageCode  string
	SampleRate    int
	FrameDuration int
	Backend       genai.Backend
}

type Provider struct {
	cfg             providerConfig
	client          *genai.Client
	mu              sync.RWMutex
	runtimeVoice    string
	runtimeLanguage string
}

func init() {
	tts.Register([]string{constants.TtsTypeGoogleGenAI}, "Google GenAI Live TTS", func(config map[string]interface{}) (tts.BaseTTSProvider, error) {
		provider, err := NewProvider(config)
		if err != nil {
			log.Errorf("初始化Google GenAI TTS提供者失败: %v", err)
			return nil, err
		}
		log.Infof("Google GenAI TTS提供者初始化成功，model=%s", provider.cfg.Model)
		return provider, nil
	})
}

func NewProvider(config map[string]interface{}) (*Provider, error) {
	helper := confighelper.New(config)
	validator := confighelper.NewValidator()

	model := strings.TrimSpace(helper.GetString("model"))
	validator.RequireString(helper, "model", "Google GenAI TTS 模型名称")

	backend, apiVersion, err := parseBackend(helper.GetString("backend"), helper.GetString("api_version"))
	if err != nil {
		return nil, err
	}

	switch backend {
	case genai.BackendGeminiAPI:
		validator.RequireString(helper, "api_key", "Google GenAI TTS API Key")
	case genai.BackendVertexAI:
		validator.RequireString(helper, "project", "Google GenAI TTS Project")
		validator.RequireString(helper, "location", "Google GenAI TTS Location")
		if strings.TrimSpace(helper.GetString("api_key")) != "" {
			return nil, fmt.Errorf("Google GenAI TTS 在 vertex backend 下不支持 api_key")
		}
	default:
		return nil, fmt.Errorf("Google GenAI TTS backend 无效")
	}

	sampleRate := helper.GetInt("sample_rate", defaultTtsSampleRate)
	if sampleRate <= 0 {
		sampleRate = defaultTtsSampleRate
	}
	frameDuration := helper.GetInt("frame_duration", defaultTtsFrameDuration)
	if frameDuration <= 0 {
		frameDuration = defaultTtsFrameDuration
	}

	if validator.HasErrors() {
		return nil, validator.FirstError()
	}

	cfg := providerConfig{
		APIKey:        strings.TrimSpace(helper.GetString("api_key")),
		Project:       strings.TrimSpace(helper.GetString("project")),
		Location:      strings.TrimSpace(helper.GetString("location")),
		Model:         model,
		BaseURL:       strings.TrimSpace(helper.GetString("base_url")),
		APIVersion:    apiVersion,
		Voice:         strings.TrimSpace(helper.GetString("voice")),
		LanguageCode:  strings.TrimSpace(helper.GetString("language_code")),
		SampleRate:    sampleRate,
		FrameDuration: frameDuration,
		Backend:       backend,
	}

	client, err := newGenAIClient(cfg)
	if err != nil {
		return nil, err
	}

	return &Provider{
		cfg:             cfg,
		client:          client,
		runtimeVoice:    cfg.Voice,
		runtimeLanguage: cfg.LanguageCode,
	}, nil
}

func (p *Provider) ChangeVoice(ctx context.Context, options *tts.ChangeVoiceOptions) error {
	if options == nil {
		return fmt.Errorf("change voice options cannot be nil")
	}

	voice := strings.TrimSpace(options.Voice)
	if voice == "" {
		voice = strings.TrimSpace(options.VoiceID)
	}
	if voice == "" && options.Params != nil {
		if v, ok := options.Params["voice"]; ok {
			voice = strings.TrimSpace(fmt.Sprintf("%v", v))
		}
	}

	language := ""
	if options.Params != nil {
		if v, ok := options.Params["language_code"]; ok {
			language = strings.TrimSpace(fmt.Sprintf("%v", v))
		}
	}

	p.mu.Lock()
	if voice != "" {
		p.runtimeVoice = voice
	}
	if language != "" {
		p.runtimeLanguage = language
	}
	p.mu.Unlock()
	return nil
}

func (p *Provider) TextToSpeech(ctx context.Context, text string, sampleRate int, channels int, frameDuration int) ([][]byte, error) {
	stream, err := p.TextToSpeechStream(ctx, text, sampleRate, channels, frameDuration)
	if err != nil {
		return nil, err
	}

	var frames [][]byte
	for frame := range stream {
		frames = append(frames, frame)
	}
	return frames, nil
}

func (p *Provider) TextToSpeechStream(ctx context.Context, text string, sampleRate int, channels int, frameDuration int) (chan []byte, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, fmt.Errorf("Google GenAI TTS 文本不能为空")
	}

	targetSampleRate := sampleRate
	if targetSampleRate <= 0 {
		targetSampleRate = p.cfg.SampleRate
	}
	targetFrameDuration := frameDuration
	if targetFrameDuration <= 0 {
		targetFrameDuration = p.cfg.FrameDuration
	}
	if channels <= 0 {
		channels = 1
	}

	voice, language := p.getRuntimeSettings()
	session, err := p.connect(ctx, voice, language)
	if err != nil {
		return nil, err
	}

	if err := session.SendClientContent(genai.LiveClientContentInput{
		Turns: genai.Text(text),
	}); err != nil {
		_ = session.Close()
		return nil, fmt.Errorf("发送Google GenAI TTS请求失败: %w", err)
	}

	outputChan := make(chan []byte, defaultOutputChannelDepth)
	go p.consumeAudio(ctx, session, outputChan, targetSampleRate, channels, targetFrameDuration)
	return outputChan, nil
}

func (p *Provider) getRuntimeSettings() (string, string) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.runtimeVoice, p.runtimeLanguage
}

func (p *Provider) connect(ctx context.Context, voice string, language string) (*genai.Session, error) {
	connectConfig := &genai.LiveConnectConfig{
		ResponseModalities: []genai.Modality{genai.ModalityAudio},
	}

	speechConfig := buildSpeechConfig(voice, language)
	if speechConfig != nil {
		connectConfig.SpeechConfig = speechConfig
	}

	session, err := p.client.Live.Connect(ctx, p.cfg.Model, connectConfig)
	if err != nil {
		return nil, fmt.Errorf("连接Google GenAI TTS失败: %w", err)
	}
	return session, nil
}

func (p *Provider) consumeAudio(ctx context.Context, session *genai.Session, outputChan chan<- []byte, sampleRate int, channels int, frameDuration int) {
	defer close(outputChan)
	defer session.Close()

	encoder, err := audio.NewOpusEncoder(sampleRate, channels, frameDuration)
	if err != nil {
		log.Errorf("Google GenAI TTS 创建Opus编码器失败: %v", err)
		return
	}

	frameBytes := encoder.GetFrameBytes()
	pcmBuffer := make([]byte, 0, frameBytes*2)

	go func() {
		<-ctx.Done()
		_ = session.Close()
	}()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		message, err := session.Receive()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Errorf("Google GenAI TTS 接收失败: %v", err)
			return
		}

		if message.GoAway != nil {
			log.Warn("Google GenAI TTS 收到 GoAway，结束连接")
			return
		}

		content := message.ServerContent
		if content == nil {
			continue
		}

		if content.ModelTurn != nil {
			for _, part := range content.ModelTurn.Parts {
				if part == nil || part.InlineData == nil {
					continue
				}
				if !strings.HasPrefix(part.InlineData.MIMEType, defaultAudioMimeType) {
					continue
				}
				if len(part.InlineData.Data) == 0 {
					continue
				}

				pcmBuffer = append(pcmBuffer, part.InlineData.Data...)
				for len(pcmBuffer) >= frameBytes {
					frame := pcmBuffer[:frameBytes]
					pcmBuffer = pcmBuffer[frameBytes:]

					opusFrame, encodeErr := encoder.Encode(frame)
					if encodeErr != nil {
						log.Errorf("Google GenAI TTS Opus编码失败: %v", encodeErr)
						return
					}
					if opusFrame != nil {
						select {
						case outputChan <- opusFrame:
						case <-ctx.Done():
							return
						}
					}
				}
			}
		}

		if content.TurnComplete || content.GenerationComplete {
			if len(pcmBuffer) > 0 {
				opusFrame, encodeErr := encoder.Encode(pcmBuffer)
				if encodeErr != nil {
					log.Errorf("Google GenAI TTS Opus编码失败: %v", encodeErr)
					return
				}
				if opusFrame != nil {
					select {
					case outputChan <- opusFrame:
					case <-ctx.Done():
					}
				}
			}
			return
		}
	}
}

func buildSpeechConfig(voice string, language string) *genai.SpeechConfig {
	voice = strings.TrimSpace(voice)
	language = strings.TrimSpace(language)
	if voice == "" && language == "" {
		return nil
	}

	cfg := &genai.SpeechConfig{
		LanguageCode: language,
	}
	if voice != "" {
		cfg.VoiceConfig = &genai.VoiceConfig{
			PrebuiltVoiceConfig: &genai.PrebuiltVoiceConfig{
				VoiceName: voice,
			},
		}
	}
	return cfg
}

func parseBackend(rawBackend string, rawAPIVersion string) (genai.Backend, string, error) {
	backend := strings.ToLower(strings.TrimSpace(rawBackend))
	if backend == "" {
		backend = "gemini"
	}

	switch backend {
	case "gemini", "gemini_api", "geminiapi":
		if strings.TrimSpace(rawAPIVersion) == "" {
			rawAPIVersion = defaultGeminiAPIVersion
		}
		return genai.BackendGeminiAPI, rawAPIVersion, nil
	case "vertex", "vertexai", "vertex_ai":
		if strings.TrimSpace(rawAPIVersion) == "" {
			rawAPIVersion = defaultVertexAPIVersion
		}
		return genai.BackendVertexAI, rawAPIVersion, nil
	default:
		return genai.BackendUnspecified, "", fmt.Errorf("Google GenAI backend 仅支持 gemini 或 vertex")
	}
}

func newGenAIClient(cfg providerConfig) (*genai.Client, error) {
	clientCfg := &genai.ClientConfig{
		APIKey:   cfg.APIKey,
		Backend:  cfg.Backend,
		Project:  cfg.Project,
		Location: cfg.Location,
		HTTPOptions: genai.HTTPOptions{
			BaseURL:    cfg.BaseURL,
			APIVersion: cfg.APIVersion,
		},
	}
	return genai.NewClient(context.Background(), clientCfg)
}

var _ tts.BaseTTSProvider = (*Provider)(nil)
