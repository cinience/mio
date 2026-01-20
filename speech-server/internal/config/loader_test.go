package config

import (
	"path/filepath"
	"runtime"
	"testing"

	"speech-server/internal/asr"
)

func TestBuildVADModel(t *testing.T) {
	dir := t.TempDir()

	model := fileVADModel{
		Provider:      "ten",
		Model:         "models/ten.onnx",
		Threshold:     0.6,
		MinSilence:    0.25,
		MinSpeech:     0.12,
		MaxSpeech:     12.0,
		WindowSize:    16,
		SampleRate:    22050,
		Threads:       2,
		Device:        "cpu",
		Debug:         1,
		BufferSeconds: 4.5,
	}

	cfg, err := buildVADModel(model, dir)
	if err != nil {
		t.Fatalf("buildVADModel returned error: %v", err)
	}

	expectedPath := filepath.Clean(filepath.Join(dir, "models/ten.onnx"))
	if cfg.ModelPath != expectedPath {
		t.Fatalf("expected model path %s, got %s", expectedPath, cfg.ModelPath)
	}
	if cfg.Provider != asr.VADProviderTen {
		t.Fatalf("expected provider %s, got %s", asr.VADProviderTen, cfg.Provider)
	}
	if cfg.Threshold != float32(0.6) {
		t.Fatalf("expected threshold 0.6, got %f", cfg.Threshold)
	}
	if cfg.MinSilenceSeconds != float32(0.25) {
		t.Fatalf("expected min silence 0.25, got %f", cfg.MinSilenceSeconds)
	}
	if cfg.BufferSeconds != float32(4.5) {
		t.Fatalf("expected buffer seconds 4.5, got %f", cfg.BufferSeconds)
	}
}

func TestBuildVADModelDefaultThreads(t *testing.T) {
	dir := t.TempDir()
	cfg, err := buildVADModel(fileVADModel{
		Provider: "silero",
		Model:    "models/silero.onnx",
		Threads:  0,
	}, dir)
	if err != nil {
		t.Fatalf("buildVADModel returned error: %v", err)
	}
	want := runtime.NumCPU()
	if want < 1 {
		want = 1
	}
	if cfg.NumThreads != want {
		t.Fatalf("expected threads %d, got %d", want, cfg.NumThreads)
	}
}

func TestEnsurePrimaryProvidersVAD(t *testing.T) {
	cfg := NewDefault()
	cfg.Addr = ":8009"
	cfg.ASRProvider = "sense"
	cfg.ASRModels = map[string]asr.Config{"sense": {ModelDir: "./models"}}
	cfg.VADEnabled = true
	cfg.VADModels = map[string]asr.VADModel{"silero": {Provider: asr.VADProviderSilero, ModelPath: "./models/silero_vad.onnx"}}
	cfg.TTSModels = map[string]TTSModel{}

	if err := ensurePrimaryProviders(&cfg); err != nil {
		t.Fatalf("ensurePrimaryProviders returned error: %v", err)
	}
	if cfg.VADProvider != "silero" {
		t.Fatalf("expected vad provider silero, got %s", cfg.VADProvider)
	}
}

func TestFirstNonEmpty(t *testing.T) {
	if got := firstNonEmpty("", " ", "value", "ignored"); got != "value" {
		t.Fatalf("expected value, got %s", got)
	}
	if got := firstNonEmpty("", "  "); got != "" {
		t.Fatalf("expected empty string, got %s", got)
	}
}

func TestLoadConfigFileEnvOverride(t *testing.T) {
	t.Setenv("SPEECH_SERVER_ADDR", ":9100")
	fc, baseDir, err := loadConfigFile("")
	if err != nil {
		t.Fatalf("loadConfigFile: %v", err)
	}
	cfg := NewDefault()
	if err := mergeFileConfig(&cfg, &fc, baseDir); err != nil {
		t.Fatalf("mergeFileConfig: %v", err)
	}
	applyEnvOverrides(&cfg)
	if cfg.Addr != ":9100" {
		t.Fatalf("expected addr override :9100, got %s", cfg.Addr)
	}
}

func TestBuildTTSModelCN2ANFlag(t *testing.T) {
	dir := t.TempDir()
	model := fileTTSModel{
		AcousticModel: "./models/acoustic.onnx",
		VocoderModel:  "./models/vocos.onnx",
		Lexicon:       "./models/lexicon.txt",
		Tokens:        "./models/tokens.txt",
		CN2ANEnabled:  true,
	}

	built, err := buildTTSModel(model, dir)
	if err != nil {
		t.Fatalf("buildTTSModel returned error: %v", err)
	}

	if !built.Config.EnableCN2AN {
		t.Fatalf("expected EnableCN2AN to be true")
	}
}
