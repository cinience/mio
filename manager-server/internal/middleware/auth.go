package middleware

import (
	"net/http"
	"strings"
	"manager-server/internal/models"
	"manager-server/internal/service"

	"github.com/gin-gonic/gin"
)

// AuthMiddleware 认证中间件结构体
type AuthMiddleware struct {
	tokenService  service.TokenService
	paramsService service.SysParamsService
}

// NewAuthMiddleware 创建认证中间件实例
func NewAuthMiddleware(tokenService service.TokenService, paramsService service.SysParamsService) *AuthMiddleware {
	return &AuthMiddleware{
		tokenService:  tokenService,
		paramsService: paramsService,
	}
}

// Middleware 混合认证中间件函数（支持系统密钥和用户token）
func (am *AuthMiddleware) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 从请求头获取Token
		token := c.GetHeader("Authorization")
		if token == "" {
			token = c.GetHeader("token")
		}
		if token == "" {
			am.handleUnauthorized(c, "未提供认证令牌")
			return
		}

		// 去除 Bearer 前缀
		token = strings.Replace(token, "Bearer ", "", 1)

		// 从参数字典获取系统密钥
		systemSecret, err := am.paramsService.GetValue(c.Request.Context(), "server.secret", "")
		if err != nil {
			am.handleUnauthorized(c, "系统配置错误: "+err.Error())
			return
		}

		// 如果没有配置系统密钥，使用默认值（向后兼容）
		if systemSecret == "" {
			systemSecret = "cc36d3ca-b065-47ef-ae29-d34f7540abd1"
		}

		// 检查是否为系统密钥
		if token == systemSecret {
			// 系统级调用，使用admin身份
			adminUser := &models.SysUserDTO{
				ID:         0, // 系统用户ID为0
				Username:   "system",
				RealName:   "系统管理员",
				SuperAdmin: func() *int { v := 1; return &v }(), // 超级管理员
			}
			c.Set("userID", uint64(0))
			c.Set("user", adminUser)
			c.Set("isSystemCall", true)
			c.Next()
			return
		}

		// 普通用户token验证
		userDTO, err := am.tokenService.GetUserByToken(c.Request.Context(), token)
		if err != nil {
			am.handleUnauthorized(c, "认证失败: "+err.Error())
			return
		}

		// 将用户ID和用户信息存储到上下文中
		c.Set("userID", userDTO.ID)
		c.Set("user", userDTO)
		c.Set("isSystemCall", false)

		c.Next()
	}
}

// handleUnauthorized 处理未授权请求
// 根据请求类型决定是返回JSON错误还是重定向到登录页面
func (am *AuthMiddleware) handleUnauthorized(c *gin.Context, message string) {
	// 检查是否为API请求（通过Accept头或Content-Type判断）
	accept := c.GetHeader("Accept")
	contentType := c.GetHeader("Content-Type")

	// 如果是API请求（Accept包含application/json或Content-Type为application/json），返回JSON错误
	if strings.Contains(accept, "application/json") ||
		strings.Contains(contentType, "application/json") ||
		strings.Contains(accept, "application/xml") ||
		strings.Contains(contentType, "application/xml") {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code": http.StatusUnauthorized,
			"msg":  message,
		})
		c.Abort()
		return
	}

	// 对于浏览器请求，重定向到登录页面
	// 获取当前请求的完整URL，用于登录后重定向回来
	currentPath := c.Request.URL.Path
	if currentPath == "" {
		currentPath = "/"
	}

	// 构建登录页面URL，包含重定向参数
	// 前端路由守卫会处理 redirect 参数
	loginURL := "/login?redirect=" + currentPath
	if c.Request.URL.RawQuery != "" {
		loginURL += "&" + c.Request.URL.RawQuery
	}

	c.Redirect(http.StatusFound, loginURL)
	c.Abort()
}

// GetUserIDFromContext 从上下文中获取用户ID
func GetUserIDFromContext(c *gin.Context) (uint64, bool) {
	userID, exists := c.Get("userID")
	if !exists {
		return 0, false
	}

	id, ok := userID.(uint64)
	return id, ok
}

// GetUserFromContext 从上下文中获取用户信息
func GetUserFromContext(c *gin.Context) (*models.SysUserDTO, bool) {
	user, exists := c.Get("user")
	if !exists {
		return nil, false
	}

	userDTO, ok := user.(*models.SysUserDTO)
	return userDTO, ok
}

// IsSuperAdminFromContext 从上下文中判断是否为超级管理员
func IsSuperAdminFromContext(c *gin.Context) bool {
	user, exists := GetUserFromContext(c)
	if !exists {
		return false
	}

	return user.SuperAdmin != nil && *user.SuperAdmin == 1 // 假设1表示超级管理员
}

// IsSystemCallFromContext 从上下文中判断是否为系统调用
func IsSystemCallFromContext(c *gin.Context) bool {
	isSystemCall, exists := c.Get("isSystemCall")
	if !exists {
		return false
	}

	systemCall, ok := isSystemCall.(bool)
	return ok && systemCall
}
