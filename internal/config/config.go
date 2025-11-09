package config

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
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
		// Fallback to temp directory if home directory is unavailable
		tempDir := os.TempDir()
		configDir := filepath.Join(tempDir, "zap-config")
		if err := os.MkdirAll(configDir, 0755); err != nil {
			return "", fmt.Errorf("failed to create config directory in temp: %w", err)
		}
		return filepath.Join(configDir, "config.json"), nil
	}

	configDir := filepath.Join(homeDir, ".config", "zap")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		// Try alternative location if .config can't be created
		altDir := filepath.Join(os.TempDir(), "zap-config")
		if mkdirErr := os.MkdirAll(altDir, 0755); mkdirErr == nil {
			return filepath.Join(altDir, "config.json"), nil
		}
		return "", fmt.Errorf("failed to create config directory: %w", err)
	}
	return filepath.Join(configDir, "config.json"), nil
}

func getBackupPath(configPath string) string {
	return configPath + ".backup"
}

// getBackupPath2 returns a secondary backup path (for multiple backup levels)
func getBackupPath2(configPath string) string {
	return configPath + ".backup2"
}

// renameFile performs an atomic rename, falling back to copy+remove for cross-filesystem moves
func renameFile(src, dst string) error {
	// Try atomic rename first (works on same filesystem)
	err := os.Rename(src, dst)
	if err == nil {
		return nil
	}

	// Check if error is due to cross-filesystem move (EXDEV)
	if linkErr, ok := err.(*os.LinkError); ok {
		if linkErr.Err == syscall.EXDEV {
			// Cross-filesystem: use copy+remove
			if copyErr := copyConfigFile(src, dst); copyErr != nil {
				return fmt.Errorf("cross-filesystem rename failed (copy step): %w", copyErr)
			}
			// Verify destination before removing source
			if _, statErr := os.Stat(dst); statErr != nil {
				return fmt.Errorf("cross-filesystem rename failed (verification): %w", statErr)
			}
			// Remove source after successful copy
			if removeErr := os.Remove(src); removeErr != nil {
				// Log but don't fail - destination is correct
			}
			return nil
		}
	}

	// Other rename errors
	return fmt.Errorf("rename failed: %w", err)
}

// copyConfigFile copies a config file preserving permissions
func copyConfigFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open source file: %w", err)
	}
	defer sourceFile.Close()

	// Get source file info for permissions
	sourceInfo, err := sourceFile.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat source file: %w", err)
	}

	// Create destination file with same permissions
	destFile, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, sourceInfo.Mode())
	if err != nil {
		return fmt.Errorf("failed to create destination file: %w", err)
	}
	defer destFile.Close()

	// Copy contents
	_, err = io.Copy(destFile, sourceFile)
	if err != nil {
		return fmt.Errorf("failed to copy file contents: %w", err)
	}

	// Sync to ensure data is written
	if err := destFile.Sync(); err != nil {
		return fmt.Errorf("failed to sync destination file: %w", err)
	}

	return nil
}

// checkDiskSpaceForConfig verifies sufficient disk space for config file operations
func checkDiskSpaceForConfig(filePath string, requiredBytes int64) error {
	if runtime.GOOS == "windows" {
		// Windows: skip disk space check
		return nil
	}

	var stat unix.Statfs_t
	dir := filepath.Dir(filePath)
	if err := unix.Statfs(dir, &stat); err != nil {
		// If we can't check, allow operation (better than blocking)
		return nil
	}

	// Calculate available space
	availableBytes := int64(stat.Bavail) * int64(stat.Bsize)

	// Require at least 2x the size to be available (safety margin)
	requiredWithMargin := requiredBytes * 2

	if availableBytes < requiredWithMargin {
		return fmt.Errorf("insufficient disk space: need %d bytes, have %d bytes available", requiredWithMargin, availableBytes)
	}

	return nil
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
			// Config is corrupted - try to recover
			return recoverFromCorruption(configPath, err)
		}

		// Validate config
		if err := cfg.Validate(); err != nil {
			// Try backup
			if backupCfg, backupErr := loadFromBackup(configPath); backupErr == nil {
				if backupErr := backupCfg.Validate(); backupErr == nil {
					if saveErr := saveWithLock(backupCfg); saveErr == nil {
						return backupCfg, nil
					}
				}
			}
			// Reset to defaults
			cfg = defaultConfig
			if saveErr := saveWithLock(&cfg); saveErr != nil {
				return nil, fmt.Errorf("config validation failed: %w", err)
			}
			return &cfg, nil
		}

		// Create backup
		backupPath := getBackupPath(configPath)
		os.WriteFile(backupPath, data, 0644)

		mergeWithDefaults(&cfg)
		return &cfg, nil
	}

	// Read file content for backup and decoding
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	// Decode from file
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		// Config is corrupted - try to recover from backup
		return recoverFromCorruption(configPath, err)
	}

	// Validate config
	if err := cfg.Validate(); err != nil {
		// Config has invalid values - try backup, then reset to defaults
		if backupCfg, backupErr := loadFromBackup(configPath); backupErr == nil {
			if backupErr := backupCfg.Validate(); backupErr == nil {
				// Backup is valid, restore it
				if saveErr := saveWithLock(backupCfg); saveErr == nil {
					return backupCfg, nil
				}
			}
		}
		// Backup invalid or restore failed - reset to defaults
		cfg = defaultConfig
		if saveErr := saveWithLock(&cfg); saveErr != nil {
			return nil, fmt.Errorf("config validation failed and could not reset: %w (original error: %v)", saveErr, err)
		}
		return &cfg, nil
	}

	// Successfully loaded - create/update backup
	backupPath := getBackupPath(configPath)
	os.WriteFile(backupPath, data, 0644)

	// Merge with defaults for missing fields
	mergeWithDefaults(&cfg)

	return &cfg, nil
}

func recoverFromCorruption(configPath string, decodeErr error) (*Config, error) {
	// Try to restore from primary backup first
	if backupCfg, err := loadFromBackup(configPath); err == nil {
		// Backup exists and is valid - restore it
		if saveErr := saveWithLock(backupCfg); saveErr == nil {
			return backupCfg, nil
		}
	}

	// Try secondary backup if primary backup failed
	backupPath2 := getBackupPath2(configPath)
	if backupData2, err := os.ReadFile(backupPath2); err == nil {
		var backupCfg2 Config
		if json.Unmarshal(backupData2, &backupCfg2) == nil {
			// Secondary backup is valid - restore it
			if saveErr := saveWithLock(&backupCfg2); saveErr == nil {
				return &backupCfg2, nil
			}
		}
	}

	// No valid backup - rename corrupted file and create new
	corruptedPath := configPath + ".corrupted." + fmt.Sprintf("%d", time.Now().Unix())
	if renameErr := os.Rename(configPath, corruptedPath); renameErr == nil {
		// Create new config with defaults
		cfg := defaultConfig
		if saveErr := saveWithLock(&cfg); saveErr != nil {
			return nil, fmt.Errorf("config corrupted and could not create new config: %w (corrupted file saved as: %s)", saveErr, corruptedPath)
		}
		return &cfg, nil
	}

	return nil, fmt.Errorf("config file corrupted and recovery failed: %w", decodeErr)
}

func loadFromBackup(configPath string) (*Config, error) {
	backupPath := getBackupPath(configPath)
	data, err := os.ReadFile(backupPath)
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

	// Check disk space before writing (prevent corruption from disk full)
	if err := checkDiskSpaceForConfig(configPath, int64(len(data))); err != nil {
		return fmt.Errorf("insufficient disk space for config save: %w", err)
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

		// Acquire exclusive lock with timeout (non-blocking first, then blocking with timeout)
		// Try non-blocking first
		if err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
			// Lock is held - this shouldn't happen in normal operation since we have mutex
			// But handle it gracefully with a timeout
			if err == unix.EWOULDBLOCK {
				return fmt.Errorf("config file is locked by another process (timeout)")
			}
			return fmt.Errorf("failed to lock config file: %w", err)
		}
		defer unix.Flock(int(file.Fd()), unix.LOCK_UN)

		if _, err := file.Write(data); err != nil {
			return fmt.Errorf("failed to write config: %w", err)
		}

		if err := file.Sync(); err != nil {
			return fmt.Errorf("failed to sync config: %w", err)
		}

		// Create multiple backup levels before replacing (check disk space first)
		if existingData, readErr := os.ReadFile(configPath); readErr == nil {
			// Primary backup
			backupPath := getBackupPath(configPath)
			if backupErr := checkDiskSpaceForConfig(backupPath, int64(len(existingData))); backupErr == nil {
				os.WriteFile(backupPath, existingData, 0644)
			}
			// Secondary backup (rotate: backup2 becomes backup, current becomes backup2)
			backupPath2 := getBackupPath2(configPath)
			if backupData2, readErr2 := os.ReadFile(backupPath); readErr2 == nil {
				if backupErr2 := checkDiskSpaceForConfig(backupPath2, int64(len(backupData2))); backupErr2 == nil {
					os.WriteFile(backupPath2, backupData2, 0644)
				}
			}
			// Log but don't fail - backups are optional
		}

		// Atomic rename (atomic on most filesystems, fallback for cross-filesystem)
		// This is crash-safe: if rename fails, temp file remains and can be recovered
		if err := renameFile(tempPath, configPath); err != nil {
			// On failure, temp file still exists - attempt recovery
			// Check if temp file is valid JSON before suggesting recovery
			if tempData, readErr := os.ReadFile(tempPath); readErr == nil {
				var testCfg Config
				if json.Unmarshal(tempData, &testCfg) == nil {
					// Temp file is valid - suggest manual recovery
					return fmt.Errorf("failed to commit config (temp file is valid at %s): %w", tempPath, err)
				}
			}
			return fmt.Errorf("failed to commit config: %w", err)
		}

		// Verify the config was written correctly (crash recovery check)
		if verifyData, readErr := os.ReadFile(configPath); readErr == nil {
			var verifyCfg Config
			if json.Unmarshal(verifyData, &verifyCfg) != nil {
				// Config is corrupted after write - attempt recovery from backup
				backupPath := getBackupPath(configPath)
				if backupData, backupErr := os.ReadFile(backupPath); backupErr == nil {
					// Restore from backup
					os.WriteFile(configPath, backupData, 0644)
					return fmt.Errorf("config corrupted after write - restored from backup")
				}
				return fmt.Errorf("config corrupted after write and backup recovery failed")
			}
		}
	} else {
		// Windows: simple atomic write (no file locking support)
		if err := os.WriteFile(tempPath, data, 0644); err != nil {
			return fmt.Errorf("failed to write temp config: %w", err)
		}
		defer os.Remove(tempPath) // Cleanup on error

		// Create multiple backup levels before replacing (check disk space first)
		if existingData, readErr := os.ReadFile(configPath); readErr == nil {
			// Primary backup
			backupPath := getBackupPath(configPath)
			if backupErr := checkDiskSpaceForConfig(backupPath, int64(len(existingData))); backupErr == nil {
				os.WriteFile(backupPath, existingData, 0644)
			}
			// Secondary backup (rotate: backup2 becomes backup, current becomes backup2)
			backupPath2 := getBackupPath2(configPath)
			if backupData2, readErr2 := os.ReadFile(backupPath); readErr2 == nil {
				if backupErr2 := checkDiskSpaceForConfig(backupPath2, int64(len(backupData2))); backupErr2 == nil {
					os.WriteFile(backupPath2, backupData2, 0644)
				}
			}
			// Log but don't fail - backups are optional
		}

		// Atomic rename (with cross-filesystem fallback)
		if err := renameFile(tempPath, configPath); err != nil {
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

// Validate checks that all config values are within acceptable ranges
func (c *Config) Validate() error {
	// Validate protected ports
	for _, port := range c.ProtectedPorts {
		if port < 1 || port > 65535 {
			return fmt.Errorf("invalid protected port: %d (must be 1-65535)", port)
		}
	}

	// Validate max age
	if c.MaxAgeDaysForCleanup < 1 {
		return fmt.Errorf("max_age_days_for_cleanup must be at least 1")
	}
	if c.MaxAgeDaysForCleanup > 365 {
		return fmt.Errorf("max_age_days_for_cleanup cannot exceed 365 days")
	}

	// Validate exclude paths
	for _, path := range c.ExcludePaths {
		if path == "" {
			return fmt.Errorf("exclude path cannot be empty")
		}
		if !filepath.IsAbs(path) {
			return fmt.Errorf("exclude path must be absolute: %s", path)
		}
	}

	return nil
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
