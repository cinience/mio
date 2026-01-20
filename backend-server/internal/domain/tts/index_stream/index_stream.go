package index_stream

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
	"unicode"

	"backend-server/constants"
	log "backend-server/internal/infrastructure/logger"
	"backend-server/internal/registry/tts"
	"backend-server/pkg/audio"
	"backend-server/pkg/confighelper"
)

// IndexStreamTTSProvider Index Stream TTS 提供者
// 支持流式处理PCM音频数据并编码为Opus格式
type IndexStreamTTSProvider struct {
	APIURL string // TTS API 地址
	Voice  string // 语音角色

	RefAudios string // 少样本音频文件路径
	Seed      int

	AudioFormat string        // 音频格式，默认为pcm
	SampleRate  int           // 采样率，默认24000
	Channels    int           // 声道数，默认1
	Timeout     time.Duration // 请求超时时间

	OutputDir string
	// HTTP 客户端
	client     *http.Client
	settingsMu sync.RWMutex
}

// TTSRequest TTS请求结构体
type TTSRequest struct {
	Text      string `json:"text"`
	Character string `json:"character"`
}

type TTSRefRequest struct {
	Text      string   `json:"text"`
	RefAudios []string `json:"audio_paths"`
	Seed      int      `json:"seed"`
}

// init 函数注册GPT-SoVITS V2 TTS提供者
func init() {
	// 注册 GPT-SoVITS V2 TTS 提供者
	tts.Register([]string{"TTS_IndexStreamTTS", "TTS_GPT_SOVITS_V2", constants.TtsTypeGptSovitsV2}, "GPT-SoVITS V2 TTS 语音合成服务", func(config map[string]interface{}) (tts.BaseTTSProvider, error) {
		provider := NewIndexStreamTTSProvider(config)
		return provider, nil
	})
}

// NewIndexStreamTTSProvider 创建IndexStreamTTSProvider
func NewIndexStreamTTSProvider(config map[string]interface{}) *IndexStreamTTSProvider {
	helper := confighelper.New(config)
	refAudios := helper.GetString("ref_audio")
	voice := helper.GetString("voice", "xiao_he")
	apiURL := helper.GetString("api_url")
	if apiURL == "" {
		apiURL = helper.GetString("url")
	}
	outputDir := helper.GetString("output_dir")
	audioFormat := helper.GetString("audio_format", "wav")
	sampleRate := helper.GetInt("sample_rate", 24000)
	channels := helper.GetInt("channels", 1)
	seed := helper.GetInt("seed", 8)
	timeout := helper.GetInt("timeout", 10)
	privateVoice := helper.GetString("private_voice")
	if privateVoice != "" {
		voice = privateVoice
	}

	log.Debugf("IndexStream TTS API URL: %s config: %v", apiURL, config)

	return &IndexStreamTTSProvider{
		RefAudios:   refAudios,
		Seed:        seed,
		Voice:       voice,
		APIURL:      apiURL,
		AudioFormat: audioFormat,
		SampleRate:  sampleRate,
		Channels:    channels,
		OutputDir:   outputDir,
		Timeout:     time.Duration(timeout) * time.Second,
		client: &http.Client{
			Timeout: time.Duration(timeout) * time.Second,
		},
	}
}

// ChangeVoice updates streaming voice or reference audio parameters for the provider.
func (p *IndexStreamTTSProvider) ChangeVoice(ctx context.Context, options *tts.ChangeVoiceOptions) error {
	if options == nil {
		return fmt.Errorf("change voice options cannot be nil")
	}

	return nil
}

// TextToSpeech 一次性合成，返回Opus帧
func (p *IndexStreamTTSProvider) TextToSpeech(ctx context.Context, text string, sampleRate int, channels int, frameDuration int) ([][]byte, error) {
	startTs := time.Now().UnixMilli()
	log.Infof("IndexStream TTS开始合成文本: %s", text)

	p.settingsMu.RLock()
	voice := p.Voice
	refAudios := p.RefAudios
	seed := p.Seed
	p.settingsMu.RUnlock()

	var payload any
	if refAudios != "" {
		payload = TTSRefRequest{
			Text:      text,
			RefAudios: strings.Split(refAudios, ","),
			Seed:      seed,
		}
	} else {
		payload = TTSRequest{
			Text:      text,
			Character: voice,
		}
	}

	audioFormat, body, err := p.doPost(ctx, payload)
	outputChan := make(chan []byte, 1000)
	decoder, err := audio.CreateAudioDecoderWithSampleRate(ctx, body, outputChan, frameDuration, audioFormat, sampleRate)
	if err != nil {
		return nil, fmt.Errorf("创建解码器失败: %v", err)
	}

	var opusFrames [][]byte
	done := make(chan struct{})
	go func() {
		for frame := range outputChan {
			opusFrames = append(opusFrames, frame)
		}
		done <- struct{}{}
	}()
	if err := decoder.Run(startTs); err != nil {
		return nil, fmt.Errorf("MP3解码失败: %v", err)
	}
	<-done
	return opusFrames, nil
}

// TextToSpeechStream 流式合成，返回Opus帧chan
func (p *IndexStreamTTSProvider) TextToSpeechStream(ctx context.Context, text string, sampleRate int, channels int, frameDuration int) (chan []byte, error) {
	startTs := time.Now().UnixMilli()
	log.Infof("IndexStream TTS开始流式合成文本: %s", text)

	text = replaceEmojiWithSpace(text)

	p.settingsMu.RLock()
	voice := p.Voice
	refAudios := p.RefAudios
	seed := p.Seed
	p.settingsMu.RUnlock()

	var payload any
	if refAudios != "" {
		payload = TTSRefRequest{
			Text:      text,
			RefAudios: strings.Split(refAudios, ","),
			Seed:      seed,
		}
	} else {
		payload = TTSRequest{
			Text:      text,
			Character: voice,
		}
	}

	outputChan := make(chan []byte, 1000)

	/*
		pipeReader, pipeWriter := io.Pipe()
		// 启动音频解码
		go func() {
			defer func() {
				pipeReader.Close()
				pipeWriter.Close()
			}()
			// 创建音频解码器，根据检测到的格式设置
		audioDecoder, err := audio.CreateAudioDecoderWithSampleRate(ctx, pipeReader, outputChan, frameDuration, "wav", sampleRate)
			if err != nil {
				log.Errorf("IndexStream 音频解码器创建失败: %v", err)
				close(outputChan)
				return
			}

			if err := audioDecoder.Run(startTs); err != nil {
				log.Errorf("IndexStream 音频解码失败: %v", err)
			}

			log.Debugf("IndexStream 音频解码结束, 耗时: %d ms", time.Now().UnixMilli()-startTs)
		}()
	*/

	go func() {
		audioFormat, body, err := p.doPost(ctx, payload)
		if err != nil {
			log.Errorf("TTS请求失败: %v", err)
			return
		}
		if audioFormat == "" {
			audioFormat = "wav"
		}
		data, err := io.ReadAll(body)
		if err != nil {
			log.Errorf("读取TTS响应失败: %v", err)
		}
		body.Close()

		//pipeWriter.Write(data)
		defer func() {
			p.saveAudio(data, text)
		}()
		// 创建音频解码器，根据检测到的格式设置
		audioDecoder, err := audio.CreateAudioDecoderWithSampleRate(ctx, io.NopCloser(bytes.NewReader(data)), outputChan, frameDuration, audioFormat, sampleRate)
		if err != nil {
			log.Errorf("IndexStream 音频解码器创建失败: %v", err)
			close(outputChan)
			return
		}

		if err := audioDecoder.Run(startTs); err != nil {
			log.Errorf("IndexStream 音频解码失败: %v", err)
		}

		log.Debugf("IndexStream 音频解码结束, 耗时: %d ms", time.Now().UnixMilli()-startTs)
	}()

	return outputChan, nil
}

func (p *IndexStreamTTSProvider) saveAudio(data []byte, text string) error {
	if p.OutputDir == "" || p.OutputDir == "tmp/" {
		return nil
	}

	audioFile := fmt.Sprintf("%s/%s.wav", p.OutputDir, text)
	err := os.MkdirAll(p.OutputDir, 0755)
	if err != nil {
		return err
	}
	err = os.WriteFile(audioFile, data, 0644)
	if err != nil {
		return err
	}

	log.Debugf("IndexStream TTS保存音频文件: %s", audioFile)
	return nil
}

func (p *IndexStreamTTSProvider) doPost(ctx context.Context, payload any) (string, io.ReadCloser, error) {
	start := time.Now()
	p.settingsMu.RLock()
	apiURL := p.APIURL
	client := p.client
	p.settingsMu.RUnlock()
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return "", nil, fmt.Errorf("序列化请求失败: %v", err)
	}

	// 创建HTTP请求
	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", nil, fmt.Errorf("创建请求失败: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	tryCount := 0
	var resp *http.Response
	for {
		resp, err = client.Do(req)
		if err == nil {
			break
		}
		if tryCount > 3 {
			log.Errorf("TTS请求失败超过3次，退出: %v", err)
			return "", nil, err
		}
		tryCount++
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return "", nil, fmt.Errorf("TTS请求[%s]失败，参数[%s], 状态码[%d], 返回内容：%s", apiURL, string(jsonData), resp.StatusCode, string(body))
	}

	audioFormat := audio.GetAudioFormatByMimeType(resp.Header.Get("Content-Type"))

	log.Debugf("TTS请求URL:[%s], 参数:[%s] 音频格式:%s 耗时: %d ms", apiURL, string(jsonData), audioFormat, time.Now().UnixMilli()-start.UnixMilli())

	return audioFormat, resp.Body, nil
}

func (p *IndexStreamTTSProvider) doLocal(ctx context.Context, payload any) (string, io.ReadCloser, error) {
	f, err := os.Open("2025-08-26T142659.200.wav")
	return "wav", f, err
}

// isEmoji 检查一个 rune 是否是 emoji 或 emoji 的一部分。
func isEmoji(r rune) bool {
	// 使用更通用的方法检测emoji，因为unicode包中没有直接的Emoji相关常量
	// 检查是否是emoji表现形式或在常见emoji范围内
	return unicode.Is(unicode.Sk, r) || unicode.Is(unicode.So, r) ||
		(r >= 0x1F300 && r <= 0x1F6FF) || // 杂项符号和表情符号
		(r >= 0x1F700 && r <= 0x1F77F) || // 炼金术符号
		(r >= 0x1F780 && r <= 0x1F7FF) || // 几何图形扩展
		(r >= 0x1F800 && r <= 0x1F8FF) || // 补充箭头-C
		(r >= 0x1F900 && r <= 0x1F9FF) || // 补充符号和象形文字
		(r >= 0x1FA00 && r <= 0x1FA6F) || // 象形文字扩展
		(r >= 0x1FA70 && r <= 0x1FAFF) || // 象形文字扩展-B
		(r >= 0x2600 && r <= 0x26FF) || // 杂项符号
		(r >= 0x2700 && r <= 0x27BF) // 装饰符号
}

// replaceEmojiWithSpace 将字符串中所有的 emoji 字符替换为空格。
func replaceEmojiWithSpace(s string) string {
	var builder strings.Builder
	builder.Grow(len(s)) // 预分配容量以提高性能

	for _, r := range s {
		if isEmoji(r) {
			builder.WriteRune('.') // 如果是 emoji，替换为空格
		} else {
			builder.WriteRune(r) // 否则保留原字符
		}
	}

	return builder.String()
}
