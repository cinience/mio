package runnerclient

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"aider-server/internal/config"
	"aider-server/internal/runnerpb"
	"aider-server/internal/task"
	"aider-server/internal/taskmodel"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type Runner struct {
	cfg       config.RunnerConfig
	dataDir   string
	sendCh    chan *runnerpb.RunnerMessage
	mu        sync.Mutex
	tasks     map[string]*taskState
	logStore  *logStore
	startedAt time.Time
	quitCh    chan struct{}
	quitOnce  sync.Once
}

type taskState struct {
	spec       *runnerpb.TaskSpec
	proc       *task.Process
	seq        int64
	lineCh     chan logLine
	mu         sync.Mutex
	pausedLogs bool
}

type logLine struct {
	seq int64
	ts  time.Time
	msg string
}

func NewRunner(cfg config.RunnerConfig, dataDir string) (*Runner, error) {
	logStore, err := newLogStore(cfg.LogDir, cfg.LogMaxBytes, cfg.LogMaxBackups)
	if err != nil {
		return nil, err
	}
	if cfg.RunnerID == "" {
		hostname, _ := os.Hostname()
		cfg.RunnerID = fmt.Sprintf("%s-%d", hostname, os.Getpid())
	}
	return &Runner{
		cfg:       cfg,
		dataDir:   dataDir,
		sendCh:    make(chan *runnerpb.RunnerMessage, 128),
		tasks:     map[string]*taskState{},
		logStore:  logStore,
		startedAt: time.Now(),
		quitCh:    make(chan struct{}),
	}, nil
}

func (r *Runner) Run(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-r.quitCh:
			return nil
		default:
		}
		if err := r.connectOnce(ctx); err != nil {
			slog.Error("runner connection failed", "error", err)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(2 * time.Second):
			}
		}
	}
}

func (r *Runner) connectOnce(ctx context.Context) error {
	timeout := time.Duration(r.cfg.DialTimeoutSeconds) * time.Second
	dialCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	}
	if strings.TrimSpace(r.cfg.AuthToken) != "" {
		opts = append(opts, grpc.WithPerRPCCredentials(tokenCreds{token: r.cfg.AuthToken}))
	}
	conn, err := grpc.DialContext(dialCtx, r.cfg.ServerAddr, opts...)
	if err != nil {
		return err
	}
	defer conn.Close()

	client := runnerpb.NewRunnerControlClient(conn)
	stream, err := client.Connect(ctx)
	if err != nil {
		return err
	}
	hello := &runnerpb.RunnerMessage{
		Msg: &runnerpb.RunnerMessage_Hello{
			Hello: r.buildHello(),
		},
	}
	if err := stream.Send(hello); err != nil {
		return err
	}

	sendDone := make(chan error, 1)
	recvDone := make(chan error, 1)

	go func() {
		sendDone <- r.sendLoop(ctx, stream)
	}()
	go func() {
		recvDone <- r.recvLoop(ctx, stream)
	}()

	heartbeatTicker := time.NewTicker(time.Duration(r.cfg.HeartbeatSeconds) * time.Second)
	defer heartbeatTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-sendDone:
			return err
		case err := <-recvDone:
			return err
		case <-heartbeatTicker.C:
			r.enqueue(&runnerpb.RunnerMessage{
				Msg: &runnerpb.RunnerMessage_Heartbeat{
					Heartbeat: &runnerpb.Heartbeat{
						TsUnixMs: time.Now().UnixMilli(),
					},
				},
			})
		}
	}
}

func (r *Runner) buildHello() *runnerpb.RunnerHello {
	r.mu.Lock()
	defer r.mu.Unlock()
	tasks := make([]*runnerpb.TaskBrief, 0, len(r.tasks))
	lastSeq := map[string]int64{}
	for id, state := range r.tasks {
		tasks = append(tasks, &runnerpb.TaskBrief{
			TaskId: id,
			Status: runnerpb.TaskStatus_TASK_STATUS_RUNNING,
		})
		lastSeq[id] = state.seq
	}
	return &runnerpb.RunnerHello{
		RunnerId:            r.cfg.RunnerID,
		Version:             "v1",
		Tasks:               tasks,
		LastSeqByTask:       lastSeq,
		HeartbeatIntervalMs: int64(time.Duration(r.cfg.HeartbeatSeconds) * time.Second / time.Millisecond),
	}
}

func (r *Runner) sendLoop(ctx context.Context, stream runnerpb.RunnerControl_ConnectClient) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case msg := <-r.sendCh:
			if msg == nil {
				continue
			}
			if err := stream.Send(msg); err != nil {
				return err
			}
		}
	}
}

func (r *Runner) recvLoop(ctx context.Context, stream runnerpb.RunnerControl_ConnectClient) error {
	for {
		msg, err := stream.Recv()
		if err != nil {
			return err
		}
		switch payload := msg.Msg.(type) {
		case *runnerpb.ServerMessage_Start:
			r.handleStart(ctx, payload.Start)
		case *runnerpb.ServerMessage_Interrupt:
			r.handleInterrupt(ctx, payload.Interrupt)
		case *runnerpb.ServerMessage_Input:
			r.handleInput(ctx, payload.Input)
		case *runnerpb.ServerMessage_Resize:
			r.handleResize(payload.Resize)
		case *runnerpb.ServerMessage_Flow:
			r.handleFlow(payload.Flow)
		case *runnerpb.ServerMessage_Backfill:
			r.handleBackfill(payload.Backfill)
		}
	}
}

func (r *Runner) handleStart(ctx context.Context, cmd *runnerpb.StartTaskCommand) {
	if cmd == nil || cmd.Task == nil {
		return
	}
	spec := cmd.Task
	workDir := spec.WorkDir
	if workDir == "" {
		workDir = filepath.Join(r.dataDir, "tasks", spec.TaskId)
	}
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		r.emitStatus(spec.TaskId, runnerpb.TaskStatus_TASK_STATUS_FAILED, err, 0)
		return
	}
	proc, err := task.StartProcess(task.ExecSpec{
		Command: spec.Command,
		Args:    spec.Args,
		Env:     mapEnv(spec.Env),
		WorkDir: workDir,
	})
	if err != nil {
		r.emitStatus(spec.TaskId, runnerpb.TaskStatus_TASK_STATUS_FAILED, err, 0)
		return
	}
	state := &taskState{
		spec:   spec,
		proc:   proc,
		lineCh: make(chan logLine, 128),
	}
	r.mu.Lock()
	r.tasks[spec.TaskId] = state
	r.mu.Unlock()

	go r.runOutput(spec.TaskId, state)
	go r.runWait(spec.TaskId, state)
	go r.runLogSender(spec.TaskId, state)

	r.emitStatus(spec.TaskId, runnerpb.TaskStatus_TASK_STATUS_RUNNING, nil, 0)
}

func (r *Runner) handleInterrupt(ctx context.Context, cmd *runnerpb.InterruptTaskCommand) {
	if cmd == nil {
		return
	}
	state := r.taskFor(cmd.TaskId)
	if state == nil || state.proc == nil {
		return
	}
	timeoutCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_ = state.proc.Interrupt(timeoutCtx)
}

func (r *Runner) handleInput(ctx context.Context, cmd *runnerpb.SendInputCommand) {
	if cmd == nil {
		return
	}
	state := r.taskFor(cmd.TaskId)
	if state == nil || state.proc == nil {
		return
	}
	_, _ = state.proc.Write(cmd.Input)
}

func (r *Runner) handleResize(cmd *runnerpb.ResizeTerminalCommand) {
	if cmd == nil {
		return
	}
	state := r.taskFor(cmd.TaskId)
	if state == nil || state.proc == nil {
		return
	}
	_ = state.proc.Resize(int(cmd.Cols), int(cmd.Rows))
}

func (r *Runner) handleFlow(cmd *runnerpb.FlowControl) {
	if cmd == nil {
		return
	}
	state := r.taskFor(cmd.TaskId)
	if state == nil {
		return
	}
	state.setPaused(cmd.Pause)
}

func (r *Runner) handleBackfill(cmd *runnerpb.LogBackfillRequest) {
	if cmd == nil {
		return
	}
	entries, err := r.logStore.Load(cmd.TaskId, cmd.FromSeq, cmd.Limit)
	if err != nil {
		return
	}
	batchSize := r.cfg.LogBackfillBatchSize
	if batchSize <= 0 {
		batchSize = 200
	}
	var current []string
	var startSeq int64
	for _, entry := range entries {
		if len(current) == 0 {
			startSeq = entry.Seq
		}
		current = append(current, entry.Line)
		if len(current) >= batchSize {
			r.enqueue(&runnerpb.RunnerMessage{Msg: &runnerpb.RunnerMessage_Logs{
				Logs: &runnerpb.LogBatch{
					TaskId:   cmd.TaskId,
					SeqStart: startSeq,
					Lines:    current,
					TsUnixMs: time.Now().UnixMilli(),
				},
			}})
			current = nil
		}
	}
	if len(current) > 0 {
		r.enqueue(&runnerpb.RunnerMessage{Msg: &runnerpb.RunnerMessage_Logs{
			Logs: &runnerpb.LogBatch{
				TaskId:   cmd.TaskId,
				SeqStart: startSeq,
				Lines:    current,
				TsUnixMs: time.Now().UnixMilli(),
			},
		}})
	}
}

func (r *Runner) runOutput(taskID string, state *taskState) {
	reader := state.proc.OutputReader()
	buf := make([]byte, 4096)
	var lineBuf strings.Builder
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			r.replyCursorPosition(state, chunk)
			r.enqueue(&runnerpb.RunnerMessage{
				Msg: &runnerpb.RunnerMessage_Terminal{
					Terminal: &runnerpb.TerminalOutput{
						TaskId:   taskID,
						Data:     chunk,
						TsUnixMs: time.Now().UnixMilli(),
					},
				},
			})
			for _, b := range chunk {
				if b == '\n' {
					line := task.SanitizeLogLine(lineBuf.String())
					lineBuf.Reset()
					if line == "" {
						continue
					}
					r.appendLogLine(taskID, state, line)
				} else if b == '\r' {
					lineBuf.Reset()
				} else {
					lineBuf.WriteByte(b)
				}
			}
		}
		if err != nil {
			if lineBuf.Len() > 0 {
				line := task.SanitizeLogLine(lineBuf.String())
				r.appendLogLine(taskID, state, line)
			}
			return
		}
	}
}

func (r *Runner) replyCursorPosition(state *taskState, chunk []byte) {
	if state == nil || state.proc == nil {
		return
	}
	if bytes.Contains(chunk, []byte("\x1b[6n")) || bytes.Contains(chunk, []byte("\x1b[?6n")) {
		_, _ = state.proc.Write([]byte("\x1b[1;1R"))
	}
}

func (r *Runner) runWait(taskID string, state *taskState) {
	err := state.proc.Wait()
	exitCode := 0
	status := runnerpb.TaskStatus_TASK_STATUS_COMPLETED
	if err != nil {
		if code := task.ExitCode(err); code != nil {
			exitCode = *code
		}
		status = runnerpb.TaskStatus_TASK_STATUS_FAILED
	}
	r.emitStatus(taskID, status, err, exitCode)
	r.mu.Lock()
	delete(r.tasks, taskID)
	remaining := len(r.tasks)
	r.mu.Unlock()
	close(state.lineCh)
	if r.cfg.ExitOnComplete && remaining == 0 {
		r.quitOnce.Do(func() {
			close(r.quitCh)
		})
	}
}

func (r *Runner) runLogSender(taskID string, state *taskState) {
	batchSize := r.cfg.LogBatchSize
	if batchSize <= 0 {
		batchSize = 50
	}
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	var batch []string
	var startSeq int64
	flush := func() {
		if len(batch) == 0 || state.isPaused() {
			batch = nil
			return
		}
		r.enqueue(&runnerpb.RunnerMessage{
			Msg: &runnerpb.RunnerMessage_Logs{
				Logs: &runnerpb.LogBatch{
					TaskId:   taskID,
					SeqStart: startSeq,
					Lines:    batch,
					TsUnixMs: time.Now().UnixMilli(),
				},
			},
		})
		batch = nil
	}
	for {
		select {
		case line, ok := <-state.lineCh:
			if !ok {
				flush()
				return
			}
			if len(batch) == 0 {
				startSeq = line.seq
			}
			batch = append(batch, line.msg)
			if len(batch) >= batchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

func (r *Runner) appendLogLine(taskID string, state *taskState, line string) {
	state.seq++
	entry := taskmodel.LogEntry{
		TaskID: taskID,
		Seq:    state.seq,
		TS:     time.Now(),
		Stream: "stdout",
		Line:   line,
	}
	if err := r.logStore.Append(entry); err != nil {
		return
	}
	state.lineCh <- logLine{seq: entry.Seq, ts: entry.TS, msg: line}
}

func (r *Runner) emitStatus(taskID string, status runnerpb.TaskStatus, err error, exitCode int) {
	msg := &runnerpb.TaskStatusEvent{
		TaskId:   taskID,
		Status:   status,
		ExitCode: int32(exitCode),
		TsUnixMs: time.Now().UnixMilli(),
	}
	if err != nil {
		msg.Error = err.Error()
	}
	r.enqueue(&runnerpb.RunnerMessage{Msg: &runnerpb.RunnerMessage_Status{Status: msg}})
}

func (r *Runner) enqueue(msg *runnerpb.RunnerMessage) {
	select {
	case r.sendCh <- msg:
	default:
	}
}

func (r *Runner) taskFor(id string) *taskState {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.tasks[id]
}

func (t *taskState) setPaused(paused bool) {
	t.mu.Lock()
	t.pausedLogs = paused
	t.mu.Unlock()
}

func (t *taskState) isPaused() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.pausedLogs
}

func mapEnv(env map[string]string) []string {
	result := make([]string, 0, len(env))
	for key, value := range env {
		result = append(result, fmt.Sprintf("%s=%s", key, value))
	}
	return result
}

type tokenCreds struct {
	token string
}

func (t tokenCreds) GetRequestMetadata(ctx context.Context, uri ...string) (map[string]string, error) {
	if strings.TrimSpace(t.token) == "" {
		return nil, nil
	}
	return map[string]string{"authorization": "Bearer " + t.token}, nil
}

func (t tokenCreds) RequireTransportSecurity() bool {
	return false
}
