package functions

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"

	manager_api "backend-server/internal/adapters/manager"
	manager_types "backend-server/internal/adapters/manager/types"
	"backend-server/internal/config"
	"backend-server/internal/domain/imagegen"
	log "backend-server/internal/infrastructure/logger"
)

type storedImage struct {
	URL string
}

func storeImagesToMedia(
	ctx context.Context,
	managerSvc manager_api.ManagerAPIService,
	deviceID string,
	source string,
	description string,
	related map[string]any,
	images []imagegen.ImageData,
) ([]imagegen.ImageData, []string) {
	if managerSvc == nil {
		return images, collectImageURLs(images)
	}

	var (
		baseURL = resolveManagerBaseURL()
		urls    []string
		updated = make([]imagegen.ImageData, 0, len(images))
	)

	for _, item := range images {
		content, filename, contentType, err := resolveImagePayload(ctx, item)
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
		item.B64JSON = ""
		updated = append(updated, item)
		if mediaURL != "" {
			urls = append(urls, mediaURL)
		}
	}

	return updated, urls
}

func collectImageURLs(images []imagegen.ImageData) []string {
	var urls []string
	for _, item := range images {
		if item.URL != "" {
			urls = append(urls, item.URL)
		}
	}
	return urls
}

func resolveImagePayload(ctx context.Context, item imagegen.ImageData) ([]byte, string, string, error) {
	if item.URL != "" {
		return downloadImage(ctx, item.URL)
	}
	if item.B64JSON != "" {
		return decodeBase64Image(item.B64JSON)
	}
	return nil, "", "", fmt.Errorf("empty image payload")
}

func downloadImage(ctx context.Context, url string) ([]byte, string, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, "", "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", "", fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", "", err
	}

	contentType := strings.TrimSpace(resp.Header.Get("Content-Type"))
	if contentType == "" {
		contentType = http.DetectContentType(body)
	}
	filename := path.Base(strings.TrimSpace(req.URL.Path))
	if filename == "." || filename == "/" || filename == "" {
		filename = "image" + extensionFromContentType(contentType)
	}
	return body, filename, contentType, nil
}

func decodeBase64Image(raw string) ([]byte, string, string, error) {
	payload := strings.TrimSpace(raw)
	contentType := "image/png"
	if strings.HasPrefix(payload, "data:") {
		parts := strings.SplitN(payload, ",", 2)
		if len(parts) == 2 {
			meta := parts[0]
			payload = parts[1]
			if strings.HasPrefix(meta, "data:") {
				meta = strings.TrimPrefix(meta, "data:")
			}
			if idx := strings.Index(meta, ";"); idx >= 0 {
				contentType = meta[:idx]
			} else if meta != "" {
				contentType = meta
			}
		}
	}

	decoded, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return nil, "", "", err
	}
	filename := "image" + extensionFromContentType(contentType)
	return decoded, filename, contentType, nil
}

func extensionFromContentType(contentType string) string {
	switch strings.ToLower(strings.TrimSpace(contentType)) {
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	case "image/bmp":
		return ".bmp"
	case "video/mp4":
		return ".mp4"
	case "video/quicktime":
		return ".mov"
	case "video/x-matroska":
		return ".mkv"
	case "video/x-msvideo":
		return ".avi"
	case "video/webm":
		return ".webm"
	case "audio/wav", "audio/x-wav", "audio/wave", "audio/vnd.wave":
		return ".wav"
	case "audio/mpeg":
		return ".mp3"
	case "audio/aac":
		return ".aac"
	case "audio/mp4":
		return ".m4a"
	case "audio/ogg":
		return ".ogg"
	case "audio/flac":
		return ".flac"
	default:
		return ".png"
	}
}

func resolveManagerBaseURL() string {
	cfg := config.GetConfig()
	if cfg == nil {
		return ""
	}
	base := strings.TrimSpace(cfg.ManagerAPI.BaseURL)
	if base == "" {
		return ""
	}
	parts := strings.Split(base, ",")
	if len(parts) > 0 {
		base = strings.TrimSpace(parts[0])
	}
	return strings.TrimRight(base, "/")
}

func buildMediaContentURL(baseURL string, mediaID uint64) string {
	if baseURL == "" || mediaID == 0 {
		return ""
	}
	return fmt.Sprintf("%s/media/%d/content", baseURL, mediaID)
}

func buildMarkdownImageLinks(urls []string) string {
	if len(urls) == 0 {
		return ""
	}
	lines := make([]string, 0, len(urls))
	for _, url := range urls {
		trimmed := strings.TrimSpace(url)
		if trimmed == "" {
			continue
		}
		lines = append(lines, fmt.Sprintf("![图片](%s)", trimmed))
	}
	return strings.Join(lines, "\n")
}
