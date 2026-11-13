# Configuration

The server creates `{config_dir}/server.yml` on first startup and fills unset values with defaults. The YAML root sections are `server`, `web`, `web_robots`, and `web_security`.

```yaml
server:
  address: "[::]"
  port: "64080"
  baseurl: /
  api_version: v1
  mode: production
  database:
    driver: sqlite
web:
  ui:
    theme: dark
  cors: "*"
web_robots:
  allow: ["/", "/api"]
  deny: ["/debug"]
```

Review the generated file before changing values. It contains comments and computed defaults for optional capabilities.

## Configuration paths

`--config DIR` and `CONFIG_DIR` select the configuration directory. `--data DIR` and `DATA_DIR` select the data directory. `--cache DIR` / `CACHE_DIR`, `--log DIR` / `LOG_DIR`, and `--backup DIR` / `BACKUP_DIR` select their respective directories. `DATABASE_DIR` selects the database directory. The `--pid FILE` flag selects a PID file; PID files are skipped in containers.

The server chooses platform-appropriate defaults when paths are not supplied. The binary creates required directories at startup. See [Server Operations](SERVER.md) for runtime and service information.

## Server settings

Settings are grouped under `server` in `server.yml`:

| Group | Settings |
|-------|----------|
| Listener and routing | `port`, `fqdn`, `address`, `baseurl`, `api_version`, `daemonize`, `mode` |
| Runtime identity | `user`, `group`, `pidfile` |
| Branding and discovery | `branding`, `seo`, `admin` |
| TLS | `ssl.enabled`, `ssl.letsencrypt` (including challenge and DNS credentials) |
| Scheduling and protection | `schedule`, `rate_limit`, `maintenance`, `healthz.root.enabled` |
| Metrics and proxy trust | `metrics`, `trusted_proxies.additional` |
| Storage and logs | `database`, `logging` |
| Backups and notifications | `backup.encryption`, `notifications.webui`, `notifications.email` |
| Language | `i18n.enabled`, `default_language`, `fallback_language`, `available_languages`, `cookie_name`, `cookie_max_age` |
| Network integrations | `tor`, `geoip` |
| Updates | `update.branch`, `auto_install`, `defer_days` |

Email SMTP fields can also be supplied with `SMTP_HOST`, `SMTP_PORT`, `SMTP_USERNAME`, `SMTP_PASSWORD`, `SMTP_TLS`, `SMTP_FROM_NAME`, and `SMTP_FROM_EMAIL`. Secrets such as SMTP passwords, metrics tokens, and TLS DNS credentials should be kept private and must not be committed.

The `web` section controls the UI theme and CORS setting. `web_robots.allow` and `web_robots.deny` determine the generated `robots.txt` directives. `web_security.admin` supplies the security contact shown by `security.txt`; `web_security.cors` controls the web security CORS setting.

## Environment variables and precedence

Directory environment variables are read during startup. `PORT` and `LISTEN` override the listen port and address; `DOMAIN` and `HOSTNAME` influence FQDN discovery. SMTP variables override their corresponding values from `server.yml`.

For settings that have all three sources, the precedence is CLI flag, environment variable, config file, then built-in default. Init-only directory variables must be present before starting the process.

## Base URL and API version

`server.baseurl` (or `--baseurl`) sets a URL path prefix, for example `/gitignore`. A root prefix (`/`) serves the app at the origin root. `server.api_version` controls the version segment, which is `v1` by default.

## Update channel

`server.update.branch` accepts `stable`, `beta`, or `devel`. The aliases `daily`, `rolling`, `dev`, and `development` normalize to `devel`. `auto_install` controls whether scheduled update checks install a found update, and `defer_days` controls release age for scheduled checks.

## Optional endpoints

The canonical health page is `/server/healthz`. The root `/healthz` alias is enabled only with `server.healthz.root.enabled: true`. Prometheus metrics are enabled by default at `/metrics`; the endpoint can be changed with `server.metrics.endpoint`. Metrics should be firewalled from public access; an optional bearer token is available through `server.metrics.token`.
