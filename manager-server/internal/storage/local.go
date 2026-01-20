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

// LocalStorage persists documents on the local filesystem.
type LocalStorage struct {
	baseDir string
}

// NewLocalStorage constructs a LocalStorage rooted at baseDir.
func NewLocalStorage(baseDir string) (*LocalStorage, error) {
	if strings.TrimSpace(baseDir) == "" {
		return nil, fmt.Errorf("baseDir is required")
	}
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return nil, fmt.Errorf("prepare storage directory: %w", err)
	}
	return &LocalStorage{baseDir: baseDir}, nil
}

// Save writes the provided file under a knowledge base specific directory.
func (s *LocalStorage) Save(ctx context.Context, kbID uint64, file *DocumentFile) (*SaveResult, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if file == nil {
		return nil, fmt.Errorf("file payload is nil")
	}
	if file.Reader == nil {
		return nil, fmt.Errorf("file reader is nil")
	}

	sanitizedName := sanitizeFilename(file.Name)
	if sanitizedName == "" {
		sanitizedName = fmt.Sprintf("document_%d", time.Now().UnixNano())
	}
	datePrefix := time.Now().UTC().Format("20060102")
	relDir := filepath.Join(fmt.Sprintf("kb_%d", kbID), datePrefix)
	if err := os.MkdirAll(filepath.Join(s.baseDir, relDir), 0o755); err != nil {
		return nil, fmt.Errorf("create storage dir: %w", err)
	}
	filename := fmt.Sprintf("%d_%s", time.Now().UnixNano(), sanitizedName)
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
		return nil, fmt.Errorf("write storage file: %w", err)
	}

	uri := SchemeLocal + filepath.ToSlash(filepath.Join(relDir, filename))
	return &SaveResult{
		URI:      uri,
		Size:     written,
		Checksum: hex.EncodeToString(hasher.Sum(nil)),
	}, nil
}

// Open returns a reader for the given URI.
func (s *LocalStorage) Open(ctx context.Context, uri string) (io.ReadCloser, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	path, err := s.ResolvePath(uri)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open storage uri %s: %w", uri, err)
	}
	return file, nil
}

// ResolvePath converts a local storage URI into an absolute filesystem path.
func (s *LocalStorage) ResolvePath(uri string) (string, error) {
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

// Delete removes the artifact addressed by the URI.
func (s *LocalStorage) Delete(_ context.Context, uri string) error {
	path, err := s.ResolvePath(uri)
	if err != nil {
		if errors.Is(err, ErrUnsupportedScheme) {
			return nil
		}
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete storage file: %w", err)
	}
	return nil
}

func sanitizeFilename(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	name = filepath.Base(name)
	name = strings.ReplaceAll(name, string(filepath.Separator), "_")
	name = strings.ReplaceAll(name, "..", "_")
	// Replace spaces with underscores to keep URLs simple.
	name = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= 'A' && r <= 'Z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == '.', r == '-', r == '_':
			return r
		default:
			return '_'
		}
	}, name)
	return name
}
