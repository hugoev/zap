package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

type Config struct {
	ProtectedPorts        []int    `json:"protected_ports"`
	MaxAgeDaysForCleanup  int      `json:"max_age_days_for_cleanup"`
	ExcludePaths          []string `json:"exclude_paths"`
	AutoConfirmSafeActions bool    `json:"auto_confirm_safe_actions"`
}

var defaultConfig = Config{
	ProtectedPorts:        []int{5432, 6379, 3306, 27017}, // Postgres, Redis, MySQL, MongoDB
	MaxAgeDaysForCleanup:  14,
	ExcludePaths:          []string{},
	AutoConfirmSafeActions: false,
}

func getConfigPath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	configDir := filepath.Join(homeDir, ".config", "zap")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return "", err
	}
	return filepath.Join(configDir, "config.json"), nil
}

func Load() (*Config, error) {
	configPath, err := getConfigPath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(configPath)
	if os.IsNotExist(err) {
		cfg := defaultConfig
		if err := Save(&cfg); err != nil {
			return nil, err
		}
		return &cfg, nil
	}
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	// Merge with defaults for missing fields
	if len(cfg.ProtectedPorts) == 0 {
		cfg.ProtectedPorts = defaultConfig.ProtectedPorts
	}
	if cfg.MaxAgeDaysForCleanup == 0 {
		cfg.MaxAgeDaysForCleanup = defaultConfig.MaxAgeDaysForCleanup
	}
	if cfg.ExcludePaths == nil {
		cfg.ExcludePaths = []string{}
	}

	return &cfg, nil
}

func Save(cfg *Config) error {
	configPath, err := getConfigPath()
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(configPath, data, 0644)
}

func (c *Config) IsPortProtected(port int) bool {
	for _, p := range c.ProtectedPorts {
		if p == port {
			return true
		}
	}
	return false
}

func (c *Config) AddExcludePath(path string) error {
	// Expand ~ to home directory
	if path[:2] == "~/" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		path = filepath.Join(homeDir, path[2:])
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return err
	}

	// Check if already exists
	for _, existing := range c.ExcludePaths {
		if existing == absPath {
			return nil
		}
	}

	c.ExcludePaths = append(c.ExcludePaths, absPath)
	return Save(c)
}

func (c *Config) ShouldCleanup(path string, modTime time.Time) bool {
	// Check if path is excluded
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}

	for _, excluded := range c.ExcludePaths {
		if absPath == excluded {
			return false
		}
	}

	// Check if recently modified
	age := time.Since(modTime)
	maxAge := time.Duration(c.MaxAgeDaysForCleanup) * 24 * time.Hour
	return age > maxAge
}

