package eino_integration

import (
	"encoding/json"
	"fmt"

	schemautil "backend-server/internal/shared/schemautil"

	"github.com/cloudwego/eino/schema"
	"github.com/getkin/kin-openapi/openapi3"
)

// SchemaConverter provides utilities to convert between different schema formats
type SchemaConverter struct{}

// NewSchemaConverter creates a new schema converter
func NewSchemaConverter() *SchemaConverter {
	return &SchemaConverter{}
}

// ConvertPythonDescToEinoToolInfo converts Python function description to eino ToolInfo
// This handles the conversion from Python's function description format to eino's schema
func (c *SchemaConverter) ConvertPythonDescToEinoToolInfo(name string, description interface{}) (*schema.ToolInfo, error) {
	if description == nil {
		return nil, fmt.Errorf("description cannot be nil")
	}

	// Try to parse as map first (most common case)
	var descMap map[string]interface{}

	switch desc := description.(type) {
	case map[string]interface{}:
		descMap = desc
	case string:
		// Try to parse JSON string
		if err := json.Unmarshal([]byte(desc), &descMap); err != nil {
			// If not JSON, treat as simple description
			return &schema.ToolInfo{
				Name:        name,
				Desc:        desc,
				ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{}),
			}, nil
		}
	default:
		// Convert to JSON and back to get map representation
		jsonBytes, err := json.Marshal(description)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal description: %v", err)
		}
		if err := json.Unmarshal(jsonBytes, &descMap); err != nil {
			return nil, fmt.Errorf("failed to unmarshal description: %v", err)
		}
	}

	// Extract function information from Python format
	functionInfo, ok := descMap["function"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid function description format")
	}

	funcName, _ := functionInfo["name"].(string)
	if funcName == "" {
		funcName = name
	}

	funcDesc, _ := functionInfo["description"].(string)

	// Convert parameters
	paramsOneOf, err := c.convertParameters(functionInfo["parameters"])
	if err != nil {
		return nil, fmt.Errorf("failed to convert parameters: %v", err)
	}

	return &schema.ToolInfo{
		Name:        funcName,
		Desc:        funcDesc,
		ParamsOneOf: paramsOneOf,
	}, nil
}

// convertParameters converts Python parameters to eino ParamsOneOf
func (c *SchemaConverter) convertParameters(params interface{}) (*schema.ParamsOneOf, error) {
	if params == nil {
		return schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{}), nil
	}

	paramsMap, ok := params.(map[string]interface{})
	if !ok {
		return schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{}), nil
	}

	// Convert to OpenAPI 3 schema
	openAPISchema, err := c.convertToOpenAPI3Schema(paramsMap)
	if err != nil {
		return nil, err
	}

	jsonSchema, err := schemautil.OpenAPIToJSONSchema(openAPISchema)
	if err != nil {
		return nil, err
	}

	return schema.NewParamsOneOfByJSONSchema(jsonSchema), nil
}

// convertToOpenAPI3Schema converts parameter map to OpenAPI 3 schema
func (c *SchemaConverter) convertToOpenAPI3Schema(paramsMap map[string]interface{}) (*openapi3.Schema, error) {
	schema := &openapi3.Schema{
		Type: "object",
	}

	// Extract type
	if schemaType, ok := paramsMap["type"].(string); ok {
		schema.Type = schemaType
	}

	// Extract properties
	if properties, ok := paramsMap["properties"].(map[string]interface{}); ok {
		schema.Properties = make(openapi3.Schemas)

		for propName, propDef := range properties {
			propSchema, err := c.convertPropertyToSchema(propDef)
			if err != nil {
				return nil, fmt.Errorf("failed to convert property %s: %v", propName, err)
			}
			schema.Properties[propName] = &openapi3.SchemaRef{Value: propSchema}
		}
	}

	// Extract required fields
	if required, ok := paramsMap["required"].([]interface{}); ok {
		for _, req := range required {
			if reqStr, ok := req.(string); ok {
				schema.Required = append(schema.Required, reqStr)
			}
		}
	}

	return schema, nil
}

// convertPropertyToSchema converts a property definition to OpenAPI schema
func (c *SchemaConverter) convertPropertyToSchema(propDef interface{}) (*openapi3.Schema, error) {
	propMap, ok := propDef.(map[string]interface{})
	if !ok {
		return &openapi3.Schema{Type: "string"}, nil
	}

	schema := &openapi3.Schema{}

	// Extract type
	if propType, ok := propMap["type"].(string); ok {
		schema.Type = propType
	} else {
		schema.Type = "string" // default
	}

	// Extract description
	if desc, ok := propMap["description"].(string); ok {
		schema.Description = desc
	}

	// Extract enum values
	if enum, ok := propMap["enum"].([]interface{}); ok {
		for _, val := range enum {
			schema.Enum = append(schema.Enum, val)
		}
	}

	// Extract format
	if format, ok := propMap["format"].(string); ok {
		schema.Format = format
	}

	// Extract default value
	if defaultVal, exists := propMap["default"]; exists {
		schema.Default = defaultVal
	}

	// Handle array type
	if schema.Type == "array" {
		if items, ok := propMap["items"].(map[string]interface{}); ok {
			itemSchema, err := c.convertPropertyToSchema(items)
			if err != nil {
				return nil, fmt.Errorf("failed to convert array items: %v", err)
			}
			schema.Items = &openapi3.SchemaRef{Value: itemSchema}
		}
	}

	// Handle object type
	if schema.Type == "object" {
		if properties, ok := propMap["properties"].(map[string]interface{}); ok {
			schema.Properties = make(openapi3.Schemas)
			for propName, propDef := range properties {
				propSchema, err := c.convertPropertyToSchema(propDef)
				if err != nil {
					return nil, fmt.Errorf("failed to convert nested property %s: %v", propName, err)
				}
				schema.Properties[propName] = &openapi3.SchemaRef{Value: propSchema}
			}
		}

		if required, ok := propMap["required"].([]interface{}); ok {
			for _, req := range required {
				if reqStr, ok := req.(string); ok {
					schema.Required = append(schema.Required, reqStr)
				}
			}
		}
	}

	return schema, nil
}

// ValidateToolInfo validates that a ToolInfo is properly formatted for eino
func (c *SchemaConverter) ValidateToolInfo(toolInfo *schema.ToolInfo) error {
	if toolInfo == nil {
		return fmt.Errorf("tool info cannot be nil")
	}

	if toolInfo.Name == "" {
		return fmt.Errorf("tool name cannot be empty")
	}

	if toolInfo.ParamsOneOf == nil {
		return fmt.Errorf("params cannot be nil")
	}

	return nil
}

// Global converter instance
var globalConverter = NewSchemaConverter()

// GetGlobalConverter returns the global schema converter
func GetGlobalConverter() *SchemaConverter {
	return globalConverter
}
