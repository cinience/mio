package confighelper

import (
	"fmt"
	"reflect"
	"strconv"
	"time"
)

// Helper 提供类型安全的 map[string]interface{} 配置读取能力。
type Helper struct {
	config map[string]interface{}
}

// New 创建一个新的 Helper。
func New(config map[string]interface{}) *Helper {
	return &Helper{config: config}
}

// GetString 获取字符串值。
func (c *Helper) GetString(key string, defaultValue ...string) string {
	value, exists := c.config[key]
	if !exists {
		return c.defaultString(defaultValue)
	}

	switch v := value.(type) {
	case string:
		return v
	case int, int8, int16, int32, int64:
		return fmt.Sprintf("%d", v)
	case uint, uint8, uint16, uint32, uint64:
		return fmt.Sprintf("%d", v)
	case float32, float64:
		return fmt.Sprintf("%g", v)
	case bool:
		return fmt.Sprintf("%t", v)
	default:
		return c.defaultString(defaultValue)
	}
}

// GetInt 获取整数值。
func (c *Helper) GetInt(key string, defaultValue ...int) int {
	value, exists := c.config[key]
	if !exists {
		return c.defaultInt(defaultValue)
	}

	switch v := value.(type) {
	case int:
		return v
	case int8:
		return int(v)
	case int16:
		return int(v)
	case int32:
		return int(v)
	case int64:
		return int(v)
	case uint:
		return int(v)
	case uint8:
		return int(v)
	case uint16:
		return int(v)
	case uint32:
		return int(v)
	case uint64:
		return int(v)
	case float32:
		return int(v)
	case float64:
		return int(v)
	case string:
		if intVal, err := strconv.Atoi(v); err == nil {
			return intVal
		}
	}
	return c.defaultInt(defaultValue)
}

// GetFloat64 获取浮点数值。
func (c *Helper) GetFloat64(key string, defaultValue ...float64) float64 {
	value, exists := c.config[key]
	if !exists {
		return c.defaultFloat64(defaultValue)
	}

	switch v := value.(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int, int8, int16, int32, int64:
		return float64(reflect.ValueOf(v).Int())
	case uint, uint8, uint16, uint32, uint64:
		return float64(reflect.ValueOf(v).Uint())
	case string:
		if floatVal, err := strconv.ParseFloat(v, 64); err == nil {
			return floatVal
		}
	}
	return c.defaultFloat64(defaultValue)
}

// GetBool 获取布尔值。
func (c *Helper) GetBool(key string, defaultValue ...bool) bool {
	value, exists := c.config[key]
	if !exists {
		return c.defaultBool(defaultValue)
	}

	switch v := value.(type) {
	case bool:
		return v
	case string:
		if boolVal, err := strconv.ParseBool(v); err == nil {
			return boolVal
		}
	case int, int8, int16, int32, int64:
		return reflect.ValueOf(v).Int() != 0
	case uint, uint8, uint16, uint32, uint64:
		return reflect.ValueOf(v).Uint() != 0
	case float32, float64:
		return reflect.ValueOf(v).Float() != 0
	}
	return c.defaultBool(defaultValue)
}

// GetDuration 获取时间间隔值（支持秒、毫秒等）。
func (c *Helper) GetDuration(key string, unit time.Duration, defaultValue ...time.Duration) time.Duration {
	value := c.GetInt(key, -1)
	if value == -1 {
		return c.defaultDuration(defaultValue)
	}
	return time.Duration(value) * unit
}

// GetStringSlice 获取字符串切片。
func (c *Helper) GetStringSlice(key string, defaultValue ...[]string) []string {
	value, exists := c.config[key]
	if !exists {
		return c.defaultStringSlice(defaultValue)
	}

	switch v := value.(type) {
	case []string:
		return v
	case []interface{}:
		result := make([]string, len(v))
		for i, item := range v {
			result[i] = fmt.Sprintf("%v", item)
		}
		return result
	case string:
		return []string{v}
	}
	return c.defaultStringSlice(defaultValue)
}

// GetIntSlice 获取整数切片。
func (c *Helper) GetIntSlice(key string, defaultValue ...[]int) []int {
	value, exists := c.config[key]
	if !exists {
		return c.defaultIntSlice(defaultValue)
	}

	switch v := value.(type) {
	case []int:
		return v
	case []interface{}:
		result := make([]int, 0, len(v))
		for _, item := range v {
			helper := New(map[string]interface{}{"temp": item})
			result = append(result, helper.GetInt("temp"))
		}
		return result
	}
	return c.defaultIntSlice(defaultValue)
}

// HasKey 检查是否存在指定键。
func (c *Helper) HasKey(key string) bool {
	_, exists := c.config[key]
	return exists
}

// GetKeys 返回所有键。
func (c *Helper) GetKeys() []string {
	keys := make([]string, 0, len(c.config))
	for key := range c.config {
		keys = append(keys, key)
	}
	return keys
}

func (c *Helper) defaultString(defaultValue []string) string {
	if len(defaultValue) > 0 {
		return defaultValue[0]
	}
	return ""
}

func (c *Helper) defaultInt(defaultValue []int) int {
	if len(defaultValue) > 0 {
		return defaultValue[0]
	}
	return 0
}

func (c *Helper) defaultFloat64(defaultValue []float64) float64 {
	if len(defaultValue) > 0 {
		return defaultValue[0]
	}
	return 0.0
}

func (c *Helper) defaultBool(defaultValue []bool) bool {
	if len(defaultValue) > 0 {
		return defaultValue[0]
	}
	return false
}

func (c *Helper) defaultDuration(defaultValue []time.Duration) time.Duration {
	if len(defaultValue) > 0 {
		return defaultValue[0]
	}
	return 0
}

func (c *Helper) defaultStringSlice(defaultValue [][]string) []string {
	if len(defaultValue) > 0 {
		return defaultValue[0]
	}
	return []string{}
}

func (c *Helper) defaultIntSlice(defaultValue [][]int) []int {
	if len(defaultValue) > 0 {
		return defaultValue[0]
	}
	return []int{}
}

// Validator 提供简单的配置验证能力。
type Validator struct {
	errors []error
}

// NewValidator 创建新的 Validator。
func NewValidator() *Validator {
	return &Validator{
		errors: make([]error, 0),
	}
}

// RequireString 要求必须有字符串值。
func (v *Validator) RequireString(helper *Helper, key string, fieldName string) *Validator {
	if !helper.HasKey(key) || helper.GetString(key) == "" {
		v.errors = append(v.errors, fmt.Errorf("%s 不能为空", fieldName))
	}
	return v
}

// RequirePositiveInt 要求必须有正整数值。
func (v *Validator) RequirePositiveInt(helper *Helper, key string, fieldName string) *Validator {
	if !helper.HasKey(key) || helper.GetInt(key) <= 0 {
		v.errors = append(v.errors, fmt.Errorf("%s 必须是正整数", fieldName))
	}
	return v
}

// RequireNonNegativeInt 要求必须有非负整数值。
func (v *Validator) RequireNonNegativeInt(helper *Helper, key string, fieldName string) *Validator {
	if !helper.HasKey(key) || helper.GetInt(key) < 0 {
		v.errors = append(v.errors, fmt.Errorf("%s 必须是非负整数", fieldName))
	}
	return v
}

// ValidateRange 验证数值范围。
func (v *Validator) ValidateRange(helper *Helper, key string, fieldName string, min, max int) *Validator {
	value := helper.GetInt(key)
	if value < min || value > max {
		v.errors = append(v.errors, fmt.Errorf("%s 必须在 %d 和 %d 之间", fieldName, min, max))
	}
	return v
}

// Errors 返回所有错误。
func (v *Validator) Errors() []error {
	return v.errors
}

// Error 返回组合后的错误。
func (v *Validator) Error() error {
	return fmt.Errorf("配置验证失败: %v", v.errors)
}

// GetError 保留兼容方法，行为等同于 Error()。
func (v *Validator) GetError() error {
	if !v.HasErrors() {
		return nil
	}
	return v.Error()
}

// HasErrors 是否存在错误。
func (v *Validator) HasErrors() bool {
	return len(v.errors) > 0
}

// FirstError 返回第一个错误。
func (v *Validator) FirstError() error {
	if len(v.errors) > 0 {
		return v.errors[0]
	}
	return nil
}
