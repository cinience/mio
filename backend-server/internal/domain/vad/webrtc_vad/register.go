package webrtc_vad

import (
	"backend-server/constants"
	vadregistry "backend-server/internal/registry/vad"
)

func init() {
	vadregistry.Register(constants.VadTypeWebRTCVad, vadregistry.Provider{
		Name:    constants.VadTypeWebRTCVad,
		Acquire: AcquireVAD,
		Release: ReleaseVAD,
	})
	vadregistry.SetCapabilities(constants.VadTypeWebRTCVad, vadregistry.ProviderCapabilities{
		RequiresWindow: true,
		ResetEachCall:  true,
	})
	vadregistry.SetDefaultProvider(constants.VadTypeWebRTCVad)
}
