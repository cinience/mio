package realtime

import (
	"slices"
	"testing"

	openairt "github.com/WqyJh/go-openai-realtime/v2"
)

func TestApplyOutputConfigUpdatesRate(t *testing.T) {
	state := newSessionState(24000, sessionModeRealtime, openairt.VoiceAlloy, 1.0)
	if state.outputRate != 24000 {
		t.Fatalf("expected default output rate 24000, got %d", state.outputRate)
	}

	realtime := state.sessionCfg.Realtime
	if realtime == nil || realtime.Audio == nil {
		t.Fatalf("realtime session not initialised")
	}

	desired := &openairt.SessionAudioOutput{
		Format: &openairt.AudioFormatUnion{
			PCM: &openairt.AudioFormatPCM{Rate: 16000},
		},
		Voice: openairt.Voice("Verse"),
		Speed: 1.25,
	}

	state.applyOutputConfig(&realtime.Audio.Output, desired)

	if want, got := 16000, state.outputRate; want != got {
		t.Fatalf("expected output rate %d, got %d", want, got)
	}
	if realtime.Audio.Output == nil || realtime.Audio.Output.Format == nil || realtime.Audio.Output.Format.PCM == nil {
		t.Fatalf("output format not persisted")
	}
	if got := realtime.Audio.Output.Format.PCM.Rate; got != 16000 {
		t.Fatalf("expected format rate 16000, got %d", got)
	}
	if state.outputVoice != normalizeVoice(openairt.Voice("Verse")) {
		t.Fatalf("voice not normalised or persisted")
	}
	if state.outputSpeed != 1.25 {
		t.Fatalf("speed not persisted")
	}
}

func TestAlignTTSSampleRate(t *testing.T) {
	input := []float32{0, 0.5, -0.5, 1}

	resampled, rate := alignTTSSampleRate(input, 4, 2)
	if rate != 2 {
		t.Fatalf("expected rate 2, got %d", rate)
	}
	if len(resampled) == len(input) {
		t.Fatalf("expected resampled length to differ, got %d", len(resampled))
	}

	passthrough, rate2 := alignTTSSampleRate(input, 16000, 0)
	if rate2 != 16000 {
		t.Fatalf("expected passthrough rate 16000, got %d", rate2)
	}
	if !slices.Equal(passthrough, input) {
		t.Fatalf("expected passthrough samples to match input")
	}
	if len(passthrough) > 0 && &passthrough[0] == &input[0] {
		t.Fatalf("expected passthrough to return copy, but underlying array matches input")
	}

	empty, rate3 := alignTTSSampleRate(nil, 0, 24000)
	if rate3 != 24000 {
		t.Fatalf("expected empty output rate 24000, got %d", rate3)
	}
	if empty != nil {
		t.Fatalf("expected nil samples for empty input")
	}
}
