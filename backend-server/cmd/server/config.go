package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"backend-server/internal/config"
	redisdb "backend-server/internal/infrastructure/db/redis"
	logger "backend-server/internal/infrastructure/logger"

	"gopkg.in/natefinch/lumberjack.v2"
)

func Init(configFile string) error {
	// 使用新的配置系统加载配置
	err := config.InitGlobalConfig(configFile)
	if err != nil {
		fmt.Printf("加载配置失败: %+v\n", err)
		os.Exit(1)
		return err
	}

	if err := initLogger(); err != nil {
		fmt.Printf("初始化日志失败: %+v\n", err)
		os.Exit(1)
		return err
	}

	logger.Info("配置加载成功")

	//init vad
	//initVad()

	//init redis
	initRedis()

	return nil
}

func initRedis() error {
	// 获取配置
	cfg := config.GetConfig()
	redisCfg := cfg.Redis

	// 初始化我们的统一Redis模块
	redisConfig := &redisdb.Config{
		Host:     redisCfg.Host,
		Port:     redisCfg.Port,
		Password: redisCfg.Password,
		DB:       redisCfg.DB,
	}

	err := redisdb.Init(redisConfig)
	if err != nil {
		fmt.Printf("init redis error: %v\n", err)
		return err
	}

	logger.Infof("Redis初始化成功: %s:%d", redisCfg.Host, redisCfg.Port)
	return nil
}

func initLogger() error {
	cfg := config.GetConfig()
	logCfg := cfg.Log

	level, err := logger.ParseLevel(logCfg.Level)
	if err != nil {
		logger.Warnf("无法解析日志等级 %q，使用默认 info: %v", logCfg.Level, err)
		level = logger.LevelInfo
	}
	logger.SetLevel(level)

	outputs := make([]logger.Output, 0, 2)
	if logCfg.Stdout {
		outputs = append(outputs, logger.Output{Writer: os.Stdout, Console: true})
	}

	if logCfg.File.Enabled {
		if fileWriter, err := buildFileWriter(logCfg.File); err != nil {
			return err
		} else if fileWriter != nil {
			outputs = append(outputs, logger.Output{Writer: fileWriter, Console: false})
		}
	}

	if len(outputs) == 0 {
		outputs = append(outputs, logger.Output{Writer: os.Stdout, Console: true})
	}

	logger.SetOutputs(outputs...)
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

	l := &lumberjack.Logger{
		Filename:  fullPath,
		LocalTime: true,
	}

	if fileCfg.MaxSizeMB > 0 {
		l.MaxSize = fileCfg.MaxSizeMB
	}
	if fileCfg.MaxAgeDays > 0 {
		l.MaxAge = fileCfg.MaxAgeDays
	}

	if fileCfg.MaxBackups >= 0 {
		l.MaxBackups = fileCfg.MaxBackups
	}

	l.Compress = fileCfg.Compress

	rotationInterval := time.Duration(fileCfg.RotationHours) * time.Hour
	return logger.WrapWithInterval(l, rotationInterval), nil
}
