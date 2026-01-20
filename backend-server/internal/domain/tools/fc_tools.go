package tools

import (
	"context"
	"fmt"
	"log/slog"

	"backend-server/internal/domain/tools/eino_integration"
	"backend-server/internal/domain/tools/registry"
	"backend-server/internal/domain/tools/types"
	log "backend-server/internal/infrastructure/logger"

	"github.com/cloudwego/eino/schema"
)

// FCTools is the main interface for the function calling tools system
type FCTools struct {
	registry       registry.Registry
	deviceRegistry *registry.DeviceTypeRegistry
	adapterManager *eino_integration.ToolAdapterManager
	converter      *eino_integration.SchemaConverter
	logger         *slog.Logger
}

// NewFCTools creates a new FCTools instance
func NewFCTools(conn types.Connection) *FCTools {
	var logger *slog.Logger
	if conn != nil {
		logger = conn.GetLogger()
	}

	return &FCTools{
		registry:       registry.NewFunctionRegistry(logger),
		deviceRegistry: registry.NewDeviceTypeRegistry(),
		adapterManager: eino_integration.NewToolAdapterManager(conn),
		converter:      eino_integration.NewSchemaConverter(),
		logger:         logger,
	}
}

// RegisterFunction registers a function tool
func (fc *FCTools) RegisterFunction(name string, tool types.FunctionTool) error {
	if err := fc.registry.RegisterFunction(name, tool); err != nil {
		return err
	}

	// Also add to adapter manager for eino integration
	fc.adapterManager.AddTool(tool)

	if fc.logger != nil {
		fc.logger.Info("函数工具注册成功", "name", name)
	}

	return nil
}

// RegisterFunctionWithDescription registers a function with Python-style description
func (fc *FCTools) RegisterFunctionWithDescription(name string, description interface{},
	executor func(ctx context.Context, conn types.Connection, args map[string]interface{}) (*types.ActionResponse, error),
	toolType types.ToolType) error {

	// Convert description to eino ToolInfo
	toolInfo, err := fc.converter.ConvertPythonDescToEinoToolInfo(name, description)
	if err != nil {
		return fmt.Errorf("failed to convert description for %s: %v", name, err)
	}

	// Create function tool
	tool := &BaseFunctionTool{
		name:        name,
		description: description,
		toolInfo:    toolInfo,
		toolType:    toolType,
		executor:    executor,
	}

	return fc.RegisterFunction(name, tool)
}

// UnregisterFunction unregisters a function tool
func (fc *FCTools) UnregisterFunction(name string) error {
	if err := fc.registry.UnregisterFunction(name); err != nil {
		return err
	}

	// Also remove from adapter manager
	fc.adapterManager.RemoveTool(name)

	if fc.logger != nil {
		fc.logger.Info("函数工具注销成功", "name", name)
	}

	return nil
}

// GetFunction retrieves a function tool
func (fc *FCTools) GetFunction(name string) (types.FunctionTool, error) {
	return fc.registry.GetFunction(name)
}

// GetAllFunctions returns all registered function tools
func (fc *FCTools) GetAllFunctions() map[string]types.FunctionTool {
	return fc.registry.GetAllFunctions()
}

// GetEinoTools returns all functions as eino ToolInfo for LLM integration
func (fc *FCTools) GetEinoTools() []*schema.ToolInfo {
	return fc.registry.GetEinoTools()
}

// ExecuteFunction executes a function by name with given arguments
func (fc *FCTools) ExecuteFunction(ctx context.Context, conn types.Connection, name string, args map[string]interface{}) (*types.ActionResponse, error) {
	tool, err := fc.GetFunction(name)
	if err != nil {
		return types.NewActionResponse(types.ActionNotFound, nil, fmt.Sprintf("函数 '%s' 未找到", name)), err
	}

	return tool.Execute(ctx, conn, args)
}

// GetDeviceRegistry returns the device registry for IOT device management
func (fc *FCTools) GetDeviceRegistry() *registry.DeviceTypeRegistry {
	return fc.deviceRegistry
}

// GetAdapterManager returns the eino adapter manager
func (fc *FCTools) GetAdapterManager() *eino_integration.ToolAdapterManager {
	return fc.adapterManager
}

// LoadFunctions loads all function implementations
// This is equivalent to Python's auto_import_modules
func (fc *FCTools) LoadFunctions() error {
	// This will be implemented to automatically load all function implementations
	// For now, functions need to be registered manually
	if fc.logger != nil {
		fc.logger.Info("函数加载完成")
	}
	return nil
}

// BaseFunctionTool provides a basic implementation of FunctionTool interface
type BaseFunctionTool struct {
	name        string
	description interface{}
	toolInfo    *schema.ToolInfo
	toolType    types.ToolType
	executor    func(ctx context.Context, conn types.Connection, args map[string]interface{}) (*types.ActionResponse, error)
}

// GetInfo returns the eino ToolInfo
func (b *BaseFunctionTool) GetInfo() *schema.ToolInfo {
	return b.toolInfo
}

// Execute runs the function
func (b *BaseFunctionTool) Execute(ctx context.Context, conn types.Connection, args map[string]interface{}) (*types.ActionResponse, error) {
	if b.executor == nil {
		return types.NewActionResponse(types.ActionError, "函数执行器未设置", nil), fmt.Errorf("executor not set")
	}
	return b.executor(ctx, conn, args)
}

// GetType returns the tool type
func (b *BaseFunctionTool) GetType() types.ToolType {
	return b.toolType
}

// GetName returns the function name
func (b *BaseFunctionTool) GetName() string {
	return b.name
}

// GetDescription returns the function description
func (b *BaseFunctionTool) GetDescription() interface{} {
	return b.description
}

// Global FCTools instance
var globalFCTools *FCTools

// InitGlobalFCTools initializes the global FCTools instance
func InitGlobalFCTools(conn types.Connection) {
	globalFCTools = NewFCTools(conn)
}

// GetGlobalFCTools returns the global FCTools instance
func GetGlobalFCTools() *FCTools {
	if globalFCTools == nil {
		log.Warnf("Global FCTools not initialized, creating with nil connection")
		globalFCTools = NewFCTools(nil)
	}
	return globalFCTools
}

// RegisterGlobalFunction registers a function in the global FCTools instance
func RegisterGlobalFunction(name string, tool types.FunctionTool) error {
	return GetGlobalFCTools().RegisterFunction(name, tool)
}

// RegisterGlobalFunctionWithDescription registers a function with description in the global instance
func RegisterGlobalFunctionWithDescription(name string, description interface{},
	executor func(ctx context.Context, conn types.Connection, args map[string]interface{}) (*types.ActionResponse, error),
	toolType types.ToolType) error {
	return GetGlobalFCTools().RegisterFunctionWithDescription(name, description, executor, toolType)
}
