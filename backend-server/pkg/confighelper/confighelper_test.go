package confighelper

import (
	"testing"
	"time"
)

func TestHelper_GetString(t *testing.T) {
	tests := []struct {
		name         string
		config       map[string]interface{}
		key          string
		defaultValue string
		expected     string
	}{
		{
			name:         "existing string",
			config:       map[string]interface{}{"key": "value"},
			key:          "key",
			defaultValue: "default",
			expected:     "value",
		},
		{
			name:         "non-existing key with default",
			config:       map[string]interface{}{},
			key:          "key",
			defaultValue: "default",
			expected:     "default",
		},
		{
			name:         "int to string conversion",
			config:       map[string]interface{}{"key": 123},
			key:          "key",
			defaultValue: "default",
			expected:     "123",
		},
		{
			name:         "float to string conversion",
			config:       map[string]interface{}{"key": 123.45},
			key:          "key",
			defaultValue: "default",
			expected:     "123.45",
		},
		{
			name:         "bool to string conversion",
			config:       map[string]interface{}{"key": true},
			key:          "key",
			defaultValue: "default",
			expected:     "true",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			helper := New(tt.config)
			result := helper.GetString(tt.key, tt.defaultValue)
			if result != tt.expected {
				t.Errorf("GetString() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestHelper_GetInt(t *testing.T) {
	tests := []struct {
		name         string
		config       map[string]interface{}
		key          string
		defaultValue int
		expected     int
	}{
		{
			name:         "existing int",
			config:       map[string]interface{}{"key": 123},
			key:          "key",
			defaultValue: 0,
			expected:     123,
		},
		{
			name:         "float to int conversion",
			config:       map[string]interface{}{"key": 123.0},
			key:          "key",
			defaultValue: 0,
			expected:     123,
		},
		{
			name:         "string to int conversion",
			config:       map[string]interface{}{"key": "456"},
			key:          "key",
			defaultValue: 0,
			expected:     456,
		},
		{
			name:         "non-existing key with default",
			config:       map[string]interface{}{},
			key:          "key",
			defaultValue: 999,
			expected:     999,
		},
		{
			name:         "invalid string returns default",
			config:       map[string]interface{}{"key": "invalid"},
			key:          "key",
			defaultValue: 999,
			expected:     999,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			helper := New(tt.config)
			result := helper.GetInt(tt.key, tt.defaultValue)
			if result != tt.expected {
				t.Errorf("GetInt() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestHelper_GetBool(t *testing.T) {
	tests := []struct {
		name         string
		config       map[string]interface{}
		key          string
		defaultValue bool
		expected     bool
	}{
		{
			name:         "existing bool true",
			config:       map[string]interface{}{"key": true},
			key:          "key",
			defaultValue: false,
			expected:     true,
		},
		{
			name:         "existing bool false",
			config:       map[string]interface{}{"key": false},
			key:          "key",
			defaultValue: true,
			expected:     false,
		},
		{
			name:         "string true conversion",
			config:       map[string]interface{}{"key": "true"},
			key:          "key",
			defaultValue: false,
			expected:     true,
		},
		{
			name:         "string false conversion",
			config:       map[string]interface{}{"key": "false"},
			key:          "key",
			defaultValue: true,
			expected:     false,
		},
		{
			name:         "non-zero int to true",
			config:       map[string]interface{}{"key": 1},
			key:          "key",
			defaultValue: false,
			expected:     true,
		},
		{
			name:         "zero int to false",
			config:       map[string]interface{}{"key": 0},
			key:          "key",
			defaultValue: true,
			expected:     false,
		},
		{
			name:         "non-existing key with default",
			config:       map[string]interface{}{},
			key:          "key",
			defaultValue: true,
			expected:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			helper := New(tt.config)
			result := helper.GetBool(tt.key, tt.defaultValue)
			if result != tt.expected {
				t.Errorf("GetBool() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestHelper_GetStringSlice(t *testing.T) {
	tests := []struct {
		name         string
		config       map[string]interface{}
		key          string
		defaultValue []string
		expected     []string
	}{
		{
			name:         "existing string slice",
			config:       map[string]interface{}{"key": []string{"a", "b", "c"}},
			key:          "key",
			defaultValue: []string{"default"},
			expected:     []string{"a", "b", "c"},
		},
		{
			name:         "interface slice conversion",
			config:       map[string]interface{}{"key": []interface{}{"a", 1, true}},
			key:          "key",
			defaultValue: []string{"default"},
			expected:     []string{"a", "1", "true"},
		},
		{
			name:         "single string to slice",
			config:       map[string]interface{}{"key": "single"},
			key:          "key",
			defaultValue: []string{"default"},
			expected:     []string{"single"},
		},
		{
			name:         "non-existing key with default",
			config:       map[string]interface{}{},
			key:          "key",
			defaultValue: []string{"default1", "default2"},
			expected:     []string{"default1", "default2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			helper := New(tt.config)
			result := helper.GetStringSlice(tt.key, tt.defaultValue)
			if len(result) != len(tt.expected) {
				t.Errorf("GetStringSlice() length = %v, expected %v", len(result), len(tt.expected))
				return
			}
			for i, v := range result {
				if v != tt.expected[i] {
					t.Errorf("GetStringSlice()[%d] = %v, expected %v", i, v, tt.expected[i])
				}
			}
		})
	}
}

func TestValidator(t *testing.T) {
	config := map[string]interface{}{
		"host":     "localhost",
		"port":     "8080",
		"timeout":  30,
		"negative": -5,
	}

	helper := New(config)
	validator := NewValidator()

	validator.RequireString(helper, "host", "主机地址")
	validator.RequirePositiveInt(helper, "timeout", "超时时间")

	if validator.HasErrors() {
		t.Errorf("Expected no errors for valid config, but got: %v", validator.Errors())
	}

	validator2 := NewValidator()
	validator2.RequireString(helper, "missing", "缺失的字段")
	validator2.RequirePositiveInt(helper, "negative", "负数字段")

	if !validator2.HasErrors() {
		t.Error("Expected validation errors, but got none")
	}

	if len(validator2.Errors()) != 2 {
		t.Errorf("Expected 2 validation errors, got %d", len(validator2.Errors()))
	}
}

func TestHelper_HasKey(t *testing.T) {
	config := map[string]interface{}{
		"existing": "value",
	}

	helper := New(config)

	if !helper.HasKey("existing") {
		t.Error("HasKey() should return true for existing key")
	}

	if helper.HasKey("non-existing") {
		t.Error("HasKey() should return false for non-existing key")
	}
}

func TestHelper_GetDuration(t *testing.T) {
	config := map[string]interface{}{
		"timeout_seconds": 30,
		"timeout_ms":      5000,
	}

	helper := New(config)

	duration := helper.GetDuration("timeout_seconds", time.Second, 10*time.Second)
	expected := 30 * time.Second
	if duration != expected {
		t.Errorf("GetDuration() = %v, expected %v", duration, expected)
	}

	duration = helper.GetDuration("timeout_ms", time.Millisecond, 1000*time.Millisecond)
	expected = 5000 * time.Millisecond
	if duration != expected {
		t.Errorf("GetDuration() = %v, expected %v", duration, expected)
	}

	duration = helper.GetDuration("non-existing", time.Second, 60*time.Second)
	expected = 60 * time.Second
	if duration != expected {
		t.Errorf("GetDuration() with default = %v, expected %v", duration, expected)
	}
}
