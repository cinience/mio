package api

import (
	"context"
	"net/http"

	"manager-server/internal/middleware"
	"manager-server/internal/models"
	"manager-server/internal/service"

	"github.com/gin-gonic/gin"
)

// TimbreHandlers 音色管理处理器
type TimbreHandlers struct {
	timbreService service.TimbreService
}

// NewTimbreHandlers 创建音色管理处理器实例
func NewTimbreHandlers(timbreService service.TimbreService) *TimbreHandlers {
	return &TimbreHandlers{
		timbreService: timbreService,
	}
}

// pageTimbre 分页查找音色
func (h *TimbreHandlers) pageTimbre(c *gin.Context) {
	var dto models.TimbrePageDTO

	// 从查询参数获取数据
	dto.TTSModelID = c.Query("ttsModelId")
	dto.Name = c.Query("name")
	dto.Page = c.Query("page")
	dto.Limit = c.Query("limit")

	// 验证必需参数
	if dto.TTSModelID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": http.StatusBadRequest,
			"msg":  "TTS模型ID不能为空",
		})
		return
	}

	result, err := h.timbreService.Page(context.Background(), &dto)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.CommonResponse{
			Code: http.StatusInternalServerError,
			Msg:  "查询音色列表失败: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, models.CommonResponse{
		Code: 0,
		Msg:  "success",
		Data: map[string]interface{}{
			"total": result.TotalCount,
			"list":  result.List,
		},
	})
}

// saveTimbre 音色保存
func (h *TimbreHandlers) saveTimbre(c *gin.Context) {
	var dto models.TimbreDataDTO
	if err := c.ShouldBindJSON(&dto); err != nil {
		c.JSON(http.StatusBadRequest, models.CommonResponse{
			Code: http.StatusBadRequest,
			Msg:  "请求参数错误: " + err.Error(),
		})
		return
	}

	// 从认证中间件中获取用户ID
	userID, exists := middleware.GetUserIDFromContext(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, models.CommonResponse{
			Code: http.StatusUnauthorized,
			Msg:  "用户未认证",
		})
		return
	}

	err := h.timbreService.Save(context.Background(), &dto, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.CommonResponse{
			Code: http.StatusInternalServerError,
			Msg:  "保存音色失败: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, models.CommonResponse{Code: 0, Msg: "success"})
}

// updateTimbre 音色修改
func (h *TimbreHandlers) updateTimbre(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, models.CommonResponse{
			Code: http.StatusBadRequest,
			Msg:  "音色ID不能为空",
		})
		return
	}

	var dto models.TimbreDataDTO
	if err := c.ShouldBindJSON(&dto); err != nil {
		c.JSON(http.StatusBadRequest, models.CommonResponse{
			Code: http.StatusBadRequest,
			Msg:  "请求参数错误: " + err.Error(),
		})
		return
	}

	// 从认证中间件中获取用户ID
	userID, exists := middleware.GetUserIDFromContext(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, models.CommonResponse{
			Code: http.StatusUnauthorized,
			Msg:  "用户未认证",
		})
		return
	}

	err := h.timbreService.Update(context.Background(), id, &dto, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.CommonResponse{
			Code: http.StatusInternalServerError,
			Msg:  "修改音色失败: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, models.CommonResponse{Code: 0, Msg: "success"})
}

// deleteTimbre 音色删除
func (h *TimbreHandlers) deleteTimbre(c *gin.Context) {
	var ids []string
	if err := c.ShouldBindJSON(&ids); err != nil {
		c.JSON(http.StatusBadRequest, models.CommonResponse{
			Code: http.StatusBadRequest,
			Msg:  "请求参数错误: " + err.Error(),
		})
		return
	}

	if len(ids) == 0 {
		c.JSON(http.StatusBadRequest, models.CommonResponse{
			Code: http.StatusBadRequest,
			Msg:  "请选择要删除的音色",
		})
		return
	}

	err := h.timbreService.Delete(context.Background(), ids)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.CommonResponse{
			Code: http.StatusInternalServerError,
			Msg:  "删除音色失败: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, models.CommonResponse{Code: 0, Msg: "success"})
}
