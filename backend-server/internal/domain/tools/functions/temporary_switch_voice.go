package functions

import (
	"context"
	"fmt"
	"strings"

	"backend-server/internal/adapters/manager"
	managerapitypes "backend-server/internal/adapters/manager/types"
	"backend-server/internal/domain/tools"
	"backend-server/internal/domain/tools/eino_integration"
	"backend-server/internal/domain/tools/types"
	ttsdomain "backend-server/internal/domain/tts"
	log "backend-server/internal/infrastructure/logger"
	"backend-server/internal/app/service"

	"github.com/cloudwego/eino/schema"
)

const (
	// TemporarySwitchVoiceFunctionName exposes the runtime voice switch function to LLMs.
	TemporarySwitchVoiceFunctionName = "temporary_switch_voice"
)

var (
	// TemporarySwitchVoiceFunctionDesc describes the temporary voice switch schema for LLM planners.
	TemporarySwitchVoiceFunctionDesc = map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        TemporarySwitchVoiceFunctionName,
			"description": "临时切换当前会话的TTS音色，仅对本次会话生效，不会同步至管理后台",
			"parameters": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"device_id": map[string]interface{}{
						"type":        "string",
						"description": "可选，指定设备ID，默认使用当前会话设备",
					},
					"tts_voice_id": map[string]interface{}{
						"type":        "string",
						"description": "可选，音色ID，用于从管理后台映射具体音色",
					},
					"tts_voice": map[string]interface{}{
						"type":        "string",
						"description": "可选，直接指定TTS服务所需的音色编码",
					},
					"tts_model_id": map[string]interface{}{
						"type":        "string",
						"description": "可选，关联的TTS模型ID",
					},
					"voice_name": map[string]interface{}{
						"type":        "string",
						"description": "可选，用于反馈给用户的音色名称",
					},
					"voice_params": map[string]interface{}{
						"type":        "object",
						"description": "可选，提供给具体TTS供应商的附加参数，例如参考音频、语速等",
					},
				},
				"required": []string{},
			},
		},
	}
)

// TemporarySwitchVoiceFunction enables session scoped voice switching without persisting changes.
type TemporarySwitchVoiceFunction struct {
	config map[string]interface{}
}

// NewTemporarySwitchVoiceFunction constructs a new function executor.
func NewTemporarySwitchVoiceFunction(args map[string]interface{}) *TemporarySwitchVoiceFunction {
	return &TemporarySwitchVoiceFunction{config: args}
}

// Execute performs the in-session voice update.
func (f *TemporarySwitchVoiceFunction) Execute(ctx context.Context, conn types.Connection, args map[string]interface{}) (*types.ActionResponse, error) {
	payload := make(map[string]interface{})
	for k, v := range f.config {
		payload[k] = v
	}
	for k, v := range args {
		payload[k] = v
	}

	if conn == nil {
		return types.NewActionResponse(types.ActionError, "临时切换音色失败", "连接上下文不可用"), fmt.Errorf("connection is nil")
	}

	deviceID := strings.TrimSpace(conn.GetDeviceID())
	if (deviceID == "" || deviceID == "<nil>") && payload["device_id"] != nil {
		deviceID = strings.TrimSpace(fmt.Sprintf("%v", payload["device_id"]))
	}
	if deviceID == "" || deviceID == "<nil>" {
		return types.NewActionResponse(types.ActionError, "临时切换音色失败", "缺少device_id参数"), fmt.Errorf("missing device_id")
	}

	voiceID := strings.TrimSpace(fmt.Sprintf("%v", payload["tts_voice_id"]))
	if voiceID == "<nil>" {
		voiceID = ""
	}

	ttsVoice := strings.TrimSpace(fmt.Sprintf("%v", payload["tts_voice"]))
	if ttsVoice == "<nil>" {
		ttsVoice = ""
	}

	ttsModelID := strings.TrimSpace(fmt.Sprintf("%v", payload["tts_model_id"]))
	if ttsModelID == "<nil>" {
		ttsModelID = ""
	}

	voiceName := strings.TrimSpace(fmt.Sprintf("%v", payload["voice_name"]))
	if voiceName == "<nil>" {
		voiceName = ""
	}

	var voiceParams map[string]interface{}
	if rawParams, ok := payload["voice_params"]; ok {
		if typed, ok := rawParams.(map[string]interface{}); ok {
			voiceParams = make(map[string]interface{}, len(typed))
			for k, v := range typed {
				voiceParams[k] = v
			}
		}
	}

	logger := conn.GetLogger()

	if ttsVoice == "" && voiceID == "" {
		if logger != nil {
			logger.Error("临时切换音色失败", "reason", "voice_id和tts_voice均为空")
		}
		return types.NewActionResponse(types.ActionError, "临时切换音色失败", "请提供tts_voice或tts_voice_id"), fmt.Errorf("missing voice identifiers")
	}

	if ttsVoice == "" && voiceID != "" {
		var svc manager_api.ManagerAPIService
		if conn != nil {
			svc = conn.GetManagerAPIService()
		}
		if svc == nil {
			svc = service.DefaultRegistry().ManagerAPIService()
		}
		if svc == nil {
			return types.NewActionResponse(types.ActionError, "临时切换音色失败", "管理服务未就绪，无法解析音色ID"), fmt.Errorf("manager-api service unavailable")
		}
		options, err := svc.GetAgentVoiceOptions(ctx, deviceID, "")
		if err != nil {
			return types.NewActionResponse(types.ActionError, "临时切换音色失败", err.Error()), err
		}
		var matched *managerapitypes.VoiceOption
		if options != nil {
			normalizedID := strings.TrimSpace(voiceID)
			targets := make([]string, 0, 2)
			if normalizedID != "" {
				targets = append(targets, normalizedID)
			}
			if trimmedName := strings.TrimSpace(voiceName); trimmedName != "" {
				duplicate := false
				for _, val := range targets {
					if strings.EqualFold(val, trimmedName) {
						duplicate = true
						break
					}
				}
				if !duplicate {
					targets = append(targets, trimmedName)
				}
			}

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

			matched = findMatch(func(opt *managerapitypes.VoiceOption) string { return opt.ID })
			if matched == nil {
				matched = findMatch(func(opt *managerapitypes.VoiceOption) string { return opt.Name })
			}
		}

		if matched == nil {
			return types.NewActionResponse(types.ActionError, "临时切换音色失败", "未在管理后台找到匹配的音色ID"), fmt.Errorf("voice id %s not found", voiceID)
		}
		if voiceID == "" || !strings.EqualFold(strings.TrimSpace(matched.ID), voiceID) {
			voiceID = strings.TrimSpace(matched.ID)
		}
		ttsVoice = strings.TrimSpace(matched.TTSVoice)
		if voiceName == "" {
			voiceName = strings.TrimSpace(matched.Name)
		}
	}

	if ttsVoice == "" {
		return types.NewActionResponse(types.ActionError, "临时切换音色失败", "无法解析到具体TTS音色"), fmt.Errorf("resolved voice token is empty")
	}

	if voiceParams == nil {
		voiceParams = make(map[string]interface{})
	}
	if _, exists := voiceParams["voice"]; !exists {
		voiceParams["voice"] = ttsVoice
	}

	options := &ttsdomain.ChangeVoiceOptions{
		VoiceID:   voiceID,
		Voice:     ttsVoice,
		VoiceName: voiceName,
		ModelID:   ttsModelID,
		Params:    voiceParams,
	}

	if conn == nil {
		return types.NewActionResponse(types.ActionError, "临时切换音色失败", "无法切换音色"), fmt.Errorf("cannot change voice")
	}

	if err := conn.TemporaryChangeVoice(ctx, options); err != nil {
		if logger != nil {
			logger.Error("临时切换音色失败", "error", err)
		}
		return types.NewActionResponse(types.ActionError, "临时切换音色失败", err.Error()), err
	}

	displayName := voiceName
	if displayName == "" {
		displayName = ttsVoice
	}
	if displayName == "" {
		displayName = voiceID
	}

	if logger != nil {
		logger.Info("临时切换音色成功", "device_id", deviceID, "voice", ttsVoice)
	}

	data := map[string]interface{}{
		"device_id":  deviceID,
		"tts_voice":  ttsVoice,
		"voice_name": displayName,
	}
	if voiceID != "" {
		data["tts_voice_id"] = voiceID
	}
	if ttsModelID != "" {
		data["tts_model_id"] = ttsModelID
	}
	if len(voiceParams) > 0 {
		data["voice_params"] = voiceParams
	}

	message := fmt.Sprintf("已临时切换音色为%s，本次会话内生效。", displayName)
	return types.NewActionResponse(types.ActionDirectResponse, data, message), nil
}

// GetInfo returns the eino tool metadata.
func (f *TemporarySwitchVoiceFunction) GetInfo() *schema.ToolInfo {
	converter := eino_integration.GetGlobalConverter()
	toolInfo, err := converter.ConvertPythonDescToEinoToolInfo(TemporarySwitchVoiceFunctionName, TemporarySwitchVoiceFunctionDesc)
	if err != nil {
		log.Errorf("Failed to convert tool info for %s: %v", TemporarySwitchVoiceFunctionName, err)
		return nil
	}
	return toolInfo
}

// GetType identifies the function category.
func (f *TemporarySwitchVoiceFunction) GetType() types.ToolType {
	return types.ToolTypeSystemCtl
}

// GetName returns the exported function name.
func (f *TemporarySwitchVoiceFunction) GetName() string {
	return TemporarySwitchVoiceFunctionName
}

// GetDescription exposes the raw schema definition.
func (f *TemporarySwitchVoiceFunction) GetDescription() interface{} {
	return TemporarySwitchVoiceFunctionDesc
}

// RegisterTemporarySwitchVoiceFunction registers the function globally.
func RegisterTemporarySwitchVoiceFunction() error {
	function := NewTemporarySwitchVoiceFunction(nil)
	return tools.RegisterGlobalFunction(TemporarySwitchVoiceFunctionName, function)
}

// init ensures the function is available as soon as package loads.
func init() {
	if err := RegisterTemporarySwitchVoiceFunction(); err != nil {
		log.Errorf("Failed to register %s function: %v", TemporarySwitchVoiceFunctionName, err)
	}
}
