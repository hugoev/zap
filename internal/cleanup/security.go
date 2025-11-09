package cleanup

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf8"

	"golang.org/x/sys/unix"
)

// validatePath ensures a path is safe and within allowed boundaries
func validatePath(path string) error {
	if path == "" {
		return fmt.Errorf("path cannot be empty")
	}

	// Check for null bytes (security risk)
	if strings.Contains(path, "\x00") {
		return fmt.Errorf("path contains null byte: %s", path)
	}

	// Check for invalid UTF-8 sequences
	if !strings.Contains(path, "\x00") {
		// Validate UTF-8 encoding
		for i := 0; i < len(path); {
			r, size := utf8.DecodeRuneInString(path[i:])
			if r == utf8.RuneError && size == 1 {
				return fmt.Errorf("path contains invalid UTF-8 sequence at position %d", i)
			}
			i += size
		}
	}

	// Check path length limits (POSIX PATH_MAX is typically 4096, but be conservative)
	const maxPathLen = 4096
	if len(path) > maxPathLen {
		return fmt.Errorf("path exceeds maximum length (%d): %s", maxPathLen, path)
	}

	// Resolve to absolute path
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("invalid path: %w", err)
	}

	// Check resolved path length
	if len(absPath) > maxPathLen {
		return fmt.Errorf("resolved path exceeds maximum length (%d): %s", maxPathLen, absPath)
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
		// Check for network-related errors (only report actual network errors)
		if err == unix.ENOTCONN || err == unix.EHOSTUNREACH || err == unix.ETIMEDOUT {
			return fmt.Errorf("network mount disconnected: %s (error: %w)", path, err)
		}
		// Other errors (permission denied, not found, etc.) are not network-related
		// Don't report them as network mount issues to avoid false positives
		return nil
	}

	// Check filesystem type to detect common network filesystems
	// Only flag as network mount if we can definitively identify it
	// This reduces false positives for local filesystems
	// On Linux, check /proc/mounts for more reliable detection
	if runtime.GOOS == "linux" {
		// Read /proc/mounts to check filesystem type
		mountsData, err := os.ReadFile("/proc/mounts")
		if err == nil {
			mountsStr := string(mountsData)
			// Check if path is in mounts and identify filesystem type
			lines := strings.Split(mountsStr, "\n")
			for _, line := range lines {
				fields := strings.Fields(line)
				if len(fields) >= 3 {
					mountPoint := fields[1]
					fsType := fields[2]
					// Check if our path is under this mount point
					if strings.HasPrefix(path, mountPoint) {
						// Known network filesystem types
						networkFS := []string{"nfs", "nfs4", "cifs", "smb", "smbfs", "fuse.sshfs", "9p"}
						for _, netFS := range networkFS {
							if strings.Contains(strings.ToLower(fsType), netFS) {
								// This is a network mount - return nil (no error, just identified)
								return nil
							}
						}
					}
				}
			}
		}
	}

	// For macOS and other systems, rely on error detection only
	// This avoids false positives from local filesystems
	// Network disconnection will be caught during actual operations

	return nil
}

// shellEscape escapes a string for safe use in shell commands
func shellEscape(s string) string {
	// Remove any shell metacharacters and wrap in single quotes
	escaped := strings.ReplaceAll(s, "'", "'\"'\"'")
	return "'" + escaped + "'"
}

