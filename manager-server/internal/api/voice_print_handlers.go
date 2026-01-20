package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"manager-server/internal/middleware"
	"manager-server/internal/models"
	"manager-server/internal/service"
)

// VoicePrintHandlers 声纹处理器结构体
type VoicePrintHandlers struct {
	voicePrintService service.AgentVoicePrintService
}

// NewVoicePrintHandlers 创建声纹处理器实例
func NewVoicePrintHandlers(voicePrintService service.AgentVoicePrintService) *VoicePrintHandlers {
	return &VoicePrintHandlers{
		voicePrintService: voicePrintService,
	}
}

// createVoicePrint 创建声纹
func (h *VoicePrintHandlers) createVoicePrint(c *gin.Context) {
	var req models.AgentVoicePrintSaveDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  "参数错误: " + err.Error(),
		})
		return
	}

	// 从上下文获取用户ID
	userID, exists := middleware.GetUserIDFromContext(c)
	if !exists {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  "用户未登录",
		})
		return
	}

	err := h.voicePrintService.CreateVoicePrint(c.Request.Context(), &req, userID)
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

// getVoicePrintList 获取声纹列表
func (h *VoicePrintHandlers) getVoicePrintList(c *gin.Context) {
	agentID := c.Param("id")
	if agentID == "" {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  "智能体ID不能为空",
		})
		return
	}

	// 从上下文获取用户ID
	userID, exists := middleware.GetUserIDFromContext(c)
	if !exists {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  "用户未登录",
		})
		return
	}

	voicePrintList, err := h.voicePrintService.GetVoicePrintList(c.Request.Context(), agentID, userID)
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
		Data: voicePrintList,
	})
}

// updateVoicePrint 更新声纹
func (h *VoicePrintHandlers) updateVoicePrint(c *gin.Context) {
	var req models.AgentVoicePrintUpdateDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  "参数错误: " + err.Error(),
		})
		return
	}

	// 从上下文获取用户ID
	userID, exists := middleware.GetUserIDFromContext(c)
	if !exists {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  "用户未登录",
		})
		return
	}

	err := h.voicePrintService.UpdateVoicePrint(c.Request.Context(), &req, userID)
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

// deleteVoicePrint 删除声纹
func (h *VoicePrintHandlers) deleteVoicePrint(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  "声纹ID不能为空",
		})
		return
	}

	// 从上下文获取用户ID
	userID, exists := middleware.GetUserIDFromContext(c)
	if !exists {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  "用户未登录",
		})
		return
	}

	err := h.voicePrintService.DeleteVoicePrint(c.Request.Context(), id, userID)
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
