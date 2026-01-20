package httpapi

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"backend-server/internal/adapters/manager"
	"backend-server/internal/adapters/manager/types"
	"backend-server/internal/config"
	log "backend-server/internal/infrastructure/logger"
	chatutils "backend-server/internal/server/chat/utils"
)

func (h *Handler) handleVision(c *gin.Context) {
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

	cfg := config.GetConfig()
	if cfg.Vision.EnableAuth {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			log.Error("缺少Authorization")
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "缺少Authorization"})
			return
		}
		token := strings.TrimPrefix(authHeader, "Bearer ")
		if err := chatutils.VisvionAuth(token); err != nil {
			log.Errorf("图片识别认证失败: %v", err)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "图片识别认证失败"})
			return
		}
	}

	question := c.PostForm("question")
	if question == "" {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "缺少question参数"})
		return
	}

	file, fileHeader, err := c.Request.FormFile("file")
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "缺少file参数或文件读取失败"})
		return
	}
	defer file.Close()

	fileBytes, err := io.ReadAll(file)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "文件读取失败"})
		return
	}
	contentType := fileHeader.Header.Get("Content-Type")
	if contentType == "" {
		contentType = http.DetectContentType(fileBytes)
	}

	managerService := h.managerAPI
	var overrideProvider string
	var overrideConfig map[string]interface{}

	if managerService != nil {
		clientIdentifier := clientID
		if clientIdentifier == "" {
			clientIdentifier = deviceID
		}

		selectedModules := map[string]string{"VLLM": ""}

		agentConfig, err := managerService.GetDeviceConfig(c.Request.Context(), deviceID, clientIdentifier, selectedModules, false)
		if err != nil {
			log.Warnf("通过manager-api获取设备 %s 的VLLM配置失败: %v", deviceID, err)
		} else if agentConfig != nil {
			var selectedVLLM string
			if agentConfig.SelectedModule != nil {
				if val, ok := agentConfig.SelectedModule["VLLM"]; ok {
					selectedVLLM = val
				}
			}

			if selectedVLLM != "" && agentConfig.VLLM != nil {
				if cfgMap, ok := agentConfig.VLLM[selectedVLLM]; ok {
					overrideConfig = cfgMap
				}
			}

			if overrideConfig == nil && len(agentConfig.VLLM) == 1 {
				for _, cfgMap := range agentConfig.VLLM {
					overrideConfig = cfgMap
					break
				}
			}

			if overrideConfig != nil {
				if typ, ok := overrideConfig["type"].(string); ok {
					overrideProvider = typ
				}
			}
		}
	}

	log.Debugf("vision api called, deviceId:%s, question:%s, overrideProvider:%s, overrideConfig:%v", deviceID, question, overrideProvider, overrideConfig)

	start := time.Now()
	result, err := chatutils.HandleVLM(deviceID, fileBytes, question, overrideProvider, overrideConfig)
	if err != nil {
		log.Errorf("图片识别失败: %v", err)
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "图片识别失败"})
		return
	}
	visionDuration := time.Since(start)

	var uploadedAsset *types.MediaAsset
	if managerService != nil {
		uploadCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		asset, err := uploadVisionMedia(uploadCtx, managerService, deviceID, fileHeader, contentType, fileBytes, question, result, overrideProvider, visionDuration)
		if err != nil {
			log.Warnf("上传视觉媒体到manager-server失败: %v", err)
		} else {
			uploadedAsset = asset
		}
	}

	response := gin.H{
		"result": result,
	}
	if uploadedAsset != nil {
		response["mediaAsset"] = uploadedAsset
		response["mediaUrl"] = uploadedAsset.StorageURI
	}

	c.JSON(http.StatusOK, response)
}

func uploadVisionMedia(ctx context.Context, managerSvc manager_api.ManagerAPIService, deviceID string, fileHeader *multipart.FileHeader, contentType string, payload []byte, question, result, provider string, latency time.Duration) (*types.MediaAsset, error) {
	if managerSvc == nil {
		return nil, nil
	}

	description := strings.TrimSpace(question)
	if description != "" {
		runes := []rune(description)
		if len(runes) > 200 {
			description = string(runes[:200])
		}
	}

	relatedInfo := map[string]any{
		"question": question,
		"result":   result,
	}
	if provider != "" {
		relatedInfo["visionProvider"] = provider
	}
	relatedInfo["uploadedAt"] = time.Now().UTC().Format(time.RFC3339)
	if latency > 0 {
		relatedInfo["visionLatencyMs"] = latency.Milliseconds()
	}

	return managerSvc.UploadMedia(ctx, &types.MediaUploadRequest{
		DeviceID:    deviceID,
		Source:      "vision",
		Description: description,
		FileName:    fileHeader.Filename,
		ContentType: contentType,
		Reader:      bytes.NewReader(payload),
		RelatedInfo: relatedInfo,
	})
}
