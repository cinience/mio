package cosyvoice

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
	audiofmt "backend-server/internal/domain/audio"
	log "backend-server/internal/infrastructure/logger"
	"backend-server/internal/registry/tts"
	audiohelper "backend-server/pkg/audio"
	"backend-server/pkg/confighelper"
)

// init 函数注册CosyVoice TTS提供者
func init() {
	// 注册 CosyVoice TTS 提供者
	tts.Register([]string{constants.TtsTypeCosyvoice}, "CosyVoice TTS 语音合成服务", func(config map[string]interface{}) (tts.BaseTTSProvider, error) {
		provider := NewCosyVoiceTTSProvider(config)
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
			Timeout:   30 * time.Second,
		}
	})
	return httpClient
}

// CosyVoiceTTSProvider CosyVoice TTS提供者
type CosyVoiceTTSProvider struct {
	APIURL        string
	SpeakerID     string
	FrameDuration int
	TargetSR      int
	AudioFormat   string
	InstructText  string
	settingsMu    sync.RWMutex
}

// 响应结构体
type cosyVoiceResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    []byte `json:"data"`
}

// NewCosyVoiceTTSProvider 创建新的CosyVoice TTS提供者

func NewCosyVoiceTTSProvider(config map[string]interface{}) *CosyVoiceTTSProvider {
	helper := confighelper.New(config)

	apiURL := strings.TrimSpace(helper.GetString("api_url", "https://tts.linkerai.top/tts"))
	if apiURL == "" {
		apiURL = "https://tts.linkerai.top/tts"
	}
	speakerID := strings.TrimSpace(helper.GetString("spk_id", "OUeAo1mhq6IBExi"))
	if speakerID == "" {
		speakerID = "OUeAo1mhq6IBExi"
	}
	frameDuration := helper.GetInt("frame_duration", audiofmt.FrameDuration)
	if frameDuration <= 0 {
		frameDuration = audiofmt.FrameDuration
	}
	targetSR := helper.GetInt("target_sr", audiofmt.SampleRate)
	if targetSR <= 0 {
		targetSR = audiofmt.SampleRate
	}
	audioFormat := strings.TrimSpace(helper.GetString("audio_format", "mp3"))
	if audioFormat == "" {
		audioFormat = "mp3"
	}
	instructText := helper.GetString("instruct_text")

	return &CosyVoiceTTSProvider{
		APIURL:        apiURL,
		SpeakerID:     speakerID,
		FrameDuration: frameDuration,
		TargetSR:      targetSR,
		AudioFormat:   audioFormat,
		InstructText:  instructText,
	}
}

// ChangeVoice updates speaker configuration for CosyVoice runtime.
func (p *CosyVoiceTTSProvider) ChangeVoice(ctx context.Context, options *tts.ChangeVoiceOptions) error {
	if options == nil {
		return fmt.Errorf("change voice options cannot be nil")
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
			parsed, err := strconv.Atoi(strings.TrimSpace(v))
			if err == nil {
				return parsed
			}
		}
		return fallback
	}

	speaker := strings.TrimSpace(options.Voice)
	if speaker == "" && options.Params != nil {
		if v, ok := options.Params["spk_id"]; ok {
			speaker = strings.TrimSpace(fmt.Sprintf("%v", v))
		}
		if speaker == "" {
			if v, ok := options.Params["speaker_id"]; ok {
				speaker = strings.TrimSpace(fmt.Sprintf("%v", v))
			}
		}
		if speaker == "" {
			if v, ok := options.Params["voice"]; ok {
				speaker = strings.TrimSpace(fmt.Sprintf("%v", v))
			}
		}
	}
	if speaker == "" {
		speaker = strings.TrimSpace(options.VoiceID)
	}
	if speaker == "" {
		return fmt.Errorf("speaker id cannot be empty for cosyvoice provider")
	}

	p.settingsMu.Lock()
	defer p.settingsMu.Unlock()
	p.SpeakerID = speaker

	if params := options.Params; params != nil {
		if val, ok := params["frame_duration"]; ok {
			p.FrameDuration = parseInt(val, p.FrameDuration)
		}
		if val, ok := params["target_sr"]; ok {
			p.TargetSR = parseInt(val, p.TargetSR)
		}
		if val, ok := params["audio_format"]; ok {
			p.AudioFormat = resolveString(val, p.AudioFormat)
		}
		if val, ok := params["instruct_text"]; ok {
			p.InstructText = resolveString(val, p.InstructText)
		}
		if val, ok := params["api_url"]; ok {
			p.APIURL = resolveString(val, p.APIURL)
		}
	}

	return nil
}

// TextToSpeech 将文本转换为语音，返回音频帧数据和错误
func (p *CosyVoiceTTSProvider) TextToSpeech(ctx context.Context, text string, sampleRate int, channels int, frameDuration int) ([][]byte, error) {
	// 构建查询参数
	params := url.Values{}

	p.settingsMu.RLock()
	speakerID := p.SpeakerID
	frameDur := p.FrameDuration
	targetSR := p.TargetSR
	audioFormat := p.AudioFormat
	instructText := p.InstructText
	apiURL := p.APIURL
	p.settingsMu.RUnlock()

	params.Add("tts_text", text)
	params.Add("spk_id", speakerID)
	params.Add("frame_durition", fmt.Sprintf("%d", frameDur))
	params.Add("stream", "true") // 流式请求
	params.Add("target_sr", fmt.Sprintf("%d", targetSR))
	params.Add("audio_format", audioFormat)
	if instructText != "" {
		params.Add("instruct_text", instructText)
	}

	startTs := time.Now().UnixMilli()

	// 构建完整URL
	requestURL := fmt.Sprintf("%s?%s", apiURL, params.Encode())

	// 创建HTTP请求
	req, err := http.NewRequest("GET", requestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %v", err)
	}

	req.Header.Set("Accept", "application/json")

	// 使用连接池发送请求
	client := getHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("发送请求失败: %v", err)
	}
	defer resp.Body.Close()

	// 读取响应
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %v", err)
	}

	// 检查响应状态码
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API请求失败，状态码: %d, 响应: %s", resp.StatusCode, string(body))
	}

	// 检查响应内容类型和内容长度
	// contentType := resp.Header.Get("Content-Type")
	contentLength := resp.ContentLength

	// 记录响应长度到日志
	log.Debugf("收到TTS响应，Content-Length: %d", contentLength)

	// 判断Content-Length是否合理
	if contentLength == 0 {
		log.Errorf("API返回空响应，Content-Length为0")
		return nil, fmt.Errorf("API返回空响应，Content-Length为0")
	}

	// MP3文件头至少需要100字节才能正常解析
	// -1表示未知长度（例如分块传输）
	if contentLength > 0 && contentLength < 100 {
		log.Errorf("API返回的响应太小无法解析为MP3: %d字节", contentLength)
		return nil, fmt.Errorf("API返回的响应太小无法解析为MP3: %d字节", contentLength)
	}

	// 转换为Opus帧
	if p.AudioFormat == "mp3" {
		// 创建一个管道
		doneChan := make(chan struct{})
		outputChan := make(chan []byte, 1000)

		// 创建MP3解码器
		mp3Decoder, err := audiohelper.CreateAudioDecoderWithSampleRate(ctx, resp.Body, outputChan, frameDuration, p.AudioFormat, sampleRate)
		if err != nil {
			close(doneChan)
			return nil, fmt.Errorf("创建MP3解码器失败: %v", err)
		}
		// 启动解码过程
		go func() {
			if err := mp3Decoder.Run(startTs); err != nil {
				log.Errorf("MP3解码失败: %v", err)
			}
		}()

		// 收集所有的Opus帧
		var opusFrames [][]byte
		for frame := range outputChan {
			opusFrames = append(opusFrames, frame)
		}

		return opusFrames, nil
	}

	return nil, fmt.Errorf("不支持的音频格式: %s", p.AudioFormat)
}

// TextToSpeechStream 流式语音合成实现
func (p *CosyVoiceTTSProvider) TextToSpeechStream(ctx context.Context, text string, sampleRate int, channels int, frameDuration int) (outputChan chan []byte, err error) {
	// 构建查询参数
	params := url.Values{}
	params.Add("tts_text", text)
	params.Add("spk_id", p.SpeakerID)
	params.Add("frame_durition", fmt.Sprintf("%d", frameDuration))
	params.Add("stream", "true") // 流式请求
	params.Add("target_sr", fmt.Sprintf("%d", sampleRate))
	params.Add("audio_format", p.AudioFormat)

	startTs := time.Now().UnixMilli()

	// 构建完整URL
	requestURL := fmt.Sprintf("%s?%s", p.APIURL, params.Encode())

	// 创建HTTP请求
	req, err := http.NewRequest("GET", requestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %v", err)
	}

	req.Header.Set("Accept", "application/json")

	// 使用连接池创建客户端
	client := getHTTPClient()

	// 创建输出通道
	outputChan = make(chan []byte, 100)
	// 启动goroutine处理流式响应
	go func() {
		// 发送请求
		resp, err := client.Do(req)
		if err != nil {
			log.Errorf("发送请求失败: %v", err)
			return
		}
		defer func() {
			resp.Body.Close()
		}()

		// 检查响应状态码
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			log.Errorf("API请求失败，状态码: %d, 响应: %s", resp.StatusCode, string(body))
			return
		}

		// 检查响应内容类型和内容长度
		// contentType := resp.Header.Get("Content-Type")
		contentLength := resp.ContentLength

		// 记录响应长度到日志
		log.Debugf("收到TTS响应，Content-Length: %d", contentLength)

		// 判断Content-Length是否合理
		if contentLength == 0 {
			log.Errorf("API返回空响应，Content-Length为0")
			return
		}

		// MP3文件头至少需要100字节才能正常解析
		// -1表示未知长度（例如分块传输）
		if contentLength > 0 && contentLength < 100 {
			log.Errorf("API返回的响应太小无法解析为MP3: %d字节", contentLength)
			return
		}

		// 根据音频格式处理流式响应
		if p.AudioFormat == "mp3" {
			// 创建 MP3 解码器，传入 context 而不是 done 通道
			mp3Decoder, err := audiohelper.CreateAudioDecoderWithSampleRate(ctx, resp.Body, outputChan, frameDuration, p.AudioFormat, sampleRate)
			if err != nil {
				log.Errorf("创建MP3解码器失败: %v", err)
				close(outputChan)
				return
			}

			// 启动解码过程
			if err := mp3Decoder.Run(startTs); err != nil {
				log.Errorf("MP3解码失败: %v", err)
				return
			}

			select {
			case <-ctx.Done():
				log.Debugf("TTS流式合成取消, 文本: %s", text)
				return
			default:
				log.Infof("tts耗时: 从输入至获取MP3数据结束耗时: %d ms", time.Now().UnixMilli()-startTs)

			}
		} else {
			log.Errorf("当前仅支持MP3格式的流式合成")
		}
	}()

	return outputChan, nil
}
