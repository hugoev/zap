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

# Install the binary (with version from git tags)
go install -ldflags "-X github.com/hugoev/zap/internal/version.Version=$(git describe --tags --always)" ./cmd/zap

# Or use the Makefile (recommended)
make install

# Add Go bin to your PATH (if not already there)
echo 'export PATH="$HOME/go/bin:$PATH"' >> ~/.zshrc  # or ~/.bashrc for bash
source ~/.zshrc  # or open a new terminal

# Verify installation
zap version
```

The binary will be installed to `~/go/bin/zap` (or `$GOPATH/bin/zap` if GOPATH is set).

### Updating

**Method 1: If you cloned the repository**

```bash
# Navigate to where you cloned zap
cd ~/path/to/zap  # or wherever you cloned it

# Pull the latest changes
git pull

# Reinstall the updated version (with version from git tags)
go install -ldflags "-X github.com/hugoev/zap/internal/version.Version=$(git describe --tags --always)" ./cmd/zap

# Or use Makefile
make install

# Verify the update
zap version
```

**Method 2: Update from anywhere (recommended)**

```bash
# This will download and install the latest version
go install github.com/hugoev/zap/cmd/zap@latest

# Verify the update
zap version
```

**Method 3: Using the update command (easiest)**

```bash
# Simply run the update command
zap update

# Verify the update
zap version
```

**Check your current version:**

```bash
zap version
```

## Quick Start

```bash
# Free up ports
zap ports

# Free up ports without prompts
zap ports --yes

# Clean up stale directories
zap cleanup

# See what would be cleaned (dry run)
zap cleanup --dry-run
```

## Features

### Port Process Management

- Scans common development ports (3000, 5173, 8000, 8080, etc.)
- Automatically identifies safe dev servers (Node, Vite, Python, Go, etc.)
- Prompts before terminating infrastructure (Postgres, Redis, Docker)
- Respects protected ports list
- Shows process runtime and details

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

## Usage

### Commands

| Command       | Description                           |
| ------------- | ------------------------------------- |
| `zap ports`   | Scan and free up ports                |
| `zap cleanup` | Remove stale dependency/cache folders |
| `zap version` | Display version information           |
| `zap update`  | Update zap to the latest version      |

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

zap works out of the box with sensible defaults. Configuration is optional and stored at `~/.config/zap/config.json`.

```json
{
  "protected_ports": [5432, 6379],
  "max_age_days_for_cleanup": 14,
  "exclude_paths": [],
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
