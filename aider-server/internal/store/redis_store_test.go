package store

import (
	"context"
	"testing"
	"time"

	"aider-server/internal/taskmodel"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestRedisStoreCreateList(t *testing.T) {
	ctx := context.Background()
	mini := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	store := NewRedisStore(client, "aider")

	created := time.Now().Add(-time.Minute)
	taskItem := &taskmodel.Task{
		ID:        "task-1",
		Client:    "codex",
		Command:   "codex",
		Args:      []string{"--help"},
		Env:       map[string]string{"PATH": "/bin"},
		Labels:    map[string]string{"team": "core", "env": "dev"},
		Status:    taskmodel.StatusPending,
		CreatedAt: created,
		WorkDir:   "/tmp",
	}
	if err := store.Create(ctx, taskItem); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := store.Expire(ctx, taskItem.ID, created.Add(2*time.Hour)); err != nil {
		t.Fatalf("expire task: %v", err)
	}

	items, total, err := store.List(ctx, taskmodel.ListFilter{Labels: map[string]string{"team": "core"}})
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("expected 1 task, got total=%d len=%d", total, len(items))
	}

	taskItem.Status = taskmodel.StatusRunning
	started := time.Now()
	taskItem.StartedAt = &started
	if err := store.Update(ctx, taskItem); err != nil {
		t.Fatalf("update task: %v", err)
	}

	items, total, err = store.List(ctx, taskmodel.ListFilter{Status: taskmodel.StatusRunning})
	if err != nil {
		t.Fatalf("list running: %v", err)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("expected 1 running task, got total=%d len=%d", total, len(items))
	}
}

func TestRedisStoreLogs(t *testing.T) {
	ctx := context.Background()
	mini := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	store := NewRedisStore(client, "aider")

	entry := taskmodel.LogEntry{TaskID: "task-1", Seq: 1, TS: time.Now(), Stream: "stdout", Line: "hello"}
	if err := store.AppendLog(ctx, entry, time.Minute); err != nil {
		t.Fatalf("append log: %v", err)
	}
	logs, err := store.LoadLogs(ctx, "task-1", 0, 10)
	if err != nil {
		t.Fatalf("load logs: %v", err)
	}
	if len(logs) != 0 {
		t.Fatalf("expected 0 logs, got %d", len(logs))
	}
}

func TestRedisStoreExpired(t *testing.T) {
	ctx := context.Background()
	mini := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	store := NewRedisStore(client, "aider")

	created := time.Now().Add(-2 * time.Hour)
	taskItem := &taskmodel.Task{
		ID:        "task-expired",
		Client:    "codex",
		Command:   "codex",
		Status:    taskmodel.StatusCompleted,
		CreatedAt: created,
		WorkDir:   "/tmp",
	}
	if err := store.Create(ctx, taskItem); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := store.Expire(ctx, taskItem.ID, time.Now().Add(-time.Minute)); err != nil {
		t.Fatalf("expire task: %v", err)
	}

	ids, err := store.ListExpired(ctx, time.Now(), 10)
	if err != nil {
		t.Fatalf("list expired: %v", err)
	}
	if len(ids) != 1 || ids[0] != "task-expired" {
		t.Fatalf("unexpected expired ids: %v", ids)
	}
}

func TestRedisStorePersistenceAcrossInstances(t *testing.T) {
	ctx := context.Background()
	mini := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	store := NewRedisStore(client, "aider")

	created := time.Now().Add(-time.Minute)
	taskItem := &taskmodel.Task{
		ID:        "task-restart",
		Client:    "codex",
		Command:   "codex",
		Status:    taskmodel.StatusPending,
		CreatedAt: created,
		WorkDir:   "/tmp",
	}
	if err := store.Create(ctx, taskItem); err != nil {
		t.Fatalf("create task: %v", err)
	}

	anotherClient := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	anotherStore := NewRedisStore(anotherClient, "aider")
	loaded, err := anotherStore.Get(ctx, taskItem.ID)
	if err != nil {
		t.Fatalf("load task after restart: %v", err)
	}
	if loaded.ID != taskItem.ID || loaded.Status != taskItem.Status {
		t.Fatalf("unexpected task after restart: %#v", loaded)
	}
}
