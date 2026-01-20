package websocket

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	log "backend-server/internal/infrastructure/logger"
	"backend-server/internal/server/transport/types"

	"github.com/gorilla/websocket"
)

// WebSocketConn 实现 types.IConn 接口，适配 WebSocket 连接
type WebSocketConn struct {
	ctx    context.Context
	cancel context.CancelFunc

	onCloseCbList []func(deviceId string)

	conn     *websocket.Conn
	deviceID string

	protocolVersion int

	isMqttUdpBridge bool
	recvCmdChan     chan []byte
	recvAudioChan   chan []byte

	// HTTP headers for real IP detection
	header http.Header

	closed bool
	// 连接状态跟踪
	connectedAt    time.Time // 连接建立时间
	lastPingAt     time.Time // 最后一次成功ping时间
	lastPongAt     time.Time // 最后一次收到pong时间
	closeReason    string    // 关闭原因
	closeInitiator string    // 关闭发起方
	sync.RWMutex
}

type binaryPayloadKind int

const (
	binaryPayloadKindAudio binaryPayloadKind = iota
	binaryPayloadKindCmd
)

// NewWebSocketConn 创建一个新的 WebSocketConn 实例
func NewWebSocketConn(conn *websocket.Conn, deviceID string, isMqttUdpBridge bool, header http.Header, protocolVersion int) *WebSocketConn {
	ctx, cancel := context.WithCancel(context.Background())
	instance := &WebSocketConn{
		ctx:             ctx,
		cancel:          cancel,
		conn:            conn,
		deviceID:        deviceID,
		protocolVersion: normalizeProtocolVersion(protocolVersion),
		isMqttUdpBridge: isMqttUdpBridge,
		recvCmdChan:     make(chan []byte, 100),
		recvAudioChan:   make(chan []byte, 100),
		header:          header,
		connectedAt:     time.Now(),
		closeInitiator:  "unknown",
	}

	// 设置pong处理器
	conn.SetPongHandler(func(appData string) error {
		instance.Lock()
		instance.lastPongAt = time.Now()
		instance.Unlock()
		//log.Debugf("收到pong消息，设备ID: %s, 响应时间: %v", deviceID, time.Since(instance.lastPingAt))
		return nil
	})

	// 设置关闭处理器
	conn.SetCloseHandler(func(code int, text string) error {
		instance.Lock()
		instance.closeReason = fmt.Sprintf("code: %d, text: %s", code, text)
		instance.closeInitiator = "client"
		instance.Unlock()
		log.Infof("收到客户端关闭消息，设备ID: %s, 关闭码: %d, 消息: %s, 连接持续时间: %v",
			deviceID, code, text, time.Since(instance.connectedAt))
		instance.Close()
		return nil
	})

	go func() {
		for {
			select {
			case <-instance.ctx.Done():
				return
			default:
				msgType, audio, err := instance.conn.ReadMessage()
				if err != nil {
					// 记录连接关闭信息
					instance.Lock()
					if instance.closeInitiator == "unknown" {
						if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
							instance.closeInitiator = "client"
						} else {
							instance.closeInitiator = "network"
						}
						instance.closeReason = err.Error()
					}
					connectionDuration := time.Since(instance.connectedAt)
					closeInitiator := instance.closeInitiator
					closeReason := instance.closeReason
					instance.Unlock()

					// 如果是连接关闭相关错误，使用debug级别日志
					if strings.Contains(err.Error(), "use of closed network connection") ||
						strings.Contains(err.Error(), "connection reset by peer") ||
						websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
						log.Debugf("WebSocket连接关闭，设备ID: %s, 发起方: %s, 原因: %s, 连接持续时间: %v",
							instance.deviceID, closeInitiator, closeReason, connectionDuration)
					} else {
						log.Errorf("WebSocket读取消息异常，设备ID: %s, 发起方: %s, 错误: %v, 连接持续时间: %v",
							instance.deviceID, closeInitiator, err, connectionDuration)
					}
					for _, cb := range instance.onCloseCbList {
						cb(instance.deviceID) //通知注册方退出
					}

					instance.Close()
					return
				}

				if msgType == websocket.TextMessage {
					select {
					case instance.recvCmdChan <- audio:
					default:
						log.Errorf("recv cmd channel is full")
					}
				} else if msgType == websocket.BinaryMessage {
					if instance.isMqttUdpBridge {
						audio = instance.tryUnpackUdpBridgeAudioPacket(audio)
						instance.enqueueBinaryPayload(binaryPayloadKindAudio, audio)
						continue
					}

					payload, kind, err := instance.decodeBinaryPayload(audio)
					if err != nil {
						log.Warnf("解析二进制音频帧失败，设备ID: %s, 错误: %v", instance.deviceID, err)
						continue
					}
					if len(payload) == 0 {
						continue
					}
					instance.enqueueBinaryPayload(kind, payload)
				}
			}
		}
	}()

	return instance
}

// 适配mqtt udp bridge的数据格式
// 前8个字节为0, 12-16字节为音频数据长度, 16字节后为音频数据
func (w *WebSocketConn) tryUnpackUdpBridgeAudioPacket(buffer []byte) []byte {
	if len(buffer) < 16 {
		return buffer
	}
	// 检查前8字节是否全为0
	for i := 0; i < 8; i++ {
		if buffer[i] != 0 {
			return buffer
		}
	}
	dataLen := binary.BigEndian.Uint32(buffer[12:16])
	if int(dataLen) != len(buffer)-16 {
		return buffer
	}
	audioData := buffer[16:]
	return audioData
}

func (w *WebSocketConn) packUdpBridgeAudioPacket(buffer []byte) []byte {
	header := make([]byte, 16)
	// 前8字节全为0，已初始化
	// 9~12字节写入当前时间戳（秒）
	timestamp := uint32(time.Now().Unix())
	binary.BigEndian.PutUint32(header[8:12], timestamp)
	// 13~16字节写入音频长度
	binary.BigEndian.PutUint32(header[12:16], uint32(len(buffer)))
	// 拼接header和音频数据
	return append(header, buffer...)
}

func (w *WebSocketConn) SendCmd(msg []byte) error {
	w.Lock()
	defer w.Unlock()

	if w.closed {
		return errors.New("connection is closed")
	}
	//log.Debugf("### send cmd: %s", string(msg))

	err := w.conn.WriteMessage(websocket.TextMessage, msg)
	if err != nil {
		log.Errorf("send cmd error: %v", err)
		return err
	}
	return nil
}

func (w *WebSocketConn) SendAudio(audio []byte) error {
	version := w.getProtocolVersion()

	w.Lock()
	defer w.Unlock()

	if w.closed {
		return errors.New("connection is closed")
	}

	if w.isMqttUdpBridge {
		audio = w.packUdpBridgeAudioPacket(audio)
	} else {
		audio = w.packBinaryPayload(version, audio)
	}
	err := w.conn.WriteMessage(websocket.BinaryMessage, audio)
	if err != nil {
		log.Errorf("send audio error: %v", err)
		return err
	}
	return nil
}

func (w *WebSocketConn) RecvCmd(ctx context.Context, timeout int) ([]byte, error) {
	for {
		select {
		case <-ctx.Done():
			log.Debugf("recv cmd context done")
			return nil, ctx.Err()
		case msg, ok := <-w.recvCmdChan:
			if !ok {
				return nil, errors.New("connection is closed")
			}
			return msg, nil
		case <-time.After(time.Duration(timeout) * time.Second):
			return nil, errors.New("timeout")
		}
	}
}

func (w *WebSocketConn) RecvAudio(ctx context.Context, timeout int) ([]byte, error) {
	for {
		select {
		case <-ctx.Done():
			log.Debugf("recv audio context done")
			return nil, ctx.Err()
		case audio, ok := <-w.recvAudioChan:
			if !ok {
				return nil, errors.New("connection is closed")
			}
			return audio, nil
		case <-time.After(time.Duration(timeout) * time.Second):
			return nil, errors.New("timeout")
		}
	}
}

func (w *WebSocketConn) Close() error {
	w.Lock()
	defer w.Unlock()

	if w.closed {
		return nil // Already closed
	}

	w.closed = true
	if w.closeInitiator == "unknown" {
		w.closeInitiator = "server"
		w.closeReason = "server initiated close"
	}
	connectionDuration := time.Since(w.connectedAt)

	log.Infof("主动关闭WebSocket连接，设备ID: %s, 发起方: %s, 原因: %s, 连接持续时间: %v",
		w.deviceID, w.closeInitiator, w.closeReason, connectionDuration)

	w.cancel()
	w.conn.Close()
	close(w.recvCmdChan)
	close(w.recvAudioChan)
	return nil
}

func (w *WebSocketConn) OnClose(cb func(deviceId string)) {
	w.onCloseCbList = append(w.onCloseCbList, cb)
}

func (w *WebSocketConn) GetDeviceID() string {
	return w.deviceID
}

func (w *WebSocketConn) GetRemoteAddr() string {
	// 首先尝试从 X-Real-IP header 获取真实IP
	if realIP := w.header.Get("X-Real-IP"); realIP != "" {
		return realIP
	}

	// 然后尝试从 X-Forwarded-For header 获取真实IP
	if forwardedFor := w.header.Get("X-Forwarded-For"); forwardedFor != "" {
		// X-Forwarded-For 可能包含多个IP，取第一个
		ips := strings.Split(forwardedFor, ",")
		if len(ips) > 0 {
			return strings.TrimSpace(ips[0])
		}
	}

	// 最后使用 RemoteAddr
	return w.conn.RemoteAddr().String()
}

func (w *WebSocketConn) GetExternalAddr() string {
	// 优先检查代理透传的对外访问地址
	if external := w.header.Get("X-External-Addr"); external != "" {
		external = strings.TrimSpace(external)
		if external != "" {
			if hasScheme(external) {
				return external
			}
			return fmt.Sprintf("%s://%s", w.detectExternalScheme(), external)
		}
	}

	// 兼容常见代理头
	if forwardedHost := w.header.Get("X-Forwarded-Host"); forwardedHost != "" {
		parts := strings.Split(forwardedHost, ",")
		if len(parts) > 0 {
			host := strings.TrimSpace(parts[0])
			if host != "" {
				if hasScheme(host) {
					return host
				}
				return fmt.Sprintf("%s://%s", w.detectExternalScheme(), host)
			}
		}
	}

	if host := w.header.Get("Host"); host != "" {
		host = strings.TrimSpace(host)
		if host != "" {
			if hasScheme(host) {
				return host
			}
			return fmt.Sprintf("%s://%s", w.detectExternalScheme(), host)
		}
	}

	if w.conn == nil {
		return ""
	}
	local := w.conn.LocalAddr().String()
	if local == "" {
		return ""
	}
	if hasScheme(local) {
		return local
	}
	return fmt.Sprintf("%s://%s", w.detectExternalScheme(), local)
}

func (w *WebSocketConn) GetTransportType() string {
	return types.TransportTypeWebsocket
}

func (w *WebSocketConn) GetData(key string) (interface{}, error) {
	return nil, errors.New("not implemented")
}

func (w *WebSocketConn) CloseAudioChannel() error {
	return nil
}

func (w *WebSocketConn) SetProtocolVersion(version int) {
	normalized := normalizeProtocolVersion(version)
	w.Lock()
	if normalized != w.protocolVersion {
		log.Infof("WebSocket连接协议版本更新，设备ID: %s, 版本: v%d", w.deviceID, normalized)
	}
	w.protocolVersion = normalized
	w.Unlock()
}

func (w *WebSocketConn) GetProtocolVersion() int {
	return w.getProtocolVersion()
}

func (w *WebSocketConn) getProtocolVersion() int {
	w.RLock()
	defer w.RUnlock()
	return w.protocolVersion
}

func (w *WebSocketConn) decodeBinaryPayload(frame []byte) ([]byte, binaryPayloadKind, error) {
	switch w.getProtocolVersion() {
	case 2:
		return w.decodeBinaryProtocol2(frame)
	case 3:
		return w.decodeBinaryProtocol3(frame)
	default:
		return frame, binaryPayloadKindAudio, nil
	}
}

func (w *WebSocketConn) decodeBinaryProtocol2(frame []byte) ([]byte, binaryPayloadKind, error) {
	const headerSize = 16
	if len(frame) < headerSize {
		return nil, binaryPayloadKindAudio, fmt.Errorf("v2帧长度不足: %d", len(frame))
	}
	msgType := binary.BigEndian.Uint16(frame[2:4])
	payloadSize := binary.BigEndian.Uint32(frame[12:16])
	if int(payloadSize) > len(frame)-headerSize {
		return nil, binaryPayloadKindAudio, fmt.Errorf("v2帧负载长度异常: declared=%d, actual=%d", payloadSize, len(frame)-headerSize)
	}
	payload := frame[headerSize : headerSize+int(payloadSize)]
	switch msgType {
	case 0:
		return payload, binaryPayloadKindAudio, nil
	case 1:
		return payload, binaryPayloadKindCmd, nil
	default:
		return nil, binaryPayloadKindAudio, fmt.Errorf("不支持的v2消息类型: %d", msgType)
	}
}

func (w *WebSocketConn) decodeBinaryProtocol3(frame []byte) ([]byte, binaryPayloadKind, error) {
	const headerSize = 4
	if len(frame) < headerSize {
		return nil, binaryPayloadKindAudio, fmt.Errorf("v3帧长度不足: %d", len(frame))
	}
	msgType := frame[0]
	payloadSize := binary.BigEndian.Uint16(frame[2:4])
	if int(payloadSize) > len(frame)-headerSize {
		return nil, binaryPayloadKindAudio, fmt.Errorf("v3帧负载长度异常: declared=%d, actual=%d", payloadSize, len(frame)-headerSize)
	}
	payload := frame[headerSize : headerSize+int(payloadSize)]
	switch msgType {
	case 0:
		return payload, binaryPayloadKindAudio, nil
	case 1:
		return payload, binaryPayloadKindCmd, nil
	default:
		return nil, binaryPayloadKindAudio, fmt.Errorf("不支持的v3消息类型: %d", msgType)
	}
}

func (w *WebSocketConn) packBinaryPayload(version int, audio []byte) []byte {
	switch normalizeProtocolVersion(version) {
	case 2:
		return packBinaryProtocol2(audio)
	case 3:
		return packBinaryProtocol3(audio)
	default:
		return audio
	}
}

func packBinaryProtocol2(payload []byte) []byte {
	const headerSize = 16
	head := make([]byte, headerSize)
	binary.BigEndian.PutUint16(head[0:2], 2)
	binary.BigEndian.PutUint16(head[2:4], 0) // 0=audio
	binary.BigEndian.PutUint32(head[4:8], 0)
	binary.BigEndian.PutUint32(head[8:12], uint32(time.Now().UnixMilli()))
	binary.BigEndian.PutUint32(head[12:16], uint32(len(payload)))
	return append(head, payload...)
}

func packBinaryProtocol3(payload []byte) []byte {
	head := make([]byte, 4)
	head[0] = 0 // 0=audio
	head[1] = 0
	binary.BigEndian.PutUint16(head[2:4], uint16(len(payload)))
	return append(head, payload...)
}

func normalizeProtocolVersion(version int) int {
	switch version {
	case 2, 3:
		return version
	default:
		return 1
	}
}

func (w *WebSocketConn) enqueueBinaryPayload(kind binaryPayloadKind, payload []byte) {
	switch kind {
	case binaryPayloadKindCmd:
		select {
		case w.recvCmdChan <- payload:
		default:
			log.Errorf("recv cmd channel is full")
		}
	case binaryPayloadKindAudio:
		select {
		case w.recvAudioChan <- payload:
		default:
			log.Errorf("recv audio channel is full")
		}
	}
}

func hasScheme(value string) bool {
	return strings.Contains(value, "://")
}

func (w *WebSocketConn) detectExternalScheme() string {
	candidates := []string{
		w.header.Get("X-External-Scheme"),
		w.header.Get("X-Forwarded-Proto"),
		w.header.Get("X-Forwarded-Scheme"),
	}

	for _, candidate := range candidates {
		if candidate = strings.TrimSpace(candidate); candidate != "" {
			return normalizeScheme(candidate)
		}
	}

	if origin := strings.TrimSpace(w.header.Get("Origin")); origin != "" {
		if u, err := url.Parse(origin); err == nil && u.Scheme != "" {
			return normalizeScheme(u.Scheme)
		}
	}

	return normalizeScheme("")
}

func normalizeScheme(scheme string) string {
	scheme = strings.ToLower(strings.TrimSpace(scheme))
	switch scheme {
	case "wss":
		return "https"
	case "ws":
		return "http"
	case "":
		return "http"
	default:
		return scheme
	}
}
