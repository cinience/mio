package session

import (
	"testing"
	"time"

	types_audio "backend-server/internal/domain/audio"
	client "backend-server/internal/server/chat/session/state"

	"github.com/cloudwego/eino/schema"
)

func TestManagerChatUploaderCaptureUserAudio(t *testing.T) {
	buf := &client.AsrAudioBuffer{}
	frameSize := 160
	buf.Configure(frameSize, 100)

	sample := make([]float32, frameSize*4)
	for i := range sample {
		sample[i] = float32(i%10) / 10
	}
	buf.AddAsrAudioData(sample)

	state := &client.ClientState{
		AsrAudioBuffer: buf,
		InputAudioFormat: types_audio.AudioFormat{
			SampleRate:    16000,
			Channels:      1,
			FrameDuration: 20,
		},
	}

	uploader := &managerChatUploader{session: &ChatSession{clientState: state}}
	wav := uploader.captureUserAudio()
	if len(wav) == 0 {
		t.Fatalf("expected wav payload for upstream audio")
	}

	// Second call should return no data because mark advanced
	if next := uploader.captureUserAudio(); len(next) != 0 {
		t.Fatalf("expected empty payload after buffer drained, got %d bytes", len(next))
	}
}

func TestManagerChatUploaderCaptureAssistantAudio(t *testing.T) {
	uploader := &managerChatUploader{
		session:            &ChatSession{clientState: &client.ClientState{}},
		downstreamPCM:      []float32{0, 0.1, -0.2, 0.3},
		downstreamRate:     16000,
		downstreamChannels: 1,
	}

	wav := uploader.captureAssistantAudio()
	if len(wav) == 0 {
		t.Fatalf("expected downstream wav payload")
	}
	if len(uploader.downstreamPCM) != 0 {
		t.Fatalf("expected downstream buffer reset")
	}
}

func TestManagerChatUploaderRenderMessageContent(t *testing.T) {
	uploader := &managerChatUploader{session: &ChatSession{clientState: &client.ClientState{}}}

	msg := &schema.Message{MultiContent: []schema.ChatMessagePart{{Text: " 你好 "}}}
	got := uploader.renderMessageContent(msg)
	if got != "你好" {
		t.Fatalf("expected trimmed text, got %q", got)
	}
}

func TestTrimLatestSamples(t *testing.T) {
	samples := make([]float32, 200)
	for i := range samples {
		samples[i] = float32(i)
	}
	trimmed := trimLatestSamples(samples, 100, 1*time.Second)
	if len(trimmed) != 100 {
		t.Fatalf("expected 100 samples, got %d", len(trimmed))
	}
	if trimmed[0] != 100 {
		t.Fatalf("expected slice to include latest samples, got %f", trimmed[0])
	}
}
