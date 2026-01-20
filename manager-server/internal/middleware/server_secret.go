package middleware

import (
	"encoding/base64"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"manager-server/internal/constants"
	"manager-server/internal/service"
)

// ServerSecretMiddleware 服务器密钥认证中间件结构体
type ServerSecretMiddleware struct {
	paramsService service.SysParamsService
}

// NewServerSecretMiddleware 创建服务器密钥认证中间件实例
func NewServerSecretMiddleware(paramsService service.SysParamsService) *ServerSecretMiddleware {
	return &ServerSecretMiddleware{
		paramsService: paramsService,
	}
}

// Middleware 服务器密钥认证中间件函数
func (sm *ServerSecretMiddleware) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 对OPTIONS请求放行
		if c.Request.Method == "OPTIONS" {
			c.Next()
			return
		}

		// 从请求头获取Token（支持 Bearer 与 Basic，Basic 参考 Subsonic 用法）
		token := getRequestToken(c)
		if token == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"code": http.StatusUnauthorized,
				"msg":  "服务器密钥不能为空",
			})
			c.Abort()
			return
		}

		// 获取服务器密钥
		serverSecret, err := sm.paramsService.GetValue(c.Request.Context(), constants.SERVER_SECRET, "")
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{
				"code": http.StatusUnauthorized,
				"msg":  "获取服务器密钥失败: " + err.Error(),
			})
			c.Abort()
			return
		}

		// 验证token是否匹配
		if serverSecret == "" || serverSecret == "null" || serverSecret != token {
			c.JSON(http.StatusUnauthorized, gin.H{
				"code": http.StatusUnauthorized,
				"msg":  "无效的服务器密钥",
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

// getRequestToken 获取请求的token
func getRequestToken(c *gin.Context) string {
	// 从header中获取token
	authorization := c.GetHeader("Authorization")
	if authorization != "" && strings.HasPrefix(authorization, "Bearer ") {
		return strings.Replace(authorization, "Bearer ", "", 1)
	}
	if authorization != "" && strings.HasPrefix(strings.ToLower(authorization), "basic ") {
		encoded := strings.TrimSpace(authorization[len("Basic "):])
		decodedBytes, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return ""
		}
		creds := string(decodedBytes)
		parts := strings.SplitN(creds, ":", 2)
		if len(parts) != 2 {
			return ""
		}
		// Subsonic 风格：用户名/密码均可使用 "default"，其余按服务器密钥比对
		if parts[0] == "default" && parts[1] == "default" {
			return ""
		}
		if parts[0] == "default" {
			return strings.TrimSpace(parts[1])
		}
		if parts[1] == "default" {
			return strings.TrimSpace(parts[0])
		}
		// 兜底：取用户名为 token
		return strings.TrimSpace(parts[0])
	}
	return ""
}
