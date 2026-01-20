package config

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
	configdefaults "manager-server/config"
)

// Config 应用配置结构体
type Config struct {
	Server          ServerConfig          `mapstructure:"server"`
	Database        DatabaseConfig        `mapstructure:"database"`
	Log             LogConfig             `mapstructure:"log"`
	JWT             JWTConfig             `mapstructure:"jwt"`
	Vision          VisionConfig          `mapstructure:"vision"`
	Memory          MemoryConfig          `mapstructure:"memory"`
	KnowledgeBase   KnowledgeBaseConfig   `mapstructure:"knowledge_base"`
	Audio           AudioConfig           `mapstructure:"audio"`
	Media           MediaConfig           `mapstructure:"media"`
	WorkflowRuntime WorkflowRuntimeConfig `mapstructure:"workflow_runtime"`
}

// ServerConfig 服务器配置
type ServerConfig struct {
	Port int    `mapstructure:"port"`
	Mode string `mapstructure:"mode"`
}

// DatabaseConfig 数据库配置
type DatabaseConfig struct {
	DSN          string `mapstructure:"dsn"`
	MaxIdleConns int    `mapstructure:"max_idle_conns"`
	MaxOpenConns int    `mapstructure:"max_open_conns"`
}

// LogConfig 日志配置
type LogConfig struct {
	Level  string        `mapstructure:"level"`
	Stdout bool          `mapstructure:"stdout"`
	File   LogFileConfig `mapstructure:"file"`
}

// LogFileConfig 文件日志配置
type LogFileConfig struct {
	Enabled    bool   `mapstructure:"enabled"`
	Path       string `mapstructure:"path"`
	Name       string `mapstructure:"name"`
	MaxSizeMB  int    `mapstructure:"max_size_mb"`
	MaxBackups int    `mapstructure:"max_backups"`
	MaxAgeDays int    `mapstructure:"max_age_days"`
	Compress   bool   `mapstructure:"compress"`
}

// JWTConfig JWT配置
type JWTConfig struct {
	Secret     string `mapstructure:"secret"`
	ExpireTime int    `mapstructure:"expire_time"`
}

// VisionConfig handles vision secret configuration.
type VisionConfig struct {
	SecretKey              string `mapstructure:"secret_key"`
	RetentionDays          int    `mapstructure:"retention_days"`
	CleanupIntervalMinutes int    `mapstructure:"cleanup_interval_minutes"`
}

// MemoryConfig 长记忆配置
type MemoryConfig struct {
	LongTerm LongTermMemoryConfig `mapstructure:"long_term"`
}

// LongTermMemoryConfig 长期记忆配置
type LongTermMemoryConfig struct {
	Enabled   bool                    `mapstructure:"enabled"`
	Provider  string                  `mapstructure:"provider"`
	Providers LongTermMemoryProviders `mapstructure:"providers"`
}

// LongTermMemoryProviders 长期记忆提供者配置
type LongTermMemoryProviders struct {
	Memobase MemobaseConfig `mapstructure:"memobase"`
	Memu     MemuConfig     `mapstructure:"memu"`
}

// MemobaseConfig Memobase配置
type MemobaseConfig struct {
	ProjectURL string `mapstructure:"project_url"`
	APIKey     string `mapstructure:"api_key"`
	Timeout    int    `mapstructure:"timeout"`
	RetryCount int    `mapstructure:"retry_count"`
}

// MemuConfig MemU配置
type MemuConfig struct {
	BaseURL        string `mapstructure:"base_url"`
	APIKey         string `mapstructure:"api_key"`
	AgentID        string `mapstructure:"agent_id"`
	AgentName      string `mapstructure:"agent_name"`
	UserNamePrefix string `mapstructure:"user_name_prefix"`
	TimeoutMs      int    `mapstructure:"timeout_ms"`
}

// KnowledgeBaseConfig contains configuration for knowledge base capabilities.
type KnowledgeBaseConfig struct {
	ES8         ES8Config         `mapstructure:"es8"`
	Storage     StorageConfig     `mapstructure:"storage"`
	Ingestion   IngestionConfig   `mapstructure:"ingestion"`
	VectorStore VectorStoreConfig `mapstructure:"vector_store"`
}

// ES8Config captures Elasticsearch 8 pipeline settings.
type ES8Config struct {
	Enabled        bool                `mapstructure:"enabled"`
	Index          string              `mapstructure:"index"`
	ScoreThreshold float64             `mapstructure:"score_threshold"`
	DefaultTopK    int                 `mapstructure:"default_top_k"`
	Elasticsearch  ElasticsearchConfig `mapstructure:"elasticsearch"`
	Models         ModelConfig         `mapstructure:"models"`
}

// ElasticsearchConfig represents Elasticsearch connectivity options.
type ElasticsearchConfig struct {
	Addresses  []string `mapstructure:"addresses"`
	Username   string   `mapstructure:"username"`
	Password   string   `mapstructure:"password"`
	APIKey     string   `mapstructure:"api_key"`
	CACertPath string   `mapstructure:"ca_cert_path"`
}

// ModelConfig holds credentials for embedding/chat models.
type ModelConfig struct {
	APIKey         string `mapstructure:"api_key"`
	BaseURL        string `mapstructure:"base_url"`
	EmbeddingModel string `mapstructure:"embedding_model"`
	ChatModel      string `mapstructure:"chat_model"`
}

// VectorStoreConfig controls which retrieval backend is used for chunk embeddings.
type VectorStoreConfig struct {
	Provider string         `mapstructure:"provider"`
	PGVector PGVectorConfig `mapstructure:"pgvector"`
}

// PGVectorConfig captures options for the pgvector-backed vector store.
type PGVectorConfig struct {
	DistanceMetric string `mapstructure:"distance_metric"`
	BatchSize      int    `mapstructure:"batch_size"`
}

// StorageConfig defines how knowledge base artifacts should be persisted.
type StorageConfig struct {
	Provider       string   `mapstructure:"provider"`
	LocalPath      string   `mapstructure:"local_path"`
	BaseURL        string   `mapstructure:"base_url"`
	MaxFileSizeMB  int      `mapstructure:"max_file_size_mb"`
	AllowedFormats []string `mapstructure:"allowed_formats"`
}

// WorkflowRuntimeConfig captures downstream executor endpoints.
type WorkflowRuntimeConfig struct {
	BaseURL        string `mapstructure:"base_url"`
	APIToken       string `mapstructure:"api_token"`
	TimeoutSeconds int    `mapstructure:"timeout_seconds"`
}

// IngestionConfig controls default document ingestion behaviours.
type IngestionConfig struct {
	Enabled             bool                       `mapstructure:"enabled"`
	DefaultChunkSize    int                        `mapstructure:"default_chunk_size"`
	DefaultChunkOverlap int                        `mapstructure:"default_chunk_overlap"`
	SupportedSourceType []string                   `mapstructure:"supported_source_type"`
	SessionTTLMinutes   int                        `mapstructure:"session_ttl_minutes"`
	Connectors          map[string]ConnectorConfig `mapstructure:"connectors"`
	Crawler             IngestionCrawlerConfig     `mapstructure:"crawler"`
	Retry               IngestionRetryConfig       `mapstructure:"retry"`
	Quota               IngestionQuotaConfig       `mapstructure:"quota"`
	Webhook             IngestionWebhookConfig     `mapstructure:"webhook"`
}

// ConnectorConfig represents a toggle for a specific ingestion connector.
type ConnectorConfig struct {
	Enabled          bool                   `mapstructure:"enabled"`
	DisplayName      string                 `mapstructure:"display_name"`
	Description      string                 `mapstructure:"description"`
	Categories       []string               `mapstructure:"categories"`
	Icon             string                 `mapstructure:"icon"`
	SupportsOAuth    bool                   `mapstructure:"supports_oauth"`
	OAuthProvider    string                 `mapstructure:"oauth_provider"`
	SupportsWebhook  bool                   `mapstructure:"supports_webhook"`
	DefaultParams    map[string]any         `mapstructure:"default_params"`
	DefaultMetadata  map[string]any         `mapstructure:"default_metadata"`
	ParamsSchema     []ConnectorFieldConfig `mapstructure:"params_schema"`
	CredentialSchema []ConnectorFieldConfig `mapstructure:"credential_schema"`
	MetadataSchema   []ConnectorFieldConfig `mapstructure:"metadata_schema"`
}

// ConnectorFieldConfig describes a dynamic field rendered on the frontend.
type ConnectorFieldConfig struct {
	Key         string                 `mapstructure:"key"`
	Label       string                 `mapstructure:"label"`
	Type        string                 `mapstructure:"type"`
	Required    bool                   `mapstructure:"required"`
	Placeholder string                 `mapstructure:"placeholder"`
	Help        string                 `mapstructure:"help"`
	Default     any                    `mapstructure:"default"`
	Options     []ConnectorFieldOption `mapstructure:"options"`
	Advanced    bool                   `mapstructure:"advanced"`
}

// ConnectorFieldOption represents an option in a select-like field.
type ConnectorFieldOption struct {
	Label string `mapstructure:"label"`
	Value string `mapstructure:"value"`
}

// IngestionCrawlerConfig configures embedded vs HTTP crawler integration.
type IngestionCrawlerConfig struct {
	Mode                  string `mapstructure:"mode"`
	BaseURL               string `mapstructure:"base_url"`
	UploadURL             string `mapstructure:"upload_url"`
	APIKey                string `mapstructure:"api_key"`
	RequestTimeoutSeconds int    `mapstructure:"request_timeout_seconds"`
}

// IngestionRetryConfig controls automatic retry thresholds.
type IngestionRetryConfig struct {
	MaxAttempts    int   `mapstructure:"max_attempts"`
	BackoffSeconds []int `mapstructure:"backoff_seconds"`
}

// IngestionQuotaConfig defines guardrails for ingestion throughput.
type IngestionQuotaConfig struct {
	MaxDocumentsPerDay int   `mapstructure:"max_documents_per_day"`
	MaxBytesPerDay     int64 `mapstructure:"max_bytes_per_day"`
	MaxConcurrentJobs  int   `mapstructure:"max_concurrent_jobs"`
}

// IngestionWebhookConfig configures callback validation metadata.
type IngestionWebhookConfig struct {
	Enabled       bool     `mapstructure:"enabled"`
	SigningSecret string   `mapstructure:"signing_secret"`
	AllowedCIDRs  []string `mapstructure:"allowed_cidrs"`
}

// AudioConfig captures podcast subsystem configuration.
type AudioConfig struct {
	Storage                AudioStorageConfig     `mapstructure:"storage"`
	Transcoding            AudioTranscodingConfig `mapstructure:"transcoding"`
	Token                  AudioTokenConfig       `mapstructure:"token"`
	DownloadTimeoutSeconds int                    `mapstructure:"download_timeout_seconds"`
}

// AudioStorageConfig describes persistence options for audio assets.
type AudioStorageConfig struct {
	Provider       string   `mapstructure:"provider"`
	LocalPath      string   `mapstructure:"local_path"`
	MaxFileSizeMB  int      `mapstructure:"max_file_size_mb"`
	AllowedFormats []string `mapstructure:"allowed_formats"`
	QuotaMB        int      `mapstructure:"quota_mb"`
}

// AudioTranscodingConfig controls ffmpeg driven conversion.
type AudioTranscodingConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	Format  string `mapstructure:"format"`
	Bitrate int    `mapstructure:"bitrate"`
}

// AudioTokenConfig configures playback token issuance.
type AudioTokenConfig struct {
	Secret     string `mapstructure:"secret"`
	TTLSeconds int    `mapstructure:"ttl_seconds"`
}

// MediaConfig captures configuration for the media library subsystem.
type MediaConfig struct {
	Storage MediaStorageConfig `mapstructure:"storage"`
}

// MediaStorageConfig describes persistence options for image, video, and audio assets.
type MediaStorageConfig struct {
	Provider       string   `mapstructure:"provider"`
	LocalPath      string   `mapstructure:"local_path"`
	BaseURL        string   `mapstructure:"base_url"`
	MaxFileSizeMB  int      `mapstructure:"max_file_size_mb"`
	AllowedFormats []string `mapstructure:"allowed_formats"`
}

// LoadConfig 加载配置文件
func LoadConfig(configPath string) (*Config, error) {
	viper.SetConfigFile(configPath)
	viper.SetConfigType("yaml")

	// 设置环境变量前缀
	viper.SetEnvPrefix("XIAOZHI")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()

	if _, err := os.Stat(configPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			data := configdefaults.DefaultConfig()
			if len(data) == 0 {
				return nil, fmt.Errorf("config file not found: %s", configPath)
			}
			log.Printf("配置文件 %s 未找到，使用内置默认配置", configPath)
			if err := viper.ReadConfig(bytes.NewReader(data)); err != nil {
				return nil, fmt.Errorf("failed to read embedded config: %w", err)
			}
		} else {
			return nil, fmt.Errorf("stat config file failed: %w", err)
		}
	} else {
		if err := viper.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("failed to read config file: %w", err)
		}
	}

	var config Config
	if err := viper.Unmarshal(&config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	if strings.TrimSpace(config.Log.Level) == "" {
		config.Log.Level = "info"
	}
	if !config.Log.Stdout && !config.Log.File.Enabled {
		config.Log.Stdout = true
	}

	if !config.Log.File.Enabled {
		config.Log.File.Enabled = false
	}

	logDir := strings.TrimSpace(config.Log.File.Path)
	if logDir == "" {
		logDir = "./logs"
	}
	if !filepath.IsAbs(logDir) {
		logDir = filepath.Clean(logDir)
	}
	if absDir, err := filepath.Abs(logDir); err == nil {
		logDir = absDir
	}
	config.Log.File.Path = logDir
	if strings.TrimSpace(config.Log.File.Name) == "" {
		config.Log.File.Name = "manager-server.log"
	}
	if config.Log.File.MaxSizeMB <= 0 {
		config.Log.File.MaxSizeMB = 100
	}
	if config.Log.File.MaxBackups < 0 {
		config.Log.File.MaxBackups = 0
	}
	if config.Log.File.MaxAgeDays < 0 {
		config.Log.File.MaxAgeDays = 0
	}

	if strings.TrimSpace(config.KnowledgeBase.ES8.Index) == "" {
		config.KnowledgeBase.ES8.Index = "kb_chunks"
	}
	if config.KnowledgeBase.ES8.ScoreThreshold <= 0 {
		config.KnowledgeBase.ES8.ScoreThreshold = 1.1
	}
	if config.KnowledgeBase.ES8.DefaultTopK <= 0 {
		config.KnowledgeBase.ES8.DefaultTopK = 5
	}

	if strings.TrimSpace(config.KnowledgeBase.Storage.Provider) == "" {
		config.KnowledgeBase.Storage.Provider = "local"
	}
	if strings.TrimSpace(config.KnowledgeBase.Storage.LocalPath) == "" {
		config.KnowledgeBase.Storage.LocalPath = "./storage/kb"
	}
	if config.KnowledgeBase.Storage.MaxFileSizeMB <= 0 {
		config.KnowledgeBase.Storage.MaxFileSizeMB = 50
	}
	if len(config.KnowledgeBase.Storage.AllowedFormats) == 0 {
		config.KnowledgeBase.Storage.AllowedFormats = []string{
			".txt", ".md", ".pdf", ".html", ".htm", ".csv", ".xlsx",
		}
	}

	if config.KnowledgeBase.Ingestion.DefaultChunkSize <= 0 {
		config.KnowledgeBase.Ingestion.DefaultChunkSize = 800
	}
	if config.KnowledgeBase.Ingestion.DefaultChunkOverlap <= 0 {
		config.KnowledgeBase.Ingestion.DefaultChunkOverlap = 120
	}
	if len(config.KnowledgeBase.Ingestion.SupportedSourceType) == 0 {
		config.KnowledgeBase.Ingestion.SupportedSourceType = []string{"manual", "file", "url"}
	}

	vectorProvider := strings.TrimSpace(strings.ToLower(config.KnowledgeBase.VectorStore.Provider))
	if vectorProvider == "" {
		if config.KnowledgeBase.ES8.Enabled {
			vectorProvider = "elasticsearch"
		} else {
			vectorProvider = "pgvector"
		}
	}
	config.KnowledgeBase.VectorStore.Provider = vectorProvider

	if config.KnowledgeBase.VectorStore.PGVector.BatchSize <= 0 {
		config.KnowledgeBase.VectorStore.PGVector.BatchSize = 16
	}
	if strings.TrimSpace(config.KnowledgeBase.VectorStore.PGVector.DistanceMetric) == "" {
		config.KnowledgeBase.VectorStore.PGVector.DistanceMetric = "cosine"
	}

	if strings.TrimSpace(config.Audio.Storage.Provider) == "" {
		config.Audio.Storage.Provider = "local"
	}
	if strings.TrimSpace(config.Audio.Storage.LocalPath) == "" {
		config.Audio.Storage.LocalPath = "./storage/audio"
	}
	if config.Audio.Storage.MaxFileSizeMB <= 0 {
		config.Audio.Storage.MaxFileSizeMB = 500
	}
	if len(config.Audio.Storage.AllowedFormats) == 0 {
		config.Audio.Storage.AllowedFormats = []string{".mp3", ".m4a", ".aac", ".wav", ".flac"}
	}
	if config.Audio.Storage.QuotaMB <= 0 {
		config.Audio.Storage.QuotaMB = 20480
	}
	if strings.TrimSpace(config.Audio.Transcoding.Format) == "" {
		config.Audio.Transcoding.Format = "mp3"
	}
	if config.Audio.Transcoding.Bitrate <= 0 {
		config.Audio.Transcoding.Bitrate = 192000
	}
	if config.Audio.Token.TTLSeconds <= 0 {
		config.Audio.Token.TTLSeconds = 900
	}
	if strings.TrimSpace(config.Audio.Token.Secret) == "" {
		config.Audio.Token.Secret = "change-me"
	}
	if config.Audio.DownloadTimeoutSeconds <= 0 {
		config.Audio.DownloadTimeoutSeconds = 120
	}

	return &config, nil
}

// resolveLogBaseDir removed; relative log paths are now resolved from the process working directory.

// GetDSN 生成数据库连接字符串
func (db *DatabaseConfig) GetDSN() string {
	return db.DSN
}
