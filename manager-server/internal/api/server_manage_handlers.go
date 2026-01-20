package api

import (
	"context"
	"net/http"

	"manager-server/internal/models"
	"manager-server/internal/service"

	"github.com/gin-gonic/gin"
)

// ServerManageHandlers 服务端管理处理器
type ServerManageHandlers struct {
	serverManageService service.ServerManageService
}

// NewServerManageHandlers 创建服务端管理处理器实例
func NewServerManageHandlers(serverManageService service.ServerManageService) *ServerManageHandlers {
	return &ServerManageHandlers{
		serverManageService: serverManageService,
	}
}

// getWsServerList 获取WebSocket服务端列表
func (h *ServerManageHandlers) getWsServerList(c *gin.Context) {
	servers, err := h.serverManageService.GetWsServerList(context.Background())
	if err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{Code: http.StatusInternalServerError, Msg: "获取服务端列表失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, &models.CommonResponse{Code: 0, Msg: "success", Data: servers})
}

// emitServerAction 通知Python服务端更新配置
func (h *ServerManageHandlers) emitServerAction(c *gin.Context) {
	var dto models.EmitServerActionDTO
	if err := c.ShouldBindJSON(&dto); err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{Code: http.StatusBadRequest, Msg: "请求参数错误: " + err.Error()})
		return
	}

	success, err := h.serverManageService.EmitServerAction(context.Background(), &dto)
	if err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{Code: http.StatusInternalServerError, Msg: "通知服务端失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, &models.CommonResponse{Code: 0, Msg: "success", Data: success})
}
