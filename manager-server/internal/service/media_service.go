package service

import (
	"context"
	"fmt"
	"io"
	"mime"
	"path/filepath"
	"strings"
	"time"

	"gorm.io/datatypes"

	"manager-server/internal/config"
	"manager-server/internal/models"
	"manager-server/internal/repository"
	"manager-server/internal/storage"
)

// MediaService orchestrates media asset management.
type MediaService struct {
	repo        repository.MediaRepository
	deviceRepo  repository.DeviceRepository
	storage     storage.MediaStorage
	maxFileSize int64
	allowedExt  map[string]struct{}
}

// UploadMediaInput captures metadata required to persist a media asset.
type UploadMediaInput struct {
	AgentID      string
	DeviceID     string
	OriginalName string
	ContentType  string
	Size         int64
	Reader       io.Reader
	Source       string
	Description  string
	RelatedInfo  map[string]any
}

// ListMediaInput specifies filters for querying media assets.
type ListMediaInput struct {
	AgentID   string
	DeviceID  string
	MediaType string
	Query     string
	Limit     int
	Offset    int
}

// NewMediaService constructs a MediaService instance.
func NewMediaService(repo repository.MediaRepository, deviceRepo repository.DeviceRepository, storage storage.MediaStorage, cfg config.MediaConfig) *MediaService {
	ms := &MediaService{
		repo:       repo,
		deviceRepo: deviceRepo,
		storage:    storage,
		allowedExt: map[string]struct{}{},
	}

	if cfg.Storage.MaxFileSizeMB > 0 {
		ms.maxFileSize = int64(cfg.Storage.MaxFileSizeMB) * 1024 * 1024
	}

	for _, ext := range cfg.Storage.AllowedFormats {
		if cleaned := strings.ToLower(strings.TrimSpace(ext)); cleaned != "" {
			ms.allowedExt[ensureDotPrefix(cleaned)] = struct{}{}
		}
	}

	// Default allowed formats if none configured.
	if len(ms.allowedExt) == 0 {
		for _, ext := range []string{".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp", ".mp4", ".mov", ".mkv", ".avi", ".webm", ".wav", ".mp3", ".aac", ".m4a", ".ogg", ".flac"} {
			ms.allowedExt[ext] = struct{}{}
		}
	}

	return ms
}

// UploadMedia persists a new media asset and returns its metadata.
func (s *MediaService) UploadMedia(ctx context.Context, input UploadMediaInput) (*models.MediaAsset, error) {
	if s.storage == nil {
		return nil, fmt.Errorf("media storage is not configured")
	}
	if input.Reader == nil {
		return nil, fmt.Errorf("file reader is required")
	}
	if input.Size <= 0 {
		return nil, fmt.Errorf("文件内容为空")
	}
	if s.maxFileSize > 0 && input.Size > s.maxFileSize {
		return nil, fmt.Errorf("文件大小超出限制，最大允许 %d MB", s.maxFileSize/1024/1024)
	}

	var (
		agentID  = strings.TrimSpace(input.AgentID)
		deviceID *string
	)

	if strings.TrimSpace(input.DeviceID) != "" {
		id := strings.TrimSpace(input.DeviceID)
		device, err := s.deviceRepo.FindByID(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("查询设备失败: %w", err)
		}
		if device == nil {
			return nil, fmt.Errorf("设备不存在: %s", id)
		}
		deviceID = &id

		if device.AgentID == "" {
			return nil, fmt.Errorf("设备未绑定智能体，无法关联媒体")
		}
		if agentID != "" && agentID != device.AgentID {
			return nil, fmt.Errorf("指定的智能体ID与设备绑定的智能体不一致")
		}
		agentID = device.AgentID
	}

	if agentID == "" {
		return nil, fmt.Errorf("必须指定智能体或设备")
	}

	ext := strings.ToLower(strings.TrimSpace(filepath.Ext(input.OriginalName)))
	if ext == "" && input.ContentType != "" {
		if exts, _ := mime.ExtensionsByType(input.ContentType); len(exts) > 0 {
			ext = strings.ToLower(exts[0])
		}
	}
	if ext == "" {
		return nil, fmt.Errorf("无法识别文件类型")
	}
	ext = ensureDotPrefix(ext)

	if len(s.allowedExt) > 0 {
		if _, ok := s.allowedExt[ext]; !ok {
			return nil, fmt.Errorf("不支持的文件格式: %s", ext)
		}
	}

	mediaType, err := classifyMediaType(ext)
	if err != nil {
		return nil, err
	}

	displayName := strings.TrimSpace(filepath.Base(input.OriginalName))
	if displayName == "" {
		displayName = fmt.Sprintf("media_%d%s", time.Now().UnixMilli(), ext)
	}

	saveResult, err := s.storage.Save(ctx, agentID, deviceID, &storage.DocumentFile{
		Name:        displayName,
		Size:        input.Size,
		ContentType: input.ContentType,
		Reader:      input.Reader,
	})
	if err != nil {
		return nil, fmt.Errorf("保存媒体文件失败: %w", err)
	}

	asset := &models.MediaAsset{
		AgentID:      agentID,
		DeviceID:     deviceID,
		FileName:     displayName,
		OriginalName: input.OriginalName,
		MediaType:    mediaType,
		ContentType:  input.ContentType,
		FileSize:     saveResult.Size,
		StorageURI:   saveResult.URI,
		Source:       strings.TrimSpace(input.Source),
		Description:  strings.TrimSpace(input.Description),
	}

	if len(input.RelatedInfo) > 0 {
		related := make(datatypes.JSONMap, len(input.RelatedInfo))
		for k, v := range input.RelatedInfo {
			related[k] = v
		}
		asset.RelatedInfo = related
	}

	if asset.Source == "" {
		if deviceID != nil {
			asset.Source = "device"
		} else {
			asset.Source = "console"
		}
	}

	if err := s.repo.Create(ctx, asset); err != nil {
		_ = s.storage.Delete(ctx, saveResult.URI) // Best effort cleanup
		return nil, fmt.Errorf("保存媒体记录失败: %w", err)
	}

	return asset, nil
}

// ListMedia returns media assets matching the provided filters.
func (s *MediaService) ListMedia(ctx context.Context, input ListMediaInput) ([]*models.MediaAsset, int64, error) {
	return s.repo.List(ctx, repository.MediaFilter{
		AgentID:   input.AgentID,
		DeviceID:  input.DeviceID,
		MediaType: input.MediaType,
		Query:     input.Query,
	}, input.Limit, input.Offset)
}

// DeleteMedia removes a media asset and its persisted file.
func (s *MediaService) DeleteMedia(ctx context.Context, id uint64) error {
	asset, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("查询媒体信息失败: %w", err)
	}
	if asset == nil {
		return fmt.Errorf("媒体不存在")
	}

	if s.storage != nil {
		if err := s.storage.Delete(ctx, asset.StorageURI); err != nil {
			return fmt.Errorf("删除媒体文件失败: %w", err)
		}
	}
	if err := s.repo.Delete(ctx, id); err != nil {
		return fmt.Errorf("删除媒体记录失败: %w", err)
	}
	return nil
}

// OpenMedia resolves and opens the persisted media payload for streaming.
func (s *MediaService) OpenMedia(ctx context.Context, id uint64) (*models.MediaAsset, io.ReadCloser, error) {
	if s.storage == nil {
		return nil, nil, fmt.Errorf("media storage is not configured")
	}
	asset, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, nil, fmt.Errorf("查询媒体信息失败: %w", err)
	}
	if asset == nil {
		return nil, nil, nil
	}

	reader, err := s.storage.Open(ctx, asset.StorageURI)
	if err != nil {
		return nil, nil, fmt.Errorf("打开媒体文件失败: %w", err)
	}

	return asset, reader, nil
}

func ensureDotPrefix(ext string) string {
	if ext == "" {
		return ""
	}
	if strings.HasPrefix(ext, ".") {
		return ext
	}
	return "." + ext
}

func classifyMediaType(ext string) (string, error) {
	switch ext {
	case ".jpg", ".jpeg", ".png", ".gif", ".bmp", ".webp":
		return "image", nil
	case ".mp4", ".mov", ".mkv", ".avi", ".webm", ".m4v":
		return "video", nil
	case ".wav", ".mp3", ".aac", ".m4a", ".ogg", ".flac":
		return "audio", nil
	default:
		return "", fmt.Errorf("不支持的媒体类型: %s", ext)
	}
}
