package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"backend-server/internal/adapters/manager"
	"backend-server/internal/adapters/workflow"
	"backend-server/internal/app/service"
	"backend-server/internal/domain/mcp"
)

// Handler encapsulates HTTP API endpoints that are not part of the WebSocket transport.
type Handler struct {
	mcpManager *mcp.GlobalMCPManager
	managerAPI manager_api.ManagerAPIService
	workflowRT *workflow.Runtime
}

// Option allows configuring Handler dependencies.
type Option func(*Handler)

// WithMCPManager overrides the MCP manager instance used by the handler.
func WithMCPManager(manager *mcp.GlobalMCPManager) Option {
	return func(h *Handler) {
		h.mcpManager = manager
	}
}

// WithManagerAPIService overrides the manager-api service dependency.
func WithManagerAPIService(service manager_api.ManagerAPIService) Option {
	return func(h *Handler) {
		h.managerAPI = service
	}
}

// WithWorkflowRuntime overrides the workflow runtime instance.
func WithWorkflowRuntime(rt *workflow.Runtime) Option {
	return func(h *Handler) {
		h.workflowRT = rt
	}
}

// NewHandler constructs a Handler with optional dependencies.
func NewHandler(opts ...Option) *Handler {
	h := &Handler{
		mcpManager: mcp.GetGlobalMCPManager(),
		managerAPI: service.DefaultRegistry().ManagerAPIService(),
		workflowRT: service.DefaultRegistry().WorkflowRuntime(),
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// RegisterRoutes wires the HTTP endpoints onto the provided gin engine.
func (h *Handler) RegisterRoutes(router *gin.Engine) {
	router.GET("/health", h.handleHealth)

	api := router.Group("/xiaozhi/api")
	{
		api.POST("/vision", h.handleVision)
		api.POST("/images/generations", h.handleImageGeneration)
		api.GET("/mcp/tools/*deviceID", h.handleGetDeviceTools)
		api.GET("/workflows", h.handleListWorkflows)
		api.POST("/workflows/sync", h.handleSyncWorkflows)
		api.POST("/workflows/execute", h.handleExecuteWorkflowDefinition)
		api.POST("/workflows/:id/execute", h.handleExecuteWorkflow)
	}
}

func (h *Handler) handleHealth(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (h *Handler) handleListWorkflows(c *gin.Context) {
	if h.workflowRT == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": 503, "msg": "workflow runtime unavailable"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": h.workflowRT.List()})
}

func (h *Handler) handleSyncWorkflows(c *gin.Context) {
	if h.workflowRT == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": 503, "msg": "workflow runtime unavailable"})
		return
	}
	if err := h.workflowRT.Sync(c.Request.Context()); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"code": 502, "msg": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "synced"})
}

func (h *Handler) handleExecuteWorkflow(c *gin.Context) {
	if h.workflowRT == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": 503, "msg": "workflow runtime unavailable"})
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "invalid workflow id"})
		return
	}
	var payload map[string]any
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": err.Error()})
		return
	}
	result, err := h.workflowRT.Execute(c.Request.Context(), id, payload)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"code": 502, "msg": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": result})
}

func (h *Handler) handleExecuteWorkflowDefinition(c *gin.Context) {
	if h.workflowRT == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": 503, "msg": "workflow runtime unavailable"})
		return
	}
	var req struct {
		WorkflowID uint64          `json:"workflowId"`
		Definition json.RawMessage `json:"definition"`
		Input      map[string]any  `json:"input"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": err.Error()})
		return
	}
	ctx := c.Request.Context()
	var (
		result *workflow.ExecutionResult
		err    error
	)
	if req.WorkflowID > 0 && len(req.Definition) == 0 {
		result, err = h.workflowRT.Execute(ctx, req.WorkflowID, req.Input)
	} else if len(req.Definition) > 0 {
		result, err = h.workflowRT.ExecuteDefinition(ctx, req.Definition, req.Input)
	} else {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "workflowId or definition required"})
		return
	}
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"code": 502, "msg": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": result})
}
