package registry

import (
	"fmt"
	"log/slog"
	"reflect"
	"runtime"
	"strings"
)

// FunctionLoader provides functionality to automatically load and register functions
// This is equivalent to Python's auto_import_modules functionality
type FunctionLoader struct {
	registry *slog.Logger
	logger   *slog.Logger
}

// NewFunctionLoader creates a new function loader
func NewFunctionLoader(logger *slog.Logger) *FunctionLoader {
	return &FunctionLoader{
		logger: logger,
	}
}

// LoadFunction loads a single function by calling its registration function
func (l *FunctionLoader) LoadFunction(registerFunc func() error, functionName string) error {
	if registerFunc == nil {
		return fmt.Errorf("register function cannot be nil")
	}

	if err := registerFunc(); err != nil {
		if l.logger != nil {
			l.logger.Error("failed to load function", "function", functionName, "error", err)
		}
		return fmt.Errorf("failed to load function %s: %v", functionName, err)
	}

	if l.logger != nil {
		l.logger.Debug("function loaded", "function", functionName)
	}

	return nil
}

// LoadFunctions loads multiple functions
func (l *FunctionLoader) LoadFunctions(functions map[string]func() error) error {
	var errors []string

	for name, registerFunc := range functions {
		if err := l.LoadFunction(registerFunc, name); err != nil {
			errors = append(errors, err.Error())
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("failed to load some functions: %s", strings.Join(errors, "; "))
	}

	if l.logger != nil {
		l.logger.Info("all functions loaded", "count", len(functions))
	}

	return nil
}

// GetFunctionName extracts function name from a function pointer using reflection
func (l *FunctionLoader) GetFunctionName(fn interface{}) string {
	if fn == nil {
		return "unknown"
	}

	// Get the function name using reflection
	funcValue := reflect.ValueOf(fn)
	if funcValue.Kind() != reflect.Func {
		return "not_a_function"
	}

	// Get the full function name including package path
	fullName := runtime.FuncForPC(funcValue.Pointer()).Name()

	// Extract just the function name (last part after the last dot)
	parts := strings.Split(fullName, ".")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}

	return "unknown"
}

// AutoLoadFunctions automatically loads functions from a list of registration functions
// This provides similar functionality to Python's auto_import_modules
func (l *FunctionLoader) AutoLoadFunctions(registerFunctions []func() error) error {
	functionMap := make(map[string]func() error)

	for _, registerFunc := range registerFunctions {
		name := l.GetFunctionName(registerFunc)
		functionMap[name] = registerFunc
	}

	return l.LoadFunctions(functionMap)
}

// ValidateFunction validates that a function is properly registered
func (l *FunctionLoader) ValidateFunction(registry Registry, functionName string) error {
	if registry == nil {
		return fmt.Errorf("registry cannot be nil")
	}

	// Check if function exists in registry
	_, err := registry.GetFunction(functionName)
	if err != nil {
		return fmt.Errorf("function %s not found in registry: %v", functionName, err)
	}

	// Check if function has valid tool info
	item, err := registry.GetFunctionItem(functionName)
	if err != nil {
		return fmt.Errorf("function item %s not found: %v", functionName, err)
	}

	if item.Function == nil {
		return fmt.Errorf("function %s has nil function tool", functionName)
	}

	toolInfo := item.Function.GetInfo()
	if toolInfo == nil {
		return fmt.Errorf("function %s has nil tool info", functionName)
	}

	if toolInfo.Name == "" {
		return fmt.Errorf("function %s has empty tool name", functionName)
	}

	if l.logger != nil {
		l.logger.Debug("function validation passed", "function", functionName)
	}

	return nil
}

// ValidateAllFunctions validates all functions in a registry
func (l *FunctionLoader) ValidateAllFunctions(registry Registry) error {
	if registry == nil {
		return fmt.Errorf("registry cannot be nil")
	}

	allFunctions := registry.GetAllFunctions()
	var errors []string

	for name := range allFunctions {
		if err := l.ValidateFunction(registry, name); err != nil {
			errors = append(errors, err.Error())
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("validation failed for some functions: %s", strings.Join(errors, "; "))
	}

	if l.logger != nil {
		l.logger.Info("all functions validated", "count", len(allFunctions))
	}

	return nil
}

// LoadAndValidateFunctions loads functions and then validates them
func (l *FunctionLoader) LoadAndValidateFunctions(registry Registry, functions map[string]func() error) error {
	// Load functions first
	if err := l.LoadFunctions(functions); err != nil {
		return fmt.Errorf("failed to load functions: %v", err)
	}

	// Then validate them
	if err := l.ValidateAllFunctions(registry); err != nil {
		return fmt.Errorf("failed to validate functions: %v", err)
	}

	return nil
}

// GetLoadedFunctionStats returns statistics about loaded functions
func (l *FunctionLoader) GetLoadedFunctionStats(registry Registry) map[string]interface{} {
	if registry == nil {
		return map[string]interface{}{
			"error": "registry is nil",
		}
	}

	allFunctions := registry.GetAllFunctions()
	allItems := registry.GetAllFunctionItems()

	// Count by tool type
	typeCounts := make(map[string]int)
	for _, item := range allItems {
		if item.Function != nil {
			toolType := item.Function.GetType().String()
			typeCounts[toolType]++
		}
	}

	return map[string]interface{}{
		"total_functions":   len(allFunctions),
		"total_items":       len(allItems),
		"functions_by_type": typeCounts,
		"eino_tools_count":  len(registry.GetEinoTools()),
	}
}

// Global loader instance
var globalLoader = NewFunctionLoader(nil)

// GetGlobalLoader returns the global function loader
func GetGlobalLoader() *FunctionLoader {
	return globalLoader
}
