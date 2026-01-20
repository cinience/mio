package functions

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"backend-server/internal/adapters/manager"
	"backend-server/internal/domain/tools"
	"backend-server/internal/domain/tools/eino_integration"
	"backend-server/internal/domain/tools/types"
	log "backend-server/internal/infrastructure/logger"
	"backend-server/internal/app/service"

	"github.com/cloudwego/eino/schema"
)

const (
	// ListVoiceOptionsFunctionName is the identifier for querying available voices.
	ListVoiceOptionsFunctionName = "list_voice_options"
)

var (
	// ListVoiceOptionsFunctionDesc provides the schema description for listing available voices.
	ListVoiceOptionsFunctionDesc = map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        ListVoiceOptionsFunctionName,
			"description": "查询指定设备所属智能体当前可用的TTS音色列表，切换音色前请调用获取选项",
			"parameters": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"device_id": map[string]interface{}{
						"type":        "string",
						"description": "可选，指定设备ID，默认为当前会话设备",
					},
					"keyword": map[string]interface{}{
						"type":        "string",
						"description": "可选，按音色名称关键字进行模糊筛选",
					},
				},
			},
		},
	}
)

// ListVoiceOptionsFunction exposes the agent voice listing capability.
type ListVoiceOptionsFunction struct {
	config map[string]interface{}
}

// NewListVoiceOptionsFunction creates a new instance for listing available voices.
func NewListVoiceOptionsFunction(args map[string]interface{}) *ListVoiceOptionsFunction {
	return &ListVoiceOptionsFunction{config: args}
}

// Execute fetches the available voices from manager-api.
func (f *ListVoiceOptionsFunction) Execute(ctx context.Context, conn types.Connection, args map[string]interface{}) (*types.ActionResponse, error) {
	payload := make(map[string]interface{})
	for k, v := range f.config {
		payload[k] = v
	}
	for k, v := range args {
		payload[k] = v
	}

	deviceID := ""
	if conn != nil {
		deviceID = strings.TrimSpace(conn.GetDeviceID())
	}
	if deviceID == "" || deviceID == "<nil>" {
		deviceID = strings.TrimSpace(fmt.Sprintf("%v", payload["device_id"]))
	}
	if deviceID == "" || deviceID == "<nil>" {
		return types.NewActionResponse(types.ActionError, "查询音色失败", "缺少device_id参数"), fmt.Errorf("missing device_id")
	}

	keyword := strings.TrimSpace(fmt.Sprintf("%v", payload["keyword"]))
	if keyword == "<nil>" {
		keyword = ""
	}

	var logger *slog.Logger
	if conn != nil {
		logger = conn.GetLogger()
	}

	var svc manager_api.ManagerAPIService
	if conn != nil {
		svc = conn.GetManagerAPIService()
	}
	if svc == nil {
		svc = service.DefaultRegistry().ManagerAPIService()
	}
	if svc == nil {
		return types.NewActionResponse(types.ActionError, "查询音色失败", "管理服务未就绪"), fmt.Errorf("manager-api service unavailable")
	}

	options, err := svc.GetAgentVoiceOptions(ctx, deviceID, keyword)
	if err != nil {
		if logger != nil {
			logger.Error("查询音色失败", "error", err)
		}
		return types.NewActionResponse(types.ActionError, "查询音色失败", err.Error()), err
	}
	if options == nil {
		return types.NewActionResponse(types.ActionError, "查询音色失败", "返回数据为空"), fmt.Errorf("empty voice options")
	}

	voices := make([]map[string]string, 0, len(options.Voices))
	voiceNames := make([]string, 0, len(options.Voices))
	currentVoiceName := strings.TrimSpace(options.TTSVoiceID)
	if currentVoiceName == "" {
		currentVoiceName = "未设置"
	}

	for _, voice := range options.Voices {
		if voice == nil {
			continue
		}
		entry := map[string]string{
			"id":         strings.TrimSpace(voice.ID),
			"name":       strings.TrimSpace(voice.Name),
			"tts_voice":  strings.TrimSpace(voice.TTSVoice),
			"languages":  strings.TrimSpace(voice.Languages),
			"voice_demo": strings.TrimSpace(voice.VoiceDemo),
		}
		voices = append(voices, entry)

		displayName := entry["name"]
		if displayName == "" {
			displayName = entry["tts_voice"]
		}
		if displayName == "" {
			displayName = entry["id"]
		}
		if lang := entry["languages"]; lang != "" {
			displayName = fmt.Sprintf("%s(%s)", displayName, lang)
		}
		voiceNames = append(voiceNames, displayName)

		if entry["id"] == options.TTSVoiceID && entry["name"] != "" {
			currentVoiceName = entry["name"]
		}
	}

	responseData := map[string]interface{}{
		"device_id":          deviceID,
		"tts_model_id":       options.TTSModelID,
		"current_voice_id":   options.TTSVoiceID,
		"current_voice_name": currentVoiceName,
		"voice_count":        len(voices),
		"voices":             voices,
	}
	if keyword != "" {
		responseData["keyword"] = keyword
	}

	var message string
	switch {
	case len(voices) == 0 && keyword != "":
		message = fmt.Sprintf("没有找到包含“%s”的可用音色。", keyword)
	case len(voices) == 0:
		message = fmt.Sprintf("设备%s当前未配置可用音色。", deviceID)
	default:
		joined := strings.Join(voiceNames, "，")
		prefix := fmt.Sprintf("设备%s当前音色为%s", deviceID, currentVoiceName)
		if keyword != "" {
			message = fmt.Sprintf("%s，匹配到的音色有: %s", prefix, joined)
		} else {
			message = fmt.Sprintf("%s，可选音色包括: %s", prefix, joined)
		}
	}

	if logger != nil {
		logger.Info("获取音色列表成功", "device_id", deviceID, "keyword", keyword, "count", len(voices))
	}

	return types.NewActionResponse(types.ActionDirectResponse, responseData, message), nil
}

// GetInfo returns the eino ToolInfo definition for listing voices.
func (f *ListVoiceOptionsFunction) GetInfo() *schema.ToolInfo {
	converter := eino_integration.GetGlobalConverter()
	toolInfo, err := converter.ConvertPythonDescToEinoToolInfo(ListVoiceOptionsFunctionName, ListVoiceOptionsFunctionDesc)
	if err != nil {
		log.Errorf("Failed to convert tool info for %s: %v", ListVoiceOptionsFunctionName, err)
		return nil
	}
	return toolInfo
}

// GetType returns the tool type classification.
func (f *ListVoiceOptionsFunction) GetType() types.ToolType {
	return types.ToolTypeWait
}

// GetName returns the function name identifier.
func (f *ListVoiceOptionsFunction) GetName() string {
	return ListVoiceOptionsFunctionName
}

// GetDescription returns the raw description payload.
func (f *ListVoiceOptionsFunction) GetDescription() interface{} {
	return ListVoiceOptionsFunctionDesc
}

// RegisterListVoiceOptionsFunction registers the list voice options function globally.
func RegisterListVoiceOptionsFunction() error {
	function := NewListVoiceOptionsFunction(nil)
	return tools.RegisterGlobalFunction(ListVoiceOptionsFunctionName, function)
}

func init() {
	if err := RegisterListVoiceOptionsFunction(); err != nil {
		log.Errorf("Failed to register %s function: %v", ListVoiceOptionsFunctionName, err)
	}
}
