//go:build sherpa_onnx

package main

import (
	"os"
	"path/filepath"
)

func initAvailableServices() []string {
	return []string{serviceBackend, serviceManager, serviceRedis, serviceSpeech}
}

func defaultSpeechConfigPath() string {
	candidates := []string{
		filepath.Join("..", "speech-server", "config", "config.yaml"),
		filepath.Join("speech-server", "config", "config.yaml"),
	}
	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}
