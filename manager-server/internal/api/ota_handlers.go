package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"manager-server/internal/logger"
	"manager-server/internal/models"
	"manager-server/internal/service"

	"github.com/gin-gonic/gin"
)

// OTAHandlers OTA处理器
type OTAHandlers struct {
	deviceService service.DeviceService
	paramsService service.SysParamsService
}

// NewOTAHandlers 创建OTA处理器
func NewOTAHandlers(deviceService service.DeviceService, paramsService service.SysParamsService) *OTAHandlers {
	return &OTAHandlers{
		deviceService: deviceService,
		paramsService: paramsService,
	}
}

// checkOTAVersion OTA版本和设备激活状态检查
func (h *OTAHandlers) checkOTAVersion(c *gin.Context) {
	deviceID := c.GetHeader("Device-Id")
	clientID := c.GetHeader("Client-Id")

	if deviceID == "" {
		c.JSON(http.StatusOK, models.CreateErrorResponse("Device ID is required"))
		return
	}

	if clientID == "" {
		clientID = deviceID
	}

	// 验证MAC地址格式
	if !h.isMacAddressValid(deviceID) {
		logger.Warnf("Invalid device ID: %s", deviceID)
		c.JSON(http.StatusOK, models.CreateErrorResponse("Invalid device ID"))
		return
	}

	var req models.DeviceReportReqDTO
	//if err := c.ShouldBindJSON(&req); err != nil {
	//	logger.Warnf("Invalid OTA request err: %v", err)
	//}
	body, _ := io.ReadAll(c.Request.Body)
	logger.Infof("OTA版本和设备激活状态检查 请求：%s", string(body))
	err := json.Unmarshal(body, &req)
	if err != nil {
		logger.Warnf("Invalid OTA request err: %v", err)
	}

	response, err := h.deviceService.CheckDeviceActive(c.Request.Context(), deviceID, clientID, &req)
	if err != nil {
		logger.Warnf("Error checking device active: %v", err)
		c.JSON(http.StatusOK, models.CreateErrorResponse(err.Error()))
		return
	}

	if response.Websocket.URL == "auto" {
		scheme := "ws"
		// 检查 'X-Forwarded-Proto' 头，这在反向代理后很常见
		if c.GetHeader("X-Forwarded-Proto") == "https" {
			scheme = "wss"
		} else if c.Request.TLS != nil {
			// 如果直接处理 TLS 连接
			scheme = "wss"
		}
		response.Websocket.URL = fmt.Sprintf("%s://%s/xiaozhi/v1/ws", scheme, c.Request.Host)
	}
	logger.Infof("OTA版本和设备激活状态检查 响应：%+v", response)

	c.JSON(http.StatusOK, response)
}

// activateDevice 设备快速检查激活状态
func (h *OTAHandlers) activateDevice(c *gin.Context) {
	deviceID := c.GetHeader("Device-Id")

	if deviceID == "" {
		c.JSON(http.StatusAccepted, gin.H{})
		return
	}

	device, err := h.deviceService.GetDeviceByMacAddress(c.Request.Context(), deviceID)
	if err != nil || device == nil {
		c.JSON(http.StatusAccepted, gin.H{})
		return
	}

	c.JSON(http.StatusOK, "success")
}

// getOTAStatus 获取OTA状态
func (h *OTAHandlers) getOTAStatus(c *gin.Context) {
	// 检查WebSocket配置
	wsUrl, err := h.paramsService.GetValue(c.Request.Context(), "server.websocket", "")
	if err != nil || wsUrl == "" || wsUrl == "null" {
		logger.Warnf("OTA接口不正常，缺少websocket地址，请登录智控台，在参数管理找到【server.websocket】配置")
		c.JSON(http.StatusOK, "OTA接口不正常，缺少websocket地址，请登录智控台，在参数管理找到【server.websocket】配置")
		return
	}

	// 检查OTA配置
	//otaUrl, err := h.paramsService.GetValue(c.Request.Context(), "server.ota", "")
	//if err != nil || otaUrl == "" || otaUrl == "null" {
	//	logger.Warnf("OTA接口不正常，缺少ota地址，请登录智控台，在参数管理找到【server.ota】配置")
	//	c.JSON(http.StatusOK, "OTA接口不正常，缺少ota地址，请登录智控台，在参数管理找到【server.ota】配置")
	//	return
	//}

	// 计算WebSocket集群数量
	wsCount := len(strings.Split(wsUrl, ";"))

	c.Writer.Header().Add("Content-Type", "text/html; charset=utf-8")
	body := "OTA接口运行正常，websocket集群数量：" + strconv.Itoa(wsCount)

	logger.Infof("OTA接口运行正常，websocket集群数量：%d", wsCount)
	c.String(http.StatusOK, body)
}

// isMacAddressValid 验证MAC地址格式
func (h *OTAHandlers) isMacAddressValid(macAddress string) bool {
	// MAC地址格式验证：xx:xx:xx:xx:xx:xx
	macRegex := regexp.MustCompile(`^([0-9A-Fa-f]{2}[:-]){5}([0-9A-Fa-f]{2})$`)
	return macRegex.MatchString(macAddress)
}
