package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"vision-server/internal/gateway/gb28181"
	"vision-server/internal/owl/conf"
	"vision-server/internal/owl/onvifadapter"
	"vision-server/internal/store"
)

type server struct {
	store           *store.Store
	token           string
	snapshotTimeout time.Duration
	onvif           *onvifadapter.Adapter
	gbGateway       *gb28181.Gateway
}

type devicePayload struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Protocol   string            `json:"protocol"`
	Address    string            `json:"address"`
	Username   string            `json:"username"`
	Password   string            `json:"password"`
	ExternalID string            `json:"externalId"`
	Online     bool              `json:"online"`
	Meta       map[string]string `json:"meta"`
}

type channelPayload struct {
	ID            string            `json:"id"`
	DeviceID      string            `json:"deviceId"`
	Name          string            `json:"name"`
	Protocol      string            `json:"protocol"`
	StreamURL     string            `json:"streamUrl"`
	StreamRTSP    string            `json:"streamRtsp"`
	StreamHLS     string            `json:"streamHls"`
	StreamHTTPFLV string            `json:"streamHttpFlv"`
	StreamWebRTC  string            `json:"streamWebRtc"`
	Username      string            `json:"username"`
	Password      string            `json:"password"`
	ExternalID    string            `json:"externalId"`
	Online        bool              `json:"online"`
	Meta          map[string]string `json:"meta"`
}

func main() {
	addr := getenv("VISION_SERVER_ADDR", ":15123")
	token := strings.TrimSpace(os.Getenv("VISION_SERVER_TOKEN"))
	snapshotTimeout := time.Second * 8
	if raw := strings.TrimSpace(os.Getenv("VISION_SERVER_SNAPSHOT_TIMEOUT")); raw != "" {
		if seconds, err := strconv.Atoi(raw); err == nil && seconds > 0 {
			snapshotTimeout = time.Duration(seconds) * time.Second
		}
	}

	store := store.NewStore()
	srv := &server{
		store:           store,
		token:           token,
		snapshotTimeout: snapshotTimeout,
		onvif:           onvifadapter.NewAdapter(store),
	}

	if cfg, ok := loadGBConfig(); ok {
		if gateway, err := gb28181.NewGateway(store, cfg); err == nil {
			srv.gbGateway = gateway
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", srv.handleHealth)
	mux.HandleFunc("/devices", srv.handleDevices)
	mux.HandleFunc("/devices/", srv.handleDevice)
	mux.HandleFunc("/channels", srv.handleChannels)
	mux.HandleFunc("/channels/", srv.handleChannel)

	handler := authMiddleware(token, mux)
	server := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	if err := server.ListenAndServe(); err != nil {
		fmt.Fprintf(os.Stderr, "vision-server stopped: %v\n", err)
	}
}

func authMiddleware(token string, next http.Handler) http.Handler {
	if token == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			auth := strings.TrimSpace(r.Header.Get("Authorization"))
			if auth != "Bearer "+token {
				writeError(w, http.StatusUnauthorized, "unauthorized")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *server) handleDevices(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, s.store.ListDevices())
	case http.MethodPost:
		var payload devicePayload
		if err := decodeJSON(r, &payload); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		protocol := strings.ToLower(strings.TrimSpace(payload.Protocol))
		device := &store.Device{
			ID:         strings.TrimSpace(payload.ID),
			Name:       strings.TrimSpace(payload.Name),
			Protocol:   protocol,
			Address:    strings.TrimSpace(payload.Address),
			Username:   strings.TrimSpace(payload.Username),
			Password:   payload.Password,
			ExternalID: strings.TrimSpace(payload.ExternalID),
			Online:     payload.Online,
			Meta:       payload.Meta,
		}
		if device.Name == "" {
			writeError(w, http.StatusBadRequest, "device name required")
			return
		}
		if protocol == "onvif" {
			dev, channels, err := s.onvif.AddDevice(r.Context(), device)
			if err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			writeJSON(w, http.StatusCreated, map[string]interface{}{
				"device":   dev,
				"channels": channels,
			})
			return
		}
		if protocol == "gb28181" {
			if device.ExternalID == "" {
				device.ExternalID = strings.TrimSpace(payload.ID)
			}
			if device.ExternalID == "" {
				writeError(w, http.StatusBadRequest, "externalId required for gb28181")
				return
			}
		}
		writeJSON(w, http.StatusCreated, s.store.SaveDevice(device))
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *server) handleDevice(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/devices/")
	id = strings.TrimSpace(id)
	if id == "" {
		writeError(w, http.StatusNotFound, "device not found")
		return
	}
	switch r.Method {
	case http.MethodGet:
		if device, ok := s.store.GetDevice(id); ok {
			writeJSON(w, http.StatusOK, device)
			return
		}
		writeError(w, http.StatusNotFound, "device not found")
	case http.MethodDelete:
		s.store.DeleteDevice(id)
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *server) handleChannels(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		deviceID := strings.TrimSpace(r.URL.Query().Get("deviceId"))
		writeJSON(w, http.StatusOK, s.store.ListChannels(deviceID))
	case http.MethodPost:
		var payload channelPayload
		if err := decodeJSON(r, &payload); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		channel := &store.Channel{
			ID:            strings.TrimSpace(payload.ID),
			DeviceID:      strings.TrimSpace(payload.DeviceID),
			Name:          strings.TrimSpace(payload.Name),
			Protocol:      strings.ToLower(strings.TrimSpace(payload.Protocol)),
			StreamURL:     strings.TrimSpace(payload.StreamURL),
			StreamRTSP:    strings.TrimSpace(payload.StreamRTSP),
			StreamHLS:     strings.TrimSpace(payload.StreamHLS),
			StreamHTTPFLV: strings.TrimSpace(payload.StreamHTTPFLV),
			StreamWebRTC:  strings.TrimSpace(payload.StreamWebRTC),
			Username:      strings.TrimSpace(payload.Username),
			Password:      payload.Password,
			ExternalID:    strings.TrimSpace(payload.ExternalID),
			Online:        payload.Online,
			Meta:          payload.Meta,
		}
		if channel.Name == "" {
			writeError(w, http.StatusBadRequest, "channel name required")
			return
		}
		if channel.StreamURL == "" && channel.Protocol != "gb28181" {
			writeError(w, http.StatusBadRequest, "streamUrl required")
			return
		}
		writeJSON(w, http.StatusCreated, s.store.SaveChannel(channel))
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *server) handleChannel(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/channels/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		writeError(w, http.StatusNotFound, "channel not found")
		return
	}
	channelID := parts[0]
	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			if channel, ok := s.store.GetChannel(channelID); ok {
				writeJSON(w, http.StatusOK, channel)
				return
			}
			writeError(w, http.StatusNotFound, "channel not found")
		case http.MethodDelete:
			s.store.DeleteChannel(channelID)
			writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
		return
	}
	if len(parts) == 2 {
		switch parts[1] {
		case "snapshot":
			s.handleSnapshot(w, r, channelID)
			return
		case "play":
			s.handlePlay(w, r, channelID)
			return
		}
	}
	writeError(w, http.StatusNotFound, "channel not found")
}

func (s *server) handleSnapshot(w http.ResponseWriter, r *http.Request, channelID string) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	channel, ok := s.store.GetChannel(channelID)
	if !ok {
		writeError(w, http.StatusNotFound, "channel not found")
		return
	}

	snapshotCtx, cancel := context.WithTimeout(r.Context(), s.snapshotTimeout)
	defer cancel()

	if strings.EqualFold(channel.Protocol, "gb28181") && s.gbGateway != nil {
		if payload, err := s.gbGateway.Snapshot(snapshotCtx, channel); err == nil {
			w.Header().Set("Content-Type", "image/jpeg")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(payload)
			return
		}
	}

	streamURL, protocol, err := resolveStreamURL(channel)
	if err != nil || streamURL == "" {
		writeError(w, http.StatusBadRequest, "invalid streamUrl")
		return
	}

	payload, err := captureSnapshot(snapshotCtx, protocol, streamURL)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(payload)
}

func (s *server) handlePlay(w http.ResponseWriter, r *http.Request, channelID string) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	channel, ok := s.store.GetChannel(channelID)
	if !ok {
		writeError(w, http.StatusNotFound, "channel not found")
		return
	}
	if strings.EqualFold(channel.Protocol, "gb28181") && s.gbGateway != nil {
		if _, err := s.gbGateway.EnsurePlay(r.Context(), channel); err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"url":          channel.StreamURL,
		"protocol":     channel.Protocol,
		"rtsp":         channel.StreamRTSP,
		"hls":          channel.StreamHLS,
		"httpFlv":      channel.StreamHTTPFLV,
		"webrtc":       channel.StreamWebRTC,
		"playing":      channel.Playing,
		"streamUrl":    channel.StreamURL,
		"streamRtsp":   channel.StreamRTSP,
		"streamHls":    channel.StreamHLS,
		"streamFlv":    channel.StreamHTTPFLV,
		"streamWebRtc": channel.StreamWebRTC,
	})
}

func resolveStreamURL(channel *store.Channel) (string, string, error) {
	if channel == nil {
		return "", "", fmt.Errorf("empty channel")
	}
	if streamURL := strings.TrimSpace(channel.StreamRTSP); streamURL != "" {
		return streamURL, "rtsp", nil
	}
	if streamURL := strings.TrimSpace(channel.StreamHTTPFLV); streamURL != "" {
		return streamURL, "http-flv", nil
	}
	if streamURL := strings.TrimSpace(channel.StreamHLS); streamURL != "" {
		return streamURL, "hls", nil
	}
	streamURL := strings.TrimSpace(channel.StreamURL)
	if streamURL == "" {
		return "", "", fmt.Errorf("empty streamUrl")
	}
	username := strings.TrimSpace(channel.Username)
	if username == "" {
		return streamURL, channel.Protocol, nil
	}
	if !strings.Contains(streamURL, "://") {
		return streamURL, channel.Protocol, nil
	}
	parts := strings.SplitN(streamURL, "://", 2)
	if len(parts) != 2 {
		return streamURL, channel.Protocol, nil
	}
	if strings.Contains(parts[1], "@") {
		return streamURL, channel.Protocol, nil
	}
	userInfo := url.UserPassword(username, channel.Password)
	return fmt.Sprintf("%s://%s@%s", parts[0], userInfo.String(), parts[1]), channel.Protocol, nil
}

func captureSnapshot(ctx context.Context, protocol, streamURL string) ([]byte, error) {
	args := []string{"-hide_banner", "-loglevel", "error"}
	if strings.EqualFold(strings.TrimSpace(protocol), "rtsp") {
		args = append(args, "-rtsp_transport", "tcp")
	}
	args = append(args, "-i", streamURL, "-frames:v", "1", "-f", "image2pipe", "-vcodec", "mjpeg", "-")
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("snapshot failed: %w", err)
	}
	return output, nil
}

func decodeJSON(r *http.Request, target interface{}) error {
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("invalid json: %w", err)
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func getenv(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func getenvInt(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}

func loadGBConfig() (*conf.Bootstrap, bool) {
	port := getenvInt("VISION_SERVER_GB_PORT", 0)
	if port <= 0 {
		return nil, false
	}
	cfg := &conf.Bootstrap{}
	cfg.Sip.Port = port
	cfg.Sip.ID = getenv("VISION_SERVER_GB_ID", "34020000002000000001")
	cfg.Sip.Domain = getenv("VISION_SERVER_GB_DOMAIN", "3402000000")
	cfg.Sip.Password = getenv("VISION_SERVER_GB_PASSWORD", "")

	cfg.Media.IP = getenv("VISION_SERVER_MEDIA_IP", "127.0.0.1")
	cfg.Media.HTTPPort = getenvInt("VISION_SERVER_MEDIA_HTTP_PORT", 8080)
	cfg.Media.Secret = getenv("VISION_SERVER_MEDIA_SECRET", "")
	cfg.Media.Type = getenv("VISION_SERVER_MEDIA_TYPE", "zlm")
	cfg.Media.SDPIP = getenv("VISION_SERVER_MEDIA_SDP_IP", cfg.Media.IP)
	cfg.Media.RTPPortRange = getenv("VISION_SERVER_MEDIA_RTP_RANGE", "")
	return cfg, true
}
