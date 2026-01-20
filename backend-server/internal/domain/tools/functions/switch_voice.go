package functions

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"backend-server/internal/adapters/manager"
	managerapitypes "backend-server/internal/adapters/manager/types"
	"backend-server/internal/domain/tools"
	"backend-server/internal/domain/tools/eino_integration"
	"backend-server/internal/domain/tools/types"
	log "backend-server/internal/infrastructure/logger"
	"backend-server/internal/app/service"

	"github.com/cloudwego/eino/schema"
)

const (
	// SwitchVoiceFunctionName is the identifier exposed to the LLM.
	SwitchVoiceFunctionName = "switch_voice"
)

var (
	// SwitchVoiceFunctionDesc provides the schema description for the LLM.
	SwitchVoiceFunctionDesc = map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        SwitchVoiceFunctionName,
			"description": "切换指定智能体的TTS音色，更新后新的语音风格将用于后续对话",
			"parameters": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"device_id": map[string]interface{}{
						"type":        "string",
						"description": "可选，指定设备ID，默认为当前会话设备",
					},
					"tts_voice_id": map[string]interface{}{
						"type":        "string",
						"description": "要切换到的音色ID",
					},
					"tts_model_id": map[string]interface{}{
						"type":        "string",
						"description": "可选，音色对应的TTS模型ID，未提供时使用音色默认模型",
					},
					"voice_name": map[string]interface{}{
						"type":        "string",
						"description": "可选，用于日志和反馈的音色名称",
					},
				},
				"required": []string{"tts_voice_id"},
			},
		},
	}
)

// SwitchVoiceFunction implements the logic for switching agent voice.
type SwitchVoiceFunction struct {
	config map[string]interface{}
}

// NewSwitchVoiceFunction creates a new switch voice function instance.
func NewSwitchVoiceFunction(args map[string]interface{}) *SwitchVoiceFunction {
	return &SwitchVoiceFunction{config: args}
}

// Execute performs the voice switch operation.
func (f *SwitchVoiceFunction) Execute(ctx context.Context, conn types.Connection, args map[string]interface{}) (*types.ActionResponse, error) {
	payload := make(map[string]interface{})
	for k, v := range f.config {
		payload[k] = v
	}
	for k, v := range args {
		payload[k] = v
	}

	deviceID := ""
	if conn != nil {
		deviceID = conn.GetDeviceID()
	} else {
		return types.NewActionResponse(types.ActionError, "切换音色失败", "缺少device_id参数"), fmt.Errorf("missing device_id")
	}

	voiceID := strings.TrimSpace(fmt.Sprintf("%v", payload["tts_voice_id"]))
	if voiceID == "<nil>" {
		voiceID = ""
	}
	ttsModelID := strings.TrimSpace(fmt.Sprintf("%v", payload["tts_model_id"]))
	if voiceID == "" {
		return types.NewActionResponse(types.ActionError, "切换音色失败", "缺少tts_voice_id参数"), fmt.Errorf("missing tts_voice_id")
	}
	if ttsModelID == "<nil>" {
		ttsModelID = ""
	}
	voiceName := strings.TrimSpace(fmt.Sprintf("%v", payload["voice_name"]))
	if voiceName == "<nil>" {
		voiceName = ""
	}
	ttsVoice := strings.TrimSpace(fmt.Sprintf("%v", payload["tts_voice"]))
	if ttsVoice == "<nil>" {
		ttsVoice = ""
	}

	if deviceID == "" || deviceID == "<nil>" {
		deviceID = strings.TrimSpace(fmt.Sprintf("%v", payload["device_id"]))
	}

	if deviceID == "" || deviceID == "<nil>" {
		return types.NewActionResponse(types.ActionError, "切换音色失败", "缺少device_id参数"), fmt.Errorf("missing device_id")
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
		return types.NewActionResponse(types.ActionError, "切换音色失败", "管理服务未就绪"), fmt.Errorf("manager-api service unavailable")
	}

	resolvedVoiceID := voiceID
	if svc != nil {
		targets := make([]string, 0, 3)
		addTarget := func(value string) {
			value = strings.TrimSpace(value)
			if value == "" {
				return
			}
			for _, existing := range targets {
				if strings.EqualFold(existing, value) {
					return
				}
			}
			targets = append(targets, value)
		}
		addTarget(voiceID)
		addTarget(voiceName)
		addTarget(ttsVoice)

		if len(targets) > 0 {
			options, err := svc.GetAgentVoiceOptions(ctx, deviceID, voiceName)
			if err != nil {
				if logger != nil {
					logger.Warn("获取音色选项失败", slog.String("error", err.Error()))
				}
			} else if options != nil {
				findMatch := func(extractor func(*managerapitypes.VoiceOption) string) *managerapitypes.VoiceOption {
					for _, opt := range options.Voices {
						if opt == nil {
							continue
						}
						token := strings.TrimSpace(extractor(opt))
						if token == "" {
							continue
						}
						for _, target := range targets {
							if strings.EqualFold(token, target) {
								return opt
							}
						}
					}
					return nil
				}

				matched := findMatch(func(opt *managerapitypes.VoiceOption) string { return opt.ID })
				if matched == nil {
					matched = findMatch(func(opt *managerapitypes.VoiceOption) string { return opt.Name })
				}
				if matched == nil {
					matched = findMatch(func(opt *managerapitypes.VoiceOption) string { return opt.TTSVoice })
				}

				if matched != nil {
					resolvedVoiceID = strings.TrimSpace(matched.ID)
					if voiceName == "" {
						voiceName = strings.TrimSpace(matched.Name)
					}
					if ttsVoice == "" {
						ttsVoice = strings.TrimSpace(matched.TTSVoice)
					}
				}
			}
		}
	}

	if resolvedVoiceID == "" {
		return types.NewActionResponse(types.ActionError, "切换音色失败", "无法解析到有效的音色ID"), fmt.Errorf("resolved voice id is empty")
	}

	voiceID = resolvedVoiceID
	request := &managerapitypes.SwitchAgentVoiceRequest{
		TTSVoiceID: voiceID,
	}
	if ttsModelID != "" {
		request.TTSModelID = ttsModelID
	}

	if logger != nil {
		logger.Info("准备切换音色", "device_id", deviceID, "voice_id", voiceID, "tts_model_id", ttsModelID)
	}

	if err := svc.SwitchAgentVoice(ctx, deviceID, request); err != nil {
		if logger != nil {
			logger.Error("切换音色失败", "error", err)
		}
		return types.NewActionResponse(types.ActionError, "切换音色失败", err.Error()), err
	}

	if voiceName == "" {
		voiceName = voiceID
	}

	if logger != nil {
		logger.Info("音色切换成功", "device_id", deviceID, "voice_id", voiceID)
	}

	result := map[string]string{
		"device_id":    deviceID,
		"tts_voice_id": voiceID,
	}
	if ttsModelID != "" {
		result["tts_model_id"] = ttsModelID
	}

	response := fmt.Sprintf("已为设备%s切换音色为%s", deviceID, voiceName)

	return types.NewActionResponse(types.ActionDirectResponse, result, response), nil
}

// GetInfo returns the eino ToolInfo definition.
func (f *SwitchVoiceFunction) GetInfo() *schema.ToolInfo {
	converter := eino_integration.GetGlobalConverter()
	toolInfo, err := converter.ConvertPythonDescToEinoToolInfo(SwitchVoiceFunctionName, SwitchVoiceFunctionDesc)
	if err != nil {
		log.Errorf("Failed to convert tool info for %s: %v", SwitchVoiceFunctionName, err)
		return nil
	}
	return toolInfo
}

// GetType returns the tool type classification.
func (f *SwitchVoiceFunction) GetType() types.ToolType {
	return types.ToolTypeSystemCtl
}

// GetName returns the function name identifier.
func (f *SwitchVoiceFunction) GetName() string {
	return SwitchVoiceFunctionName
}

// GetDescription returns the raw description payload.
func (f *SwitchVoiceFunction) GetDescription() interface{} {
	return SwitchVoiceFunctionDesc
}

// RegisterSwitchVoiceFunction registers the function globally.
func RegisterSwitchVoiceFunction() error {
	function := NewSwitchVoiceFunction(nil)
	return tools.RegisterGlobalFunction(SwitchVoiceFunctionName, function)
}

// init registers the switch voice function on package import.
func init() {
	if err := RegisterSwitchVoiceFunction(); err != nil {
		log.Errorf("Failed to register %s function: %v", SwitchVoiceFunctionName, err)
	}
}
