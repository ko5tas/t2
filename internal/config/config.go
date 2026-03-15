package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// Config holds the application configuration.
type Config struct {
	APIKey          string        `yaml:"api_key"`
	APISecret       string        `yaml:"api_secret"`
	RefreshInterval time.Duration `yaml:"refresh_interval"`
	Listen          string        `yaml:"listen"`
	BaseURL         string        `yaml:"base_url"`
}

// rawConfig is used for YAML unmarshaling since time.Duration
// doesn't unmarshal directly from YAML strings like "15m".
type rawConfig struct {
	APIKey          string `yaml:"api_key"`
	APISecret       string `yaml:"api_secret"`
	RefreshInterval string `yaml:"refresh_interval"`
	Listen          string `yaml:"listen"`
	BaseURL         string `yaml:"base_url"`
}

// Load reads the configuration file and returns a validated Config.
// It searches in order: $T2_CONFIG, ~/.config/t2/config.yaml, /etc/t2/config.yaml.
func Load() (*Config, error) {
	path, err := findConfigFile()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config %s: %w", path, err)
	}

	var raw rawConfig
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing config %s: %w", path, err)
	}

	cfg := &Config{
		APIKey:    raw.APIKey,
		APISecret: raw.APISecret,
		Listen:    raw.Listen,
		BaseURL:   raw.BaseURL,
	}

	// Parse refresh interval with default.
	if raw.RefreshInterval != "" {
		d, err := time.ParseDuration(raw.RefreshInterval)
		if err != nil {
			return nil, fmt.Errorf("invalid refresh_interval %q: %w", raw.RefreshInterval, err)
		}
		cfg.RefreshInterval = d
	}

	applyDefaults(cfg)

	if err := validate(cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

func findConfigFile() (string, error) {
	// 1. Environment variable.
	if env := os.Getenv("T2_CONFIG"); env != "" {
		if _, err := os.Stat(env); err == nil {
			return env, nil
		}
	}

	// 2. User config directory.
	home, err := os.UserHomeDir()
	if err == nil {
		p := filepath.Join(home, ".config", "t2", "config.yaml")
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}

	// 3. System config directory.
	p := "/etc/t2/config.yaml"
	if _, err := os.Stat(p); err == nil {
		return p, nil
	}

	return "", fmt.Errorf("no config file found (checked $T2_CONFIG, ~/.config/t2/config.yaml, /etc/t2/config.yaml)")
}

func applyDefaults(cfg *Config) {
	if cfg.RefreshInterval == 0 {
		cfg.RefreshInterval = 15 * time.Minute
	}
	if cfg.Listen == "" {
		cfg.Listen = ":8080"
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://live.trading212.com/api/v0"
	}
}

func validate(cfg *Config) error {
	if cfg.APIKey == "" {
		return fmt.Errorf("config: api_key is required")
	}
	if cfg.APISecret == "" {
		return fmt.Errorf("config: api_secret is required")
	}
	return nil
}
