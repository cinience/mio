package httpapi

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"backend-server/internal/domain/imagegen"
	log "backend-server/internal/infrastructure/logger"
)

type imageGenerationRequest struct {
	Prompt            string `json:"prompt"`
	Model             string `json:"model,omitempty"`
	N                 int    `json:"n,omitempty"`
	Quality           string `json:"quality,omitempty"`
	Size              string `json:"size,omitempty"`
	Style             string `json:"style,omitempty"`
	ResponseFormat    string `json:"response_format,omitempty"`
	User              string `json:"user,omitempty"`
	Background        string `json:"background,omitempty"`
	Moderation        string `json:"moderation,omitempty"`
	OutputCompression int    `json:"output_compression,omitempty"`
	OutputFormat      string `json:"output_format,omitempty"`
}

func (h *Handler) handleImageGeneration(c *gin.Context) {
	if c.Request.Method != http.MethodPost {
		c.AbortWithStatusJSON(http.StatusMethodNotAllowed, gin.H{"error": "仅支持POST请求"})
		return
	}

	deviceID := c.GetHeader("Device-Id")
	clientID := c.GetHeader("Client-Id")
	if deviceID == "" {
		log.Error("缺少Device-Id")
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "缺少Device-Id"})
		return
	}

	var req imageGenerationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Errorf("解析生图请求失败: %v", err)
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "请求体解析失败"})
		return
	}

	req.Prompt = strings.TrimSpace(req.Prompt)
	if req.Prompt == "" {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "prompt不能为空"})
		return
	}

	providerName, providerConfig, err := imagegen.ResolveProvider(c.Request.Context(), h.managerAPI, deviceID, clientID)
	if err != nil {
		log.Errorf("获取生图provider失败: %v", err)
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "未找到生图配置"})
		return
	}

	provider, err := imagegen.GetProvider(providerName, providerConfig)
	if err != nil {
		log.Errorf("创建生图provider失败: %v", err)
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "创建生图provider失败"})
		return
	}

	result, err := provider.Generate(c.Request.Context(), &imagegen.GenerateRequest{
		Prompt:            req.Prompt,
		Model:             req.Model,
		N:                 req.N,
		Quality:           req.Quality,
		Size:              req.Size,
		Style:             req.Style,
		ResponseFormat:    req.ResponseFormat,
		User:              req.User,
		Background:        req.Background,
		Moderation:        req.Moderation,
		OutputCompression: req.OutputCompression,
		OutputFormat:      req.OutputFormat,
	})
	if err != nil {
		log.Errorf("生图生成失败: %v", err)
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "生图生成失败"})
		return
	}

	c.JSON(http.StatusOK, result)
}
