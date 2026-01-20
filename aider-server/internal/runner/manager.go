package runner

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"aider-server/internal/runnerpb"
	"aider-server/internal/store"
	"aider-server/internal/task"
	"aider-server/internal/task/adapters"
	"aider-server/internal/taskmodel"

	"github.com/google/uuid"
)

type Manager struct {
	store                store.Store
	logHub               *task.LogHub
	terminalHub          *task.TerminalHub
	registry             *adapters.Registry
	allowedBinaries      map[string]struct{}
	dataDir              string
	retention            time.Duration
	logBackfillBatchSize int64
	authToken            string
	launchCommand        string
	launchArgs           []string
	launchTimeout        time.Duration
	launchDetach         bool
	serverAddr           string
	configPath           string
	codexDefaultProtocol string

	mu            sync.Mutex
	runners       map[string]*connection
	runnerIDs     []string
	runnerIdx     int
	runnerReady   map[string]chan struct{}
	taskRunner    map[string]string
	lastSeq       map[string]int64
	pendingInputs map[string][]byte
	cleanupDelay  time.Duration
	inactivityTTL time.Duration
}

func NewManager(store store.Store, registry *adapters.Registry, allowed []string, dataDir string, retention time.Duration, logBackfillBatchSize int, authToken string, launchCommand string, launchArgs []string, launchTimeoutSeconds int, launchDetach bool, serverAddr string, configPath string, codexDefaultProtocol string) *Manager {
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
	if serverAddr == "" {
		serverAddr = "127.0.0.1:8099"
	}
	return &Manager{
		store:                store,
		logHub:               task.NewLogHub(400),
		terminalHub:          task.NewTerminalHub(16 * 1024),
		registry:             registry,
		allowedBinaries:      allowedSet,
		dataDir:              dataDir,
		retention:            retention,
		logBackfillBatchSize: int64(logBackfillBatchSize),
		authToken:            strings.TrimSpace(authToken),
		launchCommand:        strings.TrimSpace(launchCommand),
		launchArgs:           append([]string(nil), launchArgs...),
		launchTimeout:        time.Duration(launchTimeoutSeconds) * time.Second,
		launchDetach:         launchDetach,
		serverAddr:           strings.TrimSpace(serverAddr),
		configPath:           strings.TrimSpace(configPath),
		codexDefaultProtocol: strings.TrimSpace(codexDefaultProtocol),
		runners:              map[string]*connection{},
		runnerReady:          map[string]chan struct{}{},
		taskRunner:           map[string]string{},
		lastSeq:              map[string]int64{},
		pendingInputs:        map[string][]byte{},
		cleanupDelay:         5 * time.Minute,
		inactivityTTL:        30 * time.Minute,
	}
}

func (m *Manager) CreateTask(ctx context.Context, req taskmodel.CreateRequest, defaultEnv map[string]string) (*taskmodel.Task, error) {
	if strings.TrimSpace(req.Client) == "" {
		return nil, fmt.Errorf("client is required")
	}
	protocol, err := task.ResolveProtocol(req.Client, req.Protocol, m.codexDefaultProtocol)
	if err != nil {
		return nil, err
	}
	if protocol == taskmodel.ProtocolAppServer {
		return nil, fmt.Errorf("app-server protocol is not supported in runner mode")
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
	if prompt != "" {
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
	slog.Info("runner task command prepared",
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
	id := uuid.NewString()
	var (
		runnerID string
		conn     *connection
	)
	if m.launchCommand != "" {
		runnerID = "runner-" + id
		if err := m.launchRunner(ctx, runnerID); err != nil {
			return nil, err
		}
		conn = m.waitRunner(ctx, runnerID)
		if conn == nil {
			return nil, fmt.Errorf("runner not connected")
		}
	} else {
		runnerID, conn = m.pickRunner()
		if conn == nil {
			return nil, fmt.Errorf("no runner connected")
		}
	}
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
		RunnerID:  runnerID,
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
	m.taskRunner[t.ID] = runnerID
	if req.Interactive {
		if payload := task.BuildPromptPayload(prompt); len(payload) > 0 {
			m.pendingInputs[t.ID] = payload
		}
	}
	m.mu.Unlock()

	command := &runnerpb.StartTaskCommand{
		Task: &runnerpb.TaskSpec{
			TaskId:  t.ID,
			Command: t.Command,
			Args:    t.Args,
			Env:     taskEnv(defaultEnv, t.Env),
			WorkDir: t.WorkDir,
			Labels:  t.Labels,
		},
	}
	if err := conn.send(ctx, &runnerpb.ServerMessage{Msg: &runnerpb.ServerMessage_Start{Start: command}}); err != nil {
		now := time.Now()
		t.Status = taskmodel.StatusFailed
		t.FinishedAt = &now
		t.Error = err.Error()
		_ = m.store.Update(ctx, t)
		return nil, err
	}
	return t, nil
}

func (m *Manager) GetTask(ctx context.Context, id string) (*taskmodel.Task, error) {
	return m.store.Get(ctx, id)
}

func (m *Manager) ListTasks(ctx context.Context, filter taskmodel.ListFilter) ([]*taskmodel.Task, int, error) {
	return m.store.List(ctx, filter)
}

func (m *Manager) Interrupt(ctx context.Context, id string) (*taskmodel.Task, error) {
	item, err := m.store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	conn := m.runnerForTask(item)
	if conn == nil {
		return nil, fmt.Errorf("runner not available")
	}
	command := &runnerpb.InterruptTaskCommand{
		TaskId: id,
		Signal: 2,
	}
	if err := conn.send(ctx, &runnerpb.ServerMessage{Msg: &runnerpb.ServerMessage_Interrupt{Interrupt: command}}); err != nil {
		return nil, err
	}
	return item, nil
}

func (m *Manager) DeleteTask(ctx context.Context, id string) error {
	item, err := m.store.Get(ctx, id)
	if err != nil {
		return err
	}
	return m.store.Remove(ctx, item.ID)
}

func (m *Manager) SubscribeLogs(ctx context.Context, id string, fromSeq int64) (<-chan taskmodel.LogEntry, []taskmodel.LogEntry, func()) {
	ch, backlog, unsubscribe := m.logHub.Subscribe(id, fromSeq)
	if len(backlog) == 0 || backlog[0].Seq > fromSeq {
		m.requestBackfill(ctx, id, fromSeq)
	}
	return ch, backlog, unsubscribe
}

func (m *Manager) SubscribeTerminal(id string, fromCursor int64) (<-chan []byte, []byte, func()) {
	ch, backlog, unsubscribe := m.terminalHub.Subscribe(id, fromCursor)
	m.sendPendingInput(context.Background(), id)
	return ch, backlog, unsubscribe
}

func (m *Manager) SendInput(ctx context.Context, id string, payload []byte) error {
	item, err := m.store.Get(ctx, id)
	if err != nil {
		return err
	}
	conn := m.runnerForTask(item)
	if conn == nil {
		return fmt.Errorf("runner not available")
	}
	command := &runnerpb.SendInputCommand{
		TaskId: id,
		Input:  payload,
	}
	return conn.send(ctx, &runnerpb.ServerMessage{Msg: &runnerpb.ServerMessage_Input{Input: command}})
}

func (m *Manager) SendChat(ctx context.Context, id string, message string) error {
	item, err := m.store.Get(ctx, id)
	if err != nil {
		return err
	}
	if item.Protocol == taskmodel.ProtocolAppServer {
		return fmt.Errorf("app-server protocol is not supported in runner mode")
	}
	conn := m.runnerForTask(item)
	if conn == nil {
		return fmt.Errorf("runner not available")
	}
	payload := []byte(strings.TrimRight(message, "\n") + "\n")
	command := &runnerpb.SendInputCommand{
		TaskId: id,
		Input:  payload,
	}
	return conn.send(ctx, &runnerpb.ServerMessage{Msg: &runnerpb.ServerMessage_Input{Input: command}})
}

func (m *Manager) ResizeTerminal(ctx context.Context, id string, cols int, rows int) error {
	item, err := m.store.Get(ctx, id)
	if err != nil {
		return err
	}
	conn := m.runnerForTask(item)
	if conn == nil {
		return fmt.Errorf("runner not available")
	}
	command := &runnerpb.ResizeTerminalCommand{
		TaskId: id,
		Cols:   int32(cols),
		Rows:   int32(rows),
	}
	return conn.send(ctx, &runnerpb.ServerMessage{Msg: &runnerpb.ServerMessage_Resize{Resize: command}})
}

func (m *Manager) RebuildIndexes(ctx context.Context) error {
	items, _, err := m.store.List(ctx, taskmodel.ListFilter{})
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.taskRunner = map[string]string{}
	for _, t := range items {
		if t.RunnerID != "" {
			m.taskRunner[t.ID] = t.RunnerID
		}
	}
	m.mu.Unlock()
	return nil
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
			cutoff := time.Now().Add(-m.inactivityTTL)
			m.terminalHub.PruneInactive(cutoff)
			ids, err := m.store.ListExpired(ctx, time.Now(), 200)
			if err != nil {
				continue
			}
			for _, id := range ids {
				_ = m.store.Remove(ctx, id)
				m.terminalHub.Cleanup(id)
			}
		}
	}
}

func (m *Manager) RegisterRunner(id string, stream runnerpb.RunnerControl_ConnectServer) *connection {
	conn := newConnection(id, stream)
	m.mu.Lock()
	if existing, ok := m.runners[id]; ok {
		existing.close()
	}
	m.runners[id] = conn
	m.ensureRunnerID(id)
	if ready, ok := m.runnerReady[id]; ok {
		close(ready)
		delete(m.runnerReady, id)
	}
	m.mu.Unlock()
	go conn.runSendLoop()
	return conn
}

func (m *Manager) UnregisterRunner(id string) {
	m.mu.Lock()
	conn, ok := m.runners[id]
	if ok {
		delete(m.runners, id)
		m.dropRunnerID(id)
	}
	m.mu.Unlock()
	if ok {
		conn.close()
	}
}

func (m *Manager) HandleHello(ctx context.Context, id string, hello *runnerpb.RunnerHello) {
	m.mu.Lock()
	for taskID, seq := range hello.LastSeqByTask {
		if seq > m.lastSeq[taskID] {
			m.lastSeq[taskID] = seq
		}
	}
	m.mu.Unlock()
	m.reconcileRunnerTasks(ctx, id, hello.Tasks)
}

func (m *Manager) HandleRunnerMessage(ctx context.Context, runnerID string, msg *runnerpb.RunnerMessage) {
	switch payload := msg.Msg.(type) {
	case *runnerpb.RunnerMessage_Status:
		m.handleStatus(ctx, runnerID, payload.Status)
	case *runnerpb.RunnerMessage_Logs:
		m.handleLogs(ctx, runnerID, payload.Logs)
	case *runnerpb.RunnerMessage_Terminal:
		m.handleTerminal(payload.Terminal)
	case *runnerpb.RunnerMessage_Heartbeat:
		return
	}
}

func (m *Manager) handleStatus(ctx context.Context, runnerID string, status *runnerpb.TaskStatusEvent) {
	if status == nil {
		return
	}
	item, err := m.store.Get(ctx, status.TaskId)
	if err != nil {
		return
	}
	now := time.Now()
	item.RunnerID = runnerID
	switch status.Status {
	case runnerpb.TaskStatus_TASK_STATUS_RUNNING:
		item.Status = taskmodel.StatusRunning
		item.StartedAt = &now
	case runnerpb.TaskStatus_TASK_STATUS_COMPLETED:
		item.Status = taskmodel.StatusCompleted
		item.FinishedAt = &now
		code := int(status.ExitCode)
		item.ExitCode = &code
		m.scheduleTerminalCleanup(item.ID)
	case runnerpb.TaskStatus_TASK_STATUS_INTERRUPTED:
		item.Status = taskmodel.StatusInterrupted
		item.FinishedAt = &now
		m.scheduleTerminalCleanup(item.ID)
	case runnerpb.TaskStatus_TASK_STATUS_FAILED:
		item.Status = taskmodel.StatusFailed
		item.FinishedAt = &now
		if status.ExitCode != 0 {
			code := int(status.ExitCode)
			item.ExitCode = &code
		}
		item.Error = status.Error
		m.scheduleTerminalCleanup(item.ID)
	}
	_ = m.store.Update(ctx, item)
}

func (m *Manager) sendPendingInput(ctx context.Context, taskID string) {
	m.mu.Lock()
	payload := m.pendingInputs[taskID]
	delete(m.pendingInputs, taskID)
	m.mu.Unlock()
	if len(payload) == 0 {
		return
	}
	go func(data []byte) {
		time.Sleep(200 * time.Millisecond)
		if err := m.SendInput(context.Background(), taskID, data); err != nil {
			slog.Warn("runner pending input failed", "task_id", taskID, "error", err)
			return
		}
		if err := task.WaitAndSendEnter(func() time.Time {
			return m.terminalHub.LastActivity(taskID)
		}, func(data []byte) error {
			return m.SendInput(context.Background(), taskID, data)
		}, 30*time.Second, 300*time.Millisecond); err != nil {
			slog.Warn("runner pending enter failed", "task_id", taskID, "error", err)
			return
		}
		slog.Info("runner pending input sent", "task_id", taskID, "bytes", len(data))
	}(payload)
}

func (m *Manager) handleLogs(ctx context.Context, runnerID string, batch *runnerpb.LogBatch) {
	if batch == nil || batch.TaskId == "" || len(batch.Lines) == 0 {
		return
	}
	expected := m.lastSeqFor(batch.TaskId) + 1
	if batch.SeqStart > expected {
		m.requestBackfill(ctx, batch.TaskId, expected)
	}
	last := m.lastSeqFor(batch.TaskId)
	for i, line := range batch.Lines {
		seq := batch.SeqStart + int64(i)
		if seq <= last {
			continue
		}
		entry := taskmodel.LogEntry{
			TaskID: batch.TaskId,
			Seq:    seq,
			TS:     time.UnixMilli(batch.TsUnixMs),
			Stream: "stdout",
			Line:   line,
		}
		m.logHub.Append(batch.TaskId, entry)
		last = seq
	}
	m.setLastSeq(batch.TaskId, last)
}

func (m *Manager) handleTerminal(output *runnerpb.TerminalOutput) {
	if output == nil || output.TaskId == "" || len(output.Data) == 0 {
		return
	}
	m.terminalHub.Append(output.TaskId, output.Data)
}

func (m *Manager) scheduleTerminalCleanup(taskID string) {
	if m.cleanupDelay <= 0 {
		m.terminalHub.Cleanup(taskID)
		return
	}
	go func() {
		time.Sleep(m.cleanupDelay)
		m.terminalHub.Cleanup(taskID)
	}()
}

func (m *Manager) lastSeqFor(taskID string) int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastSeq[taskID]
}

func (m *Manager) setLastSeq(taskID string, seq int64) {
	m.mu.Lock()
	m.lastSeq[taskID] = seq
	m.mu.Unlock()
}

func (m *Manager) requestBackfill(ctx context.Context, taskID string, fromSeq int64) {
	item, err := m.store.Get(ctx, taskID)
	if err != nil {
		return
	}
	conn := m.runnerForTask(item)
	if conn == nil {
		return
	}
	limit := m.logBackfillBatchSize
	if limit <= 0 {
		limit = 200
	}
	req := &runnerpb.LogBackfillRequest{
		TaskId:  taskID,
		FromSeq: fromSeq,
		Limit:   limit,
	}
	_ = conn.send(ctx, &runnerpb.ServerMessage{Msg: &runnerpb.ServerMessage_Backfill{Backfill: req}})
}

func (m *Manager) reconcileRunnerTasks(ctx context.Context, runnerID string, tasks []*runnerpb.TaskBrief) {
	current := map[string]runnerpb.TaskStatus{}
	for _, item := range tasks {
		current[item.TaskId] = item.Status
	}
	items, _, err := m.store.List(ctx, taskmodel.ListFilter{})
	if err != nil {
		return
	}
	for _, item := range items {
		if item.RunnerID != runnerID {
			continue
		}
		if item.Status != taskmodel.StatusRunning && item.Status != taskmodel.StatusPending {
			continue
		}
		if status, ok := current[item.ID]; ok {
			m.handleStatus(ctx, runnerID, &runnerpb.TaskStatusEvent{
				TaskId:   item.ID,
				Status:   status,
				TsUnixMs: time.Now().UnixMilli(),
			})
			continue
		}
		now := time.Now()
		item.Status = taskmodel.StatusFailed
		item.FinishedAt = &now
		item.Error = "runner missing task after reconnect"
		_ = m.store.Update(ctx, item)
	}
}

func (m *Manager) pickRunner() (string, *connection) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.runnerIDs) == 0 {
		return "", nil
	}
	if m.runnerIdx >= len(m.runnerIDs) {
		m.runnerIdx = 0
	}
	id := m.runnerIDs[m.runnerIdx]
	m.runnerIdx++
	return id, m.runners[id]
}

func (m *Manager) runnerForTask(task *taskmodel.Task) *connection {
	if task == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if task.RunnerID != "" {
		return m.runners[task.RunnerID]
	}
	if runnerID, ok := m.taskRunner[task.ID]; ok {
		return m.runners[runnerID]
	}
	return nil
}

func (m *Manager) ensureRunnerID(id string) {
	for _, existing := range m.runnerIDs {
		if existing == id {
			return
		}
	}
	m.runnerIDs = append(m.runnerIDs, id)
}

func (m *Manager) dropRunnerID(id string) {
	for i, existing := range m.runnerIDs {
		if existing == id {
			m.runnerIDs = append(m.runnerIDs[:i], m.runnerIDs[i+1:]...)
			if m.runnerIdx > i {
				m.runnerIdx--
			}
			return
		}
	}
}

func (m *Manager) launchRunner(ctx context.Context, runnerID string) error {
	command := m.launchCommand
	if command == "" {
		var err error
		command, err = defaultLaunchCommand()
		if err != nil {
			return err
		}
	}
	if err := validateLaunch(command); err != nil {
		return err
	}
	args := m.launchArgs
	if len(args) == 0 {
		args = []string{"--runner-daemon", "--config", "{config_path}", "--runner-server", "{server_addr}", "--runner-id", "{runner_id}"}
	}
	args = expandLaunchArgs(args, map[string]string{
		"runner_id":   runnerID,
		"server_addr": m.serverAddr,
		"config_path": m.configPath,
	})
	ready := make(chan struct{})
	m.mu.Lock()
	if _, ok := m.runnerReady[runnerID]; !ok {
		m.runnerReady[runnerID] = ready
	} else {
		ready = m.runnerReady[runnerID]
	}
	m.mu.Unlock()
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if m.launchDetach && runtime.GOOS != "windows" {
		cmd.SysProcAttr = &syscall.SysProcAttr{
			Setsid: true,
		}
	}
	if err := cmd.Start(); err != nil {
		m.mu.Lock()
		delete(m.runnerReady, runnerID)
		m.mu.Unlock()
		return err
	}
	go func() {
		_ = cmd.Wait()
		m.mu.Lock()
		delete(m.runnerReady, runnerID)
		m.mu.Unlock()
	}()
	return nil
}

func (m *Manager) waitRunner(ctx context.Context, runnerID string) *connection {
	timeout := m.launchTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	m.mu.Lock()
	ready, ok := m.runnerReady[runnerID]
	conn := m.runners[runnerID]
	m.mu.Unlock()
	if conn != nil {
		return conn
	}
	if !ok {
		return nil
	}
	select {
	case <-ctx.Done():
		return nil
	case <-time.After(timeout):
		return nil
	case <-ready:
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.runners[runnerID]
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

func taskEnv(defaults, overrides map[string]string) map[string]string {
	merged := map[string]string{}
	for k, v := range defaults {
		merged[k] = v
	}
	for k, v := range overrides {
		merged[k] = v
	}
	if _, ok := merged["PATH"]; !ok {
		merged["PATH"] = "/usr/local/bin:/usr/bin:/bin"
	}
	if _, ok := merged["TERM"]; !ok {
		merged["TERM"] = "xterm-256color"
	}
	return merged
}
