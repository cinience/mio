package vad

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"backend-server/constants"
	"backend-server/internal/config"
	log "backend-server/internal/infrastructure/logger"
	client "backend-server/internal/server/chat/session/state"
	"backend-server/pkg/confighelper"
)

// VADConfig 封装构建 VADService 所需的所有参数。
type VADConfig struct {
	Provider        string
	ProviderConfig  map[string]interface{}
	NoiseFloor      float64
	MinActiveFrames int
	FrameDurationMs int
	FrameSize       int
	SampleRate      int
}

// BuildVADConfig 负责归一化 provider，并结合全局/设备配置生成最终的 VAD 参数。
func BuildVADConfig(state *client.ClientState, appCfg *config.AppConfig, frameSize int) VADConfig {
	provider := state.DeviceConfig.Vad.Provider
	if provider == constants.VadTypeSileroVad {
		provider = constants.VadTypeSherpaVad
		state.DeviceConfig.Vad.Provider = provider
	}
	if provider == "" && appCfg != nil {
		provider = appCfg.VAD.Provider
		state.DeviceConfig.Vad.Provider = provider
	}

	cfg := VADConfig{
		Provider:        provider,
		FrameDurationMs: state.InputAudioFormat.FrameDuration,
		FrameSize:       frameSize,
		SampleRate:      state.InputAudioFormat.SampleRate,
	}

	cfg.ProviderConfig = populateProviderConfig(state, appCfg, provider)
	cfg.NoiseFloor, cfg.MinActiveFrames = resolveStrategyDefaults(provider, cfg.ProviderConfig, appCfg)

	// 确保写回设备配置，便于日志/后续流程查看。
	state.DeviceConfig.Vad.Config = cfg.ProviderConfig

	return cfg
}

func populateProviderConfig(state *client.ClientState, appCfg *config.AppConfig, provider string) map[string]interface{} {
	if provider == constants.VadTypeWebRTCVad {
		// 对于 WebRTC VAD，需要从设备配置或全局配置中获取参数
		config := make(map[string]interface{})

		// 先从设备配置中获取
		if deviceCfg := state.DeviceConfig.Vad.Config; deviceCfg != nil {
			for k, v := range deviceCfg {
				config[k] = v
			}
		}

		// 如果全局配置存在，补充缺失的参数
		if appCfg != nil {
			if _, exists := config["noise_floor"]; !exists && appCfg.VAD.WebRTCVAD.NoiseFloor > 0 {
				config["noise_floor"] = appCfg.VAD.WebRTCVAD.NoiseFloor
			}
			if _, exists := config["min_active_frames"]; !exists && appCfg.VAD.WebRTCVAD.MinActiveFrames > 0 {
				config["min_active_frames"] = appCfg.VAD.WebRTCVAD.MinActiveFrames
			}
		}

		return config
	}

	if provider != constants.VadTypeSherpaVad {
		return map[string]interface{}{}
	}

	if appCfg == nil {
		return state.DeviceConfig.Vad.Config
	}

	defaultSherpa := structToMap(appCfg.VAD.SherpaVAD)
	if len(defaultSherpa) == 0 {
		defaultSherpa = structToMap(appCfg.VAD.SileroVAD)
	}
	if defaultSherpa == nil {
		defaultSherpa = make(map[string]interface{})
	}
	if cfg := state.DeviceConfig.Vad.Config; cfg != nil {
		for k, v := range cfg {
			defaultSherpa[k] = v
		}
	}

	modelPath := ""
	if raw, ok := defaultSherpa["model_path"]; ok {
		modelPath = fmt.Sprint(raw)
	}
	if strings.TrimSpace(modelPath) == "" {
		switch {
		case appCfg.VAD.SherpaVAD.ModelPath != "":
			defaultSherpa["model_path"] = appCfg.VAD.SherpaVAD.ModelPath
		case appCfg.VAD.SileroVAD.ModelPath != "":
			defaultSherpa["model_path"] = appCfg.VAD.SileroVAD.ModelPath
		}
		modelPath = fmt.Sprint(defaultSherpa["model_path"])
	}
	if resolved := resolveModelPath(modelPath); resolved != "" {
		defaultSherpa["model_path"] = resolved
	}

	if path := fmt.Sprint(defaultSherpa["model_path"]); strings.TrimSpace(path) == "" {
		log.Warn("Sherpa VAD config missing model_path; VAD will remain disabled")
	}

	return defaultSherpa
}

func structToMap(input interface{}) map[string]interface{} {
	if input == nil {
		return map[string]interface{}{}
	}
	data, err := json.Marshal(input)
	if err != nil {
		return map[string]interface{}{}
	}
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return map[string]interface{}{}
	}
	return result
}

func resolveStrategyDefaults(provider string, providerConfig map[string]interface{}, appCfg *config.AppConfig) (noiseFloor float64, minActiveFrames int) {
	if appCfg != nil {
		minActiveFrames = appCfg.VAD.WebRTCVAD.MinActiveFrames
	}
	if minActiveFrames < 1 {
		minActiveFrames = 1
	}

	// Sherpa/Silero VAD 交给模型自适应判断，不做 RMS 噪声门限
	if provider == constants.VadTypeSherpaVad || provider == constants.VadTypeSileroVad {
		if providerConfig != nil {
			helper := confighelper.New(providerConfig)
			mf := helper.GetInt("min_active_frames", minActiveFrames)
			if mf > 0 {
				minActiveFrames = mf
			}
		}
		return 0, minActiveFrames
	}

	if appCfg != nil {
		noiseFloor = appCfg.VAD.WebRTCVAD.NoiseFloor
	}

	if providerConfig == nil {
		return clampNoiseFloor(noiseFloor), minActiveFrames
	}

	helper := confighelper.New(providerConfig)
	nf := helper.GetFloat64("noise_floor", noiseFloor)
	if nf < 0 {
		nf = 0
	}
	mf := helper.GetInt("min_active_frames", minActiveFrames)
	if mf < 1 {
		mf = 1
	}
	return nf, mf
}

func clampNoiseFloor(value float64) float64 {
	if value < 0 {
		return 0
	}
	return value
}

func resolveModelPath(path string) string {
	clean := strings.TrimSpace(path)
	if clean == "" {
		return ""
	}

	tryPaths := []string{clean}
	if !filepath.IsAbs(clean) {
		if v := config.GetViper(); v != nil {
			if cfgFile := v.ConfigFileUsed(); cfgFile != "" {
				base := filepath.Dir(cfgFile)
				tryPaths = append([]string{filepath.Clean(filepath.Join(base, clean))}, tryPaths...)
			}
		}
	}

	for _, candidate := range tryPaths {
		if candidate == "" {
			continue
		}
		if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
			return candidate
		}
	}
	return tryPaths[0]
}
