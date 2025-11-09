package cleanup

import (
	"fmt"
	"os"
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

	// Attempt deletion
	err = os.RemoveAll(path)
	if err != nil {
		return fmt.Errorf("failed to delete %s: %w", path, err)
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


