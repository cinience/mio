package api

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"manager-server/internal/ginwrapper"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"manager-server/internal/constants"
	"manager-server/internal/mcp"
	"manager-server/internal/middleware"
	"manager-server/internal/models"
	"manager-server/internal/service"
)

// 简易内存映射：uuid -> audioId
var agentAudioStore = struct {
	mu       sync.Mutex
	uuid2aid map[string]string
}{uuid2aid: map[string]string{}}

// AgentHandlers 智能体处理器
type AgentHandlers struct {
	agentService         service.AgentService
	templateService      service.AgentTemplateService
	chatHistoryService   service.AgentChatHistoryService
	pluginMappingService service.AgentPluginMappingService
	paramsService        service.SysParamsService
}

type agentMcpConnectionInfo struct {
	ConnectionID    string `json:"connectionId"`
	RemoteAddr      string `json:"remoteAddr,omitempty"`
	ConnectedAt     int64  `json:"connectedAt"`
	ConnectedAtISO  string `json:"connectedAtIso,omitempty"`
	DurationSeconds int64  `json:"durationSeconds"`
}

type agentMcpToolInfo struct {
	Name           string                 `json:"name"`
	Description    string                 `json:"description,omitempty"`
	InputSchema    map[string]interface{} `json:"inputSchema,omitempty"`
	ConnectionID   string                 `json:"connectionId,omitempty"`
	ConnectedAt    int64                  `json:"connectedAt,omitempty"`
	ConnectedAtISO string                 `json:"connectedAtIso,omitempty"`
	RemoteAddr     string                 `json:"remoteAddr,omitempty"`
	UpdatedAt      int64                  `json:"updatedAt,omitempty"`
	UpdatedAtISO   string                 `json:"updatedAtIso,omitempty"`
}

type agentMcpToolsPayload struct {
	Connections []agentMcpConnectionInfo `json:"connections"`
	Tools       []agentMcpToolInfo       `json:"tools"`
}

// NewAgentHandlers 创建智能体处理器实例
func NewAgentHandlers(
	agentService service.AgentService,
	templateService service.AgentTemplateService,
	chatHistoryService service.AgentChatHistoryService,
	pluginMappingService service.AgentPluginMappingService,
	paramsService service.SysParamsService,
) *AgentHandlers {
	return &AgentHandlers{
		agentService:         agentService,
		templateService:      templateService,
		chatHistoryService:   chatHistoryService,
		pluginMappingService: pluginMappingService,
		paramsService:        paramsService,
	}
}

// getUserAgents 获取用户智能体列表
func (h *AgentHandlers) getUserAgents(c *gin.Context) {
	// 从认证中间件中获取用户ID
	userID, exists := middleware.GetUserIDFromContext(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, models.CommonResponse{
			Code: http.StatusUnauthorized,
			Msg:  "用户未认证",
		})
		return
	}

	isSuperAdmin := middleware.IsSuperAdminFromContext(c)

	agents, err := h.agentService.GetUserAgents(context.Background(), userID, isSuperAdmin)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.CommonResponse{
			Code: http.StatusInternalServerError,
			Msg:  "获取智能体列表失败: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, models.CommonResponse{
		Code: 0,
		Msg:  "success",
		Data: agents,
	})
}

// adminAgentList 智能体列表（管理员）
func (h *AgentHandlers) adminAgentList(c *gin.Context) {
	page := 1
	limit := 10

	if pageStr := c.Query("page"); pageStr != "" {
		if p, err := strconv.Atoi(pageStr); err == nil && p > 0 {
			page = p
		}
	}

	if limitStr := c.Query("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	result, err := h.agentService.AdminAgentList(context.Background(), page, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.CommonResponse{
			Code: http.StatusInternalServerError,
			Msg:  "获取智能体列表失败: " + err.Error(),
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

// getAgentByID 获取智能体详情
func (h *AgentHandlers) getAgentByID(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, models.CommonResponse{
			Code: http.StatusBadRequest,
			Msg:  "智能体ID不能为空",
		})
		return
	}

	agent, err := h.agentService.GetAgentByID(context.Background(), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.CommonResponse{
			Code: http.StatusInternalServerError,
			Msg:  "获取智能体详情失败: " + err.Error(),
		})
		return
	}

	ginwrapper.JSON(c, http.StatusOK, models.CommonResponse{
		Code: 0,
		Msg:  "success",
		Data: agent,
	})
}

// createAgent 创建智能体
func (h *AgentHandlers) createAgent(c *gin.Context) {
	var dto models.AgentCreateDTO
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

	// 使用模板填充缺省字段，使行为与Java一致
	templates, err := h.templateService.GetTemplateList(c.Request.Context())
	if err == nil && len(templates) > 0 {
		var t *models.AgentTemplate
		normalizedType := strings.TrimSpace(dto.AgentType)
		if normalizedType != "" {
			for _, template := range templates {
				if strings.TrimSpace(template.AgentType) == normalizedType {
					t = template
					break
				}
			}
		} else {
			t = templates[0]
		}

		if t != nil {
			if dto.AsrModelID == "" {
				dto.AsrModelID = t.AsrModelID
			}
			if dto.VadModelID == "" {
				dto.VadModelID = t.VadModelID
			}
			if dto.LlmModelID == "" {
				dto.LlmModelID = t.LlmModelID
			}
			if dto.VllmModelID == "" {
				dto.VllmModelID = t.VllmModelID
			}
			if dto.AgentType == "" {
				dto.AgentType = t.AgentType
			}
			if dto.TtsModelID == "" {
				dto.TtsModelID = t.TtsModelID
			}
			if dto.TtsVoiceID == "" {
				dto.TtsVoiceID = t.TtsVoiceID
			}
			if dto.MemModelID == "" {
				dto.MemModelID = t.MemModelID
			}
			if dto.IntentModelID == "" {
				dto.IntentModelID = t.IntentModelID
			}
			if dto.SystemPrompt == "" {
				dto.SystemPrompt = t.SystemPrompt
			}
			if dto.SummaryMemory == "" {
				dto.SummaryMemory = t.SummaryMemory
			}
			if dto.ChatHistoryConf == nil {
				v := t.ChatHistoryConf
				dto.ChatHistoryConf = v
			}
			if dto.LangCode == "" {
				dto.LangCode = t.LangCode
			}
			if dto.Language == "" {
				dto.Language = t.Language
			}
			if dto.Sort == nil {
				dto.Sort = t.Sort
			}
		}
	}
	if dto.Sort == nil {
		v := 0
		dto.Sort = &v
	}

	agentID, err := h.agentService.CreateAgent(context.Background(), &dto, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.CommonResponse{
			Code: http.StatusInternalServerError,
			Msg:  "创建智能体失败: " + err.Error(),
		})
		return
	}

	ginwrapper.JSON(c, http.StatusOK, models.CommonResponse{
		Code: 0,
		Msg:  "success",
		Data: agentID,
	})
}

// updateAgentMemoryByMac 根据设备MAC地址更新智能体记忆
func (h *AgentHandlers) updateAgentMemoryByMac(c *gin.Context) {
	macAddress := c.Param("macAddress")
	if macAddress == "" {
		c.JSON(http.StatusBadRequest, models.CommonResponse{
			Code: http.StatusBadRequest,
			Msg:  "设备MAC地址不能为空",
		})
		return
	}

	var dto models.AgentMemoryDTO
	if err := c.ShouldBindJSON(&dto); err != nil {
		c.JSON(http.StatusBadRequest, models.CommonResponse{
			Code: http.StatusBadRequest,
			Msg:  "请求参数错误: " + err.Error(),
		})
		return
	}

	err := h.agentService.UpdateAgentMemoryByDeviceID(context.Background(), macAddress, &dto)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.CommonResponse{
			Code: http.StatusInternalServerError,
			Msg:  "更新智能体记忆失败: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, models.CommonResponse{
		Code: 0,
		Msg:  "success",
	})
}

// updateAgent 更新智能体
func (h *AgentHandlers) updateAgent(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, models.CommonResponse{
			Code: http.StatusBadRequest,
			Msg:  "智能体ID不能为空",
		})
		return
	}

	var dto models.AgentUpdateDTO
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

	err := h.agentService.UpdateAgentByID(context.Background(), id, &dto, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.CommonResponse{
			Code: http.StatusInternalServerError,
			Msg:  "更新智能体失败: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, models.CommonResponse{
		Code: 0,
		Msg:  "success",
	})
}

// switchAgentVoice 切换智能体音色

func (h *AgentHandlers) switchAgentVoice(c *gin.Context) {
	deviceID := c.Param("deviceId")
	if deviceID == "" {
		c.JSON(http.StatusBadRequest, models.CommonResponse{
			Code: http.StatusBadRequest,
			Msg:  "设备ID不能为空",
		})
		return
	}

	var dto models.AgentVoiceSwitchDTO
	if err := c.ShouldBindJSON(&dto); err != nil {
		c.JSON(http.StatusBadRequest, models.CommonResponse{
			Code: http.StatusBadRequest,
			Msg:  "请求参数错误: " + err.Error(),
		})
		return
	}
	if dto.TtsVoiceID == "" {
		c.JSON(http.StatusBadRequest, models.CommonResponse{
			Code: http.StatusBadRequest,
			Msg:  "音色ID不能为空",
		})
		return
	}

	userID, exists := middleware.GetUserIDFromContext(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, models.CommonResponse{
			Code: http.StatusUnauthorized,
			Msg:  "用户未认证",
		})
		return
	}

	if err := h.agentService.SwitchAgentVoice(context.Background(), deviceID, &dto, userID); err != nil {
		c.JSON(http.StatusInternalServerError, models.CommonResponse{
			Code: http.StatusInternalServerError,
			Msg:  "切换音色失败: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, models.CommonResponse{
		Code: 0,
		Msg:  "success",
	})
}

// getAgentVoiceOptions 获取当前设备所属智能体可修改的音色列表
func (h *AgentHandlers) getAgentVoiceOptions(c *gin.Context) {
	deviceID := c.Param("deviceId")
	if deviceID == "" {
		c.JSON(http.StatusBadRequest, models.CommonResponse{
			Code: http.StatusBadRequest,
			Msg:  "设备ID不能为空",
		})
		return
	}

	userID, exists := middleware.GetUserIDFromContext(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, models.CommonResponse{
			Code: http.StatusUnauthorized,
			Msg:  "用户未认证",
		})
		return
	}

	voiceName := c.Query("voiceName")

	result, err := h.agentService.GetAgentVoiceOptions(context.Background(), deviceID, userID, voiceName)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.CommonResponse{
			Code: http.StatusInternalServerError,
			Msg:  "获取音色列表失败: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, models.CommonResponse{
		Code: 0,
		Msg:  "success",
		Data: result,
	})
}

// deleteAgent 删除智能体
func (h *AgentHandlers) deleteAgent(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, models.CommonResponse{
			Code: http.StatusBadRequest,
			Msg:  "智能体ID不能为空",
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

	err := h.agentService.DeleteByID(context.Background(), id, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.CommonResponse{
			Code: http.StatusInternalServerError,
			Msg:  "删除智能体失败: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, models.CommonResponse{
		Code: 0,
		Msg:  "success",
	})
}

// getTemplateList 获取智能体模板列表
func (h *AgentHandlers) getTemplateList(c *gin.Context) {
	templates, err := h.templateService.GetTemplateList(context.Background())
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.CommonResponse{
			Code: http.StatusInternalServerError,
			Msg:  "获取模板列表失败: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, models.CommonResponse{
		Code: 0,
		Msg:  "success",
		Data: templates,
	})
}

// createTemplate 新增智能体模板（仅超级管理员）
func (h *AgentHandlers) createTemplate(c *gin.Context) {
	if !middleware.IsSuperAdminFromContext(c) {
		c.JSON(http.StatusForbidden, models.CommonResponse{
			Code: http.StatusForbidden,
			Msg:  "没有权限执行该操作",
		})
		return
	}

	var req models.AgentTemplateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.CommonResponse{
			Code: http.StatusBadRequest,
			Msg:  "请求参数错误: " + err.Error(),
		})
		return
	}

	operator, ok := middleware.GetUserIDFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, models.CommonResponse{
			Code: http.StatusUnauthorized,
			Msg:  "用户未认证",
		})
		return
	}

	if err := h.templateService.CreateTemplate(context.Background(), &req, operator); err != nil {
		c.JSON(http.StatusInternalServerError, models.CommonResponse{
			Code: http.StatusInternalServerError,
			Msg:  "新增模板失败: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, models.CommonResponse{Code: 0, Msg: "success"})
}

// updateTemplate 更新智能体模板（仅超级管理员）
func (h *AgentHandlers) updateTemplate(c *gin.Context) {
	if !middleware.IsSuperAdminFromContext(c) {
		c.JSON(http.StatusForbidden, models.CommonResponse{
			Code: http.StatusForbidden,
			Msg:  "没有权限执行该操作",
		})
		return
	}

	id := c.Param("id")
	var req models.AgentTemplateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.CommonResponse{
			Code: http.StatusBadRequest,
			Msg:  "请求参数错误: " + err.Error(),
		})
		return
	}

	operator, ok := middleware.GetUserIDFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, models.CommonResponse{
			Code: http.StatusUnauthorized,
			Msg:  "用户未认证",
		})
		return
	}

	if err := h.templateService.UpdateTemplate(context.Background(), id, &req, operator); err != nil {
		c.JSON(http.StatusInternalServerError, models.CommonResponse{
			Code: http.StatusInternalServerError,
			Msg:  "更新模板失败: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, models.CommonResponse{Code: 0, Msg: "success"})
}

// deleteTemplate 删除智能体模板（仅超级管理员）
func (h *AgentHandlers) deleteTemplate(c *gin.Context) {
	if !middleware.IsSuperAdminFromContext(c) {
		c.JSON(http.StatusForbidden, models.CommonResponse{
			Code: http.StatusForbidden,
			Msg:  "没有权限执行该操作",
		})
		return
	}

	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, models.CommonResponse{
			Code: http.StatusBadRequest,
			Msg:  "模板ID不能为空",
		})
		return
	}

	if err := h.templateService.DeleteTemplate(context.Background(), id); err != nil {
		c.JSON(http.StatusInternalServerError, models.CommonResponse{
			Code: http.StatusInternalServerError,
			Msg:  "删除模板失败: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, models.CommonResponse{Code: 0, Msg: "success"})
}

// getAgentSessions 获取智能体会话列表
func (h *AgentHandlers) getAgentSessions(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, models.CommonResponse{
			Code: http.StatusBadRequest,
			Msg:  "智能体ID不能为空",
		})
		return
	}

	page := 1
	limit := 10

	if pageStr := c.Query("page"); pageStr != "" {
		if p, err := strconv.Atoi(pageStr); err == nil && p > 0 {
			page = p
		}
	}

	if limitStr := c.Query("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	result, err := h.chatHistoryService.GetSessionListByAgentID(context.Background(), id, page, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.CommonResponse{
			Code: http.StatusInternalServerError,
			Msg:  "获取会话列表失败: " + err.Error(),
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

// getAgentChatHistory 获取智能体聊天记录
func (h *AgentHandlers) getAgentChatHistory(c *gin.Context) {
	id := c.Param("id")
	sessionID := c.Param("sessionId")

	if id == "" || sessionID == "" {
		c.JSON(http.StatusBadRequest, models.CommonResponse{
			Code: http.StatusBadRequest,
			Msg:  "智能体ID和会话ID不能为空",
		})
		return
	}

	// 获取当前用户ID并检查权限
	userID, exists := middleware.GetUserIDFromContext(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, models.CommonResponse{
			Code: http.StatusUnauthorized,
			Msg:  "用户未认证",
		})
		return
	}
	hasPermission, err := h.agentService.CheckAgentPermission(context.Background(), id, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.CommonResponse{
			Code: http.StatusInternalServerError,
			Msg:  "权限检查失败: " + err.Error(),
		})
		return
	}

	if !hasPermission {
		c.JSON(http.StatusForbidden, models.CommonResponse{
			Code: http.StatusForbidden,
			Msg:  "没有权限查看该智能体的聊天记录",
		})
		return
	}

	histories, err := h.chatHistoryService.GetChatHistoryBySessionID(context.Background(), id, sessionID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.CommonResponse{
			Code: http.StatusInternalServerError,
			Msg:  "获取聊天记录失败: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, models.CommonResponse{
		Code: 0,
		Msg:  "success",
		Data: histories,
	})
}

// getRecentlyFiftyByAgentID 获取智能体最近50条聊天记录（用户）
func (h *AgentHandlers) getRecentlyFiftyByAgentID(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, models.CommonResponse{
			Code: http.StatusBadRequest,
			Msg:  "智能体ID不能为空",
		})
		return
	}

	// 获取当前用户ID并检查权限
	userID, exists := middleware.GetUserIDFromContext(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, models.CommonResponse{
			Code: http.StatusUnauthorized,
			Msg:  "用户未认证",
		})
		return
	}
	hasPermission, err := h.agentService.CheckAgentPermission(context.Background(), id, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.CommonResponse{
			Code: http.StatusInternalServerError,
			Msg:  "权限检查失败: " + err.Error(),
		})
		return
	}

	if !hasPermission {
		c.JSON(http.StatusForbidden, models.CommonResponse{
			Code: http.StatusForbidden,
			Msg:  "没有权限查看该智能体的聊天记录",
		})
		return
	}

	histories, err := h.chatHistoryService.GetRecentlyFiftyByAgentID(context.Background(), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.CommonResponse{
			Code: http.StatusInternalServerError,
			Msg:  "获取聊天记录失败: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, models.CommonResponse{
		Code: 0,
		Msg:  "success",
		Data: histories,
	})
}

// ===================== MCP 接入点 =====================

// getAgentMcpAccessAddress 获取智能体的MCP接入点地址
func (h *AgentHandlers) getAgentMcpAccessAddress(c *gin.Context) {
	agentID := c.Param("agentId")
	// 获取当前用户
	userDTO, ok := middleware.GetUserFromContext(c)
	if !ok {
		c.JSON(http.StatusOK, &models.CommonResponse{Code: 401, Msg: "未认证"})
		return
	}

	// 检查权限
	has, err := h.agentService.CheckAgentPermission(c.Request.Context(), agentID, userDTO.ID)
	if err != nil || !has {
		c.JSON(http.StatusOK, &models.CommonResponse{Code: 1, Msg: "没有权限查看该智能体的MCP接入点地址"})
		return
	}
	// 从参数中读取mcp接入点地址
	addr, _ := h.paramsService.GetValue(c.Request.Context(), constants.SERVER_MCP_ENDPOINT, "")
	if addr == "" || addr == "null" {
		scheme := "ws"
		if c.Request.TLS != nil {
			scheme = "wss"
		}
		addr = fmt.Sprintf("%s://%s/xiaozhi/mcp_endpoint/mcp/?token=%s", scheme, c.Request.Host, agentID)
	} else {
		if !strings.HasPrefix(addr, "ws://") || !strings.HasPrefix(addr, "wss://") {
			addr = "ws://" + addr
		}
	}
	c.JSON(http.StatusOK, &models.CommonResponse{Code: 0, Msg: "success", Data: addr})
}

// getAgentMcpToolsList 获取智能体的MCP工具列表
func (h *AgentHandlers) getAgentMcpToolsList(c *gin.Context) {
	agentID := c.Param("agentId")

	// 获取当前用户
	userDTO, ok := middleware.GetUserFromContext(c)
	if !ok {
		c.JSON(http.StatusOK, &models.CommonResponse{Code: 401, Msg: "未认证"})
		return
	}

	// 检查权限
	has, err := h.agentService.CheckAgentPermission(c.Request.Context(), agentID, userDTO.ID)
	if err != nil || !has {
		c.JSON(http.StatusOK, &models.CommonResponse{Code: 1, Msg: "没有权限查看该智能体的MCP工具列表"})
		return
	}

	// 从 MCP 连接管理器获取工具列表
	payload := h.getMcpToolsForAgent(agentID)

	c.JSON(http.StatusOK, &models.CommonResponse{Code: 0, Msg: "success", Data: payload})
}

// getMcpToolsForAgent 获取指定agent的MCP工具列表
func (h *AgentHandlers) getMcpToolsForAgent(agentID string) agentMcpToolsPayload {
	now := time.Now().UTC()

	connectionSnapshots := mcp.GlobalConnectionManager.GetToolConnectionSnapshots(agentID)
	connections := make([]agentMcpConnectionInfo, 0, len(connectionSnapshots))
	for _, snapshot := range connectionSnapshots {
		info := agentMcpConnectionInfo{
			ConnectionID: snapshot.ConnectionID,
			RemoteAddr:   snapshot.RemoteAddr,
			ConnectedAt:  snapshot.ConnectedAt,
		}
		if snapshot.ConnectedAt > 0 {
			connectedAt := time.Unix(snapshot.ConnectedAt, 0).UTC()
			info.ConnectedAtISO = connectedAt.Format(time.RFC3339)
			duration := now.Sub(connectedAt)
			if duration < 0 {
				duration = 0
			}
			info.DurationSeconds = int64(duration / time.Second)
		}
		connections = append(connections, info)
	}

	toolSnapshots := mcp.GlobalConnectionManager.GetToolInfoSnapshots(agentID)
	tools := make([]agentMcpToolInfo, 0, len(toolSnapshots))
	for _, snapshot := range toolSnapshots {
		tool := agentMcpToolInfo{
			Name:         snapshot.Name,
			Description:  snapshot.Description,
			ConnectionID: snapshot.ConnectionID,
			UpdatedAt:    snapshot.UpdatedAt,
		}
		if len(snapshot.InputSchema) > 0 {
			tool.InputSchema = snapshot.InputSchema
		}
		if snapshot.UpdatedAt > 0 {
			tool.UpdatedAtISO = time.Unix(snapshot.UpdatedAt, 0).UTC().Format(time.RFC3339)
		}
		tools = append(tools, tool)
	}

	return agentMcpToolsPayload{
		Connections: connections,
		Tools:       tools,
	}
}

// ===================== 音频下载 =====================

// getAudioID 获取音频下载UUID（与Java POST /agent/audio/{audioId}一致）
func (h *AgentHandlers) getAudioID(c *gin.Context) {
	audioID := c.Param("audioId")
	if audioID == "" {
		c.JSON(http.StatusOK, models.CommonResponse{Code: 1, Msg: "音频ID不能为空"})
		return
	}
	u := uuid.NewString()
	agentAudioStore.mu.Lock()
	agentAudioStore.uuid2aid[u] = audioID
	agentAudioStore.mu.Unlock()
	c.JSON(http.StatusOK, models.CommonResponse{Code: 0, Msg: "success", Data: u})
}

// playAudio 播放音频（与Java GET /agent/play/{uuid}一致）
func (h *AgentHandlers) playAudio(c *gin.Context) {
	u := c.Param("uuid")
	if u == "" {
		c.Status(http.StatusNotFound)
		return
	}
	agentAudioStore.mu.Lock()
	audioID, ok := agentAudioStore.uuid2aid[u]
	// 一次性使用，读取后删除
	delete(agentAudioStore.uuid2aid, u)
	agentAudioStore.mu.Unlock()
	if !ok || audioID == "" {
		c.Status(http.StatusNotFound)
		return
	}
	// 目前未实现音频存储，返回404模拟未找到音频，符合Java的notFound分支
	c.Status(http.StatusNotFound)
}

// ===================== 聊天上报 =====================

// reportAgentChatHistory 小智服务聊天上报请求
func (h *AgentHandlers) reportAgentChatHistory(c *gin.Context) {
	var req models.AgentChatHistoryReportDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{Code: 1, Msg: "参数错误: " + err.Error()})
		return
	}
	ok, err := h.chatHistoryService.Report(c.Request.Context(), &req)
	if err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{Code: 1, Msg: err.Error()})
		return
	}
	c.JSON(http.StatusOK, &models.CommonResponse{Code: 0, Msg: "success", Data: ok})
}
