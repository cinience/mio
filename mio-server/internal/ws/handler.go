package ws

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	appconfig "mio-server/internal/config"
	"mio-server/internal/group"
	"mio-server/internal/storage"
	"mio-server/internal/xiaozhi"
)

type Handler struct {
	logger   *zap.Logger
	upgrader websocket.Upgrader
	config   appconfig.Config
	group    *group.Manager
	sessions map[string]*session
	mu       sync.Mutex
}

type incomingMessage struct {
	Type       string    `json:"type"`
	Text       string    `json:"text,omitempty"`
	File       string    `json:"file,omitempty"`
	Audio      []float64 `json:"audio,omitempty"`
	RequestID  string    `json:"request_id,omitempty"`
	Success    *bool     `json:"success,omitempty"`
	Image      string    `json:"image,omitempty"`
	MimeType   string    `json:"mime_type,omitempty"`
	Message    string    `json:"message,omitempty"`
	InviteeUID string    `json:"invitee_uid,omitempty"`
	TargetUID  string    `json:"target_uid,omitempty"`
	HistoryUID string    `json:"history_uid,omitempty"`
}

type session struct {
	conn             *websocket.Conn
	sendMu           sync.Mutex
	logger           *zap.Logger
	xiaozhi          *xiaozhi.Client
	handler          *Handler
	clientUID        string
	confName         string
	confUID          string
	live2dModelName  string
	characterName    string
	avatar           string
	historyUID       string
	llmText          string
	inConversation   bool
	ttsActive        bool
	displaySent      bool
	frameDuration    int
	listening        bool
	audioFormat      string
	unsupportedAudio bool
	sampleRate       int
	channels         int
	micBuffer        []float32
	frameSamples     int
	ttsBuffer        []byte
	ttsSampleRate    int
	ttsChannels      int

	mcpMu          sync.Mutex
	mcpWaiters     map[string]chan captureResponse
	mcpVisionURL   string
	mcpVisionToken string
	deviceID       string
	clientID       string
}

const ttsChunkDurationMs = 300

type captureResponse struct {
	Success  bool
	Image    string
	MimeType string
	Message  string
}

type mcpRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	ID      json.RawMessage `json:"id"`
	Params  json.RawMessage `json:"params"`
}

type mcpTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

type mcpResult struct {
	Content []mcpContent `json:"content"`
	IsError bool         `json:"isError"`
}

type mcpContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func NewHandler(logger *zap.Logger, cfg appconfig.Config) *Handler {
	return &Handler{
		logger:   logger,
		config:   cfg,
		group:    group.NewManager(),
		sessions: make(map[string]*session),
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		},
	}
}

func (h *Handler) Handle(w http.ResponseWriter, r *http.Request) {
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.logger.Warn("ws upgrade failed", zap.Error(err))
		return
	}
	defer conn.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sessionID := fmt.Sprintf("%d", time.Now().UnixNano())
	xzCfg := xiaozhi.Config{
		BackendURL:      h.config.XiaoZhiBackendURL,
		ProtocolVersion: h.config.XiaoZhiProtocolVersion,
		AudioParams: xiaozhi.AudioParams{
			Format:        h.config.XiaoZhiAudioFormat,
			SampleRate:    h.config.XiaoZhiSampleRate,
			Channels:      h.config.XiaoZhiChannels,
			FrameDuration: h.config.XiaoZhiFrameDuration,
		},
		DeviceID:    fallbackID(h.config.XiaoZhiDeviceID, "mio-device-"+sessionID),
		ClientID:    fallbackID(h.config.XiaoZhiClientID, "mio-client-"+sessionID),
		AccessToken: h.config.XiaoZhiAccessToken,
	}

	sess := &session{
		conn:            conn,
		logger:          h.logger,
		handler:         h,
		clientUID:       sessionID,
		confName:        h.config.CharacterConfig.ConfName,
		confUID:         h.config.CharacterConfig.ConfUID,
		live2dModelName: h.config.CharacterConfig.Live2dModelName,
		characterName:   h.config.CharacterConfig.CharacterName,
		avatar:          h.config.CharacterConfig.Avatar,
		frameDuration:   h.config.XiaoZhiFrameDuration,
		audioFormat:     h.config.XiaoZhiAudioFormat,
		sampleRate:      h.config.XiaoZhiSampleRate,
		channels:        h.config.XiaoZhiChannels,
		frameSamples:    h.config.XiaoZhiSampleRate * h.config.XiaoZhiFrameDuration / 1000,
		mcpWaiters:      make(map[string]chan captureResponse),
		deviceID:        xzCfg.DeviceID,
		clientID:        xzCfg.ClientID,
	}

	h.registerSession(sess)
	sess.sendModelAndConf()

	callbacks := xiaozhi.Callbacks{
		OnSTT: func(text string) {
			sess.sendJSON(map[string]any{"type": "user-input-transcription", "text": text})
		},
		OnLLM: func(text string, state string) {
			sess.ensureConversation()
			sess.applyLLMText(text, state)
		},
		OnText: func(text string) {
			sess.ensureConversation()
			sess.llmText = text
			sess.sendJSON(map[string]any{"type": "full-text", "text": sess.llmText})
		},
		OnTTS: func(state string, text string) {
			sess.handleTTS(state, text)
		},
		OnMCP: func(payload json.RawMessage) {
			sess.handleMCP(ctx, payload)
		},
		OnGoodbye: func() {
			sess.sendJSON(map[string]any{"type": "error", "message": "xiaozhi backend disconnected"})
			sess.endConversation()
		},
		OnAudio: func(frame xiaozhi.AudioFrame) {
			sess.handleAudio(frame)
		},
		OnError: func(err error) {
			sess.logger.Warn("xiaozhi error", zap.Error(err))
		},
	}

	sess.xiaozhi = xiaozhi.NewClient(xzCfg, callbacks, h.logger)
	sess.xiaozhi.Connect(ctx)

	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			sess.logger.Debug("ws connection closed", zap.Error(err))
			break
		}
		var msg incomingMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			sess.sendJSON(map[string]any{"type": "error", "message": "invalid json"})
			continue
		}
		sess.handleIncoming(ctx, msg)
	}

	sess.xiaozhi.Close()
	h.unregisterSession(sess.clientUID)
}

func (s *session) handleIncoming(ctx context.Context, msg incomingMessage) {
	switch msg.Type {
	case "text-input":
		text := msg.Text
		if text == "" {
			return
		}
		if err := s.xiaozhi.SendTextInput(ctx, text); err != nil {
			s.sendJSON(map[string]any{"type": "error", "message": err.Error()})
		}
	case "interrupt-signal":
		if err := s.xiaozhi.Abort(ctx); err != nil {
			s.sendJSON(map[string]any{"type": "error", "message": err.Error()})
		}
		s.endConversation()
	case "mic-audio-data":
		s.handleMicAudio(ctx, msg.Audio)
	case "mic-audio-end":
		s.handleMicEnd(ctx)
	case "mcp-capture-response":
		s.handleCaptureResponse(msg)
	case "frontend-playback-complete":
		s.sendJSON(map[string]any{"type": "force-new-message"})
	case "fetch-configs":
		s.handleFetchConfigs(ctx)
	case "switch-config":
		s.handleConfigSwitch(ctx, msg.File)
	case "fetch-backgrounds":
		s.handleFetchBackgrounds(ctx)
	case "request-init-config":
		s.handleInitConfig(ctx)
	case "fetch-history-list":
		s.handleHistoryList(ctx)
	case "fetch-and-set-history":
		s.handleFetchHistory(ctx, msg.HistoryUID)
	case "create-new-history":
		s.handleCreateHistory(ctx)
	case "delete-history":
		s.handleDeleteHistory(ctx, msg.HistoryUID)
	case "request-group-info":
		s.handleGroupInfo(ctx)
	case "add-client-to-group":
		s.handleAddToGroup(ctx, msg.InviteeUID)
	case "remove-client-from-group":
		s.handleRemoveFromGroup(ctx, msg.TargetUID)
	case "ai-speak-signal":
		s.sendJSON(map[string]any{"type": "error", "message": "proactive speak not supported in XiaoZhi mode"})
	case "heartbeat":
		return
	default:
		return
	}
}

func (s *session) handleMicAudio(ctx context.Context, audio []float64) {
	if len(audio) == 0 {
		return
	}
	if !s.listening {
		_ = s.xiaozhi.SendListenState(ctx, "start")
		s.listening = true
	}
	if s.audioFormat == "opus" {
		s.handleMicAudioOpus(ctx, audio)
		return
	}
	if s.audioFormat != "pcm16" && s.audioFormat != "pcm" {
		if !s.unsupportedAudio {
			s.unsupportedAudio = true
			s.sendJSON(map[string]any{"type": "error", "message": "unsupported xiaozhi_audio_format for mic input"})
		}
		return
	}
	pcm := float32ToPCM16(audio)
	if err := s.xiaozhi.SendAudio(ctx, pcm); err != nil {
		s.sendJSON(map[string]any{"type": "error", "message": err.Error()})
	}
}

func (s *session) handleMicEnd(ctx context.Context) {
	if s.audioFormat == "opus" {
		s.flushMicBuffer(ctx)
	}
	if s.listening {
		_ = s.xiaozhi.SendListenState(ctx, "stop")
		s.listening = false
	}
	s.ensureConversation()
	if s.llmText == "" {
		s.sendJSON(map[string]any{"type": "full-text", "text": "Thinking..."})
	}
}

func (s *session) handleMicAudioOpus(ctx context.Context, audio []float64) {
	s.micBuffer = append(s.micBuffer, float64ToFloat32(audio)...)
	frameSize := s.frameSamples * s.channels
	if frameSize <= 0 {
		frameSize = 960 * maxInt(1, s.channels)
	}
	for len(s.micBuffer) >= frameSize {
		frame := s.micBuffer[:frameSize]
		s.micBuffer = s.micBuffer[frameSize:]
		encoded, err := s.xiaozhi.EncodeOpusFloat(frame)
		if err != nil {
			s.sendJSON(map[string]any{"type": "error", "message": err.Error()})
			return
		}
		if err := s.xiaozhi.SendAudio(ctx, encoded); err != nil {
			s.sendJSON(map[string]any{"type": "error", "message": err.Error()})
			return
		}
	}
}

func (s *session) flushMicBuffer(ctx context.Context) {
	frameSize := s.frameSamples * s.channels
	if frameSize <= 0 {
		frameSize = 960 * maxInt(1, s.channels)
	}
	if len(s.micBuffer) == 0 {
		return
	}
	padding := frameSize - (len(s.micBuffer) % frameSize)
	if padding != frameSize {
		for i := 0; i < padding; i++ {
			s.micBuffer = append(s.micBuffer, 0)
		}
	}
	for len(s.micBuffer) >= frameSize {
		frame := s.micBuffer[:frameSize]
		s.micBuffer = s.micBuffer[frameSize:]
		encoded, err := s.xiaozhi.EncodeOpusFloat(frame)
		if err != nil {
			s.sendJSON(map[string]any{"type": "error", "message": err.Error()})
			return
		}
		if err := s.xiaozhi.SendAudio(ctx, encoded); err != nil {
			s.sendJSON(map[string]any{"type": "error", "message": err.Error()})
			return
		}
	}
}

func (s *session) handleCaptureResponse(msg incomingMessage) {
	if msg.RequestID == "" || msg.Success == nil {
		return
	}
	resp := captureResponse{
		Success:  *msg.Success,
		Image:    msg.Image,
		MimeType: msg.MimeType,
		Message:  msg.Message,
	}
	s.mcpMu.Lock()
	ch, ok := s.mcpWaiters[msg.RequestID]
	if ok {
		delete(s.mcpWaiters, msg.RequestID)
	}
	s.mcpMu.Unlock()
	if ok {
		ch <- resp
	}
}

func (s *session) handleFetchConfigs(ctx context.Context) {
	files, err := appconfig.ScanConfigFiles(s.handler.config.RootDir, s.handler.config.ConfigAltsDir)
	if err != nil {
		s.sendJSON(map[string]any{"type": "error", "message": err.Error()})
		return
	}
	s.sendJSON(map[string]any{"type": "config-files", "configs": files})
}

func (s *session) handleConfigSwitch(ctx context.Context, filename string) {
	if filename == "" {
		return
	}
	configPath := filename
	if filename != "conf.yaml" {
		configPath = filepath.Join(s.handler.config.ConfigAltsDir, filepath.Base(filename))
	} else {
		configPath = filepath.Join(s.handler.config.RootDir, "conf.yaml")
	}
	conf, err := appconfig.ReadCharacterConfig(configPath)
	if err != nil {
		s.sendJSON(map[string]any{"type": "error", "message": err.Error()})
		return
	}
	s.confName = conf.ConfName
	s.confUID = conf.ConfUID
	s.live2dModelName = conf.Live2dModelName
	s.characterName = conf.CharacterName
	s.avatar = conf.Avatar
	s.historyUID = ""

	s.sendModelAndConf()
	s.sendJSON(map[string]any{"type": "config-switched"})
}

func (s *session) handleFetchBackgrounds(ctx context.Context) {
	files := appconfig.ScanBackgrounds(s.handler.config.BackgroundsDir)
	s.sendJSON(map[string]any{"type": "background-files", "files": files})
}

func (s *session) handleInitConfig(ctx context.Context) {
	s.sendModelAndConf()
}

func (s *session) handleHistoryList(ctx context.Context) {
	histories := storage.GetHistoryList(s.handler.config.ChatHistoryDir, s.confUID)
	s.sendJSON(map[string]any{"type": "history-list", "histories": histories})
}

func (s *session) handleFetchHistory(ctx context.Context, historyUID string) {
	if historyUID == "" {
		return
	}
	messages, err := storage.GetHistory(s.handler.config.ChatHistoryDir, s.confUID, historyUID)
	if err != nil {
		s.sendJSON(map[string]any{"type": "error", "message": err.Error()})
		return
	}
	s.historyUID = historyUID
	s.sendJSON(map[string]any{"type": "history-data", "messages": messages})
}

func (s *session) handleCreateHistory(ctx context.Context) {
	historyUID, err := storage.CreateHistory(s.handler.config.ChatHistoryDir, s.confUID)
	if err != nil {
		s.sendJSON(map[string]any{"type": "error", "message": err.Error()})
		return
	}
	s.historyUID = historyUID
	s.sendJSON(map[string]any{"type": "new-history-created", "history_uid": historyUID})
}

func (s *session) handleDeleteHistory(ctx context.Context, historyUID string) {
	if historyUID == "" {
		return
	}
	success := storage.DeleteHistory(s.handler.config.ChatHistoryDir, s.confUID, historyUID)
	s.sendJSON(map[string]any{"type": "history-deleted", "success": success, "history_uid": historyUID})
	if success && s.historyUID == historyUID {
		s.historyUID = ""
	}
}

func (s *session) handleGroupInfo(ctx context.Context) {
	s.handler.sendGroupUpdate(s.clientUID)
}

func (s *session) handleAddToGroup(ctx context.Context, inviteeUID string) {
	if inviteeUID == "" {
		return
	}
	success, message, members := s.handler.group.AddClient(s.clientUID, inviteeUID)
	s.sendJSON(map[string]any{"type": "group-operation-result", "success": success, "message": message})
	if success {
		s.handler.broadcastGroupUpdate(members)
	}
}

func (s *session) handleRemoveFromGroup(ctx context.Context, targetUID string) {
	if targetUID == "" {
		return
	}
	success, message, members := s.handler.group.RemoveClientFromGroup(s.clientUID, targetUID)
	s.sendJSON(map[string]any{"type": "group-operation-result", "success": success, "message": message})
	if success {
		s.handler.broadcastGroupUpdate(members)
	}
}

func (s *session) ensureConversation() {
	if s.inConversation {
		return
	}
	s.inConversation = true
	s.llmText = ""
	s.ttsBuffer = nil
	s.ttsSampleRate = 0
	s.ttsChannels = 0
	s.sendJSON(map[string]any{"type": "control", "text": "conversation-chain-start"})
}

func (s *session) endConversation() {
	if !s.inConversation {
		return
	}
	s.inConversation = false
	s.ttsActive = false
	s.displaySent = false
	s.llmText = ""
	s.ttsBuffer = nil
	s.ttsSampleRate = 0
	s.ttsChannels = 0
	s.sendJSON(map[string]any{"type": "control", "text": "conversation-chain-end"})
}

func (s *session) handleTTS(state string, text string) {
	switch state {
	case "sentence_start":
		if text == "" {
			return
		}
		s.ensureConversation()
		s.llmText += text
		s.sendJSON(map[string]any{"type": "full-text", "text": s.llmText})
	case "start":
		s.ensureConversation()
		s.ttsActive = true
		s.displaySent = false
		s.ttsBuffer = nil
		s.ttsSampleRate = 0
		s.ttsChannels = 0
		if s.llmText == "" {
			s.sendJSON(map[string]any{"type": "full-text", "text": "Thinking..."})
		}
	case "stop":
		s.ttsActive = false
		s.flushTTSAudio(true)
		s.sendJSON(map[string]any{"type": "backend-synth-complete"})
		s.endConversation()
	}
}

func (s *session) applyLLMText(text string, state string) {
	if state == "stream" {
		s.llmText += text
	} else {
		s.llmText = text
	}
	s.sendJSON(map[string]any{"type": "full-text", "text": s.llmText})
}

func (s *session) handleAudio(frame xiaozhi.AudioFrame) {
	if !s.ttsActive {
		return
	}
	if len(frame.PCM) == 0 {
		return
	}
	if s.ttsSampleRate == 0 {
		s.ttsSampleRate = frame.SampleRate
		s.ttsChannels = frame.Channels
	} else if s.ttsSampleRate != frame.SampleRate || s.ttsChannels != frame.Channels {
		s.flushTTSAudio(true)
		s.ttsSampleRate = frame.SampleRate
		s.ttsChannels = frame.Channels
	}
	s.ttsBuffer = append(s.ttsBuffer, frame.PCM...)
	s.flushTTSAudio(false)
}

func (s *session) flushTTSAudio(final bool) {
	if len(s.ttsBuffer) == 0 {
		return
	}
	sampleRate := s.ttsSampleRate
	channels := s.ttsChannels
	if sampleRate <= 0 || channels <= 0 {
		return
	}
	chunkFrames := sampleRate * ttsChunkDurationMs / 1000
	if chunkFrames <= 0 {
		chunkFrames = (len(s.ttsBuffer) / 2) / channels
	}
	chunkBytes := chunkFrames * channels * 2
	if chunkBytes <= 0 {
		return
	}
	for len(s.ttsBuffer) >= chunkBytes {
		chunk := s.ttsBuffer[:chunkBytes]
		s.ttsBuffer = s.ttsBuffer[chunkBytes:]
		s.sendAudioChunk(chunk, sampleRate, channels)
	}
	if final && len(s.ttsBuffer) > 0 {
		chunk := s.ttsBuffer
		s.ttsBuffer = nil
		s.sendAudioChunk(chunk, sampleRate, channels)
	}
}

func (s *session) sendAudioChunk(pcm []byte, sampleRate int, channels int) {
	sliceLength := s.frameDuration
	if sampleRate > 0 && channels > 0 {
		frames := (len(pcm) / 2) / channels
		if frames > 0 {
			sliceLength = int(math.Round(float64(frames*1000) / float64(sampleRate)))
		}
	}
	if sliceLength <= 0 {
		sliceLength = s.frameDuration
	}
	volumes := computeVolumes(pcm, sampleRate, channels, sliceLength)
	payload := map[string]any{
		"type":              "audio",
		"audio_pcm":         base64.StdEncoding.EncodeToString(pcm),
		"audio_format":      "pcm16",
		"audio_sample_rate": sampleRate,
		"audio_channels":    channels,
		"volumes":           volumes,
		"slice_length":      sliceLength,
		"display_text":      s.buildDisplayText(),
		"actions":           nil,
		"forwarded":         false,
	}
	if s.displaySent {
		payload["display_text"] = nil
	}
	s.sendJSON(payload)
	s.displaySent = true
}

func (s *session) buildDisplayText() map[string]any {
	if s.llmText == "" {
		return nil
	}
	return map[string]any{
		"text":   s.llmText,
		"name":   "",
		"avatar": "",
	}
}

func (s *session) sendModelAndConf() {
	modelInfo, err := appconfig.LoadModelInfo(s.live2dModelName, s.handler.config.ModelDictPath)
	if err != nil {
		s.sendJSON(map[string]any{"type": "error", "message": err.Error()})
		return
	}
	payload := map[string]any{
		"type":       "set-model-and-conf",
		"model_info": modelInfo,
		"conf_name":  s.confName,
		"conf_uid":   s.confUID,
		"client_uid": s.clientUID,
	}
	s.sendJSON(payload)
}

func (s *session) handleMCP(ctx context.Context, payload json.RawMessage) {
	var req mcpRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		s.replyMCPError(ctx, req.ID, "invalid MCP payload")
		return
	}
	if req.JSONRPC != "2.0" {
		s.replyMCPError(ctx, req.ID, "invalid JSON-RPC version")
		return
	}
	if req.Method == "" || len(req.ID) == 0 {
		s.replyMCPError(ctx, req.ID, "missing method or id")
		return
	}

	switch req.Method {
	case "initialize":
		s.handleMCPInitialize(ctx, req)
	case "tools/list":
		s.handleMCPToolsList(ctx, req)
	case "tools/call":
		s.handleMCPToolsCall(ctx, req)
	default:
		s.replyMCPError(ctx, req.ID, "method not implemented")
	}
}

func (s *session) handleMCPInitialize(ctx context.Context, req mcpRequest) {
	var params struct {
		Capabilities struct {
			Vision struct {
				URL   string `json:"url"`
				Token string `json:"token"`
			} `json:"vision"`
		} `json:"capabilities"`
	}
	if err := json.Unmarshal(req.Params, &params); err == nil {
		s.mcpVisionURL = params.Capabilities.Vision.URL
		s.mcpVisionToken = params.Capabilities.Vision.Token
	}
	result := map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{"tools": map[string]any{}},
		"serverInfo": map[string]any{
			"name":    "mio-gateway",
			"version": "1.0",
		},
	}
	s.replyMCPResult(ctx, req.ID, result)
}

func (s *session) handleMCPToolsList(ctx context.Context, req mcpRequest) {
	tools := []mcpTool{
		{
			Name:        "take_photo",
			Description: "Capture a camera frame and analyze it.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{"question": map[string]any{"type": "string", "default": ""}},
				"required":   []string{},
			},
		},
		{
			Name:        "take_screenshot",
			Description: "Capture a screen frame and analyze it.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"question": map[string]any{"type": "string", "default": ""},
					"display":  map[string]any{"type": "string", "default": ""},
				},
				"required": []string{},
			},
		},
	}
	result := map[string]any{"tools": tools}
	s.replyMCPResult(ctx, req.ID, result)
}

func (s *session) handleMCPToolsCall(ctx context.Context, req mcpRequest) {
	var params struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		s.replyMCPError(ctx, req.ID, "invalid tool call params")
		return
	}
	if params.Name == "" {
		s.replyMCPError(ctx, req.ID, "missing tool name")
		return
	}

	toolID := mcpIDString(req.ID)
	s.sendToolStatus(toolID, params.Name, "running", "")

	question := getStringArg(params.Arguments, "question")
	switch params.Name {
	case "take_photo":
		result, status, content := s.captureAndAnalyze(ctx, "camera", question, "")
		s.sendToolStatus(toolID, params.Name, status, content)
		s.replyMCPResult(ctx, req.ID, result)
	case "take_screenshot":
		display := getStringArg(params.Arguments, "display")
		result, status, content := s.captureAndAnalyze(ctx, "screen", question, display)
		s.sendToolStatus(toolID, params.Name, status, content)
		s.replyMCPResult(ctx, req.ID, result)
	default:
		s.replyMCPError(ctx, req.ID, "unknown tool")
	}
}

func (s *session) captureAndAnalyze(ctx context.Context, source string, question string, display string) (any, string, string) {
	id := newRequestID()
	ch := make(chan captureResponse, 1)
	s.mcpMu.Lock()
	s.mcpWaiters[id] = ch
	s.mcpMu.Unlock()

	s.sendJSON(map[string]any{
		"type":       "mcp-capture-request",
		"request_id": id,
		"source":     source,
		"question":   question,
		"display":    display,
	})

	resp, err := s.waitForCapture(ch, 30*time.Second)
	if err != nil {
		return mcpErrorResult(err.Error()), "error", err.Error()
	}
	if !resp.Success {
		return mcpErrorResult(resp.Message), "error", resp.Message
	}
	if s.mcpVisionURL == "" {
		return mcpErrorResult("vision service is not configured"), "error", "vision service is not configured"
	}

	imageBytes, err := decodeCaptureImage(resp.Image)
	if err != nil {
		return mcpErrorResult(err.Error()), "error", err.Error()
	}
	mimeType := resp.MimeType
	if mimeType == "" {
		mimeType = "image/jpeg"
	}

	result, err := s.callVision(ctx, imageBytes, mimeType, question)
	if err != nil {
		return mcpErrorResult(err.Error()), "error", err.Error()
	}

	return result, "completed", ""
}

func (s *session) callVision(ctx context.Context, image []byte, mimeType string, question string) (any, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("question", question); err != nil {
		return nil, err
	}
	fileWriter, err := writer.CreateFormFile("file", "capture.jpg")
	if err != nil {
		return nil, err
	}
	if _, err := fileWriter.Write(image); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.mcpVisionURL, &body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Device-Id", s.deviceID)
	req.Header.Set("Client-Id", s.clientID)
	if s.mcpVisionToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.mcpVisionToken)
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New(string(payload))
	}

	return parseVisionResponse(payload), nil
}

func parseVisionResponse(payload []byte) any {
	if json.Valid(payload) {
		var parsed any
		if err := json.Unmarshal(payload, &parsed); err == nil {
			return parsed
		}
	}
	return mcpResult{
		Content: []mcpContent{{Type: "text", Text: string(payload)}},
		IsError: false,
	}
}

func (s *session) waitForCapture(ch chan captureResponse, timeout time.Duration) (captureResponse, error) {
	select {
	case resp := <-ch:
		return resp, nil
	case <-time.After(timeout):
		return captureResponse{}, errors.New("capture timeout")
	}
}

func (s *session) replyMCPResult(ctx context.Context, id json.RawMessage, result any) {
	payload := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  result,
	}
	_ = s.xiaozhi.SendMCP(ctx, payload)
}

func (s *session) replyMCPError(ctx context.Context, id json.RawMessage, message string) {
	payload := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"error":   map[string]any{"message": message},
	}
	_ = s.xiaozhi.SendMCP(ctx, payload)
}

func newRequestID() string {
	return fmt.Sprintf("req-%d", time.Now().UnixNano())
}

func (s *session) sendToolStatus(toolID string, toolName string, status string, content string) {
	if status == "" {
		return
	}
	payload := map[string]any{
		"type":      "tool_call_status",
		"tool_id":   toolID,
		"tool_name": toolName,
		"status":    status,
		"content":   content,
		"timestamp": time.Now().Format(time.RFC3339),
	}
	s.sendJSON(payload)
}

func mcpIDString(id json.RawMessage) string {
	if len(id) == 0 {
		return ""
	}
	var str string
	if err := json.Unmarshal(id, &str); err == nil {
		return str
	}
	var num float64
	if err := json.Unmarshal(id, &num); err == nil {
		return fmt.Sprintf("%v", num)
	}
	return string(id)
}

func getStringArg(args map[string]any, key string) string {
	if args == nil {
		return ""
	}
	value, ok := args[key]
	if !ok {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}

func decodeCaptureImage(data string) ([]byte, error) {
	if data == "" {
		return nil, errors.New("empty capture image")
	}
	if strings.HasPrefix(data, "data:") {
		parts := strings.SplitN(data, ",", 2)
		if len(parts) != 2 {
			return nil, errors.New("invalid data url")
		}
		return base64.StdEncoding.DecodeString(parts[1])
	}
	return base64.StdEncoding.DecodeString(data)
}

func mcpErrorResult(message string) mcpResult {
	if message == "" {
		message = "capture failed"
	}
	return mcpResult{
		Content: []mcpContent{{Type: "text", Text: message}},
		IsError: true,
	}
}

func (s *session) sendJSON(payload any) {
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	if err := s.conn.WriteJSON(payload); err != nil {
		s.logger.Debug("ws send failed", zap.Error(err))
	}
}

func fallbackID(value string, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

func float32ToPCM16(samples []float64) []byte {
	if len(samples) == 0 {
		return nil
	}
	pcm := make([]byte, len(samples)*2)
	for i, sample := range samples {
		value := clamp(sample, -1.0, 1.0)
		scaled := int16(math.Round(value * 32767.0))
		binary.LittleEndian.PutUint16(pcm[i*2:], uint16(scaled))
	}
	return pcm
}

func clamp(value float64, min float64, max float64) float64 {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func computeVolumes(pcm []byte, sampleRate int, channels int, frameDuration int) []float64 {
	if len(pcm) == 0 || sampleRate <= 0 || channels <= 0 {
		return nil
	}
	samples := len(pcm) / 2
	if samples == 0 {
		return nil
	}
	frames := samples / channels
	if frames == 0 {
		return nil
	}
	chunkSize := sampleRate * frameDuration / 1000
	if chunkSize <= 0 {
		chunkSize = frames
	}

	volumes := make([]float64, 0, (frames+chunkSize-1)/chunkSize)
	for start := 0; start < frames; start += chunkSize {
		end := start + chunkSize
		if end > frames {
			end = frames
		}
		rms := rmsPCM(pcm, channels, start, end)
		volumes = append(volumes, rms)
	}

	maxVolume := 0.0
	for _, v := range volumes {
		if v > maxVolume {
			maxVolume = v
		}
	}
	if maxVolume == 0 {
		for i := range volumes {
			volumes[i] = 0
		}
		return volumes
	}
	for i := range volumes {
		volumes[i] = volumes[i] / maxVolume
	}
	return volumes
}

func rmsPCM(pcm []byte, channels int, startFrame int, endFrame int) float64 {
	if startFrame >= endFrame {
		return 0
	}
	sum := 0.0
	count := 0
	for frame := startFrame; frame < endFrame; frame++ {
		for ch := 0; ch < channels; ch++ {
			idx := (frame*channels + ch) * 2
			if idx+2 > len(pcm) {
				return finalizeRMS(sum, count)
			}
			sample := int16(binary.LittleEndian.Uint16(pcm[idx : idx+2]))
			value := float64(sample)
			sum += value * value
			count++
		}
	}
	return finalizeRMS(sum, count)
}

func finalizeRMS(sum float64, count int) float64 {
	if count == 0 {
		return 0
	}
	return math.Sqrt(sum / float64(count))
}

func (h *Handler) registerSession(sess *session) {
	h.mu.Lock()
	h.sessions[sess.clientUID] = sess
	h.mu.Unlock()
	h.group.RegisterClient(sess.clientUID)
}

func (h *Handler) unregisterSession(clientUID string) {
	h.mu.Lock()
	delete(h.sessions, clientUID)
	h.mu.Unlock()
	affected := h.group.RemoveClient(clientUID)
	h.broadcastGroupUpdate(affected)
}

func (h *Handler) sendGroupUpdate(clientUID string) {
	h.mu.Lock()
	sess := h.sessions[clientUID]
	h.mu.Unlock()
	if sess == nil {
		return
	}
	members := h.group.GetGroupMembers(clientUID)
	isOwner := h.group.IsOwner(clientUID)
	sess.sendJSON(map[string]any{"type": "group-update", "members": members, "is_owner": isOwner})
}

func (h *Handler) broadcastGroupUpdate(memberIDs []string) {
	for _, memberID := range memberIDs {
		h.sendGroupUpdate(memberID)
	}
}

func float64ToFloat32(samples []float64) []float32 {
	if len(samples) == 0 {
		return nil
	}
	out := make([]float32, len(samples))
	for i, sample := range samples {
		out[i] = float32(sample)
	}
	return out
}

func maxInt(a int, b int) int {
	if a > b {
		return a
	}
	return b
}
