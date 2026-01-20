package openai_realtime

import (
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"

	openairt "github.com/WqyJh/go-openai-realtime/v2"

	log "backend-server/internal/infrastructure/logger"
	registryTTS "backend-server/internal/registry/tts"
	"backend-server/pkg/audio"
	"backend-server/pkg/confighelper"
)

const (
	defaultTtsSampleRate    = 24000
	defaultTtsFrameDuration = 20
	defaultTtsSpeed         = 1.0
)

type providerConfig struct {
	URL           string
	Model         string
	AuthToken     string
	SampleRate    int
	FrameDuration int
	Voice         string
	Speed         float32
}

// Provider 基于 speech-server 的 OpenAI Realtime TTS Provider
type Provider struct {
	config       providerConfig
	mu           sync.RWMutex
	runtimeVoice string
	runtimeSpeed float32
}

// NewProvider 创建 OpenAI Realtime TTS Provider
func NewProvider(config map[string]interface{}) (*Provider, error) {
	helper := confighelper.New(config)
	validator := confighelper.NewValidator()

	validator.RequireString(helper, "url", "OpenAI Realtime TTS 服务地址")
	validator.RequireString(helper, "model", "OpenAI Realtime TTS 模型名称")
	if validator.HasErrors() {
		return nil, validator.FirstError()
	}

	cfg := providerConfig{
		URL:           helper.GetString("url"),
		Model:         helper.GetString("model"),
		AuthToken:     helper.GetString("auth_token"),
		SampleRate:    helper.GetInt("sample_rate", defaultTtsSampleRate),
		FrameDuration: helper.GetInt("frame_duration", defaultTtsFrameDuration),
		Voice:         strings.TrimSpace(helper.GetString("voice")),
		Speed:         float32(helper.GetFloat64("speed", defaultTtsSpeed)),
	}

	if cfg.SampleRate <= 0 {
		cfg.SampleRate = defaultTtsSampleRate
	}
	if cfg.FrameDuration <= 0 {
		cfg.FrameDuration = defaultTtsFrameDuration
	}
	if cfg.Speed <= 0 {
		cfg.Speed = defaultTtsSpeed
	}
	if cfg.Voice == "" {
		cfg.Voice = string(openairt.VoiceAlloy)
	}

	return &Provider{
		config:       cfg,
		runtimeVoice: normalizeVoice(cfg.Voice),
		runtimeSpeed: cfg.Speed,
	}, nil
}

// ChangeVoice 动态修改语音和参数
func (p *Provider) ChangeVoice(ctx context.Context, options *registryTTS.ChangeVoiceOptions) error {
	if options == nil {
		return fmt.Errorf("change voice options cannot be nil")
	}

	voice := strings.TrimSpace(options.Voice)
	if voice == "" {
		voice = strings.TrimSpace(options.VoiceID)
	}
	if voice == "" && options.Params != nil {
		if val, ok := options.Params["voice"]; ok {
			voice = fmt.Sprintf("%v", val)
		}
	}
	if voice == "" {
		return fmt.Errorf("voice cannot be empty")
	}

	speed := p.config.Speed
	if options.Params != nil {
		if val, ok := options.Params["speed"]; ok {
			if parsed, ok := parseFloat(val); ok && parsed > 0 {
				speed = float32(parsed)
			}
		}
	}

	p.mu.Lock()
	p.runtimeVoice = normalizeVoice(voice)
	if speed > 0 {
		p.runtimeSpeed = speed
	}
	p.mu.Unlock()
	return nil
}

// TextToSpeech 一次性合成，聚合流式输出
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

// TextToSpeechStream 流式合成
func (p *Provider) TextToSpeechStream(ctx context.Context, text string, sampleRate int, channels int, frameDuration int) (chan []byte, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, fmt.Errorf("OpenAI Realtime TTS 文本不能为空")
	}

	targetSampleRate := sampleRate
	if targetSampleRate <= 0 {
		targetSampleRate = p.config.SampleRate
	}

	targetFrameDuration := frameDuration
	if targetFrameDuration <= 0 {
		targetFrameDuration = p.config.FrameDuration
	}
	if channels <= 0 {
		channels = 1
	}

	streamCtx, cancel, conn, err := p.connectWithFallback(ctx, openairt.WithModel(p.config.Model))
	if err != nil {
		return nil, fmt.Errorf("连接OpenAI Realtime TTS失败: %w", err)
	}

	voice, speed := p.getRuntimeSettings()
	if err := p.sendSessionUpdate(streamCtx, conn, voice, speed, targetSampleRate); err != nil {
		cancel()
		_ = conn.Close()
		return nil, fmt.Errorf("初始化OpenAI Realtime TTS会话失败: %w", err)
	}

	if err := p.sendResponseCreate(streamCtx, conn, text, voice, speed, targetSampleRate); err != nil {
		cancel()
		_ = conn.Close()
		return nil, fmt.Errorf("发送OpenAI Realtime TTS请求失败: %w", err)
	}

	outputChan := make(chan []byte, 10)
	go p.consumeAudio(streamCtx, conn, cancel, outputChan, targetSampleRate, channels, targetFrameDuration)

	return outputChan, nil
}

func (p *Provider) getRuntimeSettings() (string, float32) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.runtimeVoice, p.runtimeSpeed
}

func (p *Provider) sendSessionUpdate(ctx context.Context, conn *openairt.Conn, voice string, speed float32, sampleRate int) error {
	session := openairt.SessionUpdateEvent{
		Session: openairt.SessionUnion{
			Realtime: &openairt.RealtimeSession{
				OutputModalities: []openairt.Modality{openairt.ModalityAudio},
				Audio: &openairt.RealtimeSessionAudio{
					Output: &openairt.SessionAudioOutput{
						Voice: openairt.Voice(voice),
						Speed: speed,
						Format: &openairt.AudioFormatUnion{
							PCM: &openairt.AudioFormatPCM{Rate: sampleRate},
						},
					},
				},
			},
		},
	}
	return conn.SendMessage(ctx, session)
}

func (p *Provider) connectWithFallback(ctx context.Context, opts ...openairt.ConnectOption) (context.Context, context.CancelFunc, *openairt.Conn, error) {
	streamCtx, cancel, conn, err := p.connectOnce(ctx, p.config.URL, opts...)
	if err == nil {
		return streamCtx, cancel, conn, nil
	}

	if !shouldFallbackToLocalhost(p.config.URL) {
		return nil, nil, nil, err
	}

	fallbackURL, parseErr := switchHostToLocalhost(p.config.URL)
	if parseErr != nil {
		return nil, nil, nil, err
	}

	log.Warnf("连接OpenAI Realtime TTS地址 %s 失败，自动替换 host 为 localhost 重试: %v", p.config.URL, err)

	streamCtx, cancel, conn, retryErr := p.connectOnce(ctx, fallbackURL, opts...)
	if retryErr != nil {
		return nil, nil, nil, fmt.Errorf("fallback to localhost failed: %w (original error: %v)", retryErr, err)
	}

	return streamCtx, cancel, conn, nil
}

func (p *Provider) connectOnce(ctx context.Context, baseURL string, opts ...openairt.ConnectOption) (context.Context, context.CancelFunc, *openairt.Conn, error) {
	streamCtx, cancel := context.WithCancel(ctx)

	clientCfg := openairt.DefaultConfig(p.config.AuthToken)
	clientCfg.BaseURL = baseURL
	client := openairt.NewClientWithConfig(clientCfg)

	conn, err := client.Connect(streamCtx, opts...)
	if err != nil {
		cancel()
		return nil, nil, nil, err
	}

	return streamCtx, cancel, conn, nil
}

func shouldFallbackToLocalhost(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	return parsed.Hostname() == "example.localhost"
}

func switchHostToLocalhost(rawURL string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}

	port := parsed.Port()
	if port != "" {
		parsed.Host = net.JoinHostPort("localhost", port)
	} else {
		parsed.Host = "localhost"
	}
	return parsed.String(), nil
}

func (p *Provider) sendResponseCreate(ctx context.Context, conn *openairt.Conn, text string, voice string, speed float32, sampleRate int) error {
	userItem := openairt.MessageItemUnion{
		User: &openairt.MessageItemUser{
			Content: []openairt.MessageContentInput{
				{
					Type: openairt.MessageContentTypeInputText,
					Text: text,
				},
			},
		},
	}

	params := openairt.ResponseCreateParams{
		Input: []openairt.MessageItemUnion{userItem},
		OutputModalities: []openairt.Modality{
			openairt.ModalityAudio,
		},
		Audio: &openairt.ResponseAudio{
			Output: &openairt.ResponseAudioOutput{
				Voice: openairt.Voice(voice),
				Format: &openairt.AudioFormatUnion{
					PCM: &openairt.AudioFormatPCM{Rate: sampleRate},
				},
			},
		},
	}

	event := openairt.ResponseCreateEvent{
		Response: params,
	}
	return conn.SendMessage(ctx, event)
}

func (p *Provider) consumeAudio(ctx context.Context, conn *openairt.Conn, cancel context.CancelFunc, outputChan chan<- []byte, sampleRate int, channels int, frameDuration int) {
	defer cancel()
	defer close(outputChan)
	defer conn.Close()

	encoder, err := audio.NewOpusEncoder(sampleRate, channels, frameDuration)
	if err != nil {
		log.Errorf("OpenAI Realtime TTS 创建Opus编码器失败: %v", err)
		return
	}

	frameBytes := encoder.GetFrameBytes()
	pcmBuffer := make([]byte, 0, frameBytes*4)
	// TODO(xiaozhi): speech-server 目前在 session.updated 中会返回实际 PCM 采样率（22.05kHz），
	// backend 端需要根据该值做重采样或动态重建 OpusEncoder，避免 24kHz 配置与真实音频不一致。

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		event, err := conn.ReadMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Errorf("OpenAI Realtime TTS 读取事件失败: %v", err)

			// 在退出前，处理 pcmBuffer 中的剩余数据（最后一个字的音频）
			if len(pcmBuffer) > 0 {
				log.Debugf("OpenAI Realtime TTS 在退出前处理剩余的 PCM 数据: %d 字节", len(pcmBuffer))
				opusFrame, encodeErr := encoder.Encode(pcmBuffer)
				if encodeErr != nil {
					log.Errorf("OpenAI Realtime TTS Opus编码失败: %v", encodeErr)
					return
				}
				if opusFrame != nil {
					select {
					case outputChan <- opusFrame:
						log.Debugf("OpenAI Realtime TTS 成功发送最后一帧音频数据")
					case <-ctx.Done():
					}
				}
			}
			return
		}

		switch evt := event.(type) {
		case openairt.ResponseOutputAudioDeltaEvent:
			chunk, decodeErr := base64.StdEncoding.DecodeString(evt.Delta)
			if decodeErr != nil {
				log.Errorf("OpenAI Realtime TTS 解码音频数据失败: %v", decodeErr)
				return
			}
			pcmBuffer = append(pcmBuffer, chunk...)
			for len(pcmBuffer) >= frameBytes {
				frame := pcmBuffer[:frameBytes]
				pcmBuffer = pcmBuffer[frameBytes:]

				opusFrame, encodeErr := encoder.Encode(frame)
				if encodeErr != nil {
					log.Errorf("OpenAI Realtime TTS Opus编码失败: %v", encodeErr)
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
		case openairt.ResponseOutputAudioDoneEvent, openairt.ResponseDoneEvent:
			if len(pcmBuffer) > 0 {
				opusFrame, encodeErr := encoder.Encode(pcmBuffer)
				if encodeErr != nil {
					log.Errorf("OpenAI Realtime TTS Opus编码失败: %v", encodeErr)
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
		case openairt.ErrorEvent:
			message := evt.Error.Message
			if message == "" {
				message = "speech-server 返回未知错误"
			}
			log.Errorf("OpenAI Realtime TTS 错误: %s", message)
			return
		default:
			//log.Debugf("OpenAI Realtime TTS 忽略事件: %s", evt.ServerEventType())
		}
	}
}

func normalizeVoice(v string) string {
	return strings.ToLower(strings.TrimSpace(v))
}

func parseFloat(value interface{}) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case int32:
		return float64(v), true
	case uint:
		return float64(v), true
	case uint32:
		return float64(v), true
	case uint64:
		return float64(v), true
	case string:
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f, true
		}
	default:
		if s, ok := value.(fmt.Stringer); ok {
			if f, err := strconv.ParseFloat(s.String(), 64); err == nil {
				return f, true
			}
		}
	}
	return 0, false
}

// 确保 Provider 实现 tts.BaseTTSProvider 接口
var _ registryTTS.BaseTTSProvider = (*Provider)(nil)

