package edge_offline

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"backend-server/constants"
	log "backend-server/internal/infrastructure/logger"
	"backend-server/internal/registry/tts"
	util "backend-server/internal/shared"
	"backend-server/pkg/audio"
	"backend-server/pkg/confighelper"

	"github.com/gopxl/beep"
	"github.com/gorilla/websocket"
)

// init 函数注册Edge Offline TTS提供者
func init() {
	// 注册 Edge Offline TTS 提供者
	tts.Register([]string{constants.TtsTypeEdgeOffline}, "Microsoft Edge 离线 TTS 语音合成服务", func(config map[string]interface{}) (tts.BaseTTSProvider, error) {
		provider := NewEdgeOfflineTTSProvider(config)
		return provider, nil
	})
}

// WebSocket连接配置
type WSConnConfig struct {
	ServerURL        string
	HandshakeTimeout time.Duration
}

// wsConnWrapper WebSocket 连接包装器，实现 util.Resource 接口
type wsConnWrapper struct {
	conn         *websocket.Conn
	lastActiveAt time.Time
	mu           sync.RWMutex
}

// Close 关闭连接，实现 util.Resource 接口
func (w *wsConnWrapper) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.conn != nil {
		return w.conn.Close()
	}
	return nil
}

// IsValid 检查连接是否有效，实现 util.Resource 接口
func (w *wsConnWrapper) IsValid() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.conn != nil && time.Since(w.lastActiveAt) < 30*time.Second
}

// updateLastActive 更新最后活跃时间
func (w *wsConnWrapper) updateLastActive() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.lastActiveAt = time.Now()
}

// getConnection 获取底层连接
func (w *wsConnWrapper) getConnection() *websocket.Conn {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.conn
}

// EdgeOfflineTTSProvider WebSocket TTS 提供者
type EdgeOfflineTTSProvider struct {
	ServerURL string
	Timeout   time.Duration
	pool      *util.ResourcePool
}

var resourcePool *util.ResourcePool
var once sync.Once
var lock sync.RWMutex

// NewEdgeOfflineTTSProvider 创建新的 Edge Offline TTS 提供者

func NewEdgeOfflineTTSProvider(config map[string]interface{}) *EdgeOfflineTTSProvider {
	helper := confighelper.New(config)
	serverURL := strings.TrimSpace(helper.GetString("server_url", "ws://localhost:8080/tts"))
	if serverURL == "" {
		serverURL = "ws://localhost:8080/tts"
	}
	timeoutSeconds := helper.GetInt("timeout", 30)
	if timeoutSeconds <= 0 {
		timeoutSeconds = 30
	}

	if resourcePool == nil {
		lock.Lock()
		defer lock.Unlock()
		if resourcePool == nil {
			// 创建连接池配置
			poolConfig := getPoolConfigFromMap(config)
			if poolConfig == nil {
				poolConfig = util.DefaultConfig()
				// 为TTS设置合适的默认值
				poolConfig.MaxSize = 10
				poolConfig.MinSize = 1
				poolConfig.MaxIdle = 5
				poolConfig.IdleTimeout = 1 * time.Minute
				poolConfig.ValidateOnBorrow = true
				poolConfig.ValidateOnReturn = true
			}

			// 创建WebSocket连接工厂
			wsConfig := WSConnConfig{
				ServerURL:        serverURL,
				HandshakeTimeout: 10 * time.Second,
			}
			factory := NewWebSocketConnFactory(wsConfig)

			// 创建资源池
			var err error
			resourcePool, err = util.NewResourcePool(poolConfig, factory)
			if err != nil {
				log.Errorf("创建WebSocket连接池失败: %v", err)
				return nil
			}
		}
	}

	return &EdgeOfflineTTSProvider{
		ServerURL: serverURL,
		Timeout:   time.Duration(timeoutSeconds) * time.Second,
		pool:      resourcePool,
	}
}

// ChangeVoice is not supported for the offline Edge provider because voice
// selection is handled by the remote synthesis server.
func (p *EdgeOfflineTTSProvider) ChangeVoice(ctx context.Context, options *tts.ChangeVoiceOptions) error {
	return fmt.Errorf("edge offline tts provider does not support runtime voice switching")
}

// getPoolConfigFromMap 从配置映射中获取池配置

func getPoolConfigFromMap(config map[string]interface{}) *util.PoolConfig {
	if len(config) == 0 {
		return nil
	}

	helper := confighelper.New(config)
	poolConfig := util.DefaultConfig()

	if minSize := helper.GetInt("pool_min_size", -1); minSize >= 0 {
		poolConfig.MinSize = minSize
	}
	if maxSize := helper.GetInt("pool_max_size", -1); maxSize >= 0 {
		poolConfig.MaxSize = maxSize
	}
	if maxIdle := helper.GetInt("pool_max_idle", -1); maxIdle >= 0 {
		poolConfig.MaxIdle = maxIdle
	}
	if idleTimeout := helper.GetFloat64("pool_idle_timeout", -1); idleTimeout >= 0 {
		poolConfig.IdleTimeout = time.Duration(idleTimeout * float64(time.Second))
	}
	if acquireTimeout := helper.GetFloat64("pool_acquire_timeout", -1); acquireTimeout >= 0 {
		poolConfig.AcquireTimeout = time.Duration(acquireTimeout * float64(time.Second))
	}

	return poolConfig
}

// getConnection 从连接池获取连接
func (p *EdgeOfflineTTSProvider) getConnection(ctx context.Context) (*wsConnWrapper, error) {
	if p.pool == nil {
		return nil, fmt.Errorf("连接池未初始化")
	}

	resource, err := p.pool.AcquireWithTimeout(p.Timeout)
	if err != nil {
		return nil, fmt.Errorf("从连接池获取连接失败: %v", err)
	}

	wrapper, ok := resource.(*wsConnWrapper)
	if !ok {
		p.pool.Release(resource)
		return nil, fmt.Errorf("无效的资源类型")
	}

	return wrapper, nil
}

// returnConnection 归还连接到连接池
func (p *EdgeOfflineTTSProvider) returnConnection(wrapper *wsConnWrapper) error {
	if p.pool == nil || wrapper == nil {
		return fmt.Errorf("连接池或连接为空")
	}
	return p.pool.Release(wrapper)
}

// removeConnection 从连接池中移除连接
func (p *EdgeOfflineTTSProvider) removeConnection(wrapper *wsConnWrapper) {
	if wrapper != nil {
		wrapper.Close()
	}
}

// Close 关闭资源池
func (p *EdgeOfflineTTSProvider) Close() error {
	if p.pool != nil {
		return p.pool.Close()
	}
	return nil
}

// Stats 获取资源池统计信息
func (p *EdgeOfflineTTSProvider) Stats() map[string]interface{} {
	if p.pool != nil {
		return p.pool.Stats()
	}
	return nil
}

// TextToSpeech 将文本转换为语音，返回音频帧数据
func (p *EdgeOfflineTTSProvider) TextToSpeech(ctx context.Context, text string, sampleRate int, channels int, frameDuration int) ([][]byte, error) {
	var frames [][]byte

	// 获取连接
	wrapper, err := p.getConnection(ctx)
	if err != nil {
		return nil, err
	}

	// 发送文本
	conn := wrapper.getConnection()
	err = conn.WriteMessage(websocket.TextMessage, []byte(text))
	if err != nil {
		p.removeConnection(wrapper)
		return nil, fmt.Errorf("发送文本失败: %v", err)
	}

	// 创建管道用于音频数据传输
	pipeReader, pipeWriter := io.Pipe()
	outputChan := make(chan []byte, 1000)
	startTs := time.Now().UnixMilli()

	// 创建音频解码器
	audioDecoder, err := audio.CreateAudioDecoderWithSampleRate(ctx, pipeReader, outputChan, frameDuration, "mp3", sampleRate)
	if err != nil {
		pipeReader.Close()
		return nil, fmt.Errorf("创建音频解码器失败: %v", err)
	}

	// 启动解码器
	go func() {
		if err := audioDecoder.Run(startTs); err != nil {
			log.Errorf("音频解码失败: %v", err)
		}
	}()

	// 接收WebSocket数据并写入管道
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer pipeWriter.Close()

		for {
			messageType, data, err := conn.ReadMessage()
			if err != nil {
				if websocket.IsCloseError(err, websocket.CloseNormalClosure) {
					return
				}
				log.Errorf("读取WebSocket消息失败: %v", err)
				return
			}

			if messageType == websocket.BinaryMessage {
				if _, err := pipeWriter.Write(data); err != nil {
					log.Errorf("写入音频数据失败: %v", err)
					return
				}
			}
		}
	}()

	// 收集所有的Opus帧
	go func() {
		for frame := range outputChan {
			frames = append(frames, frame)
		}
	}()

	// 等待完成或超时
	select {
	case <-ctx.Done():
		p.returnConnection(wrapper)
		return nil, fmt.Errorf("TTS合成超时或被取消")
	case <-done:
		p.returnConnection(wrapper)
		close(outputChan)
		return frames, nil
	}
}

// TextToSpeechStream 流式语音合成
func (p *EdgeOfflineTTSProvider) TextToSpeechStream(ctx context.Context, text string, sampleRate int, channels int, frameDuration int) (chan []byte, error) {
	outputChan := make(chan []byte, 100)

	go func() {
		// 获取连接
		wrapper, err := p.getConnection(ctx)
		if err != nil {
			log.Errorf("获取WebSocket连接失败: %v", err)
			return
		}
		defer p.returnConnection(wrapper)

		// 发送文本
		conn := wrapper.getConnection()
		err = conn.WriteMessage(websocket.TextMessage, []byte(text))
		if err != nil {
			p.removeConnection(wrapper)
			log.Errorf("发送文本失败: %v", err)
			return
		}

		// 创建管道用于音频数据传输
		pipeReader, pipeWriter := io.Pipe()

		defer func() {
			pipeReader.Close()
			pipeWriter.Close()
		}()

		// 启动解码器
		go func() {
			startTs := time.Now().UnixMilli()
			// 创建音频解码器
			audioDecoder, err := audio.CreateAudioDecoderWithSampleRate(ctx, pipeReader, outputChan, frameDuration, "pcm", sampleRate)
			if err != nil {
				pipeReader.Close()
				log.Errorf("创建音频解码器失败: %v", err)
				return
			}

			audioDecoder.WithFormat(beep.Format{
				SampleRate:  beep.SampleRate(sampleRate),
				NumChannels: channels,
				Precision:   2,
			})

			if err := audioDecoder.Run(startTs); err != nil {
				log.Errorf("音频解码失败: %v", err)
			}
		}()

		// 接收WebSocket数据并写入管道
		for {
			select {
			case <-ctx.Done():
				log.Debugf("TextToSpeechStream context done, exit")
				p.returnConnection(wrapper)
				return
			default:
				messageType, data, err := conn.ReadMessage()
				if err != nil {
					if websocket.IsCloseError(err, websocket.CloseNormalClosure) {
						return
					}
					log.Errorf("读取WebSocket消息失败: %v", err)
					p.removeConnection(wrapper)
					return
				}

				if messageType == websocket.BinaryMessage {
					if _, err := pipeWriter.Write(data); err != nil {
						log.Errorf("写入音频数据失败: %v", err)
						p.removeConnection(wrapper)
						return
					}
					return
				}

			}
		}
	}()

	return outputChan, nil
}
