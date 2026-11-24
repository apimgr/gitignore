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

Commands use flags with values, for example `--service status`, `--service --install`, `--maintenance backup`, `--maintenance restore FILE`, `--update check`, and `--update branch stable`. `--i2p` enables the I2P eepsite; `--i2p=false` disables it for that run. Security report metadata requires root privileges or an interactive `server.token` prompt:

```sh
gitignore --maintenance security list [--status received]
gitignore --maintenance security show sec_0123456789abcdef
gitignore --maintenance security update sec_0123456789abcdef --status triaged --comment "Reproduced and under review"
gitignore --maintenance token list
gitignore --maintenance token revoke own_12345678 --yes
gitignore --maintenance data export --owner own_12345678 --output owner-export.json
gitignore --maintenance data delete --owner own_12345678 --yes
gitignore --maintenance compliance report
```

The security command never prints report bodies. `update` accepts `--status`, `--comment`, `--disclosure-date YYYY-MM-DD`, and `--severity`; statuses may only move forward or to `wont_fix`. Token and data commands require the configured operator token or root. Token listings contain prefixes and metadata only. Revoking a linked token asks for its prefix; revoking a primary token deletes its owner and all tokens after owner ID confirmation. Data export writes a mode `0600` JSON file by default; `--output -` writes JSON to stdout. Data deletion removes the owner and tokens after owner ID confirmation. `--yes` skips those prompts for scripts. `compliance report` prints settings and counts. `--shell completions [SHELL]` and `--shell init [SHELL]` print shell integration text. Supported shells include Bash, Zsh, Fish, POSIX shells, and PowerShell.

Server environment variables include `PORT`, `LISTEN`, `MODE`, `DOMAIN`, the `SMTP_*` settings, and the initialization-only directory variables `CONFIG_DIR`, `DATA_DIR`, `DATABASE_DIR`, `CACHE_DIR`, `LOG_DIR`, and `BACKUP_DIR`. `DEBUG` enables debug output and debug routes; `NO_COLOR` disables colored output. I2P also accepts `I2P_ENABLED`. See [Configuration](configuration.md) for precedence and details.

Manage the project OpenPGP keypair with the operator token or as root. Private-key import, export, rotation, and deletion require the confirmation prompts described by the command:

```sh
gitignore --maintenance pgp generate
gitignore --maintenance pgp rotate
gitignore --maintenance pgp publish
gitignore --maintenance pgp export public [PATH]
gitignore --maintenance pgp export private PATH
gitignore --maintenance pgp import PATH
gitignore --maintenance pgp delete
```

The private key remains encrypted in the configuration security directory. Generate, rotate, and import publish the public key automatically; failed keyserver submissions are retried by the scheduled maintenance task. Backups include the key files and keyserver publication state. `gitignore --maintenance backup test [ARCHIVE]` decrypts the archive as needed and verifies that the encrypted private key decrypts and matches its public key; it does not restore files. If `ARCHIVE` is omitted, the newest backup archive is selected. Encrypted archives prompt for the backup password. See [Security](security.md) for the key lifecycle and report encryption.

Backups include `server.yml`, `secrets.yml` when present, a consistent database snapshot, and existing `yaml.d/`, `assets/`, `template/`, `theme/`, and `security/` files. The daily scheduler writes a cumulative incremental against the previous full before writing the new daily full; the hourly task, when enabled, writes a cumulative incremental against the newest full. Incrementals are verified with their base and both are protected together by retention. Retention also classifies timestamped manual archives and unrecognized application backup names; its hard size cap can override count-based retention. Scheduled backups first run retention and skip with an audit event when free space is below twice the newest backup size or volume usage exceeds the configured disk threshold. Restore accepts either archive, verifies the incremental's named base and checksum in the same directory, applies changed files and deletions in staging, loads effective restore limits from the main config and YAML drop-ins, and checks the combined limits before installing anything. A first scheduled run has no daily incremental. A configured `server.backup.encryption_password` in `secrets.yml` encrypts backups without prompting; compliance mode rejects backups when that password is missing. Encrypted restore and backup-test operations prompt for the password.

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

The client supports `--server URL`, `--config NAME`, `--output text\|json\|table`, `--debug`, `--color auto\|yes\|no`, `--lang CODE`, `--version`, and shell integration flags. `GITIGNORE_SERVER_PRIMARY` sets the default server URL, and `NO_COLOR` disables colored output. The default server URL and profile settings come from the client configuration file. See [configuration](configuration.md).
