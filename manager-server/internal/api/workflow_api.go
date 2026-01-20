package api

import (
	"bytes"
	"encoding/csv"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"manager-server/internal/middleware"
	"manager-server/internal/models"
	"manager-server/internal/service"
)

type WorkflowAPI struct {
	service *service.WorkflowService
}

func NewWorkflowAPI(service *service.WorkflowService) *WorkflowAPI {
	return &WorkflowAPI{service: service}
}

func (api *WorkflowAPI) RegisterRoutes(r *gin.RouterGroup) {
	workflow := r.Group("/workflows")
	workflow.GET("", api.ListWorkflows)
	workflow.POST("", api.CreateWorkflow)
	workflow.GET(":id", api.GetWorkflow)
	workflow.PUT(":id", api.UpdateWorkflow)
	workflow.DELETE(":id", api.DeleteWorkflow)
	workflow.POST(":id/duplicate", api.DuplicateWorkflow)
	workflow.POST(":id/publish", api.PublishWorkflow)
	workflow.POST(":id/test", api.TestWorkflow)
	workflow.POST("/test", api.TestWorkflow)
	workflow.GET("/executions", api.ListWorkflowExecutions)
	workflow.GET("/executions/:id", api.GetWorkflowExecution)
}

func (api *WorkflowAPI) ListWorkflows(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("pageSize", "12"))
	items, total, err := api.service.ListWorkflows(c.Request.Context(), page, size, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"items": items, "total": total}})
}

func (api *WorkflowAPI) CreateWorkflow(c *gin.Context) {
	var req models.Workflow
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": err.Error()})
		return
	}
	if err := api.service.CreateWorkflow(c.Request.Context(), &req); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": req})
}

func (api *WorkflowAPI) GetWorkflow(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	wf, err := api.service.GetWorkflow(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "workflow not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": wf})
}

func (api *WorkflowAPI) UpdateWorkflow(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	var req models.Workflow
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": err.Error()})
		return
	}
	req.ID = id
	if err := api.service.UpdateWorkflow(c.Request.Context(), &req); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": req})
}

func (api *WorkflowAPI) DeleteWorkflow(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	if err := api.service.DeleteWorkflow(c.Request.Context(), id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0})
}

func (api *WorkflowAPI) DuplicateWorkflow(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	copy, err := api.service.DuplicateWorkflow(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": copy})
}

func (api *WorkflowAPI) PublishWorkflow(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	var req service.WorkflowPublishRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": err.Error()})
		return
	}
	wf, err := api.service.PublishWorkflow(c.Request.Context(), id, req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": wf})
}

func (api *WorkflowAPI) TestWorkflow(c *gin.Context) {
	var req service.WorkflowTestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": err.Error()})
		return
	}
	if idStr := c.Param("id"); idStr != "" {
		if parsed, err := strconv.ParseUint(idStr, 10, 64); err == nil {
			req.WorkflowID = parsed
		}
	}
	result, err := api.service.TestWorkflow(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"code": 502, "msg": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": result})
}

func (api *WorkflowAPI) ListWorkflowExecutions(c *gin.Context) {
	userID, _ := middleware.GetUserIDFromContext(c)
	isSystem := middleware.IsSystemCallFromContext(c)
	isAdmin := middleware.IsSuperAdminFromContext(c)
	enforceOwner := !(isSystem || isAdmin)
	workflowID, _ := strconv.ParseUint(c.DefaultQuery("workflowId", "0"), 10, 64)
	status := c.Query("status")
	search := c.Query("q")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("pageSize", "20"))
	if c.Query("format") == "csv" {
		page = 1
		if size < 1 || size > 1000 {
			size = 1000
		}
	}
	var (
		startPtr *time.Time
		endPtr   *time.Time
	)
	if startStr := c.Query("startTime"); startStr != "" {
		if parsed, err := time.Parse(time.RFC3339, startStr); err == nil {
			startPtr = &parsed
		}
	}
	if endStr := c.Query("endTime"); endStr != "" {
		if parsed, err := time.Parse(time.RFC3339, endStr); err == nil {
			endPtr = &parsed
		}
	}
	filter := service.WorkflowExecutionQuery{
		WorkflowID: workflowID,
		Status:     status,
		Search:     search,
		StartFrom:  startPtr,
		StartTo:    endPtr,
	}
	items, total, err := api.service.ListExecutions(c.Request.Context(), filter, userID, enforceOwner, page, size)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": err.Error()})
		return
	}
	if c.Query("format") == "csv" {
		var buf bytes.Buffer
		writer := csv.NewWriter(&buf)
		_ = writer.Write([]string{"ID", "WorkflowID", "Version", "Status", "StartedAt", "FinishedAt", "DurationMs", "Error"})
		for _, item := range items {
			finished := ""
			if item.FinishedAt != nil {
				finished = item.FinishedAt.Format(time.RFC3339)
			}
			_ = writer.Write([]string{
				strconv.FormatUint(item.ID, 10),
				strconv.FormatUint(item.WorkflowID, 10),
				strconv.Itoa(item.WorkflowVersion),
				item.Status,
				item.StartedAt.Format(time.RFC3339),
				finished,
				strconv.FormatInt(item.DurationMs, 10),
				item.ErrorMessage,
			})
		}
		writer.Flush()
		c.Header("Content-Type", "text/csv")
		c.Header("Content-Disposition", "attachment; filename=workflow-executions.csv")
		c.String(http.StatusOK, buf.String())
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"items": items, "total": total}})
}

func (api *WorkflowAPI) GetWorkflowExecution(c *gin.Context) {
	userID, _ := middleware.GetUserIDFromContext(c)
	isSystem := middleware.IsSystemCallFromContext(c)
	isAdmin := middleware.IsSuperAdminFromContext(c)
	enforceOwner := !(isSystem || isAdmin)
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "invalid execution id"})
		return
	}
	exec, err := api.service.GetExecution(c.Request.Context(), id, userID, enforceOwner)
	if err != nil {
		if errors.Is(err, service.ErrWorkflowExecutionForbidden) {
			c.JSON(http.StatusForbidden, gin.H{"code": 403, "msg": "forbidden"})
			return
		}
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": exec})
}

func (api *WorkflowAPI) IngestWorkflowExecution(c *gin.Context) {
	var payload service.WorkflowExecutionPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": err.Error()})
		return
	}
	if payload.WorkflowID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "workflowId is required"})
		return
	}
	if payload.Status == "" {
		payload.Status = "unknown"
	}
	entity, err := api.service.SaveExecution(c.Request.Context(), payload)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": entity})
}
