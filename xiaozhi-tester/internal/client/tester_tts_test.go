package client

import (
	"context"
	"errors"
	"io"
	"log"
	"testing"
	"time"
)

func newTesterForTTS() *Tester {
	return &Tester{
		cfg: Config{
			FrameDuration:  20,
			TTSStopTimeout: time.Second,
		},
		logger:      log.New(io.Discard, "", 0),
		handshakeCh: make(chan handshakeMessage, 1),
		readErrCh:   make(chan error, 1),
		ttsStopCh:   make(chan struct{}, 8),
	}
}

func TestRecordTTSEventStopSignals(t *testing.T) {
	tester := newTesterForTTS()

	tester.recordTTSEvent("start")
	if !tester.ttsActive {
		t.Fatalf("expected ttsActive after start")
	}

	tester.recordTTSEvent("stop")
	if tester.ttsActive {
		t.Fatalf("expected ttsActive to be false after stop")
	}

	select {
	case <-tester.ttsStopCh:
	case <-time.After(100 * time.Millisecond):
		t.Fatalf("expected stop signal to be published")
	}
}

func TestHandleAudioFrameTriggersFallback(t *testing.T) {
	tester := newTesterForTTS()
	tester.setActiveTurn(conversationTurn{alias: "turn-1", index: 1})
	tester.markUploadComplete()
	alias, idx, frames, total, first, rt := tester.handleAudioFrame(160)
	if alias != "turn-1" || idx != 1 {
		t.Fatalf("unexpected turn info alias=%s idx=%d", alias, idx)
	}
	if frames != 1 || total != 160 {
		t.Fatalf("unexpected frame stats frames=%d total=%d", frames, total)
	}
	if !first {
		t.Fatalf("expected first frame flag")
	}
	if rt < 0 {
		t.Fatalf("unexpected negative rt: %v", rt)
	}
	if !tester.ttsActive {
		t.Fatalf("expected audio frame to mark ttsActive")
	}

	tester.emitFallbackStop("audio_silence")

	if tester.ttsActive {
		t.Fatalf("expected fallback to clear ttsActive")
	}

	select {
	case <-tester.ttsStopCh:
	case <-time.After(100 * time.Millisecond):
		t.Fatalf("expected fallback to send stop signal")
	}

	tester.stopTTSFallback()
}

func TestWaitForTTSEndBenignClose(t *testing.T) {
	tester := newTesterForTTS()

	go func() {
		tester.readErrCh <- errors.New("use of closed network connection")
	}()

	if err := tester.waitForTTSEnd(context.Background()); err != nil {
		t.Fatalf("expected benign close to resolve without error, got %v", err)
	}
}
