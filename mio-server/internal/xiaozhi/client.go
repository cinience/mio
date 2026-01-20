package xiaozhi

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"

	godepsopus "github.com/godeps/opus"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

const opusMaxFrameDurationMs = 120

type AudioFrame struct {
	PCM        []byte
	SampleRate int
	Channels   int
}

type Callbacks struct {
	OnSTT     func(text string)
	OnLLM     func(text string, state string)
	OnText    func(text string)
	OnTTS     func(state string, text string)
	OnMCP     func(payload json.RawMessage)
	OnGoodbye func()
	OnAudio   func(frame AudioFrame)
	OnError   func(err error)
}

type Client struct {
	cfg       Config
	logger    *zap.Logger
	callbacks Callbacks

	mu      sync.Mutex
	conn    *websocket.Conn
	closed  bool
	decoder *godepsopus.Decoder
	encoder *godepsopus.Encoder
}

func NewClient(cfg Config, callbacks Callbacks, logger *zap.Logger) *Client {
	var decoder *godepsopus.Decoder
	var encoder *godepsopus.Encoder
	if cfg.AudioParams.Format == "opus" {
		dec, err := godepsopus.NewDecoder(cfg.AudioParams.SampleRate, cfg.AudioParams.Channels)
		if err != nil {
			logger.Warn("opus decoder init failed", zap.Error(err))
		} else {
			decoder = dec
		}
		enc, err := godepsopus.NewEncoder(cfg.AudioParams.SampleRate, cfg.AudioParams.Channels, godepsopus.AppAudio)
		if err != nil {
			logger.Warn("opus encoder init failed", zap.Error(err))
		} else {
			encoder = enc
		}
	}
	return &Client{
		cfg:       cfg,
		logger:    logger,
		callbacks: callbacks,
		decoder:   decoder,
		encoder:   encoder,
	}
}

func (c *Client) Connect(ctx context.Context) {
	go c.run(ctx)
}

func (c *Client) Close() {
	c.mu.Lock()
	c.closed = true
	if c.conn != nil {
		_ = c.conn.Close()
		c.conn = nil
	}
	c.mu.Unlock()
}

func (c *Client) SendTextInput(ctx context.Context, text string) error {
	payload := map[string]any{
		"type":      "listen",
		"state":     "detect",
		"mode":      "auto",
		"text":      text,
		"device_id": c.cfg.DeviceID,
	}
	return c.sendJSON(ctx, payload)
}

func (c *Client) SendListenState(ctx context.Context, state string) error {
	payload := map[string]any{
		"type":      "listen",
		"state":     state,
		"mode":      "auto",
		"device_id": c.cfg.DeviceID,
	}
	return c.sendJSON(ctx, payload)
}

func (c *Client) Abort(ctx context.Context) error {
	payload := map[string]any{
		"type":   "abort",
		"reason": "user_interrupt",
	}
	return c.sendJSON(ctx, payload)
}

func (c *Client) SendAudio(ctx context.Context, pcm []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()
	if conn == nil {
		return errors.New("xiaozhi connection not ready")
	}
	if err := conn.WriteMessage(websocket.BinaryMessage, pcm); err != nil {
		return err
	}
	return nil
}

func (c *Client) EncodeOpusFloat(pcm []float32) ([]byte, error) {
	if c.encoder == nil {
		return nil, errors.New("opus encoder is not initialized")
	}
	if len(pcm) == 0 {
		return nil, errors.New("empty pcm frame")
	}
	out := make([]byte, 4000)
	written, err := c.encoder.EncodeFloat32(pcm, out)
	if err != nil {
		return nil, err
	}
	return out[:written], nil
}

func (c *Client) SendMCP(ctx context.Context, payload any) error {
	wrapper := map[string]any{
		"type":    "mcp",
		"payload": payload,
	}
	return c.sendJSON(ctx, wrapper)
}

func (c *Client) sendJSON(ctx context.Context, payload any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()
	if conn == nil {
		return errors.New("xiaozhi connection not ready")
	}
	if err := conn.WriteJSON(payload); err != nil {
		return err
	}
	return nil
}

func (c *Client) run(ctx context.Context) {
	delay := time.Second
	for {
		if ctx.Err() != nil {
			return
		}
		if c.isClosed() {
			return
		}
		c.logger.Info("xiaozhi connecting",
			zap.String("backend_url", c.cfg.BackendURL),
			zap.String("device_id", c.cfg.DeviceID),
			zap.String("client_id", c.cfg.ClientID),
		)
		if err := c.connectOnce(ctx); err != nil {
			c.reportError(err)
			c.logger.Warn("xiaozhi connect failed", zap.Error(err))
			time.Sleep(delay)
			delay = nextBackoff(delay)
			continue
		}
		c.logger.Info("xiaozhi connected",
			zap.String("backend_url", c.cfg.BackendURL),
			zap.String("device_id", c.cfg.DeviceID),
			zap.String("client_id", c.cfg.ClientID),
		)
		delay = time.Second
		if err := c.readLoop(); err != nil {
			c.reportError(err)
			c.logger.Warn("xiaozhi connection lost", zap.Error(err))
			time.Sleep(delay)
			delay = nextBackoff(delay)
			continue
		}
	}
}

func (c *Client) connectOnce(ctx context.Context) error {
	if c.cfg.BackendURL == "" {
		return errors.New("xiaozhi backend url is empty")
	}
	headers := http.Header{}
	headers.Set("Protocol-Version", intToString(c.cfg.ProtocolVersion))
	headers.Set("Client-Id", c.cfg.ClientID)
	headers.Set("Device-Id", c.cfg.DeviceID)
	if c.cfg.AccessToken != "" {
		headers.Set("Authorization", "Bearer "+c.cfg.AccessToken)
	}
	dialer := websocket.Dialer{}
	conn, _, err := dialer.DialContext(ctx, c.cfg.BackendURL, headers)
	if err != nil {
		return err
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		_ = conn.Close()
		return errors.New("client closed")
	}
	if c.conn != nil {
		_ = c.conn.Close()
	}
	c.conn = conn
	c.mu.Unlock()

	return c.sendHello(ctx)
}

func (c *Client) sendHello(ctx context.Context) error {
	payload := map[string]any{
		"type":      "hello",
		"version":   c.cfg.ProtocolVersion,
		"features":  map[string]any{"mcp": true},
		"transport": "websocket",
		"audio_params": map[string]any{
			"format":         c.cfg.AudioParams.Format,
			"sample_rate":    c.cfg.AudioParams.SampleRate,
			"channels":       c.cfg.AudioParams.Channels,
			"frame_duration": c.cfg.AudioParams.FrameDuration,
		},
	}
	return c.sendJSON(ctx, payload)
}

func (c *Client) readLoop() error {
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()
	if conn == nil {
		return errors.New("xiaozhi connection not ready")
	}

	for {
		msgType, data, err := conn.ReadMessage()
		if err != nil {
			c.mu.Lock()
			if c.conn == conn {
				_ = c.conn.Close()
				c.conn = nil
			}
			c.mu.Unlock()
			return err
		}

		switch msgType {
		case websocket.TextMessage:
			c.handleTextMessage(data)
		case websocket.BinaryMessage:
			c.handleBinaryFrame(data)
		}
	}
}

func (c *Client) handleTextMessage(data []byte) {
	var payload struct {
		Type   string          `json:"type"`
		Text   string          `json:"text"`
		State  string          `json:"state"`
		RawMCP json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		c.reportError(err)
		return
	}

	switch payload.Type {
	case "hello":
		return
	case "stt":
		if payload.Text != "" && c.callbacks.OnSTT != nil {
			c.callbacks.OnSTT(payload.Text)
		}
	case "llm":
		if payload.Text != "" && c.callbacks.OnLLM != nil {
			c.callbacks.OnLLM(payload.Text, payload.State)
		}
	case "text":
		if payload.Text != "" && c.callbacks.OnText != nil {
			c.callbacks.OnText(payload.Text)
		}
	case "tts":
		if c.callbacks.OnTTS != nil {
			c.callbacks.OnTTS(payload.State, payload.Text)
		}
	case "mcp":
		if c.callbacks.OnMCP != nil {
			c.callbacks.OnMCP(payload.RawMCP)
		}
	case "goodbye":
		if c.callbacks.OnGoodbye != nil {
			c.callbacks.OnGoodbye()
		}
	}
}

func (c *Client) handleBinaryFrame(frame []byte) {
	if len(frame) == 0 {
		return
	}
	if c.callbacks.OnAudio == nil {
		return
	}
	format := c.cfg.AudioParams.Format
	switch format {
	case "opus":
		pcm, err := c.decodeOpus(frame)
		if err != nil {
			c.reportError(err)
			return
		}
		c.callbacks.OnAudio(AudioFrame{
			PCM:        pcm,
			SampleRate: c.cfg.AudioParams.SampleRate,
			Channels:   c.cfg.AudioParams.Channels,
		})
	case "pcm16", "pcm":
		c.callbacks.OnAudio(AudioFrame{
			PCM:        frame,
			SampleRate: c.cfg.AudioParams.SampleRate,
			Channels:   c.cfg.AudioParams.Channels,
		})
	default:
		c.reportError(errors.New("unsupported xiaozhi audio format: " + format))
	}
}

func (c *Client) decodeOpus(frame []byte) ([]byte, error) {
	if c.decoder == nil {
		return nil, errors.New("opus decoder is not initialized")
	}
	sampleRate := c.cfg.AudioParams.SampleRate
	if sampleRate <= 0 {
		sampleRate = 48000
	}
	maxSamples := sampleRate * opusMaxFrameDurationMs / 1000
	if maxSamples <= 0 {
		maxSamples = 5760
	}
	frameSamples := maxSamples * maxInt(1, c.cfg.AudioParams.Channels)
	pcm := make([]int16, frameSamples)
	samplesDecoded, err := c.decoder.Decode(frame, pcm)
	if err != nil {
		return nil, err
	}
	if samplesDecoded <= 0 {
		return nil, nil
	}
	pcm = pcm[:samplesDecoded*maxInt(1, c.cfg.AudioParams.Channels)]
	return pcm16ToBytes(pcm), nil
}

func pcm16ToBytes(pcm []int16) []byte {
	if len(pcm) == 0 {
		return nil
	}
	out := make([]byte, len(pcm)*2)
	for i, sample := range pcm {
		binary.LittleEndian.PutUint16(out[i*2:], uint16(sample))
	}
	return out
}

func maxInt(a int, b int) int {
	if a > b {
		return a
	}
	return b
}

func (c *Client) reportError(err error) {
	if c.callbacks.OnError != nil {
		c.callbacks.OnError(err)
	}
}

func (c *Client) isClosed() bool {
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	return closed
}

func nextBackoff(delay time.Duration) time.Duration {
	if delay >= 30*time.Second {
		return 30 * time.Second
	}
	return delay * 2
}

func intToString(value int) string {
	if value == 0 {
		return "0"
	}
	buf := [20]byte{}
	i := len(buf)
	for value > 0 {
		i--
		buf[i] = byte('0' + value%10)
		value /= 10
	}
	return string(buf[i:])
}
