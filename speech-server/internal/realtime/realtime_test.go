package realtime

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"speech-server/internal/asr"
)

func TestRealtimeTranscription(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping realtime test in short mode")
	}

	modelDir := filepath.Join("..", "..", "models", "sherpa-onnx-sense-voice-zh-en-ja-ko-yue-2024-07-17")
	if _, err := os.Stat(modelDir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			t.Skip("sense-voice model not downloaded")
		}
		t.Fatalf("stat model dir: %v", err)
	}

	recog, err := asr.NewRecognizer(asr.Config{
		ModelDir:       modelDir,
		SampleRate:     16000,
		NumThreads:     1,
		Provider:       "cpu",
		EnableEndpoint: true,
	})
	if err != nil {
		t.Fatalf("create recognizer: %v", err)
	}
	defer recog.Close()

	server := &Server{
		recog:             recog,
		defaultClientRate: 24000,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/realtime", server.Handle)

	ts := httptest.NewServer(mux)
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/v1/realtime?intent=transcription"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			t.Fatalf("close websocket: %v", err)
		}
	}()

	// helper to read next event with timeout
	readEvent := func(deadline time.Duration) (map[string]any, error) {
		if err := conn.SetReadDeadline(time.Now().Add(deadline)); err != nil {
			return nil, err
		}
		_, msg, err := conn.ReadMessage()
		if err != nil {
			return nil, err
		}
		var out map[string]any
		if err := json.Unmarshal(msg, &out); err != nil {
			return nil, err
		}
		return out, nil
	}

	// Session created should be first event.
	if evt, err := readEvent(30 * time.Second); err != nil {
		t.Fatalf("read session.created: %v", err)
	} else if typ, _ := evt["type"].(string); typ != "session.created" {
		t.Fatalf("expected session.created, got %s", typ)
	}

	// Send session update to confirm 24k input.
	update := map[string]any{
		"type": "session.update",
		"session": map[string]any{
			"type": "transcription",
			"audio": map[string]any{
				"input": map[string]any{
					"format": map[string]any{
						"type": "audio/pcm",
						"rate": 24000,
					},
				},
			},
		},
	}
	if err := conn.WriteJSON(update); err != nil {
		t.Fatalf("send session.update: %v", err)
	}

	if evt, err := readEvent(30 * time.Second); err != nil {
		t.Fatalf("wait session.updated: %v", err)
	} else if typ, _ := evt["type"].(string); typ != "session.updated" {
		t.Fatalf("expected session.updated, got %s", typ)
	}

	// Generate half a second of silence audio.
	audio := make([]byte, 24000/2*2) // 0.5s * 2 bytes per sample
	encoded := base64.StdEncoding.EncodeToString(audio)

	appendPayload := map[string]any{
		"type":  "input_audio_buffer.append",
		"audio": encoded,
	}
	if err := conn.WriteJSON(appendPayload); err != nil {
		t.Fatalf("send audio: %v", err)
	}

	if err := conn.WriteJSON(map[string]any{"type": "input_audio_buffer.commit"}); err != nil {
		t.Fatalf("send commit: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	var seenTranscription bool
	for !seenTranscription {
		if ctx.Err() != nil {
			t.Fatal("timeout waiting for transcription")
		}
		evt, err := readEvent(30 * time.Second)
		if err != nil {
			t.Fatalf("read event: %v", err)
		}
		switch evt["type"] {
		case "input_audio_buffer.committed":
			// ok
		case "conversation.item.added":
			// ok
		case "conversation.item.done":
			// ok
		case "conversation.item.input_audio_transcription.completed":
			seenTranscription = true
		case "input_audio_buffer.cleared":
			seenTranscription = true
		}
	}
}
