package vad

import (
	"testing"

	"backend-server/constants"
	"backend-server/internal/config"
	"backend-server/internal/domain/audio"
	utypes "backend-server/internal/domain/config/types"
	client "backend-server/internal/server/chat/session/state"
)

func TestBuildVADConfigSherpaDisablesNoiseFloor(t *testing.T) {
	state := &client.ClientState{
		DeviceConfig: utypes.UConfig{
			Vad: utypes.VadConfig{Provider: constants.VadTypeSherpaVad},
		},
		InputAudioFormat: audio.AudioFormat{
			SampleRate:    16000,
			FrameDuration: 20,
		},
	}

	appCfg := &config.AppConfig{
		VAD: config.VADConfig{
			Provider: constants.VadTypeSherpaVad,
			WebRTCVAD: config.WebRTCVADConfig{
				NoiseFloor:      0.02,
				MinActiveFrames: 3,
			},
			SherpaVAD: config.SherpaVADConfig{
				SampleRate: 16000,
			},
		},
	}

	cfg := BuildVADConfig(state, appCfg, 320)
	if cfg.Provider != constants.VadTypeSherpaVad {
		t.Fatalf("unexpected provider %s", cfg.Provider)
	}
	if cfg.NoiseFloor != 0 {
		t.Fatalf("expected sherpa noise floor to be 0, got %f", cfg.NoiseFloor)
	}
	if cfg.MinActiveFrames != 3 {
		t.Fatalf("expected sherpa to inherit minActiveFrames, got %d", cfg.MinActiveFrames)
	}
}
