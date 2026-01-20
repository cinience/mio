package runtime

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"backend-server/internal/app"
	"backend-server/internal/config"
	redisdb "backend-server/internal/infrastructure/db/redis"
	log "backend-server/internal/infrastructure/logger"
	"backend-server/internal/shared/cleanup"

	"gopkg.in/natefinch/lumberjack.v2"
)

// Server wraps the backend application runtime.
type Server struct {
	app  *app.App
	once sync.Once
}

// New bootstraps the backend services using the provided configuration file.
func New(version, configPath string) (*Server, error) {
	log.Version = version

	if err := config.InitGlobalConfig(configPath); err != nil {
		return nil, fmt.Errorf("load backend config: %w", err)
	}

	if err := initLogger(); err != nil {
		return nil, fmt.Errorf("init backend logger: %w", err)
	}

	if err := initRedis(); err != nil {
		return nil, fmt.Errorf("init backend redis: %w", err)
	}

	application := app.NewApp()
	if application == nil {
		return nil, fmt.Errorf("backend app initialization returned nil")
	}

	return &Server{app: application}, nil
}

// Start launches the backend application loops.
func (s *Server) Start() {
	if s == nil || s.app == nil {
		return
	}
	s.once.Do(func() {
		go s.app.Run()
	})
}

// Shutdown gracefully stops the backend application.
func (s *Server) Shutdown(ctx context.Context) error {
	if s == nil || s.app == nil {
		return nil
	}
	return s.app.Shutdown(ctx)
}

// Cleanup releases global backend resources.
func Cleanup() error {
	return cleanup.Cleanup()
}

func initLogger() error {
	cfg := config.GetConfig()
	logCfg := cfg.Log

	level, err := log.ParseLevel(logCfg.Level)
	if err != nil {
		log.Warnf("无法解析日志等级 %q，使用默认 info: %v", logCfg.Level, err)
		level = log.LevelInfo
	}
	log.SetLevel(level)

	outputs := make([]log.Output, 0, 2)
	if logCfg.Stdout {
		outputs = append(outputs, log.Output{Writer: os.Stdout, Console: true})
	}

	if logCfg.File.Enabled {
		fileWriter, err := buildFileWriter(logCfg.File)
		if err != nil {
			return err
		}
		if fileWriter != nil {
			outputs = append(outputs, log.Output{Writer: fileWriter, Console: false})
		}
	}

	if len(outputs) == 0 {
		outputs = append(outputs, log.Output{Writer: os.Stdout, Console: true})
	}

	log.SetOutputs(outputs...)
	return nil
}

func buildFileWriter(fileCfg config.LogFileConfig) (io.Writer, error) {
	fileName := strings.TrimSpace(fileCfg.Name)
	logPath := strings.TrimSpace(fileCfg.Path)

	if fileName == "" {
		return nil, nil
	}
	if logPath == "" {
		logPath = "./logs/"
	}

	if err := os.MkdirAll(logPath, 0o755); err != nil {
		return nil, fmt.Errorf("创建日志目录失败: %w", err)
	}

	fullPath := filepath.Join(logPath, fileName)

	roller := &lumberjack.Logger{
		Filename:  fullPath,
		LocalTime: true,
	}

	if fileCfg.MaxSizeMB > 0 {
		roller.MaxSize = fileCfg.MaxSizeMB
	}
	if fileCfg.MaxAgeDays > 0 {
		roller.MaxAge = fileCfg.MaxAgeDays
	}
	if fileCfg.MaxBackups >= 0 {
		roller.MaxBackups = fileCfg.MaxBackups
	}
	roller.Compress = fileCfg.Compress

	rotationInterval := time.Duration(fileCfg.RotationHours) * time.Hour
	return log.WrapWithInterval(roller, rotationInterval), nil
}

func initRedis() error {
	cfg := config.GetConfig()
	redisCfg := cfg.Redis

	redisConfig := &redisdb.Config{
		Host:     redisCfg.Host,
		Port:     redisCfg.Port,
		Password: redisCfg.Password,
		DB:       redisCfg.DB,
	}

	if err := redisdb.Init(redisConfig); err != nil {
		return fmt.Errorf("init redis error: %w", err)
	}

	log.Infof("Redis初始化成功: %s:%d", redisCfg.Host, redisCfg.Port)
	return nil
}
