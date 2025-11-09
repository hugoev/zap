# zap

Developer environment cleanup and port process management tool.

## Quick Install

```bash
# Install from source (recommended)
go install github.com/hugoev/zap/cmd/zap@latest

# Or update if already installed
zap update
```

That's it! The binary will be in `~/go/bin/zap` (add to PATH if needed).

## Quick Start

```bash
# Free up ports (interactive)
zap ports

# Free up ports automatically
zap ports --yes

# Clean up stale dependency folders
zap cleanup

# Preview what would be cleaned
zap cleanup --dry-run
```

## What It Does

**Port Management:**

- Finds processes on common dev ports (3000, 5173, 8000, etc.)
- Safely terminates orphaned dev servers
- Protects infrastructure (Postgres, Redis, Docker)

**Workspace Cleanup:**

- Finds stale `node_modules`, `.venv`, `.cache`, build artifacts
- Shows size and age
- Respects recent modifications

## Commands

| Command       | Description                           |
| ------------- | ------------------------------------- |
| `zap ports`   | Scan and free up ports                |
| `zap cleanup` | Remove stale dependency/cache folders |
| `zap version` | Show version                          |
| `zap update`  | Update to latest version              |

## Flags

| Flag              | Description                                      |
| ----------------- | ------------------------------------------------ |
| `--yes`, `-y`     | Execute without confirmation (safe actions only) |
| `--dry-run`       | Preview actions without making changes           |
| `--verbose`, `-v` | Show detailed information                        |

## Example Output

```
SCAN     checking commonly used development ports
FOUND    :3000 PID 54321 (node) [5m] - npm start [~/projects/app]
FOUND    :5173 PID 55222 (vite) [2h] - vite dev [~/projects/site]
SKIP     :5432 PID 30120 (postgres) protected
ACTION   terminate 2 safe dev server process(es) [54321, 55222]? (y/N): y
STOP     PID 54321
STOP     PID 55222
STATS    terminated 2 process(es), 1 skipped
```

## Installation Options

### From Source (Recommended)

```bash
# Clone and install
git clone https://github.com/hugoev/zap.git
cd zap
make install

# Or manually
go install -ldflags "-X github.com/hugoev/zap/internal/version.Version=$(git describe --tags --always)" ./cmd/zap
```

### Update Existing Installation

```bash
# Easiest way
zap update

# Or from anywhere
go install github.com/hugoev/zap/cmd/zap@latest
```

## Configuration

Configuration is optional and stored at `~/.config/zap/config.json`. Settings update automatically based on your usage.

```json
{
  "protected_ports": [5432, 6379],
  "max_age_days_for_cleanup": 14,
  "exclude_paths": [],
  "auto_confirm_safe_actions": false
}
```

## Log Levels

| Code   | Meaning                               |
| ------ | ------------------------------------- |
| SCAN   | Discovery/search operation            |
| FOUND  | Candidate resource located            |
| SKIP   | Resource intentionally left untouched |
| ACTION | Confirmation prompt                   |
| STOP   | Process terminated                    |
| DELETE | Directory removed                     |
| OK     | Successful completion                 |
| FAIL   | Operation error                       |
| INFO   | Detailed information (verbose mode)   |
| STATS  | Summary statistics                    |

## The Problem

During development, common frustrations include:

- **Port conflicts**: "port already in use" because an old process is still running
- **Orphaned processes**: Closing a terminal doesn't always kill the underlying process
- **Disk bloat**: Accumulating gigabytes of `node_modules`, `.venv`, `.cache`, and build artifacts
- **Manual cleanup**: Wasting time hunting down processes and directories

zap solves these automatically.

## Features

### Port Process Management

- Scans common development ports (3000, 3001, 5173, 8000, 8080, etc.)
- Automatically identifies safe dev servers (Node, Vite, Python, Go, etc.)
- Prompts before terminating infrastructure (Postgres, Redis, Docker)
- Respects protected ports list
- Shows process runtime, command, and working directory

### Workspace Cleanup

- Auto-detects common project directories (Documents, Projects, Code, etc.)
- Finds stale dependency directories:
  - `node_modules`, `.venv`, `.cache`, `.gradle`, `.mypy_cache`
  - `__pycache__`, `.pytest_cache`, `target`, `dist`, `build`
  - `.next`, `.turbo`, `.nuxt`, `.output`
- Sorts by size (largest first)
- Respects exclusions and recent modifications
- Shows total space that can be reclaimed

### Safety First

- **Development servers**: Prompted for confirmation (or auto-terminate with `--yes`)
- **Infrastructure processes**: Always prompts (databases, Docker, etc.)
- **Protected ports**: Never terminated (configurable)
- **Recent directories**: Skipped automatically

## Requirements

- Go 1.21 or later (for building from source)
- macOS, Linux, or Windows with WSL

## License

[Add your license here]

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.
