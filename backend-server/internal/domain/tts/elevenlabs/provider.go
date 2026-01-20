package elevenlabs

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/LinkedDestiny/elevenlabs-golang/pkg/elevenlabs"
	"github.com/LinkedDestiny/elevenlabs-golang/pkg/elevenlabs/text_to_speech"

	"backend-server/constants"
	log "backend-server/internal/infrastructure/logger"
	"backend-server/internal/registry/tts"
	"backend-server/pkg/audio"
	"backend-server/pkg/confighelper"
)

const (
	defaultTTSModelID        = "eleven_multilingual_v2"
	defaultTTSOutputFormat   = string(text_to_speech.OutputFormatMP3_44100_128)
	defaultTTSLatencySetting = 0
	defaultTTSTimeoutSec     = 120
)

type providerConfig struct {
	APIKey                 string
	VoiceID                string
	ModelID                string
	OutputFormat           text_to_speech.OutputFormat
	Region                 string
	BaseURL                string
	OptimizeStreamingDelay int
	Timeout                time.Duration
}

type Provider struct {
	cfg    providerConfig
	client *elevenlabs.Client
	mu     sync.RWMutex
}

// init registers the ElevenLabs TTS provider.
func init() {
	tts.Register([]string{"elevenlabs", constants.TtsTypeElevenLabs}, "ElevenLabs streaming TTS", func(config map[string]interface{}) (tts.BaseTTSProvider, error) {
		return NewProvider(config)
	})
}

func NewProvider(config map[string]interface{}) (*Provider, error) {
	helper := confighelper.New(config)
	validator := confighelper.NewValidator()

	apiKey := helper.GetString("api_key")
	validator.RequireString(helper, "api_key", "api_key")

	voiceID := helper.GetString("voice_id")
	validator.RequireString(helper, "voice_id", "voice_id")

	modelID := helper.GetString("model_id", defaultTTSModelID)
	outputFormatStr := helper.GetString("output_format", defaultTTSOutputFormat)
	outputFormat := text_to_speech.OutputFormat(outputFormatStr)

	timeoutSeconds := helper.GetInt("timeout_seconds", defaultTTSTimeoutSec)
	if timeoutSeconds <= 0 {
		timeoutSeconds = defaultTTSTimeoutSec
	}

	if validator.HasErrors() {
		return nil, validator.FirstError()
	}

	baseURL, wsURL := resolveBaseURLs(helper.GetString("base_url"), helper.GetString("region"))

	cfg := elevenlabs.DefaultConfig()
	cfg.APIKey = apiKey
	cfg.Environment = elevenlabs.ProductionEnv
	cfg.Timeout = time.Duration(timeoutSeconds) * time.Second
	if baseURL != "" {
		cfg.Environment.BaseURL = baseURL
		cfg.Environment.WebSocketURL = wsURL
	}
	if region := strings.ToLower(helper.GetString("region")); region != "" {
		switch region {
		case "us":
			cfg.Environment = elevenlabs.ProductionUSEnv
		case "eu":
			cfg.Environment = elevenlabs.ProductionEUEnv
		}
	}

	client, err := elevenlabs.NewClientWithConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("elevenlabs tts: create client failed: %w", err)
	}

	return &Provider{
		cfg: providerConfig{
			APIKey:                 apiKey,
			VoiceID:                voiceID,
			ModelID:                modelID,
			OutputFormat:           outputFormat,
			Region:                 helper.GetString("region"),
			BaseURL:                baseURL,
			OptimizeStreamingDelay: helper.GetInt("optimize_streaming_latency", defaultTTSLatencySetting),
			Timeout:                time.Duration(timeoutSeconds) * time.Second,
		},
		client: client,
	}, nil
}

// ChangeVoice updates runtime voice/model parameters.
func (p *Provider) ChangeVoice(ctx context.Context, options *tts.ChangeVoiceOptions) error {
	if options == nil {
		return fmt.Errorf("elevenlabs tts: change voice options cannot be nil")
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
	if voice == "" {
		return fmt.Errorf("elevenlabs tts: voice cannot be empty")
	}

	p.mu.Lock()
	p.cfg.VoiceID = voice
	if options.ModelID != "" {
		p.cfg.ModelID = strings.TrimSpace(options.ModelID)
	}
	p.mu.Unlock()
	return nil
}

// TextToSpeech performs non-streaming synthesis by draining the streaming channel.
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

// TextToSpeechStream uses the ElevenLabs streaming API and converts audio to opus frames.
func (p *Provider) TextToSpeechStream(ctx context.Context, text string, sampleRate int, channels int, frameDuration int) (chan []byte, error) {
	cfg := p.snapshotConfig()
	req := text_to_speech.StreamRequest{
		Text:    text,
		VoiceID: cfg.VoiceID,
	}
	if cfg.ModelID != "" {
		req.ModelID = elevenlabs.StringPtr(cfg.ModelID)
	}
	outFmt := cfg.OutputFormat
	req.OutputFormat = &outFmt
	if cfg.OptimizeStreamingDelay > 0 {
		val := cfg.OptimizeStreamingDelay
		req.OptimizeStreamingLatency = &val
	}

	callCtx := ctx
	if _, hasDeadline := ctx.Deadline(); !hasDeadline && cfg.Timeout > 0 {
		var cancel context.CancelFunc
		callCtx, cancel = context.WithTimeout(ctx, cfg.Timeout)
		defer cancel()
	}

	stream, err := p.client.TextToSpeech.Stream(callCtx, req)
	if err != nil {
		return nil, fmt.Errorf("elevenlabs tts: stream request failed: %w", err)
	}

	pipeReader, pipeWriter := io.Pipe()
	outputChan := make(chan []byte, 100)

	decoder, err := audio.CreateAudioDecoderWithSampleRate(ctx, pipeReader, outputChan, frameDuration, "mp3", sampleRate)
	if err != nil {
		return nil, fmt.Errorf("elevenlabs tts: create decoder failed: %w", err)
	}

	go func() {
		defer pipeWriter.Close()
		for {
			select {
			case <-callCtx.Done():
				return
			case chunk, ok := <-stream:
				if !ok {
					return
				}
				if len(chunk) == 0 {
					continue
				}
				if _, err := pipeWriter.Write(chunk); err != nil {
					log.Errorf("elevenlabs tts: write chunk failed: %v", err)
					return
				}
			}
		}
	}()

	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Errorf("elevenlabs tts: decoder panic: %v", r)
			}
		}()
		startTs := time.Now().UnixMilli()
		if err := decoder.Run(startTs); err != nil {
			log.Errorf("elevenlabs tts: decode stream failed: %v", err)
		}
	}()

	return outputChan, nil
}

func (p *Provider) snapshotConfig() providerConfig {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.cfg
}

func resolveBaseURLs(customBaseURL, region string) (string, string) {
	if customBaseURL != "" {
		return strings.TrimSuffix(customBaseURL, "/"), toWSURL(customBaseURL)
	}

	switch strings.ToLower(region) {
	case "us":
		return elevenlabs.ProductionUSEnv.BaseURL, elevenlabs.ProductionUSEnv.WebSocketURL
	case "eu":
		return elevenlabs.ProductionEUEnv.BaseURL, elevenlabs.ProductionEUEnv.WebSocketURL
	default:
		return elevenlabs.ProductionEnv.BaseURL, elevenlabs.ProductionEnv.WebSocketURL
	}
}

func toWSURL(httpURL string) string {
	if strings.HasPrefix(httpURL, "https://") {
		return "wss://" + strings.TrimPrefix(httpURL, "https://")
	}
	if strings.HasPrefix(httpURL, "http://") {
		return "ws://" + strings.TrimPrefix(httpURL, "http://")
	}
	return httpURL
}
