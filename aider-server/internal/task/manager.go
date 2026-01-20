package task

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"aider-server/internal/store"
	"aider-server/internal/task/adapters"
	"aider-server/internal/taskmodel"

	"github.com/google/uuid"
)

const (
	interruptTimeout      = 5 * time.Second
	logBufferLimit        = 400
	terminalBuffer        = 16 * 1024
	terminalCleanupDelay  = 5 * time.Minute
	terminalInactivityTTL = 30 * time.Minute
)

type Manager struct {
	store           store.Store
	logHub          *LogHub
	terminalHub     *TerminalHub
	registry        *adapters.Registry
	allowedBinaries map[string]struct{}
	dataDir         string
	retention       time.Duration
	codexConfig     CodexProtocolConfig

	mu      sync.Mutex
	running map[string]*Process
	apps    map[string]*appServerProcess
	labels  map[string]map[string]map[string]struct{}

	pendingInputs map[string][]byte

	appLogMu    sync.Mutex
	appLogFiles map[string]*os.File
}

func NewManager(store store.Store, registry *adapters.Registry, allowed []string, dataDir string, retention time.Duration, codexConfig CodexProtocolConfig) *Manager {
	allowedSet := map[string]struct{}{}
	for _, name := range allowed {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		allowedSet[name] = struct{}{}
	}
	if retention <= 0 {
		retention = 24 * time.Hour
	}
	return &Manager{
		store:           store,
		logHub:          NewLogHub(logBufferLimit),
		terminalHub:     NewTerminalHub(terminalBuffer),
		registry:        registry,
		allowedBinaries: allowedSet,
		dataDir:         dataDir,
		retention:       retention,
		codexConfig:     codexConfig,
		running:         map[string]*Process{},
		apps:            map[string]*appServerProcess{},
		labels:          map[string]map[string]map[string]struct{}{},
		pendingInputs:   map[string][]byte{},
		appLogFiles:     map[string]*os.File{},
	}
}

func (m *Manager) RebuildIndexes(ctx context.Context) error {
	items, _, err := m.store.List(ctx, taskmodel.ListFilter{})
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.labels = map[string]map[string]map[string]struct{}{}
	for _, t := range items {
		m.addLabelIndex(t.ID, t.Labels)
	}
	m.mu.Unlock()
	return nil
}

func (m *Manager) CreateTask(ctx context.Context, req taskmodel.CreateRequest, defaultEnv map[string]string) (*taskmodel.Task, error) {
	if strings.TrimSpace(req.Client) == "" {
		return nil, fmt.Errorf("client is required")
	}
	protocol, err := ResolveProtocol(req.Client, req.Protocol, m.codexConfig.DefaultProtocol)
	if err != nil {
		return nil, err
	}
	adapter, ok := m.registry.Get(req.Client)
	if !ok {
		return nil, fmt.Errorf("unknown client %q", req.Client)
	}
	cmd, args, err := adapters.BuildCommand(adapter, req.Args)
	if err != nil {
		return nil, err
	}
	prompt := strings.TrimSpace(req.Prompt)
	if prompt != "" && protocol != taskmodel.ProtocolAppServer {
		flags := adapters.DetectPromptFlags(cmd)
		if req.Interactive {
			if !strings.EqualFold(filepath.Base(cmd), "iflow") && flags.PromptInteractive != "" {
				args = adapters.AppendPromptArgsWithFlag(args, prompt, flags.PromptInteractive)
				prompt = ""
			}
		} else {
			args = adapters.AppendPromptArgs(args, prompt, flags)
			prompt = ""
		}
	}
	slog.Info("task command prepared",
		"client", req.Client,
		"protocol", protocol,
		"interactive", req.Interactive,
		"command", cmd,
		"args", args,
		"prompt_len", len(req.Prompt),
		"prompt_pending", prompt != "",
	)
	if !m.allowedBinary(cmd) {
		return nil, fmt.Errorf("binary %q not allowed", cmd)
	}
	if protocol == taskmodel.ProtocolAppServer && !m.allowedBinary(m.codexConfig.AppServer.Command) {
		return nil, fmt.Errorf("binary %q not allowed", m.codexConfig.AppServer.Command)
	}
	id := uuid.NewString()
	workDir := filepath.Join(m.dataDir, "tasks", id)
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return nil, fmt.Errorf("create work dir: %w", err)
	}
	createdAt := time.Now()
	t := &taskmodel.Task{
		ID:        id,
		Client:    req.Client,
		Protocol:  protocol,
		Command:   cmd,
		Args:      args,
		Env:       sanitizeEnv(req.Env),
		Labels:    sanitizeLabels(req.Labels),
		Status:    taskmodel.StatusPending,
		CreatedAt: createdAt,
		WorkDir:   workDir,
	}
	if err := m.store.Create(ctx, t); err != nil {
		return nil, err
	}
	if m.retention > 0 {
		_ = m.store.Expire(ctx, t.ID, createdAt.Add(m.retention))
	}
	m.mu.Lock()
	m.addLabelIndex(t.ID, t.Labels)
	if req.Interactive && protocol == taskmodel.ProtocolPTY {
		if payload := BuildPromptPayload(prompt); len(payload) > 0 {
			m.pendingInputs[t.ID] = payload
		}
	}
	m.mu.Unlock()

	if protocol == taskmodel.ProtocolAppServer {
		go m.runAppServerTask(context.Background(), t, defaultEnv, prompt)
	} else {
		go m.runTask(context.Background(), t, defaultEnv)
	}

	return t, nil
}

func (m *Manager) GetTask(ctx context.Context, id string) (*taskmodel.Task, error) {
	return m.store.Get(ctx, id)
}

func (m *Manager) ListTasks(ctx context.Context, filter taskmodel.ListFilter) ([]*taskmodel.Task, int, error) {
	if len(filter.Labels) == 0 {
		return m.store.List(ctx, filter)
	}
	ids := m.matchLabels(filter.Labels)
	items := make([]*taskmodel.Task, 0, len(ids))
	for id := range ids {
		t, err := m.store.Get(ctx, id)
		if err != nil {
			continue
		}
		if filter.Status != "" && t.Status != filter.Status {
			continue
		}
		if filter.From != nil && t.CreatedAt.Before(*filter.From) {
			continue
		}
		if filter.To != nil && t.CreatedAt.After(*filter.To) {
			continue
		}
		items = append(items, t)
	}
	return paginate(items, filter.Limit, filter.Offset), len(items), nil
}

func (m *Manager) Interrupt(ctx context.Context, id string) (*taskmodel.Task, error) {
	m.mu.Lock()
	proc := m.running[id]
	app := m.apps[id]
	m.mu.Unlock()
	if proc == nil {
		if app != nil {
			return m.interruptAppServer(ctx, id, app)
		}
		item, err := m.store.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		if item.Status == taskmodel.StatusRunning || item.Status == taskmodel.StatusPending {
			now := time.Now()
			item.Status = taskmodel.StatusInterrupted
			item.FinishedAt = &now
			if err := m.store.Update(ctx, item); err != nil {
				return nil, err
			}
			return item, nil
		}
		return nil, fmt.Errorf("task not running")
	}
	interruptCtx, cancel := context.WithTimeout(ctx, interruptTimeout)
	defer cancel()
	if err := proc.Interrupt(interruptCtx); err != nil {
		return nil, err
	}
	item, err := m.store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if item.Status == taskmodel.StatusRunning || item.Status == taskmodel.StatusPending {
		now := time.Now()
		item.Status = taskmodel.StatusInterrupted
		item.FinishedAt = &now
		if err := m.store.Update(ctx, item); err != nil {
			return nil, err
		}
	}
	return item, nil
}

func (m *Manager) DeleteTask(ctx context.Context, id string) error {
	m.mu.Lock()
	if _, ok := m.running[id]; ok {
		m.mu.Unlock()
		return fmt.Errorf("task is running")
	}
	if _, ok := m.apps[id]; ok {
		m.mu.Unlock()
		return fmt.Errorf("task is running")
	}
	m.mu.Unlock()
	item, err := m.store.Get(ctx, id)
	if err != nil {
		return err
	}
	if err := m.store.Remove(ctx, id); err != nil {
		return err
	}
	m.mu.Lock()
	m.removeLabelIndex(item.ID, item.Labels)
	m.mu.Unlock()
	return nil
}

func (m *Manager) SubscribeLogs(ctx context.Context, id string, fromSeq int64) (<-chan taskmodel.LogEntry, []taskmodel.LogEntry, func()) {
	return m.logHub.Subscribe(id, fromSeq)
}

func (m *Manager) SubscribeTerminal(id string, fromCursor int64) (<-chan []byte, []byte, func()) {
	ch, backlog, unsubscribe := m.terminalHub.Subscribe(id, fromCursor)
	m.sendPendingInput(id)
	return ch, backlog, unsubscribe
}

func (m *Manager) SendInput(ctx context.Context, id string, payload []byte) error {
	m.mu.Lock()
	proc := m.running[id]
	app := m.apps[id]
	m.mu.Unlock()
	if proc == nil {
		if app != nil {
			return fmt.Errorf("task protocol does not support terminal input")
		}
		return fmt.Errorf("task not running")
	}
	_, err := proc.Write(payload)
	return err
}

func (m *Manager) SendChat(ctx context.Context, id string, message string) error {
	m.mu.Lock()
	proc := m.running[id]
	app := m.apps[id]
	m.mu.Unlock()
	if app != nil {
		return app.SendMessage(ctx, message)
	}
	if proc == nil {
		return fmt.Errorf("task not running")
	}
	payload := []byte(strings.TrimRight(message, "\n") + "\n")
	_, err := proc.Write(payload)
	return err
}

func (m *Manager) ResizeTerminal(ctx context.Context, id string, cols int, rows int) error {
	m.mu.Lock()
	proc := m.running[id]
	app := m.apps[id]
	m.mu.Unlock()
	if proc == nil {
		if app != nil {
			return fmt.Errorf("task protocol does not support terminal resize")
		}
		return fmt.Errorf("task not running")
	}
	return proc.Resize(cols, rows)
}

func (m *Manager) sendPendingInput(id string) {
	m.mu.Lock()
	payload := m.pendingInputs[id]
	proc := m.running[id]
	delete(m.pendingInputs, id)
	m.mu.Unlock()
	if len(payload) == 0 || proc == nil {
		return
	}
	go func(p *Process, data []byte) {
		if !waitForTerminalActivity(m.terminalHub, id, 15*time.Second) {
			slog.Warn("task pending input sent without terminal activity", "task_id", id)
		}
		time.Sleep(200 * time.Millisecond)
		if _, err := p.Write(data); err != nil {
			slog.Warn("task pending input failed", "task_id", id, "error", err)
			return
		}
		if err := WaitAndSendEnter(func() time.Time {
			return m.terminalHub.LastActivity(id)
		}, func(input []byte) error {
			_, err := p.Write(input)
			return err
		}, 30*time.Second, 300*time.Millisecond); err != nil {
			slog.Warn("task pending enter failed", "task_id", id, "error", err)
			return
		}
		slog.Info("task pending input sent", "task_id", id, "bytes", len(data))
	}(proc, payload)
}

func waitForTerminalActivity(hub *TerminalHub, taskID string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if !hub.LastActivity(taskID).IsZero() {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func (m *Manager) CleanupExpired(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cutoff := time.Now().Add(-terminalInactivityTTL)
			m.terminalHub.PruneInactive(cutoff)
			ids, err := m.store.ListExpired(ctx, time.Now(), 200)
			if err != nil {
				continue
			}
			for _, id := range ids {
				item, _ := m.store.Get(ctx, id)
				_ = m.store.Remove(ctx, id)
				if item != nil {
					m.mu.Lock()
					m.removeLabelIndex(item.ID, item.Labels)
					m.mu.Unlock()
					m.terminalHub.Cleanup(item.ID)
				}
			}
		}
	}
}

func (m *Manager) runTask(ctx context.Context, t *taskmodel.Task, defaultEnv map[string]string) {
	started := time.Now()
	t.Status = taskmodel.StatusRunning
	t.StartedAt = &started
	if err := m.store.Update(ctx, t); err != nil {
		return
	}

	proc, err := StartProcess(ExecSpec{
		Command: t.Command,
		Args:    t.Args,
		Env:     BuildEnv(defaultEnv, t.Env),
		WorkDir: t.WorkDir,
	})
	if err != nil {
		m.markFailure(ctx, t, err)
		return
	}
	m.mu.Lock()
	m.running[t.ID] = proc
	m.mu.Unlock()

	go m.consumePTY(ctx, t.ID, proc, proc.OutputReader())

	waitErr := proc.Wait()
	m.mu.Lock()
	delete(m.running, t.ID)
	m.mu.Unlock()

	if waitErr != nil {
		if errors.Is(waitErr, context.Canceled) {
			m.markInterrupted(ctx, t)
			m.scheduleTerminalCleanup(t.ID)
			return
		}
		m.markExit(ctx, t, waitErr)
		m.scheduleTerminalCleanup(t.ID)
		return
	}
	m.markSuccess(ctx, t)
	m.scheduleTerminalCleanup(t.ID)
}

func (m *Manager) consumePTY(ctx context.Context, taskID string, proc *Process, reader io.Reader) {
	buf := make([]byte, 4096)
	var lineBuf strings.Builder
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			replyCursorPosition(proc, chunk)
			m.terminalHub.Append(taskID, chunk)
			for _, b := range chunk {
				if b == '\n' {
					line := SanitizeLogLine(lineBuf.String())
					lineBuf.Reset()
					if line == "" {
						continue
					}
					entry := taskmodel.LogEntry{
						TaskID: taskID,
						Seq:    -1,
						TS:     time.Now(),
						Stream: "stdout",
						Line:   line,
					}
					entry = m.logHub.Append(taskID, entry)
					_ = m.store.AppendLog(ctx, entry, m.retention)
				} else if b == '\r' {
					lineBuf.Reset()
				} else {
					lineBuf.WriteByte(b)
				}
			}
		}
		if err != nil {
			if lineBuf.Len() > 0 {
				entry := taskmodel.LogEntry{
					TaskID: taskID,
					Seq:    -1,
					TS:     time.Now(),
					Stream: "stdout",
					Line:   SanitizeLogLine(lineBuf.String()),
				}
				entry = m.logHub.Append(taskID, entry)
				_ = m.store.AppendLog(ctx, entry, m.retention)
			}
			return
		}
	}
}

func replyCursorPosition(proc *Process, chunk []byte) {
	if proc == nil {
		return
	}
	if bytes.Contains(chunk, []byte("\x1b[6n")) || bytes.Contains(chunk, []byte("\x1b[?6n")) {
		_, _ = proc.Write([]byte("\x1b[1;1R"))
	}
}

func (m *Manager) scheduleTerminalCleanup(taskID string) {
	if terminalCleanupDelay <= 0 {
		m.terminalHub.Cleanup(taskID)
		return
	}
	go func() {
		time.Sleep(terminalCleanupDelay)
		m.terminalHub.Cleanup(taskID)
	}()
}

func (m *Manager) markSuccess(ctx context.Context, t *taskmodel.Task) {
	now := time.Now()
	exit := 0
	t.Status = taskmodel.StatusCompleted
	t.FinishedAt = &now
	t.ExitCode = &exit
	_ = m.store.Update(ctx, t)
}

func (m *Manager) markInterrupted(ctx context.Context, t *taskmodel.Task) {
	now := time.Now()
	t.Status = taskmodel.StatusInterrupted
	t.FinishedAt = &now
	_ = m.store.Update(ctx, t)
}

func (m *Manager) markExit(ctx context.Context, t *taskmodel.Task, err error) {
	now := time.Now()
	t.Status = taskmodel.StatusFailed
	t.FinishedAt = &now
	if code := ExitCode(err); code != nil {
		t.ExitCode = code
	}
	t.Error = err.Error()
	_ = m.store.Update(ctx, t)
}

func (m *Manager) markFailure(ctx context.Context, t *taskmodel.Task, err error) {
	now := time.Now()
	t.Status = taskmodel.StatusFailed
	t.FinishedAt = &now
	t.Error = err.Error()
	_ = m.store.Update(ctx, t)
}

func (m *Manager) allowedBinary(cmd string) bool {
	if len(m.allowedBinaries) == 0 {
		return true
	}
	base := filepath.Base(cmd)
	_, ok := m.allowedBinaries[base]
	return ok
}

func sanitizeLabels(labels map[string]string) map[string]string {
	clean := map[string]string{}
	for key, value := range labels {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		clean[key] = value
	}
	return clean
}

func sanitizeEnv(env map[string]string) map[string]string {
	clean := map[string]string{}
	for key, value := range env {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		clean[key] = value
	}
	return clean
}

func (m *Manager) addLabelIndex(id string, labels map[string]string) {
	for key, value := range labels {
		valueMap, ok := m.labels[key]
		if !ok {
			valueMap = map[string]map[string]struct{}{}
			m.labels[key] = valueMap
		}
		set, ok := valueMap[value]
		if !ok {
			set = map[string]struct{}{}
			valueMap[value] = set
		}
		set[id] = struct{}{}
	}
}

func (m *Manager) removeLabelIndex(id string, labels map[string]string) {
	for key, value := range labels {
		valueMap, ok := m.labels[key]
		if !ok {
			continue
		}
		set, ok := valueMap[value]
		if !ok {
			continue
		}
		delete(set, id)
		if len(set) == 0 {
			delete(valueMap, value)
		}
		if len(valueMap) == 0 {
			delete(m.labels, key)
		}
	}
}

func (m *Manager) matchLabels(labels map[string]string) map[string]struct{} {
	m.mu.Lock()
	defer m.mu.Unlock()
	var matched map[string]struct{}
	for key, value := range labels {
		valueMap, ok := m.labels[key]
		if !ok {
			return map[string]struct{}{}
		}
		set, ok := valueMap[value]
		if !ok {
			return map[string]struct{}{}
		}
		if matched == nil {
			matched = map[string]struct{}{}
			for id := range set {
				matched[id] = struct{}{}
			}
			continue
		}
		for id := range matched {
			if _, ok := set[id]; !ok {
				delete(matched, id)
			}
		}
	}
	if matched == nil {
		return map[string]struct{}{}
	}
	return matched
}

func paginate(items []*taskmodel.Task, limit, offset int) []*taskmodel.Task {
	if offset < 0 {
		offset = 0
	}
	if offset >= len(items) {
		return []*taskmodel.Task{}
	}
	if limit <= 0 {
		return items[offset:]
	}
	end := offset + limit
	if end > len(items) {
		end = len(items)
	}
	return items[offset:end]
}
