# CLI Reference

The project ships a server binary named `gitignore` and a companion client named `gitignore-cli`. Use `--help` on either binary for the flags supported by that build.

## Server

```sh
gitignore [flags]
```

| Flag | Description |
|------|-------------|
| `--help`, `-h` | Print help |
| `--version`, `-v` | Print version and build information |
| `--status` | Check server health |
| `--port PORT` | Override the listen port |
| `--address ADDR` | Override the listen address |
| `--config DIR` | Set the configuration directory |
| `--data DIR` | Set the data directory |
| `--cache DIR` | Set the cache directory |
| `--log DIR` | Set the log directory |
| `--backup DIR` | Set the backup directory |
| `--pid FILE` | Set the PID file path |
| `--baseurl PATH` | Set the URL path prefix |
| `--mode production\|development` | Select the application mode |
| `--debug` | Enable debug endpoints and debug output |
| `--daemon` | Detach on supported Unix platforms |
| `--lang CODE` | Set the process locale |
| `--color auto\|yes\|no` | Select colored output |
| `--force`, `-y` | Skip confirmation prompts where supported |

Commands use flags with values, for example `--service status`, `--service --install`, `--maintenance backup`, `--maintenance restore FILE`, `--update check`, and `--update branch stable`. `--shell completions [SHELL]` and `--shell init [SHELL]` print shell integration text. Supported shells include Bash, Zsh, Fish, POSIX shells, and PowerShell.

## Client (`gitignore-cli`)

With no arguments, the client selects its interactive interface based on the terminal. Bare template names compose templates directly:

```sh
gitignore-cli Go Node > .gitignore
```

Commands include:

| Command | Purpose |
|---------|---------|
| `list` | List template names |
| `search QUERY` | Search template names and tags |
| `categories` | List categories |
| `category NAME` | List templates in a category |
| `get NAME` or `template NAME` | Print one template |
| `combine NAME...` | Combine templates |
| `stats` | Show catalog statistics |

The client supports `--server URL`, `--config NAME`, `--output text\|json\|table`, `--debug`, `--color auto\|yes\|no`, `--lang CODE`, `--version`, and shell integration flags. The default server URL and profile settings come from the client configuration file. See [configuration](configuration.md).
