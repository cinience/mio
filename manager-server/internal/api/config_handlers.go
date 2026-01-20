package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"manager-server/internal/models"
	"manager-server/internal/service"

	"github.com/gin-gonic/gin"
)

// ConfigHandlers 配置管理处理器
type ConfigHandlers struct {
	configService service.ConfigService
}

// NewConfigHandlers 创建配置管理处理器实例
func NewConfigHandlers(configService service.ConfigService) *ConfigHandlers {
	return &ConfigHandlers{
		configService: configService,
	}
}

// getServerConfig 获取服务端配置
func (h *ConfigHandlers) getServerConfig(c *gin.Context) {
	config, err := h.configService.GetConfig(context.Background(), true)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": http.StatusInternalServerError,
			"msg":  "获取服务端配置失败: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "success",
		"data": config,
	})
}

// getAgentModels 获取智能体模型配置
func (h *ConfigHandlers) getAgentModels(c *gin.Context) {
	var dto models.AgentModelsDTO
	if err := c.ShouldBindJSON(&dto); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": http.StatusBadRequest,
			"msg":  "请求参数错误: " + err.Error(),
		})
		return
	}

	modelsResult, err := h.configService.GetAgentModels(context.Background(), &dto)
	if err != nil {
		if errors.Is(err, service.ErrDeviceNotFound) {
			c.JSON(http.StatusUnauthorized, gin.H{
				"code": http.StatusUnauthorized,
				"msg":  "获取智能体模型配置失败: " + err.Error(),
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": http.StatusInternalServerError,
			"msg":  "获取智能体模型配置失败: " + err.Error(),
		})
		return
	}
	_, exists := modelsResult["mcp_endpoint"]
	if !exists {
	}
	mcpEndpoint := modelsResult["mcp_endpoint"].(string)

	if strings.HasPrefix(mcpEndpoint, "auto") {
		schema := "https"
		if c.Request.TLS == nil {
			schema = "http"
		}
		mcpEndpoint = strings.Replace(mcpEndpoint, "auto", fmt.Sprintf("%s://%s", schema, c.Request.Host), 1)
		modelsResult["mcp_endpoint"] = mcpEndpoint
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "success",
		"data": modelsResult,
	})
}
