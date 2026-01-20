//go:build !sherpa_onnx

package main

func initAvailableServices() []string {
	return []string{serviceBackend, serviceManager, serviceRedis}
}

func defaultSpeechConfigPath() string {
	return ""
}
