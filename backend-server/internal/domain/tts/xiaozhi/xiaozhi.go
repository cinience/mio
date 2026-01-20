package xiaozhi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"backend-server/constants"
	log "backend-server/internal/infrastructure/logger"
	"backend-server/internal/registry/tts"

	"github.com/gorilla/websocket"
)

// init 函数注册Xiaozhi TTS提供者
func init() {
	// 注册 Xiaozhi TTS 提供者
	tts.Register([]string{constants.TtsTypeXiaozhi}, "Xiaozhi TTS 语音合成服务", func(config map[string]interface{}) (tts.BaseTTSProvider, error) {
		provider := NewXiaozhiProvider(config)
		return provider, nil
	})
}

// WSConnWrapper WebSocket连接包装器，带有最后活跃时间
// 与 doubao_ws.go 保持一致

type WSConnWrapper struct {
	Conn         *websocket.Conn
	LastActiveAt time.Time
	InUse        bool   // 标记连接是否正在使用中
	DeviceId     string // 使用的设备ID
}

var deviceIdList = []string{
	"ba:8f:17:de:94:94",
	"f2:85:44:27:7b:51",
	"4f:57:fb:d4:69:fa",
	"b3:1e:1c:80:cc:78",
	"32:a5:cc:b7:c0:e4",
	"2b:60:6a:5a:72:10",
	"ca:a6:8b:20:f1:6f",
	"26:1a:d7:27:9f:f8",
	"03:02:26:58:2b:06",
	"5f:f3:85:8b:5d:da",
}

// 记录最近出错的deviceId及其禁用到期时间
var (
	deviceIdBlocklist     = make(map[string]time.Time)
	deviceIdBlocklistLock sync.Mutex
	// 设备ID禁用时间（出错后多久内不使用）
	deviceIdBlockDuration = 5 * time.Second
)

// XiaozhiProvider 小智TTS WebSocket Provider，支持连接池
// 支持流式文本转语音

type XiaozhiProvider struct {
	ServerAddr  string
	DeviceID    string
	AudioFormat map[string]interface{}
	Header      http.Header
}

var (
	// 连接池，使用slice存储WebSocket连接
	wsConnPool   = make([]*WSConnWrapper, 0, 10)
	wsClientLock sync.Mutex
	// 连接池最大连接数
	maxPoolSize = 10
	// 连接池最大超时时间为5分钟
	maxIdleTime = 5 * time.Minute
)

// 定期清理过期连接的协程
func init() {
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			wsClientLock.Lock()
			// 检查并清理所有过期的连接
			// 倒序遍历，避免删除元素时的索引问题
			for i := len(wsConnPool) - 1; i >= 0; i-- {
				conn := wsConnPool[i]
				if time.Since(conn.LastActiveAt) > maxIdleTime {
					log.Infof("清理超时连接，超时时间: %v", time.Since(conn.LastActiveAt))
					conn.Conn.Close()
					wsConnPool = append(wsConnPool[:i], wsConnPool[i+1:]...)
				}
			}
			wsClientLock.Unlock()

			// 清理过期的deviceId禁用列表
			deviceIdBlocklistLock.Lock()
			now := time.Now()
			for id, expireTime := range deviceIdBlocklist {
				if now.After(expireTime) {
					delete(deviceIdBlocklist, id)
					log.Debugf("设备ID禁用已过期，重新启用: %s", id)
				}
			}
			deviceIdBlocklistLock.Unlock()
		}
	}()
}

// 将deviceId添加到禁用列表
func blockDeviceId(deviceId string) {
	deviceIdBlocklistLock.Lock()
	defer deviceIdBlocklistLock.Unlock()

	deviceIdBlocklist[deviceId] = time.Now().Add(deviceIdBlockDuration)
	log.Warnf("设备ID %s 已添加到禁用列表，将在 %v 后重新启用", deviceId, deviceIdBlockDuration)
}

// 检查deviceId是否在禁用列表中
func isDeviceIdBlocked(deviceId string) bool {
	deviceIdBlocklistLock.Lock()
	defer deviceIdBlocklistLock.Unlock()

	expireTime, exists := deviceIdBlocklist[deviceId]
	if !exists {
		return false
	}

	// 如果过期时间已过，则从禁用列表中移除
	if time.Now().After(expireTime) {
		delete(deviceIdBlocklist, deviceId)
		log.Debugf("设备ID禁用已过期，重新启用: %s", deviceId)
		return false
	}

	return true
}

// NewXiaozhiProvider 创建新的小智TTS Provider
func NewXiaozhiProvider(config map[string]interface{}) *XiaozhiProvider {
	serverAddr, _ := config["server_addr"].(string)
	deviceID, _ := config["device_id"].(string)
	clientID, _ := config["client_id"].(string)
	token, _ := config["token"].(string)
	format := map[string]interface{}{
		"sample_rate":    16000,
		"channels":       1,
		"frame_duration": 20,
		"format":         "opus",
	}

	// 可选配置连接池大小
	if poolSize, ok := config["pool_size"].(int); ok && poolSize > 0 {
		wsClientLock.Lock()
		maxPoolSize = poolSize
		wsClientLock.Unlock()
		log.Infof("设置连接池大小: %d", maxPoolSize)
	}

	// 设置连接超时时间（秒）
	if idleTime, ok := config["idle_timeout"].(int); ok && idleTime > 0 {
		wsClientLock.Lock()
		maxIdleTime = time.Duration(idleTime) * time.Second
		wsClientLock.Unlock()
		log.Infof("设置连接超时时间: %v", maxIdleTime)
	}

	header := http.Header{}
	header.Set("Device-Id", deviceID)
	header.Set("Content-Type", "application/json")
	header.Set("Authorization", "Bearer "+token)
	header.Set("Protocol-Version", "1")
	header.Set("Client-Id", clientID)

	return &XiaozhiProvider{
		ServerAddr:  serverAddr,
		DeviceID:    deviceID,
		AudioFormat: format,
		Header:      header,
	}
}

// ChangeVoice is not supported because voice routing is managed by the Xiaozhi backend service.
func (p *XiaozhiProvider) ChangeVoice(ctx context.Context, options *tts.ChangeVoiceOptions) error {
	return fmt.Errorf("xiaozhi tts provider does not support runtime voice switching")
}

// getWSConnection 获取或复用全局WebSocket连接
func (p *XiaozhiProvider) getWSConnection() (*websocket.Conn, error) {
	wsClientLock.Lock()
	defer wsClientLock.Unlock()

	// 首先检查是否有已存在且未使用的连接
	for i, conn := range wsConnPool {
		if !conn.InUse {
			// 如果设备ID在禁用列表中，跳过这个连接
			if isDeviceIdBlocked(conn.DeviceId) {
				log.Warnf("跳过禁用中的设备ID连接: %s", conn.DeviceId)
				continue
			}

			err := conn.Conn.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(1*time.Second))
			isTimeout := time.Since(conn.LastActiveAt) > maxIdleTime
			if err == nil && !isTimeout {
				conn.LastActiveAt = time.Now()
				conn.InUse = true

				// 更新p.DeviceID为此连接对应的DeviceId
				p.DeviceID = conn.DeviceId
				p.Header.Set("Device-Id", conn.DeviceId)

				log.Debugf("复用已有连接，设备ID: %s", conn.DeviceId)
				return conn.Conn, nil
			}
			// 连接已失效或超时，关闭并移除
			log.Debugf("连接已失效或超时，关闭并创建新连接")
			conn.Conn.Close()
			wsConnPool = append(wsConnPool[:i], wsConnPool[i+1:]...)
			break
		}
	}

	// 检查连接池大小，如果已满则关闭最旧的连接
	if len(wsConnPool) >= maxPoolSize {
		log.Infof("连接池已满，关闭最旧的连接")
		var oldestConn *WSConnWrapper
		var oldestIdx int = -1
		var oldestTime time.Time
		// 寻找最旧的未使用的连接
		for i, conn := range wsConnPool {
			if !conn.InUse && (oldestTime.IsZero() || conn.LastActiveAt.Before(oldestTime)) {
				oldestConn = conn
				oldestIdx = i
				oldestTime = conn.LastActiveAt
			}
		}

		// 如果没有找到未使用的连接，寻找最旧的使用中连接
		if oldestIdx < 0 {
			for i, conn := range wsConnPool {
				if oldestTime.IsZero() || conn.LastActiveAt.Before(oldestTime) {
					oldestConn = conn
					oldestIdx = i
					oldestTime = conn.LastActiveAt
				}
			}
		}

		// 关闭并移除最旧的连接
		if oldestIdx >= 0 {
			log.Infof("关闭最旧的连接，设备ID: %s, 活跃时间: %v, 使用状态: %v",
				oldestConn.DeviceId, oldestTime, oldestConn.InUse)
			oldestConn.Conn.Close()
			wsConnPool = append(wsConnPool[:oldestIdx], wsConnPool[oldestIdx+1:]...)
		}
	}

	// 查找一个未被使用且未被禁用的deviceId
	var selectedDeviceId string
	usedDeviceIds := make(map[string]bool)

	// 收集所有已经在连接池中使用的deviceId
	for _, conn := range wsConnPool {
		usedDeviceIds[conn.DeviceId] = true
	}

	// 从deviceIdList中找出未被使用且未被禁用的deviceId
	for _, deviceId := range deviceIdList {
		if !usedDeviceIds[deviceId] && !isDeviceIdBlocked(deviceId) {
			selectedDeviceId = deviceId
			log.Debugf("选择未被使用的设备ID: %s", selectedDeviceId)
			break
		}
	}

	// 如果所有deviceId都已使用或被禁用，尝试找一个未使用但被禁用的
	if selectedDeviceId == "" {
		for _, deviceId := range deviceIdList {
			if !usedDeviceIds[deviceId] {
				selectedDeviceId = deviceId
				log.Warnf("所有未禁用的deviceId已被使用，使用被禁用的设备ID: %s", selectedDeviceId)
				break
			}
		}
	}

	// 如果所有deviceId都已使用，从deviceIdList中轮询选择未禁用的
	if selectedDeviceId == "" {
		// 将未禁用的deviceId添加到候选列表
		candidates := make([]string, 0)
		for _, id := range deviceIdList {
			if !isDeviceIdBlocked(id) {
				candidates = append(candidates, id)
			}
		}

		if len(candidates) > 0 {
			selectedIndex := len(wsConnPool) % len(candidates)
			selectedDeviceId = candidates[selectedIndex]
			log.Warnf("所有deviceId已被使用，轮询选择未禁用设备ID: %s (索引: %d)", selectedDeviceId, selectedIndex)
		} else if len(deviceIdList) > 0 {
			// 如果所有deviceId都被禁用，则从所有deviceId中选择
			selectedIndex := len(wsConnPool) % len(deviceIdList)
			selectedDeviceId = deviceIdList[selectedIndex]
			log.Warnf("所有deviceId均被禁用，被迫选择设备ID: %s (索引: %d)", selectedDeviceId, selectedIndex)
		} else {
			// 如果deviceIdList为空，使用传入的deviceId
			selectedDeviceId = p.DeviceID
			log.Warnf("deviceIdList为空，使用当前设备ID: %s", selectedDeviceId)
		}
	}

	// 更新当前p.DeviceID和Header
	p.DeviceID = selectedDeviceId
	p.Header.Set("Device-Id", selectedDeviceId)

	// 创建新连接
	conn, _, err := websocket.DefaultDialer.Dial(p.ServerAddr, p.Header)
	if err != nil {
		log.Errorf("创建WebSocket连接失败: %v, 设备ID: %s", err, selectedDeviceId)
		blockDeviceId(selectedDeviceId) // 将失败的deviceId加入禁用列表
		return nil, err
	}
	// 设置保持连接
	conn.SetPingHandler(func(appData string) error {
		return conn.WriteControl(websocket.PongMessage, []byte(appData), time.Now().Add(5*time.Second))
	})

	// 新建连接时发送hello消息
	helloMsg := map[string]interface{}{
		"type":         "hello",
		"device_id":    selectedDeviceId,
		"transport":    "websocket",
		"version":      1,
		"audio_params": p.AudioFormat,
	}
	log.Debugf("创建新连接并发送hello消息，设备ID: %s", selectedDeviceId)
	conn.WriteJSON(helloMsg)

	newConn := &WSConnWrapper{
		Conn:         conn,
		LastActiveAt: time.Now(),
		InUse:        true,
		DeviceId:     selectedDeviceId,
	}
	wsConnPool = append(wsConnPool, newConn)
	log.Infof("当前连接池大小: %d/%d, 使用设备ID: %s", len(wsConnPool), maxPoolSize, selectedDeviceId)
	return conn, nil
}

// removeWSConnection 从连接池中移除连接
func (p *XiaozhiProvider) removeWSConnection() {
	removeWSConnection(p.DeviceID)
}

// removeWSConnection 根据deviceId从连接池中移除连接
func removeWSConnection(deviceId string) {
	wsClientLock.Lock()
	defer wsClientLock.Unlock()

	removedCount := 0
	// 查找并移除指定设备ID的连接
	for i := len(wsConnPool) - 1; i >= 0; i-- {
		conn := wsConnPool[i]
		// 只移除DeviceId匹配的连接
		if conn.DeviceId == deviceId {
			// 确保连接被正确关闭
			closeErr := conn.Conn.Close()
			if closeErr != nil {
				log.Warnf("关闭连接时出错: %v, 设备ID: %s", closeErr, conn.DeviceId)
			}

			// 从连接池中移除
			wsConnPool = append(wsConnPool[:i], wsConnPool[i+1:]...)
			removedCount++

			log.Infof("已关闭并从池中移除连接，设备ID: %s，当前连接池大小: %d/%d",
				conn.DeviceId, len(wsConnPool), maxPoolSize)
		}
	}

	if removedCount == 0 {
		log.Warnf("未找到要移除的连接，设备ID: %s", deviceId)
	} else if removedCount > 1 {
		// 如果移除了多个连接，这可能表明连接池中有重复
		log.Warnf("移除了多个相同设备ID的连接(%d)，设备ID: %s", removedCount, deviceId)
	}
}

type RecvMsg struct {
	Type    string `json:"type"`
	State   string `json:"state"`
	Text    string `json:"text"`
	Version int    `json:"version"`
}

// updateWSConnectionActiveTime 更新连接活跃时间
func (p *XiaozhiProvider) updateWSConnectionActiveTime() {
	updateWSConnectionActiveTime(p.DeviceID)
}

// updateWSConnectionActiveTime 根据deviceId更新连接活跃时间
func updateWSConnectionActiveTime(deviceId string) {
	wsClientLock.Lock()
	defer wsClientLock.Unlock()
	for _, conn := range wsConnPool {
		if conn.DeviceId == deviceId {
			conn.LastActiveAt = time.Now()
			break
		}
	}
}

// releaseWSConnection 归还连接到连接池
func (p *XiaozhiProvider) releaseWSConnection() {
	releaseWSConnection(p.DeviceID)
}

// releaseWSConnection 根据deviceId归还连接到连接池
func releaseWSConnection(deviceId string) {
	wsClientLock.Lock()
	defer wsClientLock.Unlock()

	connFound := false
	for _, conn := range wsConnPool {
		if conn.DeviceId == deviceId {
			connFound = true
			// 只有在连接仍在使用中时才发送stop消息
			if conn.InUse {
				// 发送stop消息，确保服务器端状态与客户端一致
				stopMsg := map[string]interface{}{
					"type":      "listen",
					"device_id": conn.DeviceId,
					"state":     "stop",
				}
				err := conn.Conn.WriteJSON(stopMsg)
				if err != nil {
					log.Errorf("发送stop消息失败: %v, 设备ID: %s", err, conn.DeviceId)
					// 尝试发送ping消息来检查连接是否还活着
					pingErr := conn.Conn.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(1*time.Second))
					if pingErr != nil {
						errorType := "unknown"
						if strings.Contains(pingErr.Error(), "close sent") {
							errorType = "connection_closed_by_local"
						} else if strings.Contains(pingErr.Error(), "use of closed network connection") {
							errorType = "network_connection_closed"
						} else if websocket.IsCloseError(pingErr) {
							errorType = "websocket_close_error"
						}
						log.Errorf("连接ping检测失败，关闭连接: 错误类型: %s, 错误详情: %v, 设备ID: %s",
							errorType, pingErr, conn.DeviceId)
						conn.Conn.Close()
						conn.InUse = false
						blockDeviceId(conn.DeviceId) // 将失败的deviceId加入禁用列表
					} else {
						// 连接还活着，只是发送stop消息失败，仍然可以标记为未使用
						log.Warnf("发送stop消息失败但连接仍活着，设备ID: %s，标记为未使用", conn.DeviceId)
						conn.InUse = false
						conn.LastActiveAt = time.Now()
					}
				} else {
					log.Debugf("归还连接到连接池，设备ID: %s, 发送stop消息成功", conn.DeviceId)
					// 更新活跃时间并标记为未使用，但保留DeviceId关联
					conn.LastActiveAt = time.Now()
					conn.InUse = false
				}
			} else {
				log.Debugf("连接已经标记为未使用，设备ID: %s", conn.DeviceId)
			}
			break
		}
	}

	if !connFound {
		log.Warnf("释放连接时未找到匹配的连接，设备ID: %s", deviceId)
	}
}

// handleTTSConnection 封装获取连接、发送消息和接收消息的逻辑
func (p *XiaozhiProvider) handleTTSConnection(ctx context.Context, text string, outputChan chan []byte) error {
	// 获取连接
	conn, err := p.getWSConnection()
	if err != nil {
		return fmt.Errorf("连接小智TTS服务失败: %v", err)
	}

	deviceId := p.DeviceID // 保存当前连接的deviceId，防止后续变化

	// 发送listen detect消息
	sendText := fmt.Sprintf("`%s`", text)
	listenMsg := map[string]interface{}{
		"type":      "listen",
		"device_id": deviceId,
		"state":     "detect",
		"text":      sendText,
	}
	log.Debugf("发送xiaozhi服务端消息: %v", listenMsg)

	if err := conn.WriteJSON(listenMsg); err != nil {
		log.Errorf("发送listen消息失败: %v，设备ID: %s", err, deviceId)
		blockDeviceId(deviceId) // 将出错的deviceId加入禁用列表

		// 直接从连接池中移除连接
		removeWSConnection(deviceId)

		return fmt.Errorf("发送消息失败: %v", err)
	}

	// 读取并处理消息
	startTs := time.Now().UnixMilli()
	var firstFrameTs bool
	i := 0
	receivedFrames := false

	for {
		select {
		case <-ctx.Done():
			log.Debugf("xiaozhi服务端消息ctx.Done(), 设备ID: %s", deviceId)
			releaseWSConnection(deviceId)
			return nil
		default:
		}
		msgType, msg, err := conn.ReadMessage()
		if err != nil {
			// 连接出错
			log.Errorf("读取消息错误: %v，设备ID: %s", err, deviceId)

			// 如果还没有收到任何音频帧，说明连接可能有问题，将deviceId加入禁用列表
			if !receivedFrames {
				blockDeviceId(deviceId)
			}

			// 直接从连接池中移除这个连接，而不是标记为未使用
			removeWSConnection(deviceId)

			return fmt.Errorf("读取消息错误: %v", err)
		}
		if msgType == websocket.TextMessage {
			log.Debugf("收到xiaozhi服务端消息: %s", string(msg))
			var recvMsg RecvMsg
			err := json.Unmarshal(msg, &recvMsg)
			if err != nil {
				continue
			}
			if recvMsg.Type == "tts" {
				if recvMsg.State == "stop" {
					log.Debugf("xiaozhi服务端消息tts stop消息")
					// 更新连接活跃时间
					updateWSConnectionActiveTime(deviceId)
					releaseWSConnection(deviceId)
					return nil
				}
			}
		} else if msgType == websocket.BinaryMessage {
			receivedFrames = true
			if !firstFrameTs {
				firstFrameTs = true
				log.Debugf("tts耗时统计: xiaozhi服务tts 第一个音频帧时间: %d", time.Now().UnixMilli()-startTs)
			}
			outputChan <- msg
			if i%20 == 0 {
				log.Debugf("xiaozhi服务端音频消息, 已收到%d个音频帧", i)
			}
			i++
		}
	}
}

// TextToSpeechStream 实现流式TTS，返回opus音频帧chan
func (p *XiaozhiProvider) TextToSpeechStream(ctx context.Context, text string, sampleRate int, channels int, frameDuration int) (chan []byte, error) {
	outputChan := make(chan []byte, 1000)

	// 尝试处理TTS连接，支持重试
	go func() {
		defer close(outputChan)

		retryCount := 0
		maxRetries := 2
		var lastError error

		// 最多尝试maxRetries次
		for retryCount <= maxRetries {
			if retryCount > 0 {
				log.Infof("尝试重新获取连接，第 %d/%d 次重试", retryCount, maxRetries)

				// 在重试前检查上下文是否已取消
				select {
				case <-ctx.Done():
					log.Debugf("上下文已取消，停止重试")
					return
				default:
					// 继续重试
				}
			}

			// 处理TTS连接
			err := p.handleTTSConnection(ctx, text, outputChan)

			if err == nil {
				// 连接处理成功，无需重试
				return
			}

			lastError = err
			log.Errorf("TTS连接处理失败: %v (重试: %d/%d)", err, retryCount, maxRetries)

			retryCount++
		}

		if retryCount > maxRetries {
			log.Warnf("达到最大重试次数 %d，放弃重试，最后错误: %v", maxRetries, lastError)
		}
	}()

	return outputChan, nil
}

// GetVoiceInfo 获取TTS配置信息
func (p *XiaozhiProvider) GetVoiceInfo() map[string]interface{} {
	return map[string]interface{}{
		"type":         "xiaozhi_ws",
		"server_addr":  p.ServerAddr,
		"device_id":    p.DeviceID,
		"audio_format": p.AudioFormat,
	}
}

// TextToSpeech 实现 BaseTTSProvider 接口，直接聚合流式帧
func (p *XiaozhiProvider) TextToSpeech(ctx context.Context, text string, sampleRate int, channels int, frameDuration int) ([][]byte, error) {
	ch, err := p.TextToSpeechStream(ctx, text, sampleRate, channels, frameDuration)
	if err != nil {
		return nil, err
	}
	var frames [][]byte
	for frame := range ch {
		frames = append(frames, frame)
	}
	return frames, nil
}

// GetPoolStatus 获取连接池状态
func GetPoolStatus() map[string]interface{} {
	wsClientLock.Lock()
	defer wsClientLock.Unlock()

	connStatus := make([]map[string]interface{}, 0)
	var inUseCount int = 0

	for _, conn := range wsConnPool {
		if conn.InUse {
			inUseCount++
		}

		connStatus = append(connStatus, map[string]interface{}{
			"device_id":    conn.DeviceId,
			"last_active":  conn.LastActiveAt,
			"idle_time_ms": time.Since(conn.LastActiveAt).Milliseconds(),
			"in_use":       conn.InUse,
		})
	}

	return map[string]interface{}{
		"total_size":      maxPoolSize,
		"current_size":    len(wsConnPool),
		"in_use_count":    inUseCount,
		"available_count": len(wsConnPool) - inUseCount,
		"connections":     connStatus,
		"max_idle_time_s": maxIdleTime.Seconds(),
	}
}

// Close 关闭当前Provider的连接
func (p *XiaozhiProvider) Close() error {
	//p.removeWSConnection()
	return nil
}

// IsConnected 检查当前Provider是否有活跃连接
func (p *XiaozhiProvider) IsConnected() bool {
	wsClientLock.Lock()
	defer wsClientLock.Unlock()

	for _, conn := range wsConnPool {
		if conn.InUse && conn.DeviceId == p.DeviceID {
			// 发送ping检查连接是否可用
			err := conn.Conn.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(1*time.Second))
			return err == nil
		}
	}
	return false
}

// TestPoolConnection 测试创建和归还连接，用于自动化测试
func (p *XiaozhiProvider) TestPoolConnection() error {
	// 获取连接
	conn, err := p.getWSConnection()
	if err != nil {
		return fmt.Errorf("获取连接失败: %v", err)
	}

	deviceId := p.DeviceID // 保存当前deviceId

	// 测试连接是否可用
	err = conn.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(1*time.Second))
	if err != nil {
		blockDeviceId(deviceId) // 将失败的deviceId加入禁用列表
		return fmt.Errorf("连接不可用: %v", err)
	}

	// 归还连接
	releaseWSConnection(deviceId)

	// 检查连接池状态
	status := GetPoolStatus()
	log.Infof("连接池状态: %+v", status)

	// 将刚刚测试的连接状态输出
	log.Infof("测试连接完成，设备ID: %s, 连接可用", deviceId)

	return nil
}
