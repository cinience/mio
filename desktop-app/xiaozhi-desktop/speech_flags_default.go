//go:build !sherpa_onnx

package main

func speechServiceEnabled() bool {
	return false
}

func defaultSpeechConfigPath(root string) string {
	return ""
}
