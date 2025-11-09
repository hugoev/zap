# Version Management

## Current Version

The version is defined in `cmd/zap/main.go`:

```go
const version = "0.1.0"
```

## How to Update the Version

### Method 1: Manual Update (Simple)

1. Edit `cmd/zap/main.go` and change the version:

   ```go
   const version = "0.2.0"  // or whatever version you want
   ```

2. Commit the change:

   ```bash
   git add cmd/zap/main.go
   git commit -m "Bump version to 0.2.0"
   ```

3. Tag the release (recommended):

   ```bash
   git tag -a v0.2.0 -m "Release version 0.2.0"
   git push origin main --tags
   ```

4. Rebuild and reinstall:
   ```bash
   go install ./cmd/zap
   ```

### Method 2: Using Git Tags (Recommended)

For a more professional workflow:

1. Update the version in `cmd/zap/main.go`
2. Commit the changes
3. Create a git tag:
   ```bash
   git tag v0.2.0
   git push origin v0.2.0
   ```

This allows users to install specific versions:

```bash
go install github.com/hugoev/zap/cmd/zap@v0.2.0
```

## Version Numbering

Follow [Semantic Versioning](https://semver.org/):

- **MAJOR.MINOR.PATCH** (e.g., 1.2.3)
- **MAJOR**: Breaking changes
- **MINOR**: New features (backward compatible)
- **PATCH**: Bug fixes (backward compatible)

Examples:

- `0.1.0` → `0.1.1` (bug fix)
- `0.1.1` → `0.2.0` (new feature)
- `0.2.0` → `1.0.0` (stable release or breaking change)
