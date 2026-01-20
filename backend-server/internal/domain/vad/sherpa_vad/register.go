//go:build sherpa_onnx
// +build sherpa_onnx

package sherpa_vad

import (
	"backend-server/constants"
	"backend-server/internal/domain/vad/inter"
	vadregistry "backend-server/internal/registry/vad"
)

func init() {
	vadregistry.Register(constants.VadTypeSherpaVad, vadregistry.Provider{
		Name: constants.VadTypeSherpaVad,
		Acquire: func(cfg map[string]interface{}) (inter.VAD, error) {
			return AcquireVAD(cfg)
		},
		Release: func(v inter.VAD) error {
			return ReleaseVAD(v)
		},
	})
	vadregistry.SetCapabilities(constants.VadTypeSherpaVad, vadregistry.ProviderCapabilities{
		Incremental: true,
	})
}
