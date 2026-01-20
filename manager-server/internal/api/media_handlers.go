package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"manager-server/internal/constants"
	"manager-server/internal/models"
	"manager-server/internal/service"
)

// MediaHandlers hosts HTTP endpoints for the media library.
type MediaHandlers struct {
	service       *service.MediaService
	paramsService service.SysParamsService
}

// NewMediaHandlers constructs a MediaHandlers instance.
func NewMediaHandlers(svc *service.MediaService, paramsService service.SysParamsService) *MediaHandlers {
	return &MediaHandlers{service: svc, paramsService: paramsService}
}

func (h *MediaHandlers) registerRoutes(router *gin.RouterGroup) {
	router.GET("", h.listMedia)
	router.POST("", h.uploadMedia)
	router.DELETE("/:mediaId", h.deleteMedia)
	router.GET("/:mediaId/content", h.streamMedia)
}

func (h *MediaHandlers) registerPublicRoutes(router *gin.RouterGroup) {
	router.GET("/:mediaId/content", h.streamMediaPublic)
}

func (h *MediaHandlers) listMedia(c *gin.Context) {
	page, limit := parsePageLimit(c)
	offset := (page - 1) * limit

	assets, total, err := h.service.ListMedia(c.Request.Context(), service.ListMediaInput{
		AgentID:   c.Query("agentId"),
		DeviceID:  c.Query("deviceId"),
		MediaType: c.Query("mediaType"),
		Query:     c.Query("q"),
		Limit:     limit,
		Offset:    offset,
	})
	if err != nil {
		writeError(c, http.StatusInternalServerError, "获取媒体列表失败: "+err.Error())
		return
	}

	c.JSON(http.StatusOK, models.CommonResponse{
		Code: 0,
		Msg:  "success",
		Data: models.PageResponse{
			List:       assets,
			TotalCount: total,
			PageSize:   limit,
			CurrPage:   page,
			TotalPage:  calcTotalPages(total, limit),
		},
	})
}

func (h *MediaHandlers) uploadMedia(c *gin.Context) {
	fileHeader, err := c.FormFile("file")
	if err != nil {
		writeError(c, http.StatusBadRequest, "请上传图片、视频或音频文件")
		return
	}

	file, err := fileHeader.Open()
	if err != nil {
		writeError(c, http.StatusInternalServerError, "读取上传文件失败: "+err.Error())
		return
	}
	defer file.Close()

	input := service.UploadMediaInput{
		AgentID:      c.PostForm("agentId"),
		DeviceID:     c.PostForm("deviceId"),
		OriginalName: fileHeader.Filename,
		ContentType:  fileHeader.Header.Get("Content-Type"),
		Size:         fileHeader.Size,
		Reader:       file,
		Source:       c.PostForm("source"),
		Description:  c.PostForm("description"),
	}

	if metaRaw := strings.TrimSpace(c.PostForm("relationInfo")); metaRaw != "" {
		var related map[string]any
		if err := json.Unmarshal([]byte(metaRaw), &related); err != nil {
			writeError(c, http.StatusBadRequest, "关联信息不是有效的JSON: "+err.Error())
			return
		}
		input.RelatedInfo = related
	}

	asset, err := h.service.UploadMedia(c.Request.Context(), input)
	if err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}

	asset.PublicURL = h.buildPublicMediaURL(c, asset.ID, time.Now().Add(72*time.Hour), asset.FileName)

	c.JSON(http.StatusOK, models.CommonResponse{
		Code: 0,
		Msg:  "success",
		Data: asset,
	})
}

func (h *MediaHandlers) deleteMedia(c *gin.Context) {
	mediaID, err := parseUintParam(c, "mediaId")
	if err != nil {
		writeError(c, http.StatusBadRequest, "媒体ID无效")
		return
	}

	if err := h.service.DeleteMedia(c.Request.Context(), mediaID); err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}

	c.JSON(http.StatusOK, models.CommonResponse{
		Code: 0,
		Msg:  "success",
	})
}

func (h *MediaHandlers) streamMedia(c *gin.Context) {
	mediaID, err := parseUintParam(c, "mediaId")
	if err != nil {
		writeError(c, http.StatusBadRequest, "媒体ID无效")
		return
	}
	h.streamMediaContent(c, mediaID)
}

func (h *MediaHandlers) streamMediaPublic(c *gin.Context) {
	mediaID, err := parseUintParam(c, "mediaId")
	if err != nil {
		writeError(c, http.StatusBadRequest, "媒体ID无效")
		return
	}
	if !h.verifyMediaToken(c, mediaID) {
		writeError(c, http.StatusUnauthorized, "无效或过期的媒体访问凭证")
		return
	}
	h.streamMediaContent(c, mediaID)
}

func (h *MediaHandlers) streamMediaContent(c *gin.Context, mediaID uint64) {
	asset, reader, err := h.service.OpenMedia(c.Request.Context(), mediaID)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "读取媒体文件失败: "+err.Error())
		return
	}
	if asset == nil || reader == nil {
		writeError(c, http.StatusNotFound, "媒体不存在")
		return
	}
	defer reader.Close()

	contentType := asset.ContentType
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	fileName := asset.FileName
	if fileName == "" {
		fileName = fmt.Sprintf("media-%d", asset.ID)
	}

	c.Header("Content-Type", contentType)
	c.Header("Content-Length", strconv.FormatInt(asset.FileSize, 10))
	c.Header("Content-Disposition", fmt.Sprintf("inline; filename=\"%s\"", fileName))

	if _, err := io.Copy(c.Writer, reader); err != nil {
		// streaming errors cannot change response status code at this point, log only
		c.Error(fmt.Errorf("stream media %d: %w", asset.ID, err)) //nolint:errcheck
	}
}

func (h *MediaHandlers) buildPublicMediaURL(c *gin.Context, mediaID uint64, expiresAt time.Time, fileName string) string {
	token := h.signMediaToken(c.Request.Context(), mediaID, expiresAt.Unix())
	if token == "" || mediaID == 0 {
		return ""
	}
	host := strings.TrimSpace(c.GetHeader("X-Forwarded-Host"))
	if host == "" {
		host = strings.TrimSpace(c.Request.Host)
	}
	if host == "" {
		return ""
	}
	proto := strings.TrimSpace(c.GetHeader("X-Forwarded-Proto"))
	if proto == "" {
		if c.Request.TLS != nil {
			proto = "https"
		} else {
			proto = "http"
		}
	}
	publicPath := fmt.Sprintf("%s/media/public/%d/content", apiBasePath, mediaID)
	query := url.Values{}
	query.Set("token", token)
	query.Set("expires", strconv.FormatInt(expiresAt.Unix(), 10))
	if trimmed := strings.TrimSpace(fileName); trimmed != "" {
		query.Set("filename", trimmed)
	}
	return fmt.Sprintf("%s://%s%s?%s", proto, host, publicPath, query.Encode())
}

func (h *MediaHandlers) signMediaToken(ctx context.Context, mediaID uint64, expires int64) string {
	if h.paramsService == nil {
		return ""
	}
	secret, err := h.paramsService.GetValue(ctx, constants.SERVER_SECRET, "")
	if err != nil {
		return ""
	}
	secret = strings.TrimSpace(secret)
	if secret == "" || mediaID == 0 || expires <= 0 {
		return ""
	}
	payload := fmt.Sprintf("%d:%d", mediaID, expires)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

func (h *MediaHandlers) verifyMediaToken(c *gin.Context, mediaID uint64) bool {
	token := strings.TrimSpace(c.Query("token"))
	if token == "" {
		return false
	}
	expiresRaw := strings.TrimSpace(c.Query("expires"))
	if expiresRaw == "" {
		return false
	}
	expires, err := strconv.ParseInt(expiresRaw, 10, 64)
	if err != nil || expires <= 0 {
		return false
	}
	if time.Now().Unix() > expires {
		return false
	}
	expected := h.signMediaToken(c.Request.Context(), mediaID, expires)
	if expected == "" {
		return false
	}
	return hmac.Equal([]byte(expected), []byte(token))
}
