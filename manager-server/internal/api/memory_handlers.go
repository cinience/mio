package api

import (
	"net/http"
	"strconv"

	"github.com/cloudwego/eino/schema"
	"github.com/gin-gonic/gin"

	"manager-server/internal/middleware"
	"manager-server/internal/models"
	"manager-server/internal/service"
)

// MemoryHandlers 长记忆管理处理器
type MemoryHandlers struct {
	memoryService service.MemoryService
	deviceService service.DeviceService
}

// NewMemoryHandlers 创建长记忆管理处理器实例
func NewMemoryHandlers(memoryService service.MemoryService, deviceService service.DeviceService) *MemoryHandlers {
	return &MemoryHandlers{
		memoryService: memoryService,
		deviceService: deviceService,
	}
}

// storeDeviceMessages 存储设备长记忆消息（服务间调用）
func (h *MemoryHandlers) storeDeviceMessages(c *gin.Context) {
	deviceID := c.Param("deviceId")
	if deviceID == "" {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  "设备ID不能为空",
		})
		return
	}

	var req struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  "参数错误: " + err.Error(),
		})
		return
	}

	if len(req.Messages) == 0 {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 0,
			Msg:  "success",
		})
		return
	}

	messages := make([]schema.Message, 0, len(req.Messages))
	for _, msg := range req.Messages {
		role := schema.RoleType(msg.Role)
		if role == "" {
			role = schema.User
		}
		messages = append(messages, schema.Message{
			Role:    role,
			Content: msg.Content,
		})
	}

	if err := h.memoryService.StoreDeviceMessages(c.Request.Context(), deviceID, messages); err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  "存储消息失败: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, &models.CommonResponse{
		Code: 0,
		Msg:  "success",
	})
}

// internalGetDeviceProfile 获取设备的用户画像摘要（服务间调用）
func (h *MemoryHandlers) internalGetDeviceProfile(c *gin.Context) {
	deviceID := c.Param("deviceId")
	if deviceID == "" {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  "设备ID不能为空",
		})
		return
	}

	profile, err := h.memoryService.GetDeviceProfile(c.Request.Context(), deviceID)
	if err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  "获取用户画像失败: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, &models.CommonResponse{
		Code: 0,
		Msg:  "success",
		Data: gin.H{
			"profile": profile,
		},
	})
}

// internalUpdateDeviceProfile 更新设备的用户画像（服务间调用）
func (h *MemoryHandlers) internalUpdateDeviceProfile(c *gin.Context) {
	deviceID := c.Param("deviceId")
	if deviceID == "" {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  "设备ID不能为空",
		})
		return
	}

	var req struct {
		Content  string `json:"content" binding:"required"`
		Topic    string `json:"topic" binding:"required"`
		SubTopic string `json:"subTopic"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  "参数错误: " + err.Error(),
		})
		return
	}

	if err := h.memoryService.UpdateDeviceProfile(c.Request.Context(), deviceID, req.Content, req.Topic, req.SubTopic); err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  "更新用户画像失败: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, &models.CommonResponse{
		Code: 0,
		Msg:  "success",
	})
}

// internalGetDeviceProfiles 获取设备的用户画像列表（服务间调用）
func (h *MemoryHandlers) internalGetDeviceProfiles(c *gin.Context) {
	deviceID := c.Param("deviceId")
	if deviceID == "" {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  "设备ID不能为空",
		})
		return
	}

	topicsParam := c.Query("topics")
	var topics []string
	if topicsParam != "" {
		topics = []string{topicsParam}
	}

	profiles, err := h.memoryService.GetDeviceProfiles(c.Request.Context(), deviceID, topics)
	if err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  "获取用户画像失败: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, &models.CommonResponse{
		Code: 0,
		Msg:  "success",
		Data: profiles,
	})
}

// internalGetDeviceEvents 获取设备的用户事件（服务间调用）
func (h *MemoryHandlers) internalGetDeviceEvents(c *gin.Context) {
	deviceID := c.Param("deviceId")
	if deviceID == "" {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  "设备ID不能为空",
		})
		return
	}

	limitStr := c.DefaultQuery("limit", "50")
	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit <= 0 {
		limit = 50
	}

	events, err := h.memoryService.GetDeviceEvents(c.Request.Context(), deviceID, limit)
	if err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  "获取用户事件失败: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, &models.CommonResponse{
		Code: 0,
		Msg:  "success",
		Data: events,
	})
}

// internalGetDeviceLongTermContext 获取长记忆上下文（服务间调用）
func (h *MemoryHandlers) internalGetDeviceLongTermContext(c *gin.Context) {
	deviceID := c.Param("deviceId")
	if deviceID == "" {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  "设备ID不能为空",
		})
		return
	}

	maxTokensStr := c.DefaultQuery("maxTokens", "4000")
	maxTokens, err := strconv.Atoi(maxTokensStr)
	if err != nil || maxTokens <= 0 {
		maxTokens = 4000
	}

	contextData, err := h.memoryService.GetDeviceLongTermContext(c.Request.Context(), deviceID, maxTokens)
	if err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  "获取长记忆上下文失败: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, &models.CommonResponse{
		Code: 0,
		Msg:  "success",
		Data: gin.H{
			"context": contextData,
		},
	})
}

// getDeviceProfiles 获取设备的用户画像
func (h *MemoryHandlers) getDeviceProfiles(c *gin.Context) {
	deviceID := c.Param("deviceId")
	if deviceID == "" {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  "设备ID不能为空",
		})
		return
	}

	// 从认证中间件中获取用户ID
	userID, exists := middleware.GetUserIDFromContext(c)
	if !exists {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 401,
			Msg:  "用户未认证",
		})
		return
	}

	// 检查权限（非超级管理员需要检查设备所有权）
	isSuperAdmin := middleware.IsSuperAdminFromContext(c)
	if !isSuperAdmin {
		err := h.memoryService.CheckDevicePermission(c.Request.Context(), deviceID, userID)
		if err != nil {
			c.JSON(http.StatusOK, &models.CommonResponse{
				Code: 403,
				Msg:  err.Error(),
			})
			return
		}
	}

	// 获取查询参数
	topicsParam := c.Query("topics")
	var topics []string
	if topicsParam != "" {
		topics = []string{topicsParam}
	}

	profiles, err := h.memoryService.GetDeviceProfiles(c.Request.Context(), deviceID, topics)
	if err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  "获取用户画像失败: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, &models.CommonResponse{
		Code: 0,
		Msg:  "success",
		Data: profiles,
	})
}

// getDeviceEvents 获取设备的用户事件
func (h *MemoryHandlers) getDeviceEvents(c *gin.Context) {
	deviceID := c.Param("deviceId")
	if deviceID == "" {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  "设备ID不能为空",
		})
		return
	}

	// 从认证中间件中获取用户ID
	userID, exists := middleware.GetUserIDFromContext(c)
	if !exists {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 401,
			Msg:  "用户未认证",
		})
		return
	}

	// 检查权限（非超级管理员需要检查设备所有权）
	isSuperAdmin := middleware.IsSuperAdminFromContext(c)
	if !isSuperAdmin {
		err := h.memoryService.CheckDevicePermission(c.Request.Context(), deviceID, userID)
		if err != nil {
			c.JSON(http.StatusOK, &models.CommonResponse{
				Code: 403,
				Msg:  err.Error(),
			})
			return
		}
	}

	// 获取查询参数
	limitStr := c.DefaultQuery("limit", "50")
	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit <= 0 {
		limit = 50
	}

	events, err := h.memoryService.GetDeviceEvents(c.Request.Context(), deviceID, limit)
	if err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  "获取用户事件失败: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, &models.CommonResponse{
		Code: 0,
		Msg:  "success",
		Data: events,
	})
}

// updateDeviceProfile 更新设备的用户画像
func (h *MemoryHandlers) updateDeviceProfile(c *gin.Context) {
	deviceID := c.Param("deviceId")
	if deviceID == "" {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  "设备ID不能为空",
		})
		return
	}

	// 从认证中间件中获取用户ID
	userID, exists := middleware.GetUserIDFromContext(c)
	if !exists {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 401,
			Msg:  "用户未认证",
		})
		return
	}

	// 检查权限（非超级管理员需要检查设备所有权）
	isSuperAdmin := middleware.IsSuperAdminFromContext(c)
	if !isSuperAdmin {
		err := h.memoryService.CheckDevicePermission(c.Request.Context(), deviceID, userID)
		if err != nil {
			c.JSON(http.StatusOK, &models.CommonResponse{
				Code: 403,
				Msg:  err.Error(),
			})
			return
		}
	}

	// 解析请求体
	var req struct {
		Content  string `json:"content" binding:"required"`
		Topic    string `json:"topic" binding:"required"`
		SubTopic string `json:"subTopic"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  "参数错误: " + err.Error(),
		})
		return
	}

	err := h.memoryService.UpdateDeviceProfile(c.Request.Context(), deviceID, req.Content, req.Topic, req.SubTopic)
	if err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  "更新用户画像失败: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, &models.CommonResponse{
		Code: 0,
		Msg:  "success",
	})
}

// deleteDeviceProfile 删除设备的用户画像
func (h *MemoryHandlers) deleteDeviceProfile(c *gin.Context) {
	deviceID := c.Param("deviceId")
	profileID := c.Param("profileId")

	if deviceID == "" || profileID == "" {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  "设备ID和画像ID不能为空",
		})
		return
	}

	// 从认证中间件中获取用户ID
	userID, exists := middleware.GetUserIDFromContext(c)
	if !exists {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 401,
			Msg:  "用户未认证",
		})
		return
	}

	// 检查权限（非超级管理员需要检查设备所有权）
	isSuperAdmin := middleware.IsSuperAdminFromContext(c)
	if !isSuperAdmin {
		err := h.memoryService.CheckDevicePermission(c.Request.Context(), deviceID, userID)
		if err != nil {
			c.JSON(http.StatusOK, &models.CommonResponse{
				Code: 403,
				Msg:  err.Error(),
			})
			return
		}
	}

	err := h.memoryService.DeleteDeviceProfile(c.Request.Context(), deviceID, profileID)
	if err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  "删除用户画像失败: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, &models.CommonResponse{
		Code: 0,
		Msg:  "success",
	})
}

// getDeviceLongTermContext 获取设备的长记忆上下文
func (h *MemoryHandlers) getDeviceLongTermContext(c *gin.Context) {
	deviceID := c.Param("deviceId")
	if deviceID == "" {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  "设备ID不能为空",
		})
		return
	}

	// 从认证中间件中获取用户ID
	userID, exists := middleware.GetUserIDFromContext(c)
	if !exists {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 401,
			Msg:  "用户未认证",
		})
		return
	}

	// 检查权限（非超级管理员需要检查设备所有权）
	isSuperAdmin := middleware.IsSuperAdminFromContext(c)
	if !isSuperAdmin {
		err := h.memoryService.CheckDevicePermission(c.Request.Context(), deviceID, userID)
		if err != nil {
			c.JSON(http.StatusOK, &models.CommonResponse{
				Code: 403,
				Msg:  err.Error(),
			})
			return
		}
	}

	// 获取查询参数
	maxTokensStr := c.DefaultQuery("maxTokens", "2000")
	maxTokens, err := strconv.Atoi(maxTokensStr)
	if err != nil || maxTokens <= 0 {
		maxTokens = 2000
	}

	context, err := h.memoryService.GetDeviceLongTermContext(c.Request.Context(), deviceID, maxTokens)
	if err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  "获取长记忆上下文失败: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, &models.CommonResponse{
		Code: 0,
		Msg:  "success",
		Data: map[string]interface{}{
			"context": context,
		},
	})
}
