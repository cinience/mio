package openai_realtime

import (
	"backend-server/constants"
	log "backend-server/internal/infrastructure/logger"
	registryTTS "backend-server/internal/registry/tts"
)

func init() {
	registryTTS.Register([]string{constants.TtsTypeOpenAIRealtime}, "OpenAI Realtime Speech-Server TTS", func(config map[string]interface{}) (registryTTS.BaseTTSProvider, error) {
		provider, err := NewProvider(config)
		if err != nil {
			log.Errorf("初始化OpenAI Realtime TTS提供者失败: %v", err)
			return nil, err
		}
		log.Infof("OpenAI Realtime TTS提供者初始化成功，endpoint=%s model=%s", provider.config.URL, provider.config.Model)
		return provider, nil
	})
}
