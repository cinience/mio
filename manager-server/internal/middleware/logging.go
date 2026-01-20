package middleware

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"manager-server/internal/logger"
)

// RequestResponseLogger 请求响应日志中间件
func RequestResponseLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 创建日志记录器
		respLogger := &responseLogger{
			ResponseWriter: c.Writer,
			requestBody:    "",
			responseBody:   &bytes.Buffer{},
			statusCode:     0,
		}

		// 读取请求体（如果存在）
		if c.Request.Body != nil {
			bodyBytes, err := io.ReadAll(c.Request.Body)
			if err == nil {
				respLogger.requestBody = string(bodyBytes)
				// 重置请求体供后续处理使用
				c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
			}
		}

		// 记录开始时间
		start := time.Now()

		// 替换ResponseWriter
		c.Writer = respLogger

		// 处理请求
		c.Next()

		// 计算处理时间
		duration := time.Since(start)

		// 静态资源不记录日志
		if shouldSkipLogging(c) {
			logJSONAccessIfNeeded(c, respLogger, duration) // 仍旧尝试记录JSON访问日志
			return
		}

		// 记录JSON访问日志
		jsonLogged := logJSONAccessIfNeeded(c, respLogger, duration)

		// 获取状态码
		statusCode := c.Writer.Status()

		// 检查是否需要记录详细日志
		shouldLog := statusCode != 200 || hasErrorInResponse(respLogger.responseBody.String())

		if shouldLog {
			logCompactDetailedResponse(c, respLogger, statusCode, duration)
		} else if !jsonLogged {
			logger.Infof("[INFO] %d %s %s (%dms)",
				statusCode,
				c.Request.Method,
				c.Request.URL.Path,
				duration.Milliseconds(),
			)
		}
	}
}

// responseLogger 自定义响应记录器
type responseLogger struct {
	gin.ResponseWriter
	requestBody  string
	responseBody *bytes.Buffer
	statusCode   int
}

// Write 重写Write方法以捕获响应体
func (w *responseLogger) Write(b []byte) (int, error) {
	// 记录响应体
	w.responseBody.Write(b)
	// 写入实际响应
	return w.ResponseWriter.Write(b)
}

// WriteString 重写WriteString方法以捕获响应体
func (w *responseLogger) WriteString(s string) (int, error) {
	// 记录响应体
	w.responseBody.WriteString(s)
	// 写入实际响应
	return w.ResponseWriter.WriteString(s)
}

// WriteHeader 重写WriteHeader方法以捕获状态码
func (w *responseLogger) WriteHeader(statusCode int) {
	w.statusCode = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

// Status 重写Status方法以返回正确的状态码
func (w *responseLogger) Status() int {
	if w.statusCode == 0 {
		return 200 // 默认状态码
	}
	return w.statusCode
}

// logDetailedResponse 记录详细日志（非200状态码或包含错误的响应）
func logDetailedResponse(c *gin.Context, respLogger *responseLogger, statusCode int, duration time.Duration) {
	// 构建日志信息
	logData := map[string]interface{}{
		"timestamp":    time.Now().Format("2006-01-02 15:04:05"),
		"method":       c.Request.Method,
		"path":         c.Request.URL.Path,
		"query":        c.Request.URL.RawQuery,
		"status_code":  statusCode,
		"duration_ms":  duration.Milliseconds(),
		"client_ip":    c.ClientIP(),
		"user_agent":   c.Request.UserAgent(),
		"content_type": c.Request.Header.Get("Content-Type"),
		"headers":      extractHeaders(c),
	}

	// 记录请求体（仅对POST、PUT、PATCH等方法）
	if shouldLogRequestBody(c.Request.Method) && respLogger.requestBody != "" {
		// 尝试格式化JSON请求体
		if isJSONContent(c.Request.Header.Get("Content-Type")) {
			if formatted, err := formatJSON(respLogger.requestBody); err == nil {
				logData["request_body"] = formatted
			} else {
				logData["request_body"] = respLogger.requestBody
			}
		} else {
			logData["request_body"] = respLogger.requestBody
		}
	}

	// 记录响应体
	responseBody := respLogger.responseBody.String()
	if responseBody != "" {
		// 尝试格式化JSON响应体
		if isJSONContent(c.Writer.Header().Get("Content-Type")) {
			if formatted, err := formatJSON(responseBody); err == nil {
				logData["response_body"] = formatted
			} else {
				logData["response_body"] = responseBody
			}
		} else {
			logData["response_body"] = responseBody
		}
	}

	// 输出格式化的日志
	logJSON, err := json.MarshalIndent(logData, "", "  ")
	if err != nil {
		logger.Errorf("Failed to marshal log data: %v", err)
		return
	}

	// 根据状态码选择不同的日志级别
	logLevel := getLogLevel(statusCode)
	if statusCode == 200 {
		logger.Warnf("[WARN] Error Response Detected (Status 200):\n%s", string(logJSON))
	} else {
		switch logLevel {
		case "ERROR":
			logger.Errorf("[ERROR] Non-200 Response Detected:\n%s", string(logJSON))
		case "WARN":
			logger.Warnf("[WARN] Non-200 Response Detected:\n%s", string(logJSON))
		default:
			logger.Infof("[INFO] Non-200 Response Detected:\n%s", string(logJSON))
		}
	}
}

// extractHeaders 提取重要的请求头
func extractHeaders(c *gin.Context) map[string]string {
	headers := make(map[string]string)
	importantHeaders := []string{
		"Authorization", "Device-Id", "Client-Id", "X-Forwarded-For",
		"X-Real-IP", "Accept", "Accept-Language", "Accept-Encoding",
	}

	for _, headerName := range importantHeaders {
		if value := c.Request.Header.Get(headerName); value != "" {
			// 对敏感信息进行脱敏处理
			if headerName == "Authorization" && len(value) > 10 {
				headers[headerName] = value[:10] + "***"
			} else {
				headers[headerName] = value
			}
		}
	}

	return headers
}

// hasErrorInResponse 检查响应体是否包含错误信息
func hasErrorInResponse(responseBody string) bool {
	if responseBody == "" {
		return false
	}

	// 检查是否包含error字段的JSON响应
	if isJSONContent("application/json") {
		var jsonResponse map[string]interface{}
		if err := json.Unmarshal([]byte(responseBody), &jsonResponse); err == nil {
			if errorMsg, exists := jsonResponse["error"]; exists {
				if errorStr, ok := errorMsg.(string); ok && strings.TrimSpace(errorStr) != "" {
					return true
				}
			}
		}
	}

	// 检查是否包含常见错误关键词
	lowerBody := strings.ToLower(responseBody)
	errorKeywords := []string{"error", "failed", "invalid", "unauthorized", "forbidden", "not found"}
	for _, keyword := range errorKeywords {
		if strings.Contains(lowerBody, keyword) {
			return true
		}
	}

	return false
}

// shouldLogRequestBody 判断是否应该记录请求体
func shouldLogRequestBody(method string) bool {
	return method == "POST" || method == "PUT" || method == "PATCH"
}

// isJSONContent 判断是否为JSON内容类型
func isJSONContent(contentType string) bool {
	return strings.Contains(strings.ToLower(contentType), "application/json")
}

// formatJSON 格式化JSON字符串
func formatJSON(data string) (interface{}, error) {
	var jsonData interface{}
	if err := json.Unmarshal([]byte(data), &jsonData); err != nil {
		return nil, err
	}
	return jsonData, nil
}

// getLogLevel 根据状态码获取日志级别
func getLogLevel(statusCode int) string {
	switch {
	case statusCode >= 500:
		return "ERROR"
	case statusCode >= 400:
		return "WARN"
	case statusCode >= 300:
		return "INFO"
	default:
		return "INFO"
	}
}

// CompactRequestResponseLogger 紧凑版日志中间件（仅记录关键信息）
func CompactRequestResponseLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 创建日志记录器
		respLogger := &responseLogger{
			ResponseWriter: c.Writer,
			requestBody:    "",
			responseBody:   &bytes.Buffer{},
			statusCode:     0,
		}

		// 读取请求体（如果存在且为非GET请求）
		if c.Request.Body != nil && shouldLogRequestBody(c.Request.Method) {
			bodyBytes, err := io.ReadAll(c.Request.Body)
			if err == nil {
				respLogger.requestBody = string(bodyBytes)
				// 重置请求体供后续处理使用
				c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
			}
		}

		// 记录开始时间
		start := time.Now()

		// 替换ResponseWriter
		c.Writer = respLogger

		// 处理请求
		c.Next()

		// 计算处理时间
		duration := time.Since(start)

		// 静态资源不记录日志
		if shouldSkipLogging(c) {
			return
		}

		// 获取状态码
		statusCode := c.Writer.Status()

		// 检查是否需要记录详细日志
		shouldLog := statusCode != 200 || hasErrorInResponse(respLogger.responseBody.String())

		if shouldLog {
			logCompactDetailedResponse(c, respLogger, statusCode, duration)
		}
	}
}

// logCompactDetailedResponse 记录紧凑版详细日志（非200状态码或包含错误的响应）
func logCompactDetailedResponse(c *gin.Context, respLogger *responseLogger, statusCode int, duration time.Duration) {
	logLevel := getLogLevel(statusCode)

	logMsg := fmt.Sprintf("[%s] %d %s %s (%dms)",
		logLevel,
		statusCode,
		c.Request.Method,
		c.Request.URL.Path,
		duration.Milliseconds(),
	)

	// 添加查询参数（如果存在）
	if c.Request.URL.RawQuery != "" {
		logMsg += fmt.Sprintf(" ?%s", c.Request.URL.RawQuery)
	}

	// 添加重要请求头
	if deviceId := c.Request.Header.Get("Device-Id"); deviceId != "" {
		logMsg += fmt.Sprintf(" [Device-Id: %s]", deviceId)
	}
	if clientId := c.Request.Header.Get("Client-Id"); clientId != "" {
		logMsg += fmt.Sprintf(" [Client-Id: %s]", clientId)
	}

	// 记录请求体（限制长度）
	if respLogger.requestBody != "" && len(respLogger.requestBody) < 500 {
		logMsg += fmt.Sprintf(" [Request: %s]", strings.ReplaceAll(respLogger.requestBody, "\n", " "))
	} else if len(respLogger.requestBody) >= 500 {
		logMsg += fmt.Sprintf(" [Request: %s...]", strings.ReplaceAll(respLogger.requestBody[:497], "\n", " "))
	}

	// 记录响应体（限制长度）
	responseBody := respLogger.responseBody.String()
	if responseBody != "" && len(responseBody) < 500 {
		logMsg += fmt.Sprintf(" [Response: %s]", strings.ReplaceAll(responseBody, "\n", " "))
	} else if len(responseBody) >= 500 {
		logMsg += fmt.Sprintf(" [Response: %s...]", strings.ReplaceAll(responseBody[:497], "\n", " "))
	}

	logger.Infof("%s", logMsg)
}

var staticExtensions = map[string]struct{}{
	".js":    {},
	".css":   {},
	".png":   {},
	".jpg":   {},
	".jpeg":  {},
	".gif":   {},
	".ico":   {},
	".svg":   {},
	".webp":  {},
	".woff":  {},
	".woff2": {},
	".ttf":   {},
	".eot":   {},
	".otf":   {},
	".map":   {},
	".html":  {},
	".htm":   {},
}

func shouldSkipLogging(c *gin.Context) bool {
	path := c.Request.URL.Path

	switch path {
	case "/health", "/xiaozhi/health":
		return true
	}

	ext := strings.ToLower(filepath.Ext(path))
	if ext != "" {
		if _, ok := staticExtensions[ext]; ok {
			return true
		}
	}

	if strings.HasPrefix(path, "/xiaozhi") {
		return false
	}

	if path == "/" || path == "" {
		return true
	}

	contentType := strings.ToLower(c.Writer.Header().Get("Content-Type"))
	if contentType == "" {
		contentType = strings.ToLower(c.Request.Header.Get("Accept"))
	}

	switch {
	case strings.HasPrefix(contentType, "text/html"),
		strings.HasPrefix(contentType, "text/css"),
		strings.HasPrefix(contentType, "text/javascript"),
		strings.HasPrefix(contentType, "application/javascript"),
		strings.HasPrefix(contentType, "image/"),
		strings.HasPrefix(contentType, "font/"):
		return true
	default:
		return false
	}
}

func logJSONAccessIfNeeded(c *gin.Context, respLogger *responseLogger, duration time.Duration) bool {
	if shouldSkipLogging(c) {
		return false
	}

	requestContentType := strings.ToLower(c.Request.Header.Get("Content-Type"))
	responseContentType := strings.ToLower(c.Writer.Header().Get("Content-Type"))

	if !isJSONContent(requestContentType) && !isJSONContent(responseContentType) {
		return false
	}

	requestBody := truncateAndSanitize(respLogger.requestBody, 500)
	responseBody := truncateAndSanitize(respLogger.responseBody.String(), 500)

	logger.Infof("[ACCESS] %d %s %s%s (%dms) req=%s resp=%s",
		c.Writer.Status(),
		c.Request.Method,
		c.Request.URL.Path,
		formatQuery(c.Request.URL.RawQuery),
		duration.Milliseconds(),
		requestBody,
		responseBody,
	)

	return true
}

func truncateAndSanitize(body string, limit int) string {
	if limit <= 0 {
		return "-"
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return "-"
	}

	body = strings.ReplaceAll(body, "\n", " ")
	body = strings.ReplaceAll(body, "\r", " ")

	if len(body) > limit {
		suffix := "...(truncated)"
		if len(suffix) > limit {
			suffix = "..."
		}
		cutoff := limit - len(suffix)
		if cutoff < 0 {
			cutoff = 0
		}
		body = strings.TrimSpace(body[:cutoff]) + suffix
	}

	if body == "" {
		return "-"
	}
	return body
}

func formatQuery(query string) string {
	if query == "" {
		return ""
	}
	return " ?" + query
}
