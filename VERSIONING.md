# Version Management

## Fully Automated Versioning

**Versioning is 100% automated - you never need to touch version files!**

The version is **automatically derived from git tags** at build time using Go's `-ldflags`. This is the production-grade approach used by major Go projects.

### How It Works

1. **Development builds**: Version shows as `dev` (no git tags needed)
2. **Release builds**: Version comes from git tags automatically
3. **No manual updates**: Version is never hardcoded or manually updated
4. **Always accurate**: Version always matches the git tag

### Version Source

- **Development**: `internal/version/version.go` defaults to `"dev"`
- **Releases**: Version injected at build time from git tags using `-ldflags`
- **Build command**: `go build -ldflags "-X github.com/hugoev/zap/internal/version.Version=$(git describe --tags)"`

## Automated Release Workflow

**Zero manual work required!** The workflow automatically:

1. ✅ Detects changes to any `.go` file
2. ✅ Analyzes commit messages to determine bump type (major/minor/patch)
3. ✅ Calculates new version based on latest git tag
4. ✅ Creates git tag automatically (e.g., `v0.3.1`, `v0.4.0`, `v1.0.0`)
5. ✅ Creates GitHub release with changelog
6. ✅ Version is always derived from tags - no file updates needed

### Commit Message Conventions

The workflow analyzes your commit messages to determine the version bump:

- **`feat:` or `feature:`** → MINOR bump (`0.3.0` → `0.4.0`)
- **`fix:` or `bugfix:`** → PATCH bump (`0.3.0` → `0.3.1`)
- **`BREAKING:` or `major:`** → MAJOR bump (`0.3.0` → `1.0.0`)
- **Other types** → PATCH bump (default)

See [CONTRIBUTING.md](CONTRIBUTING.md) for detailed commit message guidelines.

## How to Release a New Version

### Automatic Version Bumping (Recommended)

Just commit your changes with the appropriate prefix:

**For a new feature (MINOR bump):**

```bash
git commit -m "feat: add new command"
git push
# Automatically bumps: 0.3.0 → 0.4.0
```

**For a bug fix (PATCH bump):**

```bash
git commit -m "fix: resolve port scanning issue"
git push
# Automatically bumps: 0.3.0 → 0.3.1
```

**For a breaking change (MAJOR bump):**

```bash
git commit -m "BREAKING: change API structure"
git push
# Automatically bumps: 0.3.0 → 1.0.0
```

The workflow will:

- Detect `.go` file changes
- Analyze commit messages
- Auto-bump version appropriately
- Create tag and release

### Manual Version Override

If you need to set a specific version, create a git tag:

```bash
git tag -a v0.5.0 -m "Release v0.5.0"
git push origin v0.5.0
```

The version will automatically be derived from this tag in all builds. **No file edits needed!**

### Building with Version

**Development build** (no tags):

```bash
go build ./cmd/zap
# Version will be "dev"
```

**Release build** (with git tags):

```bash
go build -ldflags "-X github.com/hugoev/zap/internal/version.Version=$(git describe --tags)" ./cmd/zap
# Version will be from git tags (e.g., "v0.3.0")
```

**Using Makefile** (recommended):

```bash
make build    # Builds with version from git tags
make install  # Installs with version from git tags
make version  # Shows current version
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
