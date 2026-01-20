package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"aider-server/internal/taskmodel"

	"github.com/redis/go-redis/v9"
)

type RedisStore struct {
	client    *redis.Client
	keyPrefix string
}

func NewRedisStore(client *redis.Client, keyPrefix string) *RedisStore {
	if strings.TrimSpace(keyPrefix) == "" {
		keyPrefix = "aider"
	}
	return &RedisStore{client: client, keyPrefix: strings.TrimRight(keyPrefix, ":")}
}

func (s *RedisStore) Create(ctx context.Context, t *taskmodel.Task) error {
	if t == nil {
		return fmt.Errorf("task is nil")
	}
	payload, err := encodeTask(t)
	if err != nil {
		return err
	}
	id := t.ID
	pipe := s.client.TxPipeline()
	s.enqueueTaskHash(ctx, pipe, s.taskKey(id), payload)
	pipe.ZAdd(ctx, s.allKey(), redis.Z{Score: float64(t.CreatedAt.Unix()), Member: id})
	pipe.SAdd(ctx, s.statusKey(t.Status), id)
	for key, value := range t.Labels {
		pipe.SAdd(ctx, s.labelKey(key, value), id)
	}
	if t.CreatedAt.IsZero() {
		return fmt.Errorf("created_at is required")
	}
	if t.CreatedAt.After(time.Now().Add(24 * time.Hour)) {
		return fmt.Errorf("created_at is in the future")
	}
	_, err = pipe.Exec(ctx)
	return err
}

func (s *RedisStore) Update(ctx context.Context, t *taskmodel.Task) error {
	if t == nil {
		return fmt.Errorf("task is nil")
	}
	existing, err := s.Get(ctx, t.ID)
	if err != nil && !errors.Is(err, redis.Nil) {
		return err
	}
	payload, err := encodeTask(t)
	if err != nil {
		return err
	}
	pipe := s.client.TxPipeline()
	s.enqueueTaskHash(ctx, pipe, s.taskKey(t.ID), payload)
	for _, status := range []taskmodel.Status{taskmodel.StatusPending, taskmodel.StatusRunning, taskmodel.StatusCompleted, taskmodel.StatusFailed, taskmodel.StatusInterrupted} {
		pipe.SRem(ctx, s.statusKey(status), t.ID)
	}
	pipe.SAdd(ctx, s.statusKey(t.Status), t.ID)
	if existing != nil {
		for key, value := range existing.Labels {
			pipe.SRem(ctx, s.labelKey(key, value), t.ID)
		}
	}
	for key, value := range t.Labels {
		pipe.SAdd(ctx, s.labelKey(key, value), t.ID)
	}
	_, err = pipe.Exec(ctx)
	return err
}

func (s *RedisStore) Get(ctx context.Context, id string) (*taskmodel.Task, error) {
	data, err := s.client.HGetAll(ctx, s.taskKey(id)).Result()
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, redis.Nil
	}
	return decodeTask(data)
}

func (s *RedisStore) List(ctx context.Context, filter taskmodel.ListFilter) ([]*taskmodel.Task, int, error) {
	fromScore := "-inf"
	toScore := "+inf"
	if filter.From != nil {
		fromScore = strconv.FormatInt(filter.From.Unix(), 10)
	}
	if filter.To != nil {
		toScore = strconv.FormatInt(filter.To.Unix(), 10)
	}
	ids, err := s.client.ZRevRangeByScore(ctx, s.allKey(), &redis.ZRangeBy{
		Min: fromScore,
		Max: toScore,
	}).Result()
	if err != nil {
		return nil, 0, err
	}
	tasks := make([]*taskmodel.Task, 0, len(ids))
	for _, id := range ids {
		item, err := s.Get(ctx, id)
		if err != nil {
			if errors.Is(err, redis.Nil) {
				continue
			}
			return nil, 0, err
		}
		if filter.Status != "" && item.Status != filter.Status {
			continue
		}
		if !labelsMatch(item.Labels, filter.Labels) {
			continue
		}
		tasks = append(tasks, item)
	}
	return paginateTasks(tasks, filter.Limit, filter.Offset), len(tasks), nil
}

func (s *RedisStore) AppendLog(ctx context.Context, entry taskmodel.LogEntry, retention time.Duration) error {
	// Logs are intentionally kept out of Redis; streaming uses in-memory log hubs.
	return nil
}

func (s *RedisStore) LoadLogs(ctx context.Context, id string, fromSeq int64, limit int64) ([]taskmodel.LogEntry, error) {
	return []taskmodel.LogEntry{}, nil
}

func (s *RedisStore) Expire(ctx context.Context, id string, at time.Time) error {
	key := s.taskKey(id)
	pipe := s.client.TxPipeline()
	pipe.ExpireAt(ctx, key, at)
	pipe.ZAdd(ctx, s.expiresKey(), redis.Z{Score: float64(at.Unix()), Member: id})
	_, err := pipe.Exec(ctx)
	return err
}

func (s *RedisStore) ListExpired(ctx context.Context, now time.Time, limit int64) ([]string, error) {
	max := strconv.FormatInt(now.Unix(), 10)
	rangeBy := &redis.ZRangeBy{
		Min:   "-inf",
		Max:   max,
		Count: limit,
	}
	return s.client.ZRangeByScore(ctx, s.expiresKey(), rangeBy).Result()
}

func (s *RedisStore) RebuildIndexes(ctx context.Context) error {
	return nil
}

func (s *RedisStore) Remove(ctx context.Context, id string) error {
	t, err := s.Get(ctx, id)
	if err != nil && !errors.Is(err, redis.Nil) {
		return err
	}
	pipe := s.client.TxPipeline()
	pipe.Del(ctx, s.taskKey(id))
	pipe.ZRem(ctx, s.allKey(), id)
	pipe.ZRem(ctx, s.expiresKey(), id)
	for _, status := range []taskmodel.Status{taskmodel.StatusPending, taskmodel.StatusRunning, taskmodel.StatusCompleted, taskmodel.StatusFailed, taskmodel.StatusInterrupted} {
		pipe.SRem(ctx, s.statusKey(status), id)
	}
	if t != nil {
		for key, value := range t.Labels {
			pipe.SRem(ctx, s.labelKey(key, value), id)
		}
	}
	_, err = pipe.Exec(ctx)
	return err
}

func (s *RedisStore) taskKey(id string) string {
	return fmt.Sprintf("%s:task:%s", s.keyPrefix, id)
}

func (s *RedisStore) enqueueTaskHash(ctx context.Context, pipe redis.Pipeliner, key string, payload map[string]interface{}) {
	for field, value := range payload {
		pipe.HSet(ctx, key, field, value)
	}
}

func (s *RedisStore) allKey() string {
	return fmt.Sprintf("%s:tasks:all", s.keyPrefix)
}

func (s *RedisStore) expiresKey() string {
	return fmt.Sprintf("%s:tasks:expires", s.keyPrefix)
}

func (s *RedisStore) statusKey(status taskmodel.Status) string {
	return fmt.Sprintf("%s:tasks:status:%s", s.keyPrefix, status)
}

func (s *RedisStore) labelKey(key, value string) string {
	return fmt.Sprintf("%s:tasks:label:%s:%s", s.keyPrefix, escapeLabel(key), escapeLabel(value))
}

func escapeLabel(value string) string {
	return url.QueryEscape(value)
}

func encodeTask(t *taskmodel.Task) (map[string]interface{}, error) {
	args, err := json.Marshal(t.Args)
	if err != nil {
		return nil, fmt.Errorf("encode args: %w", err)
	}
	labels, err := json.Marshal(t.Labels)
	if err != nil {
		return nil, fmt.Errorf("encode labels: %w", err)
	}
	env, err := json.Marshal(t.Env)
	if err != nil {
		return nil, fmt.Errorf("encode env: %w", err)
	}
	payload := map[string]interface{}{
		"id":         t.ID,
		"client":     t.Client,
		"protocol":   string(t.Protocol),
		"command":    t.Command,
		"args":       string(args),
		"env":        string(env),
		"labels":     string(labels),
		"runner_id":  t.RunnerID,
		"status":     string(t.Status),
		"created_at": t.CreatedAt.Format(time.RFC3339Nano),
		"work_dir":   t.WorkDir,
	}
	if t.StartedAt != nil {
		payload["started_at"] = t.StartedAt.Format(time.RFC3339Nano)
	}
	if t.FinishedAt != nil {
		payload["finished_at"] = t.FinishedAt.Format(time.RFC3339Nano)
	}
	if t.ExitCode != nil {
		payload["exit_code"] = strconv.Itoa(*t.ExitCode)
	}
	if t.Error != "" {
		payload["error"] = t.Error
	}
	return payload, nil
}

func decodeTask(data map[string]string) (*taskmodel.Task, error) {
	get := func(key string) string {
		if v, ok := data[key]; ok {
			return v
		}
		return ""
	}
	createdAt, err := time.Parse(time.RFC3339Nano, get("created_at"))
	if err != nil {
		return nil, fmt.Errorf("parse created_at: %w", err)
	}
	var args []string
	if raw := get("args"); raw != "" {
		_ = json.Unmarshal([]byte(raw), &args)
	}
	labels := map[string]string{}
	if raw := get("labels"); raw != "" {
		_ = json.Unmarshal([]byte(raw), &labels)
	}
	env := map[string]string{}
	if raw := get("env"); raw != "" {
		_ = json.Unmarshal([]byte(raw), &env)
	}
	t := &taskmodel.Task{
		ID:        get("id"),
		Client:    get("client"),
		Protocol:  taskmodel.Protocol(get("protocol")),
		Command:   get("command"),
		Args:      args,
		Env:       env,
		Labels:    labels,
		RunnerID:  get("runner_id"),
		Status:    taskmodel.Status(get("status")),
		CreatedAt: createdAt,
		WorkDir:   get("work_dir"),
	}
	if t.Protocol == "" {
		t.Protocol = taskmodel.ProtocolPTY
	}
	if raw := get("started_at"); raw != "" {
		if ts, err := time.Parse(time.RFC3339Nano, raw); err == nil {
			t.StartedAt = &ts
		}
	}
	if raw := get("finished_at"); raw != "" {
		if ts, err := time.Parse(time.RFC3339Nano, raw); err == nil {
			t.FinishedAt = &ts
		}
	}
	if raw := get("exit_code"); raw != "" {
		if val, err := strconv.Atoi(raw); err == nil {
			t.ExitCode = &val
		}
	}
	if raw := get("error"); raw != "" {
		t.Error = raw
	}
	return t, nil
}

func labelsMatch(taskLabels, filter map[string]string) bool {
	if len(filter) == 0 {
		return true
	}
	for key, value := range filter {
		if taskLabels[key] != value {
			return false
		}
	}
	return true
}

func paginateTasks(tasks []*taskmodel.Task, limit, offset int) []*taskmodel.Task {
	if offset < 0 {
		offset = 0
	}
	if offset >= len(tasks) {
		return []*taskmodel.Task{}
	}
	if limit <= 0 {
		return tasks[offset:]
	}
	end := offset + limit
	if end > len(tasks) {
		end = len(tasks)
	}
	return tasks[offset:end]
}
