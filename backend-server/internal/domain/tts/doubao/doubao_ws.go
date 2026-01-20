package doubao

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"backend-server/constants"
	log "backend-server/internal/infrastructure/logger"
	"backend-server/internal/registry/tts"
	"backend-server/pkg/audio"
	"backend-server/pkg/confighelper"

	"github.com/gorilla/websocket"
)

// init 函数注册Doubao WebSocket TTS提供者
func init() {
	// 注册 Doubao WebSocket TTS 提供者
	tts.Register([]string{constants.TtsTypeDoubaoWS}, "Doubao WebSocket TTS 语音合成服务", func(config map[string]interface{}) (tts.BaseTTSProvider, error) {
		return NewDoubaoWSProvider(config)
	})
}

// 枚举消息类型
var (
	enumMessageType = map[byte]string{
		11: "audio-only server response",
		12: "frontend server response",
		15: "error message from server",
	}
	enumMessageTypeSpecificFlags = map[byte]string{
		0: "no sequence number",
		1: "sequence number > 0",
		2: "last message from server (seq < 0)",
		3: "sequence number < 0",
	}
	enumMessageSerializationMethods = map[byte]string{
		0:  "no serialization",
		1:  "JSON",
		15: "custom type",
	}
	enumMessageCompression = map[byte]string{
		0:  "no compression",
		1:  "gzip",
		15: "custom compression method",
	}
)

// 常量定义
const (
	optQuery   string = "query"  // 一次性合成
	optSubmit  string = "submit" // 流式合成
	wsScheme   string = "wss"
	wsPath     string = "/api/v1/tts/ws_binary"
	headerSize int    = 4 // 默认头部大小(字节)

	doubaoMaxRetries  = 3
	doubaoBaseBackoff = 250 * time.Millisecond
)

// 默认二进制协议头
// version: b0001 (4 bits)
// header size: b0001 (4 bits)
// message type: b0001 (Full client request) (4bits)
// message type specific flags: b0000 (none) (4bits)
// message serialization method: b0001 (JSON) (4 bits)
// message compression: b0001 (gzip) (4bits)
// reserved data: 0x00 (1 byte)
var defaultHeader = []byte{0x11, 0x10, 0x11, 0x00}

// 全局WebSocket客户端连接池
var (
	wsConnPools     = make(map[string]*WSConnPool)
	wsConnPoolsLock sync.RWMutex
	// 全局WebSocket Dialer，配置更大的缓冲区以避免slice bounds out of range错误
	wsDialer = websocket.Dialer{
		ReadBufferSize:   16384, // 16KB 读取缓冲区
		WriteBufferSize:  16384, // 16KB 写入缓冲区
		HandshakeTimeout: 45 * time.Second,
	}
)

// WSConnWrapper WebSocket连接包装器，带有最后活跃时间
type WSConnWrapper struct {
	Conn         *websocket.Conn
	LastActiveAt time.Time
	InUse        bool // 标记连接是否正在使用
}

// WSConnPool WebSocket连接池
type WSConnPool struct {
	connections []*WSConnWrapper
	maxConns    int
	minConns    int
	lock        sync.Mutex
	wsURL       *url.URL
	header      http.Header
	maxIdleTime time.Duration
}

// WSConnPoolOptions 连接池配置
type WSConnPoolOptions struct {
	MaxConns    int
	MinConns    int
	MaxIdleTime time.Duration
}

// NewWSConnPool 创建新的WebSocket连接池
func NewWSConnPool(wsURL *url.URL, header http.Header, opts WSConnPoolOptions) *WSConnPool {
	if opts.MaxConns <= 0 {
		opts.MaxConns = 200 // 默认最大连接数
	}
	if opts.MinConns < 0 {
		opts.MinConns = 0 // 默认最小连接数
	}
	if opts.MinConns > opts.MaxConns {
		opts.MinConns = opts.MaxConns
	}
	if opts.MaxIdleTime < 0 {
		opts.MaxIdleTime = 0
	}

	return &WSConnPool{
		connections: make([]*WSConnWrapper, 0, opts.MaxConns),
		maxConns:    opts.MaxConns,
		minConns:    opts.MinConns,
		wsURL:       wsURL,
		header:      header,
		maxIdleTime: opts.MaxIdleTime,
	}
}

// UpdateOptions 更新连接池配置
func (pool *WSConnPool) UpdateOptions(opts WSConnPoolOptions) {
	pool.lock.Lock()
	defer pool.lock.Unlock()

	if opts.MaxConns <= 0 {
		opts.MaxConns = pool.maxConns
		if opts.MaxConns <= 0 {
			opts.MaxConns = 200
		}
	}
	if opts.MinConns < 0 {
		opts.MinConns = 0
	}
	if opts.MinConns > opts.MaxConns {
		opts.MinConns = opts.MaxConns
	}
	if opts.MaxIdleTime < 0 {
		opts.MaxIdleTime = 0
	}

	pool.maxConns = opts.MaxConns
	pool.minConns = opts.MinConns
	pool.maxIdleTime = opts.MaxIdleTime

	pool.pruneExcessLocked()
	if err := pool.ensureMinConnectionsLocked(); err != nil {
		log.Warnf("预热豆包TTS连接池失败: %v", err)
	}
}

// PreWarm 主动拉起最小连接数
func (pool *WSConnPool) PreWarm() error {
	pool.lock.Lock()
	defer pool.lock.Unlock()

	return pool.ensureMinConnectionsLocked()
}

// ensureMinConnectionsLocked 确保最小连接数（需持有锁）
func (pool *WSConnPool) ensureMinConnectionsLocked() error {
	if pool.minConns <= 0 {
		return nil
	}

	var firstErr error
	for len(pool.connections) < pool.minConns && len(pool.connections) < pool.maxConns {
		conn, err := pool.createConnection()
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			break
		}
		wrapper := &WSConnWrapper{
			Conn:         conn,
			LastActiveAt: time.Now(),
			InUse:        false,
		}
		pool.connections = append(pool.connections, wrapper)
	}

	return firstErr
}

// pruneExcessLocked 收缩多余连接（需持有锁）
func (pool *WSConnPool) pruneExcessLocked() {
	if pool.maxConns <= 0 || len(pool.connections) <= pool.maxConns {
		return
	}

	kept := pool.connections[:0]
	excess := len(pool.connections) - pool.maxConns
	for _, wrapper := range pool.connections {
		if excess > 0 && !wrapper.InUse {
			wrapper.Conn.Close()
			excess--
			continue
		}
		kept = append(kept, wrapper)
	}

	pool.connections = kept

	if len(pool.connections) > pool.maxConns {
		log.Warnf("豆包TTS连接池当前连接数(%d)超过限制(%d)", len(pool.connections), pool.maxConns)
	}
}

// Get 从连接池获取一个可用连接
func (pool *WSConnPool) Get() (*websocket.Conn, error) {
	pool.lock.Lock()
	defer pool.lock.Unlock()

	// 清理过期连接
	pool.cleanup()

	if err := pool.ensureMinConnectionsLocked(); err != nil {
		log.Warnf("豆包TTS连接池补充最小连接失败: %v", err)
	}

	// 寻找空闲连接
	for _, wrapper := range pool.connections {
		if !wrapper.InUse && pool.isValidConnection(wrapper) {
			// 标记为使用中
			wrapper.InUse = true
			wrapper.LastActiveAt = time.Now()
			log.Debugf("复用连接池中的空闲连接，当前连接数: %d", len(pool.connections))
			return wrapper.Conn, nil
		}
	}

	// 如果没有空闲连接且未达到最大连接数，创建新连接
	if len(pool.connections) < pool.maxConns {
		conn, err := pool.createConnection()
		if err != nil {
			return nil, fmt.Errorf("创建新连接失败: %v", err)
		}

		wrapper := &WSConnWrapper{
			Conn:         conn,
			LastActiveAt: time.Now(),
			InUse:        true,
		}
		pool.connections = append(pool.connections, wrapper)
		log.Debugf("创建新的WebSocket连接，当前连接数: %d/%d", len(pool.connections), pool.maxConns)
		return conn, nil
	}

	return nil, fmt.Errorf("连接池已满，无法获取连接")
}

// Put 将连接归还到连接池
func (pool *WSConnPool) Put(conn *websocket.Conn) {
	pool.lock.Lock()
	defer pool.lock.Unlock()

	// 找到对应的连接包装器并标记为空闲
	for _, wrapper := range pool.connections {
		if wrapper.Conn == conn {
			wrapper.InUse = false
			wrapper.LastActiveAt = time.Now()
			// 重置读取截止时间，避免复用时立即超时
			if err := conn.SetReadDeadline(time.Time{}); err != nil {
				log.Debugf("重置豆包TTS连接读取截止时间失败: %v", err)
			}
			log.Debugf("连接已归还到连接池")
			return
		}
	}
}

// createConnection 创建新的WebSocket连接
func (pool *WSConnPool) createConnection() (*websocket.Conn, error) {
	conn, _, err := wsDialer.Dial(pool.wsURL.String(), pool.header)
	if err != nil {
		return nil, err
	}

	// 设置消息读取限制，防止过大的消息
	conn.SetReadLimit(1024 * 1024) // 1MB 最大消息大小

	// 设置保持连接
	conn.SetPingHandler(func(appData string) error {
		return conn.WriteControl(websocket.PongMessage, []byte(appData), time.Now().Add(1*time.Second))
	})

	return conn, nil
}

// isValidConnection 检查连接是否有效
func (pool *WSConnPool) isValidConnection(wrapper *WSConnWrapper) bool {
	// 检查连接是否超时
	if pool.maxIdleTime > 0 && time.Since(wrapper.LastActiveAt) > pool.maxIdleTime {
		return false
	}

	// 发送ping检查连接是否有效
	err := wrapper.Conn.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(1*time.Second))
	return err == nil
}

// cleanup 清理过期或无效的连接
func (pool *WSConnPool) cleanup() {
	validConnections := make([]*WSConnWrapper, 0, len(pool.connections))

	for _, wrapper := range pool.connections {
		if wrapper.InUse {
			// 正在使用的连接保留
			validConnections = append(validConnections, wrapper)
		} else if pool.isValidConnection(wrapper) {
			// 空闲且有效的连接保留
			validConnections = append(validConnections, wrapper)
		} else {
			// 关闭无效连接
			wrapper.Conn.Close()
			log.Debugf("清理无效连接")
		}
	}

	pool.connections = validConnections
}

// Close 关闭连接池中的所有连接
func (pool *WSConnPool) Close() {
	pool.lock.Lock()
	defer pool.lock.Unlock()

	for _, wrapper := range pool.connections {
		wrapper.Conn.Close()
	}
	pool.connections = pool.connections[:0]
	log.Debugf("连接池已关闭")
}

// 合成响应结构
type synResp struct {
	Audio  []byte
	IsLast bool
}

type doubaoServerError struct {
	HTTPCode    int
	BackendCode int
	Message     string
	RequestID   string
}

func (e *doubaoServerError) Error() string {
	return fmt.Sprintf("doubao server error (http=%d backend=%d req=%s): %s", e.HTTPCode, e.BackendCode, e.RequestID, e.Message)
}

// DoubaoWSProvider 读伴WebSocket TTS提供者
type DoubaoWSProvider struct {
	AppID       string
	AccessToken string
	Cluster     string
	Voice       string
	WSHost      string
	WSURL       *url.URL
	Header      http.Header

	pitchRatio  float64
	volumeRatio float64
	speedRatio  float64

	UseStream bool // 是否使用流式合成
	// 音频片段处理回调函数，仅在流式模式下使用
	OnAudioChunk func(chunkData []byte, isLast bool) error

	poolMaxConns int
	poolMinConns int
	poolMaxIdle  time.Duration
	poolWarmup   bool

	settingsMu sync.RWMutex
}

// NewDoubaoWSProvider 创建新的读伴WebSocket TTS提供者
func NewDoubaoWSProvider(config map[string]interface{}) (*DoubaoWSProvider, error) {
	helper := confighelper.New(config)

	validator := confighelper.NewValidator()
	validator.RequireString(helper, "appid", "应用ID")
	validator.RequireString(helper, "access_token", "访问令牌")
	if validator.HasErrors() {
		return nil, validator.GetError()
	}

	appID := helper.GetString("appid")
	accessToken := helper.GetString("access_token")
	cluster := helper.GetString("cluster")
	voice := helper.GetString("voice")
	wsHost := helper.GetString("ws_host", "openspeech.bytedance.com")
	useStream := helper.GetBool("use_stream", true)
	pitchRatio := helper.GetFloat64("pitch_ratio", 1.0)
	volumeRatio := helper.GetFloat64("volume_ratio", 1.0)
	speedRatio := helper.GetFloat64("speed_ratio", 1.0)
	if volumeRatio < 0.1 {
		volumeRatio = 1.0
	}
	if pitchRatio < 0.1 {
		pitchRatio = 1.0
	}
	if speedRatio < 0.1 {
		speedRatio = 1.0
	}
	maxConnections := helper.GetInt("max_connections", 300)
	if maxConnections <= 0 {
		maxConnections = 300
	}
	minConnections := helper.GetInt("min_connections", 1)
	if minConnections < 0 {
		minConnections = 0
	}
	if minConnections > maxConnections {
		minConnections = maxConnections
	}
	maxIdleSeconds := helper.GetInt("max_idle_seconds", 900)
	if maxIdleSeconds < 0 {
		maxIdleSeconds = 0
	}
	warmup := helper.GetBool("warmup", true)

	poolMaxIdle := time.Duration(maxIdleSeconds) * time.Second

	if privateVoice := helper.GetString("private_voice"); privateVoice != "" {
		voice = privateVoice
	}

	// 构建WebSocket URL
	wsURL := url.URL{Scheme: wsScheme, Host: wsHost, Path: wsPath}

	// 检查令牌
	if accessToken == "" {
		log.Error("TTS WebSocket 访问令牌不能为空")
	}

	// 创建HTTP头
	header := http.Header{}
	header.Add("Authorization", fmt.Sprintf("Bearer;%s", accessToken))

	provider := &DoubaoWSProvider{
		AppID:        appID,
		AccessToken:  accessToken,
		Cluster:      cluster,
		Voice:        voice,
		WSHost:       wsHost,
		WSURL:        &wsURL,
		Header:       header,
		UseStream:    useStream,
		pitchRatio:   pitchRatio,
		volumeRatio:  volumeRatio,
		speedRatio:   speedRatio,
		poolMaxConns: maxConnections,
		poolMinConns: minConnections,
		poolMaxIdle:  poolMaxIdle,
		poolWarmup:   warmup,
	}

	if provider.poolWarmup && provider.poolMinConns > 0 {
		go provider.preWarmConnections()
	}

	return provider, nil
}

// ChangeVoice updates the active voice and optional modifiers for new sessions.
func (p *DoubaoWSProvider) ChangeVoice(ctx context.Context, options *tts.ChangeVoiceOptions) error {
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
		return fmt.Errorf("voice cannot be empty for doubao websocket provider")
	}

	p.settingsMu.Lock()
	defer p.settingsMu.Unlock()
	p.Voice = newVoice

	return nil
}

func (p *DoubaoWSProvider) TextToSpeech(ctx context.Context, text string, sampleRate int, channels int, frameDuration int) ([][]byte, error) {
	return nil, nil
}

// TextToSpeechStream 将文本转换为语音，返回音频帧数据和错误
func (p *DoubaoWSProvider) TextToSpeechStream(ctx context.Context, text string, sampleRate int, channels int, frameDuration int) (chan []byte, error) {
	outputOpusChan := make(chan []byte, 1000)

	go func() {
		defer close(outputOpusChan)

		for attempt := 0; attempt < doubaoMaxRetries; attempt++ {
			err := p.streamOnce(ctx, text, sampleRate, channels, frameDuration, outputOpusChan)
			if err == nil {
				return
			}

			if ctx.Err() != nil {
				log.Debugf("DoubaoWs TextToSpeechStream canceled: %v", ctx.Err())
				return
			}

			var serverErr *doubaoServerError
			if errors.As(err, &serverErr) && serverErr.HTTPCode == http.StatusForbidden {
				backoff := doubaoBaseBackoff * time.Duration(1<<attempt)
				log.Warnf("DoubaoWs 请求资源未授予，%d/%d 次重试，等待 %v: %v", attempt+1, doubaoMaxRetries, backoff, serverErr)
				select {
				case <-ctx.Done():
					return
				case <-time.After(backoff):
				}
				continue
			}

			log.Errorf("DoubaoWs TextToSpeechStream 失败: %v", err)
			return
		}

		log.Errorf("DoubaoWs TextToSpeechStream 在 %d 次重试后仍未成功", doubaoMaxRetries)
	}()

	return outputOpusChan, nil
}

func (p *DoubaoWSProvider) streamOnce(ctx context.Context, text string, sampleRate int, channels int, frameDuration int, outputOpusChan chan []byte) error {
	var operation string
	if p.UseStream {
		operation = optSubmit
	} else {
		operation = optQuery
	}

	p.settingsMu.RLock()
	voiceType := p.Voice
	p.settingsMu.RUnlock()
	input := p.setupInput(text, voiceType, operation, sampleRate, channels, frameDuration)

	conn, err := p.getWSConnection()
	if err != nil {
		return fmt.Errorf("获取WebSocket连接失败: %w", err)
	}

	compressedInput := gzipCompress(input)
	payloadSize := len(compressedInput)

	payloadArr := make([]byte, 4)
	binary.BigEndian.PutUint32(payloadArr, uint32(payloadSize))

	clientRequest := make([]byte, len(defaultHeader))
	copy(clientRequest, defaultHeader)
	clientRequest = append(clientRequest, payloadArr...)
	clientRequest = append(clientRequest, compressedInput...)

	if err := conn.WriteMessage(websocket.BinaryMessage, clientRequest); err != nil {
		p.removeWSConnection(conn)
		return fmt.Errorf("发送WebSocket消息失败: %w", err)
	}

	pipeReader, pipeWriter := io.Pipe()

	decoderOutput := make(chan []byte, cap(outputOpusChan))
	decoder, err := audio.CreateAudioDecoderWithSampleRate(ctx, pipeReader, decoderOutput, frameDuration, "mp3", sampleRate)
	if err != nil {
		pipeReader.Close()
		pipeWriter.CloseWithError(err)
		p.removeWSConnection(conn)
		return fmt.Errorf("创建MP3解码器失败: %w", err)
	}

	forwardDone := make(chan struct{})
	go func() {
		defer close(forwardDone)
		for frame := range decoderOutput {
			outputOpusChan <- frame
		}
	}()

	startTs := time.Now().UnixMilli()

	decoderErrCh := make(chan error, 1)
	go func() {
		decoderErrCh <- decoder.Run(startTs)
	}()

	readErrCh := make(chan error, 1)
	go func() {
		var closeErr error
		var returned bool

		defer func() {
			if closeErr != nil {
				if err := pipeWriter.CloseWithError(closeErr); err != nil {
					log.Debugf("关闭音频写入管道失败: %v", err)
				}
				if !returned {
					p.removeWSConnection(conn)
				}
			} else {
				if err := pipeWriter.Close(); err != nil {
					log.Debugf("关闭音频写入管道失败: %v", err)
				}
				if !returned {
					p.returnWSConnection(conn)
					returned = true
				}
			}
			readErrCh <- closeErr
		}()

		conn.SetReadDeadline(time.Now().Add(10 * time.Second))
		chunkCount := 0

		for {
			select {
			case <-ctx.Done():
				log.Debugf("DoubaoWs streamOnce context canceled")
				closeErr = ctx.Err()
				return
			default:
			}

			_, message, err := conn.ReadMessage()
			if err != nil {
				closeErr = err
				return
			}

			resp, err := parseResponse(message)
			if err != nil {
				closeErr = err
				return
			}

			if len(resp.Audio) > 0 {
				chunkCount++
				if _, err := pipeWriter.Write(resp.Audio); err != nil {
					closeErr = err
					return
				}
			}

			if resp.IsLast {
				log.Debugf("收到最后一个音频片段，共%d个片段", chunkCount)
				p.returnWSConnection(conn)
				returned = true
				return
			}

			conn.SetReadDeadline(time.Now().Add(10 * time.Second))
		}
	}()

	readErr := <-readErrCh
	decoderErr := <-decoderErrCh

	<-forwardDone

	if decoderErr != nil && !errors.Is(decoderErr, context.Canceled) {
		log.Debugf("MP3解码器退出: %v", decoderErr)
	}

	if readErr != nil {
		return readErr
	}

	return nil
}

// GetVoiceInfo 获取语音信息
func (p *DoubaoWSProvider) GetVoiceInfo() map[string]interface{} {
	p.settingsMu.RLock()
	voice := p.Voice
	p.settingsMu.RUnlock()
	return map[string]interface{}{
		"voice": voice,
		"type":  "doubao_ws",
	}
}

func (p *DoubaoWSProvider) poolKey() string {
	return fmt.Sprintf("%s_%s", p.WSHost, p.AccessToken)
}

func (p *DoubaoWSProvider) poolOptions() WSConnPoolOptions {
	return WSConnPoolOptions{
		MaxConns:    p.poolMaxConns,
		MinConns:    p.poolMinConns,
		MaxIdleTime: p.poolMaxIdle,
	}
}

func (p *DoubaoWSProvider) getOrCreatePool() *WSConnPool {
	connKey := p.poolKey()
	opts := p.poolOptions()

	wsConnPoolsLock.RLock()
	pool, exists := wsConnPools[connKey]
	wsConnPoolsLock.RUnlock()

	if !exists {
		wsConnPoolsLock.Lock()
		if pool, exists = wsConnPools[connKey]; !exists {
			pool = NewWSConnPool(p.WSURL, p.Header, opts)
			wsConnPools[connKey] = pool
			log.Debugf("为key %s 创建新的连接池", connKey)
		}
		wsConnPoolsLock.Unlock()
	}

	if pool != nil {
		pool.UpdateOptions(opts)
	}

	return pool
}

func (p *DoubaoWSProvider) preWarmConnections() {
	pool := p.getOrCreatePool()
	if pool == nil {
		return
	}

	if err := pool.PreWarm(); err != nil {
		log.Warnf("豆包TTS连接池预热失败: %v", err)
	}
}

// 获取WebSocket连接(连接池实现)
func (p *DoubaoWSProvider) getWSConnection() (*websocket.Conn, error) {
	pool := p.getOrCreatePool()
	if pool == nil {
		return nil, fmt.Errorf("未能创建豆包TTS连接池")
	}

	// 从连接池获取连接
	return pool.Get()
}

// 归还连接到连接池
func (p *DoubaoWSProvider) returnWSConnection(conn *websocket.Conn) {
	wsConnPoolsLock.RLock()
	pool, exists := wsConnPools[p.poolKey()]
	wsConnPoolsLock.RUnlock()

	if exists && pool != nil {
		pool.Put(conn)
	}
}

// 从连接池中移除连接(发生错误时使用)
func (p *DoubaoWSProvider) removeWSConnection(conn *websocket.Conn) {
	wsConnPoolsLock.RLock()
	pool, exists := wsConnPools[p.poolKey()]
	wsConnPoolsLock.RUnlock()

	if exists && pool != nil {
		// 关闭连接并从连接池中移除
		pool.lock.Lock()
		defer pool.lock.Unlock()

		for i, wrapper := range pool.connections {
			if wrapper.Conn == conn {
				wrapper.Conn.Close()
				// 从切片中移除该连接
				pool.connections = append(pool.connections[:i], pool.connections[i+1:]...)
				log.Debugf("从连接池中移除无效连接")
				if err := pool.ensureMinConnectionsLocked(); err != nil {
					log.Warnf("豆包TTS连接池移除后补充失败: %v", err)
				}
				break
			}
		}
	}
}

// 设置请求输入
func (p *DoubaoWSProvider) setupInput(text, voiceType, opt string, sampleRate int, channels int, frameDuration int) []byte {
	// 生成请求ID
	reqID := generateUUID()

	p.settingsMu.RLock()
	speedRatio := p.speedRatio
	volumeRatio := p.volumeRatio
	pitchRatio := p.pitchRatio
	p.settingsMu.RUnlock()

	// 构建请求参数
	params := make(map[string]map[string]interface{})

	// 应用信息
	params["app"] = make(map[string]interface{})
	params["app"]["appid"] = p.AppID
	params["app"]["token"] = p.AccessToken
	params["app"]["cluster"] = p.Cluster
	// 用户信息
	params["user"] = make(map[string]interface{})
	params["user"]["uid"] = "uid"

	// 音频参数
	params["audio"] = make(map[string]interface{})
	params["audio"]["voice_type"] = voiceType
	params["audio"]["encoding"] = "mp3"
	params["audio"]["rate"] = sampleRate
	params["audio"]["speed_ratio"] = speedRatio
	params["audio"]["volume_ratio"] = volumeRatio
	params["audio"]["pitch_ratio"] = pitchRatio

	// 请求信息
	params["request"] = make(map[string]interface{})
	params["request"]["reqid"] = reqID
	params["request"]["text"] = text
	params["request"]["text_type"] = "plain"
	params["request"]["operation"] = opt

	// 序列化为JSON
	resBytes, _ := json.Marshal(params)
	return resBytes
}

// gzip压缩
func gzipCompress(input []byte) []byte {
	var b bytes.Buffer
	w := gzip.NewWriter(&b)
	w.Write(input)
	w.Close()
	return b.Bytes()
}

// gzip解压缩
func gzipDecompress(input []byte) []byte {
	b := bytes.NewBuffer(input)
	r, err := gzip.NewReader(b)
	if err != nil {
		log.Debugf("豆包TTS gzip解压失败: %v", err)
		return nil
	}
	defer r.Close()

	out, err := io.ReadAll(r)
	if err != nil {
		log.Debugf("豆包TTS 读取解压数据失败: %v", err)
		return nil
	}
	return out
}

// 解析WebSocket响应
func parseResponse(res []byte) (resp synResp, err error) {
	if len(res) < headerSize {
		return resp, fmt.Errorf("响应头长度不足: %d", len(res))
	}

	// 解析头部
	protoVersion := res[0] >> 4
	headSizeByte := res[0] & 0x0f
	messageType := res[1] >> 4
	messageTypeSpecificFlags := res[1] & 0x0f
	serializationMethod := res[2] >> 4
	messageCompression := res[2] & 0x0f

	_ = protoVersion
	_ = serializationMethod
	/*
		log.Debugf("协议版本: %x - 版本 %d\n", protoVersion, protoVersion)
		log.Debugf("头部大小: %x - %d 字节\n", headSizeByte, headSizeByte*4)
		log.Debugf("消息类型: %x - %s\n", messageType, enumMessageType[messageType])
		log.Debugf("消息类型特定标志: %x - %s\n", messageTypeSpecificFlags, enumMessageTypeSpecificFlags[messageTypeSpecificFlags])
		log.Debugf("消息序列化方法: %x - %s\n", serializationMethod, enumMessageSerializationMethods[serializationMethod])
		log.Debugf("消息压缩: %x - %s\n", messageCompression, enumMessageCompression[messageCompression])
	*/

	// 计算头部大小（以字节为单位）
	headSizeBytes := int(headSizeByte) * 4

	// 分离payload
	if headSizeBytes > len(res) {
		return resp, fmt.Errorf("head size %d 超过消息总长度 %d", headSizeBytes, len(res))
	}

	payload := res[headSizeBytes:]

	// 根据消息类型处理
	if messageType == 0xb { // audio-only server response (11)
		// 检查是否有序列号
		if messageTypeSpecificFlags == 0 {
			// 无序列号，空payload
			return
		} else {
			// 有序列号，提取payload
			if len(payload) < 8 {
				return resp, fmt.Errorf("payload 长度不足以解析音频头: %d", len(payload))
			}
			sequenceNumber := int32(binary.BigEndian.Uint32(payload[0:4]))
			payloadSize := int32(binary.BigEndian.Uint32(payload[4:8]))
			payload = payload[8:]

			_ = payloadSize
			//log.Debugf("序列号: %d", sequenceNumber)
			//log.Debugf("Payload大小: %d", payloadSize)

			resp.Audio = append(resp.Audio, payload...)

			// 检查是否为最后一个包
			if sequenceNumber < 0 {
				resp.IsLast = true
			}
		}
	} else if messageType == 0xf { // error message (15)
		// 解析错误信息
		if len(payload) < 8 {
			return resp, fmt.Errorf("错误响应长度不足: %d", len(payload))
		}

		backendCode := int32(binary.BigEndian.Uint32(payload[0:4]))
		errMsg := payload[8:]

		decodedMsg := errMsg
		if messageCompression == 1 {
			if decompressed := gzipDecompress(errMsg); decompressed != nil {
				decodedMsg = decompressed
			}
		}

		serverErr := &doubaoServerError{
			BackendCode: int(backendCode),
			Message:     string(decodedMsg),
		}

		if len(decodedMsg) > 0 {
			var parsed struct {
				ReqID       string `json:"reqid"`
				Message     string `json:"message"`
				Code        int    `json:"code"`
				BackendCode int    `json:"backend_code"`
			}
			if json.Unmarshal(decodedMsg, &parsed) == nil {
				serverErr.RequestID = parsed.ReqID
				if parsed.Message != "" {
					serverErr.Message = parsed.Message
				}
				if parsed.Code != 0 {
					serverErr.HTTPCode = parsed.Code
				}
				if parsed.BackendCode != 0 {
					serverErr.BackendCode = parsed.BackendCode
				}
			}
		}

		if serverErr.HTTPCode == 0 {
			serverErr.HTTPCode = http.StatusInternalServerError
		}

		log.Errorf("服务端错误 (代码: %d): %s", serverErr.BackendCode, string(decodedMsg))
		return resp, serverErr
	} else if messageType == 0xc { // frontend server response (12)
		// 解析前端消息
		if len(payload) < 4 {
			return resp, fmt.Errorf("前端消息长度不足: %d", len(payload))
		}

		msgSize := int32(binary.BigEndian.Uint32(payload[0:4]))
		payload = payload[4:]

		// 如果是压缩的，解压缩
		if messageCompression == 1 {
			payload = gzipDecompress(payload)
		}

		// 记录前端消息
		if os.Getenv("DEBUG") == "1" {
			log.Debugf("前端消息大小: %d", msgSize)
			log.Debugf("前端消息内容: %s", string(payload))
		}
	} else {
		// 未知消息类型
		log.Warnf("未知消息类型: %d", messageType)
		err = fmt.Errorf("未知消息类型: %d", messageType)
		return
	}

	return
}

// saveAudioToTmp 将音频数据保存到tmp目录
func saveAudioToTmp(audioData []byte, format string) error {
	// 确保tmp目录存在
	tmpDir := "tmp"
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return fmt.Errorf("创建tmp目录失败: %v", err)
	}

	// 生成唯一文件名
	timestamp := time.Now().Format("20060102_150405")
	uuid := generateUUID()
	filename := filepath.Join(tmpDir, fmt.Sprintf("audio_%s_%s.%s", timestamp, uuid[:8], format))

	// 写入文件
	if err := os.WriteFile(filename, audioData, 0644); err != nil {
		return fmt.Errorf("写入音频文件失败: %v", err)
	}

	log.Debugf("音频文件已保存: %s", filename)
	return nil
}
