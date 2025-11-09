package lock

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// InstanceLock prevents multiple instances of zap from running simultaneously
type InstanceLock struct {
	lockFile *os.File
	path     string
}

// AcquireLock creates a lock file and acquires an exclusive lock
// Returns an error if another instance is already running
func AcquireLock() (*InstanceLock, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	lockPath := filepath.Join(homeDir, ".config", "zap", ".lock")

	// Check if lock directory is on a network mount (could cause issues)
	// We'll handle this gracefully by checking if we can create the directory
	lockDir := filepath.Dir(lockPath)
	if err := os.MkdirAll(lockDir, 0755); err != nil {
		// Check if it's a network mount error
		if pathErr, ok := err.(*os.PathError); ok {
			if pathErr.Err == syscall.ENOTCONN || pathErr.Err == syscall.EHOSTUNREACH || pathErr.Err == syscall.ETIMEDOUT {
				return nil, fmt.Errorf("lock directory is on a disconnected network mount: %w", err)
			}
		}
		return nil, fmt.Errorf("failed to create lock directory: %w", err)
	}

	// Open lock file first (before cleanup to avoid race condition)
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to create lock file: %w", err)
	}

	// Try to acquire exclusive lock (non-blocking) first
	err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err == nil {
		// Lock acquired successfully - check for stale lock and clean up if needed
		// (We have the lock, so it's safe to clean up)
		cleanupStaleLock(lockPath)
	} else {
		// Lock is held - check if it's stale before reporting error
		file.Close()
		// Try to clean up stale lock (might allow us to acquire it)
		if cleanupErr := cleanupStaleLock(lockPath); cleanupErr == nil {
			// Try again after cleanup
			file, err = os.OpenFile(lockPath, os.O_CREATE|os.O_WRONLY, 0644)
			if err == nil {
				err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
			}
		}
		if err != nil {
			// Still can't acquire - check if lock file exists and read PID
			if existingPID, readErr := os.ReadFile(lockPath); readErr == nil {
				return nil, fmt.Errorf("another instance of zap is already running (PID: %s)", string(existingPID))
			}
			return nil, fmt.Errorf("another instance of zap is already running")
		}
	}

	// Write PID to lock file
	pid := fmt.Sprintf("%d\n", os.Getpid())
	file.Truncate(0)
	file.Seek(0, 0)
	if _, err := file.WriteString(pid); err != nil {
		file.Close()
		return nil, fmt.Errorf("failed to write PID to lock file: %w", err)
	}
	file.Sync()

	return &InstanceLock{lockFile: file, path: lockPath}, nil
}

// cleanupStaleLock checks if lock file is stale and removes it if the process is no longer running
func cleanupStaleLock(lockPath string) error {
	info, err := os.Stat(lockPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No lock file, nothing to clean
		}
		return err
	}

	// Check if lock is stale (older than 1 hour)
	if time.Since(info.ModTime()) > 1*time.Hour {
		// Read PID from lock file
		pidData, readErr := os.ReadFile(lockPath)
		if readErr != nil {
			// Can't read PID, but file is stale - remove it
			os.Remove(lockPath)
			return nil
		}

		// Parse PID
		pidStr := strings.TrimSpace(string(pidData))
		pid, parseErr := strconv.Atoi(pidStr)
		if parseErr != nil {
			// Invalid PID format - remove stale lock
			os.Remove(lockPath)
			return nil
		}

		// Check if process is still running
		if !isProcessRunning(pid) {
			// Process is gone, remove stale lock
			os.Remove(lockPath)
			return nil
		}
		// Process is still running, lock is valid
	}

	return nil
}

// isProcessRunning checks if a process with the given PID is running
func isProcessRunning(pid int) bool {
	if pid <= 0 {
		return false
	}

	// Use ps to check if process exists
	cmd := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "pid=")
	output, err := cmd.Output()
	if err != nil {
		return false
	}

	// If output contains the PID, process is running
	return strings.TrimSpace(string(output)) == strconv.Itoa(pid)
}

// Release releases the lock and removes the lock file
func (l *InstanceLock) Release() error {
	if l.lockFile != nil {
		syscall.Flock(int(l.lockFile.Fd()), syscall.LOCK_UN)
		l.lockFile.Close()
		os.Remove(l.path)
	}
	return nil
}

