# CLI Reference

The project ships two binaries: the `gitignore` server and the `gitignore-cli`
companion client.

## Server

```bash
gitignore [flags]
```

### Information

| Flag | Description |
|------|-------------|
| `--version` | Print version and exit |
| `--help` | Print help and exit |
| `--status` | Report server status |

### Shell Integration

| Flag | Description |
|------|-------------|
| `--shell completions [SHELL]` | Print shell completions |
| `--shell init [SHELL]` | Print the shell init one-liner |

Supported shells: `bash`, `zsh`, `fish`, `sh`, `dash`, `ksh`, `powershell`,
`pwsh`. The shell is auto-detected from `$SHELL` when omitted.

```bash
# bash
source <(gitignore --shell completions bash)
```

### Server Configuration

| Flag | Description |
|------|-------------|
| `--port` | Listen port |
| `--address` | Listen address |
| `--config` | Configuration directory |
| `--data` | Data directory |
| `--cache` | Cache directory |
| `--log` | Log directory |
| `--backup` | Backup directory |
| `--pid` | PID file path |
| `--baseurl` | Base URL path prefix |
| `--daemon` | Detach and run in the background (Unix only) |
| `--lang` | Set locale (`LANG`) for the process |
| `--mode` | Run mode |
| `--debug` | Enable debug logging and pprof |
| `--color` / `--force` | Output control |

### Service Management

| Flag | Description |
|------|-------------|
| `--service --install` | Install the system service |
| `--service --uninstall` | Remove the system service |
| `--maintenance` | Maintenance operations |
| `--update` | Self-update the binary |

## Client (`gitignore-cli`)

The companion client is designed for shell pipelines:

```bash
# Fetch a template
gitignore-cli go > .gitignore

# Combine several templates
gitignore-cli go node macos >> .gitignore
```

It targets the server URL from its configuration or the `--url` flag and honors
the same shell-completion integration.
