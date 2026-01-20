package imagegen

import (
	"context"
	"fmt"

	"backend-server/pkg/confighelper"
)

// GenerateRequest 封装生图生成请求参数，字段与OpenAI生图协议保持一致
type GenerateRequest struct {
	Prompt            string
	Model             string
	N                 int
	Quality           string
	Size              string
	Style             string
	ResponseFormat    string
	User              string
	Background        string
	Moderation        string
	OutputCompression int
	OutputFormat      string
}

// EditRequest 封装图像编辑请求参数
type EditRequest struct {
	Prompt         string
	Model          string
	ImageURL       string
	ImageBase64    string
	MaskURL        string
	Size           string
	NegativePrompt string
	PromptExtend   *bool
	Watermark      *bool
	User           string
}

// ImageData 表示生成结果中的单张图片数据
type ImageData struct {
	URL           string `json:"url,omitempty"`
	B64JSON       string `json:"b64_json,omitempty"`
	RevisedPrompt string `json:"revised_prompt,omitempty"`
}

// Usage 表示生图接口返回的token用量信息
type Usage struct {
	TotalTokens  int `json:"total_tokens,omitempty"`
	InputTokens  int `json:"input_tokens,omitempty"`
	OutputTokens int `json:"output_tokens,omitempty"`
	TextTokens   int `json:"text_tokens,omitempty"`
	ImageTokens  int `json:"image_tokens,omitempty"`
}

// GenerateResponse 封装生图生成响应数据
type GenerateResponse struct {
	Created int64       `json:"created,omitempty"`
	Data    []ImageData `json:"data,omitempty"`
	Usage   *Usage      `json:"usage,omitempty"`
}

// Provider 生图模型提供者接口
type Provider interface {
	Generate(ctx context.Context, req *GenerateRequest) (*GenerateResponse, error)
}

// EditProvider 支持图像编辑的提供者接口
type EditProvider interface {
	Edit(ctx context.Context, req *EditRequest) (*GenerateResponse, error)
}

// GetProvider 根据提供者配置创建生图提供者实例
func GetProvider(providerName string, config map[string]interface{}) (Provider, error) {
	if providerName == "" {
		return nil, fmt.Errorf("provider不能为空")
	}

	cfgHelper := confighelper.New(config)
	providerType := cfgHelper.GetString("type", providerName)

	switch providerType {
	case "openai":
		return newOpenAIProvider(config)
	case "dashscope":
		return newDashscopeProvider(config)
	default:
		return nil, fmt.Errorf("不支持的生图提供者类型: %s", providerType)
	}
}
