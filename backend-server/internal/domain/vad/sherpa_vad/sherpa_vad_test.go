//go:build sherpa_onnx
// +build sherpa_onnx

package sherpa_vad

import (
	"testing"

	sherpa "github.com/k2-fsa/sherpa-onnx-go/sherpa_onnx"
)

func TestParseConfigValidation(t *testing.T) {
	_, err := parseConfig(map[string]interface{}{"sample_rate": -1})
	if err == nil {
		t.Fatal("expected error for negative sample rate")
	}

	_, err = parseConfig(map[string]interface{}{
		"sample_rate":          16000,
		"threshold":            1.5,
		"min_speech_duration":  0.2,
		"max_speech_duration":  0.1,
		"min_silence_duration": -0.1,
	})
	if err == nil {
		t.Fatal("expected validation error for invalid durations/threshold")
	}
}

func TestIsVADExtRejectsSampleRateMismatch(t *testing.T) {
	vad := &SherpaVAD{cfg: SherpaConfig{SampleRate: 16000}}
	if _, err := vad.IsVADExt([]float32{0}, 8000, 0); err == nil {
		t.Fatal("expected mismatch error when providing wrong sample rate")
	}
}

func TestPoolKeyForConfigDeterministic(t *testing.T) {
	cfg := SherpaConfig{SampleRate: 16000, ModelPath: "/tmp/model.onnx", NumThreads: 2, Debug: 1, Threshold: 0.5, MinSilenceDuration: 0.5, MinSpeechDuration: 0.25, MaxSpeechDuration: 0.0, BufferSizeSeconds: 5, WindowSize: 512, Provider: "cpu"}
	keyA := poolKeyForConfig(cfg)
	keyB := poolKeyForConfig(cfg)
	if keyA != keyB {
		t.Fatalf("expected deterministic key, got %q vs %q", keyA, keyB)
	}

	cfg.WindowSize = 1024
	keyC := poolKeyForConfig(cfg)
	if keyA == keyC {
		t.Fatal("expected key to change when config changes")
	}
}

func TestReturnToPoolAndReuse(t *testing.T) {
	cfg := SherpaConfig{SampleRate: 16000, ModelPath: "/tmp/model.onnx", WindowSize: 512, BufferSizeSeconds: 5}
	key := poolKeyForConfig(cfg)
	buf := sherpa.NewCircularBuffer(2048)
	if buf == nil {
		t.Fatal("expected circular buffer to be created")
	}
	vad := &SherpaVAD{cfg: cfg, poolKey: key, buffer: buf, windowSize: cfg.WindowSize}
	if !vad.returnToPool() {
		t.Fatal("expected instance to return to pool")
	}

	reused := takeFromPool(key)
	if reused == nil {
		t.Fatal("expected to reuse pooled instance")
	}
	if reused != vad {
		t.Fatal("expected pooled instance to be reused")
	}

	poolMu.Lock()
	delete(poolMap, key)
	poolMu.Unlock()
}
