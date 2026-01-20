package state

import (
	"backend-server/internal/domain/tools"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"backend-server/internal/domain/asr"
	utypes "backend-server/internal/domain/config/types"
	"backend-server/internal/domain/llm"
	llm_common "backend-server/internal/domain/llm/common"
	"backend-server/internal/domain/tts"

	"backend-server/internal/domain/audio"

	_ "backend-server/internal/domain/tools/functions"
	log "backend-server/internal/infrastructure/logger"

	"backend-server/internal/config"

	"github.com/cloudwego/eino/schema"
)

// Dialogue 表示对话历史
type Dialogue struct {
	Messages []*schema.Message
}

const (
	ClientStatusInit       = "init"
	ClientStatusListening  = "listening"
	ClientStatusListenStop = "listenStop"
	ClientStatusLLMStart   = "llmStart"
	ClientStatusTTSStart   = "ttsStart"
)

type SendAudioData func(audioData []byte) error

type AudioStreamStatsSnapshot struct {
	RecvFrames     uint64
	RecvBytes      uint64
	DroppedFrames  uint64
	DroppedBytes   uint64
	ConsumedFrames uint64
	ConsumedBytes  uint64
}

type AudioStreamStats struct {
	recvFrames     atomic.Uint64
	recvBytes      atomic.Uint64
	droppedFrames  atomic.Uint64
	droppedBytes   atomic.Uint64
	consumedFrames atomic.Uint64
	consumedBytes  atomic.Uint64
}

func (s *AudioStreamStats) RecordRecv(size int) {
	if size <= 0 {
		return
	}
	s.recvFrames.Add(1)
	s.recvBytes.Add(uint64(size))
}

func (s *AudioStreamStats) RecordDrop(size int) {
	if size <= 0 {
		return
	}
	s.droppedFrames.Add(1)
	s.droppedBytes.Add(uint64(size))
}

func (s *AudioStreamStats) RecordConsume(size int) {
	if size <= 0 {
		return
	}
	s.consumedFrames.Add(1)
	s.consumedBytes.Add(uint64(size))
}

func (s *AudioStreamStats) Snapshot() AudioStreamStatsSnapshot {
	if s == nil {
		return AudioStreamStatsSnapshot{}
	}
	return AudioStreamStatsSnapshot{
		RecvFrames:     s.recvFrames.Load(),
		RecvBytes:      s.recvBytes.Load(),
		DroppedFrames:  s.droppedFrames.Load(),
		DroppedBytes:   s.droppedBytes.Load(),
		ConsumedFrames: s.consumedFrames.Load(),
		ConsumedBytes:  s.consumedBytes.Load(),
	}
}

// ClientState 表示客户端状态
type ClientState struct {
	// 对话历史
	Dialogue *Dialogue
	// 打断状态
	Abort bool
	// 拾音模式
	ListenMode string
	// 设备ID
	DeviceID string
	// 远程Addr
	RemoteAddr string
	// 协议版本
	ProtocolVersion int
	// 会话ID
	SessionID string
	//设备配置
	DeviceConfig utypes.UConfig

	Vad
	Asr
	Llm

	// TTS 提供者
	TTSProvider tts.TTSProvider

	// 上下文控制
	Ctx    context.Context
	Cancel context.CancelFunc

	//prompt, 系统提示词
	SystemPrompt string

	InputAudioFormat  audio.AudioFormat //输入音频格式
	OutputAudioFormat audio.AudioFormat //输出音频格式

	// opus接收的音频数据缓冲区
	OpusAudioBuffer chan []byte

	AudioStats AudioStreamStats

	// pcm接收的音频数据缓冲区
	AsrAudioBuffer *AsrAudioBuffer

	VoiceStatus
	DefaultSilenceThresholdTime int64
	SessionCtx                  Ctx
	AfterAsrCtx                 Ctx

	UdpSendAudioData SendAudioData //发送音频数据
	Statistic        Statistic     //耗时统计
	MqttLastActiveTs int64         //最后活跃时间
	VadLastActiveTs  int64         //vad最后活跃时间, 超过 60s && 没有在tts则断开连接

	Status string //状态 listening, llmStart, ttsStart

	// 透传 TTS 管理器给 transport 做队列检查（由 session 初始化时注入）
	TTSManager interface {
		QueueEmpty() bool
	}

	IsTtsStart        bool   //是否tts开始
	IsWelcomeSpeaking bool   //是否已经欢迎语
	WakeWord          string //唤醒词文本，用于判断是否需要播放欢迎语

	autoRecoverReady   atomic.Bool //是否允许自动恢复监听
	ttsPlaybackWaitMs  atomic.Int64
	asrFinalizingUntil atomic.Int64
	pendingAsrRestart  atomic.Bool
	lastTTSText        atomic.Value
	lastTTSTextAt      atomic.Int64
}

// AddMessage 历史消息相关的方法开始
func (c *ClientState) AddMessage(msg *schema.Message) {
	if msg == nil {
		log.Warnf("尝试添加 nil 消息到对话历史")
		return
	}
	c.Dialogue.Messages = append(c.Dialogue.Messages, msg)
}

// SetTTSPlaybackWait overrides the playback wait duration after a TTS stream completes.
func (c *ClientState) SetTTSPlaybackWait(d time.Duration) {
	if d <= 0 {
		return
	}
	c.ttsPlaybackWaitMs.Store(d.Milliseconds())
}

// ConsumeTTSPlaybackWait returns the configured wait duration and resets it.
func (c *ClientState) ConsumeTTSPlaybackWait() time.Duration {
	if c == nil {
		return 0
	}
	ms := c.ttsPlaybackWaitMs.Swap(0)
	if ms <= 0 {
		return 0
	}
	return time.Duration(ms) * time.Millisecond
}

func (c *ClientState) GetMessages(count int) []*schema.Message {
	// 添加边界检查，防止数组越界
	if len(c.Dialogue.Messages) == 0 {
		return []*schema.Message{}
	}

	// 计算起始索引，确保不会越界
	startIndex := len(c.Dialogue.Messages) - count
	if startIndex < 0 {
		startIndex = 0
	}

	return AlignToolMessages(c.Dialogue.Messages[startIndex:])
}

// AlignToolMessages 保证 role:tool 消息中的 tool_call_id 与 role:assistant 消息中的 tool_calls 的 id 对应
// 如果不匹配则删除对应的 tool 消息，同时处理反向不匹配的场景
func AlignToolMessages(messages []*schema.Message) []*schema.Message {
	if len(messages) == 0 {
		return messages
	}

	// 收集所有 assistant 消息中的 tool_calls id
	validToolCallIDs := make(map[string]bool)
	// 收集所有 tool 消息中的 tool_call_id
	usedToolCallIDs := make(map[string]bool)

	// 第一遍遍历：收集 assistant 消息中的 tool_calls id 和 tool 消息中的 tool_call_id
	for _, msg := range messages {
		if msg == nil {
			continue
		}

		if msg.Role == schema.Assistant && len(msg.ToolCalls) > 0 {
			for _, toolCall := range msg.ToolCalls {
				if toolCall.ID != "" {
					validToolCallIDs[toolCall.ID] = true
				}
			}
		}

		if msg.Role == schema.Tool && msg.ToolCallID != "" {
			usedToolCallIDs[msg.ToolCallID] = true
		}
	}

	// 过滤消息，处理双向不匹配的情况
	var alignedMessages []*schema.Message
	for _, msg := range messages {
		if msg == nil {
			continue
		}

		// 如果是 tool 消息，检查 tool_call_id 是否有效
		if msg.Role == schema.Tool {
			if msg.ToolCallID != "" && validToolCallIDs[msg.ToolCallID] {
				alignedMessages = append(alignedMessages, msg)
			}
		} else if msg.Role == schema.Assistant && len(msg.ToolCalls) > 0 {
			// 处理 assistant 消息，检查是否有未使用的 tool_calls
			for _, toolCall := range msg.ToolCalls {
				if toolCall.ID != "" {
					if usedToolCallIDs[toolCall.ID] {
						alignedMessages = append(alignedMessages, msg)
					} else {
						continue
					}
				}
			}
		} else {
			// 其他类型的消息直接保留
			alignedMessages = append(alignedMessages, msg)
		}
	}

	return alignedMessages
}

func (c *ClientState) InitMessages(messages []*schema.Message) error {
	c.Dialogue.Messages = AlignToolMessages(messages)
	return nil
}

//历史消息相关的方法结束

func (c *ClientState) SetTtsStart(isStart bool) {
	c.IsTtsStart = isStart
}

func (c *ClientState) GetTtsStart() bool {
	return c.IsTtsStart
}

func (c *ClientState) SetLastTTSText(text string) {
	if c == nil {
		return
	}
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return
	}
	c.lastTTSText.Store(trimmed)
	c.lastTTSTextAt.Store(time.Now().UnixMilli())
}

func (c *ClientState) GetLastTTSText() (string, int64) {
	if c == nil {
		return "", 0
	}
	raw := c.lastTTSText.Load()
	if raw == nil {
		return "", 0
	}
	text, ok := raw.(string)
	if !ok {
		return "", 0
	}
	return text, c.lastTTSTextAt.Load()
}

func (c *ClientState) GetMaxIdleDuration() int64 {
	cfg := config.GetConfig()
	maxIdleDuration := int64(cfg.Chat.MaxIdleDuration)
	if maxIdleDuration == 0 {
		maxIdleDuration = time.Hour.Milliseconds()
	}
	return maxIdleDuration
}

func (c *ClientState) UpdateLastActiveTs() {
	c.MqttLastActiveTs = time.Now().Unix()
}

func (c *ClientState) IsActive() bool {
	diff := time.Now().Unix() - c.MqttLastActiveTs
	return c.MqttLastActiveTs > 0 && diff <= ClientActiveTs
}

func (c *ClientState) SetStatus(status string) {
	c.Status = status
}

func (c *ClientState) GetStatus() string {
	return c.Status
}

func (c *ClientState) ResetSessionCtx() {
	c.SessionCtx.Lock()
	defer c.SessionCtx.Unlock()
	if c.SessionCtx.Ctx == nil {
		c.SessionCtx.Ctx, c.SessionCtx.Cancel = context.WithCancel(c.Ctx)
	}
}

func (c *ClientState) CancelSessionCtx() {
	c.SessionCtx.Lock()
	defer c.SessionCtx.Unlock()
	if c.SessionCtx.Ctx != nil {
		log.Debugf("CancelSessionCtx: device=%s session=%s status=%s mode=%s",
			c.DeviceID, c.SessionID, c.Status, c.ListenMode)
		c.SessionCtx.Cancel()
		c.SessionCtx.Ctx = nil
	}
}

func (c *ClientState) GetSessionCtx() context.Context {
	c.SessionCtx.Lock()
	defer c.SessionCtx.Unlock()
	if c.SessionCtx.Ctx == nil {
		c.SessionCtx.Ctx, c.SessionCtx.Cancel = context.WithCancel(c.Ctx)
	}
	return c.SessionCtx.Ctx
}

func (c *ClientState) ResetAfterAsrCtx(baseCtx context.Context) {
	c.AfterAsrCtx.Lock()
	defer c.AfterAsrCtx.Unlock()
	if c.AfterAsrCtx.Ctx == nil {
		if baseCtx == nil {
			baseCtx = c.GetSessionCtx()
		}
		c.AfterAsrCtx.Ctx, c.AfterAsrCtx.Cancel = context.WithCancel(baseCtx)
	}
}

func (c *ClientState) CancelAfterAsrCtx() {
	c.AfterAsrCtx.Lock()
	defer c.AfterAsrCtx.Unlock()
	if c.AfterAsrCtx.Ctx != nil {
		log.Debugf("CancelAfterAsrCtx: device=%s session=%s status=%s mode=%s",
			c.DeviceID, c.SessionID, c.Status, c.ListenMode)
		c.AfterAsrCtx.Cancel()
		c.AfterAsrCtx.Ctx = nil
	}
}

func (c *ClientState) GetAfterAsrCtx(baseCtx context.Context) context.Context {
	c.AfterAsrCtx.Lock()
	defer c.AfterAsrCtx.Unlock()
	if c.AfterAsrCtx.Ctx == nil {
		if baseCtx == nil {
			baseCtx = c.GetSessionCtx()
		}
		c.AfterAsrCtx.Ctx, c.AfterAsrCtx.Cancel = context.WithCancel(baseCtx)
	}
	return c.AfterAsrCtx.Ctx
}

type Ctx struct {
	sync.RWMutex
	Ctx    context.Context
	Cancel context.CancelFunc
}

func (c *ClientState) getLLMProvider() (llm.LLMProvider, error) {
	llmConfig := c.DeviceConfig.Llm
	if llmConfig.Config == nil {
		llmConfig.Config = make(map[string]interface{})
	}

	var llmType string
	if rawType, ok := llmConfig.Config["type"]; ok {
		if s, ok := rawType.(string); ok && s != "" {
			llmType = s
		}
	}

	if llmType == "" {
		llmType = llmConfig.Provider
		if llmType == "" {
			log.Errorf("getLLMProvider err: not found llm type: %+v", llmConfig)
			return nil, fmt.Errorf("llm config type not found")
		}
		llmConfig.Config["type"] = llmType
	}

	llmProvider, err := llm.GetLLMProvider(llmType, llmConfig.Config)
	if err != nil {
		return nil, fmt.Errorf("创建 LLM 提供者失败: %v", err)
	}
	return llmProvider, nil
}

// GetDeviceMemoryConfig 获取设备的记忆配置
func (c *ClientState) GetDeviceMemoryConfig() *utypes.MemoryConfig {
	if c.DeviceConfig.Memory.Provider == "" {
		return nil
	}
	return &c.DeviceConfig.Memory
}

// HasCustomMemoryConfig 检查设备是否有自定义记忆配置
func (c *ClientState) HasCustomMemoryConfig() bool {
	memoryConfig := c.DeviceConfig.Memory
	// 如果Provider不为空，说明有自定义配置
	return memoryConfig.Provider != ""
}

func (c *ClientState) InitLlm(tools *tools.FCTools) error {
	ctx, cancel := context.WithCancel(c.Ctx)

	llmProvider, err := c.getLLMProvider()
	if err != nil {
		log.Errorf("创建 LLM 提供者失败: %v", err)
		cancel()
		return err
	}

	c.Llm = Llm{
		Ctx:         ctx,
		Cancel:      cancel,
		LLMProvider: llmProvider,
		FCTools:     tools,
	}
	return nil
}

func (c *ClientState) InitAsr() error {
	asrConfig := c.DeviceConfig.Asr

	log.Infof("初始化asr, asrConfig: %+v", asrConfig)

	// 先取消之前的context（如果存在）
	if c.Asr.Cancel != nil {
		c.Asr.Cancel()
	}

	//初始化asr
	asrProvider, err := asr.NewAsrProvider(asrConfig.Provider, asrConfig.Config)
	if err != nil {
		log.Errorf("创建asr提供者失败: %v", err)
		return fmt.Errorf("创建asr提供者失败: %v", err)
	}
	ctx, cancel := context.WithCancel(c.Ctx)
	ringBufferFrames := DefaultRingBufferFrames
	if rawFrames, ok := asrConfig.Config["ring_buffer_frames"]; ok {
		switch v := rawFrames.(type) {
		case int:
			if v > 0 {
				ringBufferFrames = v
			}
		case int64:
			if v > 0 {
				ringBufferFrames = int(v)
			}
		case float64:
			if v > 0 {
				ringBufferFrames = int(v)
			}
		}
	}
	c.Asr = Asr{
		Ctx:              ctx,
		Cancel:           cancel,
		AsrProvider:      asrProvider,
		AsrAudioChannel:  NewRingAudioChannel(ringBufferFrames),
		AsrEnd:           make(chan bool, 1),
		AsrResult:        bytes.Buffer{},
		RingBufferFrames: ringBufferFrames,
	}

	if rawAutoEnd, ok := asrConfig.Config["auto_end"]; ok {
		if autoEnd, ok := rawAutoEnd.(bool); ok {
			c.Asr.AutoEnd = autoEnd
		}
	}
	return nil
}

func (c *ClientState) Destroy() {
	c.Asr.Stop()
	c.Vad.Reset()

	c.VoiceStatus.Reset()
	c.AsrAudioBuffer.ClearAsrAudioData()

	// 取消LLM和ASR的context
	if c.Llm.Cancel != nil {
		c.Llm.Cancel()
	}
	if c.Asr.Cancel != nil {
		c.Asr.Cancel()
	}
	if closer, ok := c.Asr.AsrProvider.(interface{ Close() error }); ok {
		if err := closer.Close(); err != nil {
			log.Warnf("关闭ASR提供者失败: %v", err)
		}
	}

	c.ResetSessionCtx()
	c.CancelAfterAsrCtx()
	c.Statistic.Reset()
	c.SetTtsStart(false)
	c.IsWelcomeSpeaking = false // reset welcome speech flag
	c.ClearAsrFinalizing()
}

func (c *ClientState) SetAsrPcmFrameSize(sampleRate int, channels int, perFrameDuration int) {
	//	c.AsrAudioBuffer.PcmFrameSize = sampleRate * channels * perFrameDuration / 1000
	// WebRTC VAD 只支持单声道处理，frameSize 表示采样点数量（不包含声道数）
	// 正确计算: sampleRate * perFrameDuration / 1000
	// 错误计算: sampleRate * channels * perFrameDuration / 1000 (会导致 VAD 检测失败)
	frameSize := sampleRate * perFrameDuration / 1000
	if frameSize <= 0 {
		// fallback to a reasonable default (20ms frames)
		if perFrameDuration <= 0 {
			perFrameDuration = 20
		}
		frameSize = sampleRate * perFrameDuration / 1000
		if frameSize <= 0 {
			frameSize = sampleRate / 50
		}
		if frameSize <= 0 {
			frameSize = 320 // assume 16kHz mono 20ms
		}
	}

	maxFrames := defaultAsrBufferDurationMs / perFrameDuration
	if maxFrames < 4 {
		maxFrames = 4
	}

	c.AsrAudioBuffer.Configure(frameSize, maxFrames)
}

func (c *ClientState) OnManualStop() {
	log.Infof("收到手动停止消息")
	c.onVoiceSilence(true)
}

func (c *ClientState) OnVoiceSilence() {
	c.onVoiceSilence(false)
}

func (c *ClientState) onVoiceSilence(forceStop bool) {
	c.SetAutoRecoverReady(false)
	if forceStop || c.ListenMode != "realtime" {
		c.SetClientVoiceStop(true) //设置停止说话标志位, 此时收到的音频数据不会进vad
		c.SetStatus(ClientStatusListenStop)
	}
	if !forceStop && c.ListenMode == "realtime" {
		c.StartAsrFinalizing(800 * time.Millisecond)
	}
	//客户端停止说话
	c.Asr.Stop() //停止asr并获取结果，进行llm
	//释放vad
	c.Vad.Reset() //释放vad实例
	if !forceStop && c.ListenMode == "realtime" {
		c.VoiceStatus.Reset()
	}
	//asr统计
	c.SetStartAsrTs() //进行asr统计
}

func (c *ClientState) SetAutoRecoverReady(ready bool) {
	c.autoRecoverReady.Store(ready)
}

func (c *ClientState) AutoRecoverReady() bool {
	return c.autoRecoverReady.Load()
}

func (c *ClientState) StartAsrFinalizing(timeout time.Duration) {
	if timeout <= 0 {
		return
	}
	c.asrFinalizingUntil.Store(time.Now().Add(timeout).UnixMilli())
}

func (c *ClientState) ClearAsrFinalizing() {
	c.asrFinalizingUntil.Store(0)
}

func (c *ClientState) IsAsrFinalizing() bool {
	until := c.asrFinalizingUntil.Load()
	return until > 0 && time.Now().UnixMilli() < until
}

func (c *ClientState) SetPendingAsrRestart(pending bool) {
	c.pendingAsrRestart.Store(pending)
}

func (c *ClientState) ConsumePendingAsrRestart() bool {
	return c.pendingAsrRestart.Swap(false)
}

type Llm struct {
	Ctx    context.Context
	Cancel context.CancelFunc
	// LLM 提供者
	LLMProvider llm.LLMProvider

	FCTools *tools.FCTools
	//asr to text接收的通道
	LLmRecvChannel chan llm_common.LLMResponseStruct
}

// ClientMessage 表示客户端消息
type ClientMessage struct {
	Type        string             `json:"type"`
	DeviceID    string             `json:"device_id,omitempty"`
	SessionID   string             `json:"session_id,omitempty"`
	Text        string             `json:"text,omitempty"`
	Mode        string             `json:"mode,omitempty"`
	State       string             `json:"state,omitempty"`
	Token       string             `json:"token,omitempty"`
	DeviceMac   string             `json:"device_mac,omitempty"`
	Version     int                `json:"version,omitempty"`
	Transport   string             `json:"transport,omitempty"`
	Features    map[string]bool    `json:"features,omitempty"`
	AudioParams *audio.AudioFormat `json:"audio_params,omitempty"`
	PayLoad     json.RawMessage    `json:"payload,omitempty"`
}
