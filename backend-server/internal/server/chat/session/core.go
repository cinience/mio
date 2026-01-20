package session

import (
	"backend-server/internal/domain/tools/fc_factory"
	apperrors "backend-server/internal/infrastructure/errors"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"runtime/debug"
	"strings"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/cloudwego/eino/schema"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/errgroup"

	manager_api "backend-server/internal/adapters/manager"
	"backend-server/internal/app/service"
	"backend-server/internal/config"
	asr_types "backend-server/internal/domain/asr/types"
	"backend-server/internal/domain/audio"
	llm_common "backend-server/internal/domain/llm/common"
	domainmcp "backend-server/internal/domain/mcp"
	"backend-server/internal/domain/meetingminutes"
	"backend-server/internal/domain/tts"
	log "backend-server/internal/infrastructure/logger"
	"backend-server/internal/server/channels"
	managers "backend-server/internal/server/chat/managers"
	chatmcp "backend-server/internal/server/chat/mcp"
	chatmetrics "backend-server/internal/server/chat/metrics"
	monitoring "backend-server/internal/server/chat/monitoring"
	client "backend-server/internal/server/chat/session/state"
	transport "backend-server/internal/server/chat/transport"
	chatutils "backend-server/internal/server/chat/utils"
	"backend-server/internal/server/observability"
	transporttypes "backend-server/internal/server/transport/types"
)

type AsrResponseChannelItem struct {
	ctx     context.Context
	text    string
	roundID string
	span    trace.Span
	metrics *chatmetrics.ConversationMetrics
}

const minRealtimeSilenceThresholdMs int64 = 800

type ChatSession struct {
	clientState     *client.ClientState
	asrManager      *managers.ASRManager
	ttsManager      *managers.TTSManager
	llmManager      *managers.LLMManager
	stateMachine    *managers.SessionStateMachine
	serverTransport *transport.ServerTransport
	sessionLogger   *monitoring.SessionLogger
	services        *service.Registry
	loopGroup       *errgroup.Group
	loopCtx         context.Context
	loopsDone       chan struct{}
	errorHandler    *apperrors.Handler

	ctx    context.Context
	cancel context.CancelFunc

	chatTextQueue *channels.ManagedQueue[AsrResponseChannelItem]

	roundSeq        uint64
	roundStartTime  time.Time
	roundAudioBytes int
	roundHasAudio   bool

	monitor  *SessionMonitor
	pipeline *conversationPipeline

	lastActivityTime        atomic.Int64
	lastUpstreamAudioTime   atomic.Int64
	lastDownstreamAudioTime atomic.Int64
	lastUpstreamTextTime    atomic.Int64
	lastDownstreamTextTime  atomic.Int64
	closed                  atomic.Bool
	onClose                 func()

	lastPartialAt atomic.Int64
	metrics       *chatmetrics.ConversationMetrics

	managerUploader *managerChatUploader
	meetingMinutes  *meetingMinutesState
}

type ChatSessionOption func(*ChatSession)

func WithSessionMonitorOption(m *SessionMonitor) ChatSessionOption {
	return func(s *ChatSession) {
		s.monitor = m
	}
}

func WithSessionOnClose(onClose func()) ChatSessionOption {
	return func(s *ChatSession) {
		s.onClose = onClose
	}
}

func WithSessionServiceRegistry(reg *service.Registry) ChatSessionOption {
	return func(s *ChatSession) {
		s.services = reg
	}
}

func NewChatSession(clientState *client.ClientState, serverTransport *transport.ServerTransport, opts ...ChatSessionOption) *ChatSession {
	cfg := config.GetConfig()
	queueCfg := channels.QueueConfig{
		Name:       "session_chat_text",
		Capacity:   cfg.Channels.Session.ChatTextQueue,
		DropPolicy: channels.ParseDropPolicy(cfg.Channels.DropPolicy),
		Timeout:    time.Duration(cfg.Channels.Timeout) * time.Second,
	}

	s := &ChatSession{
		clientState:     clientState,
		serverTransport: serverTransport,
		chatTextQueue:   channels.NewManagedQueue[AsrResponseChannelItem](queueCfg),
		meetingMinutes:  newMeetingMinutesState(cfg.MeetingMinutes.StartPhrase, cfg.MeetingMinutes.StopPhrase),
	}
	for _, opt := range opts {
		opt(s)
	}

	if s.services == nil {
		s.services = service.DefaultRegistry()
	}

	s.stateMachine = managers.NewSessionStateMachine(clientState, serverTransport)
	if s.stateMachine != nil {
		s.stateMachine.SetStateChangeHook(func(from, to managers.SessionState) {
			s.touchActivity(time.Now())
		})
	}

	s.errorHandler = apperrors.NewHandler(observability.Server())
	s.errorHandler.RegisterHandler(apperrors.ErrorTypeNetwork, s.handleNetworkError)
	s.errorHandler.RegisterHandler(apperrors.ErrorTypeAI, s.handleAIError)
	s.errorHandler.RegisterHandler(apperrors.ErrorTypeBusiness, s.handleBusinessError)

	s.asrManager = managers.NewASRManager(clientState, serverTransport)
	s.ttsManager = managers.NewTTSManager(clientState, serverTransport, s.stateMachine)
	s.llmManager = managers.NewLLMManager(clientState, serverTransport, s.ttsManager, s.stateMachine, s)
	// 反向暴露给 transport 检查队列状态
	clientState.TTSManager = s.ttsManager
	s.asrManager.SetInterruptHandler(s.onRealtimeInterrupt)
	s.pipeline = newConversationPipeline(s)

	if serverTransport != nil {
		serverTransport.AttachSession(s)
	}

	s.managerUploader = newManagerChatUploader(s)

	return s
}

func (s *ChatSession) Start(pctx context.Context) error {
	s.ctx, s.cancel = context.WithCancel(pctx)
	s.touchActivity(time.Now())

	if metrics := observability.Server(); metrics != nil {
		metrics.TrackSessionStart()
	}

	err := s.InitAsrLlmTts()
	if err != nil {
		log.Errorf("初始化ASR/LLM/TTS失败: %v", err)
		return err
	}

	// 初始化MCP连接
	s.InitMcp()

	s.loopGroup, s.loopCtx = errgroup.WithContext(s.ctx)
	s.loopsDone = make(chan struct{})

	s.launchLoop("cmd_message_loop", func(ctx context.Context) error {
		s.CmdMessageLoop(ctx)
		return nil
	})
	s.launchLoop("audio_message_loop", func(ctx context.Context) error {
		s.AudioMessageLoop(ctx)
		return nil
	})
	s.launchLoop("chat_processing_loop", func(ctx context.Context) error {
		s.processChatText(ctx)
		return nil
	})
	s.launchLoop("llm_manager_loop", func(ctx context.Context) error {
		s.llmManager.Start(ctx)
		return nil
	})
	s.launchLoop("tts_manager_loop", func(ctx context.Context) error {
		s.ttsManager.Start(ctx)
		return nil
	})

	if s.monitor != nil {
		s.monitor.Add(s)
	}

	err = s.loopGroup.Wait()
	close(s.loopsDone)
	if err != nil && !errors.Is(err, context.Canceled) {
		log.Warnf("chat session %s terminated with error: %v", s.clientState.DeviceID, err)
	}
	return err
}

func (s *ChatSession) launchLoop(name string, fn func(ctx context.Context) error) {
	if s.loopGroup == nil || s.loopCtx == nil {
		go fn(s.ctx)
		return
	}
	s.loopGroup.Go(func() error {
		defer func() {
			if r := recover(); r != nil {
				log.Errorf("chat session loop %s panic: %v\n%s", name, r, string(debug.Stack()))
			}
		}()
		return fn(s.loopCtx)
	})
}

// InitAsrLlmTts 在mqtt 收到type: listen, state: start后进行
func (s *ChatSession) InitAsrLlmTts() error {
	ttsConfig := s.clientState.DeviceConfig.Tts
	ttsProvider, err := tts.GetTTSProvider(ttsConfig.Provider, ttsConfig.Config)
	if err != nil {
		return fmt.Errorf("创建 TTS 提供者失败: %v", err)
	}
	s.clientState.TTSProvider = ttsProvider

	tools := fc_factory.GetFCTools(s.clientState.DeviceConfig.Plugins, s)
	if err := s.clientState.InitLlm(tools); err != nil {
		return fmt.Errorf("初始化LLM失败: %v", err)
	}
	if err := s.clientState.InitAsr(); err != nil {
		return fmt.Errorf("初始化ASR失败: %v", err)
	}
	s.clientState.SetAsrPcmFrameSize(s.clientState.InputAudioFormat.SampleRate, s.clientState.InputAudioFormat.Channels, s.clientState.InputAudioFormat.FrameDuration)

	return nil
}

// TemporaryChangeVoice updates the active TTS voice for the current session only.
func (s *ChatSession) TemporaryChangeVoice(ctx context.Context, options *tts.ChangeVoiceOptions) error {
	if s == nil || s.clientState == nil {
		return fmt.Errorf("chat session not initialized")
	}
	if options == nil {
		return fmt.Errorf("change voice options cannot be nil")
	}
	if s.clientState.TTSProvider == nil {
		return fmt.Errorf("tts provider not initialized")
	}

	if err := s.clientState.TTSProvider.ChangeVoice(ctx, options); err != nil {
		return err
	}

	if s.clientState.DeviceConfig.Tts.Config == nil {
		s.clientState.DeviceConfig.Tts.Config = make(map[string]interface{})
	}

	if options.Voice != "" {
		s.clientState.DeviceConfig.Tts.Config["voice"] = options.Voice
	}
	if options.VoiceID != "" {
		s.clientState.DeviceConfig.Tts.Config["tts_voice_id"] = options.VoiceID
	}
	if options.ModelID != "" {
		s.clientState.DeviceConfig.Tts.Config["tts_model_id"] = options.ModelID
	}
	if len(options.Params) > 0 {
		s.clientState.DeviceConfig.Tts.Config["voice_params"] = options.Params
	}

	return nil
}

func (s *ChatSession) GetRemoteAddr() string {
	// 通过ServerTransport访问底层连接对象来获取真实IP
	if s.serverTransport != nil {
		realIP := s.serverTransport.GetRemoteAddr()
		if realIP != "" {
			return realIP
		}
	}
	// 如果无法获取真实IP，回退到clientState中的RemoteAddr
	if s.clientState != nil {
		return s.clientState.RemoteAddr
	}
	return ""
}

func (s *ChatSession) GetMetadata() map[string]string {
	md := make(map[string]string)
	// Session相关元数据
	md["remote_addr"] = s.GetRemoteAddr()
	md["local_addr"] = ""
	// Agent相关元数据
	md["agent_name"] = ""
	if s != nil && s.clientState != nil && s.clientState.DeviceConfig.Metadata != nil {
		for key, value := range s.clientState.DeviceConfig.Metadata {
			if key == "" || value == nil {
				continue
			}
			switch v := value.(type) {
			case string:
				md[key] = v
			case fmt.Stringer:
				md[key] = v.String()
			default:
				md[key] = fmt.Sprintf("%v", v)
			}
		}
	}
	return md
}

func (s *ChatSession) GetManagerAPIService() manager_api.ManagerAPIService {
	if s == nil || s.services == nil {
		return nil
	}
	return s.services.ManagerAPIService()
}

func (s *ChatSession) GetLogger() *slog.Logger {
	// 返回全局的 slog logger
	return log.GetLogger()
}

func (s *ChatSession) ChangeSystemPrompt(prompt string) error {
	return nil
}

func (s *ChatSession) GetAudioFormat() (int, int, string) {
	if s == nil || s.clientState == nil {
		return 0, 0, ""
	}
	format := s.clientState.OutputAudioFormat
	return format.SampleRate, format.FrameDuration, format.Format
}

func (s *ChatSession) GetUserAudio() ([]byte, string) {
	if s == nil || s.managerUploader == nil {
		return nil, ""
	}
	audio := s.managerUploader.captureUserAudio()
	if len(audio) == 0 {
		return nil, ""
	}
	return audio, "audio/wav"
}

func (s *ChatSession) StreamAudio(ctx context.Context, description string, audioChan chan []byte) (<-chan error, error) {
	if s == nil {
		return nil, fmt.Errorf("chat session not initialized")
	}
	if s.serverTransport == nil || s.ttsManager == nil {
		return nil, fmt.Errorf("audio streaming unavailable")
	}
	if audioChan == nil {
		return nil, fmt.Errorf("audio stream cannot be nil")
	}
	if ctx == nil {
		ctx = s.clientState.GetSessionCtx()
	}

	playText := strings.TrimSpace(description)
	if playText == "" {
		playText = "正在播放音频"
	}

	if err := s.serverTransport.SendTtsStart(); err != nil {
		log.Warnf("发送TTS开始指令失败: %v", err)
	}
	if err := s.serverTransport.SendSentenceStart(playText); err != nil {
		log.Warnf("发送音频开始描述失败: %v", err)
	}

	done := make(chan error, 1)

	go func() {
		defer func() {
			if err := s.serverTransport.SendSentenceEnd(playText); err != nil {
				log.Warnf("发送音频结束描述失败: %v", err)
			}
			frameDuration := s.clientState.OutputAudioFormat.FrameDuration
			if frameDuration <= 0 {
				frameDuration = 60
			}
			const extraFrames = 6
			delay := time.Duration(frameDuration*extraFrames) * time.Millisecond
			time.AfterFunc(delay, func() {
				if err := s.serverTransport.SendTtsStop(); err != nil {
					log.Warnf("发送TTS结束指令失败: %v", err)
				}
			})
			close(done)
		}()

		if err := s.ttsManager.SendTTSAudio(ctx, audioChan, true); err != nil {
			log.Errorf("播放音频流失败: %v", err)
			done <- err
			return
		}
		done <- nil
	}()

	return done, nil
}

func (s *ChatSession) StartMeetingMinutes() error {
	if s == nil || s.meetingMinutes == nil {
		return fmt.Errorf("meeting minutes not initialized")
	}
	return s.meetingMinutes.Start(time.Now())
}

func (s *ChatSession) StopMeetingMinutes() (*meetingminutes.Snapshot, error) {
	if s == nil || s.meetingMinutes == nil {
		return nil, fmt.Errorf("meeting minutes not initialized")
	}
	snapshot, err := s.meetingMinutes.Stop(time.Now())
	if snapshot != nil && s.clientState != nil {
		snapshot.SessionID = s.clientState.SessionID
		snapshot.DeviceID = s.clientState.DeviceID
	}
	return snapshot, err
}

// GetDeviceID returns the associated device ID if available.
func (s *ChatSession) GetDeviceID() string {
	if s != nil && s.clientState != nil {
		return s.clientState.DeviceID
	}
	return ""
}

func (s *ChatSession) SetLastNewsLink(link map[string]interface{}) {

}

// SetCloseAfterChat sets whether to close after chat
func (s *ChatSession) SetCloseAfterChat(close bool, text string) {
	log.Infof("设备 %s 退出MCP连接", s.clientState.DeviceID)
	s.serverTransport.SendTtsStart()
	_ = s.ttsManager.HandleTts(s.clientState.GetSessionCtx(), llm_common.LLMResponseStruct{Text: text})
	s.serverTransport.SendTtsStop()
	if close {
		s.Close()
	}
}

// InitMcp 初始化MCP连接
func (s *ChatSession) InitMcp() {
	log.Infof("为设备 %s 初始化MCP连接", s.clientState.DeviceID)
	chatmcp.InitMcp(s.clientState, s.serverTransport)
}

func (s *ChatSession) CmdMessageLoop(ctx context.Context) {
	recvFailCount := 0
	for {
		select {
		case <-ctx.Done():
			log.Infof("设备 %s recvCmd context cancel", s.clientState.DeviceID)
			return
		default:
		}

		if recvFailCount > 3 {
			log.Errorf("recv cmd timeout: %v", recvFailCount)
			return
		}

		message, err := s.serverTransport.RecvCmd(ctx, 120)
		if err != nil {
			// 如果是连接关闭或上下文取消错误，静默退出
			if strings.Contains(err.Error(), "connection is closed") ||
				strings.Contains(err.Error(), "use of closed network connection") ||
				strings.Contains(err.Error(), "context canceled") {
				log.Debugf("连接已关闭或上下文取消，退出cmd消息循环，设备 %s", s.clientState.DeviceID)
				return
			}
			if strings.Contains(err.Error(), "timeout") {
				// 忽略超时错误, 比如长时间播放歌曲等会出现这种情况
				continue
			}

			log.Errorf("recv cmd error: %v", err)
			recvFailCount = recvFailCount + 1
			continue
		}
		recvFailCount = 0
		log.Infof("收到文本消息: device=%s transport=%s session=%s payload=%s",
			s.clientState.DeviceID, s.serverTransport.GetTransportType(), s.clientState.SessionID,
			truncateTextForEvent(string(message), 200))
		if err := s.HandleTextMessage(message); err != nil {
			log.Errorf("处理文本消息失败: %v", err)
			continue
		}
	}
}

func (s *ChatSession) AudioMessageLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			log.Debugf("设备 %s recvCmd context cancel", s.clientState.DeviceID)
			return
		default:
		}
		message, err := s.serverTransport.RecvAudio(ctx, 600)
		if err != nil {
			// 如果是连接关闭或上下文取消错误，静默退出
			if strings.Contains(err.Error(), "connection is closed") ||
				strings.Contains(err.Error(), "use of closed network connection") ||
				strings.Contains(err.Error(), "context canceled") {
				log.Debugf("连接已关闭或上下文取消，退出audio消息循环，设备 %s", s.clientState.DeviceID)
				return
			}
			if strings.Contains(err.Error(), "timeout") {
				// 忽略超时错误, 比如长时间播放歌曲等会出现这种情况
				continue
			}

			log.Errorf("recv audio error: %v", err)
			return
		}
		//log.Debugf("收到音频数据，大小: %d 字节", len(message))
		status := s.clientState.GetStatus()
		if s.clientState.GetClientVoiceStop() {
			if s.canAutoRecoverListening(status) {
				s.tryRecoverListening("voice_stop_on_audio")
			}
			continue
		}
		if s.clientState.ListenMode == "realtime" && s.clientState.Asr.AsrAudioChannel == nil {
			if s.clientState.IsAsrFinalizing() {
				s.clientState.SetPendingAsrRestart(true)
				continue
			}
			if restartErr := s.asrManager.RestartAsrRecognition(ctx); restartErr != nil {
				log.Errorf("realtime 音频到达但ASR未运行，重启失败: %v", restartErr)
				continue
			}
		}
		if status == client.ClientStatusListenStop && s.canAutoRecoverListening(status) {
			s.tryRecoverListening("status_listen_stop_audio")
			continue
		}

		if ok := s.HandleAudioMessage(message); !ok {
			log.Errorf("音频缓冲区已满: %v", err)
		}
	}
}

// HandleTextMessage 处理文本消息
func (s *ChatSession) HandleTextMessage(message []byte) error {
	var clientMsg client.ClientMessage
	if err := json.Unmarshal(message, &clientMsg); err != nil {
		log.Errorf("解析消息失败: %v", err)
		return fmt.Errorf("解析消息失败: %v", err)
	}

	s.logClientTextMessage(&clientMsg)
	s.markUpstreamText()

	// 处理不同类型的消息
	switch clientMsg.Type {
	case transporttypes.MessageTypeHello:
		return s.HandleHelloMessage(&clientMsg)
	case transporttypes.MessageTypeListen:
		return s.HandleListenMessage(&clientMsg)
	case transporttypes.MessageTypeAbort:
		return s.HandleAbortMessage(&clientMsg)
	case transporttypes.MessageTypeIot:
		return s.HandleIoTMessage(&clientMsg)
	case transporttypes.MessageTypeMcp:
		return s.HandleMcpMessage(&clientMsg)
	case transporttypes.MessageTypeGoodBye:
		return s.HandleGoodByeMessage(&clientMsg)
	default:
		// 未知消息类型，直接回显
		return fmt.Errorf("未知消息类型: %s", clientMsg.Type)
	}
}

// HandleAudioMessage 处理音频消息
func (s *ChatSession) HandleAudioMessage(data []byte) bool {
	if len(data) == 0 {
		log.Debugf("收到空音频帧，跳过处理")
		return true
	}

	if len(data) > 0 {
		s.logDataMessage("client", "audio", len(data), nil)
		if s.clientState != nil {
			s.clientState.AudioStats.RecordRecv(len(data))
		}
	}
	select {
	case s.clientState.OpusAudioBuffer <- data:
		if s.roundStartTime.IsZero() {
			s.roundStartTime = time.Now()
		}
		s.roundAudioBytes += len(data)
		s.roundHasAudio = true
		s.markUpstreamAudio()
		return true
	default:
		if s.clientState != nil {
			s.clientState.AudioStats.RecordDrop(len(data))
		}
		log.Warnf("音频缓冲区已满, 丢弃音频数据")
	}
	return false
}

func (s *ChatSession) canAutoRecoverListening(status string) bool {
	if s == nil || s.clientState == nil {
		return false
	}
	cfg := config.GetConfig()
	if cfg != nil && !cfg.Chat.AutoResumeListening {
		return false
	}
	if mode := s.clientState.ListenMode; mode == "manual" || mode == "realtime" {
		return false
	}
	if !s.clientState.AutoRecoverReady() {
		return false
	}
	if status != client.ClientStatusListenStop && status != client.ClientStatusInit {
		return false
	}
	if s.clientState.GetTtsStart() {
		return false
	}
	return true
}

// tryRecoverListening 在检测到 listenStop/voiceStop 期间仍有音频帧时，自动触发一次 ListenStart
// 避免 ASR 断线后服务器停止处理音频。限制在非手动模式下执行，且不会在 LLM/TTS 阶段介入。
func (s *ChatSession) tryRecoverListening(reason string) bool {
	if s == nil || s.clientState == nil {
		return false
	}
	log.Infof("设备 %s 自动恢复监听: status=%s reason=%s", s.clientState.DeviceID, s.clientState.GetStatus(), reason)
	msg := &client.ClientMessage{
		DeviceID: s.clientState.DeviceID,
		Mode:     s.clientState.ListenMode,
		State:    transporttypes.MessageStateStart,
	}
	if err := s.HandleListenStart(msg); err != nil {
		log.Errorf("自动恢复监听失败, device=%s, err=%v", s.clientState.DeviceID, err)
		return false
	}
	return true
}

// handleHelloMessage 处理 hello 消息
func (s *ChatSession) HandleHelloMessage(msg *client.ClientMessage) error {
	if msg.Transport == "" {
		msg.Transport = transporttypes.TransportTypeWebsocket
	}

	switch msg.Transport {
	case transporttypes.TransportTypeWebsocket:
		return s.HandleWebsocketHelloMessage(msg)
	case transporttypes.TransportTypeMqttUdp:
		return s.HandleMqttHelloMessage(msg)
	default:
		return fmt.Errorf("不支持的传输类型: %s", msg.Transport)
	}
}

func (s *ChatSession) HandleMqttHelloMessage(msg *client.ClientMessage) error {
	s.HandleCommonHelloMessage(msg)

	clientState := s.clientState

	cfg := config.GetConfig()
	udpExternalHost := cfg.UDP.ExternalHost
	udpExternalPort := cfg.UDP.ExternalPort

	aesKey, err := s.serverTransport.GetData("aes_key")
	if err != nil {
		return fmt.Errorf("获取aes_key失败: %v", err)
	}
	fullNonce, err := s.serverTransport.GetData("full_nonce")
	if err != nil {
		return fmt.Errorf("获取full_nonce失败: %v", err)
	}

	strAesKey, ok := aesKey.(string)
	if !ok {
		return fmt.Errorf("aes_key不是字符串")
	}
	strFullNonce, ok := fullNonce.(string)
	if !ok {
		return fmt.Errorf("full_nonce不是字符串")
	}

	udpConfig := &transporttypes.UdpConfig{
		Server: udpExternalHost,
		Port:   udpExternalPort,
		Key:    strAesKey,
		Nonce:  strFullNonce,
	}

	// 发送响应
	return s.serverTransport.SendHello("udp", &clientState.OutputAudioFormat, udpConfig)
}

func (s *ChatSession) HandleCommonHelloMessage(msg *client.ClientMessage) error {
	// 更新客户端状态
	s.clientState.SessionID = chatutils.GenerateClientSessionID()
	s.setupSessionLogger()

	if isMcp, ok := msg.Features["mcp"]; ok && isMcp {
		go chatmcp.InitMcp(s.clientState, s.serverTransport)
	}

	clientState := s.clientState
	requestedVersion := s.serverTransport.ProtocolVersion()
	negotiatedVersion := msg.Version
	if negotiatedVersion == 0 {
		negotiatedVersion = requestedVersion
	}
	if negotiatedVersion == 0 {
		negotiatedVersion = 1
	}
	if requestedVersion != 0 && msg.Version != 0 && requestedVersion != msg.Version {
		log.Warnf("协议版本不一致, header=v%d, hello=v%d, device=%s", requestedVersion, msg.Version, clientState.DeviceID)
	}
	clientState.ProtocolVersion = negotiatedVersion
	s.serverTransport.SetProtocolVersion(negotiatedVersion)

	if msg.AudioParams != nil {
		normalizedFormat, err := normalizeAudioFormat(msg.AudioParams.Format)
		if err != nil {
			log.Warnf("不支持的音频格式: %s, device=%s", msg.AudioParams.Format, clientState.DeviceID)
			if sendErr := s.serverTransport.SendGoodbye(fmt.Sprintf("unsupported audio format: %s", msg.AudioParams.Format)); sendErr != nil {
				log.Warnf("发送格式错误提示失败: %v", sendErr)
			}
			s.Close()
			return err
		}
		normalizedOutput, err := normalizeOutputFormat(msg.AudioParams.OutputFormat, normalizedFormat)
		if err != nil {
			log.Warnf("不支持的下行音频格式: %s, device=%s", msg.AudioParams.OutputFormat, clientState.DeviceID)
			if sendErr := s.serverTransport.SendGoodbye(fmt.Sprintf("unsupported output format: %s", msg.AudioParams.OutputFormat)); sendErr != nil {
				log.Warnf("发送格式错误提示失败: %v", sendErr)
			}
			s.Close()
			return err
		}
		msg.AudioParams.Format = normalizedFormat
		msg.AudioParams.OutputFormat = normalizedOutput
		clientState.InputAudioFormat = *msg.AudioParams
		clientState.OutputAudioFormat.Format = normalizedOutput
		if msg.AudioParams.OutputFormat != "" {
			clientState.OutputAudioFormat.OutputFormat = normalizedOutput
		}
	} else {
		clientState.InputAudioFormat.Format = audio.Format
		clientState.InputAudioFormat.SampleRate = 16000
		clientState.InputAudioFormat.Channels = 1
		clientState.InputAudioFormat.FrameDuration = 20
	}
	if clientState.InputAudioFormat.Format == audio.FormatPCM16LE && clientState.OutputAudioFormat.Format == audio.Format {
		clientState.OutputAudioFormat.Format = audio.FormatPCM16LE
	}
	if clientState.OutputAudioFormat.Format == audio.FormatWAV {
		clientState.OutputAudioFormat.OutputFormat = audio.FormatWAV
	}
	clientState.SetAsrPcmFrameSize(clientState.InputAudioFormat.SampleRate, clientState.InputAudioFormat.Channels, clientState.InputAudioFormat.FrameDuration)

	s.asrManager.ProcessVadAudio(clientState.Ctx, s.Close)

	s.logClientTextMessage(msg)

	return nil
}

func (s *ChatSession) HandleWebsocketHelloMessage(msg *client.ClientMessage) error {
	err := s.HandleCommonHelloMessage(msg)
	if err != nil {
		return err
	}

	return s.serverTransport.SendHello("websocket", &s.clientState.OutputAudioFormat, nil)
}

func (s *ChatSession) applyListenModeSettings() {
	if s.clientState == nil {
		return
	}
	base := s.clientState.DefaultSilenceThresholdTime
	if base <= 0 {
		base = s.clientState.VoiceStatus.SilenceThresholdTime
	}
	if base <= 0 {
		return
	}

	desired := base
	if s.clientState.ListenMode == "realtime" && desired < minRealtimeSilenceThresholdMs {
		desired = minRealtimeSilenceThresholdMs
	}
	if s.clientState.VoiceStatus.SilenceThresholdTime != desired {
		log.Debugf("listen mode=%s silence threshold update: %dms -> %dms", s.clientState.ListenMode, s.clientState.VoiceStatus.SilenceThresholdTime, desired)
		s.clientState.VoiceStatus.SilenceThresholdTime = desired
	}
}

// HandleListenMessage  处理监听消息
func (s *ChatSession) HandleListenMessage(clientMsg *client.ClientMessage) error {
	if clientMsg.Mode != "" {
		s.clientState.ListenMode = clientMsg.Mode
	}
	s.applyListenModeSettings()
	log.Infof("收到listen消息: device=%s state=%s mode=%s transport=%s session=%s status=%s",
		clientMsg.DeviceID, clientMsg.State, clientMsg.Mode, clientMsg.Transport, s.clientState.SessionID, s.clientState.GetStatus())
	// 根据状态处理
	switch clientMsg.State {
	case transporttypes.MessageStateStart:
		s.HandleListenStart(clientMsg)
	case transporttypes.MessageStateStop:
		s.HandleListenStop()
	case transporttypes.MessageStateDetect:
		s.HandleListenDetect(clientMsg)
	}

	// 记录日志
	log.Infof("设备 %s 更新音频监听状态: %s", clientMsg.DeviceID, clientMsg.State)
	return nil
}

func (s *ChatSession) HandleListenDetect(msg *client.ClientMessage) error {
	// 唤醒词检测
	// 注意：不同客户端实现不同：
	// - ESP32 固件：只发送 detect 消息，期望立即响应
	// - py-xiaozhi：发送 detect + start 消息，期望在 start 后处理
	s.StopSpeaking(false)

	// 记录唤醒词文本
	if msg.Text != "" {
		text := msg.Text
		// 移除标点符号和处理长度
		text = chatutils.RemovePunctuation(text)

		// 将唤醒词保存到客户端状态，供后续使用
		s.clientState.WakeWord = text
		log.Infof("检测到唤醒词: %s", text)

		// 检查是否是唤醒词
		isWakeupWord := chatutils.IsWakeupWord(text)
		cfg := config.GetConfig()
		enableGreeting := cfg.Greeting.EnableGreeting

		// 延迟处理策略：等待一小段时间，如果没有收到 start 消息，则立即处理
		// 这样可以兼容只发送 detect 的客户端（如 ESP32）和发送 detect+start 的客户端（如 py-xiaozhi）
		go func() {
			time.Sleep(50 * time.Millisecond) // 等待 50ms

			// 检查是否已被 HandleListenStart 处理
			if s.clientState.WakeWord == text {
				// 还未处理，说明客户端可能不会发送 start 消息
				log.Infof("检测到单独的 detect 消息，立即处理（兼容 ESP32 模式）")
				s.clientState.WakeWord = "" // 清空，避免重复处理

				var needStartChat bool
				if !isWakeupWord || (isWakeupWord && enableGreeting) {
					needStartChat = true
				}

				if needStartChat {
					if enableGreeting && isWakeupWord {
						// 播放欢迎语
						if !s.clientState.IsWelcomeSpeaking {
							s.HandleWelcome()
						}
					} else {
						// 开始对话
						if s.roundStartTime.IsZero() {
							s.roundStartTime = time.Now()
						}
						if err := s.AddAsrResultToQueue(text); err != nil {
							log.Errorf("开始对话失败: %v", err)
						}
					}
				}
			}
		}()
	}

	return nil
}

func (s *ChatSession) HandleWelcome() {
	// mark welcome speech as in-progress to avoid duplicate playback
	s.clientState.IsWelcomeSpeaking = true
	defer func() {
		// reset welcome speech flag so the next wake-up can play again
		s.clientState.IsWelcomeSpeaking = false
	}()

	greetingText := s.GetRandomGreeting()
	if err := s.serverTransport.SendTtsStart(); err != nil {
		log.Warnf("欢迎语发送 TTS start 失败: %v", err)
	}

	// use a dedicated context so StopSpeaking does not interrupt the greeting
	ctx, cancel := context.WithTimeout(s.clientState.Ctx, 10*time.Second)
	defer cancel()

	s.ttsManager.HandleTts(ctx, llm_common.LLMResponseStruct{
		Text: greetingText,
	})

	if err := s.serverTransport.SendTtsStop(); err != nil {
		log.Warnf("欢迎语发送 TTS stop 失败: %v", err)
	}
	if s.clientState != nil && s.clientState.ListenMode == "realtime" {
		s.forceListenStart("welcome-end")
	}
}

func (s *ChatSession) GetRandomGreeting() string {
	cfg := config.GetConfig()
	greetingList := cfg.Greeting.GreetingList
	if len(greetingList) == 0 {
		return "你好，有啥好玩的."
	}
	return greetingList[rand.Intn(len(greetingList))]
}

func (s *ChatSession) AddTextToTTSQueue(text string) error {
	s.llmManager.AddTextToTTSQueue(text)
	return nil
}

func (s *ChatSession) markUpstreamAudio() {
	if s == nil {
		return
	}
	now := time.Now()
	s.lastUpstreamAudioTime.Store(now.UnixMilli())
	s.touchActivity(now)
}

func (s *ChatSession) markDownstreamAudio() {
	if s == nil {
		return
	}
	now := time.Now()
	s.lastDownstreamAudioTime.Store(now.UnixMilli())
	s.touchActivity(now)
}

// MarkDownstreamAudio exposes downstream audio tracking for transport callbacks.
func (s *ChatSession) MarkDownstreamAudio() {
	s.markDownstreamAudio()
}

func (s *ChatSession) markUpstreamText() {
	if s == nil {
		return
	}
	now := time.Now()
	s.lastUpstreamTextTime.Store(now.UnixMilli())
	s.touchActivity(now)
}

func (s *ChatSession) markDownstreamText() {
	if s == nil {
		return
	}
	now := time.Now()
	s.lastDownstreamTextTime.Store(now.UnixMilli())
	s.touchActivity(now)
}

// MarkDownstreamText exposes downstream text tracking for transport callbacks.
func (s *ChatSession) MarkDownstreamText() {
	s.markDownstreamText()
}

func formatTimestamp(ts int64) string {
	if ts <= 0 {
		return "-"
	}
	return time.UnixMilli(ts).Format(time.RFC3339Nano)
}

func formatDuration(d time.Duration) string {
	if d <= 0 {
		return "-"
	}
	return fmt.Sprintf("%dms", d.Milliseconds())
}

func (s *ChatSession) monitorSnapshot(now time.Time) map[string]interface{} {
	stats := map[string]interface{}{
		"closed": s.closed.Load(),
	}
	if s.clientState != nil {
		stats["device_id"] = s.clientState.DeviceID
		stats["session_id"] = s.clientState.SessionID
		stats["status"] = s.clientState.GetStatus()
		audioStats := s.clientState.AudioStats.Snapshot()
		stats["audio_recv_frames"] = audioStats.RecvFrames
		stats["audio_recv_bytes"] = audioStats.RecvBytes
		stats["audio_drop_frames"] = audioStats.DroppedFrames
		stats["audio_drop_bytes"] = audioStats.DroppedBytes
		stats["audio_consume_frames"] = audioStats.ConsumedFrames
		stats["audio_consume_bytes"] = audioStats.ConsumedBytes
	}
	last := s.lastActivityTime.Load()
	stats["last_activity"] = formatTimestamp(last)
	if last > 0 {
		stats["idle_ms"] = now.Sub(time.UnixMilli(last)).Milliseconds()
	}
	upAudio := s.lastUpstreamAudioTime.Load()
	stats["last_upstream_audio"] = formatTimestamp(upAudio)
	if upAudio > 0 {
		stats["idle_up_audio_ms"] = now.Sub(time.UnixMilli(upAudio)).Milliseconds()
	}
	downAudio := s.lastDownstreamAudioTime.Load()
	stats["last_downstream_audio"] = formatTimestamp(downAudio)
	if downAudio > 0 {
		stats["idle_down_audio_ms"] = now.Sub(time.UnixMilli(downAudio)).Milliseconds()
	}
	upText := s.lastUpstreamTextTime.Load()
	stats["last_upstream_text"] = formatTimestamp(upText)
	if upText > 0 {
		stats["idle_up_text_ms"] = now.Sub(time.UnixMilli(upText)).Milliseconds()
	}
	downText := s.lastDownstreamTextTime.Load()
	stats["last_downstream_text"] = formatTimestamp(downText)
	if downText > 0 {
		stats["idle_down_text_ms"] = now.Sub(time.UnixMilli(downText)).Milliseconds()
	}
	return stats
}

func truncateTextForEvent(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "..."
}

func (s *ChatSession) touchActivity(now time.Time) {
	if s == nil {
		return
	}
	if s.closed.Load() {
		return
	}
	s.lastActivityTime.Store(now.UnixMilli())
}

// handleAbortMessage 处理中止消息
func (s *ChatSession) HandleAbortMessage(msg *client.ClientMessage) error {
	// 设置打断状态
	s.clientState.Abort = true

	s.StopSpeaking(true)

	// 记录日志
	log.Infof("设备 %s abort 会话", msg.DeviceID)
	return nil
}

// handleIoTMessage 处理物联网消息
func (s *ChatSession) HandleIoTMessage(msg *client.ClientMessage) error {
	// 获取客户端状态
	//sessionID := clientState.SessionID

	// 验证设备ID
	/*
		if _, err := s.authManager.GetSession(msg.DeviceID); err != nil {
			return fmt.Errorf("会话验证失败: %v", err)
		}*/

	// 发送 IoT 响应
	err := s.serverTransport.SendIot(msg)
	if err != nil {
		return fmt.Errorf("发送响应失败: %v", err)
	}

	// 记录日志
	log.Infof("设备 %s 物联网指令: %s", msg.DeviceID, msg.Text)
	return nil
}

func (s *ChatSession) HandleMcpMessage(msg *client.ClientMessage) error {
	mcpSession := domainmcp.GetDeviceMcpClient(s.clientState.DeviceID)
	if mcpSession != nil && s.serverTransport != nil {
		if ch := s.serverTransport.MCPRecvChan(); ch != nil {
			select {
			case ch <- msg.PayLoad:
			default:
				log.Warnf("mcp 接收消息通道已满, 丢弃消息")
			}
		}
	}
	return nil
}

// 释放udp资源
func (s *ChatSession) HandleGoodByeMessage(msg *client.ClientMessage) error {
	s.serverTransport.CloseAudioChannel()
	return nil
}

func (s *ChatSession) HandleListenStart(msg *client.ClientMessage) error {
	if s == nil || s.clientState == nil {
		return nil
	}
	log.Infof("HandleListenStart: device=%s mode=%s state=%s transport=%s session=%s status=%s",
		s.clientState.DeviceID, msg.Mode, msg.State, msg.Transport, s.clientState.SessionID, s.clientState.GetStatus())
	if s.clientState.GetStatus() == client.ClientStatusTTSStart {
		log.Infof("listen start during tts, interrupting playback: device=%s mode=%s", s.clientState.DeviceID, msg.Mode)
	}
	if s.clientState.GetStatus() == client.ClientStatusLLMStart {
		log.Infof("listen start during llm, interrupting response: device=%s mode=%s", s.clientState.DeviceID, msg.Mode)
	}

	// 处理拾音模式
	if msg.Mode != "" {
		s.clientState.ListenMode = msg.Mode
		log.Infof("设备 %s 拾音模式: %s", msg.DeviceID, msg.Mode)
	}
	//if s.clientState.ListenMode == "manual" {
	s.StopSpeaking(false)
	//}
	s.clientState.SetStatus(client.ClientStatusListening)
	s.roundStartTime = time.Now()
	s.roundAudioBytes = 0
	s.roundHasAudio = false

	// 处理唤醒词和欢迎语逻辑
	if s.clientState.WakeWord != "" {
		wakeWord := s.clientState.WakeWord
		s.clientState.WakeWord = "" // 清空，避免重复处理

		// 检查是否是唤醒词
		isWakeupWord := chatutils.IsWakeupWord(wakeWord)
		cfg := config.GetConfig()
		enableGreeting := cfg.Greeting.EnableGreeting

		// 如果是唤醒词且启用了欢迎语，播放欢迎语
		if isWakeupWord && enableGreeting {
			if !s.clientState.IsWelcomeSpeaking {
				log.Infof("检测到唤醒词 '%s'，播放欢迎语", wakeWord)
				go s.HandleWelcome()
				// 欢迎语播放后会自动返回监听状态，所以这里直接返回
				return nil
			}
		}
	}

	if s.stateMachine != nil {
		s.stateMachine.OnListenStart()
	}

	return s.OnListenStart()
}

func (s *ChatSession) GetCurrentStatus() string {
	return s.clientState.GetStatus()
}

func (s *ChatSession) HandleListenStop() error {
	/*if s.clientState.ListenMode == "auto" {
		s.clientState.CancelSessionCtx()
	}*/

	//调用
	s.clientState.OnManualStop()

	return nil
}

// ToolInFlight 对 ServerTransport 暴露工具执行状态，便于 auto resume 判定
func (s *ChatSession) ToolInFlight() bool {
	if s.llmManager == nil {
		return false
	}
	return s.llmManager.ToolInFlight()
}

// LLMInFlight 返回当前是否有 LLM 正在处理
func (s *ChatSession) LLMInFlight() bool {
	if s.llmManager == nil {
		return false
	}
	return s.llmManager.LLMInFlight()
}

func (s *ChatSession) OnListenStart() error {
	log.Debugf("OnListenStart start")
	defer log.Debugf("OnListenStart end")

	select {
	case <-s.clientState.Ctx.Done():
		log.Debugf("OnListenStart Ctx done, return")
		return nil
	default:
	}

	s.clientState.Destroy()

	ctx := s.clientState.GetSessionCtx()

	//初始化asr相关
	if s.clientState.ListenMode == "manual" {
		s.clientState.VoiceStatus.SetClientHaveVoice(true)
	}

	// 启动asr流式识别，复用 restartAsrRecognition 函数
	err := s.asrManager.RestartAsrRecognition(ctx)
	if err != nil {
		log.Errorf("asr流式识别失败: %v", err)
		s.Close()
		return err
	}

	// 启动一个goroutine处理asr结果
	go func() {
		log.Debugf("ASR结果处理循环启动: device=%s mode=%s", s.clientState.DeviceID, s.clientState.ListenMode)
		defer func() {
			if r := recover(); r != nil {
				log.Errorf("asr结果处理goroutine panic: %v, stack: %s", r, string(debug.Stack()))
			}
			log.Debugf("ASR结果处理循环退出: device=%s mode=%s status=%s", s.clientState.DeviceID, s.clientState.ListenMode, s.clientState.GetStatus())
		}()
		defer s.clientState.SetAutoRecoverReady(true)

		//最大空闲 60s

		var startIdleTime, maxIdleTime int64
		startIdleTime = time.Now().Unix()
		maxIdleTime = 60
		// ASR错误重试相关
		var asrErrorRetryCount int
		const maxAsrErrorRetries = 3 // 最大重试次数

		for {
			select {
			case <-ctx.Done():
				log.Debugf("asr ctx done")
				return
			default:
			}

			var handler func(result asr_types.StreamingResult) error
			if s.clientState.ListenMode == "realtime" {
				var lastPartial string
				handler = func(result asr_types.StreamingResult) error {
					if result.Error != nil || result.IsFinal {
						return result.Error
					}
					partialText := strings.TrimSpace(result.Text)
					if partialText == "" || partialText == lastPartial {
						return nil
					}
					now := time.Now()
					throttleMs := config.GetConfig().Chat.AsrPartialThrottleMs
					lastAt := s.lastPartialAt.Load()
					if throttleMs > 0 && lastAt > 0 {
						if delta := now.Sub(time.UnixMilli(lastAt)); delta < time.Duration(throttleMs)*time.Millisecond {
							return nil
						}
					}
					s.lastPartialAt.Store(now.UnixMilli())
					lastPartial = partialText
					if err := s.serverTransport.SendAsrResultWithState(partialText, transporttypes.MessageStateStream); err != nil {
						log.Warnf("发送实时ASR结果失败: %v", err)
					}
					return nil
				}
			}

			text, err := s.clientState.RetireAsrResultWithHandler(ctx, handler)
			if s.clientState.ListenMode == "realtime" {
				s.clientState.ClearAsrFinalizing()
				if s.clientState.ConsumePendingAsrRestart() && s.clientState.Asr.AsrAudioChannel == nil {
					if restartErr := s.asrManager.RestartAsrRecognition(ctx); restartErr != nil {
						log.Errorf("realtime 收尾后重启ASR识别失败: %v", restartErr)
						return
					}
				}
			}
			if err != nil {
				status := s.clientState.GetStatus()
				listenMode := s.clientState.ListenMode
				log.Errorf("处理asr结果失败: %v (mode=%s status=%s)", err, listenMode, status)

				if listenMode == "realtime" {
					if asrErrorRetryCount < maxAsrErrorRetries {
						asrErrorRetryCount++
						log.Warnf("realtime ASR处理失败，尝试重启ASR (第 %d/%d 次重试): %v", asrErrorRetryCount, maxAsrErrorRetries, err)
						select {
						case <-ctx.Done():
							log.Debugf("asr ctx done during retry wait")
							return
						case <-time.After(500 * time.Millisecond):
						}
						if restartErr := s.asrManager.RestartAsrRecognition(ctx); restartErr != nil {
							log.Errorf("realtime 重启ASR识别失败: %v", restartErr)
							continue
						}
						log.Infof("realtime ASR重启成功，继续处理音频")
						asrErrorRetryCount = 0
						startIdleTime = time.Now().Unix()
						continue
					}

					log.Errorf("realtime ASR错误重试次数已达上限 (%d)，继续监听并强制重启ASR", maxAsrErrorRetries)
					asrErrorRetryCount = 0
					if restartErr := s.asrManager.RestartAsrRecognition(ctx); restartErr != nil {
						log.Errorf("realtime 强制重启ASR失败: %v", restartErr)
						return
					}
					startIdleTime = time.Now().Unix()
					continue
				}

				// 检查当前状态是否还在 listening，如果是则尝试重启ASR
				if status == client.ClientStatusListening || status == client.ClientStatusListenStop {
					if asrErrorRetryCount < maxAsrErrorRetries {
						asrErrorRetryCount++
						log.Warnf("ASR处理失败，尝试重启ASR (第 %d/%d 次重试): %v", asrErrorRetryCount, maxAsrErrorRetries, err)

						// 等待一小段时间再重启，避免立即重试
						select {
						case <-ctx.Done():
							log.Debugf("asr ctx done during retry wait")
							return
						case <-time.After(500 * time.Millisecond):
						}

						if restartErr := s.asrManager.RestartAsrRecognition(ctx); restartErr != nil {
							log.Errorf("重启ASR识别失败: %v", restartErr)
							// 重启失败，继续尝试或退出
							if asrErrorRetryCount >= maxAsrErrorRetries {
								log.Errorf("ASR错误重试次数已达上限 (%d)，停止处理并通知设备", maxAsrErrorRetries)
								// 通知设备停止 listen，确保状态同步
								if sendErr := s.serverTransport.SendListenStop(); sendErr != nil {
									log.Warnf("发送 listen stop 消息失败: %v", sendErr)
								}
								s.clientState.OnVoiceSilence()
								return
							}
							continue
						}

						// 重启成功，重置重试计数并继续处理
						log.Infof("ASR重启成功，继续处理音频")
						asrErrorRetryCount = 0
						startIdleTime = time.Now().Unix()
						continue
					} else {
						log.Errorf("ASR错误重试次数已达上限 (%d)，停止处理并通知设备", maxAsrErrorRetries)
						// 通知设备停止 listen，确保状态同步
						if sendErr := s.serverTransport.SendListenStop(); sendErr != nil {
							log.Warnf("发送 listen stop 消息失败: %v", sendErr)
						}
						s.clientState.OnVoiceSilence()
						return
					}
				} else {
					// 状态已经不是 listening，通知设备停止 listen 后退出
					log.Debugf("当前状态不是 listening (%s)，ASR错误后通知设备并退出", status)
					if sendErr := s.serverTransport.SendListenStop(); sendErr != nil {
						log.Warnf("发送 listen stop 消息失败: %v", sendErr)
					}
					return
				}
			}

			// ASR处理成功，重置错误重试计数
			s.clientState.MarkAsrEndTs()
			asrErrorRetryCount = 0

			//统计asr耗时
			log.Debugf("处理asr结果: %s, 耗时: %d ms", text, s.clientState.GetAsrDuration())

			if text != "" {
				if s.shouldDropEcho(text) {
					s.clientState.OnVoiceSilence()
					log.Infof("ignore asr echo: device=%s text=%q", s.clientState.DeviceID, text)
					s.lastPartialAt.Store(0)
					if s.clientState.ListenMode == "realtime" {
						if s.clientState.Asr.AsrAudioChannel == nil {
							if restartErr := s.asrManager.RestartAsrRecognition(ctx); restartErr != nil {
								log.Errorf("realtime 重启ASR识别失败: %v", restartErr)
								return
							}
						}
						startIdleTime = time.Now().Unix()
						continue
					}
					return
				}

				cfg := config.GetConfig()
				if s.clientState.ListenMode == "realtime" && cfg.Chat.RealtimeMode == 2 && cfg.Chat.RealtimeInterruptEnabled {
					status := s.clientState.GetStatus()
					if status == client.ClientStatusLLMStart || status == client.ClientStatusTTSStart {
						s.cancelRealtimeWork("asr-final", true)
					}
				}

				// 重置重试计数器
				startIdleTime = 0

				//当获取到asr结果时, 结束语音输入
				s.clientState.OnVoiceSilence()

				if s.clientState.ListenMode == "realtime" && s.asrManager != nil && s.asrManager.ConsumeListenStartPending() {
					roundStartTime := s.roundStartTime
					roundAudioBytes := s.roundAudioBytes
					roundHasAudio := s.roundHasAudio
					s.forceListenStart("vad-asr-final")
					s.roundStartTime = roundStartTime
					s.roundAudioBytes = roundAudioBytes
					s.roundHasAudio = roundHasAudio
				}

				//发送asr消息
				err = s.serverTransport.SendAsrResult(text)
				if err != nil {
					log.Errorf("发送asr消息失败: %v", err)
					return
				}

				err = s.AddAsrResultToQueue(text)
				if err != nil {
					log.Errorf("开始对话失败: %v", err)
					return
				}
				s.lastPartialAt.Store(0)
				if s.clientState.ListenMode == "realtime" {
					if s.clientState.Asr.AsrAudioChannel == nil {
						if restartErr := s.asrManager.RestartAsrRecognition(ctx); restartErr != nil {
							log.Errorf("realtime 重启ASR识别失败: %v", restartErr)
							return
						}
					}
					startIdleTime = time.Now().Unix()
					continue
				}
				return
			} else {
				select {
				case <-ctx.Done():
					log.Debugf("asr ctx done")
					return
				default:
				}
				log.Debugf("ready Restart Asr, s.clientState.Status: %s", s.clientState.Status)
				if s.clientState.ListenMode == "realtime" {
					if s.clientState.Asr.AsrAudioChannel == nil {
						if restartErr := s.asrManager.RestartAsrRecognition(ctx); restartErr != nil {
							log.Errorf("realtime 重启ASR识别失败: %v", restartErr)
							return
						}
					}
					startIdleTime = time.Now().Unix()
					continue
				}
				if s.clientState.Status == client.ClientStatusListening || s.clientState.Status == client.ClientStatusListenStop {
					// text 为空，检查是否需要重新启动ASR
					diffTs := time.Now().Unix() - startIdleTime
					if startIdleTime > 0 && diffTs <= maxIdleTime {
						log.Warnf("ASR识别结果为空，尝试重启ASR识别, diff ts: %d", diffTs)
						if restartErr := s.asrManager.RestartAsrRecognition(ctx); restartErr != nil {
							log.Errorf("重启ASR识别失败: %v", restartErr)
							return
						}
						startIdleTime = time.Now().Unix()
						continue
					} else {
						log.Warnf("ASR识别结果为空，已达到最大空闲时间: %d", maxIdleTime)
						if sendErr := s.serverTransport.SendListenStop(); sendErr != nil {
							log.Warnf("发送 listen stop 消息失败: %v", sendErr)
						}
						s.Close()
						return
					}
				}
			}
			return
		}
	}()
	return nil
}

// startChat 开始对话
func (s *ChatSession) AddAsrResultToQueue(text string) error {
	log.Debugf("AddAsrResultToQueue text: %s", text)

	if s.roundStartTime.IsZero() {
		s.roundStartTime = time.Now()
	}
	if s.meetingMinutes != nil {
		s.meetingMinutes.Append(text, time.Now())
	}

	roundID := fmt.Sprintf("%s-%d", s.clientState.SessionID, atomic.AddUint64(&s.roundSeq, 1))
	baseCtx := s.clientState.GetSessionCtx()
	afterAsrCtx := s.clientState.GetAfterAsrCtx(baseCtx)

	metrics := chatmetrics.NewConversationMetrics(roundID, text, time.Now())
	if ts := s.lastUpstreamAudioTime.Load(); ts > 0 {
		metrics.SetLastUpstreamAudio(time.UnixMilli(ts))
	}
	s.metrics = metrics
	if s.metrics != nil && s.clientState.ListenMode == "realtime" {
		s.metrics.IncPartialCount() // 记录一次完整句输出的计数，partial 计数在下行路径处理
	}

	var audioDuration time.Duration
	if s.roundHasAudio {
		audioDuration = time.Since(s.roundStartTime)
	}
	metrics.SetAudioStats(audioDuration, s.roundAudioBytes)

	ctx := chatmetrics.WithConversationMetrics(afterAsrCtx, metrics)

	attrs := []attribute.KeyValue{
		attribute.String("device.id", s.clientState.DeviceID),
		attribute.String("session.id", s.clientState.SessionID),
		attribute.String("conversation.round_id", roundID),
		attribute.Int("stage.input.text_length", len([]rune(text))),
		attribute.Bool("stage.audio.present", s.roundHasAudio),
	}

	if audioDuration > 0 {
		attrs = append(attrs,
			attribute.Int64("stage.audio.capture_duration_ms", audioDuration.Milliseconds()),
			attribute.Int("stage.audio.capture_bytes", s.roundAudioBytes),
		)
	}

	ctx, span := observability.Tracer().Start(
		ctx,
		"conversation.round",
		trace.WithSpanKind(trace.SpanKindServer),
		trace.WithAttributes(attrs...),
	)

	span.AddEvent("stage.asr.result", trace.WithAttributes(
		attribute.String("asr.output.preview", truncateTextForEvent(text, 120)),
	))

	metrics.SetSpan(span)

	item := AsrResponseChannelItem{
		ctx:     ctx,
		text:    text,
		roundID: roundID,
		span:    span,
		metrics: metrics,
	}

	enqueueCtx := ctx
	if enqueueCtx == nil {
		enqueueCtx = s.ctx
	}
	if enqueueCtx == nil {
		enqueueCtx = context.Background()
	}

	if err := s.chatTextQueue.Enqueue(enqueueCtx, item); err != nil {
		log.Warnf("chatTextQueue enqueue failed: %v", err)
	}

	// 重置录音统计，为下一轮对话做准备
	s.roundStartTime = time.Time{}
	s.roundAudioBytes = 0
	s.roundHasAudio = false

	return nil
}

func (s *ChatSession) cancelRealtimeWork(reason string, stopTTS bool) {
	if s.clientState == nil {
		return
	}

	log.Infof("realtime interrupt: device=%s reason=%s stop_tts=%v listen_mode=%s status=%s",
		s.clientState.DeviceID, reason, stopTTS, s.clientState.ListenMode, s.clientState.GetStatus())

	if s.metrics != nil {
		s.metrics.IncInterruptCount()
	}

	s.clientState.CancelAfterAsrCtx()
	s.ClearChatTextQueue()
	s.llmManager.ClearLLMResponseQueue()
	s.ttsManager.ClearTTSQueue()

	if stopTTS {
		if err := s.serverTransport.SendTtsStop(); err != nil {
			log.Warnf("发送 TTS stop 失败: %v", err)
		}
	}

	s.clientState.SetStatus(client.ClientStatusListening)
}

func (s *ChatSession) onRealtimeInterrupt(reason string) {
	s.cancelRealtimeWork(reason, true)
}

func (s *ChatSession) forceListenStart(reason string) {
	if s == nil || s.clientState == nil {
		return
	}
	if s.clientState.ListenMode != "realtime" {
		return
	}
	status := s.clientState.GetStatus()
	if status == client.ClientStatusListening {
		return
	}
	log.Infof("force listen start: device=%s status=%s mode=%s reason=%s",
		s.clientState.DeviceID, status, s.clientState.ListenMode, reason)
	msg := &client.ClientMessage{
		DeviceID:  s.clientState.DeviceID,
		Mode:      s.clientState.ListenMode,
		State:     transporttypes.MessageStateStart,
		Transport: transporttypes.TransportTypeServer,
	}
	if err := s.HandleListenStart(msg); err != nil {
		log.Errorf("force listen start failed: device=%s err=%v", s.clientState.DeviceID, err)
	}
}

func (s *ChatSession) shouldDropEcho(text string) bool {
	if s == nil || s.clientState == nil {
		return false
	}
	lastText, lastAt := s.clientState.GetLastTTSText()
	if lastText == "" || lastAt == 0 {
		return false
	}
	now := time.Now()
	if !s.clientState.GetTtsStart() {
		if downTs := s.lastDownstreamAudioTime.Load(); downTs > 0 {
			if now.Sub(time.UnixMilli(downTs)) > 1500*time.Millisecond {
				return false
			}
		} else if now.Sub(time.UnixMilli(lastAt)) > 1500*time.Millisecond {
			return false
		}
	}

	cleanedInput := normalizeEchoText(text)
	cleanedLast := normalizeEchoText(lastText)
	if cleanedInput == "" || cleanedLast == "" {
		return false
	}
	return strings.Contains(cleanedLast, cleanedInput) || strings.Contains(cleanedInput, cleanedLast)
}

func normalizeEchoText(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range trimmed {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

func (s *ChatSession) processChatText(ctx context.Context) {
	log.Debugf("processChatText start")
	defer log.Debugf("processChatText end")

	for {
		itemVal, err := s.chatTextQueue.Dequeue(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) ||
				errors.Is(err, channels.ErrQueueClosed) {
				return
			}
			continue
		}

		item := itemVal
		err = s.pipeline.Execute(&item)
		if err != nil {
			s.handleSessionError(item.ctx, err)
		}
		if item.metrics != nil {
			if err != nil {
				item.metrics.AddError(err)
			}
			item.metrics.ApplySpanAttributes()
		}
		if item.span != nil {
			item.span.End()
		}

		if err != nil {
			log.Errorf("处理对话失败: %v", err)
		}

		s.logConversationSummary(&item)
	}
}

func (s *ChatSession) logConversationSummary(item *AsrResponseChannelItem) {
	if item == nil || item.metrics == nil {
		return
	}

	snapshot := item.metrics.Snapshot()
	if snapshot.RoundID == "" {
		return
	}

	errInfo := ""
	if len(snapshot.Errors) > 0 {
		errInfo = fmt.Sprintf(" | errors=%s", strings.Join(snapshot.Errors, "; "))
	}

	outputText := strings.TrimSpace(strings.ReplaceAll(snapshot.OutputText, "\n", " "))
	upAudio := formatTimestamp(s.lastUpstreamAudioTime.Load())
	downAudio := formatTimestamp(s.lastDownstreamAudioTime.Load())
	upText := formatTimestamp(s.lastUpstreamTextTime.Load())
	downText := formatTimestamp(s.lastDownstreamTextTime.Load())
	rtAudio := formatDuration(snapshot.ResponseLatency)

	log.Infof(
		"会话轮次 %s 完成 | input=\"%s\" | output=\"%s\" | total=%dms | audio=%dms/%dB | llm=%dms | tools=%dms(%d) | tts=%dms | rt=%s | downstream=%dms/%dB/%d frames | tUpAudio=%s | tDownAudio=%s | tUpText=%s | tDownText=%s%s",
		snapshot.RoundID,
		snapshot.InputPreview,
		outputText,
		snapshot.TotalDuration.Milliseconds(),
		snapshot.AudioDuration.Milliseconds(),
		snapshot.AudioBytes,
		snapshot.LLMDuration.Milliseconds(),
		snapshot.ToolDuration.Milliseconds(),
		snapshot.ToolCalls,
		snapshot.TTSDuration.Milliseconds(),
		rtAudio,
		snapshot.AudioDownDuration.Milliseconds(),
		snapshot.AudioDownBytes,
		snapshot.AudioDownFrames,
		upAudio,
		downAudio,
		upText,
		downText,
		errInfo,
	)
}

func (s *ChatSession) ClearChatTextQueue() {
	s.chatTextQueue.Clear()
}

func (s *ChatSession) Close() {
	if s == nil {
		return
	}
	if !s.closed.CompareAndSwap(false, true) {
		return
	}
	log.Debugf("ChatSession.Close() 开始清理会话资源, 设备 %s", s.clientState.DeviceID)
	if s.monitor != nil {
		s.monitor.Remove(s)
	}

	// 1. 首先取消会话级别的上下文，让所有 goroutine 开始退出
	if s.cancel != nil {
		s.cancel()
	}

	if s.stateMachine != nil {
		s.stateMachine.Close()
	}

	// 2. 停止说话和清理音频相关资源
	s.StopSpeaking(true)
	if s.clientState != nil {
		s.clientState.Destroy()
		s.clientState.SetStatus(client.ClientStatusInit)
	}

	// 3. 清理聊天文本队列
	s.ClearChatTextQueue()

	// 4. 等待处理循环退出
	s.waitForLoopShutdown()

	// 5. 最后关闭服务端传输
	if s.serverTransport != nil {
		s.serverTransport.Close()
	}

	if s.sessionLogger != nil {
		s.sessionLogger.Close()
	}

	log.Debugf("ChatSession.Close() 会话资源清理完成, 设备 %s", s.clientState.DeviceID)
	if metrics := observability.Server(); metrics != nil {
		metrics.TrackSessionEnd()
	}
	if s.onClose != nil {
		s.onClose()
	}
}

// IsClosed reports whether the session has been closed.
func (s *ChatSession) IsClosed() bool {
	return s.closed.Load()
}

func (s *ChatSession) waitForLoopShutdown() {
	if s == nil || s.loopsDone == nil {
		return
	}
	select {
	case <-s.loopsDone:
	case <-time.After(2 * time.Second):
		log.Warnf("chat session %s goroutines did not stop in time", s.clientState.DeviceID)
	}
}

func (s *ChatSession) handleSessionError(ctx context.Context, err error) {
	if s == nil || s.errorHandler == nil || err == nil {
		return
	}
	if appErr := s.errorHandler.Handle(ctx, err); appErr != nil {
		log.Warnf("chat session %s error: [%s] %s", s.clientState.DeviceID, appErr.Type, appErr.Message)
	}
}

func (s *ChatSession) handleNetworkError(ctx context.Context, appErr *apperrors.AppError) error {
	log.Warnf("network error for device %s: %s", s.clientState.DeviceID, appErr.Error())
	return nil
}

func (s *ChatSession) handleAIError(ctx context.Context, appErr *apperrors.AppError) error {
	return s.AddTextToTTSQueue("抱歉，我刚刚遇到了一些问题，我们稍后再试一次。")
}

func (s *ChatSession) handleBusinessError(ctx context.Context, appErr *apperrors.AppError) error {
	message := appErr.Message
	if message == "" {
		message = "当前请求暂时无法完成，请稍后再试。"
	}
	return s.AddTextToTTSQueue(message)
}

func (s *ChatSession) setupSessionLogger() {
	if s == nil || s.clientState == nil {
		return
	}
	if !monitoring.IsSessionLoggingEnabled() {
		return
	}

	deviceID := s.clientState.DeviceID
	sessionID := s.clientState.SessionID
	if deviceID == "" || sessionID == "" {
		return
	}

	if s.sessionLogger != nil {
		if s.sessionLogger.DeviceID() == deviceID && s.sessionLogger.SessionID() == sessionID {
			return
		}
		s.sessionLogger.Close()
	}

	logger, err := monitoring.NewSessionLogger(deviceID, sessionID)
	if err != nil {
		log.Warnf("创建会话日志失败: %v", err)
		return
	}
	if logger == nil {
		return
	}
	s.sessionLogger = logger
}

func (s *ChatSession) logSessionMessage(msg *schema.Message) {
	if s == nil || msg == nil {
		return
	}

	if monitoring.IsSessionLoggingEnabled() {
		if s.sessionLogger == nil {
			s.setupSessionLogger()
		}

		if s.sessionLogger != nil {
			if err := s.sessionLogger.LogMessage(msg); err != nil {
				log.Warnf("写入会话日志失败: %v", err)
			}
		}
	}

	if s.managerUploader != nil {
		s.managerUploader.OnMessageLogged(msg)
	}
}

// LogSessionMessage exposes session logging to manager packages.
func (s *ChatSession) LogSessionMessage(msg *schema.Message) {
	s.logSessionMessage(msg)
}

// RecordDownstreamAudio tracks audio that was streamed to the client so it can be uploaded to manager-server.
func (s *ChatSession) RecordDownstreamAudio(data []byte) {
	if s == nil || len(data) == 0 {
		return
	}
	if s.managerUploader == nil {
		return
	}
	s.managerUploader.RecordDownstreamAudio(data)
}

func (s *ChatSession) logClientTextMessage(msg *client.ClientMessage) {
	if s == nil || msg == nil {
		return
	}

	if len(msg.PayLoad) > 0 {
		meta := map[string]any{}
		if msg.State != "" {
			meta["state"] = msg.State
		}
		if msg.Mode != "" {
			meta["mode"] = msg.Mode
		}
		s.logDataMessage("client", msg.Type, len(msg.PayLoad), meta)
	}

	text := strings.TrimSpace(msg.Text)
	if text == "" {
		if msg.Type == transporttypes.MessageTypeListen && msg.State != "" {
			meta := map[string]any{}
			if msg.State != "" {
				meta["state"] = msg.State
			}
			if msg.Mode != "" {
				meta["mode"] = msg.Mode
			}
			if msg.Transport != "" {
				meta["transport"] = msg.Transport
			}
			s.logText("client", msg.Type, msg.State, meta)
		}
		return
	}

	meta := map[string]any{}
	if msg.State != "" {
		meta["state"] = msg.State
	}
	if msg.Mode != "" {
		meta["mode"] = msg.Mode
	}
	if msg.Transport != "" {
		meta["transport"] = msg.Transport
	}
	if len(msg.Features) > 0 {
		meta["features"] = msg.Features
	}
	if msg.Version != 0 {
		meta["version"] = msg.Version
	}
	if msg.DeviceMac != "" {
		meta["device_mac"] = msg.DeviceMac
	}
	if msg.AudioParams != nil {
		meta["audio_params"] = msg.AudioParams
	}
	if msg.Token != "" {
		meta["token_present"] = true
	}

	s.logText("client", msg.Type, text, meta)
}

func (s *ChatSession) logServerTextMessage(message *transporttypes.ServerMessage) {
	if s == nil || message == nil {
		return
	}

	if len(message.PayLoad) > 0 {
		meta := map[string]any{}
		if message.State != "" {
			meta["state"] = message.State
		}
		s.logDataMessage("server", message.Type, len(message.PayLoad), meta)
	}

	text := strings.TrimSpace(message.Text)
	if text == "" {
		return
	}

	meta := map[string]any{}
	if message.State != "" {
		meta["state"] = message.State
	}
	if message.Transport != "" {
		meta["transport"] = message.Transport
	}
	if message.Emotion != "" {
		meta["emotion"] = message.Emotion
	}
	if message.AudioFormat != nil {
		meta["audio_params"] = message.AudioFormat
	}
	if message.Udp != nil {
		meta["udp"] = message.Udp
	}

	s.logText("server", message.Type, text, meta)
}

// LogServerTextMessage exposes server text logging for transport callbacks.
func (s *ChatSession) LogServerTextMessage(message *transporttypes.ServerMessage) {
	s.logServerTextMessage(message)
}

func (s *ChatSession) logText(direction, messageType, text string, payload any) {
	if s == nil || strings.TrimSpace(text) == "" {
		return
	}

	if s.sessionLogger == nil {
		s.setupSessionLogger()
	}
	if s.sessionLogger == nil {
		return
	}

	if err := s.sessionLogger.LogText(direction, messageType, text, payload); err != nil {
		log.Warnf("写入会话日志失败: %v", err)
	}
}

func (s *ChatSession) logDataMessage(direction, messageType string, size int, payload any) {
	if s == nil {
		return
	}
	if size <= 0 {
		return
	}

	if s.sessionLogger == nil {
		s.setupSessionLogger()
	}
	if s.sessionLogger == nil {
		return
	}

	if err := s.sessionLogger.LogData(direction, messageType, size, payload); err != nil {
		log.Warnf("写入会话日志失败: %v", err)
	}
}

// LogDataMessage exposes structured logging for transport callbacks.
func (s *ChatSession) LogDataMessage(direction, messageType string, size int, payload any) {
	s.logDataMessage(direction, messageType, size, payload)
}
