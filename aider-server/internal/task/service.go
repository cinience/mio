package task

import (
	"context"
	"time"

	"aider-server/internal/taskmodel"
)

type Service interface {
	CreateTask(ctx context.Context, req taskmodel.CreateRequest, defaultEnv map[string]string) (*taskmodel.Task, error)
	GetTask(ctx context.Context, id string) (*taskmodel.Task, error)
	ListTasks(ctx context.Context, filter taskmodel.ListFilter) ([]*taskmodel.Task, int, error)
	Interrupt(ctx context.Context, id string) (*taskmodel.Task, error)
	DeleteTask(ctx context.Context, id string) error
	SubscribeLogs(ctx context.Context, id string, fromSeq int64) (<-chan taskmodel.LogEntry, []taskmodel.LogEntry, func())
	SubscribeTerminal(id string, fromCursor int64) (<-chan []byte, []byte, func())
	SendInput(ctx context.Context, id string, payload []byte) error
	SendChat(ctx context.Context, id string, message string) error
	ResizeTerminal(ctx context.Context, id string, cols int, rows int) error
	RebuildIndexes(ctx context.Context) error
	CleanupExpired(ctx context.Context, interval time.Duration)
}
