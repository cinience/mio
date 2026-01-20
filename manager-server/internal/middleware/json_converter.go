package middleware

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// JSONLongConverterMiddleware 创建JSON Long类型转换中间件
func JSONLongConverterMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 处理请求体 - 将String转换为Long
		if c.Request.Body != nil && c.Request.ContentLength > 0 {
			bodyBytes, err := io.ReadAll(c.Request.Body)
			if err == nil {
				// 重置请求体以供后续处理使用
				c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

				// 如果是JSON请求，暂存原始数据供ShouldBindJSON使用
				if strings.Contains(c.ContentType(), "application/json") {
					c.Set("__original_json_body", bodyBytes)
				}
			}
		}

		// 重写ResponseWriter以拦截JSON响应
		writer := &responseWriter{
			ResponseWriter: c.Writer,
			body:           bytes.NewBufferString(""),
		}
		c.Writer = writer

		c.Next()

		// 处理响应 - 将Long转换为String
		if writer.body.Len() > 0 {
			contentType := c.Writer.Header().Get("Content-Type")
			if strings.Contains(contentType, "application/json") {
				convertedBody := convertLongToStringInJSON(writer.body.Bytes())
				c.Writer.Header().Set("Content-Length", fmt.Sprintf("%d", len(convertedBody)))
				writer.ResponseWriter.Write(convertedBody)
			} else {
				writer.ResponseWriter.Write(writer.body.Bytes())
			}
		}
	}
}

// responseWriter 自定义响应写入器，用于拦截响应内容
type responseWriter struct {
	gin.ResponseWriter
	body *bytes.Buffer
}

func (w *responseWriter) Write(b []byte) (int, error) {
	return w.body.Write(b)
}

func (w *responseWriter) WriteString(s string) (int, error) {
	return w.body.WriteString(s)
}

// convertLongToStringInJSON 在JSON中将Long类型转换为String
func convertLongToStringInJSON(data []byte) []byte {
	var jsonData interface{}
	if err := json.Unmarshal(data, &jsonData); err != nil {
		return data // 如果不是有效JSON，返回原数据
	}

	converted := convertLongValues(jsonData)
	result, err := json.Marshal(converted)
	if err != nil {
		return data // 如果转换失败，返回原数据
	}
	return result
}

// convertLongValues 递归转换JSON中的Long值为String
func convertLongValues(data interface{}) interface{} {
	switch v := data.(type) {
	case map[string]interface{}:
		result := make(map[string]interface{})
		for key, value := range v {
			result[key] = convertLongValues(value)
		}
		return result
	case []interface{}:
		result := make([]interface{}, len(v))
		for i, item := range v {
			result[i] = convertLongValues(item)
		}
		return result
	case float64:
		// JSON解析时，所有数字都被解析为float64
		// 如果是整数且在int64或uint64范围内，转换为字符串
		if v == float64(int64(v)) {
			// 是整数，转换为字符串以避免JavaScript精度丢失
			return fmt.Sprintf("%.0f", v)
		}
		return v
	default:
		return v
	}
}

// ShouldBindJSONWithConversion 支持Long类型转换的JSON绑定方法
func ShouldBindJSONWithConversion(c *gin.Context, obj interface{}) error {
	// 获取原始JSON数据
	bodyBytes, exists := c.Get("__original_json_body")
	if !exists {
		// 如果没有原始数据，使用标准方法
		return c.ShouldBindJSON(obj)
	}

	data, ok := bodyBytes.([]byte)
	if !ok {
		return c.ShouldBindJSON(obj)
	}

	// 预处理JSON：将字符串格式的数字转换为实际数字
	processedData := convertStringToLongInJSON(data)

	// 使用处理后的数据进行绑定
	return json.Unmarshal(processedData, obj)
}

// convertStringToLongInJSON 在JSON中将String格式的数字转换为Long
func convertStringToLongInJSON(data []byte) []byte {
	var jsonData interface{}
	if err := json.Unmarshal(data, &jsonData); err != nil {
		return data
	}

	converted := convertStringToLong(jsonData)
	result, err := json.Marshal(converted)
	if err != nil {
		return data
	}
	return result
}

// convertStringToLong 递归转换JSON中的String数字为实际数字
func convertStringToLong(data interface{}) interface{} {
	switch v := data.(type) {
	case map[string]interface{}:
		result := make(map[string]interface{})
		for key, value := range v {
			result[key] = convertStringToLong(value)
		}
		return result
	case []interface{}:
		result := make([]interface{}, len(v))
		for i, item := range v {
			result[i] = convertStringToLong(item)
		}
		return result
	case string:
		// 尝试将字符串转换为数字（如果是纯数字字符串）
		if num, err := strconv.ParseInt(v, 10, 64); err == nil {
			return num
		}
		if num, err := strconv.ParseUint(v, 10, 64); err == nil {
			return num
		}
		if num, err := strconv.ParseFloat(v, 64); err == nil {
			return num
		}
		return v
	default:
		return v
	}
}

// JSONWithLongConversion 支持Long类型转换的JSON响应方法
func JSONWithLongConversion(c *gin.Context, code int, obj interface{}) {
	// 先序列化对象
	data, err := json.Marshal(obj)
	if err != nil {
		c.JSON(code, gin.H{"error": "serialization failed"})
		return
	}

	// 转换Long为String
	convertedData := convertLongToStringInJSON(data)

	// 设置响应头并写入数据
	c.Header("Content-Type", "application/json; charset=utf-8")
	c.Status(code)
	c.Writer.Write(convertedData)
}
