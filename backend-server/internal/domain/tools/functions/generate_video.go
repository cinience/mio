package functions

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	manager_api "backend-server/internal/adapters/manager"
	"backend-server/internal/app/service"
	"backend-server/internal/domain/tools"
	"backend-server/internal/domain/tools/eino_integration"
	"backend-server/internal/domain/tools/types"
	"backend-server/internal/domain/videogen"
	log "backend-server/internal/infrastructure/logger"

	"github.com/cloudwego/eino/schema"
)

const (
	// GenerateVideoFunctionName 定义生视频函数名称
	GenerateVideoFunctionName = "generate_video"
)

// GenerateVideoFunctionDesc 提供给LLM的函数描述
var GenerateVideoFunctionDesc = map[string]interface{}{
	"type": "function",
	"function": map[string]interface{}{
		"name":        GenerateVideoFunctionName,
		"description": "根据文本提示生成视频，支持尺寸、时长、分镜类型与音频驱动参数",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"prompt": map[string]interface{}{
					"type":        "string",
					"description": "用于生成视频的详细文本描述，越具体越好",
				},
				"model": map[string]interface{}{
					"type":        "string",
					"description": "可选的模型名称，不指定则使用默认配置",
				},
				"size": map[string]interface{}{
					"type":        "string",
					"description": "视频尺寸，例如 1280*720",
				},
				"duration": map[string]interface{}{
					"type":        "integer",
					"description": "视频时长(秒)，例如 10",
					"minimum":     1,
					"maximum":     60,
				},
				"shot_type": map[string]interface{}{
					"type":        "string",
					"description": "分镜类型，例如 multi/single",
				},
				"audio_url": map[string]interface{}{
					"type":        "string",
					"description": "可选的音频地址，用于音频驱动视频生成",
				},
				"prompt_extend": map[string]interface{}{
					"type":        "boolean",
					"description": "是否自动扩展提示词",
				},
			},
			"required": []string{"prompt"},
		},
	},
}

// GenerateVideoFunction 实现视频生成功能
// 使用 DashScope 万象异步任务接口并轮询获取结果
type GenerateVideoFunction struct{}

// NewGenerateVideoFunction 创建新的生视频函数
func NewGenerateVideoFunction() *GenerateVideoFunction {
	return &GenerateVideoFunction{}
}

// Execute 执行生视频逻辑
func (f *GenerateVideoFunction) Execute(ctx context.Context, conn types.Connection, args map[string]interface{}) (*types.ActionResponse, error) {
	var logger *slog.Logger
	if conn != nil {
		logger = conn.GetLogger()
	}

	prompt := ""
	if v, ok := args["prompt"].(string); ok {
		prompt = strings.TrimSpace(v)
	}
	if prompt == "" {
		msg := "请提供用于生成视频的描述"
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

	providerName, providerConfig, err := videogen.ResolveProvider(ctx, managerSvc, deviceID, "")
	if err != nil {
		log.Errorf("解析生视频提供者失败: %v", err)
		return types.NewActionResponse(types.ActionDirectResponse, nil, fmt.Sprintf("暂时无法生成视频:%v", err)), nil
	}

	if !supportsVideoCapability(providerConfig, "text_to_video") {
		return types.NewActionResponse(types.ActionDirectResponse, nil, "当前配置的视频模型不支持文生视频"), nil
	}

	provider, err := videogen.GetProvider(providerName, providerConfig)
	if err != nil {
		log.Errorf("创建生视频provider失败: %v", err)
		return types.NewActionResponse(types.ActionDirectResponse, nil, "视频生成服务暂不可用"), nil
	}

	audioURL := getStringArg(args, "audio_url")
	if audioURL == "" && conn != nil {
		if bytes, contentType := conn.GetUserAudio(); len(bytes) > 0 {
			if supportsVideoCapability(providerConfig, "audio_guided") {
				url, err := uploadAudioToMedia(ctx, managerSvc, deviceID, "videogen", "voice prompt", bytes, contentType)
				if err != nil {
					log.Warnf("上传语音音频失败: %v", err)
				} else {
					audioURL = url
				}
			}
		}
	}
	if audioURL != "" && !supportsVideoCapability(providerConfig, "audio_guided") {
		return types.NewActionResponse(types.ActionDirectResponse, nil, "当前配置的视频模型不支持音频驱动"), nil
	}

	req := &videogen.GenerateRequest{
		Prompt:   prompt,
		Model:    getStringArg(args, "model"),
		Size:     getStringArg(args, "size"),
		Duration: getIntArgWithDefault(args, "duration", 0),
		ShotType: getStringArg(args, "shot_type"),
		AudioURL: audioURL,
		User:     deviceID,
	}

	if value, ok := args["prompt_extend"].(bool); ok {
		req.PromptExtend = &value
	}

	applyStringDefault(&req.Model, providerConfig, "model", "model_name")
	applyStringDefault(&req.Size, providerConfig, "size")
	applyStringDefault(&req.ShotType, providerConfig, "shot_type")
	applyIntDefault(&req.Duration, providerConfig, "duration")
	if req.PromptExtend == nil {
		if v, ok := providerConfig["prompt_extend"].(bool); ok {
			req.PromptExtend = &v
		}
	}

	if req.Duration <= 0 {
		req.Duration = 10
	}
	if normalized := normalizeVideoSize(req.Size); normalized != "" {
		req.Size = normalized
	}

	if logger != nil {
		logger.Info("开始生成视频", "device_id", deviceID, "provider", providerName)
	}

	resp, err := provider.Generate(ctx, req)
	if err != nil {
		log.Errorf("视频生成失败: %v", err)
		return types.NewActionResponse(types.ActionDirectResponse, nil, fmt.Sprintf("生成视频失败: %v", err)), nil
	}

	result := buildVideoResult(providerName, req, resp)

	related := map[string]any{
		"tool":     GenerateVideoFunctionName,
		"provider": providerName,
		"model":    req.Model,
		"prompt":   req.Prompt,
	}
	storedVideos, urls := storeVideosToMedia(
		ctx,
		managerSvc,
		deviceID,
		"videogen",
		"generated video",
		related,
		result.Videos,
	)
	result.Videos = storedVideos

	message := fmt.Sprintf("已生成%d个视频", len(result.Videos))
	if markdown := buildMarkdownVideoLinks(urls); markdown != "" {
		message = markdown
	}
	if logger != nil {
		logger.Info("视频生成成功", "count", len(result.Videos))
	}

	return types.NewActionResponse(types.ActionDirectResponse, result, message), nil
}

func normalizeVideoSize(input string) string {
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
func (f *GenerateVideoFunction) GetInfo() *schema.ToolInfo {
	converter := eino_integration.GetGlobalConverter()
	toolInfo, err := converter.ConvertPythonDescToEinoToolInfo(GenerateVideoFunctionName, GenerateVideoFunctionDesc)
	if err != nil {
		log.Errorf("转换生视频工具描述失败: %v", err)
		return nil
	}
	return toolInfo
}

// GetType 返回工具类型
func (f *GenerateVideoFunction) GetType() types.ToolType {
	return types.ToolTypeWait
}

// GetName 返回工具名称
func (f *GenerateVideoFunction) GetName() string {
	return GenerateVideoFunctionName
}

// GetDescription 返回工具描述
func (f *GenerateVideoFunction) GetDescription() interface{} {
	return GenerateVideoFunctionDesc
}

// VideoGenerationResult 生视频结果结构
type VideoGenerationResult struct {
	Provider string                    `json:"provider"`
	Model    string                    `json:"model"`
	Created  int64                     `json:"created"`
	TaskID   string                    `json:"task_id,omitempty"`
	Status   string                    `json:"status,omitempty"`
	Videos   []videogen.VideoData      `json:"videos"`
	Options  *videogen.GenerateRequest `json:"options,omitempty"`
}

func buildVideoResult(provider string, req *videogen.GenerateRequest, resp *videogen.GenerateResponse) *VideoGenerationResult {
	result := &VideoGenerationResult{
		Provider: provider,
		Model:    req.Model,
		Created:  resp.Created,
		TaskID:   resp.TaskID,
		Status:   resp.Status,
		Videos:   append([]videogen.VideoData(nil), resp.Data...),
	}

	result.Options = &videogen.GenerateRequest{
		Prompt:       req.Prompt,
		Model:        req.Model,
		Size:         req.Size,
		Duration:     req.Duration,
		ShotType:     req.ShotType,
		PromptExtend: req.PromptExtend,
		AudioURL:     req.AudioURL,
		User:         req.User,
	}

	return result
}

func getIntArgWithDefault(args map[string]interface{}, key string, fallback int) int {
	if args == nil {
		return fallback
	}
	if val, ok := args[key]; ok {
		switch v := val.(type) {
		case int:
			return v
		case int64:
			return int(v)
		case float64:
			return int(v)
		}
	}
	return fallback
}

// RegisterGenerateVideoFunction 自动注册生视频函数
func RegisterGenerateVideoFunction() error {
	function := NewGenerateVideoFunction()
	return tools.RegisterGlobalFunction(GenerateVideoFunctionName, function)
}

func init() {
	if err := RegisterGenerateVideoFunction(); err != nil {
		log.Errorf("注册生视频函数失败: %v", err)
	}
}
