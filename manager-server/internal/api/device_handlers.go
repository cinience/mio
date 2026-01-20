package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"manager-server/internal/middleware"
	"manager-server/internal/models"
	"manager-server/internal/service"
)

// DeviceHandlers 设备处理器结构体
type DeviceHandlers struct {
	deviceService service.DeviceService
	paramsService service.SysParamsService
}

// NewDeviceHandlers 创建设备处理器实例
func NewDeviceHandlers(deviceService service.DeviceService, paramsService service.SysParamsService) *DeviceHandlers {
	return &DeviceHandlers{
		deviceService: deviceService,
		paramsService: paramsService,
	}
}

// registerDevice 注册设备
// @Summary 注册设备
// @Description 注册新设备到系统
// @Tags device
// @Accept json
// @Produce json
// @Param body body models.DeviceRegisterDTO true "设备注册信息"
// @Success 200 {object} models.CommonResponse "注册成功"
// @Failure 400 {object} models.CommonResponse "参数错误"
// @Failure 500 {object} models.CommonResponse "服务器错误"
// @Router /device/register [post]
func (h *DeviceHandlers) registerDevice(c *gin.Context) {
	var req models.DeviceRegisterDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  "参数错误: " + err.Error(),
		})
		return
	}

	code, err := h.deviceService.RegisterDevice(c.Request.Context(), &req)
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
		Data: code,
	})
}

// bindDevice 绑定设备
// @Summary 绑定设备
// @Description 将设备绑定到指定智能体
// @Tags device
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param agentId path string true "智能体ID"
// @Param deviceCode path string true "设备激活码"
// @Success 200 {object} models.CommonResponse "绑定成功"
// @Failure 400 {object} models.CommonResponse "参数错误"
// @Failure 401 {object} models.CommonResponse "未授权"
// @Failure 500 {object} models.CommonResponse "服务器错误"
// @Router /device/bind/{agentId}/{deviceCode} [post]
func (h *DeviceHandlers) bindDevice(c *gin.Context) {
	agentID := c.Param("agentId")
	deviceCode := c.Param("deviceCode")

	if agentID == "" || deviceCode == "" {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  "智能体ID和设备验证码不能为空",
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

	err := h.deviceService.ActivateDevice(c.Request.Context(), userID, agentID, deviceCode)
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

// getUserDevices 获取用户设备列表
// @Summary 获取用户设备列表
// @Description 获取当前用户在指定智能体下的设备列表。系统密钥可获取所有设备，普通用户只能获取自己的设备
// @Tags device
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param agentId path string true "智能体ID"
// @Success 200 {object} models.CommonResponse{data=[]models.DeviceEntity} "获取成功"
// @Failure 401 {object} models.CommonResponse "未授权"
// @Failure 500 {object} models.CommonResponse "服务器错误"
// @Router /device/bind/{agentId} [get]
func (h *DeviceHandlers) getUserDevices(c *gin.Context) {
	agentID := c.Param("agentId")

	// 检查是否为系统调用
	isSystemCall := middleware.IsSystemCallFromContext(c)

	if isSystemCall {
		// 系统调用：获取所有设备
		devices, err := h.deviceService.GetAllDevices(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusOK, &models.CommonResponse{
				Code: 1,
				Msg:  "获取设备列表失败: " + err.Error(),
			})
			return
		}

		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 0,
			Msg:  "success",
			Data: devices,
		})
		return
	}

	// 普通用户调用：获取用户设备
	userID, exists := middleware.GetUserIDFromContext(c)
	if !exists {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 401,
			Msg:  "用户未认证",
		})
		return
	}

	if middleware.IsSuperAdminFromContext(c) {
		userID = 0
	}

	devices, err := h.deviceService.GetUserDevices(c.Request.Context(), userID, agentID)
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
		Data: devices,
	})
}

// getUserDevicesPage 分页获取用户设备
// @Summary 分页获取用户设备
// @Tags device
// @Produce json
// @Security BearerAuth
// @Param agentId path string true "智能体ID"
// @Param page query int false "页码，从1开始"
// @Param limit query int false "每页条数，默认10"
// @Param keywords query string false "搜索关键字（mac/别名）"
// @Success 200 {object} models.CommonResponse
// @Router /device/bind/{agentId}/page [get]
func (h *DeviceHandlers) getUserDevicesPage(c *gin.Context) {
	agentID := c.Param("agentId")

	userID, exists := middleware.GetUserIDFromContext(c)
	if !exists {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 401,
			Msg:  "用户未认证",
		})
		return
	}

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))
	keywords := c.Query("keywords")

	if middleware.IsSuperAdminFromContext(c) {
		userID = 0
	}

	resp, err := h.deviceService.PageUserDevices(c.Request.Context(), userID, agentID, page, limit, keywords)
	if err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{Code: 1, Msg: err.Error()})
		return
	}
	c.JSON(http.StatusOK, &models.CommonResponse{Code: 0, Msg: "success", Data: resp})
}

// unbindDevice 解绑设备
// @Summary 解绑设备
// @Description 解除设备与用户的绑定关系
// @Tags device
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param body body models.DeviceUnBindDTO true "解绑设备信息"
// @Success 200 {object} models.CommonResponse "解绑成功"
// @Failure 400 {object} models.CommonResponse "参数错误"
// @Failure 401 {object} models.CommonResponse "未授权"
// @Failure 500 {object} models.CommonResponse "服务器错误"
// @Router /device/unbind [post]
func (h *DeviceHandlers) unbindDevice(c *gin.Context) {
	var req models.DeviceUnBindDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  "参数错误: " + err.Error(),
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

	if middleware.IsSuperAdminFromContext(c) {
		userID = 0
	}

	err := h.deviceService.UnbindDevice(c.Request.Context(), userID, req.DeviceID)
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

// updateDevice 更新设备信息
// @Summary 更新设备信息
// @Description 更新指定设备的信息
// @Tags device
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "设备ID"
// @Param body body models.DeviceUpdateDTO true "设备更新信息"
// @Success 200 {object} models.CommonResponse "更新成功"
// @Failure 400 {object} models.CommonResponse "参数错误"
// @Failure 401 {object} models.CommonResponse "未授权"
// @Failure 500 {object} models.CommonResponse "服务器错误"
// @Router /device/{id} [put]
func (h *DeviceHandlers) updateDevice(c *gin.Context) {
	deviceID := c.Param("id")
	if deviceID == "" {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  "设备ID不能为空",
		})
		return
	}

	var req models.DeviceUpdateDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  "参数错误: " + err.Error(),
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

	err := h.deviceService.UpdateDevice(c.Request.Context(), userID, deviceID, &req)
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

// getDeviceInfo 获取设备详细信息（包含使用时长统计）
// @Summary 获取设备详细信息
// @Description 获取指定设备的详细信息，包含使用时长统计。系统密钥可查看任意设备，普通用户只能查看自己的设备
// @Tags device
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "设备ID"
// @Success 200 {object} models.CommonResponse{data=models.DeviceWithUserInfoDTO} "获取成功"
// @Failure 400 {object} models.CommonResponse "参数错误"
// @Failure 401 {object} models.CommonResponse "未授权"
// @Failure 403 {object} models.CommonResponse "无权限访问"
// @Failure 404 {object} models.CommonResponse "设备不存在"
// @Failure 500 {object} models.CommonResponse "服务器错误"
// @Router /device/info/{id} [get]
func (h *DeviceHandlers) getDeviceInfo(c *gin.Context) {
	deviceID := c.Param("id")
	if deviceID == "" {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  "设备ID不能为空",
		})
		return
	}

	// 检查是否为系统调用
	isSystemCall := middleware.IsSystemCallFromContext(c)

	device, err := h.deviceService.GetDeviceWithUserInfo(c.Request.Context(), deviceID)
	if err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  err.Error(),
		})
		return
	}

	if device == nil {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  "设备不存在",
		})
		return
	}

	// 如果不是系统调用，检查用户权限
	if !isSystemCall {
		userID, exists := middleware.GetUserIDFromContext(c)
		if !exists {
			c.JSON(http.StatusOK, &models.CommonResponse{
				Code: 401,
				Msg:  "用户未认证",
			})
			return
		}

		// 检查设备是否属于当前用户
		if device.UserID == nil || *device.UserID != userID {
			c.JSON(http.StatusOK, &models.CommonResponse{
				Code: 403,
				Msg:  "无权限访问该设备",
			})
			return
		}
	}

	c.JSON(http.StatusOK, &models.CommonResponse{
		Code: 0,
		Msg:  "success",
		Data: device,
	})
}

// manualAddDevice 手动添加设备
// @Summary 手动添加设备
// @Description 管理员手动添加设备到系统
// @Tags device
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param body body models.DeviceManualAddDTO true "设备添加信息"
// @Success 200 {object} models.CommonResponse "添加成功"
// @Failure 400 {object} models.CommonResponse "参数错误"
// @Failure 401 {object} models.CommonResponse "未授权"
// @Failure 500 {object} models.CommonResponse "服务器错误"
// @Router /device/manual-add [post]
func (h *DeviceHandlers) manualAddDevice(c *gin.Context) {
	var req models.DeviceManualAddDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  "参数错误: " + err.Error(),
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

	err := h.deviceService.ManualAddDevice(c.Request.Context(), userID, &req)
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

// pageDevicesForAdmin 分页查询所有设备（管理员功能）
// @Summary 分页查询所有设备
// @Description 管理员分页查询所有设备信息，包含用户信息
// @Tags device
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param page query int false "页码" default(1)
// @Param limit query int false "每页数量" default(10)
// @Param keywords query string false "搜索关键词"
// @Success 200 {object} models.CommonResponse{data=models.PageResult} "查询成功"
// @Failure 401 {object} models.CommonResponse "未授权"
// @Failure 500 {object} models.CommonResponse "服务器错误"
// @Router /device/admin/page [get]
func (h *DeviceHandlers) pageDevicesForAdmin(c *gin.Context) {
	pageStr := c.Query("page")
	limitStr := c.Query("limit")
	keywords := c.Query("keywords")

	page, _ := strconv.Atoi(pageStr)
	if page <= 0 {
		page = 1
	}

	limit, _ := strconv.Atoi(limitStr)
	if limit <= 0 {
		limit = 10
	}

	pageResp, err := h.deviceService.PageDevicesWithUserInfo(c.Request.Context(), page, limit, keywords)
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
		Data: map[string]interface{}{
			"total": pageResp.TotalCount,
			"list":  pageResp.List,
		},
	})
}

// deleteIfUnbound 管理员删除未绑定的设备
// @Summary 删除未绑定的设备
// @Description 仅当设备未绑定用户与智能体时允许删除
// @Tags admin-device
// @Produce json
// @Security BearerAuth
// @Param id path string true "设备ID"
// @Success 200 {object} models.CommonResponse "删除成功"
// @Failure 400 {object} models.CommonResponse "参数错误"
// @Failure 401 {object} models.CommonResponse "未授权"
// @Failure 500 {object} models.CommonResponse "服务器错误"
// @Router /admin/device/{id} [delete]
func (h *DeviceHandlers) deleteIfUnbound(c *gin.Context) {
	id := c.Param("id")
	if strings.TrimSpace(id) == "" {
		c.JSON(http.StatusOK, &models.CommonResponse{Code: 1, Msg: "设备ID不能为空"})
		return
	}

	// 权限：已使用管理员组中间件
	if err := h.deviceService.DeleteIfUnbound(c.Request.Context(), id); err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{Code: 1, Msg: err.Error()})
		return
	}

	c.JSON(http.StatusOK, &models.CommonResponse{Code: 0, Msg: "success"})
}

// forwardToMqttGateway godoc
// @Summary 转发POST请求到MQTT网关
// @Description 获取用户设备状态并转发到MQTT网关
// @Tags device
// @Accept json
// @Produce json
// @Param agentId path string true "智能体ID"
// @Param body body string false "请求体"
// @Success 200 {object} models.CommonResponse "成功"
// @Failure 401 {object} models.CommonResponse "未授权"
// @Failure 500 {object} models.CommonResponse "内部错误"
// @Router /device/bind/{agentId} [post]
func (h *DeviceHandlers) forwardToMqttGateway(c *gin.Context) {
	agentID := c.Param("agentId")

	// 从认证中间件中获取用户ID
	userID, exists := middleware.GetUserIDFromContext(c)
	if !exists {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 401,
			Msg:  "用户未认证",
		})
		return
	}

	// 从系统参数中获取MQTT网关地址
	mqttGatewayURL, err := h.paramsService.GetValue(c.Request.Context(), "server.mqtt_manager_api", "")
	if err != nil || mqttGatewayURL == "" || mqttGatewayURL == "null" {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 0,
			Msg:  "success",
		})
		return
	}

	// 获取当前用户的设备列表
	devices, err := h.deviceService.GetUserDevices(c.Request.Context(), userID, agentID)
	if err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  "获取设备列表失败: " + err.Error(),
		})
		return
	}

	// 构建deviceIds数组
	var deviceIDs []string
	for _, device := range devices {
		macAddress := device.MacAddress
		if macAddress == "" {
			macAddress = "unknown"
		}

		groupID := device.Board
		if groupID == "" {
			groupID = "GID_default"
		}

		// 替换冒号为下划线
		groupID = strings.ReplaceAll(groupID, ":", "_")
		macAddress = strings.ReplaceAll(macAddress, ":", "_")

		// 构建mqtt客户端ID格式：groupId@@@macAddress@@@macAddress
		mqttClientID := fmt.Sprintf("%s@@@%s@@@%s", groupID, macAddress, macAddress)
		deviceIDs = append(deviceIDs, mqttClientID)
	}

	// 构建完整的URL
	url := fmt.Sprintf("http://%s/api/devices/status", mqttGatewayURL)

	// 生成Bearer令牌
	token, err := h.generateBearerToken(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  "令牌生成失败",
		})
		return
	}

	// 构建请求体JSON
	requestData := map[string]interface{}{
		"clientIds": deviceIDs,
	}
	jsonBody, err := json.Marshal(requestData)
	if err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  "构建请求体失败: " + err.Error(),
		})
		return
	}

	// 创建HTTP请求
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonBody))
	if err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  "创建请求失败: " + err.Error(),
		})
		return
	}

	// 设置请求头
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	// 发送HTTP请求
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  "转发请求失败: " + err.Error(),
		})
		return
	}
	defer resp.Body.Close()

	// 读取响应
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  "读取响应失败: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, &models.CommonResponse{
		Code: 0,
		Msg:  "success",
		Data: string(body),
	})
}

// generateBearerToken 生成Bearer令牌
func (h *DeviceHandlers) generateBearerToken(ctx context.Context) (string, error) {
	// 获取当前日期，格式为yyyy-MM-dd
	dateStr := time.Now().Format("2006-01-02")

	// 获取MQTT签名密钥
	signatureKey, err := h.paramsService.GetValue(ctx, "server.mqtt_signature_key", "")
	if err != nil || signatureKey == "" {
		return "", fmt.Errorf("MQTT签名密钥未配置")
	}

	// 将日期字符串与MQTT_SIGNATURE_KEY连接
	tokenContent := dateStr + signatureKey

	// 对连接后的字符串进行SHA256哈希计算
	hash := sha256.Sum256([]byte(tokenContent))
	token := fmt.Sprintf("%x", hash)

	return token, nil
}

// reportDeviceStatus 上报设备状态和使用时长
// @Summary 上报设备状态
// @Description 设备上报使用时长和在线状态。支持系统密钥和用户token认证
// @Tags device
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param body body models.DeviceStatusReportDTO true "设备状态信息"
// @Success 200 {object} models.CommonResponse "上报成功"
// @Failure 400 {object} models.CommonResponse "参数错误"
// @Failure 401 {object} models.CommonResponse "未授权"
// @Failure 404 {object} models.CommonResponse "设备不存在"
// @Failure 500 {object} models.CommonResponse "服务器错误"
// @Router /device/report-status [post]
func (h *DeviceHandlers) reportDeviceStatus(c *gin.Context) {
	var req models.DeviceStatusReportDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  "请求参数错误: " + err.Error(),
		})
		return
	}

	// 调用服务层进行状态上报
	err := h.deviceService.ReportDeviceStatus(c.Request.Context(), &req)
	if err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, &models.CommonResponse{
		Code: 0,
		Msg:  "设备状态上报成功",
	})
}
