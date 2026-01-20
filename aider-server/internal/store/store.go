package store

import (
	"context"
	"time"

	"aider-server/internal/taskmodel"
)

type Store interface {
	Create(ctx context.Context, t *taskmodel.Task) error
	Update(ctx context.Context, t *taskmodel.Task) error
	Get(ctx context.Context, id string) (*taskmodel.Task, error)
	List(ctx context.Context, filter taskmodel.ListFilter) ([]*taskmodel.Task, int, error)
	AppendLog(ctx context.Context, entry taskmodel.LogEntry, retention time.Duration) error
	LoadLogs(ctx context.Context, id string, fromSeq int64, limit int64) ([]taskmodel.LogEntry, error)
	Expire(ctx context.Context, id string, at time.Time) error
	ListExpired(ctx context.Context, now time.Time, limit int64) ([]string, error)
	RebuildIndexes(ctx context.Context) error
	Remove(ctx context.Context, id string) error
}
