package cleanup

import (
	"fmt"
	"os"
	"strings"
	"time"
)

const (
	// DeletionVerificationTimeout is how long we wait to verify deletion
	DeletionVerificationTimeout = 2 * time.Second
	// DeletionCheckInterval is how often we check if deletion succeeded
	DeletionCheckInterval = 100 * time.Millisecond
)

func DeleteDirectory(path string) error {
	// Validate path security first
	if err := validatePath(path); err != nil {
		return fmt.Errorf("path validation failed: %w", err)
	}

	// Validate path exists before attempting deletion
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // Already deleted, not an error
		}
		return fmt.Errorf("cannot access path %s: %w", path, err)
	}

	if !info.IsDir() {
		return fmt.Errorf("path is not a directory: %s", path)
	}

	// Check disk space before deletion (safety check)
	if err := checkDiskSpace(path, info.Size()); err != nil {
		return fmt.Errorf("disk space check failed: %w", err)
	}

	// Attempt deletion with retry logic (handles active writes)
	maxRetries := 3
	baseDelay := 100 * time.Millisecond
	
	var lastErr error
	for attempt := 1; attempt <= maxRetries; attempt++ {
		err = os.RemoveAll(path)
		if err == nil {
			// Success
			break
		}
		
		lastErr = err
		
		// Check if error is transient (file/directory busy, permission denied temporarily)
		errStr := err.Error()
		isTransient := strings.Contains(errStr, "device or resource busy") ||
			strings.Contains(errStr, "resource temporarily unavailable") ||
			strings.Contains(errStr, "permission denied")
		
		if !isTransient || attempt == maxRetries {
			// Not a transient error or last attempt
			break
		}
		
		// Exponential backoff: 100ms, 200ms, 400ms
		delay := baseDelay * time.Duration(1<<uint(attempt-1))
		time.Sleep(delay)
	}
	
	if err != nil {
		return fmt.Errorf("failed to delete %s after %d attempts: %w", path, maxRetries, lastErr)
	}

	// Verify deletion succeeded
	deadline := time.Now().Add(DeletionVerificationTimeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return nil // Successfully deleted
		}
		time.Sleep(DeletionCheckInterval)
	}

	// Final check
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil
	}

	return fmt.Errorf("deletion verification failed: %s still exists", path)
}

func DeleteDirectories(dirs []DirectoryInfo) error {
	var errors []error
	deletedCount := 0

	for _, dir := range dirs {
		if err := DeleteDirectory(dir.Path); err != nil {
			errors = append(errors, fmt.Errorf("%s: %w", dir.Path, err))
			// Continue with other directories even if one fails
		} else {
			deletedCount++
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("failed to delete %d of %d directories: %v", len(errors), len(dirs), errors)
	}

	return nil
}

func GetTotalSize(dirs []DirectoryInfo) int64 {
	var total int64
	for _, dir := range dirs {
		total += dir.Size
	}
	return total
}


