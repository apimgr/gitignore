# Configuration

The server reads a YAML config file at startup and applies environment and CLI
overrides on top of it.

## Config File

Default location: `{config_dir}/server.yml`. The config directory is resolved
from `--config`, the `CONFIG_DIR` environment variable, or a platform default
(system path when privileged, XDG path otherwise).

```yaml
server:
  address: 0.0.0.0
  port: 8080
  # Base URL path prefix the app is mounted under. "/" means the root.
  baseurl: /
  # REST API version segment used in /api/{version}/ routes.
  api_version: v1
  # Detach from the controlling terminal at startup (Unix only).
  daemonize: false

database:
  type: sqlite
  # Path auto-detected from the data/database directory when unset.
```

## Directory Flags

The server creates each directory it is given at startup. Directory permissions
are locked at process start from the effective UID: root uses `0755`
directories / `0644` files, an unprivileged user uses `0700` / `0600`.

| Flag | Environment | Purpose |
|------|-------------|---------|
| `--config` | `CONFIG_DIR` | Configuration directory |
| `--data` | `DATA_DIR` | Application data directory |
| (n/a) | `DATABASE_DIR` | Database directory (defaults under data) |
| `--cache` | `CACHE_DIR` | Cache directory |
| `--log` | `LOG_DIR` | Log directory |
| `--backup` | `BACKUP_DIR` | Backup directory |
| `--pid` | (n/a) | PID file path (skipped in containers) |

## Init-Only Environment Variables

These are read once at startup and cannot be changed at runtime:

```bash
CONFIG_DIR    # configuration directory
DATA_DIR      # data directory
DATABASE_DIR  # database directory
CACHE_DIR     # cache directory
LOG_DIR       # log directory
BACKUP_DIR    # backup directory
PORT          # listen port
LISTEN        # listen address
```

## Precedence

For each setting the order of precedence is: CLI flag > environment variable >
config file > built-in default.

## Base URL

When `server.baseurl` (or `--baseurl`) is set to a non-root path such as
`/gitignore`, the server transparently strips the prefix from incoming requests
and redirects the bare prefix to the trailing-slash form.
