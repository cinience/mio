package api

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"manager-server/internal/middleware"
	"manager-server/internal/models"
	"manager-server/internal/service"
)

// VisionHandlers 视觉配置与事件处理器
type VisionHandlers struct {
	agentService  service.AgentService
	visionService service.VisionService
}

func NewVisionHandlers(agentService service.AgentService, visionService service.VisionService) *VisionHandlers {
	return &VisionHandlers{
		agentService:  agentService,
		visionService: visionService,
	}
}

func (h *VisionHandlers) authorizeAgent(c *gin.Context, agentID string) bool {
	if agentID == "" {
		c.JSON(http.StatusBadRequest, models.CommonResponse{Code: http.StatusBadRequest, Msg: "智能体ID不能为空"})
		return false
	}
	if middleware.IsSuperAdminFromContext(c) {
		return true
	}
	userID, exists := middleware.GetUserIDFromContext(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, models.CommonResponse{Code: http.StatusUnauthorized, Msg: "用户未认证"})
		return false
	}
	allowed, err := h.agentService.CheckAgentPermission(c.Request.Context(), agentID, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.CommonResponse{Code: http.StatusInternalServerError, Msg: err.Error()})
		return false
	}
	if !allowed {
		c.JSON(http.StatusForbidden, models.CommonResponse{Code: http.StatusForbidden, Msg: "没有权限操作此智能体"})
		return false
	}
	return true
}

// listSources 获取视觉来源列表
func (h *VisionHandlers) listSources(c *gin.Context) {
	agentID := c.Param("id")
	if !h.authorizeAgent(c, agentID) {
		return
	}
	items, err := h.visionService.ListSources(c.Request.Context(), agentID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.CommonResponse{Code: http.StatusInternalServerError, Msg: err.Error()})
		return
	}
	c.JSON(http.StatusOK, models.CommonResponse{Code: 0, Msg: "success", Data: items})
}

// createSource 创建视觉来源
func (h *VisionHandlers) createSource(c *gin.Context) {
	agentID := c.Param("id")
	if !h.authorizeAgent(c, agentID) {
		return
	}
	var dto models.VisionSourceCreateDTO
	if err := c.ShouldBindJSON(&dto); err != nil {
		c.JSON(http.StatusBadRequest, models.CommonResponse{Code: http.StatusBadRequest, Msg: "请求参数错误: " + err.Error()})
		return
	}
	item, err := h.visionService.CreateSource(c.Request.Context(), agentID, &dto)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.CommonResponse{Code: http.StatusInternalServerError, Msg: err.Error()})
		return
	}
	c.JSON(http.StatusOK, models.CommonResponse{Code: 0, Msg: "success", Data: item})
}

// updateSource 更新视觉来源
func (h *VisionHandlers) updateSource(c *gin.Context) {
	agentID := c.Param("id")
	if !h.authorizeAgent(c, agentID) {
		return
	}
	sourceID := c.Param("sourceId")
	if sourceID == "" {
		c.JSON(http.StatusBadRequest, models.CommonResponse{Code: http.StatusBadRequest, Msg: "来源ID不能为空"})
		return
	}
	var dto models.VisionSourceUpdateDTO
	if err := c.ShouldBindJSON(&dto); err != nil {
		c.JSON(http.StatusBadRequest, models.CommonResponse{Code: http.StatusBadRequest, Msg: "请求参数错误: " + err.Error()})
		return
	}
	item, err := h.visionService.UpdateSource(c.Request.Context(), sourceID, &dto)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.CommonResponse{Code: http.StatusInternalServerError, Msg: err.Error()})
		return
	}
	c.JSON(http.StatusOK, models.CommonResponse{Code: 0, Msg: "success", Data: item})
}

// deleteSource 删除视觉来源
func (h *VisionHandlers) deleteSource(c *gin.Context) {
	agentID := c.Param("id")
	if !h.authorizeAgent(c, agentID) {
		return
	}
	sourceID := c.Param("sourceId")
	if sourceID == "" {
		c.JSON(http.StatusBadRequest, models.CommonResponse{Code: http.StatusBadRequest, Msg: "来源ID不能为空"})
		return
	}
	if err := h.visionService.DeleteSource(c.Request.Context(), sourceID); err != nil {
		c.JSON(http.StatusInternalServerError, models.CommonResponse{Code: http.StatusInternalServerError, Msg: err.Error()})
		return
	}
	c.JSON(http.StatusOK, models.CommonResponse{Code: 0, Msg: "success"})
}

// listRules 获取视觉规则列表
func (h *VisionHandlers) listRules(c *gin.Context) {
	agentID := c.Param("id")
	if !h.authorizeAgent(c, agentID) {
		return
	}
	items, err := h.visionService.ListRules(c.Request.Context(), agentID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.CommonResponse{Code: http.StatusInternalServerError, Msg: err.Error()})
		return
	}
	c.JSON(http.StatusOK, models.CommonResponse{Code: 0, Msg: "success", Data: items})
}

// createRule 创建视觉规则
func (h *VisionHandlers) createRule(c *gin.Context) {
	agentID := c.Param("id")
	if !h.authorizeAgent(c, agentID) {
		return
	}
	var dto models.VisionRuleCreateDTO
	if err := c.ShouldBindJSON(&dto); err != nil {
		c.JSON(http.StatusBadRequest, models.CommonResponse{Code: http.StatusBadRequest, Msg: "请求参数错误: " + err.Error()})
		return
	}
	item, err := h.visionService.CreateRule(c.Request.Context(), agentID, &dto)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.CommonResponse{Code: http.StatusInternalServerError, Msg: err.Error()})
		return
	}
	c.JSON(http.StatusOK, models.CommonResponse{Code: 0, Msg: "success", Data: item})
}

// updateRule 更新视觉规则
func (h *VisionHandlers) updateRule(c *gin.Context) {
	agentID := c.Param("id")
	if !h.authorizeAgent(c, agentID) {
		return
	}
	ruleID := c.Param("ruleId")
	if ruleID == "" {
		c.JSON(http.StatusBadRequest, models.CommonResponse{Code: http.StatusBadRequest, Msg: "规则ID不能为空"})
		return
	}
	var dto models.VisionRuleUpdateDTO
	if err := c.ShouldBindJSON(&dto); err != nil {
		c.JSON(http.StatusBadRequest, models.CommonResponse{Code: http.StatusBadRequest, Msg: "请求参数错误: " + err.Error()})
		return
	}
	item, err := h.visionService.UpdateRule(c.Request.Context(), ruleID, &dto)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.CommonResponse{Code: http.StatusInternalServerError, Msg: err.Error()})
		return
	}
	c.JSON(http.StatusOK, models.CommonResponse{Code: 0, Msg: "success", Data: item})
}

// deleteRule 删除视觉规则
func (h *VisionHandlers) deleteRule(c *gin.Context) {
	agentID := c.Param("id")
	if !h.authorizeAgent(c, agentID) {
		return
	}
	ruleID := c.Param("ruleId")
	if ruleID == "" {
		c.JSON(http.StatusBadRequest, models.CommonResponse{Code: http.StatusBadRequest, Msg: "规则ID不能为空"})
		return
	}
	if err := h.visionService.DeleteRule(c.Request.Context(), ruleID); err != nil {
		c.JSON(http.StatusInternalServerError, models.CommonResponse{Code: http.StatusInternalServerError, Msg: err.Error()})
		return
	}
	c.JSON(http.StatusOK, models.CommonResponse{Code: 0, Msg: "success"})
}

// queryEvents 查询视觉事件
func (h *VisionHandlers) queryEvents(c *gin.Context) {
	agentID := c.Param("id")
	if !h.authorizeAgent(c, agentID) {
		return
	}
	var dto models.VisionEventQueryDTO
	if err := c.ShouldBindJSON(&dto); err != nil {
		c.JSON(http.StatusBadRequest, models.CommonResponse{Code: http.StatusBadRequest, Msg: "请求参数错误: " + err.Error()})
		return
	}
	items, total, err := h.visionService.QueryEvents(c.Request.Context(), agentID, &dto)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.CommonResponse{Code: http.StatusInternalServerError, Msg: err.Error()})
		return
	}
	c.JSON(http.StatusOK, models.CommonResponse{Code: 0, Msg: "success", Data: gin.H{"items": items, "total": total}})
}

// getEvent 获取视觉事件详情
func (h *VisionHandlers) getEvent(c *gin.Context) {
	agentID := c.Param("id")
	if !h.authorizeAgent(c, agentID) {
		return
	}
	eventID := c.Param("eventId")
	if eventID == "" {
		c.JSON(http.StatusBadRequest, models.CommonResponse{Code: http.StatusBadRequest, Msg: "事件ID不能为空"})
		return
	}
	item, err := h.visionService.GetEvent(c.Request.Context(), eventID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.CommonResponse{Code: http.StatusInternalServerError, Msg: err.Error()})
		return
	}
	c.JSON(http.StatusOK, models.CommonResponse{Code: 0, Msg: "success", Data: item})
}

// reportEvent 供后台服务上报视觉事件（server secret）
func (h *VisionHandlers) reportEvent(c *gin.Context) {
	var dto models.VisionEventCreateDTO
	if err := c.ShouldBindJSON(&dto); err != nil {
		c.JSON(http.StatusBadRequest, models.CommonResponse{Code: http.StatusBadRequest, Msg: "请求参数错误: " + err.Error()})
		return
	}
	item, err := h.visionService.CreateEvent(c.Request.Context(), &dto)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.CommonResponse{Code: http.StatusInternalServerError, Msg: err.Error()})
		return
	}
	c.JSON(http.StatusOK, models.CommonResponse{Code: 0, Msg: "success", Data: item})
}

// listAgentConfigs 内部接口：列出视觉智能体完整配置（server secret）
func (h *VisionHandlers) listAgentConfigs(c *gin.Context) {
	agentType := strings.TrimSpace(c.Query("agentType"))
	if agentType == "" {
		agentType = "vision-primary"
	}
	agents, err := h.agentService.ListAgentsByType(c.Request.Context(), agentType)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.CommonResponse{Code: http.StatusInternalServerError, Msg: err.Error()})
		return
	}
	items := make([]*models.VisionAgentConfig, 0, len(agents))
	for _, agent := range agents {
		sources, _ := h.visionService.ListSourcesForSync(c.Request.Context(), agent.ID)
		rules, _ := h.visionService.ListRules(c.Request.Context(), agent.ID)
		agentTypeValue := strings.TrimSpace(agent.AgentType)
		if agentTypeValue == "" {
			agentTypeValue = "assistant"
		}
		items = append(items, &models.VisionAgentConfig{
			AgentID:      agent.ID,
			AgentName:    agent.AgentName,
			AgentType:    agentTypeValue,
			SystemPrompt: agent.SystemPrompt,
			Sources:      sources,
			Rules:        rules,
		})
	}
	c.JSON(http.StatusOK, models.CommonResponse{Code: 0, Msg: "success", Data: items})
}
