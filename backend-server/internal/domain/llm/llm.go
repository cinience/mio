package llm

import (
	"context"
	"strings"
	"time"

	"backend-server/internal/domain/llm/common"
	log "backend-server/internal/infrastructure/logger"
	streamseg "backend-server/internal/shared/segmentation/streaming"

	"github.com/cloudwego/eino/schema"
)

type firstTokenTraceKey struct{}

// WithFirstTokenTraceHook attaches a hook that runs when the first token arrives.
func WithFirstTokenTraceHook(ctx context.Context, hook func()) context.Context {
	if ctx == nil || hook == nil {
		return ctx
	}
	return context.WithValue(ctx, firstTokenTraceKey{}, hook)
}

func getFirstTokenTraceHook(ctx context.Context) func() {
	if ctx == nil {
		return nil
	}
	if hook, ok := ctx.Value(firstTokenTraceKey{}).(func()); ok {
		return hook
	}
	return nil
}

// HandleLLMWithContextAndTools 使用上下文控制来处理LLM响应（兼容带工具和不带工具）
func HandleLLMWithContextAndTools(ctx context.Context, llmProvider LLMProvider, dialogue []*schema.Message, tools []*schema.ToolInfo, sessionID string) (chan common.LLMResponseStruct, error) {
	var llmResponse interface{} = llmProvider.ResponseWithContext(ctx, sessionID, dialogue, tools)

	sentenceChannel := make(chan common.LLMResponseStruct, 8)
	startTime := time.Now()
	streamer := streamseg.NewSentencizerStreamer(2, 100, 30)
	firstTokenHook := getFirstTokenTraceHook(ctx)

	go func() {
		defer close(sentenceChannel)

		msgChan, ok := llmResponse.(chan *schema.Message)
		if !ok {
			log.Errorf("llmResponse 断言为 chan *schema.Message 失败")
			return
		}

		didEmitFirstSentence := false
		didLogFirstToken := false
		isFirst := true

		send := func(resp common.LLMResponseStruct) bool {
			select {
			case <-ctx.Done():
				log.Infof("上下文已取消，停止LLM响应处理: %v, context done, exit", ctx.Err())
				return false
			case sentenceChannel <- resp:
				return true
			}
		}

		emitSentence := func(sentence string) bool {
			if sentence == "" {
				return true
			}
			if !didEmitFirstSentence {
				didEmitFirstSentence = true
				log.Infof("耗时统计: llm工具首句: %s", time.Since(startTime))
			}
			log.Infof("处理完整句子: %s", sentence)
			ok := send(common.LLMResponseStruct{
				Text:    sentence,
				IsStart: isFirst,
				IsEnd:   false,
			})
			if ok && isFirst {
				isFirst = false
			}
			return ok
		}

		for {
			select {
			case <-ctx.Done():
				log.Infof("上下文已取消，停止LLM响应处理: %v, session=%s, context done, exit", ctx.Err(), sessionID)
				return
			case message, ok := <-msgChan:
				if !ok {
					flushStart := time.Now()
					finalSentences, remainder := streamer.Flush()
					log.Infof("分句耗时: %s, flush_sentences=%d", time.Since(flushStart), len(finalSentences))
					for _, sentence := range finalSentences {
						if !emitSentence(sentence) {
							return
						}
					}
					if !send(common.LLMResponseStruct{
						Text:    remainder,
						IsStart: isFirst,
						IsEnd:   true,
					}) {
						return
					}
					return
				}
				if message == nil {
					break
				}
				if !didLogFirstToken {
					didLogFirstToken = true
					if firstTokenHook != nil {
						firstTokenHook()
					} else {
						log.Infof(
							"首帧链路耗时片段: llm_start->llm_first_token=%dms (session=%s)",
							time.Since(startTime).Milliseconds(),
							sessionID,
						)
					}
				}
				// 避免热路径 JSON 序列化开销，仅打印关键信息
				log.Debugf("收到message: content_len=%d, toolCalls=%d", len(message.Content), len(message.ToolCalls))

				// 检测错误标记，直接发送错误响应
				if strings.HasPrefix(message.Content, "__LLM_ERROR__:") {
					errorText := strings.TrimPrefix(message.Content, "__LLM_ERROR__:")
					errorText = strings.TrimSpace(errorText)
					log.Warnf("检测到LLM错误: %s", errorText)
					// 发送标记为错误的响应，让 llm_manager 处理
					if !send(common.LLMResponseStruct{
						Text:    "__LLM_ERROR__:" + errorText,
						IsStart: true,
						IsEnd:   true,
					}) {
						return
					}
					return
				}

				if message.Content != "" {
					start := time.Now()
					sentences := streamer.Append(message.Content)
					if len(sentences) > 0 {
						log.Infof("分句耗时: %s, chunk_len=%d, sentences=%d", time.Since(start), len(message.Content), len(sentences))
					} else {
						log.Infof("分句耗时: %s, chunk_len=%d, sentences=%d", time.Since(start), len(message.Content), 0)
					}
					for _, sentence := range sentences {
						if !emitSentence(sentence) {
							return
						}
					}
				}
				// 工具调用响应（假设 ToolCalls 字段）
				if len(message.ToolCalls) > 0 {
					log.Infof("处理工具调用: %+v", message.ToolCalls)
					if !send(common.LLMResponseStruct{
						ToolCalls: message.ToolCalls,
						IsStart:   isFirst,
						IsEnd:     false,
					}) {
						return
					}
				}
			}
		}
	}()
	return sentenceChannel, nil
}

// ConvertMCPToolsToEinoTools 将MCP工具转换为Eino ToolInfo格式
func ConvertMCPToolsToEinoTools(ctx context.Context, mcpTools map[string]interface{}) ([]*schema.ToolInfo, error) {
	einoTools := make([]*schema.ToolInfo, 0, len(mcpTools))

	for toolName, mcpTool := range mcpTools {
		// 尝试获取工具信息
		if invokableTool, ok := mcpTool.(interface {
			Info(context.Context) (*schema.ToolInfo, error)
		}); ok {
			toolInfo, err := invokableTool.Info(ctx)
			if err != nil {
				log.Errorf("获取工具 %s 信息失败: %v", toolName, err)
				continue
			}
			einoTools = append(einoTools, toolInfo)
		} else {
			log.Warnf("工具 %s 不支持Info接口，跳过转换", toolName)
		}
	}

	log.Infof("成功转换了 %d 个MCP工具为Eino工具", len(einoTools))
	return einoTools, nil
}
