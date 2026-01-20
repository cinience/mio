package functions

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"backend-server/internal/adapters/manager"
	"backend-server/internal/app/service"
	"backend-server/internal/domain/imagegen"
	"backend-server/internal/domain/tools"
	"backend-server/internal/domain/tools/eino_integration"
	"backend-server/internal/domain/tools/types"
	log "backend-server/internal/infrastructure/logger"

	"github.com/cloudwego/eino/schema"
)

const (
	// GenerateImageFunctionName 定义生图函数名称
	GenerateImageFunctionName = "generate_image"
)

// GenerateImageFunctionDesc 提供给LLM的函数描述
var GenerateImageFunctionDesc = map[string]interface{}{
	"type": "function",
	"function": map[string]interface{}{
		"name":        GenerateImageFunctionName,
		"description": "根据文本提示生成图片，支持指定尺寸、风格和质量等参数",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"prompt": map[string]interface{}{
					"type":        "string",
					"description": "用于生成图片的详细文本描述，越具体越好",
				},
				"model": map[string]interface{}{
					"type":        "string",
					"description": "可选的模型名称，不指定则使用默认配置",
				},
				"size": map[string]interface{}{
					"type":        "string",
					"description": "生成图片尺寸，例如 1024x1024、512x512 等",
				},
				"style": map[string]interface{}{
					"type":        "string",
					"description": "生成图片的风格，例如 vivid、natural 等",
				},
				"quality": map[string]interface{}{
					"type":        "string",
					"description": "生成质量，可选 standard/hd/high",
				},
				"response_format": map[string]interface{}{
					"type":        "string",
					"description": "返回格式，可选 url 或 b64_json，默认继承配置",
				},
				"n": map[string]interface{}{
					"type":        "integer",
					"description": "生成的图片数量，默认为1",
					"minimum":     1,
					"maximum":     4,
				},
			},
			"required": []string{"prompt"},
		},
	},
}

// GenerateImageFunction 实现图像生成功能
type GenerateImageFunction struct{}

// NewGenerateImageFunction 创建新的生图函数
func NewGenerateImageFunction() *GenerateImageFunction {
	return &GenerateImageFunction{}
}

// Execute 执行生图逻辑
func (f *GenerateImageFunction) Execute(ctx context.Context, conn types.Connection, args map[string]interface{}) (*types.ActionResponse, error) {
	var logger *slog.Logger
	if conn != nil {
		logger = conn.GetLogger()
	}

	prompt := ""
	if v, ok := args["prompt"].(string); ok {
		prompt = strings.TrimSpace(v)
	}
	if prompt == "" {
		msg := "请提供用于生成图片的描述"
		if logger != nil {
			logger.Warn(msg)
		}
		return types.NewActionResponse(types.ActionDirectResponse, nil, msg), nil
	}

	deviceID := ""
	if conn != nil {
		deviceID = conn.GetDeviceID()
	}

	var managerSvc manager_api.ManagerAPIService
	if conn != nil {
		managerSvc = conn.GetManagerAPIService()
	}
	if managerSvc == nil {
		managerSvc = service.DefaultRegistry().ManagerAPIService()
	}

	providerName, providerConfig, err := imagegen.ResolveProvider(ctx, managerSvc, deviceID, "")
	if err != nil {
		log.Errorf("解析生图提供者失败: %v", err)
		return types.NewActionResponse(types.ActionDirectResponse, nil, fmt.Sprintf("暂时无法生成图片:%v", err)), nil
	}

	if !supportsImageCapability(providerConfig, "text_to_image") {
		return types.NewActionResponse(types.ActionDirectResponse, nil, "当前配置的图像模型不支持文生图"), nil
	}

	provider, err := imagegen.GetProvider(providerName, providerConfig)
	if err != nil {
		log.Errorf("创建生图provider失败: %v", err)
		return types.NewActionResponse(types.ActionDirectResponse, nil, "图片生成服务暂不可用"), nil
	}

	req := &imagegen.GenerateRequest{
		Prompt:         prompt,
		Model:          getStringArg(args, "model"),
		Quality:        getStringArg(args, "quality"),
		Size:           getStringArg(args, "size"),
		Style:          getStringArg(args, "style"),
		ResponseFormat: getStringArg(args, "response_format"),
		User:           deviceID,
	}

	if n, ok := getIntArg(args, "n"); ok {
		req.N = n
	}

	applyStringDefault(&req.Model, providerConfig, "model", "model_name")
	applyStringDefault(&req.ResponseFormat, providerConfig, "response_format")
	applyStringDefault(&req.Size, providerConfig, "size")
	applyStringDefault(&req.Style, providerConfig, "style")
	applyStringDefault(&req.Quality, providerConfig, "quality")
	applyIntDefault(&req.N, providerConfig, "n")

	if req.N <= 0 {
		req.N = 1
	}
	if req.N > 4 {
		req.N = 4
	}

	if normalized := normalizeImageSize(req.Size); normalized != "" {
		req.Size = normalized
	}

	if logger != nil {
		logger.Info("开始生成图片", "device_id", deviceID, "provider", providerName)
	}

	resp, err := provider.Generate(ctx, req)
	if err != nil {
		log.Errorf("图片生成失败: %v", err)
		return types.NewActionResponse(types.ActionDirectResponse, nil, fmt.Sprintf("生成图片失败: %v", err)), nil
	}

	result := buildImageResult(providerName, req, resp)

	related := map[string]any{
		"tool":     GenerateImageFunctionName,
		"provider": providerName,
		"model":    req.Model,
		"prompt":   req.Prompt,
	}
	storedImages, urls := storeImagesToMedia(
		ctx,
		managerSvc,
		deviceID,
		"imagegen",
		"generated image",
		related,
		result.Images,
	)
	result.Images = storedImages

	message := fmt.Sprintf("已生成%d张图片", len(result.Images))
	if markdown := buildMarkdownImageLinks(urls); markdown != "" {
		message = markdown
	}
	if logger != nil {
		logger.Info("图片生成成功", "count", len(result.Images))
	}

	return types.NewActionResponse(types.ActionDirectResponse, result, message), nil
}

func normalizeImageSize(input string) string {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return ""
	}
	normalized := strings.ToLower(trimmed)
	normalized = strings.ReplaceAll(normalized, "×", "*")
	normalized = strings.ReplaceAll(normalized, "x", "*")
	normalized = strings.ReplaceAll(normalized, "＊", "*")
	normalized = strings.ReplaceAll(normalized, " ", "")
	return normalized
}

// GetInfo 返回工具信息
func (f *GenerateImageFunction) GetInfo() *schema.ToolInfo {
	converter := eino_integration.GetGlobalConverter()
	toolInfo, err := converter.ConvertPythonDescToEinoToolInfo(GenerateImageFunctionName, GenerateImageFunctionDesc)
	if err != nil {
		log.Errorf("转换生图工具描述失败: %v", err)
		return nil
	}
	return toolInfo
}

// GetType 返回工具类型
func (f *GenerateImageFunction) GetType() types.ToolType {
	return types.ToolTypeWait
}

// GetName 返回工具名称
func (f *GenerateImageFunction) GetName() string {
	return GenerateImageFunctionName
}

// GetDescription 返回工具描述
func (f *GenerateImageFunction) GetDescription() interface{} {
	return GenerateImageFunctionDesc
}

// ImageGenerationResult 生图结果结构
type ImageGenerationResult struct {
	Provider string                    `json:"provider"`
	Model    string                    `json:"model"`
	Created  int64                     `json:"created"`
	Images   []imagegen.ImageData      `json:"images"`
	Usage    *imagegen.Usage           `json:"usage,omitempty"`
	Options  *imagegen.GenerateRequest `json:"options,omitempty"`
}

func buildImageResult(provider string, req *imagegen.GenerateRequest, resp *imagegen.GenerateResponse) *ImageGenerationResult {
	result := &ImageGenerationResult{
		Provider: provider,
		Model:    req.Model,
		Created:  resp.Created,
		Images:   append([]imagegen.ImageData(nil), resp.Data...),
		Usage:    resp.Usage,
	}

	// 记录实际使用的请求参数，便于追踪
	result.Options = &imagegen.GenerateRequest{
		Prompt:            req.Prompt,
		Model:             req.Model,
		N:                 req.N,
		Quality:           req.Quality,
		Size:              req.Size,
		Style:             req.Style,
		ResponseFormat:    req.ResponseFormat,
		User:              req.User,
		Background:        req.Background,
		Moderation:        req.Moderation,
		OutputFormat:      req.OutputFormat,
		OutputCompression: req.OutputCompression,
	}

	return result
}

func getStringArg(args map[string]interface{}, key string) string {
	if v, ok := args[key]; ok {
		switch val := v.(type) {
		case string:
			return strings.TrimSpace(val)
		}
	}
	return ""
}

func getIntArg(args map[string]interface{}, key string) (int, bool) {
	if v, ok := args[key]; ok {
		switch val := v.(type) {
		case int:
			return val, true
		case float64:
			return int(val), true
		}
	}
	return 0, false
}

func applyStringDefault(target *string, cfg map[string]interface{}, keys ...string) {
	if target == nil || *target != "" {
		return
	}
	for _, key := range keys {
		if v, ok := cfg[key]; ok {
			if s, ok := v.(string); ok {
				trimmed := strings.TrimSpace(s)
				if trimmed != "" {
					*target = trimmed
					return
				}
			}
		}
	}
}

func applyIntDefault(target *int, cfg map[string]interface{}, keys ...string) {
	if target == nil || *target > 0 {
		return
	}
	for _, key := range keys {
		if v, ok := cfg[key]; ok {
			switch val := v.(type) {
			case int:
				if val > 0 {
					*target = val
					return
				}
			case float64:
				if val > 0 {
					*target = int(val)
					return
				}
			}
		}
	}
}

// RegisterGenerateImageFunction 自动注册生图函数
func RegisterGenerateImageFunction() error {
	function := NewGenerateImageFunction()
	return tools.RegisterGlobalFunction(GenerateImageFunctionName, function)
}

func init() {
	if err := RegisterGenerateImageFunction(); err != nil {
		log.Errorf("注册生图函数失败: %v", err)
	}
}
