# Version Management

## Current Version

The version is defined in `internal/version/version.go`:

```go
const Version = "0.3.0"
```

## Automated Release Workflow

**Versioning is fully automated!** The workflow uses commit message conventions to automatically determine version bumps:

1. ✅ Detects changes to any `.go` file
2. ✅ Analyzes commit messages to determine bump type (major/minor/patch)
3. ✅ Auto-bumps version based on commit type
4. ✅ Creates git tag (e.g., `v0.3.1`, `v0.4.0`, `v1.0.0`)
5. ✅ Creates GitHub release with changelog
6. ✅ Supports manual version overrides

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

If you need to set a specific version, edit `internal/version/version.go`:

```go
const Version = "0.5.0"  // Your desired version
```

Then commit and push:

```bash
git commit -m "chore: bump version to 0.5.0"
git push
```

**Manual version changes take precedence** - the workflow will use your version instead of auto-bumping.

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
