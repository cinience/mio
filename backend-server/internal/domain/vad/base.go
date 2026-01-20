package vad

import (
	"backend-server/internal/domain/vad/inter"
	vadregistry "backend-server/internal/registry/vad"

	// 注册所有内置 VAD 实现（根据 build tags 自动启用）
	_ "backend-server/internal/domain/vad/sherpa_vad"
	_ "backend-server/internal/domain/vad/webrtc_vad"
)

// Provider exposes metadata about a registered VAD provider.
type Provider = vadregistry.Provider

// AcquireFunc constructs or fetches a VAD instance using an optional config map.
type AcquireFunc = vadregistry.AcquireFunc

// ReleaseFunc returns a VAD instance to the underlying pool/implementation.
type ReleaseFunc = vadregistry.ReleaseFunc

// Register makes a VAD implementation available under the given name.
func Register(name string, provider Provider) {
	vadregistry.Register(name, provider)
}

// SetDefaultProvider overrides the provider used when the requested one is unavailable.
func SetDefaultProvider(name string) {
	vadregistry.SetDefaultProvider(name)
}

// GetDefaultProvider returns the currently configured default provider name.
func GetDefaultProvider() string {
	return vadregistry.GetDefaultProvider()
}

// AcquireVAD attempts to retrieve a VAD implementation for the requested provider.
func AcquireVAD(provider string, config map[string]interface{}) (inter.VAD, error) {
	return vadregistry.Acquire(provider, config)
}

// ReleaseVAD returns a VAD instance to the underlying provider implementation.
func ReleaseVAD(v inter.VAD) error {
	return vadregistry.Release(v)
}

// ListProviders returns the identifiers of all registered providers.
func ListProviders() []string {
	return vadregistry.ListProviders()
}
