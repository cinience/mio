package gpt_sovits_v3

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"backend-server/constants"
	log "backend-server/internal/infrastructure/logger"
	"backend-server/internal/registry/tts"
	"backend-server/pkg/audio"
	"backend-server/pkg/confighelper"
)

// init 函数注册GPT-SoVITS V3 TTS提供者
func init() {
	// 注册 GPT-SoVITS V3 TTS 提供者
	tts.Register([]string{"TTS_GPT_SOVITS_V3", constants.TtsTypeGptSovitsV3}, "GPT-SoVITS V3 TTS 语音合成服务", func(config map[string]interface{}) (tts.BaseTTSProvider, error) {
		provider := NewGptSovitsV3TTSProvider(config)
		return provider, nil
	})
}

// 全局HTTP客户端，实现连接池
var (
	httpClient     *http.Client
	httpClientOnce sync.Once
)

// 获取配置了连接池的HTTP客户端
func getHTTPClient() *http.Client {
	httpClientOnce.Do(func() {
		transport := &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			MaxIdleConns:          100,
			MaxIdleConnsPerHost:   10,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		}
		httpClient = &http.Client{
			Transport: transport,
			Timeout:   60 * time.Second, // GPT-SoVITS可能需要更长时间
		}
	})
	return httpClient
}

// GptSovitsV3TTSProvider GPT-SoVITS V3 TTS提供者
type GptSovitsV3TTSProvider struct {
	URL            string  `json:"url"`
	ReferWavPath   string  `json:"refer_wav_path"`
	PromptText     string  `json:"prompt_text"`
	PromptLanguage string  `json:"prompt_language"`
	TextLanguage   string  `json:"text_language"`
	TopK           int     `json:"top_k"`
	TopP           float64 `json:"top_p"`
	Temperature    float64 `json:"temperature"`
	SampleSteps    int     `json:"sample_steps"`
	Speed          float64 `json:"speed"`
	CutPunc        string  `json:"cut_punc"`
	InpRefs        string  `json:"inp_refs"`
	IfSr           bool    `json:"if_sr"`
	AudioFileType  string  `json:"audio_file_type"`
	settingsMu     sync.RWMutex
}

// NewGptSovitsV3TTSProvider 创建新的GPT-SoVITS V3 TTS提供者
func NewGptSovitsV3TTSProvider(config map[string]interface{}) *GptSovitsV3TTSProvider {
	// 使用配置助手简化配置获取
	helper := confighelper.New(config)

	provider := &GptSovitsV3TTSProvider{
		// 使用配置助手获取值，提供默认值
		URL:            helper.GetString("url", "http://127.0.0.1:9880"),
		ReferWavPath:   helper.GetString("refer_wav_path", ""),
		PromptText:     helper.GetString("prompt_text", ""),
		PromptLanguage: helper.GetString("prompt_language", "auto"),
		TextLanguage:   helper.GetString("text_language", "auto"),
		TopK:           helper.GetInt("top_k", 5),
		TopP:           helper.GetFloat64("top_p", 1.0),
		Temperature:    helper.GetFloat64("temperature", 1.0),
		SampleSteps:    helper.GetInt("sample_steps", 50),
		Speed:          helper.GetFloat64("speed", 1.0),
		CutPunc:        helper.GetString("cut_punc", "。？！.?!；;：:"),
		InpRefs:        helper.GetString("inp_refs", ""),
		IfSr:           helper.GetBool("if_sr", false),
		AudioFileType:  helper.GetString("audio_file_type", "wav"),
	}

	return provider
}

// ChangeVoice updates the GPT-SoVITS provider runtime parameters.
func (p *GptSovitsV3TTSProvider) ChangeVoice(ctx context.Context, options *tts.ChangeVoiceOptions) error {
	if options == nil {
		return fmt.Errorf("change voice options cannot be nil")
	}

	parseFloat := func(val interface{}, fallback float64) float64 {
		switch v := val.(type) {
		case float64:
			return v
		case float32:
			return float64(v)
		case int:
			return float64(v)
		case int64:
			return float64(v)
		case string:
			if v == "" {
				return fallback
			}
			if parsed, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
				return parsed
			}
		}
		return fallback
	}

	parseInt := func(val interface{}, fallback int) int {
		switch v := val.(type) {
		case int:
			return v
		case int64:
			return int(v)
		case float64:
			return int(v)
		case float32:
			return int(v)
		case string:
			if v == "" {
				return fallback
			}
			if parsed, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
				return parsed
			}
		}
		return fallback
	}

	resolveString := func(val interface{}, fallback string) string {
		if val == nil {
			return fallback
		}
		str := strings.TrimSpace(fmt.Sprintf("%v", val))
		if str == "" {
			return fallback
		}
		return str
	}

	p.settingsMu.Lock()
	defer p.settingsMu.Unlock()

	if voice := strings.TrimSpace(options.Voice); voice != "" {
		p.PromptText = voice
	}

	if params := options.Params; params != nil {
		if val, ok := params["refer_wav_path"]; ok {
			p.ReferWavPath = resolveString(val, p.ReferWavPath)
		}
		if val, ok := params["prompt_text"]; ok {
			p.PromptText = resolveString(val, p.PromptText)
		}
		if val, ok := params["prompt_language"]; ok {
			p.PromptLanguage = resolveString(val, p.PromptLanguage)
		}
		if val, ok := params["text_language"]; ok {
			p.TextLanguage = resolveString(val, p.TextLanguage)
		}
		if val, ok := params["top_k"]; ok {
			p.TopK = parseInt(val, p.TopK)
		}
		if val, ok := params["top_p"]; ok {
			p.TopP = parseFloat(val, p.TopP)
		}
		if val, ok := params["temperature"]; ok {
			p.Temperature = parseFloat(val, p.Temperature)
		}
		if val, ok := params["sample_steps"]; ok {
			p.SampleSteps = parseInt(val, p.SampleSteps)
		}
		if val, ok := params["speed"]; ok {
			p.Speed = parseFloat(val, p.Speed)
		}
		if val, ok := params["cut_punc"]; ok {
			p.CutPunc = resolveString(val, p.CutPunc)
		}
		if val, ok := params["inp_refs"]; ok {
			p.InpRefs = resolveString(val, p.InpRefs)
		}
		if val, ok := params["if_sr"]; ok {
			switch v := val.(type) {
			case bool:
				p.IfSr = v
			case string:
				parsed, err := strconv.ParseBool(strings.TrimSpace(v))
				if err == nil {
					p.IfSr = parsed
				}
			}
		}
		if val, ok := params["audio_file_type"]; ok {
			p.AudioFileType = resolveString(val, p.AudioFileType)
		}
		if val, ok := params["url"]; ok {
			p.URL = resolveString(val, p.URL)
		}
	}

	return nil
}

// TextToSpeech 将文本转换为语音，返回音频帧数据和错误
func (p *GptSovitsV3TTSProvider) TextToSpeech(ctx context.Context, text string, sampleRate int, channels int, frameDuration int) ([][]byte, error) {
	// 构建请求URL，GPT-SoVITS V3使用GET请求方式
	p.settingsMu.RLock()
	baseURL := p.URL
	referWavPath := p.ReferWavPath
	promptText := p.PromptText
	promptLanguage := p.PromptLanguage
	textLanguage := p.TextLanguage
	topK := p.TopK
	topP := p.TopP
	temperature := p.Temperature
	sampleSteps := p.SampleSteps
	speed := p.Speed
	cutPunc := p.CutPunc
	inpRefs := p.InpRefs
	ifSr := p.IfSr
	audioFileType := p.AudioFileType
	p.settingsMu.RUnlock()

	params := url.Values{}

	// 添加所有参数作为查询参数
	params.Add("text", text)
	params.Add("refer_wav_path", referWavPath)
	params.Add("prompt_text", promptText)
	params.Add("prompt_language", promptLanguage)
	params.Add("text_language", textLanguage)
	params.Add("top_k", strconv.Itoa(topK))
	params.Add("top_p", fmt.Sprintf("%.2f", topP))
	params.Add("temperature", fmt.Sprintf("%.2f", temperature))
	params.Add("sample_steps", strconv.Itoa(sampleSteps))
	params.Add("speed", fmt.Sprintf("%.2f", speed))
	params.Add("cut_punc", cutPunc)
	params.Add("inp_refs", inpRefs)
	params.Add("if_sr", strconv.FormatBool(ifSr))
	params.Add("audio_file_type", audioFileType)

	// 构建完整的URL
	fullURL := baseURL + "?" + params.Encode()

	log.Infof("GPT-SoVITS V3 请求 URL: %s", fullURL)

	// 创建HTTP GET请求
	req, err := http.NewRequestWithContext(ctx, "GET", fullURL, nil)
	if err != nil {
		return nil, fmt.Errorf("创建GPT-SoVITS V3请求失败: %v", err)
	}

	// 设置请求头
	req.Header.Set("Accept", "audio/wav")

	// 发送请求
	client := getHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GPT-SoVITS V3请求失败: %v", err)
	}
	defer resp.Body.Close()

	// 检查响应状态
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GPT-SoVITS V3返回错误状态 %d: %s", resp.StatusCode, string(body))
	}

	// 读取音频数据
	audioData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取GPT-SoVITS V3音频数据失败: %v", err)
	}

	if len(audioData) == 0 {
		return nil, fmt.Errorf("GPT-SoVITS V3返回空音频数据")
	}

	log.Infof("GPT-SoVITS V3 接收到音频数据 %d 字节", len(audioData))

	// 转换音频格式为Opus帧
	opusFrames, err := audio.WavToOpus(audioData, sampleRate, channels, frameDuration)
	if err != nil {
		return nil, fmt.Errorf("GPT-SoVITS V3音频格式转换失败: %v", err)
	}

	log.Infof("GPT-SoVITS V3 转换完成，生成 %d 个Opus帧", len(opusFrames))

	return opusFrames, nil
}

// TextToSpeechStream 流式文本转语音（GPT-SoVITS V3目前不支持真正的流式，使用非流式方式实现）
func (p *GptSovitsV3TTSProvider) TextToSpeechStream(ctx context.Context, text string, sampleRate int, channels int, frameDuration int) (outputChan chan []byte, err error) {
	// GPT-SoVITS V3目前不支持流式输出，使用非流式方式实现
	outputChan = make(chan []byte, 10)

	go func() {
		defer close(outputChan)

		frames, err := p.TextToSpeech(ctx, text, sampleRate, channels, frameDuration)
		if err != nil {
			log.Errorf("GPT-SoVITS V3流式转换失败: %v", err)
			return
		}

		// 将所有帧发送到输出通道
		for _, frame := range frames {
			select {
			case <-ctx.Done():
				return
			case outputChan <- frame:
			}
		}
	}()

	return outputChan, nil
}

// GetVoiceInfo 获取语音信息
func (p *GptSovitsV3TTSProvider) GetVoiceInfo() map[string]interface{} {
	return map[string]interface{}{
		"type":            "gpt_sovits_v3",
		"url":             p.URL,
		"refer_wav_path":  p.ReferWavPath,
		"prompt_text":     p.PromptText,
		"prompt_language": p.PromptLanguage,
		"text_language":   p.TextLanguage,
		"speed":           p.Speed,
		"temperature":     p.Temperature,
		"top_k":           p.TopK,
		"top_p":           p.TopP,
		"sample_steps":    p.SampleSteps,
		"audio_file_type": p.AudioFileType,
	}
}
