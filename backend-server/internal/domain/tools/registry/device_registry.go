package registry

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"backend-server/internal/domain/tools/types"
)

// DeviceDescriptor represents a device capability description
type DeviceDescriptor struct {
	Name       string                 `json:"name"`
	Properties map[string]interface{} `json:"properties"`
	Methods    map[string]interface{} `json:"methods"`
}

// DeviceTypeRegistry manages IOT device types and their functions
// Equivalent to Python's DeviceTypeRegistry
type DeviceTypeRegistry struct {
	mu            sync.RWMutex
	typeFunctions map[string]map[string]*types.FunctionItem // type_signature -> {func_name: FunctionItem}
}

// NewDeviceTypeRegistry creates a new device type registry
func NewDeviceTypeRegistry() *DeviceTypeRegistry {
	return &DeviceTypeRegistry{
		typeFunctions: make(map[string]map[string]*types.FunctionItem),
	}
}

// GenerateDeviceTypeID generates a unique type ID from device descriptor
// Equivalent to Python's generate_device_type_id
func (r *DeviceTypeRegistry) GenerateDeviceTypeID(descriptor *DeviceDescriptor) string {
	if descriptor == nil {
		return ""
	}

	// Sort properties and methods for consistent type signature
	var properties []string
	for prop := range descriptor.Properties {
		properties = append(properties, prop)
	}
	sort.Strings(properties)

	var methods []string
	for method := range descriptor.Methods {
		methods = append(methods, method)
	}
	sort.Strings(methods)

	// Create type signature: name:properties:methods
	typeSignature := fmt.Sprintf("%s:%s:%s",
		descriptor.Name,
		strings.Join(properties, ","),
		strings.Join(methods, ","),
	)

	return typeSignature
}

// GetDeviceFunctions returns all functions for a device type
// Equivalent to Python's get_device_functions
func (r *DeviceTypeRegistry) GetDeviceFunctions(typeID string) map[string]*types.FunctionItem {
	r.mu.RLock()
	defer r.mu.RUnlock()

	functions, exists := r.typeFunctions[typeID]
	if !exists {
		return make(map[string]*types.FunctionItem)
	}

	// Return a copy to prevent external modification
	result := make(map[string]*types.FunctionItem)
	for name, function := range functions {
		result[name] = function
	}

	return result
}

// RegisterDeviceType registers a device type and its functions
// Equivalent to Python's register_device_type
func (r *DeviceTypeRegistry) RegisterDeviceType(typeID string, functions map[string]*types.FunctionItem) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if typeID == "" {
		return fmt.Errorf("device type ID cannot be empty")
	}

	if functions == nil {
		functions = make(map[string]*types.FunctionItem)
	}

	// Only register if not already exists
	if _, exists := r.typeFunctions[typeID]; !exists {
		r.typeFunctions[typeID] = functions
	}

	return nil
}

// UnregisterDeviceType removes a device type and all its functions
func (r *DeviceTypeRegistry) UnregisterDeviceType(typeID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.typeFunctions[typeID]; !exists {
		return fmt.Errorf("device type '%s' not found", typeID)
	}

	delete(r.typeFunctions, typeID)
	return nil
}

// AddDeviceFunction adds a function to an existing device type
func (r *DeviceTypeRegistry) AddDeviceFunction(typeID string, functionName string, function *types.FunctionItem) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	functions, exists := r.typeFunctions[typeID]
	if !exists {
		return fmt.Errorf("device type '%s' not found", typeID)
	}

	if function == nil {
		return fmt.Errorf("function item cannot be nil")
	}

	functions[functionName] = function
	return nil
}

// RemoveDeviceFunction removes a function from a device type
func (r *DeviceTypeRegistry) RemoveDeviceFunction(typeID string, functionName string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	functions, exists := r.typeFunctions[typeID]
	if !exists {
		return fmt.Errorf("device type '%s' not found", typeID)
	}

	if _, exists := functions[functionName]; !exists {
		return fmt.Errorf("function '%s' not found in device type '%s'", functionName, typeID)
	}

	delete(functions, functionName)
	return nil
}

// GetAllDeviceTypes returns all registered device type IDs
func (r *DeviceTypeRegistry) GetAllDeviceTypes() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var types []string
	for typeID := range r.typeFunctions {
		types = append(types, typeID)
	}

	sort.Strings(types)
	return types
}

// GetDeviceFunction retrieves a specific function from a device type
func (r *DeviceTypeRegistry) GetDeviceFunction(typeID string, functionName string) (*types.FunctionItem, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	functions, exists := r.typeFunctions[typeID]
	if !exists {
		return nil, fmt.Errorf("device type '%s' not found", typeID)
	}

	function, exists := functions[functionName]
	if !exists {
		return nil, fmt.Errorf("function '%s' not found in device type '%s'", functionName, typeID)
	}

	return function, nil
}

// GetTotalFunctionCount returns the total number of functions across all device types
func (r *DeviceTypeRegistry) GetTotalFunctionCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	total := 0
	for _, functions := range r.typeFunctions {
		total += len(functions)
	}

	return total
}

// Global device registry instance
var globalDeviceRegistry = NewDeviceTypeRegistry()

// GetGlobalDeviceRegistry returns the global device registry instance
func GetGlobalDeviceRegistry() *DeviceTypeRegistry {
	return globalDeviceRegistry
}
