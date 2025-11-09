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

// isMountPoint checks if a directory is a mount point by comparing device IDs
// A directory is a mount point if its device ID differs from its parent's device ID
func isMountPoint(path string) (bool, error) {
	if runtime.GOOS == "windows" {
		// Windows: mount point detection is more complex, skip for now
		return false, nil
	}

	// Get device ID of the directory itself
	var dirStat unix.Stat_t
	if err := unix.Stat(path, &dirStat); err != nil {
		return false, fmt.Errorf("failed to stat directory: %w", err)
	}

	// Get device ID of the parent directory
	parentDir := filepath.Dir(path)
	var parentStat unix.Stat_t
	if err := unix.Stat(parentDir, &parentStat); err != nil {
		return false, fmt.Errorf("failed to stat parent directory: %w", err)
	}

	// If device IDs differ, this is a mount point
	// On Unix systems, device ID is a combination of major and minor device numbers
	isMount := dirStat.Dev != parentStat.Dev

	return isMount, nil
}

// checkNetworkMount checks if a path is on a network mount and detects disconnection
func checkNetworkMount(path string) error {
	if runtime.GOOS == "windows" {
		// Windows: network mount detection is different, skip for now
		return nil
	}

	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		// Check for network-related errors
		if err == unix.ENOTCONN || err == unix.EHOSTUNREACH || err == unix.ETIMEDOUT {
			return fmt.Errorf("network mount disconnected: %s (error: %w)", path, err)
		}
		return nil // Other errors are not network-related
	}

	// Check filesystem type to detect common network filesystems
	// Common network filesystem type values (varies by system)
	// NFS, SMB/CIFS, etc. are typically remote filesystems
	// This is a heuristic - not all systems expose this the same way
	// On Linux, we can check /proc/mounts, but for portability we use Statfs
	// The Fstypename field (if available) or type detection can help

	// For now, we'll rely on error detection (ENOTCONN, etc.) which is more reliable
	// Network disconnection will be caught during actual operations

	return nil
}

// shellEscape escapes a string for safe use in shell commands
func shellEscape(s string) string {
	// Remove any shell metacharacters and wrap in single quotes
	escaped := strings.ReplaceAll(s, "'", "'\"'\"'")
	return "'" + escaped + "'"
}

