package storage

import (
	"context"
	"errors"
	"io"
)

// SchemeLocal identifies local file system backed storage URIs.
const SchemeLocal = "local://"

// ErrUnsupportedScheme is returned when the storage implementation cannot handle the provided URI scheme.
var ErrUnsupportedScheme = errors.New("unsupported storage scheme")

// DocumentFile encapsulates an inbound file payload.
type DocumentFile struct {
	Name        string
	Size        int64
	ContentType string
	Reader      io.Reader
}

// SaveResult captures metadata derived during persistence.
type SaveResult struct {
	URI      string
	Size     int64
	Checksum string
}

// DocumentStorage defines the persistence contract for knowledge base artifacts.
type DocumentStorage interface {
	Save(ctx context.Context, kbID uint64, file *DocumentFile) (*SaveResult, error)
	Open(ctx context.Context, uri string) (io.ReadCloser, error)
	ResolvePath(uri string) (string, error)
	Delete(ctx context.Context, uri string) error
}
