# Contributing to zap

## Commit Message Conventions

zap uses **Conventional Commits** to automatically determine version bumps. Your commit message format determines how the version is incremented.

### Commit Message Format

```
<type>: <description>

[optional body]

[optional footer]
```

### Commit Types

#### `feat:` or `feature:` - New Features (Minor Bump)
Use for new features or functionality.

**Examples:**
```bash
git commit -m "feat: add zap update command"
git commit -m "feature: support custom port ranges"
```

**Version Impact:** `0.3.0` → `0.4.0` (MINOR bump)

#### `fix:` or `bugfix:` - Bug Fixes (Patch Bump)
Use for bug fixes and patches.

**Examples:**
```bash
git commit -m "fix: handle edge case in port scanning"
git commit -m "bugfix: correct version parsing logic"
```

**Version Impact:** `0.3.0` → `0.3.1` (PATCH bump)

#### `BREAKING:` or `major:` - Breaking Changes (Major Bump)
Use for breaking changes that require users to update their code or configuration.

**Examples:**
```bash
git commit -m "BREAKING: change config file location"
git commit -m "major: remove deprecated --force flag"
```

**Version Impact:** `0.3.0` → `1.0.0` (MAJOR bump)

#### Other Types (Patch Bump)
Any other commit type defaults to a patch bump:
- `docs:` - Documentation changes
- `refactor:` - Code refactoring
- `test:` - Test additions/changes
- `chore:` - Maintenance tasks
- `style:` - Code style changes

**Examples:**
```bash
git commit -m "docs: update README"
git commit -m "refactor: improve error handling"
git commit -m "chore: update dependencies"
```

**Version Impact:** `0.3.0` → `0.3.1` (PATCH bump)

## Version Bump Logic

The workflow analyzes commit messages since the last tag to determine the bump type:

1. **If `BREAKING:` or `major:` found** → MAJOR bump
2. **Else if `feat:` or `feature:` found** → MINOR bump  
3. **Else if `fix:` or `bugfix:` found** → PATCH bump
4. **Else** → PATCH bump (default)

### Examples

**Scenario 1: Feature Release**
```bash
git commit -m "feat: add new cleanup patterns"
git push
# Result: 0.3.0 → 0.4.0 (MINOR bump)
```

**Scenario 2: Bug Fix Release**
```bash
git commit -m "fix: correct port detection logic"
git push
# Result: 0.3.0 → 0.3.1 (PATCH bump)
```

**Scenario 3: Breaking Change**
```bash
git commit -m "BREAKING: change default config path"
git push
# Result: 0.3.0 → 1.0.0 (MAJOR bump)
```

**Scenario 4: Multiple Commits**
```bash
git commit -m "feat: add new command"
git commit -m "fix: handle edge case"
git push
# Result: 0.3.0 → 0.4.0 (MINOR bump - highest priority)
```

## Manual Version Override

If you need to manually set a version, just update `internal/version/version.go`:

```go
const Version = "0.5.0"  // Your desired version
```

Then commit and push:
```bash
git commit -m "chore: bump version to 0.5.0"
git push
```

**Manual version changes take precedence** - the workflow will use your version instead of auto-bumping.

## Best Practices

1. **Be descriptive**: Write clear commit messages
   ```bash
   # Good
   git commit -m "feat: add support for custom port ranges"
   
   # Bad
   git commit -m "update"
   ```

2. **Use conventional prefixes**: Always start with `feat:`, `fix:`, etc.
   ```bash
   # Good
   git commit -m "fix: resolve memory leak in port scanner"
   
   # Bad
   git commit -m "memory leak fix"
   ```

3. **One feature per commit**: Keep commits focused
   ```bash
   # Good - separate commits
   git commit -m "feat: add verbose logging"
   git commit -m "feat: add progress indicators"
   
   # Bad - combined
   git commit -m "feat: add logging and progress"
   ```

4. **Use BREAKING carefully**: Only for actual breaking changes
   ```bash
   # Good
   git commit -m "BREAKING: rename --yes flag to --auto-confirm"
   
   # Bad
   git commit -m "BREAKING: update README"
   ```

## Workflow Summary

1. **Make your changes** to `.go` files
2. **Commit with appropriate prefix** (`feat:`, `fix:`, `BREAKING:`, etc.)
3. **Push to main branch**
4. **Workflow automatically:**
   - Detects `.go` file changes
   - Analyzes commit messages
   - Determines bump type (major/minor/patch)
   - Updates version in `internal/version/version.go`
   - Creates git tag and GitHub release

That's it! No manual version management needed.

