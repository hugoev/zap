package version

// Version is the current version of zap.
// This is automatically updated by the GitHub Actions workflow.
// Format: MAJOR.MINOR.PATCH (semantic versioning)
const Version = "0.3.0"

// Get returns the current version string.
func Get() string {
	return Version
}

