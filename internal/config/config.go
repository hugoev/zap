package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Config struct {
	ProtectedPorts         []int    `json:"protected_ports"`
	MaxAgeDaysForCleanup   int      `json:"max_age_days_for_cleanup"`
	ExcludePaths           []string `json:"exclude_paths"`
	AutoConfirmSafeActions bool     `json:"auto_confirm_safe_actions"`
}

var defaultConfig = Config{
	ProtectedPorts:         []int{5432, 6379, 3306, 27017}, // Postgres, Redis, MySQL, MongoDB
	MaxAgeDaysForCleanup:   14,
	ExcludePaths:           []string{},
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
	if path == "" {
		return fmt.Errorf("path cannot be empty")
	}

	// Expand ~ to home directory
	if len(path) >= 2 && path[:2] == "~/" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("failed to get home directory: %w", err)
		}
		path = filepath.Join(homeDir, path[2:])
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("failed to resolve absolute path: %w", err)
	}

	// Verify path exists
	if _, err := os.Stat(absPath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("path does not exist: %s", absPath)
		}
		return fmt.Errorf("cannot access path %s: %w", absPath, err)
	}

	// Check if already exists
	for _, existing := range c.ExcludePaths {
		if existing == absPath {
			return nil // Already excluded
		}
	}

	c.ExcludePaths = append(c.ExcludePaths, absPath)
	return Save(c)
}

func (c *Config) ShouldCleanup(path string, modTime time.Time) bool {
	// Validate inputs
	if path == "" {
		return false
	}
	if modTime.IsZero() {
		return false
	}

	// Check if path is excluded
	absPath, err := filepath.Abs(path)
	if err != nil {
		// If we can't resolve the path, err on the side of caution and don't cleanup
		return false
	}

	for _, excluded := range c.ExcludePaths {
		if absPath == excluded {
			return false
		}
		// Also check if the path is a subdirectory of an excluded path
		rel, err := filepath.Rel(excluded, absPath)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, "..") {
			return false
		}
	}

	// Validate max age is reasonable
	maxAgeDays := c.MaxAgeDaysForCleanup
	if maxAgeDays <= 0 {
		maxAgeDays = 14 // Default fallback
	}
	if maxAgeDays > 365 {
		maxAgeDays = 365 // Cap at 1 year for safety
	}

	// Check if recently modified
	age := time.Since(modTime)
	maxAge := time.Duration(maxAgeDays) * 24 * time.Hour
	return age > maxAge
}
