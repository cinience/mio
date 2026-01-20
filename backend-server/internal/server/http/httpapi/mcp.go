package httpapi

import (
	"net/http"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"

	log "backend-server/internal/infrastructure/logger"
)

func (h *Handler) handleGetDeviceTools(c *gin.Context) {
	devicePath := strings.Trim(c.Param("deviceID"), "/")
	if devicePath == "" {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "设备ID不能为空"})
		return
	}

	if h.mcpManager == nil {
		log.Warn("MCP 管理器未初始化，无法返回工具列表")
		c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": "MCP 管理器未初始化"})
		return
	}

	tools := h.mcpManager.GetAllTools()
	if len(tools) == 0 {
		c.JSON(http.StatusOK, gin.H{
			"deviceId": devicePath,
			"tools":    []string{},
			"count":    0,
		})
		return
	}

	names := make([]string, 0, len(tools))
	for name := range tools {
		names = append(names, name)
	}
	sort.Strings(names)

	c.JSON(http.StatusOK, gin.H{
		"deviceId": devicePath,
		"tools":    names,
		"count":    len(names),
	})
}
