package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"manager-server/internal/middleware"
	"manager-server/internal/models"
	"manager-server/internal/repository"
	"manager-server/internal/service"
	"manager-server/internal/vectorstore"
)

// KBHandlers wraps HTTP handlers for the knowledge base domain.
type KBHandlers struct {
	service    *service.KBService
	ingestion  *service.KBIngestionService
	events     *service.KBJobEventHub
	deviceRepo repository.DeviceRepository
}

// NewKBHandlers constructs a KBHandlers instance.
func NewKBHandlers(svc *service.KBService, ingestion *service.KBIngestionService, events *service.KBJobEventHub, deviceRepo repository.DeviceRepository) *KBHandlers {
	return &KBHandlers{service: svc, ingestion: ingestion, events: events, deviceRepo: deviceRepo}
}

// registerRoutes wires the KB endpoints.
func (h *KBHandlers) registerRoutes(router *gin.RouterGroup) {
	router.GET("/projects", h.listProjects)
	router.GET("/projects/:projectId", h.getProject)
	router.POST("/projects", h.createProject)
	router.PUT("/projects/:projectId", h.updateProject)
	router.DELETE("/projects/:projectId", h.deleteProject)

	router.GET("/projects/:projectId/knowledge-bases", h.listKnowledgeBases)
	router.POST("/projects/:projectId/knowledge-bases", h.createKnowledgeBase)

	router.GET("/knowledge-bases/:kbId", h.getKnowledgeBase)
	router.PUT("/knowledge-bases/:kbId", h.updateKnowledgeBase)
	router.POST("/knowledge-bases/:kbId/archive", h.archiveKnowledgeBase)

	router.POST("/knowledge-bases/:kbId/documents", h.createDocument)
	router.POST("/knowledge-bases/:kbId/documents/upload", h.uploadDocuments)
	router.POST("/knowledge-bases/:kbId/documents/url", h.submitDocumentURL)
	router.GET("/knowledge-bases/:kbId/documents", h.listDocuments)

	router.GET("/knowledge-bases/:kbId/ingestion/connectors", h.listIngestionConnectors)
	router.POST("/knowledge-bases/:kbId/ingestion/parse", h.parseIngestionSource)
	router.POST("/knowledge-bases/:kbId/ingestion/import", h.importIngestionSelections)
	router.POST("/knowledge-bases/:kbId/ingestion/tasks/:taskId/callback", h.ingestionWebhookCallback)

	router.GET("/knowledge-bases/:kbId/documents/:docId/chunks", h.listChunks)
	router.POST("/knowledge-bases/:kbId/documents/:docId/chunks", h.createChunks)
	router.PATCH("/chunks/:chunkId", h.updateChunk)

	router.GET("/knowledge-bases/:kbId/jobs", h.listJobs)
	router.POST("/knowledge-bases/:kbId/jobs", h.createJob)
	router.GET("/knowledge-bases/:kbId/jobs/stream", h.streamJobEvents)
	router.POST("/knowledge-bases/:kbId/jobs/:jobId/retry", h.retryJob)
	router.GET("/knowledge-bases/:kbId/jobs/:jobId/webhook-events", h.listJobWebhookEvents)
	router.POST("/knowledge-bases/jobs/pause", h.pauseJobs)
	router.POST("/knowledge-bases/jobs/resume", h.resumeJobs)
	router.GET("/knowledge-bases/jobs/status", h.jobRunnerStatus)
	router.GET("/knowledge-bases/:kbId/jobs/summary", h.jobStatusSummary)

	router.POST("/knowledge-bases/:kbId/query", h.queryKnowledgeBase)

	router.POST("/knowledge-bases/:kbId/permissions", h.grantPermission)
	router.GET("/knowledge-bases/:kbId/permissions", h.listPermissions)
	router.DELETE("/knowledge-bases/:kbId/permissions/:userId", h.revokePermission)

	router.POST("/projects/:projectId/mounts", h.mountAgentProject)
	router.DELETE("/projects/:projectId/mounts/:agentId", h.unmountAgentProject)
	router.GET("/agents/:agentId/mounts", h.listAgentMounts)
}

// registerInternalRoutes wires internal-only KB endpoints (server secret).
func (h *KBHandlers) registerInternalRoutes(router *gin.RouterGroup) {
	router.POST("/knowledge-bases/:kbId/query", h.internalQueryKnowledgeBase)
	router.GET("/knowledge-bases/:kbId/metadata", h.internalKnowledgeBaseMetadata)
	router.POST("/meeting-minutes/ingest", h.internalIngestMeetingMinutes)
}

func (h *KBHandlers) listProjects(c *gin.Context) {
	actor, ok := h.actorFromContext(c)
	if !ok {
		return
	}
	page, limit := parsePageLimit(c)
	offset := (page - 1) * limit

	filter := repository.KBProjectFilter{}
	if vis := c.Query("visibility"); vis != "" {
		filter.Visibility = splitAndClean(vis)
	}
	if name := c.Query("name"); name != "" {
		filter.NameLike = name
	}

	projects, total, err := h.service.ListProjects(c.Request.Context(), actor, filter, limit, offset)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "获取项目列表失败: "+err.Error())
		return
	}

	c.JSON(http.StatusOK, models.CommonResponse{
		Code: 0,
		Msg:  "success",
		Data: models.PageResponse{
			List:       projects,
			TotalCount: total,
			PageSize:   limit,
			CurrPage:   page,
			TotalPage:  calcTotalPages(total, limit),
		},
	})
}

func (h *KBHandlers) getProject(c *gin.Context) {
	actor, ok := h.actorFromContext(c)
	if !ok {
		return
	}
	projectID, err := parseUUIDParam(c, "projectId")
	if err != nil {
		writeError(c, http.StatusBadRequest, "项目ID无效: "+err.Error())
		return
	}
	project, err := h.service.GetProject(c.Request.Context(), actor, projectID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusNotFound, "项目不存在")
			return
		}
		writeError(c, http.StatusInternalServerError, "获取项目失败: "+err.Error())
		return
	}
	c.JSON(http.StatusOK, models.CommonResponse{
		Code: 0,
		Msg:  "success",
		Data: project,
	})
}

func (h *KBHandlers) createProject(c *gin.Context) {
	actor, ok := h.actorFromContext(c)
	if !ok {
		return
	}
	var payload struct {
		Name       string         `json:"name" binding:"required"`
		Visibility string         `json:"visibility"`
		Metadata   map[string]any `json:"metadata"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		writeError(c, http.StatusBadRequest, "参数错误: "+err.Error())
		return
	}
	project, err := h.service.CreateProject(c.Request.Context(), actor, service.CreateProjectInput{
		Name:       payload.Name,
		Visibility: payload.Visibility,
		Metadata:   payload.Metadata,
	})
	if err != nil {
		writeError(c, http.StatusInternalServerError, "创建项目失败: "+err.Error())
		return
	}
	c.JSON(http.StatusOK, models.CommonResponse{
		Code: 0,
		Msg:  "success",
		Data: project,
	})
}

func (h *KBHandlers) updateProject(c *gin.Context) {
	actor, ok := h.actorFromContext(c)
	if !ok {
		return
	}
	projectID, err := parseUUIDParam(c, "projectId")
	if err != nil {
		writeError(c, http.StatusBadRequest, "项目ID无效: "+err.Error())
		return
	}
	var payload struct {
		Name       *string        `json:"name"`
		Visibility *string        `json:"visibility"`
		Metadata   map[string]any `json:"metadata"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		writeError(c, http.StatusBadRequest, "参数错误: "+err.Error())
		return
	}
	project, err := h.service.UpdateProject(c.Request.Context(), actor, projectID, service.UpdateProjectInput{
		Name:       payload.Name,
		Visibility: payload.Visibility,
		Metadata:   payload.Metadata,
	})
	if err != nil {
		writeError(c, http.StatusInternalServerError, "更新项目失败: "+err.Error())
		return
	}
	c.JSON(http.StatusOK, models.CommonResponse{
		Code: 0,
		Msg:  "success",
		Data: project,
	})
}

func (h *KBHandlers) deleteProject(c *gin.Context) {
	actor, ok := h.actorFromContext(c)
	if !ok {
		return
	}
	projectID, err := parseUUIDParam(c, "projectId")
	if err != nil {
		writeError(c, http.StatusBadRequest, "项目ID无效: "+err.Error())
		return
	}
	if err := h.service.DeleteProject(c.Request.Context(), actor, projectID); err != nil {
		writeError(c, http.StatusInternalServerError, "删除项目失败: "+err.Error())
		return
	}
	c.JSON(http.StatusOK, models.CommonResponse{Code: 0, Msg: "success"})
}

func (h *KBHandlers) listKnowledgeBases(c *gin.Context) {
	actor, ok := h.actorFromContext(c)
	if !ok {
		return
	}
	projectID, err := parseUUIDParam(c, "projectId")
	if err != nil {
		writeError(c, http.StatusBadRequest, "项目ID无效: "+err.Error())
		return
	}
	page, limit := parsePageLimit(c)
	offset := (page - 1) * limit

	filter := repository.KBKnowledgeBaseFilter{
		ProjectID: projectID,
	}
	if status := c.Query("status"); status != "" {
		filter.Status = splitAndClean(status)
	}
	if name := c.Query("name"); name != "" {
		filter.NameLike = name
	}

	kbs, total, err := h.service.ListKnowledgeBases(c.Request.Context(), actor, filter, limit, offset)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "获取知识库列表失败: "+err.Error())
		return
	}
	c.JSON(http.StatusOK, models.CommonResponse{
		Code: 0,
		Msg:  "success",
		Data: models.PageResponse{
			List:       kbs,
			TotalCount: total,
			PageSize:   limit,
			CurrPage:   page,
			TotalPage:  calcTotalPages(total, limit),
		},
	})
}

func (h *KBHandlers) createKnowledgeBase(c *gin.Context) {
	actor, ok := h.actorFromContext(c)
	if !ok {
		return
	}
	projectID, err := parseUUIDParam(c, "projectId")
	if err != nil {
		writeError(c, http.StatusBadRequest, "项目ID无效: "+err.Error())
		return
	}
	var payload struct {
		Name              string         `json:"name" binding:"required"`
		Description       string         `json:"description"`
		EmbeddingModel    string         `json:"embeddingModel" binding:"required"`
		EmbeddingParams   map[string]any `json:"embeddingParams"`
		RetrievalStrategy string         `json:"retrievalStrategy"`
		RetrievalParams   map[string]any `json:"retrievalParams"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		writeError(c, http.StatusBadRequest, "参数错误: "+err.Error())
		return
	}
	kb, err := h.service.CreateKnowledgeBase(c.Request.Context(), actor, service.CreateKnowledgeBaseInput{
		ProjectID:         projectID,
		Name:              payload.Name,
		Description:       payload.Description,
		EmbeddingModel:    payload.EmbeddingModel,
		EmbeddingParams:   payload.EmbeddingParams,
		RetrievalStrategy: payload.RetrievalStrategy,
		RetrievalParams:   payload.RetrievalParams,
	})
	if err != nil {
		writeError(c, http.StatusInternalServerError, "创建知识库失败: "+err.Error())
		return
	}
	c.JSON(http.StatusOK, models.CommonResponse{
		Code: 0,
		Msg:  "success",
		Data: kb,
	})
}

func (h *KBHandlers) getKnowledgeBase(c *gin.Context) {
	actor, ok := h.actorFromContext(c)
	if !ok {
		return
	}
	kbID, err := parseUintParam(c, "kbId")
	if err != nil {
		writeError(c, http.StatusBadRequest, "知识库ID无效: "+err.Error())
		return
	}
	kb, err := h.service.GetKnowledgeBase(c.Request.Context(), actor, kbID)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "获取知识库失败: "+err.Error())
		return
	}
	c.JSON(http.StatusOK, models.CommonResponse{
		Code: 0,
		Msg:  "success",
		Data: kb,
	})
}

func (h *KBHandlers) updateKnowledgeBase(c *gin.Context) {
	actor, ok := h.actorFromContext(c)
	if !ok {
		return
	}
	kbID, err := parseUintParam(c, "kbId")
	if err != nil {
		writeError(c, http.StatusBadRequest, "知识库ID无效: "+err.Error())
		return
	}
	var payload struct {
		Name              *string        `json:"name"`
		Description       *string        `json:"description"`
		EmbeddingModel    *string        `json:"embeddingModel"`
		EmbeddingParams   map[string]any `json:"embeddingParams"`
		RetrievalStrategy *string        `json:"retrievalStrategy"`
		RetrievalParams   map[string]any `json:"retrievalParams"`
		Status            *string        `json:"status"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		writeError(c, http.StatusBadRequest, "参数错误: "+err.Error())
		return
	}
	kb, err := h.service.UpdateKnowledgeBase(c.Request.Context(), actor, kbID, service.UpdateKnowledgeBaseInput{
		Name:              payload.Name,
		Description:       payload.Description,
		EmbeddingModel:    payload.EmbeddingModel,
		EmbeddingParams:   payload.EmbeddingParams,
		RetrievalStrategy: payload.RetrievalStrategy,
		RetrievalParams:   payload.RetrievalParams,
		Status:            payload.Status,
	})
	if err != nil {
		writeError(c, http.StatusInternalServerError, "更新知识库失败: "+err.Error())
		return
	}
	c.JSON(http.StatusOK, models.CommonResponse{
		Code: 0,
		Msg:  "success",
		Data: kb,
	})
}

func (h *KBHandlers) archiveKnowledgeBase(c *gin.Context) {
	actor, ok := h.actorFromContext(c)
	if !ok {
		return
	}
	kbID, err := parseUintParam(c, "kbId")
	if err != nil {
		writeError(c, http.StatusBadRequest, "知识库ID无效: "+err.Error())
		return
	}
	if err := h.service.ArchiveKnowledgeBase(c.Request.Context(), actor, kbID); err != nil {
		writeError(c, http.StatusInternalServerError, "归档知识库失败: "+err.Error())
		return
	}
	c.JSON(http.StatusOK, models.CommonResponse{Code: 0, Msg: "success"})
}

func (h *KBHandlers) createDocument(c *gin.Context) {
	actor, ok := h.actorFromContext(c)
	if !ok {
		return
	}
	kbID, err := parseUintParam(c, "kbId")
	if err != nil {
		writeError(c, http.StatusBadRequest, "知识库ID无效: "+err.Error())
		return
	}
	var payload struct {
		Title            string         `json:"title" binding:"required"`
		SourceType       string         `json:"sourceType" binding:"required"`
		StorageURI       string         `json:"storageUri"`
		Checksum         string         `json:"checksum"`
		SizeBytes        int64          `json:"sizeBytes"`
		Metadata         map[string]any `json:"metadata"`
		EnqueueIngestion bool           `json:"enqueueIngestion"`
		RawContent       string         `json:"rawContent"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		writeError(c, http.StatusBadRequest, "参数错误: "+err.Error())
		return
	}
	doc, job, err := h.service.CreateDocument(c.Request.Context(), actor, service.CreateDocumentInput{
		KnowledgeBaseID:  kbID,
		Title:            payload.Title,
		SourceType:       payload.SourceType,
		StorageURI:       payload.StorageURI,
		Checksum:         payload.Checksum,
		SizeBytes:        payload.SizeBytes,
		Metadata:         payload.Metadata,
		EnqueueIngestion: payload.EnqueueIngestion,
		RawContent:       payload.RawContent,
	})
	if err != nil {
		writeError(c, http.StatusInternalServerError, "创建文档失败: "+err.Error())
		return
	}
	response := map[string]any{"document": doc}
	if job != nil {
		response["job"] = job
	}
	c.JSON(http.StatusOK, models.CommonResponse{
		Code: 0,
		Msg:  "success",
		Data: response,
	})
}

func (h *KBHandlers) uploadDocuments(c *gin.Context) {
	actor, ok := h.actorFromContext(c)
	if !ok {
		return
	}
	kbID, err := parseUintParam(c, "kbId")
	if err != nil {
		writeError(c, http.StatusBadRequest, "知识库ID无效: "+err.Error())
		return
	}
	form, err := c.MultipartForm()
	if err != nil {
		writeError(c, http.StatusBadRequest, "解析上传内容失败: "+err.Error())
		return
	}
	fileHeaders := form.File["files"]
	if len(fileHeaders) == 0 {
		fileHeaders = form.File["file"]
	}
	if len(fileHeaders) == 0 {
		writeError(c, http.StatusBadRequest, "请提供至少一个文件")
		return
	}

	inputs := make([]service.UploadDocumentInput, 0, len(fileHeaders))
	closers := make([]io.Closer, 0, len(fileHeaders))
	for _, fh := range fileHeaders {
		file, err := fh.Open()
		if err != nil {
			for _, closer := range closers {
				_ = closer.Close()
			}
			writeError(c, http.StatusBadRequest, "读取上传文件失败: "+err.Error())
			return
		}
		closers = append(closers, file)
		inputs = append(inputs, service.UploadDocumentInput{
			FileName:    fh.Filename,
			Size:        fh.Size,
			ContentType: fh.Header.Get("Content-Type"),
			Reader:      file,
		})
	}

	docs, jobs, err := h.service.UploadDocuments(c.Request.Context(), actor, kbID, inputs)
	if err != nil {
		for _, closer := range closers {
			_ = closer.Close()
		}
		writeError(c, http.StatusBadRequest, "上传文件失败: "+err.Error())
		return
	}
	for _, closer := range closers {
		_ = closer.Close()
	}

	response := map[string]any{
		"documents": docs,
	}
	if len(jobs) > 0 {
		response["jobs"] = jobs
	}
	c.JSON(http.StatusOK, models.CommonResponse{
		Code: 0,
		Msg:  "success",
		Data: response,
	})
}

func (h *KBHandlers) submitDocumentURL(c *gin.Context) {
	actor, ok := h.actorFromContext(c)
	if !ok {
		return
	}
	kbID, err := parseUintParam(c, "kbId")
	if err != nil {
		writeError(c, http.StatusBadRequest, "知识库ID无效: "+err.Error())
		return
	}
	var payload struct {
		URL         string         `json:"url" binding:"required"`
		Title       string         `json:"title"`
		ContentType string         `json:"contentType"`
		Metadata    map[string]any `json:"metadata"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		writeError(c, http.StatusBadRequest, "参数错误: "+err.Error())
		return
	}
	doc, job, err := h.service.SubmitDocumentURL(c.Request.Context(), actor, service.SubmitDocumentURLInput{
		KnowledgeBaseID: kbID,
		URL:             payload.URL,
		Title:           payload.Title,
		ContentType:     payload.ContentType,
		Metadata:        payload.Metadata,
	})
	if err != nil {
		writeError(c, http.StatusBadRequest, "提交链接失败: "+err.Error())
		return
	}
	response := map[string]any{"document": doc}
	if job != nil {
		response["job"] = job
	}
	c.JSON(http.StatusOK, models.CommonResponse{
		Code: 0,
		Msg:  "success",
		Data: response,
	})
}

func (h *KBHandlers) listDocuments(c *gin.Context) {
	actor, ok := h.actorFromContext(c)
	if !ok {
		return
	}
	kbID, err := parseUintParam(c, "kbId")
	if err != nil {
		writeError(c, http.StatusBadRequest, "知识库ID无效: "+err.Error())
		return
	}
	page, limit := parsePageLimit(c)
	offset := (page - 1) * limit

	filter := repository.KBDocumentFilter{
		KnowledgeBaseID: kbID,
	}
	if uploader := c.Query("uploaderId"); uploader != "" {
		if id, err := strconv.ParseUint(uploader, 10, 64); err == nil {
			filter.UploaderID = &id
		}
	}
	if status := c.Query("statuses"); status != "" {
		filter.ParseStatus = splitAndClean(status)
	}
	if sources := c.Query("sourceTypes"); sources != "" {
		filter.SourceTypes = splitAndClean(sources)
	}
	if connectors := c.Query("sources"); connectors != "" {
		filter.Sources = splitAndClean(connectors)
	}
	if originIDs := c.Query("originIds"); originIDs != "" {
		filter.OriginIDs = splitAndClean(originIDs)
	}

	docs, total, err := h.service.ListDocuments(c.Request.Context(), actor, kbID, filter, limit, offset)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "获取文档列表失败: "+err.Error())
		return
	}
	c.JSON(http.StatusOK, models.CommonResponse{
		Code: 0,
		Msg:  "success",
		Data: models.PageResponse{
			List:       docs,
			TotalCount: total,
			PageSize:   limit,
			CurrPage:   page,
			TotalPage:  calcTotalPages(total, limit),
		},
	})
}

func (h *KBHandlers) listIngestionConnectors(c *gin.Context) {
	if h.ingestion == nil {
		writeError(c, http.StatusNotImplemented, "多源采集功能未启用")
		return
	}
	actor, ok := h.actorFromContext(c)
	if !ok {
		return
	}
	kbID, err := parseUintParam(c, "kbId")
	if err != nil {
		writeError(c, http.StatusBadRequest, "知识库ID无效: "+err.Error())
		return
	}
	overview, err := h.ingestion.Describe(c.Request.Context(), actor, kbID)
	if err != nil {
		writeError(c, http.StatusBadRequest, "获取连接器信息失败: "+err.Error())
		return
	}
	c.JSON(http.StatusOK, models.CommonResponse{
		Code: 0,
		Msg:  "success",
		Data: overview,
	})
}

func (h *KBHandlers) parseIngestionSource(c *gin.Context) {
	if h.ingestion == nil {
		writeError(c, http.StatusNotImplemented, "多源采集功能未启用")
		return
	}
	actor, ok := h.actorFromContext(c)
	if !ok {
		return
	}
	kbID, err := parseUintParam(c, "kbId")
	if err != nil {
		writeError(c, http.StatusBadRequest, "知识库ID无效: "+err.Error())
		return
	}
	var payload struct {
		Connector   string         `json:"connector" binding:"required"`
		Params      map[string]any `json:"params"`
		Credentials map[string]any `json:"credentials"`
		Metadata    map[string]any `json:"metadata"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		writeError(c, http.StatusBadRequest, "参数错误: "+err.Error())
		return
	}
	result, err := h.ingestion.ParseSource(c.Request.Context(), actor, kbID, payload.Connector, payload.Params, payload.Credentials, payload.Metadata)
	if err != nil {
		writeError(c, http.StatusBadRequest, "解析源失败: "+err.Error())
		return
	}
	c.JSON(http.StatusOK, models.CommonResponse{
		Code: 0,
		Msg:  "success",
		Data: result,
	})
}

func (h *KBHandlers) importIngestionSelections(c *gin.Context) {
	if h.ingestion == nil {
		writeError(c, http.StatusNotImplemented, "多源采集功能未启用")
		return
	}
	actor, ok := h.actorFromContext(c)
	if !ok {
		return
	}
	kbID, err := parseUintParam(c, "kbId")
	if err != nil {
		writeError(c, http.StatusBadRequest, "知识库ID无效: "+err.Error())
		return
	}
	var payload struct {
		SessionToken string `json:"sessionToken" binding:"required"`
		Selections   []struct {
			NodeID   string         `json:"nodeId" binding:"required"`
			Metadata map[string]any `json:"metadata"`
			Title    string         `json:"title"`
		} `json:"selections" binding:"required"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		writeError(c, http.StatusBadRequest, "参数错误: "+err.Error())
		return
	}
	if len(payload.Selections) == 0 {
		writeError(c, http.StatusBadRequest, "至少选择一个节点导入")
		return
	}
	selections := make([]service.ImportSelection, 0, len(payload.Selections))
	for _, item := range payload.Selections {
		if strings.TrimSpace(item.NodeID) == "" {
			writeError(c, http.StatusBadRequest, "节点ID不能为空")
			return
		}
		selections = append(selections, service.ImportSelection{
			NodeID:   item.NodeID,
			Metadata: item.Metadata,
			Title:    item.Title,
		})
	}
	documents, jobs, err := h.ingestion.ImportSelections(c.Request.Context(), actor, kbID, payload.SessionToken, selections)
	if err != nil {
		writeError(c, http.StatusBadRequest, "导入节点失败: "+err.Error())
		return
	}
	c.JSON(http.StatusOK, models.CommonResponse{
		Code: 0,
		Msg:  "success",
		Data: gin.H{
			"documents": documents,
			"jobs":      jobs,
		},
	})
}

func (h *KBHandlers) ingestionWebhookCallback(c *gin.Context) {
	if h.ingestion == nil {
		writeError(c, http.StatusNotImplemented, "多源采集功能未启用")
		return
	}
	if !h.ingestion.WebhookEnabled() {
		writeError(c, http.StatusForbidden, "Webhook 未启用")
		return
	}
	secret := c.GetHeader("X-Ingestion-Secret")
	if !h.ingestion.VerifyWebhookSecret(secret) {
		writeError(c, http.StatusUnauthorized, "签名校验失败")
		return
	}
	if err := h.ingestion.ValidateWebhookSource(c.ClientIP()); err != nil {
		writeError(c, http.StatusForbidden, err.Error())
		return
	}
	kbID, err := parseUintParam(c, "kbId")
	if err != nil {
		writeError(c, http.StatusBadRequest, "知识库ID无效: "+err.Error())
		return
	}
	jobID, err := parseUintParam(c, "taskId")
	if err != nil {
		writeError(c, http.StatusBadRequest, "任务ID无效: "+err.Error())
		return
	}
	var payload struct {
		Status       string         `json:"status"`
		Progress     *int           `json:"progress"`
		ErrorMessage string         `json:"errorMessage"`
		ErrorType    string         `json:"errorType"`
		Payload      map[string]any `json:"payload"`
		Completed    bool           `json:"completed"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		writeError(c, http.StatusBadRequest, "参数错误: "+err.Error())
		return
	}
	event := service.IngestionWebhookEvent{
		KnowledgeBaseID: kbID,
		JobID:           jobID,
		Status:          payload.Status,
		Progress:        payload.Progress,
		ErrorMessage:    payload.ErrorMessage,
		ErrorType:       payload.ErrorType,
		Payload:         payload.Payload,
		Completed:       payload.Completed,
		RemoteAddr:      c.ClientIP(),
	}
	job, err := h.ingestion.HandleWebhookEvent(c.Request.Context(), event)
	if err != nil {
		writeError(c, http.StatusBadRequest, "Webhook处理失败: "+err.Error())
		return
	}
	c.JSON(http.StatusOK, models.CommonResponse{Code: 0, Msg: "success", Data: job})
}

func (h *KBHandlers) listJobWebhookEvents(c *gin.Context) {
	actor, ok := h.actorFromContext(c)
	if !ok {
		return
	}
	if h.ingestion == nil {
		writeError(c, http.StatusNotImplemented, "多源采集功能未启用")
		return
	}
	kbID, err := parseUintParam(c, "kbId")
	if err != nil {
		writeError(c, http.StatusBadRequest, "知识库ID无效: "+err.Error())
		return
	}
	jobID, err := parseUintParam(c, "jobId")
	if err != nil {
		writeError(c, http.StatusBadRequest, "任务ID无效: "+err.Error())
		return
	}
	limit := 50
	if v := strings.TrimSpace(c.Query("limit")); v != "" {
		if parsed, convErr := strconv.Atoi(v); convErr == nil && parsed > 0 {
			limit = parsed
		}
	}
	logs, err := h.ingestion.ListWebhookEvents(c.Request.Context(), actor, kbID, jobID, limit)
	if err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}
	c.JSON(http.StatusOK, models.CommonResponse{Code: 0, Msg: "success", Data: logs})
}

func (h *KBHandlers) listChunks(c *gin.Context) {
	actor, ok := h.actorFromContext(c)
	if !ok {
		return
	}
	kbID, err := parseUintParam(c, "kbId")
	if err != nil {
		writeError(c, http.StatusBadRequest, "知识库ID无效: "+err.Error())
		return
	}
	docID, err := parseUintParam(c, "docId")
	if err != nil {
		writeError(c, http.StatusBadRequest, "文档ID无效: "+err.Error())
		return
	}
	page, limit := parsePageLimit(c)
	offset := (page - 1) * limit

	filter := repository.KBChunkFilter{
		DocumentID: docID,
	}
	if manual := c.Query("manualEdit"); manual != "" {
		value := manual == "true"
		filter.ManualEdit = &value
	}

	chunks, total, err := h.service.ListChunks(c.Request.Context(), actor, kbID, filter, limit, offset)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "获取分片失败: "+err.Error())
		return
	}
	c.JSON(http.StatusOK, models.CommonResponse{
		Code: 0,
		Msg:  "success",
		Data: models.PageResponse{
			List:       chunks,
			TotalCount: total,
			PageSize:   limit,
			CurrPage:   page,
			TotalPage:  calcTotalPages(total, limit),
		},
	})
}

func (h *KBHandlers) createChunks(c *gin.Context) {
	actor, ok := h.actorFromContext(c)
	if !ok {
		return
	}
	kbID, err := parseUintParam(c, "kbId")
	if err != nil {
		writeError(c, http.StatusBadRequest, "知识库ID无效: "+err.Error())
		return
	}
	docID, err := parseUintParam(c, "docId")
	if err != nil {
		writeError(c, http.StatusBadRequest, "文档ID无效: "+err.Error())
		return
	}

	var payload struct {
		Chunks []struct {
			ChunkKey string         `json:"chunkKey"`
			Content  string         `json:"content" binding:"required"`
			Metadata map[string]any `json:"metadata"`
			Manual   bool           `json:"manualEdit"`
			QAQ      string         `json:"qaQuestion"`
			QAA      string         `json:"qaAnswer"`
		} `json:"chunks" binding:"required"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		writeError(c, http.StatusBadRequest, "参数错误: "+err.Error())
		return
	}
	if len(payload.Chunks) == 0 {
		writeError(c, http.StatusBadRequest, "chunks不能为空")
		return
	}

	drafts := make([]service.ChunkDraft, 0, len(payload.Chunks))
	for idx, chunk := range payload.Chunks {
		key := chunk.ChunkKey
		if key == "" {
			key = strconv.FormatInt(int64(idx), 10)
		}
		drafts = append(drafts, service.ChunkDraft{
			DocumentID: docID,
			ChunkKey:   key,
			Content:    chunk.Content,
			Metadata:   chunk.Metadata,
			ManualEdit: chunk.Manual,
			QAQuestion: chunk.QAQ,
			QAAnswer:   chunk.QAA,
		})
	}
	if err := h.service.CreateChunks(c.Request.Context(), actor, kbID, drafts); err != nil {
		writeError(c, http.StatusInternalServerError, "创建分片失败: "+err.Error())
		return
	}
	c.JSON(http.StatusOK, models.CommonResponse{Code: 0, Msg: "success"})
}

func (h *KBHandlers) updateChunk(c *gin.Context) {
	actor, ok := h.actorFromContext(c)
	if !ok {
		return
	}
	chunkID, err := parseUintParam(c, "chunkId")
	if err != nil {
		writeError(c, http.StatusBadRequest, "分片ID无效: "+err.Error())
		return
	}
	var payload struct {
		Content           *string        `json:"content"`
		Metadata          map[string]any `json:"metadata"`
		ManualEdit        *bool          `json:"manualEdit"`
		QAQuestion        *string        `json:"qaQuestion"`
		QAAnswer          *string        `json:"qaAnswer"`
		EmbeddingVectorID *string        `json:"embeddingVectorId"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		writeError(c, http.StatusBadRequest, "参数错误: "+err.Error())
		return
	}
	chunk, err := h.service.UpdateChunk(c.Request.Context(), actor, chunkID, service.UpdateChunkInput{
		Content:           payload.Content,
		Metadata:          payload.Metadata,
		ManualEdit:        payload.ManualEdit,
		QAQuestion:        payload.QAQuestion,
		QAAnswer:          payload.QAAnswer,
		EmbeddingVectorID: payload.EmbeddingVectorID,
	})
	if err != nil {
		writeError(c, http.StatusInternalServerError, "更新分片失败: "+err.Error())
		return
	}
	c.JSON(http.StatusOK, models.CommonResponse{
		Code: 0,
		Msg:  "success",
		Data: chunk,
	})
}

func (h *KBHandlers) listJobs(c *gin.Context) {
	actor, ok := h.actorFromContext(c)
	if !ok {
		return
	}
	kbID, err := parseUintParam(c, "kbId")
	if err != nil {
		writeError(c, http.StatusBadRequest, "知识库ID无效: "+err.Error())
		return
	}
	page, limit := parsePageLimit(c)
	offset := (page - 1) * limit

	filter := repository.KBJobFilter{
		KnowledgeBaseID: kbID,
	}
	if jobTypes := c.Query("jobTypes"); jobTypes != "" {
		filter.JobTypes = splitAndClean(jobTypes)
	}
	if statuses := c.Query("statuses"); statuses != "" {
		filter.Status = splitAndClean(statuses)
	}

	jobs, total, err := h.service.ListJobs(c.Request.Context(), actor, filter, limit, offset)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "获取任务列表失败: "+err.Error())
		return
	}
	c.JSON(http.StatusOK, models.CommonResponse{
		Code: 0,
		Msg:  "success",
		Data: models.PageResponse{
			List:       jobs,
			TotalCount: total,
			PageSize:   limit,
			CurrPage:   page,
			TotalPage:  calcTotalPages(total, limit),
		},
	})
}

func (h *KBHandlers) createJob(c *gin.Context) {
	actor, ok := h.actorFromContext(c)
	if !ok {
		return
	}
	kbID, err := parseUintParam(c, "kbId")
	if err != nil {
		writeError(c, http.StatusBadRequest, "知识库ID无效: "+err.Error())
		return
	}
	var payload struct {
		JobType    string         `json:"jobType" binding:"required"`
		DocumentID *uint64        `json:"documentId"`
		Payload    map[string]any `json:"payload"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		writeError(c, http.StatusBadRequest, "参数错误: "+err.Error())
		return
	}
	job, err := h.service.EnqueueJob(c.Request.Context(), actor, kbID, payload.JobType, payload.Payload, payload.DocumentID)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "创建任务失败: "+err.Error())
		return
	}
	c.JSON(http.StatusOK, models.CommonResponse{
		Code: 0,
		Msg:  "success",
		Data: job,
	})
}

func (h *KBHandlers) streamJobEvents(c *gin.Context) {
	if h.events == nil {
		c.Status(http.StatusNoContent)
		return
	}
	actor, ok := h.actorFromContext(c)
	if !ok {
		return
	}
	kbID, err := parseUintParam(c, "kbId")
	if err != nil {
		writeError(c, http.StatusBadRequest, "知识库ID无效: "+err.Error())
		return
	}
	if _, err := h.service.GetKnowledgeBase(c.Request.Context(), actor, kbID); err != nil {
		writeError(c, http.StatusInternalServerError, "获取知识库失败: "+err.Error())
		return
	}

	sub := h.events.Subscribe(kbID)
	if sub == nil {
		c.Status(http.StatusNoContent)
		return
	}
	defer sub.Close()

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		writeError(c, http.StatusInternalServerError, "SSE不受支持")
		return
	}

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Status(http.StatusOK)
	flusher.Flush()

	ctx := c.Request.Context()
	heartbeat := time.NewTicker(25 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-heartbeat.C:
			fmt.Fprint(c.Writer, ": ping\n\n")
			flusher.Flush()
		case event, ok := <-sub.Events:
			if !ok {
				return
			}
			payload, err := json.Marshal(event)
			if err != nil {
				continue
			}
			fmt.Fprintf(c.Writer, "data: %s\n\n", payload)
			flusher.Flush()
		}
	}
}

func (h *KBHandlers) retryJob(c *gin.Context) {
	actor, ok := h.actorFromContext(c)
	if !ok {
		return
	}
	kbID, err := parseUintParam(c, "kbId")
	if err != nil {
		writeError(c, http.StatusBadRequest, "知识库ID无效: "+err.Error())
		return
	}
	jobID, err := parseUintParam(c, "jobId")
	if err != nil {
		writeError(c, http.StatusBadRequest, "任务ID无效: "+err.Error())
		return
	}
	var payload struct {
		Force bool `json:"force"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil && !errors.Is(err, io.EOF) {
		writeError(c, http.StatusBadRequest, "参数错误: "+err.Error())
		return
	}
	job, err := h.service.RetryJob(c.Request.Context(), actor, kbID, jobID, payload.Force)
	if err != nil {
		writeError(c, http.StatusBadRequest, "重试任务失败: "+err.Error())
		return
	}
	c.JSON(http.StatusOK, models.CommonResponse{Code: 0, Msg: "success", Data: job})
}

func (h *KBHandlers) pauseJobs(c *gin.Context) {
	actor, ok := h.actorFromContext(c)
	if !ok {
		return
	}
	if err := h.service.PauseJobRunner(c.Request.Context(), actor); err != nil {
		writeError(c, http.StatusForbidden, "暂停任务失败: "+err.Error())
		return
	}
	c.JSON(http.StatusOK, models.CommonResponse{Code: 0, Msg: "success"})
}

func (h *KBHandlers) resumeJobs(c *gin.Context) {
	actor, ok := h.actorFromContext(c)
	if !ok {
		return
	}
	if err := h.service.ResumeJobRunner(c.Request.Context(), actor); err != nil {
		writeError(c, http.StatusForbidden, "恢复任务失败: "+err.Error())
		return
	}
	c.JSON(http.StatusOK, models.CommonResponse{Code: 0, Msg: "success"})
}

func (h *KBHandlers) jobRunnerStatus(c *gin.Context) {
	actor, ok := h.actorFromContext(c)
	if !ok {
		return
	}
	status, err := h.service.JobRunnerStatus(c.Request.Context(), actor)
	if err != nil {
		writeError(c, http.StatusForbidden, "获取任务状态失败: "+err.Error())
		return
	}
	c.JSON(http.StatusOK, models.CommonResponse{Code: 0, Msg: "success", Data: status})
}

func (h *KBHandlers) jobStatusSummary(c *gin.Context) {
	actor, ok := h.actorFromContext(c)
	if !ok {
		return
	}
	kbID, err := parseUintParam(c, "kbId")
	if err != nil {
		writeError(c, http.StatusBadRequest, "知识库ID无效: "+err.Error())
		return
	}
	summary, err := h.service.JobStatusSummary(c.Request.Context(), actor, kbID)
	if err != nil {
		writeError(c, http.StatusForbidden, "获取任务统计失败: "+err.Error())
		return
	}
	c.JSON(http.StatusOK, models.CommonResponse{Code: 0, Msg: "success", Data: summary})
}

func (h *KBHandlers) queryKnowledgeBase(c *gin.Context) {
	actor, ok := h.actorFromContext(c)
	if !ok {
		return
	}
	kbID, err := parseUintParam(c, "kbId")
	if err != nil {
		writeError(c, http.StatusBadRequest, "知识库ID无效: "+err.Error())
		return
	}
	var payload struct {
		Query string `json:"query" binding:"required"`
		TopK  int    `json:"topK"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		writeError(c, http.StatusBadRequest, "参数错误: "+err.Error())
		return
	}
	results, err := h.service.QueryKnowledgeBase(c.Request.Context(), actor, kbID, payload.Query, payload.TopK)
	if err != nil {
		if err == vectorstore.ErrNotConfigured {
			writeError(c, http.StatusServiceUnavailable, "向量检索未配置")
			return
		}
		writeError(c, http.StatusInternalServerError, "检索失败: "+err.Error())
		return
	}
	c.JSON(http.StatusOK, models.CommonResponse{
		Code: 0,
		Msg:  "success",
		Data: results,
	})
}

// internalQueryKnowledgeBase allows backend-server to query with server-secret auth.
func (h *KBHandlers) internalQueryKnowledgeBase(c *gin.Context) {
	kbID, err := parseUintParam(c, "kbId")
	if err != nil {
		writeError(c, http.StatusBadRequest, "知识库ID无效: "+err.Error())
		return
	}
	var payload struct {
		Query          string                 `json:"query" binding:"required"`
		TopK           int                    `json:"top_k"`
		ScoreThreshold float64                `json:"score_threshold"`
		RerankModel    string                 `json:"rerank_model"`
		Filters        map[string]interface{} `json:"filters"`
		TimeoutMs      int                    `json:"timeout_ms"`
		Debug          bool                   `json:"debug"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		writeError(c, http.StatusBadRequest, "参数错误: "+err.Error())
		return
	}
	ctx := c.Request.Context()
	results, err := h.service.InternalQueryKnowledgeBase(ctx, kbID, service.InternalQueryOptions{
		Query:          payload.Query,
		TopK:           payload.TopK,
		ScoreThreshold: payload.ScoreThreshold,
		RerankModel:    payload.RerankModel,
		Filters:        payload.Filters,
		TimeoutMs:      payload.TimeoutMs,
		Debug:          payload.Debug,
	})
	if err != nil {
		if errors.Is(err, vectorstore.ErrNotConfigured) {
			writeError(c, http.StatusServiceUnavailable, "向量检索未配置")
			return
		}
		writeError(c, http.StatusInternalServerError, "检索失败: "+err.Error())
		return
	}
	c.JSON(http.StatusOK, models.CommonResponse{
		Code: 0,
		Msg:  "success",
		Data: results,
	})
}

// internalKnowledgeBaseMetadata returns KB metadata for backend warmup.
func (h *KBHandlers) internalKnowledgeBaseMetadata(c *gin.Context) {
	kbID, err := parseUintParam(c, "kbId")
	if err != nil {
		writeError(c, http.StatusBadRequest, "知识库ID无效: "+err.Error())
		return
	}
	meta, err := h.service.GetKnowledgeBaseMetadata(c.Request.Context(), kbID)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "获取知识库元数据失败: "+err.Error())
		return
	}
	c.JSON(http.StatusOK, models.CommonResponse{
		Code: 0,
		Msg:  "success",
		Data: meta,
	})
}

// internalIngestMeetingMinutes ingests meeting minutes through server-secret auth.
func (h *KBHandlers) internalIngestMeetingMinutes(c *gin.Context) {
	deviceID := strings.TrimSpace(c.PostForm("device_id"))
	if deviceID == "" {
		writeError(c, http.StatusBadRequest, "device_id is required")
		return
	}
	rawKBID := strings.TrimSpace(c.PostForm("kb_id"))
	var kbID uint64
	if rawKBID != "" {
		parsed, err := strconv.ParseUint(rawKBID, 10, 64)
		if err != nil {
			writeError(c, http.StatusBadRequest, "kb_id is invalid: "+err.Error())
			return
		}
		kbID = parsed
	}
	var agentID uint64
	if rawAgentID := strings.TrimSpace(c.PostForm("agent_id")); rawAgentID != "" {
		parsed, err := strconv.ParseUint(rawAgentID, 10, 64)
		if err != nil {
			writeError(c, http.StatusBadRequest, "agent_id is invalid: "+err.Error())
			return
		}
		agentID = parsed
	}

	fileHeader, err := c.FormFile("file")
	if err != nil {
		writeError(c, http.StatusBadRequest, "missing file: "+err.Error())
		return
	}
	file, err := fileHeader.Open()
	if err != nil {
		writeError(c, http.StatusBadRequest, "读取上传文件失败: "+err.Error())
		return
	}
	defer file.Close()

	if h.deviceRepo == nil {
		writeError(c, http.StatusInternalServerError, "device repo not configured")
		return
	}
	device, err := h.deviceRepo.FindByMacAddress(c.Request.Context(), deviceID)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "获取设备失败: "+err.Error())
		return
	}
	if device == nil {
		device, err = h.deviceRepo.FindByID(c.Request.Context(), deviceID)
		if err != nil {
			writeError(c, http.StatusInternalServerError, "获取设备失败: "+err.Error())
			return
		}
	}
	if device == nil || device.UserID == nil || *device.UserID == 0 {
		writeError(c, http.StatusBadRequest, "设备未绑定用户")
		return
	}

	actor := service.ActorContext{UserID: *device.UserID, IsAdmin: true}
	if kbID == 0 {
		if agentID == 0 {
			agentID = *device.UserID
		}
		kb, err := h.service.EnsureMeetingMinutesKnowledgeBase(c.Request.Context(), actor, agentID)
		if err != nil {
			writeError(c, http.StatusInternalServerError, "创建会议纪要知识库失败: "+err.Error())
			return
		}
		kbID = kb.ID
	}

	docs, jobs, err := h.service.UploadDocuments(c.Request.Context(), actor, kbID, []service.UploadDocumentInput{
		{
			FileName:    fileHeader.Filename,
			Size:        fileHeader.Size,
			ContentType: fileHeader.Header.Get("Content-Type"),
			Reader:      file,
		},
	})
	if err != nil {
		writeError(c, http.StatusBadRequest, "上传会议纪要失败: "+err.Error())
		return
	}

	var docID uint64
	if len(docs) > 0 && docs[0] != nil {
		docID = docs[0].ID
	}
	var jobID uint64
	if len(jobs) > 0 && jobs[0] != nil {
		jobID = jobs[0].ID
	}

	c.JSON(http.StatusOK, models.CommonResponse{
		Code: 0,
		Msg:  "success",
		Data: map[string]any{
			"knowledgeBaseId": kbID,
			"documentId":      docID,
			"jobId":           jobID,
		},
	})
}

func (h *KBHandlers) grantPermission(c *gin.Context) {
	actor, ok := h.actorFromContext(c)
	if !ok {
		return
	}
	kbID, err := parseUintParam(c, "kbId")
	if err != nil {
		writeError(c, http.StatusBadRequest, "知识库ID无效: "+err.Error())
		return
	}
	var payload struct {
		UserID uint64 `json:"userId" binding:"required"`
		Role   string `json:"role" binding:"required"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		writeError(c, http.StatusBadRequest, "参数错误: "+err.Error())
		return
	}
	if err := h.service.GrantPermission(c.Request.Context(), actor, service.PermissionGrantInput{
		KnowledgeBaseID: kbID,
		UserID:          payload.UserID,
		Role:            payload.Role,
	}); err != nil {
		writeError(c, http.StatusInternalServerError, "添加权限失败: "+err.Error())
		return
	}
	c.JSON(http.StatusOK, models.CommonResponse{Code: 0, Msg: "success"})
}

func (h *KBHandlers) listPermissions(c *gin.Context) {
	actor, ok := h.actorFromContext(c)
	if !ok {
		return
	}
	kbID, err := parseUintParam(c, "kbId")
	if err != nil {
		writeError(c, http.StatusBadRequest, "知识库ID无效: "+err.Error())
		return
	}
	perms, err := h.service.ListPermissions(c.Request.Context(), actor, kbID)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "查询权限失败: "+err.Error())
		return
	}
	c.JSON(http.StatusOK, models.CommonResponse{
		Code: 0,
		Msg:  "success",
		Data: perms,
	})
}

func (h *KBHandlers) revokePermission(c *gin.Context) {
	actor, ok := h.actorFromContext(c)
	if !ok {
		return
	}
	kbID, err := parseUintParam(c, "kbId")
	if err != nil {
		writeError(c, http.StatusBadRequest, "知识库ID无效: "+err.Error())
		return
	}
	userID, err := parseUintParam(c, "userId")
	if err != nil {
		writeError(c, http.StatusBadRequest, "用户ID无效: "+err.Error())
		return
	}
	if err := h.service.RevokePermission(c.Request.Context(), actor, kbID, userID); err != nil {
		writeError(c, http.StatusInternalServerError, "移除权限失败: "+err.Error())
		return
	}
	c.JSON(http.StatusOK, models.CommonResponse{Code: 0, Msg: "success"})
}

func (h *KBHandlers) mountAgentProject(c *gin.Context) {
	actor, ok := h.actorFromContext(c)
	if !ok {
		return
	}
	projectID, err := parseUUIDParam(c, "projectId")
	if err != nil {
		writeError(c, http.StatusBadRequest, "项目ID无效: "+err.Error())
		return
	}
	var payload struct {
		AgentID      uint64         `json:"agentId" binding:"required"`
		Capabilities map[string]any `json:"capabilities"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		writeError(c, http.StatusBadRequest, "参数错误: "+err.Error())
		return
	}
	if err := h.service.MountAgentProject(c.Request.Context(), actor, service.AgentProjectMountInput{
		AgentID:      payload.AgentID,
		ProjectID:    projectID,
		Capabilities: payload.Capabilities,
	}); err != nil {
		writeError(c, http.StatusInternalServerError, "挂载代理失败: "+err.Error())
		return
	}
	c.JSON(http.StatusOK, models.CommonResponse{Code: 0, Msg: "success"})
}

func (h *KBHandlers) unmountAgentProject(c *gin.Context) {
	actor, ok := h.actorFromContext(c)
	if !ok {
		return
	}
	projectID, err := parseUUIDParam(c, "projectId")
	if err != nil {
		writeError(c, http.StatusBadRequest, "项目ID无效: "+err.Error())
		return
	}
	agentID, err := parseUintParam(c, "agentId")
	if err != nil {
		writeError(c, http.StatusBadRequest, "Agent ID无效: "+err.Error())
		return
	}
	if err := h.service.UnmountAgentProject(c.Request.Context(), actor, agentID, projectID); err != nil {
		writeError(c, http.StatusInternalServerError, "取消挂载失败: "+err.Error())
		return
	}
	c.JSON(http.StatusOK, models.CommonResponse{Code: 0, Msg: "success"})
}

func (h *KBHandlers) listAgentMounts(c *gin.Context) {
	actor, ok := h.actorFromContext(c)
	if !ok {
		return
	}
	agentID, err := parseUintParam(c, "agentId")
	if err != nil {
		writeError(c, http.StatusBadRequest, "Agent ID无效: "+err.Error())
		return
	}
	mounts, err := h.service.ListAgentProjectMounts(c.Request.Context(), actor, agentID)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "查询挂载失败: "+err.Error())
		return
	}
	c.JSON(http.StatusOK, models.CommonResponse{
		Code: 0,
		Msg:  "success",
		Data: mounts,
	})
}

func (h *KBHandlers) actorFromContext(c *gin.Context) (service.ActorContext, bool) {
	userID, exists := middleware.GetUserIDFromContext(c)
	if !exists {
		writeError(c, http.StatusUnauthorized, "未认证用户")
		return service.ActorContext{}, false
	}
	isAdmin := middleware.IsSuperAdminFromContext(c)
	return service.ActorContext{
		UserID:  userID,
		IsAdmin: isAdmin,
	}, true
}

func writeError(c *gin.Context, status int, msg string) {
	c.JSON(status, models.CommonResponse{
		Code: status,
		Msg:  msg,
	})
}

func parseUintParam(c *gin.Context, name string) (uint64, error) {
	value := c.Param(name)
	if value == "" {
		return 0, strconv.ErrSyntax
	}
	return strconv.ParseUint(value, 10, 64)
}

func parseUUIDParam(c *gin.Context, name string) (string, error) {
	value := strings.TrimSpace(c.Param(name))
	if value == "" {
		return "", errors.New("empty parameter")
	}
	if _, err := uuid.Parse(value); err != nil {
		return "", err
	}
	return value, nil
}

func splitAndClean(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func parsePageLimit(c *gin.Context) (int, int) {
	page := 1
	limit := 20
	if pageStr := c.Query("page"); pageStr != "" {
		if p, err := strconv.Atoi(pageStr); err == nil && p > 0 {
			page = p
		}
	}
	if limitStr := c.Query("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 100 {
			limit = l
		}
	}
	return page, limit
}

func calcTotalPages(total int64, pageSize int) int {
	if pageSize <= 0 {
		return 1
	}
	pages := int(total) / pageSize
	if int(total)%pageSize != 0 {
		pages++
	}
	if pages == 0 {
		pages = 1
	}
	return pages
}
