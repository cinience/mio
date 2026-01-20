package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"aider-server/internal/task"
	"aider-server/internal/taskmodel"

	"github.com/gorilla/websocket"
)

type Server struct {
	manager    task.Service
	metrics    *Metrics
	authTok    string
	defaultEnv map[string]string
	mux        *http.ServeMux
	uiHandler  http.Handler
}

func NewServer(manager task.Service, metrics *Metrics, authToken string, defaultEnv map[string]string) *Server {
	s := &Server{
		manager:    manager,
		metrics:    metrics,
		authTok:    strings.TrimSpace(authToken),
		defaultEnv: defaultEnv,
		mux:        http.NewServeMux(),
	}
	s.uiHandler = s.buildUIHandler()
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler {
	return s.mux
}

func (s *Server) routes() {
	s.mux.Handle("/", s.wrapUI("ui", s.uiHandler))
	s.mux.HandleFunc("/healthz", s.wrap("health", s.handleHealth))
	s.mux.Handle("/metrics", s.wrapHandler("metrics", s.metrics.Handler()))
	s.mux.HandleFunc("/api/v1/tasks", s.wrap("tasks", s.handleTasks))
	s.mux.HandleFunc("/api/v1/tasks/", s.wrap("task", s.handleTask))
}

func (s *Server) wrap(route string, handler func(http.ResponseWriter, *http.Request) int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.authorize(w, r) {
			s.observe(route, http.StatusUnauthorized)
			return
		}
		code := handler(w, r)
		s.observe(route, code)
	}
}

func (s *Server) wrapHandler(route string, handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.authorize(w, r) {
			s.observe(route, http.StatusUnauthorized)
			return
		}
		handler.ServeHTTP(w, r)
		s.observe(route, http.StatusOK)
	})
}

func (s *Server) wrapUI(route string, handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.authorizeUI(w, r) {
			s.observe(route, http.StatusUnauthorized)
			return
		}
		handler.ServeHTTP(w, r)
		s.observe(route, http.StatusOK)
	})
}

func (s *Server) authorize(w http.ResponseWriter, r *http.Request) bool {
	if s.authTok == "" {
		return true
	}
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if auth == "" {
		token := strings.TrimSpace(r.URL.Query().Get("token"))
		if token != "" && token == s.authTok {
			return true
		}
		writeError(w, http.StatusUnauthorized, "missing authorization")
		return false
	}
	if !strings.HasPrefix(auth, "Bearer ") {
		writeError(w, http.StatusUnauthorized, "invalid authorization")
		return false
	}
	token := strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
	if token != s.authTok {
		writeError(w, http.StatusUnauthorized, "invalid token")
		return false
	}
	return true
}

func (s *Server) authorizeUI(w http.ResponseWriter, r *http.Request) bool {
	if s.authTok == "" {
		return true
	}
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(auth, "Bearer ") {
		token := strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
		if token == s.authTok {
			return true
		}
	}
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	if token == s.authTok {
		return true
	}
	http.Error(w, "unauthorized", http.StatusUnauthorized)
	return false
}

func (s *Server) observe(route string, code int) {
	if s.metrics == nil {
		return
	}
	s.metrics.ObserveRequest(route, code)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) int {
	payload := map[string]string{"status": "ok"}
	writeJSON(w, http.StatusOK, payload)
	return http.StatusOK
}

func (s *Server) handleTasks(w http.ResponseWriter, r *http.Request) int {
	switch r.Method {
	case http.MethodPost:
		return s.handleCreateTask(w, r)
	case http.MethodGet:
		return s.handleListTasks(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return http.StatusMethodNotAllowed
	}
}

func (s *Server) handleTask(w http.ResponseWriter, r *http.Request) int {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/tasks/")
	if path == "" {
		writeError(w, http.StatusNotFound, "task not found")
		return http.StatusNotFound
	}
	parts := strings.Split(path, "/")
	id := parts[0]
	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			return s.handleGetTask(w, r, id)
		case http.MethodDelete:
			return s.handleDeleteTask(w, r, id)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return http.StatusMethodNotAllowed
		}
	}
	action := parts[1]
	switch action {
	case "interrupt":
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return http.StatusMethodNotAllowed
		}
		return s.handleInterrupt(w, r, id)
	case "logs":
		if len(parts) >= 3 && parts[2] == "stream" && r.Method == http.MethodGet {
			return s.handleLogStream(w, r, id)
		}
		writeError(w, http.StatusNotFound, "log endpoint not found")
		return http.StatusNotFound
	case "terminal":
		if r.Method == http.MethodGet {
			return s.handleTerminalWS(w, r, id)
		}
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return http.StatusMethodNotAllowed
	case "chat":
		if r.Method == http.MethodPost {
			return s.handleChat(w, r, id)
		}
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return http.StatusMethodNotAllowed
	default:
		writeError(w, http.StatusNotFound, "endpoint not found")
		return http.StatusNotFound
	}
}

func (s *Server) handleCreateTask(w http.ResponseWriter, r *http.Request) int {
	var req taskmodel.CreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return http.StatusBadRequest
	}
	item, err := s.manager.CreateTask(r.Context(), req, s.defaultEnv)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return http.StatusBadRequest
	}
	writeJSON(w, http.StatusCreated, item)
	return http.StatusCreated
}

func (s *Server) handleGetTask(w http.ResponseWriter, r *http.Request, id string) int {
	item, err := s.manager.GetTask(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "task not found")
		return http.StatusNotFound
	}
	writeJSON(w, http.StatusOK, item)
	return http.StatusOK
}

type chatRequest struct {
	Message string `json:"message"`
}

func (s *Server) handleChat(w http.ResponseWriter, r *http.Request, id string) int {
	if !s.authorize(w, r) {
		return http.StatusUnauthorized
	}
	var req chatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return http.StatusBadRequest
	}
	message := strings.TrimSpace(req.Message)
	if message == "" {
		writeError(w, http.StatusBadRequest, "message is required")
		return http.StatusBadRequest
	}
	if err := s.manager.SendChat(r.Context(), id, message); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return http.StatusBadRequest
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": id, "status": "sent"})
	return http.StatusOK
}

func (s *Server) handleListTasks(w http.ResponseWriter, r *http.Request) int {
	query := r.URL.Query()
	filter := taskmodel.ListFilter{
		Labels: map[string]string{},
	}
	if status := query.Get("status"); status != "" {
		filter.Status = taskmodel.Status(status)
	}
	for key, values := range query {
		if !strings.HasPrefix(key, "label.") {
			continue
		}
		labelKey := strings.TrimPrefix(key, "label.")
		if labelKey == "" {
			continue
		}
		if len(values) > 0 {
			filter.Labels[labelKey] = values[0]
		}
	}
	if from := query.Get("from"); from != "" {
		if ts, err := time.Parse(time.RFC3339, from); err == nil {
			filter.From = &ts
		}
	}
	if to := query.Get("to"); to != "" {
		if ts, err := time.Parse(time.RFC3339, to); err == nil {
			filter.To = &ts
		}
	}
	if limit := query.Get("limit"); limit != "" {
		if val, err := strconv.Atoi(limit); err == nil {
			filter.Limit = val
		}
	}
	if offset := query.Get("offset"); offset != "" {
		if val, err := strconv.Atoi(offset); err == nil {
			filter.Offset = val
		}
	}
	items, total, err := s.manager.ListTasks(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return http.StatusInternalServerError
	}
	payload := map[string]interface{}{
		"total": total,
		"items": items,
	}
	writeJSON(w, http.StatusOK, payload)
	return http.StatusOK
}

func (s *Server) handleInterrupt(w http.ResponseWriter, r *http.Request, id string) int {
	item, err := s.manager.Interrupt(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return http.StatusBadRequest
	}
	writeJSON(w, http.StatusOK, item)
	return http.StatusOK
}

func (s *Server) handleDeleteTask(w http.ResponseWriter, r *http.Request, id string) int {
	if err := s.manager.DeleteTask(r.Context(), id); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return http.StatusBadRequest
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": id, "status": "deleted"})
	return http.StatusOK
}

func (s *Server) handleLogStream(w http.ResponseWriter, r *http.Request, id string) int {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return http.StatusInternalServerError
	}
	fromSeq := int64(0)
	if from := r.URL.Query().Get("from"); from != "" {
		if val, err := strconv.ParseInt(from, 10, 64); err == nil {
			fromSeq = val
		}
	}
	ch, backlog, unsubscribe := s.manager.SubscribeLogs(r.Context(), id, fromSeq)
	defer unsubscribe()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	startPayload := map[string]string{"task_id": id}
	if item, err := s.manager.GetTask(r.Context(), id); err == nil {
		if item.Protocol != "" {
			startPayload["protocol"] = string(item.Protocol)
		}
	}
	writeSSE(w, "start", startPayload)
	for _, entry := range backlog {
		writeSSE(w, "log", entry)
	}
	flusher.Flush()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			writeSSE(w, "end", map[string]string{"reason": "client_closed"})
			flusher.Flush()
			return http.StatusOK
		case entry := <-ch:
			writeSSE(w, "log", entry)
			flusher.Flush()
		}
	}
}

func (s *Server) handleTerminalWS(w http.ResponseWriter, r *http.Request, id string) int {
	if !s.authorizeWS(w, r) {
		return http.StatusUnauthorized
	}
	upgrader := websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return http.StatusBadRequest
	}
	defer conn.Close()

	cursor := parseCursorParam(r.URL.Query().Get("cursor"))
	ch, backlog, unsubscribe := s.manager.SubscribeTerminal(id, cursor)
	defer unsubscribe()

	if len(backlog) > 0 {
		_ = conn.WriteMessage(websocket.BinaryMessage, backlog)
	}

	ctx := r.Context()
	readDone := make(chan struct{})
	conn.SetReadLimit(64 * 1024)
	conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})
	go func() {
		defer close(readDone)
		for {
			msgType, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			conn.SetReadDeadline(time.Now().Add(60 * time.Second))
			if msgType != websocket.TextMessage && msgType != websocket.BinaryMessage {
				continue
			}
			if msgType == websocket.TextMessage {
				handled, err := s.handleTerminalControl(ctx, id, data)
				if err != nil {
					continue
				}
				if handled {
					continue
				}
			}
			payload := sanitizeTerminalInput(data)
			if len(payload) == 0 {
				continue
			}
			_ = s.manager.SendInput(ctx, id, payload)
		}
	}()

	pingTicker := time.NewTicker(25 * time.Second)
	defer pingTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			return http.StatusOK
		case <-readDone:
			return http.StatusOK
		case chunk, ok := <-ch:
			if !ok {
				return http.StatusOK
			}
			if len(chunk) == 0 {
				continue
			}
			if err := conn.WriteMessage(websocket.BinaryMessage, chunk); err != nil {
				return http.StatusOK
			}
		case <-pingTicker.C:
			_ = conn.WriteMessage(websocket.PingMessage, []byte("ping"))
		}
	}
}

func (s *Server) authorizeWS(w http.ResponseWriter, r *http.Request) bool {
	if s.authTok == "" {
		return true
	}
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(auth, "Bearer ") {
		token := strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
		if token == s.authTok {
			return true
		}
	}
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	if token != "" && token == s.authTok {
		return true
	}
	http.Error(w, "unauthorized", http.StatusUnauthorized)
	return false
}

func sanitizeTerminalInput(data []byte) []byte {
	if len(data) == 0 {
		return data
	}
	out := make([]byte, 0, len(data))
	for i := 0; i < len(data); {
		if data[i] == 0x1b && i+1 < len(data) && data[i+1] == '[' {
			j := i + 2
			for j < len(data) {
				b := data[j]
				if (b >= '0' && b <= '9') || b == ';' || b == '?' {
					j++
					continue
				}
				break
			}
			if j < len(data) {
				switch data[j] {
				case 'c', 'R', 'n':
					i = j + 1
					continue
				}
			}
		} else if data[i] == 0x1b && i+1 < len(data) && (data[i+1] == ']' || data[i+1] == 'P' || data[i+1] == 'X' || data[i+1] == '^' || data[i+1] == '_') {
			i = skipTerminalSequence(data, i+2)
			continue
		}
		out = append(out, data[i])
		i++
	}
	return out
}

func skipTerminalSequence(data []byte, start int) int {
	for i := start; i < len(data); i++ {
		if data[i] == 0x07 {
			return i + 1
		}
		if data[i] == 0x1b && i+1 < len(data) && data[i+1] == '\\' {
			return i + 2
		}
	}
	return len(data)
}

func parseCursorParam(raw string) int64 {
	if raw == "" {
		return 0
	}
	val, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || val < 0 {
		return 0
	}
	return val
}

type terminalControlMessage struct {
	Type string `json:"type"`
	Cols int    `json:"cols"`
	Rows int    `json:"rows"`
}

func (s *Server) handleTerminalControl(ctx context.Context, id string, payload []byte) (bool, error) {
	if len(payload) == 0 || payload[0] != '{' {
		return false, nil
	}
	var msg terminalControlMessage
	if err := json.Unmarshal(payload, &msg); err != nil {
		return false, nil
	}
	switch msg.Type {
	case "resize":
		if msg.Cols <= 0 || msg.Rows <= 0 {
			return true, nil
		}
		_ = s.manager.ResizeTerminal(ctx, id, msg.Cols, msg.Rows)
		return true, nil
	case "ping":
		return true, nil
	default:
		return false, nil
	}
}

func (s *Server) buildUIHandler() http.Handler {
	root := resolveUIRoot()
	fileServer := http.FileServer(http.Dir(root))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" || r.URL.Path == "/index.html" {
			http.ServeFile(w, r, filepath.Join(root, "index.html"))
			return
		}
		fileServer.ServeHTTP(w, r)
	})
}

func resolveUIRoot() string {
	exe, err := os.Executable()
	if err == nil {
		base := filepath.Dir(exe)
		candidate := filepath.Join(base, "..", "web")
		if _, statErr := os.Stat(filepath.Join(candidate, "index.html")); statErr == nil {
			return candidate
		}
	}
	if _, statErr := os.Stat(filepath.Join("web", "index.html")); statErr == nil {
		return "web"
	}
	return "."
}

func writeSSE(w http.ResponseWriter, event string, payload interface{}) {
	data, _ := json.Marshal(payload)
	fmt.Fprintf(w, "event: %s\n", event)
	fmt.Fprintf(w, "data: %s\n\n", string(data))
}

func writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
