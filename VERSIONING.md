# Version Management

## Current Version

The version is defined in `cmd/zap/main.go`:

```go
const version = "0.3.0"
```

## Automated Release Workflow

**Versioning is now automated!** When you push a version change to GitHub, the workflow automatically:

1. ✅ Detects the version change in `cmd/zap/main.go`
2. ✅ Validates the semantic version format
3. ✅ Creates a git tag (e.g., `v0.3.0`)
4. ✅ Creates a GitHub release with changelog
5. ✅ Prevents duplicate releases for the same version

## How to Release a New Version

### Step 1: Update the Version

Edit `cmd/zap/main.go` and change the version:

```go
const version = "0.4.0"  // or whatever version you want
```

### Step 2: Commit and Push

```bash
git add cmd/zap/main.go
git commit -m "Bump version to 0.4.0"
git push origin main
```

### Step 3: Automation Takes Over

The GitHub Actions workflow will:
- Detect the version change
- Validate the format (must be MAJOR.MINOR.PATCH)
- Check if the tag already exists (prevents duplicates)
- Create the tag `v0.4.0`
- Create a GitHub release with changelog
- Make it available for `go install github.com/hugoev/zap/cmd/zap@v0.4.0`

**That's it!** No manual tagging needed.

### Manual Tagging (Fallback)

If you need to create a tag manually (e.g., if automation fails):

```bash
git tag -a v0.4.0 -m "Release version 0.4.0"
git push origin v0.4.0
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
