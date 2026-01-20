package utils

import (
	"backend-server/internal/domain/llm"
	log "backend-server/internal/infrastructure/logger"
	"context"
	"fmt"
	"net/http"
)

var getLLMProvider = llm.GetLLMProvider

func HandleVLM(deviceId string, file []byte, text string, overrideProvider string, overrideConfig map[string]interface{}) (string, error) {
	// 默认使用配置文件中的 provider，当 overrideProvider/config 提供时优先使用
	provider := overrideProvider
	vllmConfig := make(map[string]interface{})

	if overrideConfig != nil {
		for k, v := range overrideConfig {
			vllmConfig[k] = v
		}
	}

	if provider == "" {
		if typ, ok := vllmConfig["type"].(string); ok && typ != "" {
			provider = typ
		}
	}

	if provider == "" {
		return "", fmt.Errorf("未找到可用的VLLM provider")
	}

	if len(vllmConfig) == 0 {
		return "", fmt.Errorf("未找到VLLM配置")
	}

	sample := file
	if len(sample) > 512 {
		sample = sample[:512]
	}
	mimeType := http.DetectContentType(sample)

	llmProvider, err := getLLMProvider(provider, vllmConfig)
	if err != nil {
		return "", err
	}
	responseText, err := llmProvider.ResponseWithVLM(context.Background(), file, text, mimeType)
	if err != nil {
		log.Errorf("图片识别失败: %v", err)
		return "", err
	}

	return responseText, nil
}

func VisvionAuth(token string) error {
	return nil
}
