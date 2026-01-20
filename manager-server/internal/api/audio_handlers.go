package api

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"manager-server/internal/logger"
	"manager-server/internal/middleware"
	"manager-server/internal/models"
	"manager-server/internal/repository"
	"manager-server/internal/service"
)

// AudioHandlers hosts podcast management endpoints.
type AudioHandlers struct {
	service *service.AudioService
}

// NewAudioHandlers constructs handlers for the audio domain.
func NewAudioHandlers(svc *service.AudioService) *AudioHandlers {
	return &AudioHandlers{service: svc}
}

const subsonicAuthContextKey = "subsonic_auth_ctx"
const subsonicDefaultCredential = "default"

type subsonicAuthInfo struct {
	actor   service.ActorContext
	project *service.AudioProjectSummary
}

func (h *AudioHandlers) subsonicAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if _, ok := h.subsonicAuthenticate(c); !ok {
			c.Abort()
			return
		}
		c.Next()
	}
}

func (h *AudioHandlers) subsonicAuthenticate(c *gin.Context) (*subsonicAuthInfo, bool) {
	if val, exists := c.Get(subsonicAuthContextKey); exists {
		if info, ok := val.(*subsonicAuthInfo); ok && info != nil {
			return info, true
		}
	}

	pathProjectID := strings.TrimSpace(c.Param("projectId"))
	if pathProjectID == "" {
		c.XML(http.StatusOK, newSubsonicErrorResponse(10, "Project ID required"))
		return nil, false
	}

	username := strings.TrimSpace(c.Query("u"))
	if username == "" {
		username = strings.TrimSpace(c.Query("username"))
	}

	rawPassword := strings.TrimSpace(c.Query("p"))
	if rawPassword == "" {
		rawPassword = strings.TrimSpace(c.Query("password"))
	}
	if decoded, ok := decodeSubsonicPassword(rawPassword); ok {
		rawPassword = decoded
	}

	basicUser, basicPass, hasBasicAuth := c.Request.BasicAuth()
	if username == "" && hasBasicAuth {
		username = strings.TrimSpace(basicUser)
	}

	token := strings.TrimSpace(c.Query("t"))
	salt := strings.TrimSpace(c.Query("s"))
	password := rawPassword

	if token != "" && salt != "" {
		candidates := []string{
			rawPassword,
			basicPass,
			username,
			pathProjectID,
			strings.TrimSpace(c.Query("projectId")),
			subsonicDefaultCredential,
		}
		if matched, ok := matchSubsonicToken(token, salt, candidates...); ok {
			password = matched
		} else {
			c.XML(http.StatusOK, newSubsonicErrorResponse(40, "Authentication failed"))
			return nil, false
		}
	}

	if password == "" && hasBasicAuth {
		if decoded, ok := decodeSubsonicPassword(basicPass); ok {
			password = decoded
		} else {
			password = strings.TrimSpace(basicPass)
		}
	}

	if password == "" {
		password = subsonicDefaultCredential
	}
	if username == "" {
		username = subsonicDefaultCredential
	}

	if !strings.EqualFold(username, subsonicDefaultCredential) || !strings.EqualFold(password, subsonicDefaultCredential) {
		c.XML(http.StatusOK, newSubsonicErrorResponse(40, "Authentication failed"))
		return nil, false
	}

	actor := service.ActorContext{
		UserID:  0,
		IsAdmin: true,
	}

	project, err := h.service.GetProject(c.Request.Context(), actor, pathProjectID)
	if err != nil {
		c.XML(http.StatusOK, newSubsonicErrorResponse(40, "Authentication failed"))
		return nil, false
	}
	if project.Project == nil || !strings.EqualFold(project.Project.ID, pathProjectID) {
		c.XML(http.StatusOK, newSubsonicErrorResponse(70, "Project mismatch"))
		return nil, false
	}

	info := &subsonicAuthInfo{
		actor:   actor,
		project: project,
	}
	c.Set(subsonicAuthContextKey, info)
	return info, true
}

func (h *AudioHandlers) mustSubsonicAuth(c *gin.Context) *subsonicAuthInfo {
	if val, exists := c.Get(subsonicAuthContextKey); exists {
		if info, ok := val.(*subsonicAuthInfo); ok {
			return info
		}
	}
	return nil
}

func (h *AudioHandlers) subsonicActor(c *gin.Context) (service.ActorContext, *service.AudioProjectSummary, bool) {
	info := h.mustSubsonicAuth(c)
	if info == nil {
		c.XML(http.StatusOK, newSubsonicErrorResponse(40, "Authentication required"))
		return service.ActorContext{}, nil, false
	}
	return info.actor, info.project, true
}

func decodeSubsonicPassword(raw string) (string, bool) {
	if raw == "" {
		return "", false
	}
	value := strings.TrimSpace(raw)
	if strings.HasPrefix(value, "enc:") && len(value) > 4 {
		hexStr := value[4:]
		decoded, err := hex.DecodeString(hexStr)
		if err == nil {
			return string(decoded), true
		}
		return "", false
	}
	if value != "" {
		return value, true
	}
	return "", false
}

func matchSubsonicToken(token, salt string, candidates ...string) (string, bool) {
	token = strings.TrimSpace(token)
	salt = strings.TrimSpace(salt)
	if token == "" || salt == "" {
		return "", false
	}
	normalizedToken := strings.ToLower(token)
	seen := make(map[string]struct{})
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if _, exists := seen[candidate]; exists {
			continue
		}
		seen[candidate] = struct{}{}
		sum := md5.Sum([]byte(candidate + salt))
		if fmt.Sprintf("%x", sum[:]) == normalizedToken {
			return candidate, true
		}
	}
	return "", false
}

func (h *AudioHandlers) registerRoutes(router *gin.RouterGroup) {
	router.GET("/projects", h.listProjects)
	router.POST("/projects", h.createProject)
	router.GET("/projects/:projectId", h.getProject)
	router.PUT("/projects/:projectId", h.updateProject)
	router.DELETE("/projects/:projectId", h.deleteProject)

	router.POST("/projects/:projectId/episodes", h.uploadEpisode)
	router.POST("/projects/:projectId/episodes/url", h.submitEpisodeURL)
	router.GET("/projects/:projectId/episodes", h.listEpisodes)

	router.GET("/episodes/:episodeId", h.getEpisode)
	router.PUT("/episodes/:episodeId", h.updateEpisode)
	router.DELETE("/episodes/:episodeId", h.deleteEpisode)

	router.POST("/episodes/:episodeId/stream/token", h.issueStreamToken)
	router.GET("/episodes/:episodeId/stream", h.streamEpisode)

	router.GET("/projects/:projectId/playlists", h.listPlaylists)
	router.POST("/projects/:projectId/playlists", h.createPlaylist)
	router.PUT("/playlists/:playlistId", h.updatePlaylist)
	router.DELETE("/playlists/:playlistId", h.deletePlaylist)
	router.POST("/playlists/:playlistId/episodes", h.addPlaylistEpisodes)
	router.DELETE("/playlists/:playlistId/episodes/:episodeId", h.removePlaylistEpisode)
	router.PUT("/playlists/:playlistId/order", h.replacePlaylistOrder)

	router.GET("/search", h.searchEpisodes)
	router.GET("/projects/:projectId/jobs", h.listJobs)
}

func (h *AudioHandlers) listProjects(c *gin.Context) {
	actor, ok := h.actorFromContext(c)
	if !ok {
		return
	}
	page, limit := parsePageLimit(c)
	offset := (page - 1) * limit

	filter := repository.AudioProjectFilter{}
	if vis := c.Query("visibility"); vis != "" {
		filter.Visibility = splitAndClean(vis)
	}
	if name := c.Query("name"); name != "" {
		filter.NameLike = name
	}

	projects, total, err := h.service.ListProjects(c.Request.Context(), actor, filter, limit, offset)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "获取播客项目失败: "+err.Error())
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

func (h *AudioHandlers) createProject(c *gin.Context) {
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
	project, err := h.service.CreateProject(c.Request.Context(), actor, service.CreateAudioProjectInput{
		Name:       payload.Name,
		Visibility: payload.Visibility,
		Metadata:   payload.Metadata,
	})
	if err != nil {
		writeError(c, http.StatusInternalServerError, "创建播客项目失败: "+err.Error())
		return
	}
	c.JSON(http.StatusOK, models.CommonResponse{
		Code: 0,
		Msg:  "success",
		Data: project,
	})
}

func (h *AudioHandlers) getProject(c *gin.Context) {
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

func (h *AudioHandlers) updateProject(c *gin.Context) {
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
	project, err := h.service.UpdateProject(c.Request.Context(), actor, projectID, service.UpdateAudioProjectInput{
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

func (h *AudioHandlers) deleteProject(c *gin.Context) {
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

func (h *AudioHandlers) uploadEpisode(c *gin.Context) {
	actor, ok := h.actorFromContext(c)
	if !ok {
		return
	}
	projectID, err := parseUUIDParam(c, "projectId")
	if err != nil {
		writeError(c, http.StatusBadRequest, "项目ID无效: "+err.Error())
		return
	}
	fileHeader, err := c.FormFile("file")
	if err != nil {
		writeError(c, http.StatusBadRequest, "请提供音频文件")
		return
	}
	file, err := fileHeader.Open()
	if err != nil {
		writeError(c, http.StatusInternalServerError, "打开上传文件失败: "+err.Error())
		return
	}
	input := service.UploadEpisodeInput{
		ProjectID:   projectID,
		FileName:    fileHeader.Filename,
		Size:        fileHeader.Size,
		ContentType: fileHeader.Header.Get("Content-Type"),
		Reader:      file,
		UploaderID:  actor.UserID,
	}
	episode, err := h.service.UploadEpisode(c.Request.Context(), actor, input)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "上传音频失败: "+err.Error())
		return
	}
	c.JSON(http.StatusOK, models.CommonResponse{
		Code: 0,
		Msg:  "success",
		Data: episode,
	})
}

func (h *AudioHandlers) submitEpisodeURL(c *gin.Context) {
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
		URL   string `json:"url" binding:"required"`
		Title string `json:"title"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		writeError(c, http.StatusBadRequest, "参数错误: "+err.Error())
		return
	}
	episode, err := h.service.SubmitEpisodeURL(c.Request.Context(), actor, service.SubmitEpisodeURLInput{
		ProjectID:  projectID,
		URL:        payload.URL,
		Title:      payload.Title,
		UploaderID: actor.UserID,
	})
	if err != nil {
		writeError(c, http.StatusInternalServerError, "导入播客失败: "+err.Error())
		return
	}
	c.JSON(http.StatusOK, models.CommonResponse{
		Code: 0,
		Msg:  "success",
		Data: episode,
	})
}

func (h *AudioHandlers) listEpisodes(c *gin.Context) {
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

	filter := repository.AudioEpisodeFilter{
		ProjectID: projectID,
	}
	if status := c.Query("status"); status != "" {
		filter.ParseState = splitAndClean(status)
	}
	if source := c.Query("sourceType"); source != "" {
		filter.SourceType = splitAndClean(source)
	}
	if category := c.Query("category"); category != "" {
		filter.Categories = splitAndClean(category)
	}
	if query := c.Query("q"); query != "" {
		filter.Query = query
	}
	if order := c.Query("orderBy"); order != "" {
		filter.OrderBy = order
	}

	episodes, total, err := h.service.ListEpisodes(c.Request.Context(), actor, filter, limit, offset)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "查询节目信息失败: "+err.Error())
		return
	}

	c.JSON(http.StatusOK, models.CommonResponse{
		Code: 0,
		Msg:  "success",
		Data: models.PageResponse{
			List:       episodes,
			TotalCount: total,
			PageSize:   limit,
			CurrPage:   page,
			TotalPage:  calcTotalPages(total, limit),
		},
	})
}

func (h *AudioHandlers) getEpisode(c *gin.Context) {
	actor, ok := h.actorFromContext(c)
	if !ok {
		return
	}
	episodeID, err := parseUintParam(c, "episodeId")
	if err != nil {
		writeError(c, http.StatusBadRequest, "节目ID无效")
		return
	}
	episode, err := h.service.GetEpisode(c.Request.Context(), actor, episodeID)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "获取节目失败: "+err.Error())
		return
	}
	c.JSON(http.StatusOK, models.CommonResponse{
		Code: 0,
		Msg:  "success",
		Data: episode,
	})
}

func (h *AudioHandlers) updateEpisode(c *gin.Context) {
	actor, ok := h.actorFromContext(c)
	if !ok {
		return
	}
	episodeID, err := parseUintParam(c, "episodeId")
	if err != nil {
		writeError(c, http.StatusBadRequest, "节目ID无效")
		return
	}
	var payload struct {
		EpisodeTitle  *string        `json:"episodeTitle"`
		ShowTitle     *string        `json:"showTitle"`
		PrimaryHost   *string        `json:"primaryHost"`
		Category      *string        `json:"category"`
		PublishAt     *string        `json:"publishAt"`
		SeasonNumber  *int           `json:"seasonNumber"`
		EpisodeNumber *int           `json:"episodeNumber"`
		CoverURI      *string        `json:"coverUri"`
		TranscriptURI *string        `json:"transcriptUri"`
		ParseStatus   *string        `json:"parseStatus"`
		Metadata      map[string]any `json:"metadata"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		writeError(c, http.StatusBadRequest, "参数错误: "+err.Error())
		return
	}
	var publishAt *time.Time
	if payload.PublishAt != nil && strings.TrimSpace(*payload.PublishAt) != "" {
		if ts, err := time.Parse(time.RFC3339, *payload.PublishAt); err == nil {
			publishAt = &ts
		}
	}

	episode, err := h.service.UpdateEpisode(c.Request.Context(), actor, episodeID, service.UpdateEpisodeInput{
		EpisodeTitle:  payload.EpisodeTitle,
		ShowTitle:     payload.ShowTitle,
		PrimaryHost:   payload.PrimaryHost,
		Category:      payload.Category,
		PublishAt:     publishAt,
		SeasonNumber:  payload.SeasonNumber,
		EpisodeNumber: payload.EpisodeNumber,
		CoverURI:      payload.CoverURI,
		TranscriptURI: payload.TranscriptURI,
		ParseStatus:   payload.ParseStatus,
		Metadata:      payload.Metadata,
	})
	if err != nil {
		writeError(c, http.StatusInternalServerError, "更新节目失败: "+err.Error())
		return
	}

	c.JSON(http.StatusOK, models.CommonResponse{
		Code: 0,
		Msg:  "success",
		Data: episode,
	})
}

func (h *AudioHandlers) deleteEpisode(c *gin.Context) {
	actor, ok := h.actorFromContext(c)
	if !ok {
		return
	}
	episodeID, err := parseUintParam(c, "episodeId")
	if err != nil {
		writeError(c, http.StatusBadRequest, "节目ID无效")
		return
	}
	if err := h.service.DeleteEpisode(c.Request.Context(), actor, episodeID); err != nil {
		writeError(c, http.StatusInternalServerError, "删除节目失败: "+err.Error())
		return
	}
	c.JSON(http.StatusOK, models.CommonResponse{Code: 0, Msg: "success"})
}

func (h *AudioHandlers) issueStreamToken(c *gin.Context) {
	actor, ok := h.actorFromContext(c)
	if !ok {
		return
	}
	episodeID, err := parseUintParam(c, "episodeId")
	if err != nil {
		writeError(c, http.StatusBadRequest, "节目ID无效")
		return
	}
	token, err := h.service.GenerateStreamToken(c.Request.Context(), actor, episodeID)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "生成播放令牌失败: "+err.Error())
		return
	}
	c.JSON(http.StatusOK, models.CommonResponse{
		Code: 0,
		Msg:  "success",
		Data: map[string]string{"token": token},
	})
}

func (h *AudioHandlers) streamEpisode(c *gin.Context) {
	token := c.Query("token")
	if strings.TrimSpace(token) == "" {
		writeError(c, http.StatusUnauthorized, "缺少播放令牌")
		return
	}
	claims, err := h.service.VerifyStreamToken(token)
	if err != nil {
		writeError(c, http.StatusUnauthorized, "播放令牌无效: "+err.Error())
		return
	}
	episodeID, err := parseUintParam(c, "episodeId")
	if err != nil || claims.EpisodeID != episodeID {
		writeError(c, http.StatusUnauthorized, "播放令牌与节目不匹配")
		return
	}

	path, episode, err := h.service.ResolveEpisodePath(c.Request.Context(), episodeID)
	if err != nil {
		writeError(c, http.StatusNotFound, "资源不存在: "+err.Error())
		return
	}
	file, err := os.Open(path)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "读取音频失败: "+err.Error())
		return
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		writeError(c, http.StatusInternalServerError, "读取文件信息失败: "+err.Error())
		return
	}
	contentType := mime.TypeByExtension(filepath.Ext(path))
	if contentType == "" {
		contentType = "audio/mpeg"
	}
	c.Header("Content-Type", contentType)
	c.Header("Accept-Ranges", "bytes")
	http.ServeContent(c.Writer, c.Request, filepath.Base(path), stat.ModTime(), file)
	logger.Infof("audio_stream user=%d episode=%d duration=%.2f bitrate=%d", claims.UserID, episode.ID, episode.Duration, episode.Bitrate)
}

func (h *AudioHandlers) listPlaylists(c *gin.Context) {
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

	filter := repository.AudioPlaylistFilter{
		ProjectID: projectID,
	}
	if name := c.Query("name"); name != "" {
		filter.NameLike = name
	}

	playlists, total, err := h.service.ListPlaylists(c.Request.Context(), actor, filter, limit, offset)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "获取播放列表失败: "+err.Error())
		return
	}

	c.JSON(http.StatusOK, models.CommonResponse{
		Code: 0,
		Msg:  "success",
		Data: models.PageResponse{
			List:       playlists,
			TotalCount: total,
			PageSize:   limit,
			CurrPage:   page,
			TotalPage:  calcTotalPages(total, limit),
		},
	})
}

func (h *AudioHandlers) createPlaylist(c *gin.Context) {
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
		Name        string         `json:"name" binding:"required"`
		Description *string        `json:"description"`
		CoverURI    *string        `json:"coverUri"`
		Metadata    map[string]any `json:"metadata"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		writeError(c, http.StatusBadRequest, "参数错误: "+err.Error())
		return
	}
	playlist, err := h.service.CreatePlaylist(c.Request.Context(), actor, projectID, service.PlaylistMutationInput{
		Name:        &payload.Name,
		Description: payload.Description,
		CoverURI:    payload.CoverURI,
		Metadata:    payload.Metadata,
	})
	if err != nil {
		writeError(c, http.StatusInternalServerError, "创建播放列表失败: "+err.Error())
		return
	}

	c.JSON(http.StatusOK, models.CommonResponse{
		Code: 0,
		Msg:  "success",
		Data: playlist,
	})
}

func (h *AudioHandlers) updatePlaylist(c *gin.Context) {
	actor, ok := h.actorFromContext(c)
	if !ok {
		return
	}
	playlistID, err := parseUintParam(c, "playlistId")
	if err != nil {
		writeError(c, http.StatusBadRequest, "播放列表ID无效")
		return
	}
	var payload struct {
		Name        *string        `json:"name"`
		Description *string        `json:"description"`
		CoverURI    *string        `json:"coverUri"`
		Metadata    map[string]any `json:"metadata"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		writeError(c, http.StatusBadRequest, "参数错误: "+err.Error())
		return
	}

	playlist, err := h.service.UpdatePlaylist(c.Request.Context(), actor, playlistID, service.PlaylistMutationInput{
		Name:        payload.Name,
		Description: payload.Description,
		CoverURI:    payload.CoverURI,
		Metadata:    payload.Metadata,
	})
	if err != nil {
		writeError(c, http.StatusInternalServerError, "更新播放列表失败: "+err.Error())
		return
	}

	c.JSON(http.StatusOK, models.CommonResponse{
		Code: 0,
		Msg:  "success",
		Data: playlist,
	})
}

func (h *AudioHandlers) deletePlaylist(c *gin.Context) {
	actor, ok := h.actorFromContext(c)
	if !ok {
		return
	}
	playlistID, err := parseUintParam(c, "playlistId")
	if err != nil {
		writeError(c, http.StatusBadRequest, "播放列表ID无效")
		return
	}
	if err := h.service.DeletePlaylist(c.Request.Context(), actor, playlistID); err != nil {
		writeError(c, http.StatusInternalServerError, "删除播放列表失败: "+err.Error())
		return
	}
	c.JSON(http.StatusOK, models.CommonResponse{Code: 0, Msg: "success"})
}

func (h *AudioHandlers) addPlaylistEpisodes(c *gin.Context) {
	actor, ok := h.actorFromContext(c)
	if !ok {
		return
	}
	playlistID, err := parseUintParam(c, "playlistId")
	if err != nil {
		writeError(c, http.StatusBadRequest, "播放列表ID无效")
		return
	}
	var payload struct {
		EpisodeIDs []uint64 `json:"episodeIds"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		writeError(c, http.StatusBadRequest, "参数错误: "+err.Error())
		return
	}
	if len(payload.EpisodeIDs) == 0 {
		writeError(c, http.StatusBadRequest, "请提供要添加的节目ID列表")
		return
	}
	if err := h.service.AddEpisodesToPlaylist(c.Request.Context(), actor, playlistID, payload.EpisodeIDs); err != nil {
		writeError(c, http.StatusInternalServerError, "添加节目失败: "+err.Error())
		return
	}
	c.JSON(http.StatusOK, models.CommonResponse{Code: 0, Msg: "success"})
}

func (h *AudioHandlers) removePlaylistEpisode(c *gin.Context) {
	actor, ok := h.actorFromContext(c)
	if !ok {
		return
	}
	playlistID, err := parseUintParam(c, "playlistId")
	if err != nil {
		writeError(c, http.StatusBadRequest, "播放列表ID无效")
		return
	}
	episodeID, err := parseUintParam(c, "episodeId")
	if err != nil {
		writeError(c, http.StatusBadRequest, "节目ID无效")
		return
	}
	if err := h.service.RemoveEpisodeFromPlaylist(c.Request.Context(), actor, playlistID, episodeID); err != nil {
		writeError(c, http.StatusInternalServerError, "移除节目失败: "+err.Error())
		return
	}
	c.JSON(http.StatusOK, models.CommonResponse{Code: 0, Msg: "success"})
}

func (h *AudioHandlers) replacePlaylistOrder(c *gin.Context) {
	actor, ok := h.actorFromContext(c)
	if !ok {
		return
	}
	playlistID, err := parseUintParam(c, "playlistId")
	if err != nil {
		writeError(c, http.StatusBadRequest, "播放列表ID无效")
		return
	}
	var payload struct {
		Episodes []service.PlaylistEpisodeOrder `json:"episodes"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		writeError(c, http.StatusBadRequest, "参数错误: "+err.Error())
		return
	}
	if err := h.service.ReplacePlaylistEpisodes(c.Request.Context(), actor, playlistID, payload.Episodes); err != nil {
		writeError(c, http.StatusInternalServerError, "更新排序失败: "+err.Error())
		return
	}
	c.JSON(http.StatusOK, models.CommonResponse{Code: 0, Msg: "success"})
}

func (h *AudioHandlers) searchEpisodes(c *gin.Context) {
	actor, ok := h.actorFromContext(c)
	if !ok {
		return
	}
	projectIDStr := strings.TrimSpace(c.Query("projectId"))
	if projectIDStr == "" {
		writeError(c, http.StatusBadRequest, "projectId 参数必填")
		return
	}
	if _, err := uuid.Parse(projectIDStr); err != nil {
		writeError(c, http.StatusBadRequest, "项目ID无效: "+err.Error())
		return
	}
	filter := repository.AudioEpisodeFilter{
		ProjectID: projectIDStr,
		Query:     c.Query("q"),
		OrderBy:   "create_time DESC",
	}
	results, total, err := h.service.ListEpisodes(c.Request.Context(), actor, filter, 20, 0)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "搜索节目失败: "+err.Error())
		return
	}
	c.JSON(http.StatusOK, models.CommonResponse{
		Code: 0,
		Msg:  "success",
		Data: models.PageResponse{
			List:       results,
			TotalCount: total,
			PageSize:   20,
			CurrPage:   1,
			TotalPage:  calcTotalPages(total, 20),
		},
	})
}

func (h *AudioHandlers) listJobs(c *gin.Context) {
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
	filter := repository.AudioJobFilter{
		ProjectID: projectID,
	}
	if jobType := c.Query("type"); jobType != "" {
		filter.JobTypes = splitAndClean(jobType)
	}
	if status := c.Query("status"); status != "" {
		filter.Status = splitAndClean(status)
	}
	jobs, total, err := h.service.ListJobs(c.Request.Context(), actor, filter, limit, offset)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "查询任务失败: "+err.Error())
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

func (h *AudioHandlers) actorFromContext(c *gin.Context) (service.ActorContext, bool) {
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

// --- Subsonic compatibility ---

func (h *AudioHandlers) RegisterSubsonicRoutes(router *gin.RouterGroup) {
	router.Use(h.subsonicAuthMiddleware())

	register := func(path string, handler gin.HandlerFunc) {
		router.GET(path, handler)
		if strings.HasSuffix(path, ".view") {
			alias := strings.TrimSuffix(path, ".view")
			router.GET(alias, handler)
		}
	}

	// System
	register("/ping.view", h.subsonicPing)
	register("/getLicense.view", h.subsonicGetLicense)

	// Browsing
	register("/getMusicFolders.view", h.subsonicListFolders)
	register("/getMusicDirectory.view", h.subsonicGetDirectory)
	register("/getIndexes.view", h.subsonicGetIndexes)
	register("/getArtists.view", h.subsonicGetArtists)
	register("/getArtist.view", h.subsonicGetArtist)
	register("/getAlbum.view", h.subsonicGetAlbum)
	register("/getAlbumList.view", h.subsonicGetAlbumList)
	register("/getAlbumList2.view", h.subsonicGetAlbumList2)
	register("/getSong.view", h.subsonicGetSong)

	// Search
	register("/search2.view", h.subsonicSearch2)
	register("/search3.view", h.subsonicSearch3)

	// Media retrieval
	register("/stream.view", h.subsonicStream)
	register("/download.view", h.subsonicDownload)
	register("/getCoverArt.view", h.subsonicGetCoverArt)

	// Playlists
	register("/getPlaylists.view", h.subsonicGetPlaylists)
	register("/getPlaylist.view", h.subsonicGetPlaylist)
	register("/createPlaylist.view", h.subsonicCreatePlaylist)
	register("/updatePlaylist.view", h.subsonicUpdatePlaylist)
	register("/deletePlaylist.view", h.subsonicDeletePlaylist)

	// Favorites
	register("/star.view", h.subsonicStar)
	register("/unstar.view", h.subsonicUnstar)
	register("/getStarred.view", h.subsonicGetStarred)
	register("/getStarred2.view", h.subsonicGetStarred2)

	// Playback tracking
	register("/scrobble.view", h.subsonicScrobble)
	register("/getNowPlaying.view", h.subsonicGetNowPlaying)

	// Rating
	register("/setRating.view", h.subsonicSetRating)

	// Genres
	register("/getGenres.view", h.subsonicGetGenres)
	register("/getSongsByGenre.view", h.subsonicGetSongsByGenre)

	// Random/discovery
	register("/getRandomSongs.view", h.subsonicGetRandomSongs)
	register("/getSimilarSongs.view", h.subsonicGetSimilarSongs)
	register("/getSimilarSongs2.view", h.subsonicGetSimilarSongs2)
	register("/getTopSongs.view", h.subsonicGetTopSongs)
}

// Subsonic response structures
type subsonicResponse struct {
	XMLName       xml.Name                `xml:"subsonic-response"`
	Status        string                  `xml:"status,attr"`
	Version       string                  `xml:"version,attr"`
	XMLNS         string                  `xml:"xmlns,attr"`
	MusicFolders  *subsonicMusicFolders   `xml:"musicFolders,omitempty"`
	Directory     *subsonicDirectory      `xml:"directory,omitempty"`
	License       *subsonicLicense        `xml:"license,omitempty"`
	Indexes       *subsonicIndexes        `xml:"indexes,omitempty"`
	Artists       *subsonicArtists        `xml:"artists,omitempty"`
	Artist        *subsonicArtistDetail   `xml:"artist,omitempty"`
	Album         *subsonicAlbumDetail    `xml:"album,omitempty"`
	Song          *subsonicSong           `xml:"song,omitempty"`
	SearchResult2 *subsonicSearchResult2  `xml:"searchResult2,omitempty"`
	SearchResult3 *subsonicSearchResult3  `xml:"searchResult3,omitempty"`
	Playlists     *subsonicPlaylists      `xml:"playlists,omitempty"`
	Playlist      *subsonicPlaylistDetail `xml:"playlist,omitempty"`
	Starred       *subsonicStarred        `xml:"starred,omitempty"`
	Starred2      *subsonicStarred2       `xml:"starred2,omitempty"`
	NowPlaying    *subsonicNowPlaying     `xml:"nowPlaying,omitempty"`
	Genres        *subsonicGenres         `xml:"genres,omitempty"`
	SimilarSongs  *subsonicSimilarSongs   `xml:"similarSongs,omitempty"`
	SimilarSongs2 *subsonicSimilarSongs2  `xml:"similarSongs2,omitempty"`
	TopSongs      *subsonicTopSongs       `xml:"topSongs,omitempty"`
	AlbumList     *subsonicAlbumList      `xml:"albumList,omitempty"`
	AlbumList2    *subsonicAlbumList2     `xml:"albumList2,omitempty"`
	RandomSongs   *subsonicSongs          `xml:"randomSongs,omitempty"`
	SongsByGenre  *subsonicSongs          `xml:"songsByGenre,omitempty"`
	Error         *subsonicError          `xml:"error,omitempty"`
}

type subsonicError struct {
	Code    int    `xml:"code,attr"`
	Message string `xml:"message,attr"`
}

type subsonicLicense struct {
	Valid bool   `xml:"valid,attr"`
	Email string `xml:"email,attr,omitempty"`
	Key   string `xml:"licenseExpires,attr,omitempty"`
}

type subsonicMusicFolders struct {
	Folder []subsonicFolder `xml:"musicFolder"`
}

type subsonicFolder struct {
	ID   string `xml:"id,attr"`
	Name string `xml:"name,attr"`
}

type subsonicDirectory struct {
	ID    string          `xml:"id,attr"`
	Name  string          `xml:"name,attr"`
	Child []subsonicChild `xml:"child"`
}

type subsonicChild struct {
	ID          string `xml:"id,attr"`
	Parent      string `xml:"parent,attr,omitempty"`
	IsDir       bool   `xml:"isDir,attr"`
	Title       string `xml:"title,attr"`
	Album       string `xml:"album,attr,omitempty"`
	Artist      string `xml:"artist,attr,omitempty"`
	Track       int    `xml:"track,attr,omitempty"`
	Year        int    `xml:"year,attr,omitempty"`
	Genre       string `xml:"genre,attr,omitempty"`
	CoverArt    string `xml:"coverArt,attr,omitempty"`
	Size        int64  `xml:"size,attr,omitempty"`
	ContentType string `xml:"contentType,attr,omitempty"`
	Suffix      string `xml:"suffix,attr,omitempty"`
	Duration    int    `xml:"duration,attr,omitempty"`
	BitRate     int    `xml:"bitRate,attr,omitempty"`
	Path        string `xml:"path,attr,omitempty"`
	IsVideo     bool   `xml:"isVideo,attr,omitempty"`
	Created     string `xml:"created,attr,omitempty"`
	Type        string `xml:"type,attr,omitempty"`
}

type subsonicIndexes struct {
	LastModified int64            `xml:"lastModified,attr"`
	Shortcut     []subsonicArtist `xml:"shortcut,omitempty"`
	Index        []subsonicIndex  `xml:"index"`
}

type subsonicIndex struct {
	Name   string           `xml:"name,attr"`
	Artist []subsonicArtist `xml:"artist"`
}

type subsonicArtist struct {
	ID         string `xml:"id,attr"`
	Name       string `xml:"name,attr"`
	AlbumCount int    `xml:"albumCount,attr,omitempty"`
	CoverArt   string `xml:"coverArt,attr,omitempty"`
}

type subsonicArtists struct {
	IgnoredArticles string          `xml:"ignoredArticles,attr,omitempty"`
	Index           []subsonicIndex `xml:"index"`
}

type subsonicArtistDetail struct {
	ID         string          `xml:"id,attr"`
	Name       string          `xml:"name,attr"`
	AlbumCount int             `xml:"albumCount,attr,omitempty"`
	CoverArt   string          `xml:"coverArt,attr,omitempty"`
	Album      []subsonicAlbum `xml:"album"`
}

type subsonicAlbum struct {
	ID        string `xml:"id,attr"`
	Name      string `xml:"name,attr"`
	Artist    string `xml:"artist,attr,omitempty"`
	ArtistID  string `xml:"artistId,attr,omitempty"`
	CoverArt  string `xml:"coverArt,attr,omitempty"`
	SongCount int    `xml:"songCount,attr,omitempty"`
	Duration  int    `xml:"duration,attr,omitempty"`
	Created   string `xml:"created,attr,omitempty"`
	Year      int    `xml:"year,attr,omitempty"`
	Genre     string `xml:"genre,attr,omitempty"`
}

type subsonicAlbumDetail struct {
	ID        string         `xml:"id,attr"`
	Name      string         `xml:"name,attr"`
	Artist    string         `xml:"artist,attr,omitempty"`
	ArtistID  string         `xml:"artistId,attr,omitempty"`
	CoverArt  string         `xml:"coverArt,attr,omitempty"`
	SongCount int            `xml:"songCount,attr,omitempty"`
	Duration  int            `xml:"duration,attr,omitempty"`
	Created   string         `xml:"created,attr,omitempty"`
	Year      int            `xml:"year,attr,omitempty"`
	Genre     string         `xml:"genre,attr,omitempty"`
	Song      []subsonicSong `xml:"song"`
}

type subsonicSong struct {
	ID          string `xml:"id,attr"`
	Parent      string `xml:"parent,attr,omitempty"`
	Title       string `xml:"title,attr"`
	Album       string `xml:"album,attr,omitempty"`
	Artist      string `xml:"artist,attr,omitempty"`
	Track       int    `xml:"track,attr,omitempty"`
	Year        int    `xml:"year,attr,omitempty"`
	Genre       string `xml:"genre,attr,omitempty"`
	CoverArt    string `xml:"coverArt,attr,omitempty"`
	Size        int64  `xml:"size,attr,omitempty"`
	ContentType string `xml:"contentType,attr,omitempty"`
	Suffix      string `xml:"suffix,attr,omitempty"`
	Duration    int    `xml:"duration,attr,omitempty"`
	BitRate     int    `xml:"bitRate,attr,omitempty"`
	Path        string `xml:"path,attr,omitempty"`
	Created     string `xml:"created,attr,omitempty"`
	AlbumID     string `xml:"albumId,attr,omitempty"`
	ArtistID    string `xml:"artistId,attr,omitempty"`
	Type        string `xml:"type,attr,omitempty"`
}

type subsonicSearchResult2 struct {
	Artist []subsonicArtist `xml:"artist"`
	Album  []subsonicAlbum  `xml:"album"`
	Song   []subsonicSong   `xml:"song"`
}

type subsonicSearchResult3 struct {
	Artist []subsonicArtist `xml:"artist"`
	Album  []subsonicAlbum  `xml:"album"`
	Song   []subsonicSong   `xml:"song"`
}

type subsonicPlaylists struct {
	Playlist []subsonicPlaylist `xml:"playlist"`
}

type subsonicPlaylist struct {
	ID        string `xml:"id,attr"`
	Name      string `xml:"name,attr"`
	SongCount int    `xml:"songCount,attr,omitempty"`
	Duration  int    `xml:"duration,attr,omitempty"`
	Created   string `xml:"created,attr,omitempty"`
	Changed   string `xml:"changed,attr,omitempty"`
	CoverArt  string `xml:"coverArt,attr,omitempty"`
	Owner     string `xml:"owner,attr,omitempty"`
	Public    bool   `xml:"public,attr,omitempty"`
}

type subsonicPlaylistDetail struct {
	ID        string         `xml:"id,attr"`
	Name      string         `xml:"name,attr"`
	SongCount int            `xml:"songCount,attr,omitempty"`
	Duration  int            `xml:"duration,attr,omitempty"`
	Created   string         `xml:"created,attr,omitempty"`
	Changed   string         `xml:"changed,attr,omitempty"`
	CoverArt  string         `xml:"coverArt,attr,omitempty"`
	Owner     string         `xml:"owner,attr,omitempty"`
	Public    bool           `xml:"public,attr,omitempty"`
	Entry     []subsonicSong `xml:"entry"`
}

type subsonicStarred struct {
	Artist []subsonicArtist `xml:"artist"`
	Album  []subsonicAlbum  `xml:"album"`
	Song   []subsonicSong   `xml:"song"`
}

type subsonicStarred2 struct {
	Artist []subsonicArtist `xml:"artist"`
	Album  []subsonicAlbum  `xml:"album"`
	Song   []subsonicSong   `xml:"song"`
}

type subsonicNowPlaying struct {
	Entry []subsonicNowPlayingEntry `xml:"entry"`
}

type subsonicNowPlayingEntry struct {
	subsonicSong
	Username   string `xml:"username,attr"`
	MinutesAgo int    `xml:"minutesAgo,attr"`
	PlayerID   int    `xml:"playerId,attr"`
	PlayerName string `xml:"playerName,attr,omitempty"`
}

type subsonicGenres struct {
	Genre []subsonicGenre `xml:"genre"`
}

type subsonicGenre struct {
	SongCount  int    `xml:"songCount,attr"`
	AlbumCount int    `xml:"albumCount,attr"`
	Value      string `xml:",chardata"`
}

type subsonicSimilarSongs struct {
	Song []subsonicSong `xml:"song"`
}

type subsonicSimilarSongs2 struct {
	Song []subsonicSong `xml:"song"`
}

type subsonicTopSongs struct {
	Song []subsonicSong `xml:"song"`
}

type subsonicAlbumList struct {
	Album []subsonicAlbum `xml:"album"`
}

type subsonicAlbumList2 struct {
	Album []subsonicAlbum `xml:"album"`
}

type subsonicSongs struct {
	Song []subsonicSong `xml:"song"`
}

// Helper functions for Subsonic API
const subsonicVersion = "1.16.1"
const subsonicXMLNS = "http://subsonic.org/restapi"

func newSubsonicResponse() subsonicResponse {
	return subsonicResponse{
		Status:  "ok",
		Version: subsonicVersion,
		XMLNS:   subsonicXMLNS,
	}
}

func newSubsonicErrorResponse(code int, message string) subsonicResponse {
	return subsonicResponse{
		Status:  "failed",
		Version: subsonicVersion,
		XMLNS:   subsonicXMLNS,
		Error: &subsonicError{
			Code:    code,
			Message: message,
		},
	}
}

func episodeToSubsonicSong(ep *models.AudioEpisode) subsonicSong {
	songID := fmt.Sprintf("ep-%d", ep.ID)
	albumID := fmt.Sprintf("proj-%s", ep.ProjectID)
	artistID := fmt.Sprintf("host-%s", url.QueryEscape(ep.PrimaryHost))

	suffix, contentType := resolveEpisodeMediaFormat(ep)

	year := 0
	if ep.PublishAt != nil {
		year = ep.PublishAt.Year()
	}

	return subsonicSong{
		ID:          songID,
		Parent:      albumID,
		Title:       ep.EpisodeTitle,
		Album:       ep.ShowTitle,
		Artist:      ep.PrimaryHost,
		Track:       ep.EpisodeNumber,
		Year:        year,
		Genre:       ep.Category,
		CoverArt:    ep.CoverURI,
		Size:        ep.FileSize,
		ContentType: contentType,
		Suffix:      suffix,
		Duration:    int(ep.Duration),
		BitRate:     ep.Bitrate,
		Path:        ep.FileURI,
		Created:     ep.CreateTime.Format(time.RFC3339),
		AlbumID:     albumID,
		ArtistID:    artistID,
		Type:        "podcast",
	}
}

func resolveEpisodeMediaFormat(ep *models.AudioEpisode) (string, string) {
	suffix := strings.TrimPrefix(strings.ToLower(filepath.Ext(ep.FileURI)), ".")
	if suffix == "" {
		suffix = "mp3"
	}

	if len(ep.Metadata) > 0 {
		var meta map[string]any
		if err := json.Unmarshal(ep.Metadata, &meta); err == nil {
			if raw, ok := meta["transcodedUri"]; ok {
				if uri, ok := raw.(string); ok && strings.TrimSpace(uri) != "" {
					if ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(uri)), "."); ext != "" {
						suffix = ext
					}
				}
			}
		}
	}

	contentType := service.MediaContentType(suffix)
	return suffix, contentType
}

func projectToSubsonicAlbum(proj *service.AudioProjectSummary) subsonicAlbum {
	albumID := fmt.Sprintf("proj-%s", proj.Project.ID)
	return subsonicAlbum{
		ID:        albumID,
		Name:      proj.Project.Name,
		SongCount: int(proj.EpisodeCount),
		Duration:  0, // TODO: Calculate total duration
		Created:   proj.Project.CreateTime.Format(time.RFC3339),
		Genre:     "Podcast",
	}
}

func (h *AudioHandlers) subsonicPing(c *gin.Context) {
	resp := newSubsonicResponse()
	c.XML(http.StatusOK, resp)
}

func (h *AudioHandlers) subsonicGetLicense(c *gin.Context) {
	resp := newSubsonicResponse()
	resp.License = &subsonicLicense{
		Valid: true,
		Email: "xiaozhi@example.com",
	}
	c.XML(http.StatusOK, resp)
}

func (h *AudioHandlers) subsonicListFolders(c *gin.Context) {
	_, project, ok := h.subsonicActor(c)
	if !ok {
		return
	}
	folders := []subsonicFolder{{
		ID:   project.Project.ID,
		Name: project.Project.Name,
	}}
	resp := newSubsonicResponse()
	resp.MusicFolders = &subsonicMusicFolders{
		Folder: folders,
	}
	c.XML(http.StatusOK, resp)
}

func (h *AudioHandlers) subsonicGetDirectory(c *gin.Context) {
	actor, project, ok := h.subsonicActor(c)
	if !ok {
		return
	}
	projectID := strings.TrimSpace(c.Query("id"))
	if projectID == "" {
		c.XML(http.StatusOK, newSubsonicErrorResponse(10, "Invalid directory ID"))
		return
	}
	if _, err := uuid.Parse(projectID); err != nil {
		c.XML(http.StatusOK, newSubsonicErrorResponse(10, "Invalid directory ID"))
		return
	}
	if projectID != project.Project.ID {
		c.XML(http.StatusOK, newSubsonicErrorResponse(70, "Artist not found"))
		return
	}
	filter := repository.AudioEpisodeFilter{
		ProjectID: projectID,
		OrderBy:   "create_time DESC",
	}
	projectName := project.Project.Name
	episodes, _, err := h.service.ListEpisodes(c.Request.Context(), actor, filter, 500, 0)
	if err != nil {
		c.XML(http.StatusOK, newSubsonicErrorResponse(0, "Failed to get episodes"))
		return
	}
	dir := subsonicDirectory{
		ID:   projectID,
		Name: projectName,
	}
	for _, ep := range episodes {
		song := episodeToSubsonicSong(ep)
		dir.Child = append(dir.Child, subsonicChild{
			ID:          song.ID,
			Parent:      song.Parent,
			IsDir:       false,
			Title:       song.Title,
			Album:       song.Album,
			Artist:      song.Artist,
			Track:       song.Track,
			Year:        song.Year,
			Genre:       song.Genre,
			CoverArt:    song.CoverArt,
			Size:        song.Size,
			ContentType: song.ContentType,
			Suffix:      song.Suffix,
			Duration:    song.Duration,
			BitRate:     song.BitRate,
			Path:        song.Path,
			Created:     song.Created,
			Type:        song.Type,
		})
	}
	resp := newSubsonicResponse()
	resp.Directory = &dir
	c.XML(http.StatusOK, resp)
}

func (h *AudioHandlers) subsonicStream(c *gin.Context) {
	h.serveSubsonicStream(c, false)
}

func (h *AudioHandlers) serveSubsonicStream(c *gin.Context, forceDownload bool) {
	idStr := strings.TrimSpace(c.Query("id"))
	if strings.HasPrefix(idStr, "ep-") {
		idStr = strings.TrimPrefix(idStr, "ep-")
	}
	episodeID, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		c.XML(http.StatusOK, newSubsonicErrorResponse(70, "Invalid song ID"))
		return
	}

	token := c.Query("token")
	if token != "" {
		claims, err := h.service.VerifyStreamToken(token)
		if err != nil || claims.EpisodeID != episodeID {
			c.XML(http.StatusOK, newSubsonicErrorResponse(40, "Playback token invalid"))
			return
		}
		path, _, err := h.service.ResolveEpisodePath(c.Request.Context(), uint64(episodeID))
		if err != nil {
			c.XML(http.StatusOK, newSubsonicErrorResponse(0, "Resource unavailable"))
			return
		}
		if forceDownload {
			name := fmt.Sprintf("episode_%d%s", episodeID, filepath.Ext(path))
			encoded := url.PathEscape(name)
			c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"; filename*=UTF-8''%s", name, encoded))
		}
		http.ServeFile(c.Writer, c.Request, path)
		return
	}

	actor, project, ok := h.subsonicActor(c)
	if !ok {
		return
	}

	episode, err := h.service.GetEpisode(c.Request.Context(), actor, episodeID)
	if err != nil || episode.ProjectID != project.Project.ID {
		c.XML(http.StatusOK, newSubsonicErrorResponse(70, "Song not found"))
		return
	}

	maxBitRate := 0
	if value := strings.TrimSpace(c.Query("maxBitRate")); value != "" {
		if v, err := strconv.Atoi(value); err == nil && v > 0 {
			maxBitRate = v
		}
	}
	stream, err := h.service.PrepareEpisodeStream(c.Request.Context(), (*models.AudioEpisode)(episode), service.StreamOptions{
		Format:         strings.TrimSpace(c.Query("format")),
		MaxBitRateKbps: maxBitRate,
	})
	if err != nil {
		logger.Warnf("subsonic stream failed episode=%d format=%s: %v", episodeID, c.Query("format"), err)
		c.XML(http.StatusOK, newSubsonicErrorResponse(0, "Stream unavailable"))
		return
	}
	if stream.Cleanup != nil {
		defer stream.Cleanup()
	}

	if stream.ContentType != "" {
		c.Header("Content-Type", stream.ContentType)
	}

	disposition := "inline"
	if forceDownload {
		disposition = "attachment"
	}
	if stream.FileName != "" {
		filename := url.PathEscape(stream.FileName)
		c.Header("Content-Disposition", fmt.Sprintf("%s; filename=\"%s\"; filename*=UTF-8''%s", disposition, stream.FileName, filename))
	} else {
		c.Header("Content-Disposition", disposition)
	}

	http.ServeFile(c.Writer, c.Request, stream.Path)
}
