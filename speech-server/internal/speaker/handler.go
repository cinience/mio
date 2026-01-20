package speaker

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-audio/wav"
	"github.com/gorilla/websocket"

	"speech-server/internal/logger"
)

// Handler exposes HTTP and WS endpoints.
type Handler struct {
	manager *Manager
	cfg     Config
}

func NewHandler(mgr *Manager, cfg Config) *Handler {
	return &Handler{manager: mgr, cfg: cfg}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/speaker/register", h.handleRegister)
	mux.HandleFunc("/api/v1/speaker/register_base64", h.handleRegisterBase64)
	mux.HandleFunc("/api/v1/speaker/identify", h.handleIdentify)
	mux.HandleFunc("/api/v1/speaker/identify_base64", h.handleIdentifyBase64)
	mux.HandleFunc("/api/v1/speaker/verify/", h.handleVerify) // prefix match
	mux.HandleFunc("/api/v1/speaker/list", h.handleList)
	mux.HandleFunc("/api/v1/speaker/stats", h.handleStats)
	mux.HandleFunc("/api/v1/speaker/", h.handleDelete) // DELETE /api/v1/speaker/:id
	mux.HandleFunc("/api/v1/speaker/identify_ws", h.handleIdentifyWS)
}

func (h *Handler) handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	samples, rate, err := readAudioFromRequest(r, h.cfg.SampleRate)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := r.ParseMultipartForm(32 << 20); err != nil && !errors.Is(err, http.ErrNotMultipart) {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	speakerID := r.FormValue("speaker_id")
	speakerName := r.FormValue("speaker_name")
	uid := r.FormValue("uid")
	agentID := r.FormValue("agent_id")

	info, err := h.manager.Register(r.Context(), RegisterInput{
		UID:         uid,
		AgentID:     agentID,
		SpeakerID:   speakerID,
		SpeakerName: speakerName,
		Samples:     samples,
		SampleRate:  rate,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"speaker_id":   info.ID,
		"speaker_name": info.Name,
		"sample_count": info.SampleCount,
	})
}

func (h *Handler) handleRegisterBase64(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		SpeakerID   string `json:"speaker_id"`
		SpeakerName string `json:"speaker_name"`
		Audio       string `json:"audio"`
		UID         string `json:"uid"`
		AgentID     string `json:"agent_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	samples, rate, err := decodeBase64Audio(req.Audio, h.cfg.SampleRate)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	info, err := h.manager.Register(r.Context(), RegisterInput{
		UID:         req.UID,
		AgentID:     req.AgentID,
		SpeakerID:   req.SpeakerID,
		SpeakerName: req.SpeakerName,
		Samples:     samples,
		SampleRate:  rate,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func (h *Handler) handleIdentify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	samples, rate, err := readAudioFromRequest(r, h.cfg.SampleRate)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := r.ParseMultipartForm(32 << 20); err != nil && !errors.Is(err, http.ErrNotMultipart) {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	threshold := parseFloatOrDefault(r.FormValue("threshold"), h.cfg.Threshold)
	res, err := h.manager.Identify(r.Context(), IdentifyInput{
		UID:         r.FormValue("uid"),
		AgentID:     r.FormValue("agent_id"),
		SpeakerID:   r.FormValue("speaker_id"),
		SpeakerName: r.FormValue("speaker_name"),
		Samples:     samples,
		SampleRate:  rate,
		Threshold:   threshold,
		TopK:        1,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if res == nil {
		writeJSON(w, http.StatusOK, map[string]any{"matched": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"matched":      true,
		"speaker_id":   res.SpeakerID,
		"speaker_name": res.SpeakerName,
		"confidence":   res.Confidence,
	})
}

func (h *Handler) handleIdentifyBase64(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		UID         string  `json:"uid"`
		AgentID     string  `json:"agent_id"`
		SpeakerID   string  `json:"speaker_id"`
		SpeakerName string  `json:"speaker_name"`
		Audio       string  `json:"audio"`
		Threshold   float32 `json:"threshold"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	samples, rate, err := decodeBase64Audio(req.Audio, h.cfg.SampleRate)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	res, err := h.manager.Identify(r.Context(), IdentifyInput{
		UID:         req.UID,
		AgentID:     req.AgentID,
		SpeakerID:   req.SpeakerID,
		SpeakerName: req.SpeakerName,
		Samples:     samples,
		SampleRate:  rate,
		Threshold:   req.Threshold,
		TopK:        1,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if res == nil {
		writeJSON(w, http.StatusOK, map[string]any{"matched": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"matched":      true,
		"speaker_id":   res.SpeakerID,
		"speaker_name": res.SpeakerName,
		"confidence":   res.Confidence,
	})
}

func (h *Handler) handleVerify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/speaker/verify/"), "/")
	speakerID := strings.TrimSpace(parts[0])
	if speakerID == "" {
		writeError(w, http.StatusBadRequest, errors.New("speaker_id is required"))
		return
	}
	samples, rate, err := readAudioFromRequest(r, h.cfg.SampleRate)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := r.ParseMultipartForm(32 << 20); err != nil && !errors.Is(err, http.ErrNotMultipart) {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	threshold := parseFloatOrDefault(r.FormValue("threshold"), h.cfg.Threshold)
	res, err := h.manager.Verify(r.Context(), VerifyInput{
		UID:        r.FormValue("uid"),
		AgentID:    r.FormValue("agent_id"),
		SpeakerID:  speakerID,
		Samples:    samples,
		SampleRate: rate,
		Threshold:  threshold,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (h *Handler) handleList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"speakers": h.manager.List(),
	})
}

func (h *Handler) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, h.manager.Stats())
}

func (h *Handler) handleDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		return
	}
	trimmed := strings.TrimPrefix(r.URL.Path, "/api/v1/speaker/")
	if trimmed == "" || trimmed == " " {
		writeError(w, http.StatusBadRequest, errors.New("speaker_id is required"))
		return
	}
	speakerID := filepath.Base(trimmed)
	if speakerID == "" || speakerID == "speaker" {
		writeError(w, http.StatusBadRequest, errors.New("speaker_id is required"))
		return
	}
	if err := h.manager.DeleteSpeaker(r.Context(), speakerID); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": speakerID})
}

func (h *Handler) handleIdentifyWS(w http.ResponseWriter, r *http.Request) {
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.Errorf("speaker ws upgrade: %v", err)
		return
	}
	defer conn.Close()

	var buf bytes.Buffer
	for {
		msgType, data, err := conn.ReadMessage()
		if err != nil {
			break
		}
		if msgType == websocket.BinaryMessage {
			buf.Write(data)
		} else {
			// ignore text
		}
	}

	if buf.Len() == 0 {
		conn.WriteJSON(map[string]any{"error": "no audio received"})
		return
	}

	samples := pcm16ToFloat32(buf.Bytes())
	res, err := h.manager.Identify(context.Background(), IdentifyInput{
		Samples:     samples,
		SampleRate:  h.cfg.SampleRate,
		Threshold:   h.cfg.Threshold,
		TopK:        1,
		SpeakerID:   r.URL.Query().Get("speaker_id"),
		SpeakerName: r.URL.Query().Get("speaker_name"),
		UID:         r.URL.Query().Get("uid"),
		AgentID:     r.URL.Query().Get("agent_id"),
	})
	if err != nil {
		_ = conn.WriteJSON(map[string]any{"error": err.Error()})
		return
	}
	if res == nil {
		_ = conn.WriteJSON(map[string]any{"matched": false})
		return
	}
	_ = conn.WriteJSON(map[string]any{
		"matched":      true,
		"speaker_id":   res.SpeakerID,
		"speaker_name": res.SpeakerName,
		"confidence":   res.Confidence,
	})
}

func readAudioFromRequest(r *http.Request, fallbackRate int) ([]float32, int, error) {
	if err := r.ParseMultipartForm(32 << 20); err != nil && !errors.Is(err, http.ErrNotMultipart) {
		return nil, 0, err
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		file, header, err = r.FormFile("audio")
	}
	if err != nil {
		return nil, 0, errors.New("audio file is required")
	}
	defer file.Close()

	buf, err := io.ReadAll(file)
	if err != nil {
		return nil, 0, err
	}
	if header != nil && strings.HasSuffix(strings.ToLower(header.Filename), ".wav") || bytes.HasPrefix(buf, []byte("RIFF")) {
		return decodeWAV(buf)
	}
	// default treat as pcm16
	return pcm16ToFloat32(buf), fallbackRate, nil
}

func decodeBase64Audio(b64 string, fallbackRate int) ([]float32, int, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(b64))
	if err != nil {
		return nil, 0, err
	}
	if bytes.HasPrefix(raw, []byte("RIFF")) {
		return decodeWAV(raw)
	}
	return pcm16ToFloat32(raw), fallbackRate, nil
}

func decodeWAV(data []byte) ([]float32, int, error) {
	reader := bytes.NewReader(data)
	wavDecoder := wav.NewDecoder(reader)
	if !wavDecoder.IsValidFile() {
		return nil, 0, errors.New("invalid wav file")
	}
	intBuf, err := wavDecoder.FullPCMBuffer()
	if err != nil {
		return nil, 0, err
	}
	if intBuf == nil {
		return nil, 0, errors.New("empty wav")
	}
	f32 := intBuf.AsFloat32Buffer()
	sampleRate := f32.Format.SampleRate
	if sampleRate <= 0 {
		sampleRate = 16000
	}
	return f32.Data, sampleRate, nil
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]any{"error": err.Error()})
}

func parseFloatOrDefault(raw string, def float32) float32 {
	if strings.TrimSpace(raw) == "" {
		return def
	}
	f, err := strconv.ParseFloat(raw, 32)
	if err != nil {
		return def
	}
	return float32(f)
}

// pcm16ToFloat32 converts little-endian signed 16-bit PCM to float32.
func pcm16ToFloat32(data []byte) []float32 {
	if len(data)%2 != 0 {
		data = data[:len(data)-1]
	}
	samples := make([]float32, len(data)/2)
	for i := 0; i < len(samples); i++ {
		v := int16(data[i*2]) | int16(data[i*2+1])<<8
		samples[i] = float32(v) / 32768.0
	}
	return samples
}
