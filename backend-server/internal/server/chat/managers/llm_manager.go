package managers

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"backend-server/internal/config"
	"backend-server/internal/domain/llm"
	llm_common "backend-server/internal/domain/llm/common"
	llm_memory "backend-server/internal/domain/llm/memory"
	domainmcp "backend-server/internal/domain/mcp"
	music "backend-server/internal/domain/music"
	log "backend-server/internal/infrastructure/logger"
	"backend-server/internal/server/channels"
	chatmcp "backend-server/internal/server/chat/mcp"
	chatmetrics "backend-server/internal/server/chat/metrics"
	client "backend-server/internal/server/chat/session/state"
	chattransport "backend-server/internal/server/chat/transport"
	"backend-server/internal/server/observability"
	audiohelper "backend-server/pkg/audio"

	"github.com/abadojack/whatlanggo"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	mcp_go "github.com/mark3labs/mcp-go/mcp"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const (
	MaxMessageCount = 10

	McpReadResourcePageSize       = 100 * 1024
	McpReadResourceStreamDoneFlag = "[DONE]"

	llmErrorMessageDefault = "抱歉，我刚刚没能顺利完成这个请求，请稍后再试。"
)

type LLMResponseChannelItem struct {
	ctx          context.Context
	userMessage  *schema.Message
	responseChan chan llm_common.LLMResponseStruct
	onEndFunc    func(err error, args ...any)
}

type SessionLogger interface {
	LogSessionMessage(msg *schema.Message)
}

type LLMManager struct {
	clientState     *client.ClientState
	serverTransport *chattransport.ServerTransport
	ttsManager      *TTSManager
	stateMachine    *SessionStateMachine
	session         SessionLogger

	einoTools        []*schema.ToolInfo
	llmResponseQueue *channels.ManagedQueue[LLMResponseChannelItem]

	reassureToolDelay   time.Duration
	reassureToolFollow  time.Duration
	reassureToolMessage string

	// 标记当前是否有工具调用在执行，用于 TTS 自动恢复时判定是否应跳过 listen start
	toolInFlight atomic.Bool
	llmInFlight  atomic.Bool

	reassureMessages     map[string]string
	reassureLongwaitMsgs map[string]string
	lastDetectedLanguage atomic.Value
}

func NewLLMManager(clientState *client.ClientState, serverTransport *chattransport.ServerTransport, ttsManager *TTSManager, stateMachine *SessionStateMachine, session SessionLogger) *LLMManager {
	cfg := config.GetConfig()
	queueCfg := channels.QueueConfig{
		Name:       "session_llm_response",
		Capacity:   cfg.Channels.Session.LLMResultQueue,
		DropPolicy: channels.ParseDropPolicy(cfg.Channels.DropPolicy),
		Timeout:    time.Duration(cfg.Channels.Timeout) * time.Second,
	}

	return &LLMManager{
		clientState:          clientState,
		serverTransport:      serverTransport,
		ttsManager:           ttsManager,
		stateMachine:         stateMachine,
		session:              session,
		llmResponseQueue:     channels.NewManagedQueue[LLMResponseChannelItem](queueCfg),
		reassureToolDelay:    time.Duration(cfg.Chat.ReassureToolDelayMs) * time.Millisecond,
		reassureToolFollow:   time.Duration(cfg.Chat.ReassureToolFollowupMs) * time.Millisecond,
		reassureToolMessage:  strings.TrimSpace(cfg.Chat.ReassureToolMessage),
		reassureMessages:     cfg.Chat.ReassureToolMessages,
		reassureLongwaitMsgs: cfg.Chat.ReassureToolLongwaitMessages,
	}
}

func (l *LLMManager) Start(ctx context.Context) {
	l.processLLMResponseQueue(ctx)
}

func (l *LLMManager) processLLMResponseQueue(ctx context.Context) {
	for {
		item, err := l.llmResponseQueue.Dequeue(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, channels.ErrQueueClosed) {
				return
			}
			continue
		}

		log.Debugf("processLLMResponseQueue item: %+v", item)
		_, err = l.handleLLMResponse(item.ctx, item.userMessage, item.responseChan)
		if item.onEndFunc != nil {
			item.onEndFunc(err)
		}
	}
}

func (l *LLMManager) ClearLLMResponseQueue() {
	l.llmResponseQueue.Clear()
}

func (l *LLMManager) AddTextToTTSQueue(text string) error {
	log.Debugf("AddTextToTTSQueue text: %s", text)
	msg := &schema.Message{
		Role:    schema.User,
		Content: text,
	}
	llmResponseChan := make(chan llm_common.LLMResponseStruct, 10)
	llmResponseChan <- llm_common.LLMResponseStruct{
		IsStart: true,
		IsEnd:   true,
		Text:    text,
	}
	close(llmResponseChan)
	l.HandleLLMResponseChannelAsync(l.clientState.GetSessionCtx(), msg, llmResponseChan)

	return nil
}

func (l *LLMManager) HandleLLMResponseChannelAsync(ctx context.Context, userMessage *schema.Message, responseChan chan llm_common.LLMResponseStruct) error {
	item := LLMResponseChannelItem{
		ctx:          ctx,
		userMessage:  userMessage,
		responseChan: responseChan,
		onEndFunc:    nil,
	}
	if err := l.llmResponseQueue.Enqueue(ctx, item); err != nil {
		log.Warnf("llmResponseQueue enqueue failed: %v", err)
		return fmt.Errorf("llmResponseQueue enqueue failed: %w", err)
	}
	return nil
}

func (l *LLMManager) HandleLLMResponseChannelSync(ctx context.Context, userMessage *schema.Message, llmResponseChannel chan llm_common.LLMResponseStruct, einoTools []*schema.ToolInfo) (bool, error) {
	ok, err := l.handleLLMResponse(ctx, userMessage, llmResponseChannel)

	return ok, err
}

// HandleLLMResponse 处理LLM响应
func (l *LLMManager) handleLLMResponse(ctx context.Context, userMessage *schema.Message, llmResponseChannel chan llm_common.LLMResponseStruct) (bool, error) {
	log.Debugf("handleLLMResponse start")
	defer log.Debugf("handleLLMResponse end")
	select {
	case <-ctx.Done():
		log.Debugf("handleLLMResponse ctx done, return")
		return false, nil
	default:
	}

	state := l.clientState
	var toolCalls []schema.ToolCall
	var fullText bytes.Buffer
	var hasReceivedResponse bool
	ttsStreamActive := false
	sendTextToTTS := func(resp llm_common.LLMResponseStruct, sync bool) error {
		if err := l.ttsManager.handleTextResponse(ctx, resp, sync); err != nil {
			return err
		}
		if resp.IsEnd {
			ttsStreamActive = false
		} else {
			ttsStreamActive = true
		}
		return nil
	}

	//var hasTextResponse bool
	for {
		select {
		case <-ctx.Done():
			// 上下文已取消，优先处理取消逻辑
			log.Infof("%s 上下文已取消，停止处理LLM响应, context done, exit", state.DeviceID)
			if !hasReceivedResponse {
				fallbackText := "刚刚的任务被打断了，请再说一声我帮你继续处理。"
				bgCtx := context.Background()
				if err := l.ttsManager.handleTextResponse(bgCtx, llm_common.LLMResponseStruct{
					Text:    fallbackText,
					IsStart: true,
					IsEnd:   true,
				}, true); err != nil {
					log.Warnf("发送上下文取消提示失败: %v", err)
				}
				if userMessage != nil && userMessage.Role == schema.User {
					l.AddLlmMessage(bgCtx, userMessage)
				}
				l.AddLlmMessage(bgCtx, schema.AssistantMessage(fallbackText, nil))
			}
			return false, nil
		default:
			// 非阻塞检查，如果ctx没有Done，继续处理LLM响应
			select {
			case llmResponse, ok := <-llmResponseChannel:
				if !ok {
					// 通道已关闭，检查是否收到过响应
					if !hasReceivedResponse {
						// 没有收到任何响应，说明LLM调用失败，触发错误处理
						log.Warnf("LLM 响应通道已关闭，但没有收到任何响应，可能是LLM调用失败")
						llmErr := fmt.Errorf("LLM调用失败，未收到响应")
						fallbackText := normalizeLLMErrorMessage(llmErr)
						// 发送错误响应以触发TTS
						err := sendTextToTTS(llm_common.LLMResponseStruct{
							Text:    fallbackText,
							IsStart: true,
							IsEnd:   true,
						}, true)
						if err != nil {
							log.Errorf("发送LLM错误响应失败: %v", err)
						}
						// 记录错误消息到对话历史
						if userMessage != nil && userMessage.Role == schema.User {
							l.AddLlmMessage(ctx, userMessage)
						}
						l.AddLlmMessage(ctx, schema.AssistantMessage(fallbackText, nil))
					} else {
						log.Infof("LLM 响应通道已关闭，退出协程")
					}
					return true, nil
				}

				if !hasReceivedResponse && state != nil {
					state.MarkLlmFirstChunkTs()
				}
				hasReceivedResponse = true
				log.Debugf("LLM 响应: %+v", llmResponse)

				// 检测错误标记
				if strings.HasPrefix(llmResponse.Text, "__LLM_ERROR__:") {
					errorText := strings.TrimPrefix(llmResponse.Text, "__LLM_ERROR__:")
					errorText = strings.TrimSpace(errorText)
					log.Warnf("收到LLM错误响应: %s", errorText)

					// 规整错误信息
					llmErr := fmt.Errorf("%s", errorText)
					fallbackText := normalizeLLMErrorMessage(llmErr)

					// 发送错误响应到TTS
					err := sendTextToTTS(llm_common.LLMResponseStruct{
						Text:    fallbackText,
						IsStart: true,
						IsEnd:   true,
					}, true)
					if err != nil {
						log.Errorf("发送LLM错误响应失败: %v", err)
					}

					// 记录到对话历史
					if userMessage != nil && userMessage.Role == schema.User {
						l.AddLlmMessage(ctx, userMessage)
					}
					l.AddLlmMessage(ctx, schema.AssistantMessage(fallbackText, nil))

					return true, nil
				}

				if len(llmResponse.ToolCalls) > 0 {
					log.Debugf("获取到工具: %+v", llmResponse.ToolCalls)
					toolCalls = append(toolCalls, llmResponse.ToolCalls...)
				}

				if llmResponse.Text != "" {
					if state != nil {
						state.MarkLlmFirstTextTs()
					}
					l.updateDetectedLanguage(llmResponse.Text)
					if metrics := chatmetrics.GetConversationMetrics(ctx); metrics != nil {
						metrics.AddOutput(llmResponse.Text)
					}
					if err := sendTextToTTS(llmResponse, true); err != nil {
						return true, err
					}
					fullText.WriteString(llmResponse.Text)
				} else if llmResponse.IsEnd && ttsStreamActive {
					if l.stateMachine != nil {
						l.stateMachine.OnTTSStreamComplete(true)
					}
					ttsStreamActive = false
				}

				if llmResponse.IsEnd {
					//写到redis中
					if userMessage != nil {
						if userMessage.Role == schema.User {
							l.AddLlmMessage(ctx, userMessage)
						}
					}
					strFullText := fullText.String()

					// 检查是否为空响应（LLM调用失败的情况）
					if strFullText == "" && len(toolCalls) == 0 {
						log.Warnf("收到空的LLM响应，可能是LLM调用失败")
						llmErr := fmt.Errorf("LLM调用失败，返回空响应")
						fallbackText := normalizeLLMErrorMessage(llmErr)

						// 发送错误响应到TTS
						err := sendTextToTTS(llm_common.LLMResponseStruct{
							Text:    fallbackText,
							IsStart: true,
							IsEnd:   true,
						}, true)
						if err != nil {
							log.Errorf("发送LLM错误响应失败: %v", err)
						}

						// 记录错误消息到对话历史
						l.AddLlmMessage(ctx, schema.AssistantMessage(fallbackText, nil))

						return true, nil
					}

					if strFullText != "" || len(toolCalls) > 0 {
						l.AddLlmMessage(ctx, schema.AssistantMessage(strFullText, toolCalls))
					}
					if len(toolCalls) > 0 {
						/*
							if !hasTextResponse {
								//有工具调用 && 没有文本响应，发送"查询中", 异步tts
								l.ttsManager.handleTextResponse(ctx, llm_common.LLMResponseStruct{
									Text: "查询中, 请稍候",
								}, false)
							}*/

						invokeToolSuccess, err := l.handleToolCallResponse(ctx, toolCalls)
						if err != nil {
							log.Errorf("处理工具调用响应失败: %v", err)
							return true, fmt.Errorf("处理工具调用响应失败: %v", err)
						}
						if !invokeToolSuccess {
							//工具调用失败
							if metrics := chatmetrics.GetConversationMetrics(ctx); metrics != nil {
								metrics.AddOutput(llmResponse.Text)
							}
							if err := sendTextToTTS(llmResponse, false); err != nil {
								return true, err
							}
							fullText.WriteString(llmResponse.Text)
							//sendTtsStartEndFunc(false)
						}
					} else {
						//sendTtsStartEndFunc(false)
					}

					return ok, nil
				}
			case <-ctx.Done():
				// 上下文已取消，退出协程
				log.Infof("%s 上下文已取消，停止处理LLM响应, context done, exit", state.DeviceID)
				//sendTtsStartEndFunc(false)
				return false, nil
			}
		}
	}
}

// startToolReassurance 在工具执行时根据配置播放安抚 TTS，调用返回的函数用于提前停止。
func (l *LLMManager) startToolReassurance(ctx context.Context, toolName string) func() {
	if l.ttsManager == nil || l.reassureToolDelay <= 0 {
		return func() {}
	}

	cancelCtx, cancel := context.WithCancel(ctx)
	start := time.Now()
	lang := l.getDetectedLanguage()
	initMsg := l.pickReassureMessage(lang, false)
	if initMsg == "" {
		return func() {}
	}

	go func() {
		timer := time.NewTimer(l.reassureToolDelay)
		defer timer.Stop()

		select {
		case <-cancelCtx.Done():
			log.Debugf("工具安抚提示已抑制: tool=%s elapsed=%s threshold=%s", toolName, time.Since(start), l.reassureToolDelay)
			return
		case <-timer.C:
			msg := initMsg
			if time.Since(start) >= 10*time.Second {
				msg = l.pickReassureMessage(lang, true)
			}
			if err := l.ttsManager.handleTextResponse(ctx, llm_common.LLMResponseStruct{
				Text:    msg,
				IsStart: true,
				IsEnd:   true,
			}, false); err != nil {
				log.Debugf("工具安抚TTS播放失败: tool=%s err=%v", toolName, err)
			} else {
				log.Infof("工具执行安抚提示: tool=%s sent=1 delay=%s follow=disabled", toolName, l.reassureToolDelay)
			}
		}
	}()

	return cancel
}

func (l *LLMManager) pickReassureMessage(lang string, longwait bool) string {
	cfg := config.GetConfig().Chat
	if lang == "" {
		lang = "default"
	}

	if longwait {
		if val := strings.TrimSpace(l.reassureLongwaitMsgs[lang]); val != "" {
			return val
		}
		if val := strings.TrimSpace(l.reassureLongwaitMsgs[normalizeLang(lang)]); val != "" {
			return val
		}
		if val := strings.TrimSpace(cfg.ReassureToolLongwaitMessage); val != "" {
			return val
		}
		return "Taking a bit longer, please wait."
	}

	if val := strings.TrimSpace(l.reassureMessages[lang]); val != "" {
		return val
	}
	if val := strings.TrimSpace(l.reassureMessages[normalizeLang(lang)]); val != "" {
		return val
	}
	if val := strings.TrimSpace(cfg.ReassureToolMessage); val != "" {
		return val
	}
	return "Working on it, one moment."
}

func normalizeLang(lang string) string {
	lang = strings.TrimSpace(strings.ToLower(lang))
	if lang == "" {
		return ""
	}
	if strings.HasPrefix(lang, "zh") {
		return "zh-CN"
	}
	if strings.HasPrefix(lang, "en") {
		return "en"
	}
	return lang
}

func detectLanguageFromText(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}

	// Prefer statistical detection
	if lang := whatlanggo.DetectLang(text); lang >= 0 {
		short := strings.ToLower(whatlanggo.LangToStringShort(lang))
		switch short {
		case "zh", "cmn":
			return "zh-CN"
		case "en":
			return "en"
		default:
			if short != "" {
				return short
			}
		}
	}

	for _, r := range text {
		if unicode.Is(unicode.Han, r) {
			return "zh-CN"
		}
	}
	// Fallback: assume English if mostly ASCII letters
	asciiLetters := 0
	total := 0
	for _, r := range text {
		total++
		if r <= unicode.MaxASCII && (('A' <= r && r <= 'Z') || ('a' <= r && r <= 'z')) {
			asciiLetters++
		}
	}
	if total > 0 && float64(asciiLetters)/float64(total) > 0.6 {
		return "en"
	}
	return ""
}

func (l *LLMManager) updateDetectedLanguage(text string) {
	if lang := detectLanguageFromText(text); lang != "" {
		l.lastDetectedLanguage.Store(lang)
	}
}

func (l *LLMManager) getDetectedLanguage() string {
	if v := l.lastDetectedLanguage.Load(); v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// handleToolCallResponse 处理工具调用响应
func (l *LLMManager) handleToolCallResponse(ctx context.Context, tools []schema.ToolCall) (bool, error) {
	if len(tools) == 0 {
		return false, nil
	}

	if ctx.Err() != nil {
		log.Warnf("工具阶段开始前上下文已取消: session=%s err=%v", l.clientState.SessionID, ctx.Err())
		return false, ctx.Err()
	}

	tracer := observability.Tracer()
	metrics := chatmetrics.GetConversationMetrics(ctx)
	spanCtx, span := tracer.Start(ctx, "conversation.function_call", trace.WithSpanKind(trace.SpanKindClient))
	start := time.Now()
	executedTools := 0

	defer func() {
		duration := time.Since(start)
		span.SetAttributes(
			attribute.Int("tool.requested", len(tools)),
			attribute.Int("tool.executed", executedTools),
			attribute.Int64("tool.total_duration_ms", duration.Milliseconds()),
		)
		span.End()
	}()

	ctx = spanCtx

	state := l.clientState

	log.Infof("处理 %d 个工具调用", len(tools))

	var invokeToolSuccess bool

	var shouldStopLLMProcessing bool

	var wg sync.WaitGroup

	var toolResponsesAdded bool
	addMessageFunc := func(toolCall schema.ToolCall, result string) {
		toolResultMsg := &schema.Message{
			Role:       schema.Tool,
			ToolCallID: toolCall.ID,
			Content:    result,
		}

		l.AddLlmMessage(ctx, toolResultMsg)
		toolResponsesAdded = true
	}

	for _, toolCall := range tools {
		toolName := toolCall.Function.Name
		tool, ok := domainmcp.GetToolByName(state.DeviceID, toolName)
		if !ok {
			am := l.clientState.FCTools.GetAdapterManager()
			if adapter := am.GetAdapter(toolName); adapter != nil {
				tool = adapter
				ok = true
			} else {
				tool = nil
			}
		}

		if tool == nil {
			log.Errorf("未找到工具: %s", toolName)
			addMessageFunc(toolCall, fmt.Sprintf("未找到工具: %s", toolName))
			continue
		}
		log.Infof("进行工具调用请求: %s, 参数: %+v", toolName, toolCall.Function.Arguments)
		l.toolInFlight.Store(true)
		reassureStop := l.startToolReassurance(ctx, toolName)
		toolStart := time.Now()
		fcResult, err := tool.InvokableRun(ctx, toolCall.Function.Arguments)
		reassureStop()
		l.toolInFlight.Store(false)
		if ctx.Err() != nil {
			log.Warnf("工具调用返回时上下文已取消: tool=%s session=%s err=%v", toolName, state.SessionID, ctx.Err())
		}
		if err != nil {
			log.Errorf("工具调用失败: %v (tool=%s session=%s ctx_err=%v)", err, toolName, state.SessionID, ctx.Err())
			addMessageFunc(toolCall, fmt.Sprintf("工具 %s 调用失败: %v", toolName, err))
			span.RecordError(err)
			if metrics != nil {
				metrics.AddError(err)
			}
			continue
		}
		cost := time.Since(toolStart)
		invokeToolSuccess = true
		executedTools++
		span.AddEvent("tool.invocation", trace.WithAttributes(
			attribute.String("tool.name", toolName),
			attribute.Int64("tool.duration_ms", cost.Milliseconds()),
		))
		if metrics != nil {
			metrics.AddToolDuration(cost, 1)
		}
		if len(fcResult) > 2048 {
			log.Infof("工具调用结果 len: %d, 耗时: %dms", len(fcResult), cost.Milliseconds())
		} else {
			log.Infof("工具调用结果 %s, 耗时: %dms", fcResult, cost.Milliseconds())
		}

		var result string = fcResult
		var contentList []mcp_go.Content
		var parsedToolResult *mcp_go.CallToolResult
		if mcpResp, ok := l.handleLocalToolResult(fcResult); ok {
			/*if mcpResp.IsTerminal() {
				log.Infof("工具调用结果: %s, 终止: %t", fcResult, mcpResp.IsTerminal())
				return invokeToolSuccess, nil
			}*/
			contentList = mcpResp.GetContent()
		} else if toolCallResult, ok := l.handleToolResult(fcResult); ok {
			if toolCallResult.IsError {
				log.Errorf("工具调用失败: %s, 错误: %v", fcResult, toolCallResult.IsError)
			}
			contentList = toolCallResult.Content
			parsedToolResult = &toolCallResult
		}
		if len(contentList) > 0 {
			var mcpContent string
			//如果有audio数据, 则进行播放
			for _, content := range contentList {
				if audioContent, ok := content.(mcp_go.AudioContent); ok {
					log.Debugf("调用工具 %s 返回音频资源长度: %d", toolName, len(audioContent.Data))

					mcpContent = "执行成功"
					//播放音频资源,此时mcpContent是
					err := l.handleAudioContent(ctx, mcpContent, audioContent, &wg)
					if err != nil {
						log.Errorf("mcp播放音频资源失败: %v", err)
						mcpContent = "执行失败"
					}
					shouldStopLLMProcessing = true
					break
				} else if resourceLink, ok := content.(mcp_go.ResourceLink); ok {
					log.Debugf("调用工具 %s 返回资源链接: %+v", toolName, resourceLink)
					mcpContent = "执行成功"
					err := l.handleResourceLink(ctx, resourceLink, tool, &wg)
					if err != nil {
						log.Errorf("mcp播放资源链接失败: %v", err)
						mcpContent = "执行失败"
					}

					shouldStopLLMProcessing = true
					break
				} else if textContent, ok := content.(mcp_go.TextContent); ok {
					log.Debugf("调用工具 %s 返回文本资源长度: %s", toolName, textContent.Text)
					mcpContent += textContent.Text
				}
			}
			if mcpContent != "" {
				result = mcpContent
			}
		}
		if !shouldStopLLMProcessing && parsedToolResult != nil {
			if stop, stopMessage := l.shouldStopFromStructured(parsedToolResult.StructuredContent); stop {
				shouldStopLLMProcessing = true
				if stopMessage != "" {
					result = stopMessage
				}
			}
		}

		if l.ttsManager != nil && shouldSendToolTextToDevice(result) {
			if err := l.ttsManager.handleTextResponse(ctx, llm_common.LLMResponseStruct{
				Text:    result,
				IsStart: true,
				IsEnd:   true,
			}, false); err != nil {
				log.Warnf("发送工具文本到设备失败: %v", err)
			}
		}
		addMessageFunc(toolCall, result)
	}

	wg.Wait()

	// 工具阶段只要有反馈且未被要求停止，就继续LLM处理
	if toolResponsesAdded && !shouldStopLLMProcessing {
		l.DoLLmRequest(ctx, nil, l.einoTools, true)
	}

	return invokeToolSuccess, nil
}

func shouldSendToolTextToDevice(text string) bool {
	if strings.TrimSpace(text) == "" {
		return false
	}
	return strings.Contains(text, "![") && strings.Contains(text, "](") && strings.Contains(text, ")")
}

// ToolInFlight 返回当前是否有工具正在执行
func (l *LLMManager) ToolInFlight() bool {
	return l.toolInFlight.Load()
}

// LLMInFlight 返回当前是否有 LLM 响应处理在进行
func (l *LLMManager) LLMInFlight() bool {
	return l.llmInFlight.Load()
}

func (l *LLMManager) handleResourceLink(ctx context.Context, resourceLink mcp_go.ResourceLink, toolCall tool.InvokableTool, wg *sync.WaitGroup) error {
	wg.Add(1)
	//从resourceLink中获取资源
	client := toolCall.(*domainmcp.McpTool).GetClient()

	var pipeReader *io.PipeReader
	var pipeWriter *io.PipeWriter
	pipeReader, pipeWriter = io.Pipe()

	audioFormat := audiohelper.GetAudioFormatByMimeType(resourceLink.MIMEType)

	streamChan := make(chan []byte, 0) // 增加缓冲区大小
	go func() error {
		defer func() {
			close(streamChan)
		}()

		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case audioData, ok := <-streamChan:
					if !ok {
						pipeWriter.Close()
						return
					}
					if _, err := pipeWriter.Write(audioData); err != nil {
						log.Errorf("写入pipe失败: %v", err)
						return
					}
				}
			}
		}()

		start := 0
		page := McpReadResourcePageSize
		totalRead := 0
		pageCount := 0

		log.Infof("开始读取资源: %s, 分页大小: %d", resourceLink.URI, page)

		for {
			select {
			case <-ctx.Done():
				log.Debugf("资源读取被取消")
				return nil
			default:
				pageCount++
				log.Debugf("读取第 %d 页资源，起始位置: %d, 结束位置: %d", pageCount, start, start+page)

				// 创建带超时的上下文
				readCtx, cancel := context.WithTimeout(ctx, 30*time.Second)

				// 读取资源
				resourceResult, err := client.ReadResource(readCtx, mcp_go.ReadResourceRequest{
					Params: mcp_go.ReadResourceParams{
						URI:       resourceLink.URI,
						Arguments: map[string]any{"url": resourceLink.Description, "start": start, "end": start + page},
					},
				})
				cancel()

				if err != nil {
					log.Errorf("读取资源失败 (第 %d 页), resourceUri: %s, resourceResult: %+v, err: %v", pageCount, resourceLink.Description, resourceResult, err)

					// 如果是超时错误，尝试重试
					if strings.Contains(err.Error(), "timeout") || strings.Contains(err.Error(), "deadline") {
						log.Warnf("资源读取超时，尝试重试...")
						time.Sleep(1 * time.Second)
						continue
					}

					return fmt.Errorf("读取资源失败: %v", err)
				}

				if len(resourceResult.Contents) == 0 {
					log.Infof("资源读取完成，总共读取 %d 字节，共 %d 页", totalRead, pageCount-1)
					return nil
				}

				hasData := false
				for _, content := range resourceResult.Contents {
					if audioContent, ok := content.(mcp_go.BlobResourceContents); ok {
						if len(audioContent.Blob) == 0 {
							log.Debugf("音频数据为空，跳过")
							continue
						}
						log.Debugf("第 %d 页 resourceResult len: %d", pageCount, len(audioContent.Blob))
						rawAudioData, err := base64.StdEncoding.DecodeString(audioContent.Blob)
						if err != nil {
							log.Errorf("解码音频数据失败: %v", err)
							return fmt.Errorf("解码音频数据失败: %v", err)
						}

						if string(rawAudioData) == McpReadResourceStreamDoneFlag {
							log.Debugf("资源读取完成")
							return nil
						}

						select {
						case <-ctx.Done():
							log.Debugf("资源读取被取消")
							return nil
						case streamChan <- rawAudioData:
							totalRead += len(rawAudioData)
							hasData = true
							log.Debugf("成功发送第 %d 页数据，长度: %d, 累计: %d", pageCount, len(rawAudioData), totalRead)
						}

						if len(rawAudioData) < page {
							log.Debugf("资源读取完成")
							return nil
						}
					}
				}

				// 如果这一页没有数据，说明已经读取完毕
				if !hasData {
					log.Infof("资源读取完成，总共读取 %d 字节，共 %d 页", totalRead, pageCount)
					return nil
				}

				start += page
			}
		}
	}()

	// 使用music_player播放音乐
	audioChan, err := music.PlayMusicFromPipe(ctx, pipeReader, l.clientState.OutputAudioFormat.SampleRate, l.clientState.OutputAudioFormat.FrameDuration, audioFormat)
	if err != nil {
		log.Errorf("播放音乐失败: %v", err)
		return fmt.Errorf("播放音乐失败: %v", err)
	}

	playText := fmt.Sprintf("正在播放音乐: %s", resourceLink.Name)
	sentSentenceStart := false
	sendSentenceStart := func() error {
		if sentSentenceStart {
			return nil
		}
		if err := l.serverTransport.SendSentenceStart(playText); err != nil {
			return err
		}
		sentSentenceStart = true
		return nil
	}

	ctx = withTTSFirstFrameHook(ctx, func() {
		if err := sendSentenceStart(); err != nil {
			log.Errorf("发送音乐提示文本失败: %s, %v", playText, err)
		}
	})
	if l.stateMachine != nil {
		l.stateMachine.OnTTSStreamSending()
	}

	go func() {
		defer func() {
			if sentSentenceStart {
				if err := l.serverTransport.SendSentenceEnd(playText); err != nil {
					log.Errorf("发送音乐提示文本失败: %s, %v", playText, err)
				}
			}
			if l.stateMachine != nil {
				l.stateMachine.OnTTSStreamComplete(false)
			}
			log.Infof("音乐播放完成: %s", resourceLink.Name)
			wg.Done()
		}()

		if err := l.ttsManager.SendTTSAudio(ctx, audioChan, true); err != nil {
			log.Errorf("发送音乐音频失败: %v", err)
		}
	}()

	return nil
}

func (l *LLMManager) handleAudioContent(ctx context.Context, realMusicName string, audioContent mcp_go.AudioContent, wg *sync.WaitGroup) error {
	wg.Add(1)
	rawAudioData, err := base64.StdEncoding.DecodeString(audioContent.Data)
	if err != nil {
		log.Errorf("解码音频数据失败: %v", err)
		return fmt.Errorf("解码音频数据失败: %v", err)
	}
	audioFormat := audiohelper.GetAudioFormatByMimeType(audioContent.MIMEType)
	// 使用music_player播放音乐
	audioChan, err := music.PlayMusicFromAudioData(ctx, rawAudioData, l.clientState.OutputAudioFormat.SampleRate, l.clientState.OutputAudioFormat.FrameDuration, audioFormat)
	if err != nil {
		log.Errorf("播放音乐失败: %v", err)
		return fmt.Errorf("播放音乐失败: %v", err)
	}

	playText := fmt.Sprintf("正在播放音乐: %s", realMusicName)
	sentSentenceStart := false
	sendSentenceStart := func() error {
		if sentSentenceStart {
			return nil
		}
		if err := l.serverTransport.SendSentenceStart(playText); err != nil {
			return err
		}
		sentSentenceStart = true
		return nil
	}

	ctx = withTTSFirstFrameHook(ctx, func() {
		if err := sendSentenceStart(); err != nil {
			log.Errorf("发送音乐提示文本失败: %s, %v", playText, err)
		}
	})
	if l.stateMachine != nil {
		l.stateMachine.OnTTSStreamSending()
	}

	go func() {
		defer func() {
			if sentSentenceStart {
				if err := l.serverTransport.SendSentenceEnd(playText); err != nil {
					log.Errorf("发送音乐提示文本失败: %s, %v", playText, err)
				}
			}
			if l.stateMachine != nil {
				l.stateMachine.OnTTSStreamComplete(false)
			}
			log.Infof("音乐播放完成: %s", realMusicName)
			wg.Done()
		}()
		if err := l.ttsManager.SendTTSAudio(ctx, audioChan, true); err != nil {
			log.Errorf("发送音乐音频失败: %v", err)
		}
	}()

	return nil
}

func (l *LLMManager) handleLocalToolResult(toolResult string) (chatmcp.MCPResponse, bool) {
	// 首先尝试解析新的结构化响应
	var response chatmcp.MCPResponse
	var err error
	if response, err = chatmcp.ParseMCPResponse(toolResult); err != nil {
		return nil, false
	}
	return response, true
}

func (l *LLMManager) handleToolResult(toolResultStr string) (mcp_go.CallToolResult, bool) {
	var toolResult mcp_go.CallToolResult
	if err := json.Unmarshal([]byte(toolResultStr), &toolResult); err != nil {
		log.Warnf("解析工具结果失败，降级为纯文本: %v", err)
		toolResult = mcp_go.CallToolResult{
			Content: []mcp_go.Content{
				mcp_go.TextContent{Type: "text", Text: toolResultStr},
			},
			StructuredContent: map[string]interface{}{
				"status":          "text_fallback",
				"raw_tool_result": toolResultStr,
			},
		}
		return toolResult, true
	}

	return toolResult, true
}

func (l *LLMManager) shouldStopFromStructured(structured any) (bool, string) {
	if structured == nil {
		return false, ""
	}
	var structuredMap map[string]interface{}
	switch v := structured.(type) {
	case map[string]interface{}:
		structuredMap = v
	default:
		data, err := json.Marshal(structured)
		if err != nil {
			return false, ""
		}
		if err := json.Unmarshal(data, &structuredMap); err != nil {
			return false, ""
		}
	}
	if structuredMap == nil {
		return false, ""
	}
	if stop, ok := structuredMap["should_stop_llm"].(bool); ok && stop {
		return true, firstNonEmptyString(structuredMap["response"], structuredMap["message"])
	}
	if resultMap, ok := structuredMap["result"].(map[string]interface{}); ok {
		if _, has := resultMap["now_playing"]; has {
			return true, firstNonEmptyString(structuredMap["response"], structuredMap["message"])
		}
	}
	return false, ""
}

func firstNonEmptyString(values ...interface{}) string {
	for _, v := range values {
		switch val := v.(type) {
		case string:
			if strings.TrimSpace(val) != "" {
				return val
			}
		}
	}
	return ""
}

func (l *LLMManager) DoLLmRequest(ctx context.Context, userMessage *schema.Message, einoTools []*schema.ToolInfo, isSync bool) (err error) {
	log.Debugf("发送带工具的 LLM 请求, seesionID: %s, requestEinoMessages: %+v", l.clientState.SessionID, userMessage)
	clientState := l.clientState

	tracer := observability.Tracer()
	metrics := chatmetrics.GetConversationMetrics(ctx)
	llmCtx, span := tracer.Start(
		ctx,
		"conversation.llm",
		trace.WithSpanKind(trace.SpanKindClient),
	)
	start := time.Now()

	defer func() {
		duration := time.Since(start)
		if metrics != nil {
			metrics.AddLLMDuration(duration)
		}
		span.SetAttributes(
			attribute.Int64("llm.duration_ms", duration.Milliseconds()),
			attribute.String("llm.session_id", l.clientState.SessionID),
		)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			if metrics != nil {
				metrics.AddError(err)
			}
		} else {
			span.SetStatus(codes.Ok, "success")
		}
		span.End()
	}()

	ctx = llmCtx

	einoToolNames := make([]string, 0)
	newTools := make([]*schema.ToolInfo, 0)
	newTools = append(newTools, clientState.FCTools.GetEinoTools()...)
	for _, einoTool := range newTools {
		einoToolNames = append(einoToolNames, einoTool.Name)
	}
	log.Debugf("einoTool-1: %s", strings.Join(einoToolNames, ","))

	newTools = append(newTools, einoTools...)
	einoToolNames = make([]string, 0)
	for _, einoTool := range newTools {
		einoToolNames = append(einoToolNames, einoTool.Name)
	}
	log.Debugf("einoTool-2: %s", strings.Join(einoToolNames, ","))

	l.einoTools = newTools
	span.SetAttributes(attribute.Int("llm.tool_count", len(newTools)))
	//组装历史消息和当前用户的消息
	requestMessages := l.GetMessages(ctx, userMessage, MaxMessageCount)
	clientState.MarkLlmStartTs()
	clientState.SetStatus(client.ClientStatusLLMStart)
	l.llmInFlight.Store(true)
	ctx = llm.WithFirstTokenTraceHook(ctx, func() {
		if clientState != nil {
			clientState.MarkLlmFirstTokenTs()
		}
	})
	responseSentences, err := llm.HandleLLMWithContextAndTools(
		ctx,
		clientState.LLMProvider,
		requestMessages,
		l.einoTools,
		l.clientState.SessionID,
	)
	if err != nil {
		log.Errorf("发送带工具的 LLM 请求失败, seesionID: %s, error: %v, ctx_err=%v", l.clientState.SessionID, err, ctx.Err())
		handleErr := l.respondWithLLMError(ctx, userMessage, err, isSync)
		if handleErr != nil {
			return fmt.Errorf("处理 LLM 错误响应失败: %w", handleErr)
		}
		return nil
	}

	log.Debugf("DoLLmRequest goroutine开始 - SessionID: %s, context状态: %v", l.clientState.SessionID, ctx.Err())

	if isSync {
		_, err = l.HandleLLMResponseChannelSync(ctx, userMessage, responseSentences, l.einoTools)
		if err != nil {
			log.Errorf("处理 LLM 响应失败, seesionID: %s, error: %v", l.clientState.SessionID, err)
			return err
		}
	} else {
		err = l.HandleLLMResponseChannelAsync(ctx, userMessage, responseSentences)
		if err != nil {
			log.Errorf("处理 LLM 响应失败, seesionID: %s, error: %v", l.clientState.SessionID, err)
		}
	}

	log.Debugf("DoLLmRequest 结束 - SessionID: %s", l.clientState.SessionID)
	l.llmInFlight.Store(false)

	return nil
}

func (l *LLMManager) respondWithLLMError(ctx context.Context, userMessage *schema.Message, llmErr error, isSync bool) error {
	fallbackText := normalizeLLMErrorMessage(llmErr)
	log.Warnf("LLM 请求失败，返回兜底响应: %s", fallbackText)

	fallbackChan := make(chan llm_common.LLMResponseStruct, 1)
	fallbackChan <- llm_common.LLMResponseStruct{
		Text:    fallbackText,
		IsStart: true,
		IsEnd:   true,
	}
	close(fallbackChan)

	if isSync {
		_, err := l.HandleLLMResponseChannelSync(ctx, userMessage, fallbackChan, l.einoTools)
		return err
	}
	return l.HandleLLMResponseChannelAsync(ctx, userMessage, fallbackChan)
}

func normalizeLLMErrorMessage(err error) string {
	if err == nil {
		return llmErrorMessageDefault
	}

	if errors.Is(err, context.Canceled) {
		return "请求被取消了，我们可以重新开始一次。"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "抱歉，这次思考超时了，请再问我一次。"
	}

	message := sanitizeErrorText(err.Error(), 120)
	if message == "" {
		return llmErrorMessageDefault
	}

	return fmt.Sprintf("抱歉，我处理请求时遇到问题：%s", message)
}

func sanitizeErrorText(text string, limit int) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}

	text = strings.ReplaceAll(text, "\r", " ")
	text = strings.ReplaceAll(text, "\n", " ")
	text = strings.Join(strings.Fields(text), " ")

	if limit <= 0 {
		return text
	}

	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "..."
}

func (l *LLMManager) AddLlmMessage(ctx context.Context, msg *schema.Message) error {
	if msg == nil {
		log.Warnf("尝试添加 nil 消息到 LLM 对话历史")
		return fmt.Errorf("消息不能为 nil")
	}

	// 添加到对话历史
	l.clientState.AddMessage(msg)

	if l.session != nil && msg.Role != schema.User {
		l.session.LogSessionMessage(msg)
	}

	if err := llm_memory.Get(l.clientState.GetDeviceMemoryConfig()).AddMessage(ctx, l.clientState.DeviceID, *msg); err != nil {
		log.Warnf("添加消息到全局记忆失败: %v", err)
	}

	return nil
}

func (l *LLMManager) GetMessages(ctx context.Context, userMessage *schema.Message, count int) []*schema.Message {
	//从dialogue中获取
	messageList := l.clientState.GetMessages(count)

	retMessage := make([]*schema.Message, 0)

	// 构建系统提示词，如果启用长记忆则包含用户画像
	systemPrompt := l.clientState.SystemPrompt

	if strings.Contains(systemPrompt, "{{current_datetime}}") {
		now := time.Now()
		if loc, err := time.LoadLocation("Asia/Shanghai"); err == nil {
			now = now.In(loc)
		}
		systemPrompt = strings.ReplaceAll(systemPrompt, "{{current_datetime}}", now.Format("2006-01-02 15:04:05"))
	}

	// 使用全局记忆配置
	memory := llm_memory.Get(l.clientState.GetDeviceMemoryConfig())
	userProfile, err := memory.GetUserProfile(ctx, l.clientState.DeviceID)
	if err != nil {
		log.Warnf("获取用户画像失败: %v", err)
	} else if userProfile != "" {
		log.Debugf("为设备 %s 获取到用户画像，长度: %d 字符", l.clientState.DeviceID, len(userProfile))
		// 将用户画像融入系统提示词
		systemPrompt = fmt.Sprintf("%s\n\n# 用户画像\n%s", systemPrompt, userProfile)
	}

	retMessage = append(retMessage, &schema.Message{
		Role:    schema.System,
		Content: systemPrompt,
	})
	retMessage = append(retMessage, messageList...)
	if userMessage != nil {
		retMessage = append(retMessage, userMessage)
	}
	return retMessage
}
