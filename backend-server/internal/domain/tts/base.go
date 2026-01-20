package tts

import (
	"context"

	"backend-server/internal/registry/tts"

	// 导入所有TTS提供者包，确保它们的init函数被执行
	_ "backend-server/internal/domain/tts/cosyvoice"
	_ "backend-server/internal/domain/tts/doubao"
	_ "backend-server/internal/domain/tts/edge"
	_ "backend-server/internal/domain/tts/edge_offline"
	_ "backend-server/internal/domain/tts/elevenlabs"
	_ "backend-server/internal/domain/tts/google_genai"
	_ "backend-server/internal/domain/tts/gpt_sovits_v3"
	_ "backend-server/internal/domain/tts/index_stream"
	_ "backend-server/internal/domain/tts/openai_realtime"
	_ "backend-server/internal/domain/tts/xiaozhi"
)

// 基础TTS提供者接口（不含Context方法）
// 此接口定义与独立注册包中的接口保持一致
type BaseTTSProvider = tts.BaseTTSProvider

// ChangeVoiceOptions re-exports the registry level definition so callers can
// depend on a single package.
type ChangeVoiceOptions = tts.ChangeVoiceOptions

// 完整TTS提供者接口（包含Context方法）
type TTSProvider interface {
	BaseTTSProvider
}

// ProviderFactory 定义provider工厂函数类型
// 此类型定义与独立注册包中的类型保持一致
type ProviderFactory = tts.ProviderFactory

// ProviderInfo 包含provider的元信息
// 此类型定义与独立注册包中的类型保持一致
type ProviderInfo = tts.ProviderInfo

// Registry TTS提供者注册器
// 此类型定义与独立注册包中的类型保持一致
type Registry = tts.Registry

// Register 注册TTS提供者 - 桥接到独立注册包
func Register(names []string, description string, factory ProviderFactory) {
	tts.Register(names, description, factory)
}

// GetProvider 获取已注册的TTS提供者 - 桥接到独立注册包
func GetProvider(name string, config map[string]interface{}) (TTSProvider, error) {
	baseProvider, err := tts.GetProvider(name, config)
	if err != nil {
		return nil, err
	}

	// 使用适配器包装基础提供者，转换为完整的TTSProvider
	provider := &ContextTTSAdapter{baseProvider}
	return provider, nil
}

// ListProviders 列出所有已注册的提供者 - 桥接到独立注册包
func ListProviders() []string {
	return tts.ListProviders()
}

// GetProviderInfo 获取提供者信息 - 桥接到独立注册包
func GetProviderInfo(name string) (*ProviderInfo, bool) {
	return tts.GetProviderInfo(name)
}

// GetTTSProvider 向后兼容的方法，建议使用GetProvider
func GetTTSProvider(providerName string, config map[string]interface{}) (TTSProvider, error) {
	return GetProvider(providerName, config)
}

// ContextTTSAdapter 是一个适配器，为基础TTS提供者添加Context支持
type ContextTTSAdapter struct {
	Provider BaseTTSProvider
}

// TextToSpeech 代理到原始提供者
func (a *ContextTTSAdapter) TextToSpeech(ctx context.Context, text string, sampleRate int, channels int, frameDuration int) ([][]byte, error) {
	return a.Provider.TextToSpeech(ctx, text, sampleRate, channels, frameDuration)
}

// TextToSpeechStream 代理到原始提供者
func (a *ContextTTSAdapter) TextToSpeechStream(ctx context.Context, text string, sampleRate int, channels int, frameDuration int) (outputChan chan []byte, err error) {
	return a.Provider.TextToSpeechStream(ctx, text, sampleRate, channels, frameDuration)
}

// TextToSpeechWithContext 使用Context版本的文本转语音
func (a *ContextTTSAdapter) TextToSpeechWithContext(ctx context.Context, text string, sampleRate int, channels int, frameDuration int) ([][]byte, error) {
	// 检查提供者是否直接支持Context版本
	if provider, ok := a.Provider.(interface {
		TextToSpeechWithContext(ctx context.Context, text string, sampleRate int, channels int, frameDuration int) ([][]byte, error)
	}); ok {
		// 提供者直接支持Context版本
		return provider.TextToSpeechWithContext(ctx, text, sampleRate, channels, frameDuration)
	}

	// 否则使用标准版本，并通过goroutine和channel实现上下文控制
	resultChan := make(chan struct {
		frames [][]byte
		err    error
	})

	go func() {
		frames, err := a.Provider.TextToSpeech(ctx, text, sampleRate, channels, frameDuration)
		select {
		case <-ctx.Done():
			// 上下文已取消，不发送结果
			return
		case resultChan <- struct {
			frames [][]byte
			err    error
		}{frames, err}:
			// 结果已发送
		}
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case result := <-resultChan:
		return result.frames, result.err
	}
}

// TextToSpeechStreamWithContext 使用Context版本的流式文本转语音
func (a *ContextTTSAdapter) TextToSpeechStreamWithContext(ctx context.Context, text string, sampleRate int, channels int, frameDuration int) (outputChan chan []byte, cancelFunc func(), err error) {
	// 检查提供者是否直接支持Context版本
	if provider, ok := a.Provider.(interface {
		TextToSpeechStreamWithContext(ctx context.Context, text string, sampleRate int, channels int, frameDuration int) (chan []byte, func(), error)
	}); ok {
		// 提供者直接支持Context版本
		return provider.TextToSpeechStreamWithContext(ctx, text, sampleRate, channels, frameDuration)
	}

	// 否则使用标准版本，但创建一个包装器来处理上下文取消
	streamChan, err := a.Provider.TextToSpeechStream(ctx, text, sampleRate, channels, frameDuration)
	if err != nil {
		return nil, nil, err
	}

	// 创建一个新的输出通道，用于转发和处理取消
	outputChan = make(chan []byte, 10)

	// 创建一个goroutine来转发数据并监听上下文取消
	go func() {
		defer close(outputChan)

		for {
			select {
			case <-ctx.Done():
				// 上下文已取消，调用原始取消函数并退出
				cancelFunc()
				return
			case frame, ok := <-streamChan:
				if !ok {
					// 原始通道已关闭
					return
				}
				// 转发数据
				select {
				case <-ctx.Done():
					// 上下文已取消
					cancelFunc()
					return
				case outputChan <- frame:
					// 成功转发数据
				}
			}
		}
	}()

	return outputChan, cancelFunc, nil
}

// ChangeVoice proxies runtime voice updates to the underlying provider.
func (a *ContextTTSAdapter) ChangeVoice(ctx context.Context, options *ChangeVoiceOptions) error {
	return a.Provider.ChangeVoice(ctx, options)
}
