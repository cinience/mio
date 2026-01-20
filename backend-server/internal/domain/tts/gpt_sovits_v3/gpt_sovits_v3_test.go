package gpt_sovits_v3

import (
	"context"
	"testing"
	"time"
)

func TestNewGptSovitsV3TTSProvider(t *testing.T) {
	config := map[string]interface{}{
		"url":             "http://localhost:9880",
		"refer_wav_path":  "test.wav",
		"prompt_text":     "测试提示文本",
		"prompt_language": "zh",
		"text_language":   "zh",
		"top_k":           float64(10),
		"top_p":           0.8,
		"temperature":     0.9,
		"sample_steps":    float64(100),
		"speed":           1.2,
		"cut_punc":        "。？！",
		"inp_refs":        "ref1,ref2",
		"if_sr":           true,
		"audio_file_type": "mp3",
	}

	provider := NewGptSovitsV3TTSProvider(config)

	if provider.URL != "http://localhost:9880" {
		t.Errorf("Expected URL to be 'http://localhost:9880', got '%s'", provider.URL)
	}
	if provider.ReferWavPath != "test.wav" {
		t.Errorf("Expected ReferWavPath to be 'test.wav', got '%s'", provider.ReferWavPath)
	}
	if provider.PromptText != "测试提示文本" {
		t.Errorf("Expected PromptText to be '测试提示文本', got '%s'", provider.PromptText)
	}
	if provider.PromptLanguage != "zh" {
		t.Errorf("Expected PromptLanguage to be 'zh', got '%s'", provider.PromptLanguage)
	}
	if provider.TextLanguage != "zh" {
		t.Errorf("Expected TextLanguage to be 'zh', got '%s'", provider.TextLanguage)
	}
	if provider.TopK != 10 {
		t.Errorf("Expected TopK to be 10, got %d", provider.TopK)
	}
	if provider.TopP != 0.8 {
		t.Errorf("Expected TopP to be 0.8, got %f", provider.TopP)
	}
	if provider.Temperature != 0.9 {
		t.Errorf("Expected Temperature to be 0.9, got %f", provider.Temperature)
	}
	if provider.SampleSteps != 100 {
		t.Errorf("Expected SampleSteps to be 100, got %d", provider.SampleSteps)
	}
	if provider.Speed != 1.2 {
		t.Errorf("Expected Speed to be 1.2, got %f", provider.Speed)
	}
	if provider.CutPunc != "。？！" {
		t.Errorf("Expected CutPunc to be '。？！', got '%s'", provider.CutPunc)
	}
	if provider.InpRefs != "ref1,ref2" {
		t.Errorf("Expected InpRefs to be 'ref1,ref2', got '%s'", provider.InpRefs)
	}
	if provider.IfSr != true {
		t.Errorf("Expected IfSr to be true, got %v", provider.IfSr)
	}
	if provider.AudioFileType != "mp3" {
		t.Errorf("Expected AudioFileType to be 'mp3', got '%s'", provider.AudioFileType)
	}
}

func TestGptSovitsV3TTSProvider_GetVoiceInfo(t *testing.T) {
	config := map[string]interface{}{
		"url":             "http://localhost:9880",
		"refer_wav_path":  "demo.wav",
		"prompt_text":     "你好",
		"prompt_language": "zh",
		"text_language":   "zh",
	}

	provider := NewGptSovitsV3TTSProvider(config)
	voiceInfo := provider.GetVoiceInfo()

	if voiceInfo["type"] != "gpt_sovits_v3" {
		t.Errorf("Expected type to be 'gpt_sovits_v3', got '%v'", voiceInfo["type"])
	}
	if voiceInfo["url"] != "http://localhost:9880" {
		t.Errorf("Expected url to be 'http://localhost:9880', got '%v'", voiceInfo["url"])
	}
	if voiceInfo["refer_wav_path"] != "demo.wav" {
		t.Errorf("Expected refer_wav_path to be 'demo.wav', got '%v'", voiceInfo["refer_wav_path"])
	}
}

// 注意：这个测试需要实际运行的GPT-SoVITS V3服务，通常在CI环境中会跳过
func TestGptSovitsV3TTSProvider_TextToSpeech(t *testing.T) {
	t.Skip("跳过需要实际GPT-SoVITS V3服务的测试")

	config := map[string]interface{}{
		"url":             "http://127.0.0.1:9880",
		"refer_wav_path":  "demo.wav",
		"prompt_text":     "你好，这是测试",
		"prompt_language": "zh",
		"text_language":   "zh",
	}

	provider := NewGptSovitsV3TTSProvider(config)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	frames, err := provider.TextToSpeech(ctx, "你好，这是一个测试。", 24000, 1, 20)
	if err != nil {
		t.Fatalf("TextToSpeech failed: %v", err)
	}

	if len(frames) == 0 {
		t.Error("Expected non-empty frames")
	}

	t.Logf("Generated %d audio frames", len(frames))
}

// 测试流式接口
func TestGptSovitsV3TTSProvider_TextToSpeechStream(t *testing.T) {
	t.Skip("跳过需要实际GPT-SoVITS V3服务的测试")

	config := map[string]interface{}{
		"url":             "http://127.0.0.1:9880",
		"refer_wav_path":  "demo.wav",
		"prompt_text":     "你好，这是测试",
		"prompt_language": "zh",
		"text_language":   "zh",
	}

	provider := NewGptSovitsV3TTSProvider(config)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	outputChan, err := provider.TextToSpeechStream(ctx, "你好，这是一个流式测试。", 24000, 1, 20)
	if err != nil {
		t.Fatalf("TextToSpeechStream failed: %v", err)
	}

	frameCount := 0
	for frame := range outputChan {
		if len(frame) == 0 {
			t.Error("Received empty frame")
		}
		frameCount++
	}

	if frameCount == 0 {
		t.Error("Expected to receive at least one frame")
	}

	t.Logf("Received %d audio frames via stream", frameCount)
}

// 测试默认配置
func TestGptSovitsV3TTSProvider_DefaultConfig(t *testing.T) {
	config := map[string]interface{}{}

	provider := NewGptSovitsV3TTSProvider(config)

	// 检查默认值
	if provider.URL != "http://127.0.0.1:9880" {
		t.Errorf("Expected default URL to be 'http://127.0.0.1:9880', got '%s'", provider.URL)
	}
	if provider.PromptLanguage != "auto" {
		t.Errorf("Expected default PromptLanguage to be 'auto', got '%s'", provider.PromptLanguage)
	}
	if provider.TextLanguage != "auto" {
		t.Errorf("Expected default TextLanguage to be 'auto', got '%s'", provider.TextLanguage)
	}
	if provider.TopK != 5 {
		t.Errorf("Expected default TopK to be 5, got %d", provider.TopK)
	}
	if provider.TopP != 1.0 {
		t.Errorf("Expected default TopP to be 1.0, got %f", provider.TopP)
	}
	if provider.Temperature != 1.0 {
		t.Errorf("Expected default Temperature to be 1.0, got %f", provider.Temperature)
	}
	if provider.SampleSteps != 50 {
		t.Errorf("Expected default SampleSteps to be 50, got %d", provider.SampleSteps)
	}
	if provider.Speed != 1.0 {
		t.Errorf("Expected default Speed to be 1.0, got %f", provider.Speed)
	}
	if provider.AudioFileType != "wav" {
		t.Errorf("Expected default AudioFileType to be 'wav', got '%s'", provider.AudioFileType)
	}
	if provider.IfSr != false {
		t.Errorf("Expected default IfSr to be false, got %v", provider.IfSr)
	}
}
