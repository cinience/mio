//go:build sherpa_onnx

package main

import "path/filepath"

func speechServiceEnabled() bool {
	return true
}

func defaultSpeechConfigPath(root string) string {
	return filepath.Join(root, "speech-server", "config", "config.yaml")
}
