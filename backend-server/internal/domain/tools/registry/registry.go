package registry

import (
	"fmt"
	"log/slog"
	"sync"

	"backend-server/internal/domain/tools/types"

	"github.com/cloudwego/eino/schema"
)

// Registry manages function registration and retrieval
type Registry interface {
	RegisterFunction(name string, tool types.FunctionTool) error
	RegisterFunctionItem(name string, item *types.FunctionItem) error
	UnregisterFunction(name string) error
	GetFunction(name string) (types.FunctionTool, error)
	GetFunctionItem(name string) (*types.FunctionItem, error)
	GetAllFunctions() map[string]types.FunctionTool
	GetAllFunctionItems() map[string]*types.FunctionItem
	GetEinoTools() []*schema.ToolInfo
	GetAllFunctionDescriptions() []interface{}
}

// FunctionRegistry implements the Registry interface
type FunctionRegistry struct {
	mu               sync.RWMutex
	functionRegistry map[string]*types.FunctionItem
	logger           *slog.Logger
}

// NewFunctionRegistry creates a new function registry
func NewFunctionRegistry(logger *slog.Logger) *FunctionRegistry {
	if logger == nil {
		// Use a default logger if none provided
		logger = slog.Default()
	}

	return &FunctionRegistry{
		functionRegistry: make(map[string]*types.FunctionItem),
		logger:           logger,
	}
}

// RegisterFunction registers a function tool
func (r *FunctionRegistry) RegisterFunction(name string, tool types.FunctionTool) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if tool == nil {
		return fmt.Errorf("function tool cannot be nil")
	}

	item := types.NewFunctionItem(name, tool.GetDescription(), tool, tool.GetType())
	r.functionRegistry[name] = item

	r.logger.Debug("函数注册成功", "name", name)
	return nil
}

// RegisterFunctionItem registers a function item directly
func (r *FunctionRegistry) RegisterFunctionItem(name string, item *types.FunctionItem) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if item == nil {
		return fmt.Errorf("function item cannot be nil")
	}

	r.functionRegistry[name] = item
	r.logger.Debug("函数直接注册成功", "name", name)
	return nil
}

// UnregisterFunction unregisters a function
func (r *FunctionRegistry) UnregisterFunction(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.functionRegistry[name]; !exists {
		r.logger.Error("函数未找到", "name", name)
		return fmt.Errorf("function '%s' not found", name)
	}

	delete(r.functionRegistry, name)
	r.logger.Info("函数注销成功", "name", name)
	return nil
}

// GetFunction retrieves a function tool by name
func (r *FunctionRegistry) GetFunction(name string) (types.FunctionTool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	item, exists := r.functionRegistry[name]
	if !exists {
		return nil, fmt.Errorf("function '%s' not found", name)
	}

	return item.Function, nil
}

// GetFunctionItem retrieves a function item by name
func (r *FunctionRegistry) GetFunctionItem(name string) (*types.FunctionItem, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	item, exists := r.functionRegistry[name]
	if !exists {
		return nil, fmt.Errorf("function '%s' not found", name)
	}

	return item, nil
}

// GetAllFunctions returns all registered function tools
func (r *FunctionRegistry) GetAllFunctions() map[string]types.FunctionTool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[string]types.FunctionTool)
	for name, item := range r.functionRegistry {
		result[name] = item.Function
	}

	return result
}

// GetAllFunctionItems returns all registered function items
func (r *FunctionRegistry) GetAllFunctionItems() map[string]*types.FunctionItem {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[string]*types.FunctionItem)
	for name, item := range r.functionRegistry {
		result[name] = item
	}

	return result
}

// GetEinoTools returns all functions as eino ToolInfo for LLM integration
func (r *FunctionRegistry) GetEinoTools() []*schema.ToolInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var tools []*schema.ToolInfo
	for _, item := range r.functionRegistry {
		if toolInfo := item.GetToolInfo(); toolInfo != nil {
			tools = append(tools, toolInfo)
		}
	}

	return tools
}

// GetAllFunctionDescriptions returns all function descriptions
func (r *FunctionRegistry) GetAllFunctionDescriptions() []interface{} {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var descriptions []interface{}
	for _, item := range r.functionRegistry {
		descriptions = append(descriptions, item.Description)
	}

	return descriptions
}

// defaultLogger provides a basic logger implementation

// Global registry instance (equivalent to Python's all_function_registry)
var globalRegistry = NewFunctionRegistry(nil)

// RegisterGlobalFunction registers a function in the global registry
func RegisterGlobalFunction(name string, tool types.FunctionTool) error {
	return globalRegistry.RegisterFunction(name, tool)
}

// GetGlobalFunction retrieves a function from the global registry
func GetGlobalFunction(name string) (types.FunctionTool, error) {
	return globalRegistry.GetFunction(name)
}

// GetGlobalRegistry returns the global registry instance
func GetGlobalRegistry() Registry {
	return globalRegistry
}
