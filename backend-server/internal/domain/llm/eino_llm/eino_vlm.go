package eino_llm

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	log "backend-server/internal/infrastructure/logger"

	"github.com/cloudwego/eino/schema"
)

func (p *EinoLLMProvider) ResponseWithVLM(ctx context.Context, file []byte, text string, mimeType string) (string, error) {
	log.Infof("[Eino-LLM] 开始进行VLLM请求 - MIMEType: %s, file length: %d", mimeType, len(file))
	start := time.Now()
	defer func() {
		log.Infof("[Eino-LLM] VLLM请求结束，耗时: %s", time.Since(start))
	}()

	// 将图片文件以base64编码，组装为data url
	base64Str := base64.StdEncoding.EncodeToString(file)
	dataURL := fmt.Sprintf("data:%s;base64,%s", mimeType, base64Str)

	msg := &schema.Message{
		Role: schema.User,
		UserInputMultiContent: []schema.MessageInputPart{
			{
				Type: schema.ChatMessagePartTypeText,
				Text: text,
			},
			{
				Type: schema.ChatMessagePartTypeImageURL,
				Image: &schema.MessageInputImage{
					MessagePartCommon: schema.MessagePartCommon{
						URL:      &dataURL,
						MIMEType: mimeType,
					},
				},
			},
		},
	}

	dialogue := []*schema.Message{
		{
			Role:    schema.System,
			Content: "你是一个专业的图片识别专家，请根据图片内容使用中文回答用户的问题。",
		},
		msg,
	}
	responseChan := p.ResponseWithContext(ctx, "", dialogue, []*schema.ToolInfo{})
	if responseChan == nil {
		log.Errorf("[Eino-VLLM] 调用视觉api请求处理失败 - responseChan为nil")
		return "", fmt.Errorf("调用视觉api请求处理失败 - responseChan为nil")
	}

	var result bytes.Buffer
	for {
		select {
		case <-ctx.Done():
			log.Errorf("[Eino-VLLM]  context done")
			return "", nil
		case response, ok := <-responseChan:
			if !ok {
				if response != nil && response.Content != "" {
					result.WriteString(response.Content)
				}
				responseText := result.String()
				return responseText, nil
			}
			if response == nil {
				continue
			}
			// 流式失败时会以特殊前缀下发错误，VLM 场景不回退，直接返回空结果。
			if strings.HasPrefix(response.Content, "__LLM_ERROR__") {
				log.Errorf("[Eino-VLLM] 流式响应错误: %s", response.Content)
				return "", nil
			}
			result.WriteString(response.Content)
		}
	}
}
