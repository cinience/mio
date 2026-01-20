package llm

import (
	"context"
	"fmt"
	"time"

	"github.com/cloudwego/eino/schema"

	"backend-server/constants"
	"backend-server/internal/domain/llm/eino_llm"
	"backend-server/pkg/confighelper"
)

// LLMProvider 大语言模型提供者接口
// 所有LLM实现必须遵循此接口，使用Eino原生类型
type LLMProvider interface {
	// ResponseWithContext 带有上下文控制的响应，支持取消操作
	// ctx: 上下文，可用于取消长时间运行的请求
	// sessionID: 会话标识符
	// dialogue: 对话历史，使用Eino原生消息类型
	ResponseWithContext(ctx context.Context, sessionID string, dialogue []*schema.Message, functions []*schema.ToolInfo) chan *schema.Message

	ResponseWithVLM(ctx context.Context, file []byte, text string, mimeType string) (string, error)

	// GetModelInfo 获取模型信息
	// 返回模型名称和其他元数据
	GetModelInfo() map[string]interface{}
}

// LLMFactory 大语言模型工厂接口
// 用于创建不同类型的LLM提供者
type LLMFactory interface {
	// CreateProvider 根据配置创建LLM提供者
	CreateProvider(config map[string]interface{}) (LLMProvider, error)
}

// GetLLMProvider 创建LLM提供者
// 统一使用EinoLLMProvider处理所有类型

func GetLLMProvider(providerName string, config map[string]interface{}) (LLMProvider, error) {
	cfg := confighelper.New(config)
	llmType := cfg.GetString("type", providerName)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	switch llmType {
	case constants.LlmTypeOpenai, constants.LlmTypeOllama, constants.LlmTypeEinoLLM, constants.LlmTypeEino:
		// 统一使用 EinoLLMProvider 处理所有类型
		provider, err := eino_llm.NewEinoLLMProvider(ctx, config)
		if err != nil {
			return nil, fmt.Errorf("创建Eino LLM提供者失败: %v", err)
		}
		return provider, nil
	}
	return nil, fmt.Errorf("不支持的LLM提供者: %s", llmType)
}

// Config LLM配置结构
type Config struct {
	ModelName  string                 `json:"model_name"`
	APIKey     string                 `json:"api_key"`
	BaseURL    string                 `json:"base_url"`
	MaxTokens  int                    `json:"max_tokens"`
	Parameters map[string]interface{} `json:"parameters,omitempty"`
}
