package client

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/godeps/opus"
	"github.com/gorilla/websocket"

	"xiaozhi-tester/internal/audio"
	"xiaozhi-tester/internal/util"
)

// Config carries runtime options for the automated tester.
type Config struct {
	URL            string
	Token          string
	DeviceID       string
	ClientID       string
	Mode           string
	WakeWord       string
	FrameDuration  int
	SampleRate     int
	Channels       int
	AudioPaths     []string
	Realtime       bool
	Repeat         int
	Gap            time.Duration
	WaitAfter      time.Duration
	EnableMCP      bool
	InsecureTLS    bool
	AutoContinue   bool
	TTSStopTimeout time.Duration
	TextMode       bool
	TextCount      int
	TextFile       string
}

type handshakeMessage struct {
	sessionID string
	params    *AudioParams
}

// AudioParams represent audio configuration exchanged in hello messages.
type AudioParams struct {
	Format        string `json:"format"`
	SampleRate    int    `json:"sample_rate"`
	Channels      int    `json:"channels"`
	FrameDuration int    `json:"frame_duration"`
}

// ServerMessage mirrors the server JSON payloads we care about.
type ServerMessage struct {
	Type        string          `json:"type"`
	Text        string          `json:"text"`
	SessionID   string          `json:"session_id"`
	State       string          `json:"state"`
	Transport   string          `json:"transport"`
	AudioParams *AudioParams    `json:"audio_params"`
	Emotion     string          `json:"emotion"`
	Payload     json.RawMessage `json:"payload"`
}

type conversationTurn struct {
	asset *audio.Asset
	alias string
	text  string
	index int
}

// Tester manages the end-to-end lifecycle of a test run.
type Tester struct {
	cfg                  Config
	conn                 *websocket.Conn
	logger               *log.Logger
	handshakeCh          chan handshakeMessage
	readErrCh            chan error
	ttsStopCh            chan struct{}
	sessionMu            sync.RWMutex
	sessionID            string
	serverParams         *AudioParams
	wg                   sync.WaitGroup
	turns                []conversationTurn
	ttsMu                sync.Mutex
	ttsActive            bool
	ttsSeenAny           bool
	ttsLastEvent         time.Time
	ttsLastState         string
	ttsFallback          *time.Timer
	turnMu               sync.RWMutex
	activeTurnAlias      string
	activeTurnIndex      int
	turnFrameCount       int
	turnFrameBytes       int
	turnUploadDone       time.Time
	turnFirstFrameLogged bool
}

var validModes = map[string]bool{
	"auto":     true,
	"manual":   true,
	"realtime": true,
}

var defaultPrompts = []string{
	"今天天气不错，帮我推荐一个适合户外的活动吧。",
	"我想做一道简单的晚餐，有什么建议吗？",
	"请讲一个简短的冷笑话让我开心一下。",
	"最近有哪些值得一看的科幻电影？",
	"帮我拟一个两个小时的学习计划，提高效率。",
	"解释一下量子计算和传统计算机的差异。",
	"给我一个每天坚持早起的五点建议。",
	"整理一份去杭州旅游的三天行程。",
	"写一段鼓励团队合作的简短寄语。",
	"如何在家里打造一个舒适的阅读角？",
}

// New validates the configuration, fills defaults, and prepares a Tester.
func New(cfg Config) (*Tester, error) {
	if cfg.URL == "" {
		return nil, errors.New("URL must not be empty")
	}
	if !cfg.TextMode && len(cfg.AudioPaths) == 0 {
		return nil, errors.New("at least one audio path is required (or enable text mode)")
	}
	if !validModes[cfg.Mode] {
		return nil, fmt.Errorf("unsupported listen mode %q", cfg.Mode)
	}
	if cfg.FrameDuration != 10 && cfg.FrameDuration != 20 && cfg.FrameDuration != 40 && cfg.FrameDuration != 60 {
		return nil, fmt.Errorf("invalid frame duration %d", cfg.FrameDuration)
	}
	if cfg.TextMode {
		if cfg.TextCount <= 0 {
			cfg.TextCount = 5
		}
		cfg.Channels = 1
		cfg.Realtime = false
	} else if cfg.Channels != 1 {
		return nil, fmt.Errorf("only mono audio is supported, got %d channels", cfg.Channels)
	}
	if cfg.Repeat <= 0 {
		cfg.Repeat = 1
	}
	if cfg.TTSStopTimeout <= 0 {
		cfg.TTSStopTimeout = 45 * time.Second
	}
	if cfg.DeviceID == "" {
		mac, err := util.RandomMAC()
		if err != nil {
			return nil, err
		}
		cfg.DeviceID = mac
	}
	if cfg.ClientID == "" {
		id, err := util.RandomUUID()
		if err != nil {
			return nil, err
		}
		cfg.ClientID = id
	}

	logger := log.New(os.Stdout, "[tester] ", log.LstdFlags|log.Lmicroseconds)

	return &Tester{
		cfg:         cfg,
		logger:      logger,
		handshakeCh: make(chan handshakeMessage, 1),
		readErrCh:   make(chan error, 1),
		ttsStopCh:   make(chan struct{}, 16),
	}, nil
}

// Run executes the configured test against the backend server.
func (t *Tester) Run(ctx context.Context) error {
	if err := t.prepareTurns(); err != nil {
		return err
	}

	if err := t.connect(ctx); err != nil {
		return err
	}
	defer t.close()

	if err := t.sendHello(); err != nil {
		return err
	}
	if err := t.awaitHello(ctx); err != nil {
		return err
	}

	var err error
	if t.cfg.Mode == "auto" && t.cfg.AutoContinue && len(t.turns) > 1 {
		err = t.runAutoTurns(ctx)
	} else {
		err = t.runSequentialTurns(ctx)
	}
	if err != nil {
		return err
	}

	if t.cfg.WaitAfter > 0 {
		t.logger.Printf("waiting %s for downstream responses", t.cfg.WaitAfter)
		if err := t.sleep(ctx, t.cfg.WaitAfter); err != nil {
			return err
		}
	}

	_ = t.sendGoodbye()
	return nil
}

func (t *Tester) prepareTurns() error {
	if t.cfg.TextMode {
		prompts, err := t.loadPrompts()
		if err != nil {
			return err
		}
		turnIdx := 1
		for r := 0; r < t.cfg.Repeat; r++ {
			for _, prompt := range prompts {
				alias := fmt.Sprintf("turn-%d:text", turnIdx)
				t.turns = append(t.turns, conversationTurn{alias: alias, text: prompt, index: turnIdx})
				turnIdx++
			}
		}
		return nil
	}

	cache := make(map[string]*audio.Asset)
	turnIdx := 1

	for r := 0; r < t.cfg.Repeat; r++ {
		for _, path := range t.cfg.AudioPaths {
			asset, ok := cache[path]
			if !ok {
				loaded, err := audio.LoadAsset(path, t.cfg.SampleRate)
				if err != nil {
					return fmt.Errorf("load audio %s: %w", path, err)
				}
				cache[path] = loaded
				asset = loaded
				t.logger.Printf("loaded %s (src %dHz/%dch -> %dHz mono, %.2fs)",
					path, asset.SourceSampleRate, asset.SourceNumChannels, asset.SampleRate, asset.DurationSeconds)
			}
			alias := fmt.Sprintf("turn-%d:%s", turnIdx, path)
			t.turns = append(t.turns, conversationTurn{asset: asset, alias: alias, index: turnIdx})
			turnIdx++
		}
	}

	return nil
}

func (t *Tester) loadPrompts() ([]string, error) {
	var prompts []string
	if strings.TrimSpace(t.cfg.TextFile) != "" {
		file, err := os.Open(t.cfg.TextFile)
		if err != nil {
			return nil, fmt.Errorf("open text file %s: %w", t.cfg.TextFile, err)
		}
		defer file.Close()

		scanner := bufio.NewScanner(file)
		buf := make([]byte, 0, 64*1024)
		scanner.Buffer(buf, 512*1024)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line != "" {
				prompts = append(prompts, line)
			}
		}
		if err := scanner.Err(); err != nil {
			return nil, fmt.Errorf("scan text file %s: %w", t.cfg.TextFile, err)
		}
	} else {
		prompts = append(prompts, defaultPrompts...)
	}

	if len(prompts) == 0 {
		return nil, errors.New("no text prompts available")
	}

	seen := make(map[string]struct{}, len(prompts))
	filtered := make([]string, 0, len(prompts))
	for _, p := range prompts {
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		filtered = append(filtered, p)
	}

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	rng.Shuffle(len(filtered), func(i, j int) {
		filtered[i], filtered[j] = filtered[j], filtered[i]
	})

	count := t.cfg.TextCount
	if count <= 0 || count > len(filtered) {
		count = len(filtered)
	}

	return filtered[:count], nil
}

func (t *Tester) runSequentialTurns(ctx context.Context) error {
	for idx, turn := range t.turns {
		if idx > 0 && t.cfg.Gap > 0 {
			if err := t.sleep(ctx, t.cfg.Gap); err != nil {
				return err
			}
		}

		if t.cfg.WakeWord != "" && !t.cfg.TextMode {
			if err := t.sendListenDetect(t.cfg.WakeWord); err != nil {
				return err
			}
		}

		if err := t.startTurn(ctx, turn); err != nil {
			return err
		}
		if err := t.sendListenStop(); err != nil {
			return err
		}
	}

	return nil
}

func (t *Tester) runAutoTurns(ctx context.Context) error {
	if len(t.turns) == 0 {
		return errors.New("no conversation turns configured")
	}

	if t.cfg.WakeWord != "" && !t.cfg.TextMode {
		if err := t.sendListenDetect(t.cfg.WakeWord); err != nil {
			return err
		}
	}

	if err := t.startTurn(ctx, t.turns[0]); err != nil {
		return err
	}
	if err := t.sendListenStop(); err != nil {
		return err
	}

	for idx := 1; idx < len(t.turns); idx++ {
		if err := t.waitForTTSEnd(ctx); err != nil {
			return err
		}
		if t.cfg.Gap > 0 {
			if err := t.sleep(ctx, t.cfg.Gap); err != nil {
				return err
			}
		}
		if err := t.startTurn(ctx, t.turns[idx]); err != nil {
			return err
		}
		if err := t.sendListenStop(); err != nil {
			return err
		}
	}

	// Wait once more so the final assistant reply can finish before teardown.
	if err := t.waitForTTSEnd(ctx); err != nil {
		return err
	}

	return nil
}

func (t *Tester) waitForTTSEnd(ctx context.Context) error {
	timeout := t.cfg.TTSStopTimeout
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-t.readErrCh:
		if err == nil {
			err = errors.New("connection closed")
		}
		if isBenignClose(err) {
			t.logger.Printf("connection closed while waiting for TTS stop (%v)", err)
			return nil
		}
		return err
	case <-t.ttsStopCh:
		return nil
	case <-timer.C:
		snapshot := t.ttsStateSnapshot()
		return fmt.Errorf("waiting for TTS stop timed out after %s (last state: %s)", timeout, snapshot)
	}
}

func (t *Tester) startTurn(ctx context.Context, turn conversationTurn) error {
	t.resetTTSState()
	t.setActiveTurn(turn)
	t.logger.Printf("starting %s", turn.alias)
	if err := t.sendListenStart(); err != nil {
		return err
	}
	if t.cfg.TextMode {
		if err := t.sendListenDetect(turn.text); err != nil {
			return err
		}
		t.markUploadComplete()
		return nil
	}
	if err := t.streamAudio(ctx, turn.asset); err != nil {
		return err
	}
	t.markUploadComplete()
	return nil
}

func (t *Tester) resetTTSState() {
	for {
		select {
		case <-t.ttsStopCh:
			continue
		default:
		}
		break
	}

	t.ttsMu.Lock()
	if t.ttsFallback != nil {
		t.ttsFallback.Stop()
		t.ttsFallback = nil
	}
	t.ttsActive = false
	t.ttsSeenAny = false
	t.ttsLastEvent = time.Time{}
	t.ttsLastState = ""
	t.ttsMu.Unlock()
}

func (t *Tester) setActiveTurn(turn conversationTurn) {
	t.turnMu.Lock()
	t.activeTurnAlias = turn.alias
	t.activeTurnIndex = turn.index
	t.turnFrameCount = 0
	t.turnFrameBytes = 0
	t.turnUploadDone = time.Time{}
	t.turnFirstFrameLogged = false
	t.turnMu.Unlock()
}

func (t *Tester) markUploadComplete() {
	now := time.Now()
	t.turnMu.Lock()
	t.turnUploadDone = now
	t.turnFirstFrameLogged = false
	t.turnMu.Unlock()
}

func (t *Tester) handleAudioFrame(size int) (string, int, int, int, bool, time.Duration) {
	alias, index, frameCount, totalBytes, firstFrame, firstRT := t.recordTurnFrame(size)
	if size <= 0 {
		return alias, index, frameCount, totalBytes, firstFrame, firstRT
	}

	now := time.Now()
	t.ttsMu.Lock()
	t.ttsSeenAny = true
	t.ttsActive = true
	t.ttsLastEvent = now
	t.ttsLastState = "audio"
	delay := t.silenceDelayLocked()
	t.scheduleFallbackLocked(delay, "audio_silence")
	t.ttsMu.Unlock()

	return alias, index, frameCount, totalBytes, firstFrame, firstRT
}

func (t *Tester) recordTurnFrame(size int) (alias string, index int, frameCount int, totalBytes int, firstFrame bool, firstRT time.Duration) {
	if size < 0 {
		size = 0
	}
	t.turnMu.Lock()
	alias = t.activeTurnAlias
	index = t.activeTurnIndex
	t.turnFrameCount++
	t.turnFrameBytes += size
	frameCount = t.turnFrameCount
	totalBytes = t.turnFrameBytes
	if frameCount == 1 && !t.turnUploadDone.IsZero() && !t.turnFirstFrameLogged {
		firstFrame = true
		firstRT = time.Since(t.turnUploadDone)
		if firstRT < 0 {
			firstRT = 0
		}
		t.turnFirstFrameLogged = true
	}
	t.turnMu.Unlock()
	return
}

func (t *Tester) recordTTSEvent(state string) {
	normalized := strings.ToLower(strings.TrimSpace(state))
	now := time.Now()

	var scheduleReason string
	var scheduleDelay time.Duration
	shouldSignalStop := false

	t.ttsMu.Lock()
	t.ttsSeenAny = true
	t.ttsLastEvent = now
	if normalized != "" {
		t.ttsLastState = normalized
	}

	switch normalized {
	case "", "start", "sentence_start":
		t.ttsActive = true
		t.cancelFallbackLocked()
	case "stop", "stopped", "done", "end", "ended", "complete", "completed", "success":
		t.ttsActive = false
		t.cancelFallbackLocked()
		shouldSignalStop = true
	case "sentence_end":
		if !t.ttsActive {
			t.ttsActive = true
		}
		scheduleReason = "sentence_end"
		scheduleDelay = t.silenceDelayLocked()
	default:
		if !t.ttsActive {
			t.ttsActive = true
		}
	}

	t.ttsMu.Unlock()

	if scheduleReason != "" && scheduleDelay > 0 {
		t.scheduleSilenceFallback(scheduleReason, scheduleDelay)
	}

	if shouldSignalStop {
		select {
		case t.ttsStopCh <- struct{}{}:
		default:
		}
	}
}

func (t *Tester) scheduleSilenceFallback(reason string, delay time.Duration) {
	t.ttsMu.Lock()
	t.scheduleFallbackLocked(delay, reason)
	t.ttsMu.Unlock()
}

func (t *Tester) scheduleFallbackLocked(delay time.Duration, reason string) {
	if delay <= 0 {
		delay = 1200 * time.Millisecond
	}
	if t.ttsFallback != nil {
		t.ttsFallback.Stop()
	}
	reasonCopy := reason
	t.ttsFallback = time.AfterFunc(delay, func() {
		t.emitFallbackStop(reasonCopy)
	})
}

func (t *Tester) cancelFallbackLocked() {
	if t.ttsFallback != nil {
		t.ttsFallback.Stop()
		t.ttsFallback = nil
	}
}

func (t *Tester) silenceDelayLocked() time.Duration {
	frame := time.Duration(t.cfg.FrameDuration) * time.Millisecond
	if frame <= 0 {
		frame = 20 * time.Millisecond
	}
	delay := frame * 6
	if delay < 1200*time.Millisecond {
		delay = 1200 * time.Millisecond
	}
	if delay > 5*time.Second {
		delay = 5 * time.Second
	}
	return delay
}

func (t *Tester) emitFallbackStop(reason string) {
	t.ttsMu.Lock()
	if !t.ttsActive {
		t.ttsMu.Unlock()
		return
	}
	t.ttsActive = false
	t.ttsLastState = reason
	t.ttsLastEvent = time.Now()
	t.ttsFallback = nil
	t.ttsMu.Unlock()

	select {
	case t.ttsStopCh <- struct{}{}:
	default:
	}
}

func (t *Tester) ttsStateSnapshot() string {
	t.ttsMu.Lock()
	state := t.ttsLastState
	active := t.ttsActive
	seen := t.ttsSeenAny
	last := t.ttsLastEvent
	t.ttsMu.Unlock()

	if state == "" {
		state = "none"
	}
	var since string
	if last.IsZero() {
		since = "never"
	} else {
		since = time.Since(last).Round(100 * time.Millisecond).String()
	}
	return fmt.Sprintf("%s (active=%t, seen=%t, idle=%s)", state, active, seen, since)
}

func (t *Tester) stopTTSFallback() {
	t.ttsMu.Lock()
	if t.ttsFallback != nil {
		t.ttsFallback.Stop()
		t.ttsFallback = nil
	}
	t.ttsMu.Unlock()
}

func isBenignClose(err error) bool {
	if err == nil {
		return false
	}
	var closeErr *websocket.CloseError
	if errors.As(err, &closeErr) {
		switch closeErr.Code {
		case websocket.CloseNormalClosure, websocket.CloseGoingAway:
			return true
		}
	}
	if errors.Is(err, net.ErrClosed) {
		return true
	}
	if strings.Contains(err.Error(), "use of closed network connection") {
		return true
	}
	return false
}

func (t *Tester) connect(ctx context.Context) error {
	dialer := websocket.Dialer{
		HandshakeTimeout:  10 * time.Second,
		EnableCompression: false,
		TLSClientConfig:   &tls.Config{InsecureSkipVerify: t.cfg.InsecureTLS},
	}

	header := http.Header{}
	header.Set("Protocol-Version", "1")
	header.Set("Device-Id", t.cfg.DeviceID)
	header.Set("Client-Id", t.cfg.ClientID)
	if t.cfg.Token != "" {
		header.Set("Authorization", "Bearer "+t.cfg.Token)
	}

	conn, resp, err := dialer.DialContext(ctx, t.cfg.URL, header)
	if resp != nil {
		resp.Body.Close()
	}
	if err != nil {
		return fmt.Errorf("dial websocket: %w", err)
	}

	t.conn = conn
	t.wg.Add(1)
	go t.readLoop()
	t.logger.Printf("connected to %s", t.cfg.URL)
	return nil
}

func (t *Tester) readLoop() {
	defer t.wg.Done()
	for {
		msgType, data, err := t.conn.ReadMessage()
		if err != nil {
			select {
			case t.readErrCh <- err:
			default:
			}
			if !errors.Is(err, websocket.ErrCloseSent) {
				t.logger.Printf("read error: %v", err)
			}
			return
		}

		switch msgType {
		case websocket.TextMessage:
			t.handleTextMessage(data)
		case websocket.BinaryMessage:
			alias, idx, frames, total, firstFrame, firstRT := t.handleAudioFrame(len(data))
			if alias == "" {
				alias = "-"
			}
			rtField := "-"
			if firstFrame {
				rtField = firstRT.Round(time.Millisecond).String()
			}
			t.logger.Printf("<- audio frame (%d bytes) turn=%d alias=%s frame=%d total=%d bytes first_rt=%s", len(data), idx, alias, frames, total, rtField)
		default:
			t.logger.Printf("<- message type %d (%d bytes)", msgType, len(data))
		}
	}
}

func (t *Tester) handleTextMessage(data []byte) {
	var msg ServerMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		t.logger.Printf("invalid json: %s (%v)", string(data), err)
		return
	}

	switch msg.Type {
	case "hello":
		t.sessionMu.Lock()
		t.sessionID = msg.SessionID
		t.sessionMu.Unlock()
		t.serverParams = msg.AudioParams

		select {
		case t.handshakeCh <- handshakeMessage{sessionID: msg.SessionID, params: msg.AudioParams}:
		default:
		}
		t.logger.Printf("<- hello ack session=%s transport=%s %+v", msg.SessionID, msg.Transport, msg.AudioParams)
	case "tts":
		t.recordTTSEvent(msg.State)
		t.logger.Printf("<- tts state=%s text=%q", msg.State, msg.Text)
	case "stt", "llm", "text":
		t.logger.Printf("<- %s state=%s text=%q", msg.Type, msg.State, msg.Text)
	default:
		t.logger.Printf("<- %s state=%s payload=%s", msg.Type, msg.State, string(msg.Payload))
	}
}

func (t *Tester) sendHello() error {
	payload := map[string]any{
		"type":      "hello",
		"version":   1,
		"transport": "websocket",
		"audio_params": map[string]any{
			"format":         "opus",
			"sample_rate":    t.cfg.SampleRate,
			"channels":       t.cfg.Channels,
			"frame_duration": t.cfg.FrameDuration,
		},
	}
	if t.cfg.EnableMCP {
		payload["features"] = map[string]bool{"mcp": true}
	}
	return t.sendJSON(payload)
}

func (t *Tester) awaitHello(ctx context.Context) error {
	timeoutCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	select {
	case msg := <-t.handshakeCh:
		t.logger.Printf("server session=%s audio=%+v", msg.sessionID, msg.params)
		return nil
	case err := <-t.readErrCh:
		if err == nil {
			err = errors.New("connection closed")
		}
		return fmt.Errorf("waiting hello failed: %w", err)
	case <-timeoutCtx.Done():
		return fmt.Errorf("waiting hello failed: %w", timeoutCtx.Err())
	}
}

func (t *Tester) sendListenDetect(text string) error {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	payload := map[string]any{
		"type":       "listen",
		"session_id": t.sessionIDSafe(),
		"state":      "detect",
		"text":       text,
	}
	return t.sendJSON(payload)
}

func (t *Tester) sendListenStart() error {
	payload := map[string]any{
		"type":       "listen",
		"session_id": t.sessionIDSafe(),
		"state":      "start",
		"mode":       t.cfg.Mode,
	}
	return t.sendJSON(payload)
}

func (t *Tester) sendListenStop() error {
	payload := map[string]any{
		"type":       "listen",
		"session_id": t.sessionIDSafe(),
		"state":      "stop",
	}
	return t.sendJSON(payload)
}

func (t *Tester) sendGoodbye() error {
	payload := map[string]any{
		"type":       "goodbye",
		"session_id": t.sessionIDSafe(),
	}
	return t.sendJSON(payload)
}

func (t *Tester) streamAudio(ctx context.Context, asset *audio.Asset) error {
	frameSize := t.cfg.SampleRate * t.cfg.FrameDuration / 1000
	if frameSize <= 0 {
		return fmt.Errorf("invalid frame size derived from sampleRate=%d frameDuration=%d", t.cfg.SampleRate, t.cfg.FrameDuration)
	}

	enc, err := newOpusEncoder(t.cfg.SampleRate, t.cfg.Channels, t.cfg.FrameDuration)
	if err != nil {
		return err
	}
	defer enc.Close()

	frameBuf := make([]int16, frameSize)
	totalSamples := len(asset.Samples)
	totalFrames := (totalSamples + frameSize - 1) / frameSize
	t.logger.Printf("-> uploading %d frames (%.2fs) from %s", totalFrames, asset.DurationSeconds, asset.Path)

	var offset int
	for frame := 0; frame < totalFrames; frame++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		copyCount := min(len(frameBuf), totalSamples-offset)
		for i := 0; i < copyCount; i++ {
			frameBuf[i] = asset.Samples[offset+i]
		}
		for i := copyCount; i < len(frameBuf); i++ {
			frameBuf[i] = 0
		}
		offset += copyCount

		encoded, err := enc.Encode(frameBuf)
		if err != nil {
			return fmt.Errorf("encode frame %d: %w", frame, err)
		}
		if len(encoded) == 0 {
			continue
		}

		if err := t.conn.WriteMessage(websocket.BinaryMessage, encoded); err != nil {
			return fmt.Errorf("send audio frame: %w", err)
		}

		if t.cfg.Realtime {
			if err := t.sleep(ctx, time.Duration(t.cfg.FrameDuration)*time.Millisecond); err != nil {
				return err
			}
		}
	}

	return nil
}

func (t *Tester) sendJSON(payload any) error {
	if t.conn == nil {
		return errors.New("connection not established")
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	if err := t.conn.WriteMessage(websocket.TextMessage, data); err != nil {
		return fmt.Errorf("send json: %w", err)
	}
	t.logger.Printf("-> %s", data)
	return nil
}

func (t *Tester) sessionIDSafe() string {
	t.sessionMu.RLock()
	defer t.sessionMu.RUnlock()
	return t.sessionID
}

func (t *Tester) close() {
	t.stopTTSFallback()
	if t.conn != nil {
		_ = t.conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "bye"))
		_ = t.conn.Close()
	}
	t.wg.Wait()
}

func (t *Tester) sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

type opusEncoder struct {
	enc    *opus.Encoder
	buffer []byte
	frame  int
}

func newOpusEncoder(sampleRate, channels, frameDuration int) (*opusEncoder, error) {
	if channels != 1 {
		return nil, fmt.Errorf("only mono channels supported, got %d", channels)
	}
	enc, err := opus.NewEncoder(sampleRate, channels, opus.AppAudio)
	if err != nil {
		return nil, fmt.Errorf("create opus encoder: %w", err)
	}
	frameSize := sampleRate * frameDuration / 1000
	if frameSize <= 0 {
		frameSize = sampleRate / 50
	}
	return &opusEncoder{
		enc:    enc,
		buffer: make([]byte, 4000),
		frame:  frameSize,
	}, nil
}

func (e *opusEncoder) Encode(samples []int16) ([]byte, error) {
	if len(samples) != e.frame {
		return nil, fmt.Errorf("expected %d samples, got %d", e.frame, len(samples))
	}
	n, err := e.enc.Encode(samples, e.buffer)
	if err != nil {
		return nil, err
	}
	out := make([]byte, n)
	copy(out, e.buffer[:n])
	return out, nil
}

func (e *opusEncoder) Close() error {
	e.enc = nil
	e.buffer = nil
	return nil
}
