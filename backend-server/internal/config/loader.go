package config

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	configdefaults "backend-server/config"
	"backend-server/constants"

	"github.com/spf13/viper"
)

// Loader 配置加载器
type Loader struct {
	v *viper.Viper
}

// NewLoader 创建新的配置加载器
func NewLoader() *Loader {
	return &Loader{
		v: viper.New(),
	}
}

// LoadFromFile 从文件加载配置
func (l *Loader) LoadFromFile(configPath string) error {
	// 解析配置文件路径
	basePath, file := filepath.Split(configPath)
	if basePath == "" {
		basePath = "."
	}

	// 获取文件名和扩展名
	fileName, fileExt := splitFileNameAndExt(file)
	if fileName == "" {
		return fmt.Errorf("配置文件名不能为空")
	}

	// 设置配置文件信息
	l.v.SetConfigName(fileName)
	l.v.AddConfigPath(basePath)

	// 根据文件扩展名设置配置类型
	switch fileExt {
	case "json":
		l.v.SetConfigType("json")
	case "yaml", "yml":
		l.v.SetConfigType("yaml")
	case "toml":
		l.v.SetConfigType("toml")
	default:
		return fmt.Errorf("不支持的配置文件类型: %s", fileExt)
	}

	if _, err := os.Stat(configPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			data := configdefaults.DefaultConfig()
			if len(data) == 0 {
				return fmt.Errorf("配置文件不存在: %s", configPath)
			}
			log.Printf("配置文件 %s 未找到，使用内置默认配置", configPath)
			if err := l.v.ReadConfig(bytes.NewReader(data)); err != nil {
				return fmt.Errorf("读取内置配置失败: %w", err)
			}
			return nil
		}
		return fmt.Errorf("检查配置文件失败: %w", err)
	}

	// 读取配置文件
	if err := l.v.ReadInConfig(); err != nil {
		return fmt.Errorf("读取配置文件失败: %w", err)
	}

	return nil
}

// LoadFromEnv 从环境变量加载配置
// 环境变量格式: XIAOZHI_SECTION_KEY=value
// 例如: XIAOZHI_REDIS_HOST=localhost
func (l *Loader) LoadFromEnv(prefix string) error {
	if prefix == "" {
		prefix = "XIAOZHI"
	}

	l.v.AllowEmptyEnv(true)
	l.v.SetEnvPrefix(prefix)
	l.v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	l.v.AutomaticEnv()

	return nil
}

// SetDefaults 设置默认值
func (l *Loader) SetDefaults() {
	// Server 默认配置
	l.v.SetDefault("server.pprof.enable", false)
	l.v.SetDefault("server.pprof.port", 6060)

	// Auth 默认配置
	l.v.SetDefault("auth.enable", false)

	// Chat 默认配置
	l.v.SetDefault("chat.max_idle_duration", 0)
	l.v.SetDefault("chat.chat_max_silence_duration", 200)
	l.v.SetDefault("chat.session_timeout_minutes", 30)
	l.v.SetDefault("chat.auto_resume_listening", false)
	l.v.SetDefault("chat.reassure_tool_delay_ms", 700)     // 首条 500-800ms 中值
	l.v.SetDefault("chat.reassure_tool_followup_ms", 4000) // 第二条约 3-5s
	l.v.SetDefault("chat.reassure_tool_message", "处理中，稍等哦")
	l.v.SetDefault("chat.reassure_tool_followup_message", "仍在处理，请再等一下。")
	l.v.SetDefault("chat.reassure_tool_longwait_message", "处理有些久，马上给你结果。")
	l.v.SetDefault("chat.reassure_tool_messages", map[string]string{
		"zh-CN": "处理中，稍等哦",
		"en":    "Working on it, one moment.",
	})
	l.v.SetDefault("chat.reassure_tool_longwait_messages", map[string]string{
		"zh-CN": "处理有些久，马上给你结果。",
		"en":    "Taking a bit longer, I'll be right back with results.",
	})
	l.v.SetDefault("chat.realtime_mode", 2)
	l.v.SetDefault("chat.realtime_min_voice_ms", 120)
	l.v.SetDefault("chat.realtime_interrupt_enabled", true)
	l.v.SetDefault("chat.asr_partial_throttle_ms", 0)

	// Meeting minutes 默认配置
	l.v.SetDefault("meeting_minutes.enabled", true)
	l.v.SetDefault("meeting_minutes.start_phrase", "开始会议纪要")
	l.v.SetDefault("meeting_minutes.stop_phrase", "结束会议纪要")
	l.v.SetDefault("meeting_minutes.output_dir", "./data/meeting-minutes")
	l.v.SetDefault("meeting_minutes.kb_id", 0)

	// Log 默认配置
	l.v.SetDefault("log.level", "info")
	l.v.SetDefault("log.stdout", true)
	l.v.SetDefault("log.file.enabled", true)
	l.v.SetDefault("log.file.path", "./logs")
	l.v.SetDefault("log.file.name", "backend-server.log")
	l.v.SetDefault("log.file.max_size_mb", 100)
	l.v.SetDefault("log.file.max_age_days", 7)
	l.v.SetDefault("log.file.max_backups", 10)
	l.v.SetDefault("log.file.rotation_hours", 24)
	l.v.SetDefault("log.file.compress", false)

	// Telemetry 默认配置
	l.v.SetDefault("telemetry.tracing.enable", false)
	l.v.SetDefault("telemetry.tracing.exporter", "stdout")
	l.v.SetDefault("telemetry.tracing.endpoint", "")
	l.v.SetDefault("telemetry.tracing.insecure", true)
	l.v.SetDefault("telemetry.tracing.sample_ratio", 0.1)
	l.v.SetDefault("telemetry.tracing.service_name", "backend-server")
	l.v.SetDefault("telemetry.tracing.environment", "local")
	l.v.SetDefault("telemetry.tracing.flush_timeout_ms", 5000)

	// Redis 默认配置
	l.v.SetDefault("redis.host", "localhost")
	l.v.SetDefault("redis.port", 6379)
	l.v.SetDefault("redis.password", "")
	l.v.SetDefault("redis.db", 0)
	l.v.SetDefault("redis.key_prefix", "xiaozhi")

	// Memory 默认配置
	l.v.SetDefault("memory.enabled", "true")
	l.v.SetDefault("memory.type", "none")
	l.v.SetDefault("memory.short_term.enabled", true)
	l.v.SetDefault("memory.short_term.provider", "redis")
	l.v.SetDefault("memory.short_term.max_messages", 50)
	l.v.SetDefault("memory.short_term.ttl_seconds", 604800)
	l.v.SetDefault("memory.short_term.compression_enabled", false)
	l.v.SetDefault("memory.long_term.enabled", true)
	l.v.SetDefault("memory.long_term.provider", "manager")
	l.v.SetDefault("memory.long_term.batch_size", 10)
	l.v.SetDefault("memory.long_term.flush_interval_seconds", 5)
	l.v.SetDefault("memory.hybrid.strategy", "parallel")
	l.v.SetDefault("memory.hybrid.short_term_priority", true)
	l.v.SetDefault("memory.hybrid.long_term_context_max_tokens", 4000)

	// WebSocket 默认配置
	l.v.SetDefault("websocket.host", "0.0.0.0")
	l.v.SetDefault("websocket.port", 8989)
	l.v.SetDefault("websocket.fallback_proxy_url", "http://example.localhost:8007")

	// MQTT 默认配置
	l.v.SetDefault("mqtt.enable", false)
	l.v.SetDefault("mqtt.broker", "127.0.0.1")
	l.v.SetDefault("mqtt.type", "tcp")
	l.v.SetDefault("mqtt.port", 1883)
	l.v.SetDefault("mqtt.client_id", "xiaozhi_client")

	// MQTT Server 默认配置
	l.v.SetDefault("mqtt_server.enable", false)
	l.v.SetDefault("mqtt_server.listen_host", "0.0.0.0")
	l.v.SetDefault("mqtt_server.listen_port", 1883)
	l.v.SetDefault("mqtt_server.enable_auth", false)
	l.v.SetDefault("mqtt_server.tls.enable", false)

	// UDP 默认配置
	l.v.SetDefault("udp.external_host", "127.0.0.1")
	l.v.SetDefault("udp.external_port", 8765)
	l.v.SetDefault("udp.listen_host", "0.0.0.0")
	l.v.SetDefault("udp.listen_port", 8765)

	// VAD 默认配置
	l.v.SetDefault("vad.provider", "webrtc_vad")
	l.v.SetDefault("vad.webrtc_vad.pool_min_size", 5)
	l.v.SetDefault("vad.webrtc_vad.pool_max_size", 1000)
	l.v.SetDefault("vad.webrtc_vad.pool_max_idle", 100)
	l.v.SetDefault("vad.webrtc_vad.vad_sample_rate", 16000)
	l.v.SetDefault("vad.webrtc_vad.vad_mode", 2)
	l.v.SetDefault("vad.sherpa_vad.model_path", "config/models/vad/silero_vad.onnx")
	l.v.SetDefault("vad.sherpa_vad.sample_rate", 16000)
	l.v.SetDefault("vad.sherpa_vad.num_threads", 1)
	l.v.SetDefault("vad.sherpa_vad.provider", "cpu")
	l.v.SetDefault("vad.sherpa_vad.debug", 0)
	l.v.SetDefault("vad.sherpa_vad.threshold", 0.5)
	l.v.SetDefault("vad.sherpa_vad.min_silence_duration", 0.5)
	l.v.SetDefault("vad.sherpa_vad.min_silence_duration_ms", 100)
	l.v.SetDefault("vad.sherpa_vad.min_speech_duration", 0.25)
	l.v.SetDefault("vad.sherpa_vad.max_speech_duration", 0.0)
	l.v.SetDefault("vad.sherpa_vad.window_size", 512)
	l.v.SetDefault("vad.sherpa_vad.buffer_size_seconds", 5.0)
	l.v.SetDefault("vad.sherpa_vad.pool_size", 100)
	l.v.SetDefault("vad.sherpa_vad.acquire_timeout_ms", 3000)

	// ASR 默认配置
	l.v.SetDefault("asr.provider", "funasr")
	l.v.SetDefault("asr.funasr.mode", "offline")
	l.v.SetDefault("asr.funasr.sample_rate", 16000)
	l.v.SetDefault("asr.funasr.max_connections", 5)
	l.v.SetDefault("asr.funasr.timeout", 30)
	l.v.SetDefault("asr.funasr.auto_end", true)
	l.v.SetDefault("asr.elevenlabs.model_id", "scribe_v1")
	l.v.SetDefault("asr.elevenlabs.file_format", "pcm_s16le_16")
	l.v.SetDefault("asr.elevenlabs.sample_rate", 16000)
	l.v.SetDefault("asr.elevenlabs.timeout_seconds", 60)
	l.v.SetDefault("asr.elevenlabs.enable_logging", true)
	l.v.SetDefault("asr.elevenlabs.timestamps_granularity", "word")
	l.v.SetDefault("asr.google_genai.sample_rate", 16000)

	// TTS 默认配置
	l.v.SetDefault("tts.provider", "xiaozhi")
	l.v.SetDefault("tts.elevenlabs.model_id", "eleven_multilingual_v2")
	l.v.SetDefault("tts.elevenlabs.output_format", "mp3_44100_128")
	l.v.SetDefault("tts.elevenlabs.optimize_streaming_latency", 0)
	l.v.SetDefault("tts.elevenlabs.timeout_seconds", 120)
	l.v.SetDefault("tts.google_genai.sample_rate", 24000)
	l.v.SetDefault("tts.google_genai.frame_duration", 20)

	// LLM 默认配置
	l.v.SetDefault("llm.provider", "local")

	// Manager API 默认配置
	l.v.SetDefault("manager_api.enabled", false)
	l.v.SetDefault("manager_api.timeout_seconds", 30)
	l.v.SetDefault("manager_api.retry_attempts", 3)
	l.v.SetDefault("manager_api.retry_delay_ms", 1000)
	l.v.SetDefault("manager_api.max_retry_delay_ms", 10000)
	l.v.SetDefault("manager_api.cache_ttl_seconds", 300)
	l.v.SetDefault("manager_api.health_check_interval_seconds", 5)
	l.v.SetDefault("manager_api.batch_size", 50)
	l.v.SetDefault("manager_api.flush_interval_seconds", 10)
	l.v.SetDefault("manager_api.circuit_breaker.max_failures", 5)
	l.v.SetDefault("manager_api.circuit_breaker.open_timeout_seconds", 30)
	l.v.SetDefault("manager_api.circuit_breaker.reset_timeout_seconds", 10)
	l.v.SetDefault("manager_api.rate_limit.requests_per_second", 0)
	l.v.SetDefault("manager_api.rate_limit.burst", 0)

	// Vision 默认配置
	l.v.SetDefault("vision.enable_auth", false)
	l.v.SetDefault("vision.vllm.provider", "aliyun_vision")
	l.v.SetDefault("vision.ingest.enabled", false)
	l.v.SetDefault("vision.ingest.sync_interval_seconds", 20)
	l.v.SetDefault("vision.ingest.snapshot_timeout_seconds", 8)
	l.v.SetDefault("vision.ingest.upload_media", true)
	l.v.SetDefault("vision.sampling.default_interval_ms", 1000)
	l.v.SetDefault("vision.sampling.max_inflight", 2)
	l.v.SetDefault("vision.sampling.global_max_inflight", 32)
	l.v.SetDefault("vision.backoff.initial_ms", 500)
	l.v.SetDefault("vision.backoff.max_ms", 10000)
	l.v.SetDefault("vision.retention_days", 7)

	// Image Generation 默认配置
	l.v.SetDefault("image.provider", "")

	// Video Generation 默认配置
	l.v.SetDefault("video.provider", "")

	// MCP 默认配置
	l.v.SetDefault("mcp.global.enabled", false)
	l.v.SetDefault("mcp.global.reconnect_interval", 300)
	l.v.SetDefault("mcp.global.max_reconnect_attempts", 10)
	l.v.SetDefault("mcp.device.enabled", false)
	l.v.SetDefault("mcp.device.websocket_path", "/xiaozhi/mcp/")
	l.v.SetDefault("mcp.device.max_connections_per_device", 5)

	// Music 默认配置
	l.v.SetDefault("music.provider", "subsonic")

	// Channels 默认配置
	l.v.SetDefault("channels.websocket.cmd_buffer", 100)
	l.v.SetDefault("channels.websocket.audio_buffer", 100)
	l.v.SetDefault("channels.mqtt.message_buffer", 10000)
	l.v.SetDefault("channels.udp.cmd_buffer", 100)
	l.v.SetDefault("channels.udp.audio_buffer", 100)
	l.v.SetDefault("channels.session.chat_text_queue", 10)
	l.v.SetDefault("channels.session.llm_result_queue", 10)
	l.v.SetDefault("channels.session.tts_queue", 10)
	l.v.SetDefault("channels.default_buffer", 100)
	l.v.SetDefault("channels.drop_policy", "block")
	l.v.SetDefault("channels.timeout", 5)
	l.v.SetDefault("channels.enable_metrics", false)
	l.v.SetDefault("channels.metrics_interval", 10)

	// Greeting 默认配置
	l.v.SetDefault("enable_greeting", false)
	l.v.SetDefault("wakeup_word_match.enable_pinyin_fuzzy", true)
	l.v.SetDefault("wakeup_word_match.max_pinyin_distance", 1)
}

// Unmarshal 反序列化配置到结构体
func (l *Loader) Unmarshal(config *AppConfig) error {
	if err := l.v.Unmarshal(config); err != nil {
		return fmt.Errorf("配置反序列化失败: %w", err)
	}
	normalizeVADConfig(config)
	return nil
}

// Load 完整的配置加载流程
func (l *Loader) Load(configPath string) (*AppConfig, error) {
	// 1. 设置默认值
	l.SetDefaults()

	// 2. 从文件加载配置
	if configPath != "" {
		if err := l.LoadFromFile(configPath); err != nil {
			return nil, fmt.Errorf("从文件加载配置失败: %w", err)
		}
	}

	// 3. 从环境变量加载配置（会覆盖文件配置）
	if err := l.LoadFromEnv("XIAOZHI"); err != nil {
		return nil, fmt.Errorf("从环境变量加载配置失败: %w", err)
	}

	// 4. 反序列化到结构体
	config := &AppConfig{}
	if err := l.Unmarshal(config); err != nil {
		return nil, fmt.Errorf("配置反序列化失败: %w", err)
	}

	// 5. 验证配置
	if err := ValidateConfig(config); err != nil {
		return nil, fmt.Errorf("配置验证失败: %w", err)
	}

	return config, nil
}

// GetViper 获取底层的viper实例（用于兼容旧代码）
func (l *Loader) GetViper() *viper.Viper {
	return l.v
}

// splitFileNameAndExt 分离文件名和扩展名
func splitFileNameAndExt(file string) (string, string) {
	if pos := strings.LastIndex(file, "."); pos != -1 {
		return file[:pos], strings.ToLower(file[pos+1:])
	}
	return file, ""
}

// LoadConfig 全局配置加载函数（便捷方法）
func LoadConfig(configPath string) (*AppConfig, error) {
	loader := NewLoader()
	return loader.Load(configPath)
}

func normalizeVADConfig(cfg *AppConfig) {
	if cfg == nil {
		return
	}

	legacy := cfg.VAD.SileroVAD
	if cfg.VAD.SherpaVAD.ModelPath == "" && legacy.ModelPath != "" {
		cfg.VAD.SherpaVAD = legacy
	}

	if cfg.VAD.Provider == constants.VadTypeSileroVad {
		log.Printf("[config] vad.provider=silero_vad 已弃用，自动切换为 sherpa_vad")
		cfg.VAD.Provider = constants.VadTypeSherpaVad
	}
}

// LoadConfigWithViper 加载配置并返回viper实例（用于向后兼容）
func LoadConfigWithViper(configPath string) (*AppConfig, *viper.Viper, error) {
	loader := NewLoader()
	config, err := loader.Load(configPath)
	if err != nil {
		return nil, nil, err
	}
	return config, loader.GetViper(), nil
}

// MustLoadConfig 加载配置，失败则panic
func MustLoadConfig(configPath string) *AppConfig {
	config, err := LoadConfig(configPath)
	if err != nil {
		fmt.Printf("加载配置失败: %v\n", err)
		os.Exit(1)
	}
	return config
}
