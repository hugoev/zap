# zap

Developer environment cleanup and port process management tool.

## The Problem

During development, common frustrations include:

- **Port conflicts**: Starting a dev server only to see "port already in use" because an old process is still running
- **Orphaned processes**: Closing a terminal window doesn't always kill the underlying process, leaving servers running in the background
- **Disk bloat**: Accumulating gigabytes of `node_modules`, `.venv`, `.cache`, and build artifacts across dozens of projects
- **Manual cleanup**: Manually hunting down processes and directories wastes time

These interruptions slow down workflows and clutter your system.

## The Solution

zap automatically detects and terminates orphaned development processes, and identifies stale dependency directories for cleanup. It's safe, predictable, and respects your preferences.

## Installation

### Prerequisites

- Go 1.21 or later
- macOS, Linux, or Windows with WSL

### Install from Source

```bash
# Clone the repository
git clone https://github.com/hugoev/zap.git
cd zap

# Install the binary
go install ./cmd/zap

# Add Go bin to your PATH (if not already there)
echo 'export PATH="$HOME/go/bin:$PATH"' >> ~/.zshrc  # or ~/.bashrc for bash
source ~/.zshrc  # or open a new terminal

# Verify installation
zap version
```

The binary will be installed to `~/go/bin/zap` (or `$GOPATH/bin/zap` if GOPATH is set).

## Quick Start

### Free up ports

```bash
# Scan and see what's running
zap ports

# Auto-terminate safe dev servers without prompts
zap ports --yes

# See what would be terminated (dry run)
zap ports --dry-run
```

### Clean up stale directories

```bash
# Find stale dependency directories
zap cleanup --dry-run

# Remove them (with confirmation)
zap cleanup

# Auto-remove without prompts
zap cleanup --yes
```

## Features

### Port Process Management

- Scans common development ports (3000, 5173, 8000, 8080, etc.)
- Automatically identifies safe dev servers (Node, Vite, Python, Go, etc.)
- Prompts before terminating infrastructure (Postgres, Redis, Docker)
- Respects protected ports list
- Shows process runtime and details

### Workspace Cleanup

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

## Usage

### Commands

| Command       | Description                               |
| ------------- | ----------------------------------------- |
| `zap ports`   | Scan and terminate orphaned dev processes |
| `zap cleanup` | Remove stale dependency/cache folders     |
| `zap version` | Display version information               |

### Flags

| Flag              | Description                                 |
| ----------------- | ------------------------------------------- |
| `--yes`, `-y`     | Execute without confirmation where safe     |
| `--dry-run`       | Show planned actions without making changes |
| `--verbose`, `-v` | Show detailed progress and information      |

### Example Output

```
SCAN     checking commonly used development ports
FOUND    :3000 PID 54321 (node) [5m]
FOUND    :5173 PID 55222 (vite) [2h]
SKIP     :5432 PID 30120 (postgres) protected
ACTION   terminate 2 safe dev server process(es) [54321, 55222]? (y/N): y
STOP     PID 54321
STOP     PID 55222
STATS    terminated 2 process(es), 1 skipped
```

## Configuration

Configuration is stored at `~/.config/zap/config.json` and persists automatically.

```json
{
  "protected_ports": [5432, 6379],
  "max_age_days_for_cleanup": 14,
  "exclude_paths": ["~/work/critical/node_modules"],
  "auto_confirm_safe_actions": false
}
```

Settings update automatically based on your interactions—no manual editing required.

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

Colors are used for clarity (ANSI-compatible) but never required.

## License

[Add your license here]

## Contributing

[Add contribution guidelines if applicable]
