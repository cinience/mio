package webrtc_vad

import (
	"time"

	util "backend-server/internal/shared"
)

func getPoolConfigFromMap(config map[string]interface{}) *util.PoolConfig {
	poolConfig := util.DefaultConfig()
	if config == nil {
		return poolConfig
	}
	if config["pool_min_size"] != nil {
		if minSize, ok := config["pool_min_size"].(int); ok {
			poolConfig.MinSize = minSize
		}
	}
	if config["pool_max_size"] != nil {
		if maxSize, ok := config["pool_max_size"].(int); ok {
			poolConfig.MaxSize = maxSize
		}
	}
	if config["pool_max_idle"] != nil {
		if maxIdle, ok := config["pool_max_idle"].(int); ok {
			poolConfig.MaxIdle = maxIdle
		}
	}
	if rawTimeout, ok := config["acquire_timeout_ms"]; ok {
		if timeoutMs, ok := normalizeInt(rawTimeout); ok && timeoutMs > 0 {
			poolConfig.AcquireTimeout = time.Duration(timeoutMs) * time.Millisecond
		}
	}
	if rawTimeout, ok := config["idle_timeout_ms"]; ok {
		if timeoutMs, ok := normalizeInt(rawTimeout); ok && timeoutMs > 0 {
			poolConfig.IdleTimeout = time.Duration(timeoutMs) * time.Millisecond
		}
	}
	if raw, ok := config["validate_on_borrow"]; ok {
		if val, ok := raw.(bool); ok {
			poolConfig.ValidateOnBorrow = val
		}
	}
	if raw, ok := config["validate_on_return"]; ok {
		if val, ok := raw.(bool); ok {
			poolConfig.ValidateOnReturn = val
		}
	}
	return poolConfig
}

func getVadConfigFromMap(config map[string]interface{}) WebRTCVADConfig {
	sampleRate := DefaultSampleRate
	mode := DefaultMode
	noiseFloor := 0.0 // 默认不启用噪声过滤

	if config == nil {
		return WebRTCVADConfig{
			SampleRate: sampleRate,
			Mode:       mode,
			NoiseFloor: noiseFloor,
		}
	}
	if val, ok := config["vad_sample_rate"]; ok {
		if v, ok := val.(int); ok {
			sampleRate = v
		}
	}
	if val, ok := config["vad_mode"]; ok {
		if v, ok := val.(int); ok {
			mode = v
		}
	}
	if val, ok := config["noise_floor"]; ok {
		switch v := val.(type) {
		case float64:
			noiseFloor = v
		case float32:
			noiseFloor = float64(v)
		case int:
			noiseFloor = float64(v)
		}
	}
	return WebRTCVADConfig{
		SampleRate: sampleRate,
		Mode:       mode,
		NoiseFloor: noiseFloor,
	}
}

func normalizeInt(value interface{}) (int, bool) {
	switch v := value.(type) {
	case int:
		return v, true
	case int32:
		return int(v), true
	case int64:
		return int(v), true
	case float64:
		return int(v), true
	case float32:
		return int(v), true
	default:
		return 0, false
	}
}
