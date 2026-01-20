package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// MediaStorage persists media assets (images, videos) for agents or devices.
type MediaStorage interface {
	Save(ctx context.Context, agentID string, deviceID *string, file *DocumentFile) (*SaveResult, error)
	Open(ctx context.Context, uri string) (io.ReadCloser, error)
	ResolvePath(uri string) (string, error)
	Delete(ctx context.Context, uri string) error
}

// LocalMediaStorage stores assets on the local filesystem.
type LocalMediaStorage struct {
	baseDir string
}

// NewLocalMediaStorage constructs a LocalMediaStorage rooted at the provided directory.
func NewLocalMediaStorage(baseDir string) (*LocalMediaStorage, error) {
	if strings.TrimSpace(baseDir) == "" {
		return nil, fmt.Errorf("media storage baseDir is required")
	}
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return nil, fmt.Errorf("prepare media storage dir: %w", err)
	}
	return &LocalMediaStorage{baseDir: baseDir}, nil
}

// Save persists the provided media payload and returns its storage metadata.
func (s *LocalMediaStorage) Save(ctx context.Context, agentID string, deviceID *string, file *DocumentFile) (*SaveResult, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if file == nil {
		return nil, fmt.Errorf("file payload is nil")
	}
	if file.Reader == nil {
		return nil, fmt.Errorf("file reader is nil")
	}

	sanitizedAgent := sanitizePathSegment(agentID)
	if sanitizedAgent == "" {
		sanitizedAgent = "unassigned"
	}

	segments := []string{"media", fmt.Sprintf("agent_%s", sanitizedAgent)}
	if deviceID != nil && strings.TrimSpace(*deviceID) != "" {
		segments = append(segments, fmt.Sprintf("device_%s", sanitizePathSegment(*deviceID)))
	}
	segments = append(segments, time.Now().UTC().Format("20060102"))

	relDir := filepath.Join(segments...)
	if err := os.MkdirAll(filepath.Join(s.baseDir, relDir), 0o755); err != nil {
		return nil, fmt.Errorf("create media directory: %w", err)
	}

	filename := fmt.Sprintf("%d_%s", time.Now().UnixNano(), sanitizeFilename(file.Name))
	if filename == "" {
		filename = fmt.Sprintf("%d_media", time.Now().UnixNano())
	}

	fullPath := filepath.Join(s.baseDir, relDir, filename)

	dest, err := os.OpenFile(fullPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open media file: %w", err)
	}
	defer dest.Close()

	hasher := sha256.New()
	written, err := io.Copy(dest, io.TeeReader(file.Reader, hasher))
	if err != nil {
		_ = dest.Close()
		_ = os.Remove(fullPath)
		return nil, fmt.Errorf("write media file: %w", err)
	}

	uri := SchemeLocal + filepath.ToSlash(filepath.Join(relDir, filename))
	return &SaveResult{
		URI:      uri,
		Size:     written,
		Checksum: hex.EncodeToString(hasher.Sum(nil)),
	}, nil
}

// Open returns a reader for the given storage URI.
func (s *LocalMediaStorage) Open(ctx context.Context, uri string) (io.ReadCloser, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	path, err := s.ResolvePath(uri)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open media file: %w", err)
	}
	return f, nil
}

// ResolvePath expands a media URI into an absolute filesystem path.
func (s *LocalMediaStorage) ResolvePath(uri string) (string, error) {
	if !strings.HasPrefix(uri, SchemeLocal) {
		return "", ErrUnsupportedScheme
	}
	rel := strings.TrimPrefix(uri, SchemeLocal)
	clean := filepath.Clean(rel)
	if strings.HasPrefix(clean, "..") {
		return "", fmt.Errorf("refuse to escape media base directory")
	}
	return filepath.Join(s.baseDir, clean), nil
}

// Delete removes the media artifact referenced by the URI.
func (s *LocalMediaStorage) Delete(_ context.Context, uri string) error {
	path, err := s.ResolvePath(uri)
	if err != nil {
		if errors.Is(err, ErrUnsupportedScheme) {
			return nil
		}
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete media file: %w", err)
	}
	return nil
}

func sanitizePathSegment(segment string) string {
	segment = strings.TrimSpace(segment)
	if segment == "" {
		return ""
	}
	segment = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= 'A' && r <= 'Z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == '-', r == '_':
			return r
		default:
			return '_'
		}
	}, segment)
	return segment
}
