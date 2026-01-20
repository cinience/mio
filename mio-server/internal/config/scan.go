package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type ConfigFileInfo struct {
	Filename string `json:"filename"`
	Name     string `json:"name"`
}

type BackgroundFile struct {
	Name string `json:"name"`
}

type modelInfoEntry struct {
	Name string `json:"name"`
}

type configFilePayload struct {
	CharacterConfig CharacterConfig `yaml:"character_config"`
}

func ScanConfigFiles(rootDir string, configAltsDir string) ([]ConfigFileInfo, error) {
	configs := []ConfigFileInfo{}
	defaultConf, err := ReadCharacterConfig(filepath.Join(rootDir, "conf.yaml"))
	if err == nil {
		configs = append(configs, ConfigFileInfo{Filename: "conf.yaml", Name: defaultConf.ConfName})
	} else {
		configs = append(configs, ConfigFileInfo{Filename: "conf.yaml", Name: "conf.yaml"})
	}

	if configAltsDir == "" {
		return configs, nil
	}

	_ = filepath.WalkDir(configAltsDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil || d == nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".yaml") {
			return nil
		}
		conf, err := ReadCharacterConfig(path)
		name := d.Name()
		if err == nil && conf.ConfName != "" {
			name = conf.ConfName
		}
		configs = append(configs, ConfigFileInfo{Filename: d.Name(), Name: name})
		return nil
	})

	return configs, nil
}

func ScanBackgrounds(backgroundDir string) []string {
	files := []string{}
	_ = filepath.WalkDir(backgroundDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d == nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(d.Name()))
		switch ext {
		case ".jpg", ".jpeg", ".png", ".gif":
			files = append(files, d.Name())
		}
		return nil
	})
	return files
}

func LoadModelInfo(modelName string, modelDictPath string) (map[string]any, error) {
	data, err := os.ReadFile(modelDictPath)
	if err != nil {
		return nil, err
	}
	var entries []map[string]any
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if name, ok := entry["name"].(string); ok && name == modelName {
			return entry, nil
		}
	}
	return nil, errors.New("model not found")
}

func ReadCharacterConfig(path string) (CharacterConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return CharacterConfig{}, err
	}
	var payload configFilePayload
	if err := yaml.Unmarshal(data, &payload); err != nil {
		return CharacterConfig{}, err
	}
	if payload.CharacterConfig.ConfName == "" {
		payload.CharacterConfig.ConfName = filepath.Base(path)
	}
	return payload.CharacterConfig, nil
}
