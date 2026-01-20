package config

import (
	"time"
)

// AppConfig 应用程序完整配置结构
type AppConfig struct {
	Server          ServerConfig          `mapstructure:"server" yaml:"server" json:"server"`
	Auth            AuthConfig            `mapstructure:"auth" yaml:"auth" json:"auth"`
	Chat            ChatConfig            `mapstructure:"chat" yaml:"chat" json:"chat"`
	MeetingMinutes  MeetingMinutesConfig  `mapstructure:"meeting_minutes" yaml:"meeting_minutes" json:"meeting_minutes"`
	SystemPrompt    string                `mapstructure:"system_prompt" yaml:"system_prompt" json:"system_prompt"`
	Log             LogConfig             `mapstructure:"log" yaml:"log" json:"log"`
	Telemetry       TelemetryConfig       `mapstructure:"telemetry" yaml:"telemetry" json:"telemetry"`
	Redis           RedisConfig           `mapstructure:"redis" yaml:"redis" json:"redis"`
	Memory          MemoryConfig          `mapstructure:"memory" yaml:"memory" json:"memory"`
	WebSocket       WebSocketConfig       `mapstructure:"websocket" yaml:"websocket" json:"websocket"`
	MQTT            MQTTConfig            `mapstructure:"mqtt" yaml:"mqtt" json:"mqtt"`
	MQTTServer      MQTTServerConfig      `mapstructure:"mqtt_server" yaml:"mqtt_server" json:"mqtt_server"`
	UDP             UDPConfig             `mapstructure:"udp" yaml:"udp" json:"udp"`
	VAD             VADConfig             `mapstructure:"vad" yaml:"vad" json:"vad"`
	ASR             ASRConfig             `mapstructure:"asr" yaml:"asr" json:"asr"`
	TTS             TTSConfig             `mapstructure:"tts" yaml:"tts" json:"tts"`
	LLM             LLMConfig             `mapstructure:"llm" yaml:"llm" json:"llm"`
	DefaultMods     DefaultModules        `mapstructure:"default_modules" yaml:"default_modules" json:"default_modules"`
	ManagerAPI      ManagerAPIConfig      `mapstructure:"manager_api" yaml:"manager_api" json:"manager_api"`
	Vision          VisionConfig          `mapstructure:"vision" yaml:"vision" json:"vision"`
	Image           ImageGenerationConfig `mapstructure:"image" yaml:"image" json:"image"`
	Video           VideoGenerationConfig `mapstructure:"video" yaml:"video" json:"video"`
	OTA             OTAConfig             `mapstructure:"ota" yaml:"ota" json:"ota"`
	MCP             MCPConfig             `mapstructure:"mcp" yaml:"mcp" json:"mcp"`
	Channels        ChannelConfig         `mapstructure:"channels" yaml:"channels" json:"channels"`
	Greeting        GreetingConfig        `mapstructure:",inline" yaml:",inline" json:",inline"`
	WakeupWords     []string              `mapstructure:"wakeup_words" yaml:"wakeup_words" json:"wakeup_words"`
	WakeupWordMatch WakeupWordMatchConfig `mapstructure:"wakeup_word_match" yaml:"wakeup_word_match" json:"wakeup_word_match"`
}

// ServerConfig 服务器配置
type ServerConfig struct {
	PProf PprofConfig `mapstructure:"pprof" yaml:"pprof" json:"pprof"`
}

// PprofConfig pprof性能分析配置
type PprofConfig struct {
	Enable bool `mapstructure:"enable" yaml:"enable" json:"enable"`
	Port   int  `mapstructure:"port" yaml:"port" json:"port"`
}

// AuthConfig 身份验证配置
type AuthConfig struct {
	Enable bool `mapstructure:"enable" yaml:"enable" json:"enable"`
}

// ChatConfig 聊天配置
type ChatConfig struct {
	MaxIdleDuration              int               `mapstructure:"max_idle_duration" yaml:"max_idle_duration" json:"max_idle_duration"`
	ChatMaxSilenceDuration       int               `mapstructure:"chat_max_silence_duration" yaml:"chat_max_silence_duration" json:"chat_max_silence_duration"`
	SessionTimeoutMinutes        int               `mapstructure:"session_timeout_minutes" yaml:"session_timeout_minutes" json:"session_timeout_minutes"`
	AutoResumeListening          bool              `mapstructure:"auto_resume_listening" yaml:"auto_resume_listening" json:"auto_resume_listening"`
	ReassureToolDelayMs          int               `mapstructure:"reassure_tool_delay_ms" yaml:"reassure_tool_delay_ms" json:"reassure_tool_delay_ms"`
	ReassureToolFollowupMs       int               `mapstructure:"reassure_tool_followup_ms" yaml:"reassure_tool_followup_ms" json:"reassure_tool_followup_ms"`
	ReassureToolMessage          string            `mapstructure:"reassure_tool_message" yaml:"reassure_tool_message" json:"reassure_tool_message"`
	ReassureToolFollowupMessage  string            `mapstructure:"reassure_tool_followup_message" yaml:"reassure_tool_followup_message" json:"reassure_tool_followup_message"`
	ReassureToolLongwaitMessage  string            `mapstructure:"reassure_tool_longwait_message" yaml:"reassure_tool_longwait_message" json:"reassure_tool_longwait_message"`
	ReassureToolMessages         map[string]string `mapstructure:"reassure_tool_messages" yaml:"reassure_tool_messages" json:"reassure_tool_messages"`
	ReassureToolLongwaitMessages map[string]string `mapstructure:"reassure_tool_longwait_messages" yaml:"reassure_tool_longwait_messages" json:"reassure_tool_longwait_messages"`
	RealtimeMode                 int               `mapstructure:"realtime_mode" yaml:"realtime_mode" json:"realtime_mode"`                                        // 1=VAD 打断, 2=ASR 打断
	RealtimeMinVoiceMs           int               `mapstructure:"realtime_min_voice_ms" yaml:"realtime_min_voice_ms" json:"realtime_min_voice_ms"`                // VAD 触发打断的最小连续发声时长
	RealtimeInterruptEnabled     bool              `mapstructure:"realtime_interrupt_enabled" yaml:"realtime_interrupt_enabled" json:"realtime_interrupt_enabled"` // 是否启用 realtime 打断
	AsrPartialThrottleMs         int               `mapstructure:"asr_partial_throttle_ms" yaml:"asr_partial_throttle_ms" json:"asr_partial_throttle_ms"`
}

// MeetingMinutesConfig configures meeting minutes capture and ingestion.
type MeetingMinutesConfig struct {
	Enabled     bool   `mapstructure:"enabled" yaml:"enabled" json:"enabled"`
	StartPhrase string `mapstructure:"start_phrase" yaml:"start_phrase" json:"start_phrase"`
	StopPhrase  string `mapstructure:"stop_phrase" yaml:"stop_phrase" json:"stop_phrase"`
	OutputDir   string `mapstructure:"output_dir" yaml:"output_dir" json:"output_dir"`
	KBID        uint64 `mapstructure:"kb_id" yaml:"kb_id" json:"kb_id"`
}

// LogConfig 日志配置
type LogConfig struct {
	Level  string        `mapstructure:"level" yaml:"level" json:"level"`
	Stdout bool          `mapstructure:"stdout" yaml:"stdout" json:"stdout"`
	File   LogFileConfig `mapstructure:"file" yaml:"file" json:"file"`
}

// LogFileConfig 文件日志配置
type LogFileConfig struct {
	Enabled       bool   `mapstructure:"enabled" yaml:"enabled" json:"enabled"`
	MaxSizeMB     int    `mapstructure:"max_size_mb" yaml:"max_size_mb" json:"max_size_mb"`
	MaxAgeDays    int    `mapstructure:"max_age_days" yaml:"max_age_days" json:"max_age_days"`
	MaxBackups    int    `mapstructure:"max_backups" yaml:"max_backups" json:"max_backups"`
	RotationHours int    `mapstructure:"rotation_hours" yaml:"rotation_hours" json:"rotation_hours"`
	Compress      bool   `mapstructure:"compress" yaml:"compress" json:"compress"`
	Path          string `mapstructure:"path" yaml:"path" json:"path"`
	Name          string `mapstructure:"name" yaml:"name" json:"name"`
}

// TelemetryConfig 遥测相关配置
type TelemetryConfig struct {
	Tracing TracingConfig `mapstructure:"tracing" yaml:"tracing" json:"tracing"`
}

// TracingConfig Trace 配置
type TracingConfig struct {
	Enable       bool    `mapstructure:"enable" yaml:"enable" json:"enable"`
	Exporter     string  `mapstructure:"exporter" yaml:"exporter" json:"exporter"`
	Endpoint     string  `mapstructure:"endpoint" yaml:"endpoint" json:"endpoint"`
	Insecure     bool    `mapstructure:"insecure" yaml:"insecure" json:"insecure"`
	SampleRatio  float64 `mapstructure:"sample_ratio" yaml:"sample_ratio" json:"sample_ratio"`
	ServiceName  string  `mapstructure:"service_name" yaml:"service_name" json:"service_name"`
	Environment  string  `mapstructure:"environment" yaml:"environment" json:"environment"`
	FlushTimeout int     `mapstructure:"flush_timeout_ms" yaml:"flush_timeout_ms" json:"flush_timeout_ms"`
}

// RedisConfig Redis数据库配置
type RedisConfig struct {
	Host      string `mapstructure:"host" yaml:"host" json:"host"`
	Port      int    `mapstructure:"port" yaml:"port" json:"port"`
	Password  string `mapstructure:"password" yaml:"password" json:"password"`
	DB        int    `mapstructure:"db" yaml:"db" json:"db"`
	KeyPrefix string `mapstructure:"key_prefix" yaml:"key_prefix" json:"key_prefix"`
}

// MemoryConfig 内存管理配置
type MemoryConfig struct {
	Enabled   string                `mapstructure:"enabled" yaml:"enabled" json:"enabled"`
	Type      string                `mapstructure:"type" yaml:"type" json:"type"`
	ShortTerm ShortTermMemoryConfig `mapstructure:"short_term" yaml:"short_term" json:"short_term"`
	LongTerm  LongTermMemoryConfig  `mapstructure:"long_term" yaml:"long_term" json:"long_term"`
	Hybrid    HybridMemoryConfig    `mapstructure:"hybrid" yaml:"hybrid" json:"hybrid"`
}

// ShortTermMemoryConfig 短期记忆配置
type ShortTermMemoryConfig struct {
	Enabled            bool        `mapstructure:"enabled" yaml:"enabled" json:"enabled"`
	Provider           string      `mapstructure:"provider" yaml:"provider" json:"provider"`
	MaxMessages        int         `mapstructure:"max_messages" yaml:"max_messages" json:"max_messages"`
	TTLSeconds         int         `mapstructure:"ttl_seconds" yaml:"ttl_seconds" json:"ttl_seconds"`
	CompressionEnabled bool        `mapstructure:"compression_enabled" yaml:"compression_enabled" json:"compression_enabled"`
	Redis              RedisConfig `mapstructure:"redis" yaml:"redis" json:"redis"`
}

// LongTermMemoryConfig 长期记忆配置
type LongTermMemoryConfig struct {
	Enabled              bool                              `mapstructure:"enabled" yaml:"enabled" json:"enabled"`
	Provider             string                            `mapstructure:"provider" yaml:"provider" json:"provider"`
	BatchSize            int                               `mapstructure:"batch_size" yaml:"batch_size" json:"batch_size"`
	FlushIntervalSeconds int                               `mapstructure:"flush_interval_seconds" yaml:"flush_interval_seconds" json:"flush_interval_seconds"`
	Providers            map[string]map[string]interface{} `mapstructure:"providers" yaml:"providers" json:"providers"`
}

// HybridMemoryConfig 混合记忆策略配置
type HybridMemoryConfig struct {
	Strategy                 string `mapstructure:"strategy" yaml:"strategy" json:"strategy"`
	ShortTermPriority        bool   `mapstructure:"short_term_priority" yaml:"short_term_priority" json:"short_term_priority"`
	LongTermContextMaxTokens int    `mapstructure:"long_term_context_max_tokens" yaml:"long_term_context_max_tokens" json:"long_term_context_max_tokens"`
}

// WebSocketConfig WebSocket服务配置
type WebSocketConfig struct {
	Host             string `mapstructure:"host" yaml:"host" json:"host"`
	Port             int    `mapstructure:"port" yaml:"port" json:"port"`
	FallbackProxyURL string `mapstructure:"fallback_proxy_url" yaml:"fallback_proxy_url" json:"fallback_proxy_url"`
}

// MQTTConfig MQTT客户端配置
type MQTTConfig struct {
	Enable   bool   `mapstructure:"enable" yaml:"enable" json:"enable"`
	Broker   string `mapstructure:"broker" yaml:"broker" json:"broker"`
	Type     string `mapstructure:"type" yaml:"type" json:"type"`
	Port     int    `mapstructure:"port" yaml:"port" json:"port"`
	ClientID string `mapstructure:"client_id" yaml:"client_id" json:"client_id"`
	Username string `mapstructure:"username" yaml:"username" json:"username"`
	Password string `mapstructure:"password" yaml:"password" json:"password"`
}

// MQTTServerConfig MQTT服务器配置
type MQTTServerConfig struct {
	Enable       bool      `mapstructure:"enable" yaml:"enable" json:"enable"`
	ListenHost   string    `mapstructure:"listen_host" yaml:"listen_host" json:"listen_host"`
	ListenPort   int       `mapstructure:"listen_port" yaml:"listen_port" json:"listen_port"`
	ClientID     string    `mapstructure:"client_id" yaml:"client_id" json:"client_id"`
	Username     string    `mapstructure:"username" yaml:"username" json:"username"`
	Password     string    `mapstructure:"password" yaml:"password" json:"password"`
	SignatureKey string    `mapstructure:"signature_key" yaml:"signature_key" json:"signature_key"`
	EnableAuth   bool      `mapstructure:"enable_auth" yaml:"enable_auth" json:"enable_auth"`
	TLS          TLSConfig `mapstructure:"tls" yaml:"tls" json:"tls"`
}

// TLSConfig TLS配置
type TLSConfig struct {
	Enable bool   `mapstructure:"enable" yaml:"enable" json:"enable"`
	Port   int    `mapstructure:"port" yaml:"port" json:"port"`
	Pem    string `mapstructure:"pem" yaml:"pem" json:"pem"`
	Key    string `mapstructure:"key" yaml:"key" json:"key"`
}

// UDPConfig UDP服务配置
type UDPConfig struct {
	ExternalHost string `mapstructure:"external_host" yaml:"external_host" json:"external_host"`
	ExternalPort int    `mapstructure:"external_port" yaml:"external_port" json:"external_port"`
	ListenHost   string `mapstructure:"listen_host" yaml:"listen_host" json:"listen_host"`
	ListenPort   int    `mapstructure:"listen_port" yaml:"listen_port" json:"listen_port"`
}

// VADConfig 语音活动检测配置
type VADConfig struct {
	Provider  string          `mapstructure:"provider" yaml:"provider" json:"provider"`
	WebRTCVAD WebRTCVADConfig `mapstructure:"webrtc_vad" yaml:"webrtc_vad" json:"webrtc_vad"`
	SherpaVAD SherpaVADConfig `mapstructure:"sherpa_vad" yaml:"sherpa_vad" json:"sherpa_vad"`
	SileroVAD SherpaVADConfig `mapstructure:"silero_vad" yaml:"silero_vad" json:"silero_vad"`
}

// WebRTCVADConfig WebRTC VAD配置
type WebRTCVADConfig struct {
	PoolMinSize     int     `mapstructure:"pool_min_size" yaml:"pool_min_size" json:"pool_min_size"`
	PoolMaxSize     int     `mapstructure:"pool_max_size" yaml:"pool_max_size" json:"pool_max_size"`
	PoolMaxIdle     int     `mapstructure:"pool_max_idle" yaml:"pool_max_idle" json:"pool_max_idle"`
	VadSampleRate   int     `mapstructure:"vad_sample_rate" yaml:"vad_sample_rate" json:"vad_sample_rate"`
	VadMode         int     `mapstructure:"vad_mode" yaml:"vad_mode" json:"vad_mode"`
	NoiseFloor      float64 `mapstructure:"noise_floor" yaml:"noise_floor" json:"noise_floor"`
	MinActiveFrames int     `mapstructure:"min_active_frames" yaml:"min_active_frames" json:"min_active_frames"`
}

// SherpaVADConfig sherpa-onnx VAD配置（兼容旧的 silero_vad 配置字段）
type SherpaVADConfig struct {
	ModelPath            string  `mapstructure:"model_path" yaml:"model_path" json:"model_path"`
	SampleRate           int     `mapstructure:"sample_rate" yaml:"sample_rate" json:"sample_rate"`
	NumThreads           int     `mapstructure:"num_threads" yaml:"num_threads" json:"num_threads"`
	Provider             string  `mapstructure:"provider" yaml:"provider" json:"provider"`
	Debug                int     `mapstructure:"debug" yaml:"debug" json:"debug"`
	Threshold            float64 `mapstructure:"threshold" yaml:"threshold" json:"threshold"`
	MinSilenceDuration   float64 `mapstructure:"min_silence_duration" yaml:"min_silence_duration" json:"min_silence_duration"`
	MinSilenceDurationMs int     `mapstructure:"min_silence_duration_ms" yaml:"min_silence_duration_ms" json:"min_silence_duration_ms"`
	MinSpeechDuration    float64 `mapstructure:"min_speech_duration" yaml:"min_speech_duration" json:"min_speech_duration"`
	MaxSpeechDuration    float64 `mapstructure:"max_speech_duration" yaml:"max_speech_duration" json:"max_speech_duration"`
	WindowSize           int     `mapstructure:"window_size" yaml:"window_size" json:"window_size"`
	BufferSizeSeconds    float64 `mapstructure:"buffer_size_seconds" yaml:"buffer_size_seconds" json:"buffer_size_seconds"`
	PoolSize             int     `mapstructure:"pool_size" yaml:"pool_size" json:"pool_size"`
	AcquireTimeoutMs     int     `mapstructure:"acquire_timeout_ms" yaml:"acquire_timeout_ms" json:"acquire_timeout_ms"`
}

// ASRConfig 自动语音识别配置
type ASRConfig struct {
	Provider       string                  `mapstructure:"provider" yaml:"provider" json:"provider"`
	FunASR         FunASRConfig            `mapstructure:"funasr" yaml:"funasr" json:"funasr"`
	Doubao         DoubaoASRConfig         `mapstructure:"doubao" yaml:"doubao" json:"doubao"`
	OpenAIRealtime OpenAIRealtimeASRConfig `mapstructure:"openai_realtime" yaml:"openai_realtime" json:"openai_realtime"`
	ElevenLabs     ElevenLabsASRConfig     `mapstructure:"elevenlabs" yaml:"elevenlabs" json:"elevenlabs"`
	GoogleGenAI    GoogleGenAIASRConfig    `mapstructure:"google_genai" yaml:"google_genai" json:"google_genai"`
}

// FunASRConfig FunASR配置
type FunASRConfig struct {
	Host           string `mapstructure:"host" yaml:"host" json:"host"`
	Port           string `mapstructure:"port" yaml:"port" json:"port"`
	Mode           string `mapstructure:"mode" yaml:"mode" json:"mode"`
	SampleRate     int    `mapstructure:"sample_rate" yaml:"sample_rate" json:"sample_rate"`
	ChunkSize      []int  `mapstructure:"chunk_size" yaml:"chunk_size" json:"chunk_size"`
	ChunkInterval  int    `mapstructure:"chunk_interval" yaml:"chunk_interval" json:"chunk_interval"`
	MaxConnections int    `mapstructure:"max_connections" yaml:"max_connections" json:"max_connections"`
	Timeout        int    `mapstructure:"timeout" yaml:"timeout" json:"timeout"`
	AutoEnd        bool   `mapstructure:"auto_end" yaml:"auto_end" json:"auto_end"`
}

// DoubaoASRConfig 豆包ASR配置
type DoubaoASRConfig struct {
	AppID         string `mapstructure:"appid" yaml:"appid" json:"appid"`
	AccessToken   string `mapstructure:"access_token" yaml:"access_token" json:"access_token"`
	Host          string `mapstructure:"host" yaml:"host" json:"host"`
	WsURL         string `mapstructure:"ws_url" yaml:"ws_url" json:"ws_url"`
	ModelName     string `mapstructure:"model_name" yaml:"model_name" json:"model_name"`
	EndWindowSize int    `mapstructure:"end_window_size" yaml:"end_window_size" json:"end_window_size"`
	EnablePunc    bool   `mapstructure:"enable_punc" yaml:"enable_punc" json:"enable_punc"`
	EnableITN     bool   `mapstructure:"enable_itn" yaml:"enable_itn" json:"enable_itn"`
	EnableDDC     bool   `mapstructure:"enable_ddc" yaml:"enable_ddc" json:"enable_ddc"`
	Timeout       int    `mapstructure:"timeout" yaml:"timeout" json:"timeout"`
}

// OpenAIRealtimeASRConfig 基于OpenAI Realtime协议的ASR配置
type OpenAIRealtimeASRConfig struct {
	URL           string `mapstructure:"url" yaml:"url" json:"url"`
	AuthToken     string `mapstructure:"auth_token" yaml:"auth_token" json:"auth_token"`
	SampleRate    int    `mapstructure:"sample_rate" yaml:"sample_rate" json:"sample_rate"`
	FrameDuration int    `mapstructure:"frame_duration" yaml:"frame_duration" json:"frame_duration"`
}

// ElevenLabsASRConfig ElevenLabs scribe speech-to-text
type ElevenLabsASRConfig struct {
	APIKey                string `mapstructure:"api_key" yaml:"api_key" json:"api_key"`
	ModelID               string `mapstructure:"model_id" yaml:"model_id" json:"model_id"`
	LanguageCode          string `mapstructure:"language_code" yaml:"language_code" json:"language_code"`
	Region                string `mapstructure:"region" yaml:"region" json:"region"`
	FileFormat            string `mapstructure:"file_format" yaml:"file_format" json:"file_format"`
	SampleRate            int    `mapstructure:"sample_rate" yaml:"sample_rate" json:"sample_rate"`
	TimeoutSeconds        int    `mapstructure:"timeout_seconds" yaml:"timeout_seconds" json:"timeout_seconds"`
	TimestampsGranularity string `mapstructure:"timestamps_granularity" yaml:"timestamps_granularity" json:"timestamps_granularity"`
	EnableLogging         bool   `mapstructure:"enable_logging" yaml:"enable_logging" json:"enable_logging"`
	BaseURL               string `mapstructure:"base_url" yaml:"base_url" json:"base_url"`
}

// GoogleGenAIASRConfig Google GenAI Live ASR配置
type GoogleGenAIASRConfig struct {
	APIKey     string `mapstructure:"api_key" yaml:"api_key" json:"api_key"`
	Backend    string `mapstructure:"backend" yaml:"backend" json:"backend"`
	Project    string `mapstructure:"project" yaml:"project" json:"project"`
	Location   string `mapstructure:"location" yaml:"location" json:"location"`
	Model      string `mapstructure:"model" yaml:"model" json:"model"`
	BaseURL    string `mapstructure:"base_url" yaml:"base_url" json:"base_url"`
	APIVersion string `mapstructure:"api_version" yaml:"api_version" json:"api_version"`
	SampleRate int    `mapstructure:"sample_rate" yaml:"sample_rate" json:"sample_rate"`
}

// TTSConfig 文本转语音配置
type TTSConfig struct {
	Provider       string                  `mapstructure:"provider" yaml:"provider" json:"provider"`
	Doubao         DoubaoTTSConfig         `mapstructure:"doubao" yaml:"doubao" json:"doubao"`
	DoubaoWS       DoubaoWSTTSConfig       `mapstructure:"doubao_ws" yaml:"doubao_ws" json:"doubao_ws"`
	Cosyvoice      CosyvoiceTTSConfig      `mapstructure:"cosyvoice" yaml:"cosyvoice" json:"cosyvoice"`
	Edge           EdgeTTSConfig           `mapstructure:"edge" yaml:"edge" json:"edge"`
	EdgeOffline    EdgeOfflineTTSConfig    `mapstructure:"edge_offline" yaml:"edge_offline" json:"edge_offline"`
	Xiaozhi        XiaozhiTTSConfig        `mapstructure:"xiaozhi" yaml:"xiaozhi" json:"xiaozhi"`
	OpenAIRealtime OpenAIRealtimeTTSConfig `mapstructure:"openai_realtime" yaml:"openai_realtime" json:"openai_realtime"`
	ElevenLabs     ElevenLabsTTSConfig     `mapstructure:"elevenlabs" yaml:"elevenlabs" json:"elevenlabs"`
	GoogleGenAI    GoogleGenAITTSConfig    `mapstructure:"google_genai" yaml:"google_genai" json:"google_genai"`
}

// DoubaoTTSConfig 豆包TTS配置
type DoubaoTTSConfig struct {
	AppID         string `mapstructure:"appid" yaml:"appid" json:"appid"`
	AccessToken   string `mapstructure:"access_token" yaml:"access_token" json:"access_token"`
	Cluster       string `mapstructure:"cluster" yaml:"cluster" json:"cluster"`
	Voice         string `mapstructure:"voice" yaml:"voice" json:"voice"`
	APIURL        string `mapstructure:"api_url" yaml:"api_url" json:"api_url"`
	Authorization string `mapstructure:"authorization" yaml:"authorization" json:"authorization"`
}

// DoubaoWSTTSConfig 豆包WebSocket TTS配置
type DoubaoWSTTSConfig struct {
	AppID          string `mapstructure:"appid" yaml:"appid" json:"appid"`
	AccessToken    string `mapstructure:"access_token" yaml:"access_token" json:"access_token"`
	Cluster        string `mapstructure:"cluster" yaml:"cluster" json:"cluster"`
	Voice          string `mapstructure:"voice" yaml:"voice" json:"voice"`
	WsHost         string `mapstructure:"ws_host" yaml:"ws_host" json:"ws_host"`
	UseStream      bool   `mapstructure:"use_stream" yaml:"use_stream" json:"use_stream"`
	MaxConnections int    `mapstructure:"max_connections" yaml:"max_connections" json:"max_connections"`
	MinConnections int    `mapstructure:"min_connections" yaml:"min_connections" json:"min_connections"`
	MaxIdleSeconds int    `mapstructure:"max_idle_seconds" yaml:"max_idle_seconds" json:"max_idle_seconds"`
	Warmup         bool   `mapstructure:"warmup" yaml:"warmup" json:"warmup"`
}

// CosyvoiceTTSConfig CosyVoice TTS配置
type CosyvoiceTTSConfig struct {
	APIURL        string `mapstructure:"api_url" yaml:"api_url" json:"api_url"`
	SpkID         string `mapstructure:"spk_id" yaml:"spk_id" json:"spk_id"`
	FrameDuration int    `mapstructure:"frame_duration" yaml:"frame_duration" json:"frame_duration"`
	TargetSR      int    `mapstructure:"target_sr" yaml:"target_sr" json:"target_sr"`
	AudioFormat   string `mapstructure:"audio_format" yaml:"audio_format" json:"audio_format"`
	InstructText  string `mapstructure:"instruct_text" yaml:"instruct_text" json:"instruct_text"`
}

// EdgeTTSConfig Edge TTS配置
type EdgeTTSConfig struct {
	Voice          string `mapstructure:"voice" yaml:"voice" json:"voice"`
	Rate           string `mapstructure:"rate" yaml:"rate" json:"rate"`
	Volume         string `mapstructure:"volume" yaml:"volume" json:"volume"`
	Pitch          string `mapstructure:"pitch" yaml:"pitch" json:"pitch"`
	ConnectTimeout int    `mapstructure:"connect_timeout" yaml:"connect_timeout" json:"connect_timeout"`
	ReceiveTimeout int    `mapstructure:"receive_timeout" yaml:"receive_timeout" json:"receive_timeout"`
}

// EdgeOfflineTTSConfig Edge离线TTS配置
type EdgeOfflineTTSConfig struct {
	ServerURL     string `mapstructure:"server_url" yaml:"server_url" json:"server_url"`
	Timeout       int    `mapstructure:"timeout" yaml:"timeout" json:"timeout"`
	SampleRate    int    `mapstructure:"sample_rate" yaml:"sample_rate" json:"sample_rate"`
	Channels      int    `mapstructure:"channels" yaml:"channels" json:"channels"`
	FrameDuration int    `mapstructure:"frame_duration" yaml:"frame_duration" json:"frame_duration"`
}

// XiaozhiTTSConfig 小智TTS配置
type XiaozhiTTSConfig struct {
	ServerAddr string `mapstructure:"server_addr" yaml:"server_addr" json:"server_addr"`
	DeviceID   string `mapstructure:"device_id" yaml:"device_id" json:"device_id"`
	ClientID   string `mapstructure:"client_id" yaml:"client_id" json:"client_id"`
	Token      string `mapstructure:"token" yaml:"token" json:"token"`
}

// OpenAIRealtimeTTSConfig 基于OpenAI Realtime协议的TTS配置
type OpenAIRealtimeTTSConfig struct {
	URL           string  `mapstructure:"url" yaml:"url" json:"url"`
	Model         string  `mapstructure:"model" yaml:"model" json:"model"`
	Voice         string  `mapstructure:"voice" yaml:"voice" json:"voice"`
	Speed         float32 `mapstructure:"speed" yaml:"speed" json:"speed"`
	SampleRate    int     `mapstructure:"sample_rate" yaml:"sample_rate" json:"sample_rate"`
	FrameDuration int     `mapstructure:"frame_duration" yaml:"frame_duration" json:"frame_duration"`
	AuthToken     string  `mapstructure:"auth_token" yaml:"auth_token" json:"auth_token"`
}

// ElevenLabsTTSConfig streaming TTS
type ElevenLabsTTSConfig struct {
	APIKey                 string `mapstructure:"api_key" yaml:"api_key" json:"api_key"`
	VoiceID                string `mapstructure:"voice_id" yaml:"voice_id" json:"voice_id"`
	ModelID                string `mapstructure:"model_id" yaml:"model_id" json:"model_id"`
	OutputFormat           string `mapstructure:"output_format" yaml:"output_format" json:"output_format"`
	OptimizeStreamingDelay int    `mapstructure:"optimize_streaming_latency" yaml:"optimize_streaming_latency" json:"optimize_streaming_latency"`
	Region                 string `mapstructure:"region" yaml:"region" json:"region"`
	TimeoutSeconds         int    `mapstructure:"timeout_seconds" yaml:"timeout_seconds" json:"timeout_seconds"`
	BaseURL                string `mapstructure:"base_url" yaml:"base_url" json:"base_url"`
}

// GoogleGenAITTSConfig Google GenAI Live TTS配置
type GoogleGenAITTSConfig struct {
	APIKey        string `mapstructure:"api_key" yaml:"api_key" json:"api_key"`
	Backend       string `mapstructure:"backend" yaml:"backend" json:"backend"`
	Project       string `mapstructure:"project" yaml:"project" json:"project"`
	Location      string `mapstructure:"location" yaml:"location" json:"location"`
	Model         string `mapstructure:"model" yaml:"model" json:"model"`
	Voice         string `mapstructure:"voice" yaml:"voice" json:"voice"`
	LanguageCode  string `mapstructure:"language_code" yaml:"language_code" json:"language_code"`
	BaseURL       string `mapstructure:"base_url" yaml:"base_url" json:"base_url"`
	APIVersion    string `mapstructure:"api_version" yaml:"api_version" json:"api_version"`
	SampleRate    int    `mapstructure:"sample_rate" yaml:"sample_rate" json:"sample_rate"`
	FrameDuration int    `mapstructure:"frame_duration" yaml:"frame_duration" json:"frame_duration"`
}

// LLMConfig 大语言模型配置
type LLMConfig struct {
	Provider  string                       `mapstructure:"provider" yaml:"provider" json:"provider"`
	Providers map[string]LLMProviderConfig `mapstructure:",remain" yaml:",inline"`
}

// LLMProviderConfig LLM提供商配置
type LLMProviderConfig struct {
	Type      string `mapstructure:"type" yaml:"type" json:"type"`
	ModelName string `mapstructure:"model_name" yaml:"model_name" json:"model_name"`
	APIKey    string `mapstructure:"api_key" yaml:"api_key" json:"api_key"`
	BaseURL   string `mapstructure:"base_url" yaml:"base_url" json:"base_url"`
	MaxTokens int    `mapstructure:"max_tokens" yaml:"max_tokens" json:"max_tokens"`
	MaxToken  int    `mapstructure:"max_token" yaml:"max_token" json:"max_token"` // 兼容性字段
}

// DefaultModules 默认模块配置
type DefaultModules struct {
	VAD string `mapstructure:"vad" yaml:"vad" json:"vad"`
	ASR string `mapstructure:"asr" yaml:"asr" json:"asr"`
	LLM string `mapstructure:"llm" yaml:"llm" json:"llm"`
	TTS string `mapstructure:"tts" yaml:"tts" json:"tts"`
}

// ManagerAPIConfig 管理API配置
type ManagerAPIConfig struct {
	Enabled                    bool                 `mapstructure:"enabled" yaml:"enabled" json:"enabled"`
	BaseURL                    string               `mapstructure:"base_url" yaml:"base_url" json:"base_url"`
	Secret                     string               `mapstructure:"secret" yaml:"secret" json:"secret"`
	TimeoutSeconds             int                  `mapstructure:"timeout_seconds" yaml:"timeout_seconds" json:"timeout_seconds"`
	RetryAttempts              int                  `mapstructure:"retry_attempts" yaml:"retry_attempts" json:"retry_attempts"`
	RetryDelayMs               int                  `mapstructure:"retry_delay_ms" yaml:"retry_delay_ms" json:"retry_delay_ms"`
	MaxRetryDelayMs            int                  `mapstructure:"max_retry_delay_ms" yaml:"max_retry_delay_ms" json:"max_retry_delay_ms"`
	CacheTTLSeconds            int                  `mapstructure:"cache_ttl_seconds" yaml:"cache_ttl_seconds" json:"cache_ttl_seconds"`
	HealthCheckIntervalSeconds int                  `mapstructure:"health_check_interval_seconds" yaml:"health_check_interval_seconds" json:"health_check_interval_seconds"`
	BatchSize                  int                  `mapstructure:"batch_size" yaml:"batch_size" json:"batch_size"`
	FlushIntervalSeconds       int                  `mapstructure:"flush_interval_seconds" yaml:"flush_interval_seconds" json:"flush_interval_seconds"`
	CircuitBreaker             CircuitBreakerConfig `mapstructure:"circuit_breaker" yaml:"circuit_breaker" json:"circuit_breaker"`
	RateLimit                  RateLimitConfig      `mapstructure:"rate_limit" yaml:"rate_limit" json:"rate_limit"`
}

type CircuitBreakerConfig struct {
	MaxFailures         int `mapstructure:"max_failures" yaml:"max_failures" json:"max_failures"`
	OpenTimeoutSeconds  int `mapstructure:"open_timeout_seconds" yaml:"open_timeout_seconds" json:"open_timeout_seconds"`
	ResetTimeoutSeconds int `mapstructure:"reset_timeout_seconds" yaml:"reset_timeout_seconds" json:"reset_timeout_seconds"`
}

type RateLimitConfig struct {
	RequestsPerSecond int `mapstructure:"requests_per_second" yaml:"requests_per_second" json:"requests_per_second"`
	Burst             int `mapstructure:"burst" yaml:"burst" json:"burst"`
}

// VisionConfig 视觉识别配置
type VisionConfig struct {
	EnableAuth    bool                 `mapstructure:"enable_auth" yaml:"enable_auth" json:"enable_auth"`
	VisionURL     string               `mapstructure:"vision_url" yaml:"vision_url" json:"vision_url"`
	VLLM          VLLMConfig           `mapstructure:"vllm" yaml:"vllm" json:"vllm"`
	Ingest        VisionIngestConfig   `mapstructure:"ingest" yaml:"ingest" json:"ingest"`
	Sampling      VisionSamplingConfig `mapstructure:"sampling" yaml:"sampling" json:"sampling"`
	Backoff       VisionBackoffConfig  `mapstructure:"backoff" yaml:"backoff" json:"backoff"`
	RetentionDays int                  `mapstructure:"retention_days" yaml:"retention_days" json:"retention_days"`
}

// VisionIngestConfig controls vision stream ingestion.
type VisionIngestConfig struct {
	Enabled                bool   `mapstructure:"enabled" yaml:"enabled" json:"enabled"`
	BaseURL                string `mapstructure:"base_url" yaml:"base_url" json:"base_url"`
	Token                  string `mapstructure:"token" yaml:"token" json:"token"`
	SyncIntervalSeconds    int    `mapstructure:"sync_interval_seconds" yaml:"sync_interval_seconds" json:"sync_interval_seconds"`
	SnapshotTimeoutSeconds int    `mapstructure:"snapshot_timeout_seconds" yaml:"snapshot_timeout_seconds" json:"snapshot_timeout_seconds"`
	UploadMedia            bool   `mapstructure:"upload_media" yaml:"upload_media" json:"upload_media"`
}

// VisionSamplingConfig controls sampling and concurrency.
type VisionSamplingConfig struct {
	DefaultIntervalMs int `mapstructure:"default_interval_ms" yaml:"default_interval_ms" json:"default_interval_ms"`
	MaxInflight       int `mapstructure:"max_inflight" yaml:"max_inflight" json:"max_inflight"`
	GlobalMaxInflight int `mapstructure:"global_max_inflight" yaml:"global_max_inflight" json:"global_max_inflight"`
}

// VisionBackoffConfig controls retry backoff.
type VisionBackoffConfig struct {
	InitialMs int `mapstructure:"initial_ms" yaml:"initial_ms" json:"initial_ms"`
	MaxMs     int `mapstructure:"max_ms" yaml:"max_ms" json:"max_ms"`
}

// ImageGenerationConfig 生图模型配置
type ImageGenerationConfig struct {
	Provider  string                         `mapstructure:"provider" yaml:"provider" json:"provider"`
	Providers map[string]ImageProviderConfig `mapstructure:",remain" yaml:",inline" json:",inline"`
}

// ImageProviderConfig 单个生图提供者配置
type ImageProviderConfig struct {
	Type              string `mapstructure:"type" yaml:"type" json:"type"`
	Model             string `mapstructure:"model" yaml:"model" json:"model"`
	ModelName         string `mapstructure:"model_name" yaml:"model_name" json:"model_name"`
	APIKey            string `mapstructure:"api_key" yaml:"api_key" json:"api_key"`
	BaseURL           string `mapstructure:"base_url" yaml:"base_url" json:"base_url"`
	NegativePrompt    string `mapstructure:"negative_prompt" yaml:"negative_prompt" json:"negative_prompt"`
	PromptExtend      bool   `mapstructure:"prompt_extend" yaml:"prompt_extend" json:"prompt_extend"`
	Watermark         bool   `mapstructure:"watermark" yaml:"watermark" json:"watermark"`
	Quality           string `mapstructure:"quality" yaml:"quality" json:"quality"`
	Size              string `mapstructure:"size" yaml:"size" json:"size"`
	Style             string `mapstructure:"style" yaml:"style" json:"style"`
	ResponseFormat    string `mapstructure:"response_format" yaml:"response_format" json:"response_format"`
	User              string `mapstructure:"user" yaml:"user" json:"user"`
	Background        string `mapstructure:"background" yaml:"background" json:"background"`
	Moderation        string `mapstructure:"moderation" yaml:"moderation" json:"moderation"`
	OutputCompression int    `mapstructure:"output_compression" yaml:"output_compression" json:"output_compression"`
	OutputFormat      string `mapstructure:"output_format" yaml:"output_format" json:"output_format"`
	N                 int    `mapstructure:"n" yaml:"n" json:"n"`
	Timeout           int    `mapstructure:"timeout" yaml:"timeout" json:"timeout"`
}

// VideoGenerationConfig 生视频模型配置
type VideoGenerationConfig struct {
	Provider  string                         `mapstructure:"provider" yaml:"provider" json:"provider"`
	Providers map[string]VideoProviderConfig `mapstructure:",remain" yaml:",inline" json:",inline"`
}

// VideoProviderConfig 单个生视频提供者配置
type VideoProviderConfig struct {
	Type                string `mapstructure:"type" yaml:"type" json:"type"`
	Model               string `mapstructure:"model" yaml:"model" json:"model"`
	ModelName           string `mapstructure:"model_name" yaml:"model_name" json:"model_name"`
	APIKey              string `mapstructure:"api_key" yaml:"api_key" json:"api_key"`
	BaseURL             string `mapstructure:"base_url" yaml:"base_url" json:"base_url"`
	RequestPath         string `mapstructure:"request_path" yaml:"request_path" json:"request_path"`
	TaskPath            string `mapstructure:"task_path" yaml:"task_path" json:"task_path"`
	Size                string `mapstructure:"size" yaml:"size" json:"size"`
	Duration            int    `mapstructure:"duration" yaml:"duration" json:"duration"`
	ShotType            string `mapstructure:"shot_type" yaml:"shot_type" json:"shot_type"`
	PromptExtend        bool   `mapstructure:"prompt_extend" yaml:"prompt_extend" json:"prompt_extend"`
	Capabilities        string `mapstructure:"capabilities" yaml:"capabilities" json:"capabilities"`
	Timeout             int    `mapstructure:"timeout" yaml:"timeout" json:"timeout"`
	PollIntervalSeconds int    `mapstructure:"poll_interval_seconds" yaml:"poll_interval_seconds" json:"poll_interval_seconds"`
	PollTimeoutSeconds  int    `mapstructure:"poll_timeout_seconds" yaml:"poll_timeout_seconds" json:"poll_timeout_seconds"`
}

// VLLMConfig 视觉语言模型配置
type VLLMConfig struct {
	Provider  string                       `mapstructure:"provider" yaml:"provider" json:"provider"`
	Providers map[string]LLMProviderConfig `mapstructure:",remain" yaml:",inline"`
}

// OTAConfig OTA升级配置
type OTAConfig struct {
	SignatureKey string       `mapstructure:"signature_key" yaml:"signature_key" json:"signature_key"`
	Test         OTAEnvConfig `mapstructure:"test" yaml:"test" json:"test"`
	External     OTAEnvConfig `mapstructure:"external" yaml:"external" json:"external"`
}

// OTAEnvConfig OTA环境配置
type OTAEnvConfig struct {
	WebSocket OTAWebSocketConfig `mapstructure:"websocket" yaml:"websocket" json:"websocket"`
	MQTT      OTAMQTTConfig      `mapstructure:"mqtt" yaml:"mqtt" json:"mqtt"`
}

// OTAWebSocketConfig OTA WebSocket配置
type OTAWebSocketConfig struct {
	URL string `mapstructure:"url" yaml:"url" json:"url"`
}

// OTAMQTTConfig OTA MQTT配置
type OTAMQTTConfig struct {
	Enable   bool   `mapstructure:"enable" yaml:"enable" json:"enable"`
	Endpoint string `mapstructure:"endpoint" yaml:"endpoint" json:"endpoint"`
}

// MCPConfig MCP配置
type MCPConfig struct {
	Global MCPGlobalConfig `mapstructure:"global" yaml:"global" json:"global"`
	Device MCPDeviceConfig `mapstructure:"device" yaml:"device" json:"device"`
}

// MCPGlobalConfig 全局MCP配置
type MCPGlobalConfig struct {
	Enabled              bool              `mapstructure:"enabled" yaml:"enabled" json:"enabled"`
	Servers              []MCPServerConfig `mapstructure:"servers" yaml:"servers" json:"servers"`
	ReconnectInterval    int               `mapstructure:"reconnect_interval" yaml:"reconnect_interval" json:"reconnect_interval"`
	MaxReconnectAttempts int               `mapstructure:"max_reconnect_attempts" yaml:"max_reconnect_attempts" json:"max_reconnect_attempts"`
}

// MCPServerConfig MCP服务器配置
type MCPServerConfig struct {
	Name    string `mapstructure:"name" yaml:"name" json:"name"`
	Type    string `mapstructure:"type" yaml:"type" json:"type"`
	URL     string `mapstructure:"url" yaml:"url" json:"url"`
	Enabled bool   `mapstructure:"enabled" yaml:"enabled" json:"enabled"`
}

// MCPDeviceConfig 设备级MCP配置
type MCPDeviceConfig struct {
	Enabled                 bool   `mapstructure:"enabled" yaml:"enabled" json:"enabled"`
	WebSocketPath           string `mapstructure:"websocket_path" yaml:"websocket_path" json:"websocket_path"`
	MaxConnectionsPerDevice int    `mapstructure:"max_connections_per_device" yaml:"max_connections_per_device" json:"max_connections_per_device"`
}

// ChannelConfig 通道配置
type ChannelConfig struct {
	WebSocket       ChannelTypeConfig    `mapstructure:"websocket" yaml:"websocket" json:"websocket"`
	MQTT            ChannelTypeConfig    `mapstructure:"mqtt" yaml:"mqtt" json:"mqtt"`
	UDP             ChannelTypeConfig    `mapstructure:"udp" yaml:"udp" json:"udp"`
	Session         SessionChannelConfig `mapstructure:"session" yaml:"session" json:"session"`
	DefaultBuffer   int                  `mapstructure:"default_buffer" yaml:"default_buffer" json:"default_buffer"`
	DropPolicy      string               `mapstructure:"drop_policy" yaml:"drop_policy" json:"drop_policy"`
	Timeout         int                  `mapstructure:"timeout" yaml:"timeout" json:"timeout"`
	EnableMetrics   bool                 `mapstructure:"enable_metrics" yaml:"enable_metrics" json:"enable_metrics"`
	MetricsInterval int                  `mapstructure:"metrics_interval" yaml:"metrics_interval" json:"metrics_interval"`
}

// ChannelTypeConfig 通道类型配置
type ChannelTypeConfig struct {
	CmdBuffer     int `mapstructure:"cmd_buffer" yaml:"cmd_buffer" json:"cmd_buffer"`
	AudioBuffer   int `mapstructure:"audio_buffer" yaml:"audio_buffer" json:"audio_buffer"`
	MessageBuffer int `mapstructure:"message_buffer" yaml:"message_buffer" json:"message_buffer"`
}

// SessionChannelConfig Session通道配置
type SessionChannelConfig struct {
	ChatTextQueue  int `mapstructure:"chat_text_queue" yaml:"chat_text_queue" json:"chat_text_queue"`
	LLMResultQueue int `mapstructure:"llm_result_queue" yaml:"llm_result_queue" json:"llm_result_queue"`
	TTSQueue       int `mapstructure:"tts_queue" yaml:"tts_queue" json:"tts_queue"`
}

// GreetingConfig 欢迎语配置
type GreetingConfig struct {
	EnableGreeting bool     `mapstructure:"enable_greeting" yaml:"enable_greeting" json:"enable_greeting"`
	GreetingList   []string `mapstructure:"greeting_list" yaml:"greeting_list" json:"greeting_list"`
}

// WakeupWordMatchConfig 唤醒词匹配配置
type WakeupWordMatchConfig struct {
	EnablePinyinFuzzy bool `mapstructure:"enable_pinyin_fuzzy" yaml:"enable_pinyin_fuzzy" json:"enable_pinyin_fuzzy"`
	MaxPinyinDistance int  `mapstructure:"max_pinyin_distance" yaml:"max_pinyin_distance" json:"max_pinyin_distance"`
}

// GetTimeout 获取超时Duration
func (l *LogConfig) GetTimeout() time.Duration {
	return time.Duration(l.File.RotationHours) * time.Hour
}

// GetMaxAge 获取日志最大保存时间Duration
func (l *LogConfig) GetMaxAge() time.Duration {
	return time.Duration(l.File.MaxAgeDays) * 24 * time.Hour
}

// GetTTL 获取TTL Duration
func (s *ShortTermMemoryConfig) GetTTL() time.Duration {
	return time.Duration(s.TTLSeconds) * time.Second
}

// GetFlushInterval 获取刷新间隔Duration
func (l *LongTermMemoryConfig) GetFlushInterval() time.Duration {
	return time.Duration(l.FlushIntervalSeconds) * time.Second
}

// GetTimeout 获取超时Duration
func (m *ManagerAPIConfig) GetTimeout() time.Duration {
	return time.Duration(m.TimeoutSeconds) * time.Second
}

// GetRetryDelay 获取重试延迟Duration
func (m *ManagerAPIConfig) GetRetryDelay() time.Duration {
	return time.Duration(m.RetryDelayMs) * time.Millisecond
}

// GetMaxRetryDelay 获取最大重试延迟Duration
func (m *ManagerAPIConfig) GetMaxRetryDelay() time.Duration {
	return time.Duration(m.MaxRetryDelayMs) * time.Millisecond
}

// GetCacheTTL 获取缓存TTL Duration
func (m *ManagerAPIConfig) GetCacheTTL() time.Duration {
	return time.Duration(m.CacheTTLSeconds) * time.Second
}

// GetHealthCheckInterval 获取健康检查间隔Duration
func (m *ManagerAPIConfig) GetHealthCheckInterval() time.Duration {
	return time.Duration(m.HealthCheckIntervalSeconds) * time.Second
}

// GetFlushInterval 获取刷新间隔Duration
func (m *ManagerAPIConfig) GetFlushInterval() time.Duration {
	return time.Duration(m.FlushIntervalSeconds) * time.Second
}

// GetTimeout 获取超时Duration
func (c *ChannelConfig) GetTimeout() time.Duration {
	return time.Duration(c.Timeout) * time.Second
}

// GetMetricsInterval 获取指标采样间隔Duration
func (c *ChannelConfig) GetMetricsInterval() time.Duration {
	return time.Duration(c.MetricsInterval) * time.Second
}
