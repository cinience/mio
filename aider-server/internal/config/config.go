package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"aider-server/config"

	"gopkg.in/yaml.v3"
)

type Config struct {
	ListenAddr          string                   `yaml:"listen_addr"`
	DataDir             string                   `yaml:"data_dir"`
	LogRetentionSeconds int                      `yaml:"log_retention_seconds"`
	AllowedBinaries     []string                 `yaml:"allowed_binaries"`
	DefaultEnv          map[string]string        `yaml:"default_env"`
	Auth                AuthConfig               `yaml:"auth"`
	Redis               RedisConfig              `yaml:"redis"`
	Adapters            map[string]AdapterConfig `yaml:"adapters"`
	Codex               CodexConfig              `yaml:"codex"`
	ExecutionMode       string                   `yaml:"execution_mode"`
	Runner              RunnerConfig             `yaml:"runner"`
}

type AuthConfig struct {
	Token string `yaml:"token"`
}

type RedisConfig struct {
	Addr      string        `yaml:"addr"`
	DB        int           `yaml:"db"`
	Password  string        `yaml:"password"`
	KeyPrefix string        `yaml:"key_prefix"`
	Embedded  RedisEmbedded `yaml:"embedded"`
}

type RedisEmbedded struct {
	Enabled           bool   `yaml:"enabled"`
	Addr              string `yaml:"addr"`
	DataDir           string `yaml:"data_dir"`
	LogDir            string `yaml:"log_dir"`
	MaxClients        int    `yaml:"max_clients"`
	Password          string `yaml:"password"`
	ConfigPath        string `yaml:"config_path"`
	AOFEnabled        *bool  `yaml:"aof_enabled"`
	AOFFilename       string `yaml:"aof_filename"`
	AOFFsync          string `yaml:"aof_fsync"`
	AOFUseRdbPreamble *bool  `yaml:"aof_use_rdb_preamble"`
}

type AdapterConfig struct {
	Command string   `yaml:"command"`
	Args    []string `yaml:"args"`
}

type CodexConfig struct {
	DefaultProtocol string               `yaml:"default_protocol"`
	AppServer       CodexAppServerConfig `yaml:"app_server"`
}

type CodexAppServerConfig struct {
	Command string            `yaml:"command"`
	Args    []string          `yaml:"args"`
	Env     map[string]string `yaml:"env"`
}

type RunnerConfig struct {
	Enabled              bool     `yaml:"enabled"`
	ServerAddr           string   `yaml:"server_addr"`
	RunnerID             string   `yaml:"runner_id"`
	LogDir               string   `yaml:"log_dir"`
	AuthToken            string   `yaml:"auth_token"`
	DialTimeoutSeconds   int      `yaml:"dial_timeout_seconds"`
	HeartbeatSeconds     int      `yaml:"heartbeat_seconds"`
	LogBatchSize         int      `yaml:"log_batch_size"`
	LogBackfillBatchSize int      `yaml:"log_backfill_batch_size"`
	LogMaxBytes          int64    `yaml:"log_max_bytes"`
	LogMaxBackups        int      `yaml:"log_max_backups"`
	ExitOnComplete       bool     `yaml:"exit_on_complete"`
	LaunchCommand        string   `yaml:"launch_command"`
	LaunchArgs           []string `yaml:"launch_args"`
	LaunchTimeoutSeconds int      `yaml:"launch_timeout_seconds"`
	LaunchDetach         bool     `yaml:"launch_detach"`
}

func Load(path string) (*Config, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return loadFromBytes(config.Default, "embedded config")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %q: %w", path, err)
	}
	return loadFromBytes(data, path)
}

func applyDefaults(cfg *Config) {
	if strings.TrimSpace(cfg.ListenAddr) == "" {
		cfg.ListenAddr = "0.0.0.0:8099"
	}
	if strings.TrimSpace(cfg.DataDir) == "" {
		cfg.DataDir = "./data"
	}
	cfg.DataDir = filepath.Clean(cfg.DataDir)
	if cfg.LogRetentionSeconds <= 0 {
		cfg.LogRetentionSeconds = 24 * 60 * 60
	}
	if strings.TrimSpace(cfg.ExecutionMode) == "" {
		cfg.ExecutionMode = "local"
	}
	if cfg.DefaultEnv == nil {
		cfg.DefaultEnv = map[string]string{}
	}
	if cfg.Redis.KeyPrefix == "" {
		cfg.Redis.KeyPrefix = "aider"
	}
	if cfg.Redis.Embedded.Addr == "" {
		cfg.Redis.Embedded.Addr = "127.0.0.1:26371"
	}
	if cfg.Redis.Embedded.DataDir == "" {
		cfg.Redis.Embedded.DataDir = filepath.Join(cfg.DataDir, "redis")
	}
	if cfg.Redis.Embedded.LogDir == "" {
		cfg.Redis.Embedded.LogDir = filepath.Join("logs", "redis")
	}
	if cfg.Redis.Embedded.MaxClients == 0 {
		cfg.Redis.Embedded.MaxClients = 1000
	}
	if cfg.Redis.Embedded.AOFEnabled == nil {
		enabled := true
		cfg.Redis.Embedded.AOFEnabled = &enabled
	}
	if cfg.Redis.Embedded.AOFFilename == "" {
		cfg.Redis.Embedded.AOFFilename = "appendonly.aof"
	}
	if cfg.Redis.Embedded.AOFFsync == "" {
		cfg.Redis.Embedded.AOFFsync = "everysec"
	}
	if cfg.Redis.Embedded.AOFUseRdbPreamble == nil {
		enabled := true
		cfg.Redis.Embedded.AOFUseRdbPreamble = &enabled
	}
	if cfg.Runner.LogDir == "" {
		cfg.Runner.LogDir = filepath.Join(cfg.DataDir, "runner-logs")
	}
	if cfg.Runner.ServerAddr == "" {
		cfg.Runner.ServerAddr = cfg.ListenAddr
	}
	if cfg.Runner.DialTimeoutSeconds == 0 {
		cfg.Runner.DialTimeoutSeconds = 5
	}
	if cfg.Runner.HeartbeatSeconds == 0 {
		cfg.Runner.HeartbeatSeconds = 10
	}
	if cfg.Runner.LogBatchSize == 0 {
		cfg.Runner.LogBatchSize = 50
	}
	if cfg.Runner.LogBackfillBatchSize == 0 {
		cfg.Runner.LogBackfillBatchSize = 200
	}
	if cfg.Runner.LogMaxBytes == 0 {
		cfg.Runner.LogMaxBytes = 10 * 1024 * 1024
	}
	if cfg.Runner.LogMaxBackups == 0 {
		cfg.Runner.LogMaxBackups = 5
	}
	if cfg.Runner.LaunchTimeoutSeconds == 0 {
		cfg.Runner.LaunchTimeoutSeconds = 10
	}
	if !cfg.Runner.LaunchDetach {
		cfg.Runner.LaunchDetach = true
	}
	if strings.TrimSpace(cfg.Codex.DefaultProtocol) == "" {
		cfg.Codex.DefaultProtocol = "pty"
	}
	if strings.TrimSpace(cfg.Codex.AppServer.Command) == "" {
		cfg.Codex.AppServer.Command = "codex"
	}
	if len(cfg.Codex.AppServer.Args) == 0 {
		cfg.Codex.AppServer.Args = []string{"app-server"}
	}
	if cfg.Codex.AppServer.Env == nil {
		cfg.Codex.AppServer.Env = map[string]string{}
	}
}

func loadFromBytes(data []byte, source string) (*Config, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, fmt.Errorf("config %s is empty", source)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", source, err)
	}
	applyDefaults(&cfg)
	return &cfg, nil
}
