package doubao

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"backend-server/constants"
	log "backend-server/internal/infrastructure/logger"
	"backend-server/internal/registry/tts"
	"backend-server/pkg/audio"
)

// init 函数注册Doubao TTS提供者
func init() {
	// 注册 Doubao TTS 提供者
	tts.Register([]string{"TTS_DoubaoTTS", constants.TtsTypeDoubao}, "Doubao TTS 语音合成服务", func(config map[string]interface{}) (tts.BaseTTSProvider, error) {
		provider := NewDoubaoTTSProvider(config)
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

// DoubaoTTSProvider 读伴TTS提供者
type DoubaoTTSProvider struct {
	AppID         string
	AccessToken   string
	Cluster       string
	Voice         string
	APIURL        string
	Authorization string
	Header        map[string]string
	voiceMu       sync.RWMutex
}

// 请求结构体
type doubaoRequest struct {
	App     appInfo     `json:"app"`
	User    userInfo    `json:"user"`
	Audio   audioInfo   `json:"audio"`
	Request requestInfo `json:"request"`
}

type appInfo struct {
	AppID   string `json:"appid"`
	Token   string `json:"token"`
	Cluster string `json:"cluster"`
}

type userInfo struct {
	UID string `json:"uid"`
}

type audioInfo struct {
	VoiceType   string  `json:"voice_type"`
	Encoding    string  `json:"encoding"`
	Rate        int     `json:"rate"`
	SpeedRatio  float64 `json:"speed_ratio"`
	VolumeRatio float64 `json:"volume_ratio"`
	PitchRatio  float64 `json:"pitch_ratio"`
}

type requestInfo struct {
	ReqID        string `json:"reqid"`
	Text         string `json:"text"`
	TextType     string `json:"text_type"`
	Operation    string `json:"operation"`
	WithFrontend int    `json:"with_frontend"`
	FrontendType string `json:"frontend_type"`
}

// 响应结构体
type doubaoResponse struct {
	Data string `json:"data"`
}

// 生成UUID
func generateUUID() string {
	b := make([]byte, 16)
	_, err := rand.Read(b)
	if err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// NewDoubaoTTSProvider 创建新的读伴TTS提供者
func NewDoubaoTTSProvider(config map[string]interface{}) *DoubaoTTSProvider {
	appID, _ := config["appid"].(string)
	accessToken, _ := config["access_token"].(string)
	cluster, _ := config["cluster"].(string)
	voice, _ := config["voice"].(string)
	apiURL, _ := config["api_url"].(string)
	authorization, _ := config["authorization"].(string)

	// 检查令牌
	if accessToken == "" {
		log.Error("TTS 访问令牌不能为空")
	}

	return &DoubaoTTSProvider{
		AppID:         appID,
		AccessToken:   accessToken,
		Cluster:       cluster,
		Voice:         voice,
		APIURL:        apiURL,
		Authorization: authorization,
		Header:        map[string]string{"Authorization": fmt.Sprintf("%s%s", authorization, accessToken)},
	}
}

// ChangeVoice updates the runtime voice token for subsequent synthesis calls.
func (p *DoubaoTTSProvider) ChangeVoice(ctx context.Context, options *tts.ChangeVoiceOptions) error {
	if options == nil {
		return fmt.Errorf("change voice options cannot be nil")
	}

	resolveVoice := func() string {
		voice := strings.TrimSpace(options.Voice)
		if voice != "" {
			return voice
		}
		if params := options.Params; params != nil {
			if v, ok := params["voice"]; ok {
				if str := strings.TrimSpace(fmt.Sprintf("%v", v)); str != "" {
					return str
				}
			}
			if v, ok := params["tts_voice"]; ok {
				if str := strings.TrimSpace(fmt.Sprintf("%v", v)); str != "" {
					return str
				}
			}
		}
		return strings.TrimSpace(options.VoiceID)
	}

	newVoice := resolveVoice()
	if newVoice == "" {
		return fmt.Errorf("voice cannot be empty for doubao provider")
	}

	p.voiceMu.Lock()
	defer p.voiceMu.Unlock()
	p.Voice = newVoice
	return nil
}

// TextToSpeech 将文本转换为语音，返回音频帧数据和错误
func (p *DoubaoTTSProvider) TextToSpeech(ctx context.Context, text string, sampleRate int, channels int, frameDuration int) ([][]byte, error) {
	// 准备请求数据
	p.voiceMu.RLock()
	voice := p.Voice
	p.voiceMu.RUnlock()
	reqData := doubaoRequest{
		App: appInfo{
			AppID:   p.AppID,
			Token:   p.AccessToken,
			Cluster: p.Cluster,
		},
		User: userInfo{
			UID: "1",
		},
		Audio: audioInfo{
			VoiceType:   voice,
			Encoding:    "wav",
			Rate:        sampleRate,
			SpeedRatio:  1.0,
			VolumeRatio: 1.0,
			PitchRatio:  1.0,
		},
		Request: requestInfo{
			ReqID:        generateUUID(),
			Text:         text,
			TextType:     "plain",
			Operation:    "query",
			WithFrontend: 1,
			FrontendType: "unitTson",
		},
	}

	// 转换为JSON
	jsonData, err := json.Marshal(reqData)
	if err != nil {
		return nil, fmt.Errorf("无法序列化请求: %v", err)
	}

	// 创建HTTP请求
	req, err := http.NewRequest("POST", p.APIURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %v", err)
	}

	// 设置请求头
	for k, v := range p.Header {
		req.Header.Set(k, v)
	}
	req.Header.Set("Content-Type", "application/json")

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

	// 解析响应
	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("解析响应失败: %v", err)
	}

	// 提取音频数据
	if audioData, ok := result["data"].(string); ok {
		// 获取原始响应数据
		wavData, err := base64.StdEncoding.DecodeString(audioData)
		if err != nil {
			return nil, fmt.Errorf("解码音频数据失败: %v", err)
		}

		// 转换为Opus帧并直接返回
		return audio.WavToOpus(wavData, 0, 0, 0)
	}

	return nil, fmt.Errorf("响应中没有数据字段, 状态码: %d, 响应: %s", resp.StatusCode, string(body))
}

// GetVoiceInfo 获取语音信息
func (p *DoubaoTTSProvider) GetVoiceInfo() map[string]interface{} {
	p.voiceMu.RLock()
	voice := p.Voice
	p.voiceMu.RUnlock()
	return map[string]interface{}{
		"voice": voice,
		"type":  "doubao",
	}
}

// saveWavToTmp 将WAV数据保存到tmp目录
func saveWavToTmp(wavData []byte) error {
	// 确保tmp目录存在
	tmpDir := "tmp"
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return fmt.Errorf("创建tmp目录失败: %v", err)
	}

	// 生成唯一文件名
	timestamp := time.Now().Format("20060102_150405")
	uuid := generateUUID()
	filename := filepath.Join(tmpDir, fmt.Sprintf("wav_%s_%s.wav", timestamp, uuid[:8]))

	// 写入文件
	if err := os.WriteFile(filename, wavData, 0644); err != nil {
		return fmt.Errorf("写入WAV文件失败: %v", err)
	}

	log.Infof("WAV文件已保存: %s", filename)
	return nil
}

func (p *DoubaoTTSProvider) TextToSpeechStream(ctx context.Context, text string, sampleRate int, channels int, frameDuration int) (outputChan chan []byte, err error) {
	return nil, nil
}
