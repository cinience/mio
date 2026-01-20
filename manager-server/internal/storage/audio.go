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

// AudioStorage exposes persistence helpers for podcast artifacts.
type AudioStorage interface {
	SaveEpisode(ctx context.Context, projectID string, file *DocumentFile) (*SaveResult, error)
	SaveEpisodeAsset(ctx context.Context, projectID string, episodeID uint64, assetKind string, file *DocumentFile) (*SaveResult, error)
	Open(ctx context.Context, uri string) (io.ReadCloser, error)
	ResolvePath(uri string) (string, error)
	Delete(ctx context.Context, uri string) error
}

// LocalAudioStorage stores audio content on the local filesystem with predictable paths.
type LocalAudioStorage struct {
	baseDir string
}

// NewLocalAudioStorage prepares a LocalAudioStorage rooted at baseDir.
func NewLocalAudioStorage(baseDir string) (*LocalAudioStorage, error) {
	if strings.TrimSpace(baseDir) == "" {
		return nil, fmt.Errorf("baseDir is required")
	}
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return nil, fmt.Errorf("prepare audio storage: %w", err)
	}
	return &LocalAudioStorage{baseDir: baseDir}, nil
}

// SaveEpisode persists the primary audio payload for the given project.
func (s *LocalAudioStorage) SaveEpisode(ctx context.Context, projectID string, file *DocumentFile) (*SaveResult, error) {
	if file == nil {
		return nil, fmt.Errorf("file payload is nil")
	}
	return s.save(ctx, []string{
		fmt.Sprintf("project_%s", sanitizeFilename(projectID)),
		"episodes",
		time.Now().UTC().Format("20060102"),
	}, file)
}

// SaveEpisodeAsset stores auxiliary artifacts (cover, transcript, waveform) for an episode.
func (s *LocalAudioStorage) SaveEpisodeAsset(ctx context.Context, projectID string, episodeID uint64, assetKind string, file *DocumentFile) (*SaveResult, error) {
	if strings.TrimSpace(assetKind) == "" {
		return nil, fmt.Errorf("asset kind is required")
	}
	if file == nil {
		return nil, fmt.Errorf("file payload is nil")
	}
	return s.save(ctx, []string{
		fmt.Sprintf("project_%s", sanitizeFilename(projectID)),
		assetKind,
		fmt.Sprintf("episode_%d", episodeID),
		time.Now().UTC().Format("20060102"),
	}, file)
}

// Open returns a reader for the provided URI.
func (s *LocalAudioStorage) Open(ctx context.Context, uri string) (io.ReadCloser, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	path, err := s.ResolvePath(uri)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open audio artifact: %w", err)
	}
	return f, nil
}

// ResolvePath expands a storage URI into an absolute file path.
func (s *LocalAudioStorage) ResolvePath(uri string) (string, error) {
	if !strings.HasPrefix(uri, SchemeLocal) {
		return "", ErrUnsupportedScheme
	}
	rel := strings.TrimPrefix(uri, SchemeLocal)
	clean := filepath.Clean(rel)
	if strings.HasPrefix(clean, "..") {
		return "", fmt.Errorf("refuse to escape base directory")
	}
	return filepath.Join(s.baseDir, clean), nil
}

// Delete removes the addressed artifact if it exists.
func (s *LocalAudioStorage) Delete(_ context.Context, uri string) error {
	path, err := s.ResolvePath(uri)
	if err != nil {
		if errors.Is(err, ErrUnsupportedScheme) {
			return nil
		}
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete audio artifact: %w", err)
	}
	return nil
}

func (s *LocalAudioStorage) save(ctx context.Context, segments []string, file *DocumentFile) (*SaveResult, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if file.Reader == nil {
		return nil, fmt.Errorf("file reader is nil")
	}

	baseName := sanitizeFilename(file.Name)
	if baseName == "" {
		baseName = fmt.Sprintf("audio_%d", time.Now().UnixNano())
	}

	pathSegments := append([]string{"audio"}, segments...)
	relDir := filepath.Join(pathSegments...)
	if err := os.MkdirAll(filepath.Join(s.baseDir, relDir), 0o755); err != nil {
		return nil, fmt.Errorf("create audio storage dir: %w", err)
	}

	filename := fmt.Sprintf("%d_%s", time.Now().UnixNano(), baseName)
	fullPath := filepath.Join(s.baseDir, relDir, filename)

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	dest, err := os.OpenFile(fullPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open storage file: %w", err)
	}
	defer dest.Close()

	hasher := sha256.New()
	written, err := io.Copy(dest, io.TeeReader(file.Reader, hasher))
	if err != nil {
		_ = dest.Close()
		_ = os.Remove(fullPath)
		return nil, fmt.Errorf("write audio file: %w", err)
	}

	uri := SchemeLocal + filepath.ToSlash(filepath.Join(relDir, filename))
	return &SaveResult{
		URI:      uri,
		Size:     written,
		Checksum: hex.EncodeToString(hasher.Sum(nil)),
	}, nil
}
