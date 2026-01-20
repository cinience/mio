package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"manager-server/internal/middleware"
	"manager-server/internal/models"
	"manager-server/internal/service"
)

// Handlers 处理器结构体
type Handlers struct {
	userService   service.SysUserService
	deviceService service.DeviceService
}

// NewHandlers 创建处理器实例
func NewHandlers(userService service.SysUserService) *Handlers {
	return &Handlers{
		userService: userService,
	}
}

// healthCheck 健康检查接口
func (h *Handlers) healthCheck(c *gin.Context) {
	c.JSON(http.StatusOK, &models.CommonResponse{
		Code: 0,
		Msg:  "success",
		Data: gin.H{
			"status": "ok",
		},
	})
}

// login 用户登录
func (h *Handlers) login(c *gin.Context) {
	var req models.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  "参数错误: " + err.Error(),
		})
		return
	}

	resp, err := h.userService.Login(c.Request.Context(), &req)
	if err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, &models.CommonResponse{
		Code: 0,
		Msg:  "success",
		Data: resp,
	})
}

// logout 用户登出
func (h *Handlers) logout(c *gin.Context) {
	// 从请求头获取Token用于登出（仍需要原始Token）
	token := c.GetHeader("Token")
	if token == "" {
		// 如果Header中没有Token，尝试从Authorization中获取
		token = c.GetHeader("Authorization")
		if token != "" {
			// 去除 Bearer 前缀
			token = strings.Replace(token, "Bearer ", "", 1)
		}
	}

	if token == "" {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  "Token不能为空",
		})
		return
	}

	err := h.userService.Logout(c.Request.Context(), token)
	if err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, &models.CommonResponse{
		Code: 0,
		Msg:  "success",
	})
}

// getUserInfo 获取用户信息
func (h *Handlers) getUserInfo(c *gin.Context) {
	// 从认证中间件的上下文中获取用户信息
	userInfo, exists := middleware.GetUserFromContext(c)
	if !exists {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  "未找到用户身份信息",
		})
		return
	}

	c.JSON(http.StatusOK, &models.CommonResponse{
		Code: 0,
		Msg:  "success",
		Data: userInfo,
	})
}

// pageUsers 分页查询用户
func (h *Handlers) pageUsers(c *gin.Context) {
	pageStr := c.Query("page")
	limitStr := c.Query("limit")
	mobile := c.Query("mobile")

	page, _ := strconv.Atoi(pageStr)
	if page <= 0 {
		page = 1
	}

	limit, _ := strconv.Atoi(limitStr)
	if limit <= 0 {
		limit = 10
	}

	pageResp, err := h.userService.PageUsers(c.Request.Context(), page, limit, mobile)
	if err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  err.Error(),
		})
		return
	}

	// 将用户列表转换为AdminPageUserVO列表，并按Java的PageData包装
	var list []map[string]interface{}
	if users, ok := pageResp.List.([]*models.SysUserDTO); ok {
		for _, u := range users {
			deviceCount := 0
			if h.deviceService != nil {
				if dc, err := h.deviceService.GetDeviceCountByUserID(c.Request.Context(), u.ID); err == nil {
					deviceCount = dc
				}
			}
			item := map[string]interface{}{
				"deviceCount": strconv.Itoa(deviceCount),
				"mobile":      u.Mobile,
				"username":    u.Username,
				"status":      u.Status,
				"userid":      strconv.FormatUint(u.ID, 10),
				"createDate":  u.CreateDate.Format("2006-01-02 15:04:05"),
			}
			list = append(list, item)
		}
	}

	data := map[string]interface{}{
		"total": pageResp.TotalCount,
		"list":  list,
	}

	c.JSON(http.StatusOK, &models.CommonResponse{
		Code: 0,
		Msg:  "success",
		Data: data,
	})
}

// resetPassword 重置用户密码
func (h *Handlers) resetPassword(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  "用户ID格式错误",
		})
		return
	}

	newPassword, err := h.userService.ResetPassword(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, &models.CommonResponse{
		Code: 0,
		Msg:  "success",
		Data: newPassword,
	})
}

// deleteUser 删除用户
func (h *Handlers) deleteUser(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  "用户ID格式错误",
		})
		return
	}

	err = h.userService.DeleteByID(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, &models.CommonResponse{
		Code: 0,
		Msg:  "success",
	})
}

// changeUserStatus 批量修改用户状态
func (h *Handlers) changeUserStatus(c *gin.Context) {
	statusStr := c.Param("status")
	status, err := strconv.Atoi(statusStr)
	if err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  "状态参数格式错误",
		})
		return
	}

	var userIds []string
	if err := c.ShouldBindJSON(&userIds); err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  "参数错误: " + err.Error(),
		})
		return
	}

	err = h.userService.ChangeStatus(c.Request.Context(), status, userIds)
	if err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, &models.CommonResponse{
		Code: 0,
		Msg:  "success",
	})
}

// pageDevices 分页查询设备（占位符实现）
func (h *Handlers) pageDevices(c *gin.Context) {
	// TODO: 实现设备分页查询逻辑
	c.JSON(http.StatusOK, &models.CommonResponse{
		Code: 0,
		Msg:  "success",
		Data: &models.PageResponse{
			List:       []interface{}{},
			TotalCount: 0,
			PageSize:   10,
			CurrPage:   1,
			TotalPage:  0,
		},
	})
}
