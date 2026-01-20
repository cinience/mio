package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"backend-server/internal/config"
	log "backend-server/internal/infrastructure/logger"
	mqtt_server "backend-server/internal/server/mqtt_server"

	"gopkg.in/natefinch/lumberjack.v2"
)

// 初始化函数
func Init(configFile string) error {
	err := initConfig(configFile)
	if err != nil {
		return err
	}

	err = initLog()
	if err != nil {
		return err
	}

	return nil
}

func initLog() error {
	cfg := config.GetConfig()
	logCfg := cfg.Log

	outputs := make([]log.Output, 0, 2)
	if logCfg.Stdout {
		outputs = append(outputs, log.Output{Writer: os.Stdout, Console: true})
	}

	if logCfg.File.Enabled {
		binPath, _ := os.Executable()
		baseDir := filepath.Dir(binPath)
		writer, err := buildMQTTFileWriter(baseDir, logCfg.File)
		if err != nil {
			fmt.Printf("init log error: %v\n", err)
			os.Exit(1)
			return err
		}
		if writer != nil {
			outputs = append(outputs, log.Output{Writer: writer, Console: false})
		}
	}

	if len(outputs) == 0 {
		outputs = append(outputs, log.Output{Writer: os.Stdout, Console: true})
	}

	log.SetOutputs(outputs...)
	if lvl, err := log.ParseLevel(cfg.Log.Level); err != nil {
		log.Warnf("invalid log level '%s', fallback to INFO", cfg.Log.Level)
		log.SetLevel(log.LevelInfo)
	} else {
		log.SetLevel(lvl)
	}

	return nil

}

func buildMQTTFileWriter(baseDir string, fileCfg config.LogFileConfig) (io.Writer, error) {
	fileName := strings.TrimSpace(fileCfg.Name)
	if fileName == "" {
		return nil, nil
	}

	logPath := strings.TrimSpace(fileCfg.Path)
	if logPath == "" {
		logPath = "./logs"
	}
	if !filepath.IsAbs(logPath) {
		logPath = filepath.Join(baseDir, logPath)
	}

	if err := os.MkdirAll(logPath, 0o755); err != nil {
		return nil, fmt.Errorf("create log dir failed: %w", err)
	}

	fullPath := filepath.Join(logPath, fileName)
	logger := &lumberjack.Logger{
		Filename:  fullPath,
		LocalTime: true,
	}

	if fileCfg.MaxSizeMB > 0 {
		logger.MaxSize = fileCfg.MaxSizeMB
	}
	if fileCfg.MaxAgeDays > 0 {
		logger.MaxAge = fileCfg.MaxAgeDays
	}
	if fileCfg.MaxBackups >= 0 {
		logger.MaxBackups = fileCfg.MaxBackups
	}
	logger.Compress = fileCfg.Compress

	rotationInterval := time.Duration(fileCfg.RotationHours) * time.Hour
	return log.WrapWithInterval(logger, rotationInterval), nil
}

func initConfig(configFile string) error {
	// 使用新的配置系统
	return config.InitGlobalConfig(configFile)
}

func main() {
	// 解析命令行参数
	configFile := flag.String("c", "config/mqtt_config.json", "配置文件路径")
	flag.Parse()

	if *configFile == "" {
		fmt.Println("配置文件路径不能为空")
		return
	}

	// 初始化配置和日志
	err := Init(*configFile)
	if err != nil {
		fmt.Printf("初始化失败: %v\n", err)
		return
	}

	// 启动MQTT服务器
	err = mqtt_server.StartMqttServer()
	if err != nil {
		log.Errorf("启动MQTT服务器失败: %v", err)
		return
	}

	fmt.Println("MQTT服务器已启动")

	// 阻塞监听退出信号
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	log.Info("MQTT服务器已启动，按 Ctrl+C 退出")
	<-quit

	log.Info("正在关闭MQTT服务器...")
	log.Info("MQTT服务器已关闭")
}
