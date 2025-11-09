# zap

Developer environment cleanup and port process management tool.

zap keeps your development machine clean and responsive by terminating orphaned dev servers, freeing ports, and removing stale dependency directories and build artifacts. It is designed to be safe, predictable, and automated, with optional interactive control and persistent user preferences.

## Overview

During development, it is common to:

- Start a dev server twice and receive a "port already in use" error
- Close a terminal window but leave the underlying process running
- Accumulate large node_modules, .venv, .cache, .gradle, or build directories across many projects
- Lose gigabytes of disk space without realizing it

These interruptions slow down workflows and clutter the system.

zap solves this by automatically detecting and terminating development-related processes holding ports, and by identifying and cleaning unused dependency/project directories.

## Features

### Port Process Cleanup

`zap ports`

- Scans for processes listening on commonly used development ports (e.g., 3000, 5173, 8000, 8080)
- Detects and handles related "+1" zombie hot-reload processes
- Automatically terminates known safe development servers (Node, Vite, Go run, Python reload, etc.)
- Prompts before terminating database, queue, and infrastructure processes (e.g., Postgres, Redis, Docker)
- Respects "protected" port list
- Allows persistent user preferences to avoid repeated prompts

### Workspace Cleanup

`zap cleanup`

- Searches for large dependency and cache directories such as:
  - node_modules, .venv, .cache, .gradle, .mypy_cache
  - **pycache**, .pytest_cache, target, dist, build
  - .next, .turbo, .nuxt, .output
- Presents size summaries before removing anything
- Allows exclusion of specific directories
- Automatically remembers user exclusions
- Reclaims wasted disk space efficiently

## Logging Output

zap uses structured, professional logging:

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

### Log Format:

| Code   | Meaning                               |
| ------ | ------------------------------------- |
| SCAN   | Discovery / search operation          |
| FOUND  | Candidate resource located            |
| SKIP   | Resource intentionally left untouched |
| ACTION | Confirmation prompt when required     |
| STOP   | Process termination event             |
| DELETE | Directory removal event               |
| OK     | Successful completion                 |
| FAIL   | Operation error                       |
| INFO   | Detailed information (verbose mode)   |
| STATS  | Summary statistics                    |

Colors are used to improve clarity (ANSI-compatible) but never required.

## Configuration

Configuration persists automatically as the user interacts with the tool.

Config file location:

```
~/.config/zap/config.json
```

Example:

```json
{
  "protected_ports": [5432, 6379],
  "max_age_days_for_cleanup": 14,
  "exclude_paths": ["~/work/critical/node_modules"],
  "auto_confirm_safe_actions": true
}
```

Users do not need to manually edit this file; settings update automatically based on interaction.

## Safety Rules

| Case                                                   | Behavior                 |
| ------------------------------------------------------ | ------------------------ |
| Development server detected on common port             | Automatically terminated |
| Database, cache server, or docker engine detected      | Prompt for confirmation  |
| Directory appears stale and matches known patterns     | Suggested for cleanup    |
| Directory recently modified or user-marked as excluded | Skipped silently         |

zap always explains its actions before executing irreversible operations unless the user has explicitly enabled non-interactive mode.

## Command Summary

| Command       | Description                                                   |
| ------------- | ------------------------------------------------------------- |
| `zap ports`   | Scan and terminate orphaned or conflict-causing dev processes |
| `zap cleanup` | Remove stale dependency/cache folders and reclaim disk space  |
| `git` | Display version and build metadata                            |

Optional flags:

- `--yes, -y` execute without confirmation where safe
- `--dry-run` show planned actions without making changes
- `--verbose, -v` show detailed progress and information

## Goals

- Safe defaults for new users
- Non-interactive mode for power users and CI systems
- Clear, minimal, professional command output
- Zero configuration required to get started
- Configuration that adapts to user behavior, not vice-versa

## Non-Goals

- No system-wide destructive cleanup
- No aggressive memory/process sweeping
- No GUI or daemon mode (CLI-first design)
- No assumption of specific frontend or backend tooling
