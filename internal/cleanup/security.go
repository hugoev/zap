package cleanup

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"golang.org/x/sys/unix"
)

// validatePath ensures a path is safe and within allowed boundaries
func validatePath(path string) error {
	if path == "" {
		return fmt.Errorf("path cannot be empty")
	}

	// Resolve to absolute path
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("invalid path: %w", err)
	}

	// Check for path traversal attempts
	if strings.Contains(path, "..") {
		return fmt.Errorf("path traversal detected: %s", path)
	}

	// Ensure path is within allowed boundaries (home directory)
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	// Resolve home directory to absolute path
	absHomeDir, err := filepath.Abs(homeDir)
	if err != nil {
		return fmt.Errorf("failed to resolve home directory: %w", err)
	}

	// Check if path is within home directory
	rel, err := filepath.Rel(absHomeDir, absPath)
	if err != nil {
		return fmt.Errorf("path outside home directory: %s", absPath)
	}

	// Prevent escaping from home directory
	if strings.HasPrefix(rel, "..") {
		return fmt.Errorf("path outside home directory: %s", absPath)
	}

	return nil
}

// checkDiskSpace verifies sufficient disk space before deletion
func checkDiskSpace(path string, requiredBytes int64) error {
	if runtime.GOOS == "windows" {
		// Windows: skip disk space check (would need different API)
		return nil
	}

	var stat unix.Statfs_t
	dir := filepath.Dir(path)
	if err := unix.Statfs(dir, &stat); err != nil {
		// If we can't check, warn but don't fail
		return nil
	}

	// Calculate available space
	availableBytes := int64(stat.Bavail) * int64(stat.Bsize)

	// Require at least 2x the size to be available (safety margin)
	requiredWithMargin := requiredBytes * 2

	if availableBytes < requiredWithMargin {
		return fmt.Errorf("insufficient disk space: need %s, have %s",
			FormatSize(requiredWithMargin), FormatSize(availableBytes))
	}

	return nil
}

// shellEscape escapes a string for safe use in shell commands
func shellEscape(s string) string {
	// Remove any shell metacharacters and wrap in single quotes
	escaped := strings.ReplaceAll(s, "'", "'\"'\"'")
	return "'" + escaped + "'"
}

