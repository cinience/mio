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
	// EditImageFunctionName 定义图像编辑函数名称
	EditImageFunctionName = "edit_image"
)

// EditImageFunctionDesc 提供给LLM的函数描述
var EditImageFunctionDesc = map[string]interface{}{
	"type": "function",
	"function": map[string]interface{}{
		"name":        EditImageFunctionName,
		"description": "根据已有图片与文本提示进行图像编辑，可用于风格化、替换背景或修复等场景",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"prompt": map[string]interface{}{
					"type":        "string",
					"description": "图像编辑的文本指令，描述要修改的内容",
				},
				"image_url": map[string]interface{}{
					"type":        "string",
					"description": "待编辑图片的URL",
				},
				"image_base64": map[string]interface{}{
					"type":        "string",
					"description": "待编辑图片的base64字符串（优先使用image_url）",
				},
				"mask_url": map[string]interface{}{
					"type":        "string",
					"description": "可选遮罩图片URL，用于局部编辑",
				},
				"model": map[string]interface{}{
					"type":        "string",
					"description": "可选的模型名称，不指定则使用默认配置",
				},
				"size": map[string]interface{}{
					"type":        "string",
					"description": "输出图片尺寸，例如 1024x1024、1664*928 等",
				},
				"negative_prompt": map[string]interface{}{
					"type":        "string",
					"description": "负向提示词，用于抑制不希望出现的内容",
				},
				"prompt_extend": map[string]interface{}{
					"type":        "boolean",
					"description": "是否自动扩展提示词",
				},
				"watermark": map[string]interface{}{
					"type":        "boolean",
					"description": "是否添加水印",
				},
			},
			"required": []string{"prompt"},
		},
	},
}

// EditImageFunction 实现图像编辑功能
type EditImageFunction struct{}

// NewEditImageFunction 创建新的图像编辑函数
func NewEditImageFunction() *EditImageFunction {
	return &EditImageFunction{}
}

// Execute 执行图像编辑逻辑
func (f *EditImageFunction) Execute(ctx context.Context, conn types.Connection, args map[string]interface{}) (*types.ActionResponse, error) {
	var logger *slog.Logger
	if conn != nil {
		logger = conn.GetLogger()
	}

	prompt := ""
	if v, ok := args["prompt"].(string); ok {
		prompt = strings.TrimSpace(v)
	}
	if prompt == "" {
		msg := "请提供图像编辑的描述"
		if logger != nil {
			logger.Warn(msg)
		}
		return types.NewActionResponse(types.ActionDirectResponse, nil, msg), nil
	}

	imageURL := ""
	if v, ok := args["image_url"].(string); ok {
		imageURL = strings.TrimSpace(v)
	}
	imageBase64 := ""
	if v, ok := args["image_base64"].(string); ok {
		imageBase64 = strings.TrimSpace(v)
	}
	if imageURL == "" && imageBase64 == "" {
		msg := "请提供待编辑的图片URL或base64内容"
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
		log.Errorf("解析图像编辑提供者失败: %v", err)
		return types.NewActionResponse(types.ActionDirectResponse, nil, fmt.Sprintf("暂时无法编辑图片:%v", err)), nil
	}

	if !supportsCapability(providerConfig, "image_edit") {
		return types.NewActionResponse(types.ActionDirectResponse, nil, "当前配置的图像模型不支持图像编辑"), nil
	}

	provider, err := imagegen.GetProvider(providerName, providerConfig)
	if err != nil {
		log.Errorf("创建图像编辑provider失败: %v", err)
		return types.NewActionResponse(types.ActionDirectResponse, nil, "图像编辑服务暂不可用"), nil
	}

	editProvider, ok := provider.(imagegen.EditProvider)
	if !ok {
		return types.NewActionResponse(types.ActionDirectResponse, nil, "图像编辑服务暂不可用"), nil
	}

	req := &imagegen.EditRequest{
		Prompt:         prompt,
		Model:          getStringArg(args, "model"),
		ImageURL:       imageURL,
		ImageBase64:    imageBase64,
		MaskURL:        getStringArg(args, "mask_url"),
		Size:           getStringArg(args, "size"),
		NegativePrompt: getStringArg(args, "negative_prompt"),
		User:           deviceID,
	}

	if val, ok := getBoolArg(args, "prompt_extend"); ok {
		req.PromptExtend = &val
	}
	if val, ok := getBoolArg(args, "watermark"); ok {
		req.Watermark = &val
	}

	applyStringDefault(&req.Model, providerConfig, "model", "model_name")
	applyStringDefault(&req.Size, providerConfig, "size")
	if req.NegativePrompt == "" {
		applyStringDefault(&req.NegativePrompt, providerConfig, "negative_prompt")
	}

	if logger != nil {
		logger.Info("开始图像编辑", "device_id", deviceID, "provider", providerName)
	}

	resp, err := editProvider.Edit(ctx, req)
	if err != nil {
		log.Errorf("图像编辑失败: %v", err)
		return types.NewActionResponse(types.ActionDirectResponse, nil, fmt.Sprintf("图像编辑失败: %v", err)), nil
	}

	result := buildEditResult(providerName, req, resp)

	related := map[string]any{
		"tool":     EditImageFunctionName,
		"provider": providerName,
		"model":    req.Model,
		"prompt":   req.Prompt,
	}
	storedImages, urls := storeImagesToMedia(
		ctx,
		managerSvc,
		deviceID,
		"imagegen",
		"edited image",
		related,
		result.Images,
	)
	result.Images = storedImages

	message := fmt.Sprintf("已生成%d张编辑结果", len(result.Images))
	if markdown := buildMarkdownImageLinks(urls); markdown != "" {
		message = markdown
	}
	if logger != nil {
		logger.Info("图像编辑成功", "count", len(result.Images))
	}

	return types.NewActionResponse(types.ActionDirectResponse, result, message), nil
}

// GetInfo 返回工具信息
func (f *EditImageFunction) GetInfo() *schema.ToolInfo {
	converter := eino_integration.GetGlobalConverter()
	toolInfo, err := converter.ConvertPythonDescToEinoToolInfo(EditImageFunctionName, EditImageFunctionDesc)
	if err != nil {
		log.Errorf("转换图像编辑工具描述失败: %v", err)
		return nil
	}
	return toolInfo
}

// GetType 返回工具类型
func (f *EditImageFunction) GetType() types.ToolType {
	return types.ToolTypeWait
}

// GetName 返回工具名称
func (f *EditImageFunction) GetName() string {
	return EditImageFunctionName
}

// GetDescription 返回工具描述
func (f *EditImageFunction) GetDescription() interface{} {
	return EditImageFunctionDesc
}

// ImageEditResult 图像编辑结果结构
type ImageEditResult struct {
	Provider string                `json:"provider"`
	Model    string                `json:"model"`
	Created  int64                 `json:"created"`
	Images   []imagegen.ImageData  `json:"images"`
	Options  *imagegen.EditRequest `json:"options,omitempty"`
}

func buildEditResult(provider string, req *imagegen.EditRequest, resp *imagegen.GenerateResponse) *ImageEditResult {
	result := &ImageEditResult{
		Provider: provider,
		Model:    req.Model,
		Created:  resp.Created,
		Images:   append([]imagegen.ImageData(nil), resp.Data...),
	}

	result.Options = &imagegen.EditRequest{
		Prompt:         req.Prompt,
		Model:          req.Model,
		ImageURL:       req.ImageURL,
		ImageBase64:    req.ImageBase64,
		MaskURL:        req.MaskURL,
		Size:           req.Size,
		NegativePrompt: req.NegativePrompt,
		PromptExtend:   req.PromptExtend,
		Watermark:      req.Watermark,
		User:           req.User,
	}

	return result
}

func getBoolArg(args map[string]interface{}, key string) (bool, bool) {
	if v, ok := args[key]; ok {
		switch val := v.(type) {
		case bool:
			return val, true
		case string:
			lower := strings.ToLower(strings.TrimSpace(val))
			if lower == "true" || lower == "false" {
				return lower == "true", true
			}
		}
	}
	return false, false
}

func supportsCapability(cfg map[string]interface{}, capability string) bool {
	return supportsImageCapability(cfg, capability)
}

// RegisterEditImageFunction 自动注册图像编辑函数
func RegisterEditImageFunction() error {
	function := NewEditImageFunction()
	return tools.RegisterGlobalFunction(EditImageFunctionName, function)
}

func init() {
	if err := RegisterEditImageFunction(); err != nil {
		log.Errorf("注册图像编辑函数失败: %v", err)
	}
}
