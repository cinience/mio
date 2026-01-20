//go:build !sherpa_onnx
// +build !sherpa_onnx

package sherpa_vad

import (
	"fmt"

	"backend-server/internal/domain/vad/inter"
)

// SherpaVAD is a placeholder to keep type assertions compiling when the sherpa_onnx tag is disabled.
type SherpaVAD struct{}

// AcquireVAD returns an error unless the sherpa_onnx build tag is enabled.
func AcquireVAD(map[string]interface{}) (inter.VAD, error) {
	return nil, fmt.Errorf("sherpa_vad: build with `-tags sherpa_onnx` to enable Sherpa VAD")
}

// ReleaseVAD returns an error unless the sherpa_onnx build tag is enabled.
func ReleaseVAD(inter.VAD) error {
	return fmt.Errorf("sherpa_vad: build with `-tags sherpa_onnx` to enable Sherpa VAD")
}

// IsVAD implements the VAD interface but always returns an error because the feature is disabled.
func (*SherpaVAD) IsVAD([]float32) (bool, error) {
	return false, fmt.Errorf("sherpa_vad: build with `-tags sherpa_onnx` to enable Sherpa VAD")
}

// IsVADExt implements the VAD interface but always returns an error because the feature is disabled.
func (*SherpaVAD) IsVADExt([]float32, int, int) (bool, error) {
	return false, fmt.Errorf("sherpa_vad: build with `-tags sherpa_onnx` to enable Sherpa VAD")
}

// Reset is a no-op because the feature is disabled.
func (*SherpaVAD) Reset() error {
	return fmt.Errorf("sherpa_vad: build with `-tags sherpa_onnx` to enable Sherpa VAD")
}

// Close is a no-op because the feature is disabled.
func (*SherpaVAD) Close() error {
	return fmt.Errorf("sherpa_vad: build with `-tags sherpa_onnx` to enable Sherpa VAD")
}
