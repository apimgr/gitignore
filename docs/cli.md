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
```

The security command never prints report bodies. `update` accepts `--status`, `--comment`, `--disclosure-date YYYY-MM-DD`, and `--severity`; statuses may only move forward or to `wont_fix`. `--shell completions [SHELL]` and `--shell init [SHELL]` print shell integration text. Supported shells include Bash, Zsh, Fish, POSIX shells, and PowerShell.

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

Backups include `server.yml`, `secrets.yml` when present, the SQLite database, and existing `yaml.d/`, `assets/`, `template/`, `theme/`, and `security/` files. Restore verifies the archive manifest and checksums before installing files. A configured `server.backup.encryption_password` in `secrets.yml` encrypts backups without prompting; compliance mode rejects backups when that password is missing. Encrypted restore and backup-test operations prompt for the password.

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
