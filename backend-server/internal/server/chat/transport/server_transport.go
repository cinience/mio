package transport

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"backend-server/internal/adapters/manager"
	types_audio "backend-server/internal/domain/audio"
	log "backend-server/internal/infrastructure/logger"
	client "backend-server/internal/server/chat/session/state"
	transporttypes "backend-server/internal/server/transport/types"
)

const (
	autoResumeCheckInterval = 200 * time.Millisecond
	autoResumeTimeout       = 2 * time.Second
)

// Session defines callbacks the ServerTransport expects from a chat session.
type Session interface {
	GetManagerAPIService() manager_api.ManagerAPIService
	HandleListenStart(*client.ClientMessage) error
	ToolInFlight() bool
	LLMInFlight() bool
	IsClosed() bool
	MarkDownstreamText()
	MarkDownstreamAudio()
	LogServerTextMessage(*transporttypes.ServerMessage)
	LogDataMessage(direction, messageType string, size int, payload any)
	RecordDownstreamAudio([]byte)
}

// ServerTransport handles sending messages to the client via the transport layer
// (原ServerMsgService)
type ServerTransport struct {
	transport               transporttypes.IConn
	clientState             *client.ClientState
	session                 Session
	McpRecvMsgChan          chan []byte
	closed                  bool
	mu                      sync.Mutex
	listenAutoResumeEnabled bool
	autoResumeMu            sync.Mutex
	autoResumeWanted        bool
	autoResumeInProgress    bool
	lastAutoResumeAt        time.Time
	autoResumeDeadline      time.Time
	autoResumeWatcherActive bool
}

func NewServerTransport(transport transporttypes.IConn, clientState *client.ClientState, autoResumeEnabled bool) *ServerTransport {
	return &ServerTransport{
		transport:               transport,
		clientState:             clientState,
		McpRecvMsgChan:          make(chan []byte, 100),
		listenAutoResumeEnabled: autoResumeEnabled,
	}
}

func (s *ServerTransport) AttachSession(sess Session) {
	s.mu.Lock()
	s.session = sess
	s.mu.Unlock()
}

// ProtocolVersion returns the currently negotiated protocol version if supported by the transport.
func (s *ServerTransport) ProtocolVersion() int {
	if aware, ok := s.transport.(transporttypes.ProtocolVersionAware); ok {
		return aware.GetProtocolVersion()
	}
	return 0
}

// SetProtocolVersion updates the protocol version on the underlying transport when supported.
func (s *ServerTransport) SetProtocolVersion(version int) {
	if aware, ok := s.transport.(transporttypes.ProtocolVersionAware); ok {
		aware.SetProtocolVersion(version)
	}
}

func (s *ServerTransport) GetManagerAPIService() manager_api.ManagerAPIService {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.session == nil {
		return nil
	}
	return s.session.GetManagerAPIService()
}

func (s *ServerTransport) SendTtsStart() error {
	message := transporttypes.ServerMessage{
		Type:      transporttypes.ServerMessageTypeTts,
		State:     transporttypes.MessageStateStart,
		SessionID: s.clientState.SessionID,
	}
	bytes, err := json.Marshal(message)
	if err != nil {
		return err
	}
	if err := s.transport.SendCmd(bytes); err != nil {
		return err
	}
	if s.session != nil {
		s.session.MarkDownstreamText()
		s.session.LogServerTextMessage(&message)
	}
	s.clientState.SetTtsStart(true)
	return nil
}

func (s *ServerTransport) SendTtsStop() error {
	message := transporttypes.ServerMessage{
		Type:      transporttypes.ServerMessageTypeTts,
		State:     transporttypes.MessageStateStop,
		SessionID: s.clientState.SessionID,
	}
	bytes, err := json.Marshal(message)
	if err != nil {
		return err
	}
	if err := s.transport.SendCmd(bytes); err != nil {
		return err
	}
	if s.session != nil {
		s.session.MarkDownstreamText()
		s.session.LogServerTextMessage(&message)
	}
	s.clientState.SetTtsStart(false)
	s.autoResumeListening("tts-stop")
	return nil
}

func (s *ServerTransport) autoResumeListening(reason string) {
	if s.clientState == nil {
		return
	}
	deviceID := s.clientState.DeviceID
	listenMode := s.clientState.ListenMode
	if !s.listenAutoResumeEnabled {
		log.Infof("SendTtsStop: auto resume disabled via config, device=%s (reason=%s)", deviceID, reason)
		return
	}
	sess := s.session
	switch listenMode {
	case "manual":
		log.Infof("SendTtsStop: skip auto resume in manual mode, device=%s (reason=%s)", deviceID, reason)
		return
	}
	if sess == nil {
		log.Warnf("SendTtsStop: auto resume enabled but session missing, device=%s listenMode=%s reason=%s", deviceID, listenMode, reason)
		return
	}
	if sess.IsClosed() {
		log.Warnf("SendTtsStop: session already closed, ignore auto resume, device=%s listenMode=%s reason=%s", deviceID, listenMode, reason)
		return
	}
	s.markAutoResumePending()
	s.tryAutoResume(reason, false)
}

func (s *ServerTransport) markAutoResumePending() {
	now := time.Now()
	s.autoResumeMu.Lock()
	s.autoResumeWanted = true
	if s.autoResumeDeadline.IsZero() || now.After(s.autoResumeDeadline) {
		s.autoResumeDeadline = now.Add(autoResumeTimeout)
	}
	if !s.autoResumeWatcherActive {
		s.autoResumeWatcherActive = true
		go s.autoResumeWatcher()
	}
	s.autoResumeMu.Unlock()
}

func (s *ServerTransport) autoResumeWatcher() {
	ticker := time.NewTicker(autoResumeCheckInterval)
	defer ticker.Stop()

	for {
		// capture session pointer once per loop; nil check handled below
		sess := s.session

		s.autoResumeMu.Lock()
		pending := s.autoResumeWanted
		deadline := s.autoResumeDeadline
		s.autoResumeMu.Unlock()

		if !pending {
			s.autoResumeMu.Lock()
			s.autoResumeWatcherActive = false
			s.autoResumeMu.Unlock()
			return
		}

		if s.clientState == nil || s.session == nil || s.session.IsClosed() {
			s.clearAutoResumeWanted()
			s.autoResumeMu.Lock()
			s.autoResumeWatcherActive = false
			s.autoResumeMu.Unlock()
			return
		}

		// 如果还有 LLM/工具在执行，延长 watch 窗口，避免强制自动恢复打断任务
		if sess != nil && (sess.ToolInFlight() || sess.LLMInFlight()) {
			now := time.Now()
			s.autoResumeMu.Lock()
			if s.autoResumeDeadline.IsZero() || now.After(s.autoResumeDeadline) {
				s.autoResumeDeadline = now.Add(autoResumeTimeout)
			}
			s.autoResumeMu.Unlock()
			<-ticker.C
			continue
		}

		if s.tryAutoResume("auto-resume-watch", false) {
			continue
		}

		if !deadline.IsZero() && time.Now().After(deadline) {
			s.tryAutoResume("auto-resume-watchdog", true)
		}

		<-ticker.C
	}
}

func (s *ServerTransport) clearAutoResumeWanted() {
	s.autoResumeMu.Lock()
	s.autoResumeWanted = false
	s.autoResumeDeadline = time.Time{}
	s.autoResumeMu.Unlock()
}

func (s *ServerTransport) tryAutoResume(reason string, force bool) bool {
	s.mu.Lock()
	sess := s.session
	s.mu.Unlock()

	if s.clientState == nil || sess == nil {
		s.clearAutoResumeWanted()
		return false
	}

	mode := s.clientState.ListenMode
	deviceID := s.clientState.DeviceID

	if sess.IsClosed() {
		s.clearAutoResumeWanted()
		return false
	}

	if !force {
		if sess.ToolInFlight() || sess.LLMInFlight() {
			return false
		}
		if tm := s.clientState.TTSManager; tm != nil && !tm.QueueEmpty() {
			return false
		}
	}

	if !s.beginAutoResume(force) {
		return false
	}

	s.clientState.SetStatus(client.ClientStatusListening)
	if mode == "realtime" {
		log.Infof("SendTtsStop: realtime auto resume triggered, device=%s reason=%s", deviceID, reason)
	} else {
		log.Infof("SendTtsStop: auto resume listening for device=%s (mode=%s, reason=%s)", deviceID, mode, reason)
	}

	s.clearAutoResumeWanted()

	go func(session Session, mode, devID string) {
		defer s.endAutoResume()
		log.Infof("auto resume listen start: device=%s mode=%s reason=%s status=%s session=%s",
			devID, mode, reason, s.clientState.GetStatus(), s.clientState.SessionID)
		if err := session.HandleListenStart(&client.ClientMessage{
			DeviceID:  devID,
			Mode:      mode,
			State:     transporttypes.MessageStateStart,
			Transport: transporttypes.TransportTypeServer,
		}); err != nil {
			log.Errorf("SendTtsStop: auto HandleListenStart failed for device=%s: %v", devID, err)
		}
	}(sess, mode, deviceID)

	return true
}

func (s *ServerTransport) beginAutoResume(force bool) bool {
	s.autoResumeMu.Lock()
	defer s.autoResumeMu.Unlock()
	if s.autoResumeInProgress {
		return false
	}
	if !force && !s.lastAutoResumeAt.IsZero() && time.Since(s.lastAutoResumeAt) < time.Second {
		return false
	}
	s.autoResumeInProgress = true
	s.lastAutoResumeAt = time.Now()
	return true
}

func (s *ServerTransport) endAutoResume() {
	s.autoResumeMu.Lock()
	s.autoResumeInProgress = false
	s.autoResumeMu.Unlock()
}

func (s *ServerTransport) SendMqttGoodbye() error {
	message := transporttypes.ServerMessage{
		Type:      transporttypes.ServerMessageTypeGoodBye,
		State:     transporttypes.MessageStateStop,
		SessionID: s.clientState.SessionID,
	}
	bytes, err := json.Marshal(message)
	if err != nil {
		return err
	}
	if err := s.transport.SendCmd(bytes); err != nil {
		return err
	}
	if s.session != nil {
		s.session.MarkDownstreamText()
		s.session.LogServerTextMessage(&message)
	}
	return nil
}

func (s *ServerTransport) SendHello(transportType string, audioFormat *types_audio.AudioFormat, udpConfig interface{}) error {
	var udp *transporttypes.UdpConfig
	if udpConfig != nil {
		cfg, ok := udpConfig.(*transporttypes.UdpConfig)
		if !ok {
			return fmt.Errorf("invalid udp config type: %T", udpConfig)
		}
		udp = cfg
	}

	message := transporttypes.ServerMessage{
		Type:        transporttypes.MessageTypeHello,
		Text:        "欢迎使用小智服务器",
		SessionID:   s.clientState.SessionID,
		Transport:   transportType,
		AudioFormat: audioFormat,
		Udp:         udp,
	}
	bytes, err := json.Marshal(message)
	if err != nil {
		return err
	}
	if err := s.transport.SendCmd(bytes); err != nil {
		return err
	}
	if s.session != nil {
		s.session.MarkDownstreamText()
		s.session.LogServerTextMessage(&message)
	}
	return nil
}

func (s *ServerTransport) SendGoodbye(reason string) error {
	message := transporttypes.ServerMessage{
		Type:      transporttypes.ServerMessageTypeGoodBye,
		Text:      reason,
		SessionID: s.clientState.SessionID,
	}
	bytes, err := json.Marshal(message)
	if err != nil {
		return err
	}
	if err := s.transport.SendCmd(bytes); err != nil {
		return err
	}
	if s.session != nil {
		s.session.MarkDownstreamText()
		s.session.LogServerTextMessage(&message)
	}
	return nil
}

func (s *ServerTransport) SendIot(clientMsg *client.ClientMessage) error {
	resp := transporttypes.ServerMessage{
		Type:      transporttypes.ServerMessageTypeIot,
		Text:      clientMsg.Text,
		SessionID: s.clientState.SessionID,
		State:     transporttypes.MessageStateSuccess,
	}
	bytes, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	if err := s.transport.SendCmd(bytes); err != nil {
		return err
	}
	if s.session != nil {
		s.session.MarkDownstreamText()
		s.session.LogServerTextMessage(&resp)
	}
	return nil
}

func (s *ServerTransport) SendAsrResult(text string) error {
	return s.SendAsrResultWithState(text, "")
}

func (s *ServerTransport) SendAsrResultWithState(text, state string) error {
	resp := transporttypes.ServerMessage{
		Type:      transporttypes.ServerMessageTypeStt,
		Text:      text,
		SessionID: s.clientState.SessionID,
	}
	if state != "" {
		resp.State = state
	}
	bytes, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	if err := s.transport.SendCmd(bytes); err != nil {
		return err
	}
	if s.session != nil {
		s.session.MarkDownstreamText()
		s.session.LogServerTextMessage(&resp)
	}
	return nil
}

// SendListenStop 发送 listen stop 消息通知设备停止监听
func (s *ServerTransport) SendListenStop() error {
	message := transporttypes.ServerMessage{
		Type:      transporttypes.MessageTypeListen, // 使用客户端消息类型，因为这是双向协议
		State:     transporttypes.MessageStateStop,
		SessionID: s.clientState.SessionID,
	}
	bytes, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("序列化 listen stop 消息失败: %w", err)
	}
	if err := s.transport.SendCmd(bytes); err != nil {
		return fmt.Errorf("发送 listen stop 消息失败: %w", err)
	}
	if s.session != nil {
		s.session.MarkDownstreamText()
		s.session.LogServerTextMessage(&message)
	}
	log.Infof("已发送 listen stop 消息通知设备停止监听, device=%s", s.clientState.DeviceID)
	return nil
}

func (s *ServerTransport) SendSentenceStart(text string) error {
	response := transporttypes.ServerMessage{
		Type:      transporttypes.ServerMessageTypeTts,
		State:     transporttypes.MessageStateSentenceStart,
		Text:      text,
		SessionID: s.clientState.SessionID,
	}
	bytes, err := json.Marshal(response)
	if err != nil {
		return err
	}
	if err := s.transport.SendCmd(bytes); err != nil {
		return err
	}
	if s.session != nil {
		s.session.MarkDownstreamText()
		s.session.LogServerTextMessage(&response)
	}
	s.clientState.SetStatus(client.ClientStatusTTSStart)
	return nil
}

func (s *ServerTransport) SendSentenceEnd(text string) error {
	response := transporttypes.ServerMessage{
		Type:      transporttypes.ServerMessageTypeTts,
		State:     transporttypes.MessageStateSentenceEnd,
		Text:      text,
		SessionID: s.clientState.SessionID,
	}
	bytes, err := json.Marshal(response)
	if err != nil {
		return err
	}
	if err := s.transport.SendCmd(bytes); err != nil {
		return err
	}
	if s.session != nil {
		s.session.MarkDownstreamText()
		s.session.LogServerTextMessage(&response)
	}
	s.clientState.SetStatus(client.ClientStatusTTSStart)
	return nil
}

func (s *ServerTransport) SendCmd(cmdBytes []byte) error {
	return s.transport.SendCmd(cmdBytes)
}

func (s *ServerTransport) SendAudio(audio []byte) error {
	if err := s.transport.SendAudio(audio); err != nil {
		return err
	}
	if s.session != nil {
		s.session.MarkDownstreamAudio()
		if len(audio) > 0 {
			s.session.LogDataMessage("server", "audio", len(audio), nil)
			s.session.RecordDownstreamAudio(audio)
		}
	}
	return nil
}

func (s *ServerTransport) GetTransportType() string {
	return s.transport.GetTransportType()
}

func (s *ServerTransport) GetRemoteAddr() string {
	if s.transport == nil {
		return ""
	}
	return s.transport.GetRemoteAddr()
}

func (s *ServerTransport) GetExternalAddr() string {
	return s.transport.GetExternalAddr()
}

func (s *ServerTransport) GetData(key string) (interface{}, error) {
	return s.transport.GetData(key)
}

func (s *ServerTransport) CloseAudioChannel() {
	if s.transport != nil {
		s.transport.CloseAudioChannel()
	}
}

func (s *ServerTransport) MCPRecvChan() chan []byte {
	return s.McpRecvMsgChan
}

func (s *ServerTransport) SendMcpMsg(payload []byte) error {
	response := transporttypes.ServerMessage{
		Type:      transporttypes.MessageTypeMcp,
		SessionID: s.clientState.SessionID,
		PayLoad:   payload,
	}
	bytes, err := json.Marshal(response)
	if err != nil {
		return err
	}
	if err := s.transport.SendCmd(bytes); err != nil {
		return err
	}
	if s.session != nil {
		s.session.MarkDownstreamText()
		s.session.LogServerTextMessage(&response)
	}
	return nil
}

func (s *ServerTransport) RecvMcpMsg(ctx context.Context, timeOut int) ([]byte, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case msg, ok := <-s.McpRecvMsgChan:
		if !ok {
			return nil, fmt.Errorf("transport is closed")
		}
		return msg, nil
	case <-time.After(time.Duration(timeOut) * time.Millisecond):
		return nil, fmt.Errorf("mcp 接收消息超时")
	}
}

func (s *ServerTransport) DeliverMcpPayload(payload []byte) {
	if s.McpRecvMsgChan == nil {
		return
	}
	s.McpRecvMsgChan <- payload
}

func (s *ServerTransport) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil // Already closed
	}

	s.closed = true

	if s.transport.GetTransportType() == transporttypes.TransportTypeMqttUdp {
		s.SendMqttGoodbye()
	}

	close(s.McpRecvMsgChan)
	return s.transport.Close()
}

func (s *ServerTransport) RecvAudio(ctx context.Context, timeOut int) ([]byte, error) {
	return s.transport.RecvAudio(ctx, timeOut)
}

func (s *ServerTransport) RecvCmd(ctx context.Context, timeOut int) ([]byte, error) {
	return s.transport.RecvCmd(ctx, timeOut)
}
