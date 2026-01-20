// Package tts 提供独立的TTS提供者注册功能
// 这个包独立于具体的TTS实现和主TTS包，完全避免循环导入
package tts

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// BaseTTSProvider 基础TTS提供者接口
type BaseTTSProvider interface {
	TextToSpeech(ctx context.Context, text string, sampleRate int, channels int, frameDuration int) ([][]byte, error)
	TextToSpeechStream(ctx context.Context, text string, sampleRate int, channels int, frameDuration int) (outputChan chan []byte, err error)
	ChangeVoice(ctx context.Context, options *ChangeVoiceOptions) error
}

// ChangeVoiceOptions captures the dynamic parameters required when switching
// voices at runtime. Concrete providers may only use a subset of the fields
// depending on their capabilities.
type ChangeVoiceOptions struct {
	// VoiceID is the identifier returned by upstream manager services.
	VoiceID string
	// Voice is the concrete voice token required by the TTS provider.
	Voice string
	// VoiceName is an optional human readable label used for logging.
	VoiceName string
	// ModelID references the specific TTS model to bind with the voice.
	ModelID string
	// Params carries provider specific overrides (e.g. reference audio path).
	Params map[string]interface{}
}

// ProviderFactory 定义provider工厂函数类型
type ProviderFactory func(config map[string]interface{}) (BaseTTSProvider, error)

// ProviderInfo 包含provider的元信息
type ProviderInfo struct {
	Name        string
	Description string
	Factory     ProviderFactory
}

// Registry TTS提供者注册器
type Registry struct {
	mu        sync.RWMutex
	providers map[string]*ProviderInfo
}

// NewRegistry 创建新的注册器实例
func NewRegistry() *Registry {
	return &Registry{
		providers: make(map[string]*ProviderInfo),
	}
}

// globalRegistry 全局注册器实例
var globalRegistry = NewRegistry()

// Register 注册TTS提供者，支持单个名称或多个名称
// 用法：
//
//	Register([]string{"name1"}, "描述", factory)           - 注册单个名称
//	Register([]string{"name1", "name2", "name3"}, "描述", factory) - 注册多个名称
func Register(names []string, description string, factory ProviderFactory) {
	if len(names) == 0 {
		panic("必须提供至少一个名称")
	}

	// 清理名称中的空格并验证
	cleanNames := make([]string, len(names))
	for i, name := range names {
		cleanNames[i] = strings.TrimSpace(name)
		if cleanNames[i] == "" {
			panic("名称不能为空")
		}
	}

	if len(cleanNames) == 1 {
		// 单个名称
		globalRegistry.Register(cleanNames[0], description, factory)
	} else {
		// 多个名称
		globalRegistry.RegisterMultiple(cleanNames, description, factory)
	}
}

// Register 注册TTS提供者到注册器
func (r *Registry) Register(name, description string, factory ProviderFactory) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[name] = &ProviderInfo{
		Name:        name,
		Description: description,
		Factory:     factory,
	}
}

// RegisterMultiple 注册TTS提供者到注册器支持多个名称
func (r *Registry) RegisterMultiple(names []string, description string, factory ProviderFactory) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, name := range names {
		r.providers[name] = &ProviderInfo{
			Name:        name,
			Description: description,
			Factory:     factory,
		}
	}
}

// GetProvider 从注册器获取TTS提供者
func (r *Registry) GetProvider(name string, config map[string]interface{}) (BaseTTSProvider, error) {
	r.mu.RLock()
	info, exists := r.providers[name]
	r.mu.RUnlock()

	if !exists {
		r.mu.RLock()
		info, exists = r.providers["default"]
		r.mu.RUnlock()
		if !exists {
			return nil, fmt.Errorf("未找到TTS提供者: %s", name)
		}
	}

	return info.Factory(config)
}

// ListProviders 列出注册器中的所有提供者
func (r *Registry) ListProviders() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.providers))
	for name := range r.providers {
		names = append(names, name)
	}
	return names
}

// GetProviderInfo 从注册器获取提供者信息
func (r *Registry) GetProviderInfo(name string) (*ProviderInfo, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	info, exists := r.providers[name]
	return info, exists
}

// GetProvider 获取已注册的TTS提供者
func GetProvider(name string, config map[string]interface{}) (BaseTTSProvider, error) {
	return globalRegistry.GetProvider(name, config)
}

// ListProviders 列出所有已注册的提供者
func ListProviders() []string {
	return globalRegistry.ListProviders()
}

// GetProviderInfo 获取提供者信息
func GetProviderInfo(name string) (*ProviderInfo, bool) {
	return globalRegistry.GetProviderInfo(name)
}

// GetGlobalRegistry 获取全局注册器实例
func GetGlobalRegistry() *Registry {
	return globalRegistry
}
