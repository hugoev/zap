package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"
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

// configMutex protects concurrent access to config file
var configMutex sync.RWMutex

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
	configMutex.RLock()
	defer configMutex.RUnlock()

	configPath, err := getConfigPath()
	if err != nil {
		return nil, err
	}

	// Open with shared lock for reading (on Unix systems)
	var file *os.File
	if runtime.GOOS != "windows" {
		file, err = os.Open(configPath)
		if os.IsNotExist(err) {
			// Release read lock and acquire write lock for creation
			configMutex.RUnlock()
			configMutex.Lock()
			defer configMutex.Unlock()

			cfg := defaultConfig
			if err := saveWithLock(&cfg); err != nil {
				return nil, err
			}
			return &cfg, nil
		}
		if err != nil {
			return nil, err
		}
		defer file.Close()

		// Acquire shared lock (read lock)
		if err := unix.Flock(int(file.Fd()), unix.LOCK_SH); err != nil {
			return nil, fmt.Errorf("failed to lock config for reading: %w", err)
		}
		defer unix.Flock(int(file.Fd()), unix.LOCK_UN)
	} else {
		// Windows: just read the file
		data, err := os.ReadFile(configPath)
		if os.IsNotExist(err) {
			configMutex.RUnlock()
			configMutex.Lock()
			defer configMutex.Unlock()

			cfg := defaultConfig
			if err := saveWithLock(&cfg); err != nil {
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
		mergeWithDefaults(&cfg)
		return &cfg, nil
	}

	// Decode from file
	var cfg Config
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("failed to decode config: %w", err)
	}

	// Merge with defaults for missing fields
	mergeWithDefaults(&cfg)

	return &cfg, nil
}

func mergeWithDefaults(cfg *Config) {
	if len(cfg.ProtectedPorts) == 0 {
		cfg.ProtectedPorts = defaultConfig.ProtectedPorts
	}
	if cfg.MaxAgeDaysForCleanup == 0 {
		cfg.MaxAgeDaysForCleanup = defaultConfig.MaxAgeDaysForCleanup
	}
	if cfg.ExcludePaths == nil {
		cfg.ExcludePaths = []string{}
	}
}

func Save(cfg *Config) error {
	configMutex.Lock()
	defer configMutex.Unlock()
	return saveWithLock(cfg)
}

// saveWithLock performs atomic write with file locking (must be called with configMutex held)
func saveWithLock(cfg *Config) error {
	configPath, err := getConfigPath()
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	// Atomic write: write to temp file, then rename
	tempPath := configPath + ".tmp"

	if runtime.GOOS != "windows" {
		// Unix: use file locking
		file, err := os.OpenFile(tempPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC|os.O_EXCL, 0644)
		if err != nil {
			return fmt.Errorf("failed to create temp config: %w", err)
		}
		defer file.Close()
		defer os.Remove(tempPath) // Cleanup on error

		// Acquire exclusive lock
		if err := unix.Flock(int(file.Fd()), unix.LOCK_EX); err != nil {
			return fmt.Errorf("failed to lock config file: %w", err)
		}
		defer unix.Flock(int(file.Fd()), unix.LOCK_UN)

		if _, err := file.Write(data); err != nil {
			return fmt.Errorf("failed to write config: %w", err)
		}

		if err := file.Sync(); err != nil {
			return fmt.Errorf("failed to sync config: %w", err)
		}

		// Atomic rename (atomic on most filesystems)
		if err := os.Rename(tempPath, configPath); err != nil {
			return fmt.Errorf("failed to commit config: %w", err)
		}
	} else {
		// Windows: simple atomic write (no file locking support)
		if err := os.WriteFile(tempPath, data, 0644); err != nil {
			return fmt.Errorf("failed to write temp config: %w", err)
		}
		defer os.Remove(tempPath) // Cleanup on error

		// Atomic rename
		if err := os.Rename(tempPath, configPath); err != nil {
			return fmt.Errorf("failed to commit config: %w", err)
		}
	}

	return nil
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
