package task

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"aider-server/internal/taskmodel"
)

type CodexProtocolConfig struct {
	DefaultProtocol string
	AppServer       CodexAppServerConfig
}

type CodexAppServerConfig struct {
	Command string
	Args    []string
	Env     map[string]string
}

type appServerProcess struct {
	cmd          *exec.Cmd
	done         chan error
	cancel       context.CancelFunc
	session      *jsonRPCSession
	mu           sync.Mutex
	conversation string
	recentMu     sync.Mutex
	recent       []string
	recentLimit  int
	pid          int
	startedAt    time.Time
}

func (p *appServerProcess) setConversation(id string) {
	p.mu.Lock()
	p.conversation = id
	p.mu.Unlock()
}

func (p *appServerProcess) conversationID() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.conversation
}

func (p *appServerProcess) addRecent(line string) {
	if p == nil || strings.TrimSpace(line) == "" {
		return
	}
	p.recentMu.Lock()
	defer p.recentMu.Unlock()
	p.recent = append(p.recent, line)
	if limit := p.recentLimit; limit > 0 && len(p.recent) > limit {
		p.recent = append([]string(nil), p.recent[len(p.recent)-limit:]...)
	}
}

func (p *appServerProcess) recentSnapshot() []string {
	p.recentMu.Lock()
	defer p.recentMu.Unlock()
	return append([]string(nil), p.recent...)
}

func (p *appServerProcess) SendMessage(ctx context.Context, message string) error {
	if p == nil || p.session == nil {
		return fmt.Errorf("app-server not ready")
	}
	conversationID := p.conversationID()
	if conversationID == "" {
		return fmt.Errorf("app-server conversation not ready")
	}
	payload := strings.TrimSpace(message)
	if payload == "" {
		return nil
	}
	_, err := p.session.sendRequest(ctx, "sendUserMessage", wrapAppServerParams(map[string]any{
		"conversationId": conversationID,
		"items": []map[string]any{
			buildAppServerTextItem(payload),
		},
	}))
	return err
}

type jsonRPCSession struct {
	writer    *bufio.Writer
	writeMu   sync.Mutex
	pendingMu sync.Mutex
	pending   map[int64]chan rpcResponse
	nextID    int64
	onNotify  func(method string, params json.RawMessage)
	onRequest func(method string, params json.RawMessage) (any, bool)
	onError   func(method string, err error)
}

type rpcResponse struct {
	result json.RawMessage
	err    *rpcError
}

type rpcEnvelope struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func newJSONRPCSession(writer io.Writer) *jsonRPCSession {
	return &jsonRPCSession{
		writer:  bufio.NewWriter(writer),
		pending: map[int64]chan rpcResponse{},
		nextID:  1,
	}
}

func (s *jsonRPCSession) setNotificationHandler(handler func(method string, params json.RawMessage)) {
	s.onNotify = handler
}

func (s *jsonRPCSession) setRequestHandler(handler func(method string, params json.RawMessage) (any, bool)) {
	s.onRequest = handler
}

func (s *jsonRPCSession) setErrorHandler(handler func(method string, err error)) {
	s.onError = handler
}

func (s *jsonRPCSession) nextRequestID() int64 {
	return atomic.AddInt64(&s.nextID, 1)
}

func (s *jsonRPCSession) sendMessage(payload any) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if _, err := s.writer.Write(data); err != nil {
		return err
	}
	if err := s.writer.WriteByte('\n'); err != nil {
		return err
	}
	return s.writer.Flush()
}

func (s *jsonRPCSession) sendNotification(method string, params any) error {
	payload := map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
	}
	if params != nil {
		payload["params"] = params
	}
	return s.sendMessage(payload)
}

func (s *jsonRPCSession) sendRequest(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := s.nextRequestID()
	respCh := make(chan rpcResponse, 1)
	s.pendingMu.Lock()
	s.pending[id] = respCh
	s.pendingMu.Unlock()

	payload := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
	}
	if params != nil {
		payload["params"] = params
	}
	if err := s.sendMessage(payload); err != nil {
		s.pendingMu.Lock()
		delete(s.pending, id)
		s.pendingMu.Unlock()
		return nil, err
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case resp := <-respCh:
		if resp.err != nil {
			err := fmt.Errorf("rpc error %d: %s", resp.err.Code, resp.err.Message)
			if s.onError != nil {
				s.onError(method, err)
			}
			return nil, err
		}
		return resp.result, nil
	}
}

func (s *jsonRPCSession) handleLine(line string) {
	var env rpcEnvelope
	if err := json.Unmarshal([]byte(line), &env); err != nil {
		if s.onError != nil {
			s.onError("decode", fmt.Errorf("invalid json: %w", err))
		}
		if s.onNotify != nil {
			s.onNotify("codex/raw", json.RawMessage(fmt.Sprintf("%q", line)))
		}
		return
	}
	if env.Method != "" {
		if len(env.ID) > 0 {
			s.handleRequest(env)
			return
		}
		if s.onNotify != nil {
			s.onNotify(env.Method, env.Params)
		}
		return
	}
	if len(env.ID) > 0 {
		s.handleResponse(env)
	}
}

func (s *jsonRPCSession) handleResponse(env rpcEnvelope) {
	id, ok := parseRPCID(env.ID)
	if !ok {
		return
	}
	s.pendingMu.Lock()
	ch := s.pending[id]
	delete(s.pending, id)
	s.pendingMu.Unlock()
	if ch == nil {
		return
	}
	ch <- rpcResponse{result: env.Result, err: env.Error}
}

func (s *jsonRPCSession) handleRequest(env rpcEnvelope) {
	id, ok := parseRPCID(env.ID)
	if !ok {
		return
	}
	result := map[string]any{}
	if s.onRequest != nil {
		if resp, handled := s.onRequest(env.Method, env.Params); handled {
			result = map[string]any{"result": resp}
		}
	}
	payload := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
	}
	if value, ok := result["result"]; ok {
		payload["result"] = value
	} else {
		payload["result"] = map[string]any{}
	}
	_ = s.sendMessage(payload)
}

func parseRPCID(raw json.RawMessage) (int64, bool) {
	var id int64
	if err := json.Unmarshal(raw, &id); err == nil {
		return id, true
	}
	var strID string
	if err := json.Unmarshal(raw, &strID); err == nil {
		if parsed, err := strconv.ParseInt(strID, 10, 64); err == nil {
			return parsed, true
		}
	}
	return 0, false
}

func (m *Manager) runAppServerTask(ctx context.Context, t *taskmodel.Task, defaultEnv map[string]string, prompt string) {
	started := time.Now()
	t.Status = taskmodel.StatusRunning
	t.StartedAt = &started
	if err := m.store.Update(ctx, t); err != nil {
		return
	}

	proc, err := m.startAppServerProcess(ctx, t, defaultEnv, prompt)
	if err != nil {
		m.appendAppServerLine(t.ID, fmt.Sprintf("app-server start failed: %v", err))
		m.closeAppServerLog(t.ID)
		m.markFailure(ctx, t, err)
		return
	}
	m.mu.Lock()
	m.apps[t.ID] = proc
	m.mu.Unlock()
	m.appendAppServerLine(t.ID, fmt.Sprintf("app-server pid: %d", proc.pid))

	err = <-proc.done
	m.mu.Lock()
	delete(m.apps, t.ID)
	m.mu.Unlock()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
				m.appendAppServerLine(t.ID, fmt.Sprintf("app-server exit status: code=%d signal=%s", status.ExitStatus(), status.Signal()))
			}
		}
		if !proc.startedAt.IsZero() {
			m.appendAppServerLine(t.ID, fmt.Sprintf("app-server runtime: %s", time.Since(proc.startedAt)))
		}
		m.appendAppServerLine(t.ID, fmt.Sprintf("app-server exited with error: %v", err))
		for _, line := range proc.recentSnapshot() {
			m.appendAppServerLine(t.ID, fmt.Sprintf("app-server recent: %s", line))
		}
		m.closeAppServerLog(t.ID)
		m.markExit(ctx, t, err)
		m.scheduleTerminalCleanup(t.ID)
		return
	}
	m.appendAppServerLine(t.ID, "app-server exited successfully")
	m.closeAppServerLog(t.ID)
	m.markSuccess(ctx, t)
	m.scheduleTerminalCleanup(t.ID)
}

func (m *Manager) startAppServerProcess(ctx context.Context, t *taskmodel.Task, defaultEnv map[string]string, prompt string) (*appServerProcess, error) {
	cmdCfg := m.codexConfig.AppServer
	if strings.TrimSpace(cmdCfg.Command) == "" {
		return nil, fmt.Errorf("codex app server command is empty")
	}
	appCtx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(appCtx, cmdCfg.Command, cmdCfg.Args...)
	cmd.Dir = t.WorkDir
	cmd.Env = buildAppServerEnv(defaultEnv, t.Env, cmdCfg.Env)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, err
	}
	m.appendAppServerLine(t.ID, fmt.Sprintf("app-server started: %s %s", cmdCfg.Command, strings.Join(cmdCfg.Args, " ")))

	session := newJSONRPCSession(stdin)
	session.setRequestHandler(func(method string, params json.RawMessage) (any, bool) {
		decision, ok := autoApproveDecision(method)
		if ok {
			slog.Warn("codex app-server approval auto-approved", "task_id", t.ID, "method", method, "decision", decision)
			return map[string]any{"decision": decision}, true
		}
		return map[string]any{}, true
	})
	session.setErrorHandler(func(method string, err error) {
		m.appendAppServerLine(t.ID, fmt.Sprintf("app-server rpc error (%s): %v", method, err))
	})

	done := make(chan error, 1)
	proc := &appServerProcess{
		cmd:         cmd,
		done:        done,
		cancel:      cancel,
		session:     session,
		recentLimit: 50,
		pid:         cmd.Process.Pid,
		startedAt:   time.Now(),
	}
	session.setNotificationHandler(func(method string, params json.RawMessage) {
		line := formatRPCNotification(method, params)
		proc.addRecent(line)
		m.appendAppServerLine(t.ID, line)
	})
	go func() {
		readAppServerOutput(stdout, session)
	}()
	go func() {
		readAppServerStderr(stderr, func(line string) {
			m.appendAppServerLine(t.ID, line)
		})
	}()
	go func() {
		defer cancel()
		initCtx, cancelInit := context.WithTimeout(appCtx, 30*time.Second)
		defer cancelInit()
		conversationID, err := startAppServerSession(initCtx, session, t.WorkDir, strings.TrimSpace(prompt))
		if err != nil {
			m.appendAppServerLine(t.ID, fmt.Sprintf("app-server setup failed: %v", err))
			m.appendAppServerLine(t.ID, fmt.Sprintf("app-server kill issued during setup (pid=%d)", cmd.Process.Pid))
			_ = cmd.Process.Kill()
			return
		}
		proc.setConversation(conversationID)
	}()
	go func() {
		done <- cmd.Wait()
	}()
	return proc, nil
}

func (m *Manager) interruptAppServer(ctx context.Context, id string, proc *appServerProcess) (*taskmodel.Task, error) {
	if proc == nil || proc.cmd == nil || proc.cmd.Process == nil {
		return nil, fmt.Errorf("task not running")
	}
	m.appendAppServerLine(id, fmt.Sprintf("app-server interrupt requested (pid=%d)", proc.cmd.Process.Pid))
	interruptCtx, cancel := context.WithTimeout(ctx, interruptTimeout)
	defer cancel()
	if err := proc.cmd.Process.Signal(syscall.SIGINT); err != nil {
		m.appendAppServerLine(id, fmt.Sprintf("app-server interrupt failed, killing process (pid=%d): %v", proc.cmd.Process.Pid, err))
		_ = proc.cmd.Process.Kill()
	}
	select {
	case <-interruptCtx.Done():
	case <-proc.done:
	}
	item, err := m.store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if item.Status == taskmodel.StatusRunning || item.Status == taskmodel.StatusPending {
		now := time.Now()
		item.Status = taskmodel.StatusInterrupted
		item.FinishedAt = &now
		_ = m.store.Update(ctx, item)
	}
	return item, nil
}

func readAppServerOutput(reader io.Reader, session *jsonRPCSession) {
	scanner := bufio.NewScanner(reader)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		session.handleLine(line)
	}
}

func readAppServerStderr(reader io.Reader, onLine func(string)) {
	if onLine == nil {
		return
	}
	scanner := bufio.NewScanner(reader)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			onLine(line)
		}
	}
}

func startAppServerSession(ctx context.Context, session *jsonRPCSession, workDir string, prompt string) (string, error) {
	_, err := session.sendRequest(ctx, "initialize", wrapAppServerParams(map[string]any{
		"protocolVersion":    1,
		"clientCapabilities": map[string]any{},
		"clientInfo": map[string]any{
			"name":    "aider-server",
			"title":   "aider-server",
			"version": "1.0.0",
		},
	}))
	if err != nil {
		return "", err
	}
	_ = session.sendNotification("initialized", wrapAppServerParams(map[string]any{}))
	response, err := session.sendRequest(ctx, "newConversation", wrapAppServerParams(map[string]any{
		"cwd": workDir,
	}))
	if err != nil {
		return "", err
	}
	conversationID, err := parseConversationID(response)
	if err != nil {
		return "", err
	}
	_, err = session.sendRequest(ctx, "addConversationListener", wrapAppServerParams(map[string]any{
		"conversationId":        conversationID,
		"experimentalRawEvents": false,
	}))
	if err != nil {
		return "", err
	}
	if prompt != "" {
		_, err = session.sendRequest(ctx, "sendUserMessage", wrapAppServerParams(map[string]any{
			"conversationId": conversationID,
			"items": []map[string]any{
				buildAppServerTextItem(prompt),
			},
		}))
	}
	return conversationID, err
}

func parseConversationID(payload json.RawMessage) (string, error) {
	var data map[string]any
	if err := json.Unmarshal(payload, &data); err != nil {
		return "", fmt.Errorf("decode conversation response: %w", err)
	}
	if nested, ok := data["data"].(map[string]any); ok {
		data = nested
	}
	if value, ok := data["conversation_id"].(string); ok && value != "" {
		return value, nil
	}
	if value, ok := data["conversationId"].(string); ok && value != "" {
		return value, nil
	}
	return "", fmt.Errorf("conversation id missing")
}

func formatRPCNotification(method string, params json.RawMessage) string {
	if len(params) == 0 {
		return method
	}
	return fmt.Sprintf("%s %s", method, strings.TrimSpace(string(params)))
}

func wrapAppServerParams(params map[string]any) map[string]any {
	wrapped := map[string]any{"data": params}
	for key, value := range params {
		wrapped[key] = value
	}
	return wrapped
}

func buildAppServerTextItem(text string) map[string]any {
	return map[string]any{
		"type": "text",
		"data": map[string]any{
			"text": text,
		},
	}
}

func autoApproveDecision(method string) (string, bool) {
	switch method {
	case "applyPatchApproval", "execCommandApproval":
		return "approved", true
	case "item/commandExecution/requestApproval", "item/fileChange/requestApproval":
		return "accept", true
	default:
		return "", false
	}
}

func buildAppServerEnv(defaults, taskEnv, appEnv map[string]string) []string {
	merged := map[string]string{}
	for key, value := range defaults {
		merged[key] = value
	}
	for key, value := range appEnv {
		merged[key] = value
	}
	for key, value := range taskEnv {
		merged[key] = value
	}
	return BuildEnv(merged, nil)
}

func (m *Manager) appendAppServerLine(taskID, line string) {
	if strings.TrimSpace(line) == "" {
		return
	}
	chunk := []byte(line + "\n")
	m.terminalHub.Append(taskID, chunk)
	entry := taskmodel.LogEntry{
		TaskID: taskID,
		Seq:    -1,
		TS:     time.Now(),
		Stream: "stdout",
		Line:   SanitizeLogLine(line),
	}
	entry = m.logHub.Append(taskID, entry)
	_ = m.store.AppendLog(context.Background(), entry, m.retention)
	m.writeAppServerLog(taskID, line)
}

func (m *Manager) writeAppServerLog(taskID, line string) {
	if strings.TrimSpace(line) == "" {
		return
	}
	file := m.appServerLogFile(taskID)
	if file == nil {
		return
	}
	timestamp := time.Now().Format(time.RFC3339Nano)
	_, _ = file.WriteString(timestamp + " " + line + "\n")
}

func (m *Manager) appServerLogFile(taskID string) *os.File {
	m.appLogMu.Lock()
	defer m.appLogMu.Unlock()
	if file, ok := m.appLogFiles[taskID]; ok {
		return file
	}
	if err := os.MkdirAll("logs", 0o755); err != nil {
		return nil
	}
	path := filepath.Join("logs", "app-server-"+taskID+".log")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil
	}
	m.appLogFiles[taskID] = file
	return file
}

func (m *Manager) closeAppServerLog(taskID string) {
	m.appLogMu.Lock()
	defer m.appLogMu.Unlock()
	file := m.appLogFiles[taskID]
	if file == nil {
		return
	}
	_ = file.Close()
	delete(m.appLogFiles, taskID)
}
