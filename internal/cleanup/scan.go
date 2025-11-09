package cleanup

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type DirectoryInfo struct {
	Path    string
	Size    int64
	ModTime time.Time
}

var cleanupPatterns = []string{
	// Node.js
	"node_modules",
	".next",
	".turbo",
	".nuxt",
	".output",
	".vite",
	".svelte-kit",
	".astro",
	"dist",
	"build",
	".cache",
	// Python
	".venv",
	"venv",
	"__pycache__",
	".pytest_cache",
	".mypy_cache",
	".ruff_cache",
	".coverage",
	"*.egg-info",
	// Rust/Cargo
	"target",
	// Java/Kotlin
	".gradle",
	"build",
	".m2",
	// Go
	"vendor",
	// General
	".cache",
	".DS_Store",
	"Thumbs.db",
	// Bun
	".bun",
	"bun.lockb",
	// Deno
	".deno",
	// TypeScript
	"*.tsbuildinfo",
	// Other
	".parcel-cache",
	".eslintcache",
	".stylelintcache",
}

func ScanDirectories(rootPath string, shouldCleanup func(path string, modTime time.Time) bool, progressCallback func(string)) ([]DirectoryInfo, error) {
	var directories []DirectoryInfo
	var scanErrors []error

	// Validate root path exists and is a directory
	rootInfo, err := os.Stat(rootPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("path does not exist: %s", rootPath)
		}
		if os.IsPermission(err) {
			return nil, fmt.Errorf("permission denied: %s", rootPath)
		}
		return nil, fmt.Errorf("cannot access path %s: %w", rootPath, err)
	}
	if !rootInfo.IsDir() {
		return nil, fmt.Errorf("path is not a directory: %s", rootPath)
	}

	err = filepath.Walk(rootPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			// Log permission errors but continue
			if os.IsPermission(err) {
				scanErrors = append(scanErrors, fmt.Errorf("permission denied: %s", path))
				return nil // Skip this path, continue scanning
			}
			// For other errors, skip but log
			scanErrors = append(scanErrors, fmt.Errorf("error accessing %s: %w", path, err))
			return nil
		}

		if !info.IsDir() {
			return nil
		}

		// Skip symlinks to avoid following them into unexpected places
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}

		// Report progress
		if progressCallback != nil {
			progressCallback(path)
		}

		// Check if this directory matches a cleanup pattern
		dirName := info.Name()
		matches := false
		for _, pattern := range cleanupPatterns {
			if dirName == pattern {
				matches = true
				break
			}
		}

		if !matches {
			return nil
		}

		// Calculate directory size with timeout protection
		size, err := calculateDirSize(path)
		if err != nil {
			scanErrors = append(scanErrors, fmt.Errorf("failed to calculate size for %s: %w", path, err))
			return filepath.SkipDir // Skip this directory but continue
		}

		// Check if should cleanup based on config
		if shouldCleanup(path, info.ModTime()) {
			directories = append(directories, DirectoryInfo{
				Path:    path,
				Size:    size,
				ModTime: info.ModTime(),
			})
		}

		// Don't descend into these directories
		return filepath.SkipDir
	})

	// Return results even if there were some errors (partial success)
	if err != nil && len(directories) == 0 {
		return nil, fmt.Errorf("scan failed: %w", err)
	}

	return directories, nil
}

func calculateDirSize(path string) (int64, error) {
	var size int64
	var sizeErrors []error
	fileCount := 0
	maxFiles := 100000 // Limit to prevent excessive scanning

	err := filepath.Walk(path, func(filePath string, info os.FileInfo, err error) error {
		if err != nil {
			// Log but continue - permission errors on individual files shouldn't stop us
			if os.IsPermission(err) {
				sizeErrors = append(sizeErrors, fmt.Errorf("permission denied: %s", filePath))
				return nil
			}
			return err
		}

		// Skip symlinks
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}

		if !info.IsDir() {
			size += info.Size()
			fileCount++
			// Safety limit to prevent excessive scanning
			if fileCount > maxFiles {
				return fmt.Errorf("directory too large (>%d files), size calculation stopped", maxFiles)
			}
		}
		return nil
	})

	// If we hit the file limit, return partial size with error
	if err != nil && strings.Contains(err.Error(), "too large") {
		return size, fmt.Errorf("directory size calculation incomplete (stopped at %d files): %w", fileCount, err)
	}

	// Return size even if there were some permission errors
	if err != nil {
		return size, fmt.Errorf("error calculating directory size: %w", err)
	}

	return size, nil
}

func FormatSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

