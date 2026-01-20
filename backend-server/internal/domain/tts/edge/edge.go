package edge

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"backend-server/constants"
	log "backend-server/internal/infrastructure/logger"
	"backend-server/internal/registry/tts"
	"backend-server/pkg/audio"

	"github.com/difyz9/edge-tts-go/pkg/communicate"
)

// init 函数注册Edge TTS提供者
func init() {
	// 注册 Edge TTS 提供者
	tts.Register([]string{"TTS_EdgeTTS", constants.TtsTypeEdge, "default"}, "Microsoft Edge TTS 语音合成服务", func(config map[string]interface{}) (tts.BaseTTSProvider, error) {
		provider := NewEdgeTTSProvider(config)
		return provider, nil
	})
}

// EdgeTTSProvider Edge TTS 提供者
// 支持一次性和流式TTS，输出Opus帧
// 配置参数：voice, rate, volume, pitch, connectTimeout, receiveTimeout
type EdgeTTSProvider struct {
	Voice          string
	Rate           string
	Volume         string
	Pitch          string
	ConnectTimeout int
	ReceiveTimeout int
	settingsMu     sync.RWMutex
}

// NewEdgeTTSProvider 创建EdgeTTSProvider
func NewEdgeTTSProvider(config map[string]interface{}) *EdgeTTSProvider {
	voice, _ := config["voice"].(string)
	rate, _ := config["rate"].(string)
	volume, _ := config["volume"].(string)
	pitch, _ := config["pitch"].(string)
	connectTimeout, _ := config["connect_timeout"].(int)
	receiveTimeout, _ := config["receive_timeout"].(int)
	if rate == "" {
		rate = "+0%"
	}
	if volume == "" {
		volume = "+0%"
	}
	if pitch == "" {
		pitch = "+0Hz"
	}
	if connectTimeout == 0 {
		connectTimeout = 10
	}
	if receiveTimeout == 0 {
		receiveTimeout = 60
	}
	return &EdgeTTSProvider{
		Voice:          voice,
		Rate:           rate,
		Volume:         volume,
		Pitch:          pitch,
		ConnectTimeout: connectTimeout,
		ReceiveTimeout: receiveTimeout,
	}
}

// ChangeVoice updates voice related runtime parameters.
func (p *EdgeTTSProvider) ChangeVoice(ctx context.Context, options *tts.ChangeVoiceOptions) error {
	if options == nil {
		return fmt.Errorf("change voice options cannot be nil")
	}

	voice := strings.TrimSpace(options.Voice)
	if voice == "" && options.Params != nil {
		if v, ok := options.Params["voice"]; ok {
			voice = strings.TrimSpace(fmt.Sprintf("%v", v))
		}
		if voice == "" {
			if v, ok := options.Params["tts_voice"]; ok {
				voice = strings.TrimSpace(fmt.Sprintf("%v", v))
			}
		}
	}
	if voice == "" {
		voice = strings.TrimSpace(options.VoiceID)
	}
	if voice == "" {
		return fmt.Errorf("voice cannot be empty for edge provider")
	}

	updateString := func(current string, candidate interface{}) string {
		if candidate == nil {
			return current
		}
		value := strings.TrimSpace(fmt.Sprintf("%v", candidate))
		if value == "" {
			return current
		}
		return value
	}

	p.settingsMu.Lock()
	defer p.settingsMu.Unlock()
	p.Voice = voice
	if params := options.Params; params != nil {
		if val, ok := params["rate"]; ok {
			p.Rate = updateString(p.Rate, val)
		}
		if val, ok := params["volume"]; ok {
			p.Volume = updateString(p.Volume, val)
		}
		if val, ok := params["pitch"]; ok {
			p.Pitch = updateString(p.Pitch, val)
		}
	}
	return nil
}

// TextToSpeech 一次性合成，返回Opus帧
func (p *EdgeTTSProvider) TextToSpeech(ctx context.Context, text string, sampleRate int, channels int, frameDuration int) ([][]byte, error) {
	startTs := time.Now().UnixMilli()
	// 临时MP3文件
	tmpFile := fmt.Sprintf("/tmp/edge-tts-%d.mp3", time.Now().UnixNano())
	defer os.Remove(tmpFile)

	p.settingsMu.RLock()
	voice := p.Voice
	rate := p.Rate
	volume := p.Volume
	pitch := p.Pitch
	connectTimeout := p.ConnectTimeout
	receiveTimeout := p.ReceiveTimeout
	p.settingsMu.RUnlock()

	comm, err := communicate.NewCommunicate(
		text,
		voice,
		rate,
		volume,
		pitch,
		"", // proxy
		connectTimeout,
		receiveTimeout,
	)
	if err != nil {
		log.Errorf("EdgeTTS Communicate创建失败: %v", err)
		return nil, err
	}
	// 保存MP3
	err = comm.Save(ctx, tmpFile, "")
	if err != nil {
		log.Errorf("EdgeTTS保存MP3失败: %v", err)
		return nil, err
	}
	// MP3转Opus
	f, err := os.Open(tmpFile)
	if err != nil {
		return nil, fmt.Errorf("打开MP3失败: %v", err)
	}
	defer f.Close()
	pipeReader, pipeWriter := io.Pipe()
	outputChan := make(chan []byte, 1000)
	// 写入MP3数据到pipe
	go func() {
		_, _ = io.Copy(pipeWriter, f)
		pipeWriter.Close()
	}()
	mp3Decoder, err := audio.CreateAudioDecoderWithSampleRate(ctx, pipeReader, outputChan, frameDuration, "mp3", sampleRate)
	if err != nil {
		return nil, fmt.Errorf("创建MP3解码器失败: %v", err)
	}
	var opusFrames [][]byte
	done := make(chan struct{})
	go func() {
		for frame := range outputChan {
			opusFrames = append(opusFrames, frame)
		}
		done <- struct{}{}
	}()
	if err := mp3Decoder.Run(startTs); err != nil {
		return nil, fmt.Errorf("MP3解码失败: %v", err)
	}
	<-done
	return opusFrames, nil
}

// TextToSpeechStream 流式合成，返回Opus帧chan
func (p *EdgeTTSProvider) TextToSpeechStream(ctx context.Context, text string, sampleRate int, channels int, frameDuration int) (chan []byte, error) {
	startTs := time.Now().UnixMilli()
	p.settingsMu.RLock()
	voice := p.Voice
	rate := p.Rate
	volume := p.Volume
	pitch := p.Pitch
	connectTimeout := p.ConnectTimeout
	receiveTimeout := p.ReceiveTimeout
	p.settingsMu.RUnlock()
	comm, err := communicate.NewCommunicate(
		text,
		voice,
		rate,
		volume,
		pitch,
		"", // proxy
		connectTimeout,
		receiveTimeout,
	)
	if err != nil {
		log.Errorf("EdgeTTS Communicate创建失败: %v", err)
		return nil, err
	}

	chunkChan, errChan := comm.Stream(ctx)
	outputChan := make(chan []byte, 100)
	pipeReader, pipeWriter := io.Pipe()
	// MP3转Opus解码器
	go func() {
		defer func() {
			pipeWriter.Close()
			log.Debugf("EdgeTTS流式合成结束, 耗时: %d ms", time.Now().UnixMilli()-startTs)
			if err := <-errChan; err != nil {
				log.Errorf("EdgeTTS流式合成出错: %v", err)
			}
		}()
		for {
			select {
			case <-ctx.Done():
				log.Debugf("EdgeTTS Stream context done, exit")
				return
			default:
				select {
				case chunk, ok := <-chunkChan:
					if !ok {
						log.Debugf("EdgeTTS Stream channel closed, exit")
						return
					}
					if chunk.Type == "audio" {
						_, _ = pipeWriter.Write(chunk.Data)
					}
				}
			}
		}

	}()
	// 启动MP3→Opus解码
	go func() {
		mp3Decoder, err := audio.CreateAudioDecoderWithSampleRate(ctx, pipeReader, outputChan, frameDuration, "mp3", sampleRate)
		if err != nil {
			log.Errorf("EdgeTTS MP3解码器创建失败: %v", err)
			return
		}
		if err := mp3Decoder.Run(startTs); err != nil {
			log.Errorf("EdgeTTS MP3解码失败: %v", err)
		}
		log.Debugf("EdgeTTS MP3解码结束, 耗时: %d ms", time.Now().UnixMilli()-startTs)
	}()
	return outputChan, nil
}
