package openai_realtime

import (
	"backend-server/constants"
	log "backend-server/internal/infrastructure/logger"
	registryAsr "backend-server/internal/registry/asr"
)

func init() {
	registryAsr.Register([]string{constants.AsrTypeOpenAIRealtime}, "OpenAI Realtime Speech-Server ASR", func(config map[string]interface{}) (registryAsr.BaseASRProvider, error) {
		provider, err := NewProvider(config)
		if err != nil {
			log.Errorf("初始化OpenAI Realtime ASR提供者失败: %v", err)
			return nil, err
		}
		log.Infof("OpenAI Realtime ASR提供者初始化成功，endpoint=%s", provider.config.URL)
		return provider, nil
	})
}
