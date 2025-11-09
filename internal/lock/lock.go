package lock

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
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

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(lockPath), 0755); err != nil {
		return nil, fmt.Errorf("failed to create lock directory: %w", err)
	}

	// Open lock file
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to create lock file: %w", err)
	}

	// Try to acquire exclusive lock (non-blocking)
	err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil {
		file.Close()
		// Check if lock file exists and read PID
		if existingPID, readErr := os.ReadFile(lockPath); readErr == nil {
			return nil, fmt.Errorf("another instance of zap is already running (PID: %s)", string(existingPID))
		}
		return nil, fmt.Errorf("another instance of zap is already running")
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

// Release releases the lock and removes the lock file
func (l *InstanceLock) Release() error {
	if l.lockFile != nil {
		syscall.Flock(int(l.lockFile.Fd()), syscall.LOCK_UN)
		l.lockFile.Close()
		os.Remove(l.path)
	}
	return nil
}

