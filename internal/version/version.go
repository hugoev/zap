package version

import "strings"

// Version is the current version of zap.
// This is set at build time using -ldflags from git tags.
// Defaults to "dev" for development builds.
// Format: MAJOR.MINOR.PATCH (semantic versioning)
var Version = "dev"

// Commit is the git commit hash (set at build time).
var Commit = "unknown"

// Date is the build date (set at build time).
var Date = "unknown"

// Get returns the current version string.
// Cleans up git describe output (removes "v" prefix and commit info).
func Get() string {
	// If version contains git describe format (e.g., "v0.3.0-14-g045e86a"), extract just the version
	if strings.Contains(Version, "-") {
		// Extract version part before the first dash
		parts := strings.Split(Version, "-")
		versionPart := parts[0]
		// Remove "v" prefix if present
		versionPart = strings.TrimPrefix(versionPart, "v")
		return versionPart
	}
	// Remove "v" prefix if present
	return strings.TrimPrefix(Version, "v")
}

// GetFull returns the full version string including commit and date.
func GetFull() string {
	if Version == "dev" {
		return "dev"
	}
	return Version
}

// GetCommit returns the git commit hash.
func GetCommit() string {
	return Commit
}

// GetDate returns the build date.
func GetDate() string {
	return Date
}
