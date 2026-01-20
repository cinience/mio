package functions

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	manager_api "backend-server/internal/adapters/manager"
	manager_types "backend-server/internal/adapters/manager/types"
	"backend-server/internal/domain/videogen"
	log "backend-server/internal/infrastructure/logger"
)

func supportsVideoCapability(cfg map[string]interface{}, capability string) bool {
	return supportsImageCapability(cfg, capability)
}

func storeVideosToMedia(
	ctx context.Context,
	managerSvc manager_api.ManagerAPIService,
	deviceID string,
	source string,
	description string,
	related map[string]any,
	videos []videogen.VideoData,
) ([]videogen.VideoData, []string) {
	if managerSvc == nil {
		return videos, collectVideoURLs(videos)
	}

	var (
		baseURL = resolveManagerBaseURL()
		urls    []string
		updated = make([]videogen.VideoData, 0, len(videos))
	)

	for _, item := range videos {
		content, filename, contentType, err := downloadImage(ctx, item.URL)
		if err != nil {
			updated = append(updated, item)
			if item.URL != "" {
				urls = append(urls, item.URL)
			}
			continue
		}

		uploadReq := &manager_types.MediaUploadRequest{
			DeviceID:    deviceID,
			Source:      source,
			Description: description,
			FileName:    filename,
			ContentType: contentType,
			Reader:      bytes.NewReader(content),
			RelatedInfo: related,
		}

		asset, err := managerSvc.UploadMedia(ctx, uploadReq)
		if err != nil || asset == nil {
			log.Warnf("上传媒体失败: %v", err)
			updated = append(updated, item)
			if item.URL != "" {
				urls = append(urls, item.URL)
			}
			continue
		}

		mediaURL := strings.TrimSpace(asset.PublicURL)
		if mediaURL == "" {
			mediaURL = buildMediaContentURL(baseURL, asset.ID)
		}
		if mediaURL == "" {
			mediaURL = asset.StorageURI
		}

		item.URL = mediaURL
		updated = append(updated, item)
		if mediaURL != "" {
			urls = append(urls, mediaURL)
		}
	}

	return updated, urls
}

func collectVideoURLs(videos []videogen.VideoData) []string {
	var urls []string
	for _, item := range videos {
		if item.URL != "" {
			urls = append(urls, item.URL)
		}
	}
	return urls
}

func buildMarkdownVideoLinks(urls []string) string {
	if len(urls) == 0 {
		return ""
	}
	lines := make([]string, 0, len(urls))
	for _, url := range urls {
		trimmed := strings.TrimSpace(url)
		if trimmed == "" {
			continue
		}
		lines = append(lines, fmt.Sprintf("[视频](%s)", trimmed))
	}
	return strings.Join(lines, "\n")
}

func uploadAudioToMedia(ctx context.Context, managerSvc manager_api.ManagerAPIService, deviceID, source, description string, audio []byte, contentType string) (string, error) {
	if managerSvc == nil {
		return "", fmt.Errorf("manager service unavailable")
	}
	if len(audio) == 0 {
		return "", fmt.Errorf("audio payload is empty")
	}

	trimmedType := strings.TrimSpace(contentType)
	if trimmedType == "" {
		trimmedType = "audio/wav"
	}
	filename := "voice" + extensionFromContentType(trimmedType)

	uploadReq := &manager_types.MediaUploadRequest{
		DeviceID:    deviceID,
		Source:      source,
		Description: description,
		FileName:    filename,
		ContentType: trimmedType,
		Reader:      bytes.NewReader(audio),
	}

	asset, err := managerSvc.UploadMedia(ctx, uploadReq)
	if err != nil || asset == nil {
		return "", fmt.Errorf("上传音频失败: %v", err)
	}

	baseURL := resolveManagerBaseURL()
	mediaURL := strings.TrimSpace(asset.PublicURL)
	if mediaURL == "" {
		mediaURL = buildMediaContentURL(baseURL, asset.ID)
	}
	if mediaURL == "" {
		mediaURL = asset.StorageURI
	}
	if mediaURL == "" {
		return "", fmt.Errorf("音频地址为空")
	}

	return mediaURL, nil
}
