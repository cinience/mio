package api

import (
	"net/http"
	"strconv"
	"manager-server/internal/ginwrapper"

	"github.com/gin-gonic/gin"

	"manager-server/internal/middleware"
	"manager-server/internal/models"
	"manager-server/internal/service"
)

// ModelHandlers 模型处理器结构体
type ModelHandlers struct {
	providerService service.AIModelProviderService
	configService   service.AIModelConfigService
	voiceService    service.AITTSVoiceService
}

// NewModelHandlers 创建模型处理器实例
func NewModelHandlers(providerService service.AIModelProviderService, configService service.AIModelConfigService, voiceService service.AITTSVoiceService) *ModelHandlers {
	return &ModelHandlers{
		providerService: providerService,
		configService:   configService,
		voiceService:    voiceService,
	}
}

// ==================== 模型基本信息接口 ====================

// getModelNames 获取所有模型名称
func (h *ModelHandlers) getModelNames(c *gin.Context) {
	modelType := c.Query("modelType")
	modelName := c.Query("modelName")

	if modelType == "" {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  "modelType不能为空",
		})
		return
	}

	modelList, err := h.configService.GetModelNames(c.Request.Context(), modelType, modelName)
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
		Data: modelList,
	})
}

// getLlmModelNames 获取LLM模型信息
func (h *ModelHandlers) getLlmModelNames(c *gin.Context) {
	modelName := c.Query("modelName")

	llmModels, err := h.configService.GetLLMModels(c.Request.Context(), modelName)
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
		Data: llmModels,
	})
}

// getModelProviderList 获取模型供应器列表
func (h *ModelHandlers) getModelProviderList(c *gin.Context) {
	modelType := c.Param("modelId")
	if modelType == "" {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  "modelType不能为空",
		})
		return
	}

	providers, err := h.providerService.GetProvidersByType(c.Request.Context(), modelType)
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
		Data: providers,
	})
}

// ==================== 模型配置管理接口 ====================

// getModelConfigList 获取模型配置列表
func (h *ModelHandlers) getModelConfigList(c *gin.Context) {
	modelType := c.Query("modelType")
	modelName := c.Query("modelName")
	pageStr := c.DefaultQuery("page", "1")
	limitStr := c.DefaultQuery("limit", "10")

	if modelType == "" {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  "modelType不能为空",
		})
		return
	}

	page, _ := strconv.Atoi(pageStr)
	limit, _ := strconv.Atoi(limitStr)

	pageResp, err := h.configService.PageConfigs(c.Request.Context(), page, limit, modelType, modelName)
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

// getModelConfig 获取模型配置详情
func (h *ModelHandlers) getModelConfig(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  "id不能为空",
		})
		return
	}

	config, err := h.configService.GetConfig(c.Request.Context(), id)
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
		Data: config,
	})
}

// getModelById 根据模型ID获取模型配置 (与Java项目/{id}路径保持一致)
func (h *ModelHandlers) getModelById(c *gin.Context) {
	id := c.Param("modelId")
	if id == "" {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  "id不能为空",
		})
		return
	}

	config, err := h.configService.GetConfig(c.Request.Context(), id)
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
		Data: config,
	})
}

// addModelConfig 新增模型配置
func (h *ModelHandlers) addModelConfig(c *gin.Context) {
	modelType := c.Param("modelId")
	provideCode := c.Param("provideCode")

	if modelType == "" || provideCode == "" {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  "modelType和provideCode不能为空",
		})
		return
	}

	var req models.ModelConfigBodyDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  "参数错误: " + err.Error(),
		})
		return
	}

	configDTO, err := h.configService.CreateConfig(c.Request.Context(), modelType, provideCode, &req)
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
		Data: configDTO,
	})
}

// editModelConfig 编辑模型配置
func (h *ModelHandlers) editModelConfig(c *gin.Context) {
	modelType := c.Param("modelId")
	provideCode := c.Param("provideCode")
	id := c.Param("id")

	if modelType == "" || provideCode == "" || id == "" {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  "modelType、provideCode和id不能为空",
		})
		return
	}

	var req models.ModelConfigBodyDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  "参数错误: " + err.Error(),
		})
		return
	}

	configDTO, err := h.configService.UpdateConfig(c.Request.Context(), modelType, provideCode, id, &req)
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
		Data: configDTO,
	})
}

// deleteModelConfig 删除模型配置
func (h *ModelHandlers) deleteModelConfig(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  "id不能为空",
		})
		return
	}

	err := h.configService.DeleteConfig(c.Request.Context(), id)
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

// enableModelConfig 启用/关闭模型配置
func (h *ModelHandlers) enableModelConfig(c *gin.Context) {
	id := c.Param("id")
	statusStr := c.Param("status")

	if id == "" || statusStr == "" {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  "id和status不能为空",
		})
		return
	}

	status, err := strconv.Atoi(statusStr)
	if err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  "status格式错误",
		})
		return
	}

	err = h.configService.EnableConfig(c.Request.Context(), id, status)
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

// setDefaultModel 设置默认模型
func (h *ModelHandlers) setDefaultModel(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  "id不能为空",
		})
		return
	}

	err := h.configService.SetDefaultModel(c.Request.Context(), id)
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

// getVoiceList 获取模型音色
func (h *ModelHandlers) getVoiceList(c *gin.Context) {
	modelID := c.Param("modelId")
	voiceName := c.Query("voiceName")

	if modelID == "" {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  "modelId不能为空",
		})
		return
	}

	voices, err := h.voiceService.GetVoicesByModelID(c.Request.Context(), modelID, voiceName)
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
		Data: voices,
	})
}

// ==================== 模型供应器管理接口 ====================

// getProviderListPage 获取模型供应器分页列表
func (h *ModelHandlers) getProviderListPage(c *gin.Context) {
	modelType := c.Query("modelType")
	pageStr := c.DefaultQuery("page", "1")
	limitStr := c.DefaultQuery("limit", "10")

	page, _ := strconv.Atoi(pageStr)
	limit, _ := strconv.Atoi(limitStr)

	pageResp, err := h.providerService.PageProviders(c.Request.Context(), page, limit, modelType)
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

// addProvider 新增模型供应器
func (h *ModelHandlers) addProvider(c *gin.Context) {
	var req models.ModelProviderDTO
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
		c.JSON(http.StatusUnauthorized, models.CommonResponse{
			Code: http.StatusUnauthorized,
			Msg:  "用户未认证",
		})
		return
	}
	// 设置创建者ID
	req.Creator = &userID

	err := h.providerService.CreateProvider(c.Request.Context(), &req)
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
		Data: req,
	})
}

// editProvider 修改模型供应器
func (h *ModelHandlers) editProvider(c *gin.Context) {
	var req models.ModelProviderDTO
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
		c.JSON(http.StatusUnauthorized, models.CommonResponse{
			Code: http.StatusUnauthorized,
			Msg:  "用户未认证",
		})
		return
	}
	// 设置更新者ID
	req.Updater = &userID

	err := h.providerService.UpdateProvider(c.Request.Context(), &req)
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
		Data: req,
	})
}

// deleteProvider 删除模型供应器
func (h *ModelHandlers) deleteProvider(c *gin.Context) {
	var ids []string
	if err := c.ShouldBindJSON(&ids); err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  "参数错误: " + err.Error(),
		})
		return
	}

	err := h.providerService.DeleteProviders(c.Request.Context(), ids)
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

// getPluginNameList 获取插件名称列表
func (h *ModelHandlers) getPluginNameList(c *gin.Context) {
	plugins, err := h.providerService.GetPluginList(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  err.Error(),
		})
		return
	}
	/*
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 0,
			Msg:  "success",
			Data: plugins,
		})

		return

	*/

	ginwrapper.JSON(c, http.StatusOK, &models.CommonResponse{
		Code: 0,
		Msg:  "success",
		Data: plugins,
	})
}
