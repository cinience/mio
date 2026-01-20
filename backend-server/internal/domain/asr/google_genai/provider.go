package google_genai

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"google.golang.org/genai"

	"backend-server/constants"
	"backend-server/internal/domain/asr/types"
	log "backend-server/internal/infrastructure/logger"
	"backend-server/internal/registry/asr"
	"backend-server/pkg/audio"
	"backend-server/pkg/confighelper"
)

const (
	defaultAsrSampleRate      = 16000
	defaultGeminiAPIVersion   = "v1alpha"
	defaultVertexAPIVersion   = "v1beta1"
	defaultAudioMimeType      = "audio/pcm"
	defaultResultChannelDepth = 10
)

type providerConfig struct {
	APIKey     string
	Project    string
	Location   string
	Model      string
	BaseURL    string
	APIVersion string
	SampleRate int
	Backend    genai.Backend
}

type Provider struct {
	cfg    providerConfig
	client *genai.Client
}

func init() {
	asr.Register([]string{constants.AsrTypeGoogleGenAI}, "Google GenAI Live ASR", func(config map[string]interface{}) (asr.BaseASRProvider, error) {
		provider, err := NewProvider(config)
		if err != nil {
			log.Errorf("初始化Google GenAI ASR提供者失败: %v", err)
			return nil, err
		}
		log.Infof("Google GenAI ASR提供者初始化成功，model=%s", provider.cfg.Model)
		return provider, nil
	})
}

func NewProvider(config map[string]interface{}) (*Provider, error) {
	helper := confighelper.New(config)
	validator := confighelper.NewValidator()

	model := strings.TrimSpace(helper.GetString("model"))
	validator.RequireString(helper, "model", "Google GenAI ASR 模型名称")

	backend, apiVersion, err := parseBackend(helper.GetString("backend"), helper.GetString("api_version"))
	if err != nil {
		return nil, err
	}

	apiKey := strings.TrimSpace(helper.GetString("api_key"))
	project := strings.TrimSpace(helper.GetString("project"))
	location := strings.TrimSpace(helper.GetString("location"))

	switch backend {
	case genai.BackendGeminiAPI:
		validator.RequireString(helper, "api_key", "Google GenAI ASR API Key")
	case genai.BackendVertexAI:
		validator.RequireString(helper, "project", "Google GenAI ASR Project")
		validator.RequireString(helper, "location", "Google GenAI ASR Location")
		if apiKey != "" {
			return nil, fmt.Errorf("Google GenAI ASR 在 vertex backend 下不支持 api_key")
		}
	default:
		return nil, fmt.Errorf("Google GenAI ASR backend 无效")
	}

	sampleRate := helper.GetInt("sample_rate", defaultAsrSampleRate)
	if sampleRate <= 0 {
		sampleRate = defaultAsrSampleRate
	}

	if validator.HasErrors() {
		return nil, validator.FirstError()
	}

	cfg := providerConfig{
		APIKey:     apiKey,
		Project:    project,
		Location:   location,
		Model:      model,
		BaseURL:    strings.TrimSpace(helper.GetString("base_url")),
		APIVersion: apiVersion,
		SampleRate: sampleRate,
		Backend:    backend,
	}

	client, err := newGenAIClient(cfg)
	if err != nil {
		return nil, err
	}

	return &Provider{
		cfg:    cfg,
		client: client,
	}, nil
}

func (p *Provider) Process(pcmData []float32) (string, error) {
	return "", fmt.Errorf("Google GenAI ASR 暂不支持一次性识别，请使用流式接口")
}

func (p *Provider) StreamingRecognize(ctx context.Context, audioStream <-chan []float32) (chan types.StreamingResult, error) {
	session, err := p.connect(ctx)
	if err != nil {
		return nil, err
	}

	resultChan := make(chan types.StreamingResult, defaultResultChannelDepth)
	sendErrCh := make(chan error, 1)
	var once sync.Once

	closeSession := func() {
		once.Do(func() {
			_ = session.Close()
		})
	}

	go func() {
		<-ctx.Done()
		closeSession()
	}()

	go func() {
		err := p.sendAudio(ctx, session, audioStream)
		if err != nil {
			closeSession()
		}
		sendErrCh <- err
	}()

	go func() {
		defer closeSession()
		defer close(resultChan)

		for {
			select {
			case <-ctx.Done():
				resultChan <- types.StreamingResult{Error: ctx.Err()}
				return
			case err := <-sendErrCh:
				if err != nil {
					resultChan <- types.StreamingResult{Error: err}
					return
				}
				sendErrCh = nil
			default:
			}

			message, err := session.Receive()
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				resultChan <- types.StreamingResult{Error: fmt.Errorf("Google GenAI ASR 接收失败: %w", err)}
				return
			}

			if message.GoAway != nil {
				resultChan <- types.StreamingResult{Error: fmt.Errorf("Google GenAI ASR 连接即将关闭")}
				return
			}

			content := message.ServerContent
			if content == nil || content.InputTranscription == nil {
				continue
			}

			text := strings.TrimSpace(content.InputTranscription.Text)
			if text == "" && !content.InputTranscription.Finished {
				continue
			}
			resultChan <- types.StreamingResult{
				Text:    text,
				IsFinal: content.InputTranscription.Finished,
			}
			if content.InputTranscription.Finished {
				return
			}
		}
	}()

	return resultChan, nil
}

func (p *Provider) connect(ctx context.Context) (*genai.Session, error) {
	connectConfig := &genai.LiveConnectConfig{
		ResponseModalities:      []genai.Modality{genai.ModalityText},
		InputAudioTranscription: &genai.AudioTranscriptionConfig{},
	}
	session, err := p.client.Live.Connect(ctx, p.cfg.Model, connectConfig)
	if err != nil {
		return nil, fmt.Errorf("连接Google GenAI ASR失败: %w", err)
	}
	return session, nil
}

func (p *Provider) sendAudio(ctx context.Context, session *genai.Session, audioStream <-chan []float32) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case frame, ok := <-audioStream:
			if !ok {
				return session.SendRealtimeInput(genai.LiveRealtimeInput{
					AudioStreamEnd: true,
				})
			}
			if len(frame) == 0 {
				continue
			}
			pcmBytes := p.float32ToPCMBytes(frame)
			if len(pcmBytes) == 0 {
				continue
			}
			if err := session.SendRealtimeInput(genai.LiveRealtimeInput{
				Audio: &genai.Blob{
					Data:     pcmBytes,
					MIMEType: defaultAudioMimeType,
				},
			}); err != nil {
				return fmt.Errorf("发送Google GenAI ASR音频失败: %w", err)
			}
		}
	}
}

func (p *Provider) float32ToPCMBytes(samples []float32) []byte {
	int16Samples := audio.Float32SliceToInt16Slice(samples)
	return audio.Int16SliceToBytes(int16Samples)
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

var _ asr.BaseASRProvider = (*Provider)(nil)
