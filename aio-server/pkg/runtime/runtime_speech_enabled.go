//go:build sherpa_onnx

package runtime

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
	speechruntime "speech-server/pkg/runtime"
)

func (r *Runtime) startSpeech(ctx context.Context) error {
	r.mu.Lock()
	if r.speech != nil {
		r.mu.Unlock()
		return nil
	}
	configPath := strings.TrimSpace(r.cfg.SpeechConfig)
	r.mu.Unlock()

	if configPath != "" {
		if _, err := os.Stat(configPath); err != nil {
			if os.IsNotExist(err) {
				log.Printf("[aio-runtime] speech config %s not found; using embedded defaults", configPath)
				configPath = ""
			} else {
				return fmt.Errorf("stat speech config %s: %w", configPath, err)
			}
		}
	}

	if err := prepareSpeechLogging(configPath); err != nil {
		return err
	}

	var svc speechService
	if err := protectServiceCall("speech.New", func() error {
		instance, err := speechruntime.New(configPath)
		if err != nil {
			return err
		}
		svc = instance
		return nil
	}); err != nil {
		return err
	}

	if err := protectServiceCall("speech.Start", svc.Start); err != nil {
		return err
	}

	r.mu.Lock()
	r.speech = svc
	r.mu.Unlock()

	return nil
}

func (r *Runtime) stopSpeech(ctx context.Context) error {
	r.mu.Lock()
	instance := r.speech
	r.speech = nil
	r.mu.Unlock()

	if instance == nil {
		return nil
	}

	shutdownCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	return protectServiceCall("speech.Shutdown", func() error {
		return instance.Shutdown(shutdownCtx)
	})
}

const speechLogFilePathEnv = "SPEECH_LOG_FILE_PATH"

func prepareSpeechLogging(configPath string) error {
	if _, ok := os.LookupEnv(speechLogFilePathEnv); ok {
		return nil
	}
	if !shouldOverrideSpeechLogPath(configPath) {
		return nil
	}
	dir, err := resolvedLogsDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create speech log directory %s: %w", dir, err)
	}
	if err := os.Setenv(speechLogFilePathEnv, dir); err != nil {
		return fmt.Errorf("set %s: %w", speechLogFilePathEnv, err)
	}
	return nil
}

func resolvedLogsDir() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("resolve working directory: %w", err)
	}
	return filepath.Join(wd, "logs"), nil
}

func shouldOverrideSpeechLogPath(configPath string) bool {
	path := strings.TrimSpace(configPath)
	if path == "" {
		return true
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return true
	}
	info, err := os.Stat(absPath)
	if err != nil || info.IsDir() {
		return true
	}
	logPath, err := readSpeechLogPath(absPath)
	if err != nil {
		return true
	}
	logPath = strings.TrimSpace(logPath)
	if logPath == "" {
		return true
	}
	if filepath.IsAbs(logPath) {
		return false
	}
	normalized := filepath.Clean(logPath)
	return normalized == "logs"
}

func readSpeechLogPath(configPath string) (string, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return "", err
	}
	var cfg struct {
		Log struct {
			File struct {
				Path string `yaml:"path"`
			} `yaml:"file"`
		} `yaml:"log"`
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return "", err
	}
	return cfg.Log.File.Path, nil
}
