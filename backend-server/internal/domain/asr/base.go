package asr

import (
	"backend-server/internal/registry/asr"

	// 导入所有ASR提供者包，确保它们的init函数被执行
	_ "backend-server/internal/domain/asr/aliyun"
	_ "backend-server/internal/domain/asr/doubao"
	_ "backend-server/internal/domain/asr/elevenlabs"
	_ "backend-server/internal/domain/asr/funasr"
	_ "backend-server/internal/domain/asr/google_genai"
	_ "backend-server/internal/domain/asr/openai_realtime"
)

// BaseASRProvider 基础ASR提供者接口（不含Context方法）
// 此接口定义与独立注册包中的接口保持一致
type BaseASRProvider = asr.BaseASRProvider

// AsrProvider 完整ASR提供者接口（包含Context方法）
type AsrProvider interface {
	BaseASRProvider
}

// ProviderFactory 定义provider工厂函数类型
// 此类型定义与独立注册包中的类型保持一致
type ProviderFactory = asr.ProviderFactory

// ProviderInfo 包含provider的元信息
// 此类型定义与独立注册包中的类型保持一致
type ProviderInfo = asr.ProviderInfo

// Registry ASR提供者注册器
// 此类型定义与独立注册包中的类型保持一致
type Registry = asr.Registry

// Register 注册ASR提供者 - 桥接到独立注册包
func Register(names []string, description string, factory ProviderFactory) {
	asr.Register(names, description, factory)
}

// GetProvider 获取已注册的ASR提供者 - 桥接到独立注册包
func GetProvider(name string, config map[string]interface{}) (AsrProvider, error) {
	baseProvider, err := asr.GetProvider(name, config)
	if err != nil {
		return nil, err
	}

	// 直接返回基础提供者，因为ASR接口与BaseASRProvider相同
	return baseProvider, nil
}

// ListProviders 列出所有已注册的提供者 - 桥接到独立注册包
func ListProviders() []string {
	return asr.ListProviders()
}

// GetProviderInfo 获取提供者信息 - 桥接到独立注册包
func GetProviderInfo(name string) (*ProviderInfo, bool) {
	return asr.GetProviderInfo(name)
}

// NewAsrProvider 向后兼容的方法，建议使用GetProvider
func NewAsrProvider(asrType string, config map[string]interface{}) (AsrProvider, error) {
	return GetProvider(asrType, config)
}
