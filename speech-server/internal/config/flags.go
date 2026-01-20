package config

import (
	"flag"
	"fmt"
	"log"
	"path/filepath"
	"strings"
)

// ParseFlags constructs a Config by parsing CLI flags, configuration files, and
// environment overrides.
func ParseFlags() (Config, error) {
	var (
		configPathFlag string
		addrOverride   string
	)

	flag.StringVar(&configPathFlag, "config", "", "path to configuration file")
	flag.StringVar(&addrOverride, "addr", "", "HTTP listen address override")
	flag.Parse()

	return Load(configPathFlag, addrOverride)
}

// Load constructs a Config from the provided path and optional address override.
func Load(configPath, addrOverride string) (Config, error) {
	cfg := NewDefault()

	defaultConfigPath := filepath.Join("config", "config.yaml")
	path := strings.TrimSpace(configPath)
	if path == "" {
		path = defaultConfigPath
	}

	if fileExists(path) {
		cfg.ConfigPath = path
	} else if configPath != "" {
		return Config{}, fmt.Errorf("config file %q not found", path)
	} else {
		if err := writeDefaultConfig(path); err != nil {
			log.Printf("failed to write default config %s: %v", path, err)
		} else {
			cfg.ConfigPath = path
		}
	}

	fc, baseDir, err := loadConfigFile(cfg.ConfigPath)
	if err != nil {
		return Config{}, err
	}
	if err := mergeFileConfig(&cfg, &fc, baseDir); err != nil {
		return Config{}, err
	}

	applyConfigOverrides(&cfg, addrOverride)
	applyEnvOverrides(&cfg)

	if strings.TrimSpace(cfg.Addr) == "" {
		cfg.Addr = ":8009"
	}

	if err := ensurePrimaryProviders(&cfg); err != nil {
		return Config{}, err
	}

	if err := validateConfig(&cfg); err != nil {
		return Config{}, err
	}

	return cfg, nil
}
