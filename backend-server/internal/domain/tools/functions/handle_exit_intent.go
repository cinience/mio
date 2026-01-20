package functions

import (
	"backend-server/internal/domain/tools/eino_integration"
	"context"
	"log/slog"

	"backend-server/internal/domain/tools"
	"backend-server/internal/domain/tools/types"
	log "backend-server/internal/infrastructure/logger"

	"github.com/cloudwego/eino/schema"
)

const (
	HandleExitIntentFunctionName = "handle_exit_intent"
)

// HandleExitIntentFunctionDesc defines the function description for LLM
var HandleExitIntentFunctionDesc = map[string]interface{}{
	"type": "function",
	"function": map[string]interface{}{
		"name":        HandleExitIntentFunctionName,
		"description": "仅在用户明确表达'再见'、'关机'、'退出'等意图时调用。严禁在无法回答问题或听不懂用户输入时调用此工具",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"say_goodbye": map[string]interface{}{
					"type":        "string",
					"description": "和用户友好结束对话的告别语",
				},
			},
			"required": []string{"say_goodbye"},
		},
	},
}

// HandleExitIntentFunction implements the exit intent handling function
type HandleExitIntentFunction struct{}

// NewHandleExitIntentFunction creates a new handle exit intent function
func NewHandleExitIntentFunction(args map[string]interface{}) *HandleExitIntentFunction {
	return &HandleExitIntentFunction{}
}

// Execute implements the function execution
func (f *HandleExitIntentFunction) Execute(ctx context.Context, conn types.Connection, args map[string]interface{}) (*types.ActionResponse, error) {
	// Use connection logger if available, otherwise use default logger
	var logger *slog.Logger
	deviceID := ""
	if conn != nil {
		logger = conn.GetLogger()
		deviceID = conn.GetDeviceID()
	}
	if logger != nil {
		logger.Info("handle_exit_intent invoked", "device_id", deviceID, "args", args)
	} else {
		log.Infof("handle_exit_intent invoked: device=%s args=%v", deviceID, args)
	}

	// Extract say_goodbye parameter
	var sayGoodbye string
	if goodbye, ok := args["say_goodbye"].(string); ok {
		sayGoodbye = goodbye
	} else {
		sayGoodbye = "再见！"
	}

	// Set close after chat flag if connection supports it
	if conn != nil {
		conn.SetCloseAfterChat(true, sayGoodbye)
		if logger != nil {
			logger.Info("退出意图已处理", "farewell", sayGoodbye)
		}
	}

	return types.NewActionResponse(
		types.ActionDirectResponse,
		"退出意图已处理",
		sayGoodbye,
	), nil
}

// GetInfo returns the eino ToolInfo
func (f *HandleExitIntentFunction) GetInfo() *schema.ToolInfo {
	converter := eino_integration.GetGlobalConverter()
	toolInfo, err := converter.ConvertPythonDescToEinoToolInfo(HandleExitIntentFunctionName, HandleExitIntentFunctionDesc)
	if err != nil {
		log.Errorf("Failed to convert tool info for %s: %v", HandleExitIntentFunctionName, err)
		return nil
	}
	return toolInfo
}

// GetType returns the tool type
func (f *HandleExitIntentFunction) GetType() types.ToolType {
	return types.ToolTypeSystemCtl
}

// GetName returns the function name
func (f *HandleExitIntentFunction) GetName() string {
	return HandleExitIntentFunctionName
}

// GetDescription returns the function description
func (f *HandleExitIntentFunction) GetDescription() interface{} {
	return HandleExitIntentFunctionDesc
}

// RegisterHandleExitIntentFunction registers the handle exit intent function
func RegisterHandleExitIntentFunction() error {
	function := NewHandleExitIntentFunction(nil)
	return tools.RegisterGlobalFunction(HandleExitIntentFunctionName, function)
}

// init automatically registers the function when the package is imported
func init() {
	if err := RegisterHandleExitIntentFunction(); err != nil {
		log.Errorf("Failed to register %s function: %v", HandleExitIntentFunctionName, err)
	}
}
