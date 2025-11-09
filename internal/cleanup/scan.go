package cleanup

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type DirectoryInfo struct {
	Path    string
	Size    int64
	ModTime time.Time
}

var cleanupPatterns = []string{
	"node_modules",
	".venv",
	".cache",
	".gradle",
	".mypy_cache",
	"__pycache__",
	".pytest_cache",
	"target", // Rust/Cargo
	"dist",
	"build",
	".next",
	".turbo",
	".nuxt",
	".output",
}

func ScanDirectories(rootPath string, shouldCleanup func(path string, modTime time.Time) bool) ([]DirectoryInfo, error) {
	var directories []DirectoryInfo

	err := filepath.Walk(rootPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip errors
		}

		if !info.IsDir() {
			return nil
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

		// Calculate directory size
		size, err := calculateDirSize(path)
		if err != nil {
			return nil // Skip if we can't calculate size
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

	return directories, err
}

func calculateDirSize(path string) (int64, error) {
	var size int64
	err := filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return nil
	})
	return size, err
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

