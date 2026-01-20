package vad

import (
	"fmt"
	"sync"

	"backend-server/internal/domain/vad/inter"
	log "backend-server/internal/infrastructure/logger"
)

// AcquireFunc constructs or fetches a VAD instance using an optional config map.
type AcquireFunc func(config map[string]interface{}) (inter.VAD, error)

// ReleaseFunc returns a VAD instance to the underlying pool/implementation.
type ReleaseFunc func(v inter.VAD) error

// Provider encapsulates the acquire/release hooks for a VAD implementation.
type Provider struct {
	Name        string
	Description string
	Acquire     AcquireFunc
	Release     ReleaseFunc
}

var (
	registryMu sync.RWMutex
	providers  = make(map[string]Provider)

	capabilitiesMu sync.RWMutex
	providerCaps   = make(map[string]ProviderCapabilities)

	defaultMu       sync.RWMutex
	defaultProvider string

	acquiredMu sync.Mutex
	acquired   = make(map[inter.VAD]ReleaseFunc)
)

// ProviderCapabilities describes behavioural hints for a VAD provider.
type ProviderCapabilities struct {
	Incremental    bool // expects incremental (delta) audio instead of fixed windows
	RequiresWindow bool // requires callers to aggregate fixed-size windows
	ResetEachCall  bool // provider should be Reset() before each IsVAD check
}

// SetCapabilities registers capability hints for a provider.
func SetCapabilities(name string, caps ProviderCapabilities) {
	capabilitiesMu.Lock()
	providerCaps[name] = caps
	capabilitiesMu.Unlock()
}

// GetCapabilities returns capability hints for a provider, falling back to zero value.
func GetCapabilities(name string) ProviderCapabilities {
	capabilitiesMu.RLock()
	defer capabilitiesMu.RUnlock()
	if caps, ok := providerCaps[name]; ok {
		return caps
	}
	return ProviderCapabilities{}
}

// Register makes a VAD implementation available under the given name.
// Providers should typically register themselves in an init() guarded by build tags.
func Register(name string, provider Provider) {
	if name == "" {
		panic("vad registry: provider name cannot be empty")
	}
	if provider.Acquire == nil {
		panic(fmt.Sprintf("vad registry: provider %s registered with nil Acquire", name))
	}
	if provider.Release == nil {
		panic(fmt.Sprintf("vad registry: provider %s registered with nil Release", name))
	}

	registryMu.Lock()
	providers[name] = provider
	registryMu.Unlock()

	log.Debugf("VAD provider 注册: %s", name)
}

// SetDefaultProvider overrides the provider used when the requested one is unavailable.
func SetDefaultProvider(name string) {
	defaultMu.Lock()
	defaultProvider = name
	defaultMu.Unlock()
}

// GetDefaultProvider returns the currently configured default provider name.
func GetDefaultProvider() string {
	defaultMu.RLock()
	defer defaultMu.RUnlock()
	return defaultProvider
}

// Acquire attempts to retrieve a VAD implementation for the requested provider.
// If the provider is unregistered (likely due to build tags), the configured default
// provider will be used instead.
func Acquire(provider string, config map[string]interface{}) (inter.VAD, error) {
	entry, ok := getProvider(provider)
	usedProvider := provider

	if !ok {
		fallback := GetDefaultProvider()
		if fallback == "" {
			return nil, fmt.Errorf("vad provider %q not available and no default provider configured", provider)
		}

		defaultEntry, defaultOK := getProvider(fallback)
		if !defaultOK {
			return nil, fmt.Errorf("vad provider %q not available and default provider %q not registered", provider, fallback)
		}
		if provider != fallback {
			log.Warnf("VAD provider %q 未注册，回退到默认 provider %q", provider, fallback)
		}
		entry = defaultEntry
		usedProvider = fallback
	}

	v, err := entry.Acquire(config)
	if err != nil {
		return nil, fmt.Errorf("获取 VAD(%s) 失败: %w", usedProvider, err)
	}

	registerInstance(v, entry.Release)
	return v, nil
}

// Release returns a VAD instance to the underlying provider implementation.
// If the instance was not acquired through Acquire, an error is returned.
func Release(v inter.VAD) error {
	if v == nil {
		return fmt.Errorf("释放 VAD 失败: 实例为空")
	}

	release := lookupRelease(v)
	if release == nil {
		registryMu.RLock()
		defer registryMu.RUnlock()

		for name, entry := range providers {
			if err := entry.Release(v); err == nil {
				log.Debugf("VAD 实例释放成功 (fallback provider=%s)", name)
				return nil
			}
		}
		return fmt.Errorf("释放 VAD 失败: 未找到对应的提供者")
	}

	err := release(v)
	if err == nil {
		removeInstance(v)
	}
	return err
}

// ListProviders returns the identifiers of all registered providers.
func ListProviders() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()

	names := make([]string, 0, len(providers))
	for name := range providers {
		names = append(names, name)
	}
	return names
}

func getProvider(name string) (Provider, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	provider, ok := providers[name]
	return provider, ok
}

func registerInstance(v inter.VAD, release ReleaseFunc) {
	if v == nil || release == nil {
		return
	}
	acquiredMu.Lock()
	acquired[v] = release
	acquiredMu.Unlock()
}

func lookupRelease(v inter.VAD) ReleaseFunc {
	acquiredMu.Lock()
	defer acquiredMu.Unlock()
	return acquired[v]
}

func removeInstance(v inter.VAD) {
	acquiredMu.Lock()
	delete(acquired, v)
	acquiredMu.Unlock()
}
