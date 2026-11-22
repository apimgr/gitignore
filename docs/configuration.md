# Configuration

On first startup, the server creates `{config_dir}/server.yml` and, as needed, area files under `{config_dir}/yaml.d/` plus `{config_dir}/secrets.yml`. Edit the generated files; the repository does not ship runtime configuration. Unknown or invalid values are handled by the validation rules in AI.md PART 12. Built-in defaults apply to settings that are not explicitly saved; area files are written only when needed for migration or an operator-driven change.

```yaml
# {config_dir}/server.yml — node-specific settings
server:
  address: "[::]"
  port: "64080" # example value; the generated default is a random available port in 64000–64999
  baseurl: /
  api_version: v1
  mode: production
  database:
    driver: sqlite
```

```yaml
# {config_dir}/yaml.d/pages.yml — public pages
server:
  pages:
    contact:
      enabled: true
      captcha: simple
    help:
      content: "Project-specific help content in Markdown."
      mcp: "Project-specific MCP usage guidance appended to the generated help page."
```

The merged view starts with built-in defaults, then reads `server.yml`, ordered `yaml.d/*.yml` drop-ins, and finally `secrets.yml`. Maps merge by key; later scalar or list values replace earlier ones. Environment variables and command-line flags override only the settings explicitly supported by the running binary; they are not a general override layer for every YAML setting. For example, `--port` overrides `PORT`, which overrides `server.port`, and `--address` overrides `LISTEN`, which overrides `server.address`. Configuration file monitoring and reload behavior follow the current AI.md configuration contract.

`server.yml`, `secrets.yml`, and each drop-in file use mode `0600`; the `yaml.d/` directory uses `0700`. The `secrets.yml` file holds credentials such as the operator token, encryption key, SMTP password, and CAPTCHA secret. The server generates required secrets and never returns them in API responses or prints them in full in configuration output.

`server.yml` contains node-specific settings. Area files are grouped under `yaml.d/`; the merge accepts any setting in any file, but the documented area file is its normal home. The canonical layout is:

| File | Settings |
|---|---|
| `server.yml` | Node identity and listener, database, cache, TLS, trusted proxies, and cluster settings |
| `yaml.d/branding.yml` | `server.branding`, `server.seo`, and `web.ui` |
| `yaml.d/web.yml` | `web.*` except `web.ui`, plus CORS, CSRF, URL detection, compression, health, and localization settings |
| `yaml.d/pages.yml` | `server.pages` |
| `yaml.d/contact.yml` | `server.contact` |
| `yaml.d/privacy.yml` | `server.privacy`, `server.compliance`, and `server.tracking` |
| `yaml.d/security.yml` | `server.security`, `server.rate_limit`, `server.geoip`, `server.limits`, `server.tokens`, and `data.*` |
| `yaml.d/logs.yml` | `server.logs` |
| `yaml.d/metrics.yml` | `server.metrics` |
| `yaml.d/notifications.yml` | `server.notifications` |
| `yaml.d/tor.yml` | `server.tor` and top-level `tor` settings |
| `yaml.d/i2p.yml` | `server.i2p` |
| `yaml.d/maintenance.yml` | Maintenance, scheduler, update, backup, and restore settings |
| `yaml.d/app.yml` | Project-defined settings not assigned to another area |
| `secrets.yml` | Credentials only |

If a legacy single-file `server.yml` contains area settings or credentials, startup migrates those values to their designated files.

## Configuration paths

The server uses OS-appropriate paths unless overridden. Directory flags and environment variables are read at startup:

| Purpose | Flag | Environment variable |
|---|---|---|
| Configuration | `--config DIR` | `CONFIG_DIR` |
| Data | `--data DIR` | `DATA_DIR` |
| Cache | `--cache DIR` | `CACHE_DIR` |
| Logs | `--log DIR` | `LOG_DIR` |
| Backups | `--backup DIR` | `BACKUP_DIR` |
| Database | — | `DATABASE_DIR` |
| PID file | `--pid FILE` | — |

PID files are skipped in containers. See [Server Operations](SERVER.md) for lifecycle and runtime behavior.

## Settings by area

All server settings are nested under `server` unless noted.

| Area | Supported settings |
|---|---|
| Listener and identity | `server.port`, `server.fqdn`, `server.address`, `server.baseurl`, `server.api_version`, `server.daemonize`, `server.mode`, `server.user`, `server.group`, `server.pidfile` |
| Branding and SEO | `server.branding.*`, `server.seo.*`, and `web.ui.*` |
| TLS and web security | `server.ssl.*`, `server.trusted_proxies.*`, and `web.*` |
| Database and cache | `server.database.*` and `server.cache.*` |
| Scheduler and limits | `server.scheduler.*`, `server.rate_limit.*`, `server.limits.*`, and `server.healthz.*` |
| Metrics and logs | `server.metrics.*` and `server.logs.*` |
| Maintenance and backup | `server.maintenance.*`, `server.backup.*`, `server.restore.*`, and `server.update.*` |
| Owner tokens | `server.tokens.max_linked`, `cookie_max_age`, and `idle_purge_days` |
| Localization | `server.i18n.*` |
| Tor and I2P | `server.tor.*`, top-level `tor.*`, and `server.i2p.*` |
| GeoIP and blocklists | `server.geoip.*`, `server.security.blocklists.*`, `server.security.allowlist`, `server.security.blocked_ips`, and `data.*` |
| Contact and pages | `server.contact.*` and `server.pages.*` |
| Privacy and compliance | `server.privacy.*`, `server.compliance.*`, and `server.tracking.*` |
| Notifications | `server.notifications.*` |
| Project settings | Project-specific settings belong in `yaml.d/app.yml` |

The table lists every persisted setting group. Generated settings include their defaults and validation guidance. `server.token` and runtime-derived `notifications.email.enabled` are not persisted settings. Database and cache connection credentials, SMTP credentials, CAPTCHA secrets, metrics service tokens, CVE API key, and encryption key belong in the protected `secrets.yml` file. Metrics tokens use `server.metrics.auth.tokens.prometheus`, `.grafana`, and `.loki`.

The canonical area-file assignments and all accepted settings are specified in AI.md PART 5 and PART 12. The server reads the SMTP environment variables listed there for mail transport. Keep credentials and private key material out of source control and logs.

## Contact form and security reports

The contact page is enabled by default and uses the built-in `simple` challenge. It works without JavaScript. External `recaptcha` and `hcaptcha` providers require their site and secret keys. Configure a recipient under `server.contact.general.email` or the appropriate role-specific contact setting and configure SMTP before expecting message delivery.

The privacy preferences page only displays the CCPA opt-out control when `server.privacy.data.sold` is `true`. Cookie-consent categories and the CCPA choice stay in browser cookies; they are not included in cross-device preference sync.

Abuse, DMCA, and privacy reports use the public contact form. Security reports are available only through a valid rotating `security_id` advertised by the security contact endpoints. Reports are encrypted before persistence. The server creates the encryption key in `secrets.yml`; configure the security report retention policy in its documented area file. See [Security](security.md) for the reporting workflow.

## Environment variables and precedence

The server reads the following environment variables:

| Variable | Purpose |
|---|---|
| `CONFIG_DIR`, `DATA_DIR`, `CACHE_DIR`, `LOG_DIR`, `BACKUP_DIR` | Runtime directory overrides; matching directory flags take precedence |
| `DATABASE_DIR` | Runtime override for the SQLite database directory |
| `NO_COLOR`, `TERM` | Control terminal color and terminal mode |
| `DOMAIN`, `HOSTNAME` | Set or contribute to FQDN resolution; `DOMAIN` has highest priority |
| `MODE`, `DEBUG` | Select application mode and debug behavior; command-line flags take precedence |
| `PORT`, `LISTEN` | First-run listener defaults; `--port` and `--address` take precedence |
| `APPLICATION_NAME`, `APPLICATION_TAGLINE` | First-run application title and description |
| `DATABASE_DRIVER`, `DATABASE_URL` | First-run database connection; the URL scheme selects the driver unless SQLite needs `DATABASE_DRIVER` |
| `CACHE_URL` | First-run cache connection string |
| `I2P_ENABLED` | I2P override; explicit `--i2p` takes precedence |
| `SMTP_HOST`, `SMTP_PORT`, `SMTP_USERNAME`, `SMTP_PASSWORD`, `SMTP_FROM_NAME`, `SMTP_FROM_EMAIL`, `SMTP_TLS` | Configure SMTP transport and sender defaults |

`PORT` and `LISTEN` override listener settings. `DOMAIN` and `HOSTNAME` contribute to FQDN discovery. SMTP environment variables override matching SMTP configuration values. Configuration files are merged in this order:

1. Embedded defaults
2. `server.yml`
3. `yaml.d/*.yml` drop-ins in lexical order
4. `secrets.yml`

Supported startup overrides are then applied for the individual settings documented above. For listener settings, the command-line flag wins over its environment variable, which wins over the merged configuration.

Directory variables are read at every startup. `PORT`, `LISTEN`, `APPLICATION_NAME`, `APPLICATION_TAGLINE`, `DATABASE_DRIVER`, `DATABASE_URL`, and `CACHE_URL` seed configuration only on first run; after the connection values are saved, later environment changes do not replace them. See AI.md PART 5 for exact precedence and URL credential handling.

### I2P eepsite

I2P is enabled by the computed default in containers and disabled by default on bare metal and in VMs. Explicit precedence is `--i2p` / `--i2p=false`, `I2P_ENABLED`, `server.i2p.enabled` in `yaml.d/i2p.yml`, then the computed default. The image does not set `I2P_ENABLED`.

```yaml
server:
  i2p:
    enabled: false
    binary: ""
    sam_address: "127.0.0.1:7656"
    virtual_port: 80
    inbound_length: 3
    outbound_length: 3
    inbound_quantity: 5
    outbound_quantity: 5
    signature_type: 7
    bootstrap_timeout: 300
```

When enabled, the app prefers a managed dedicated `i2pd` process and falls back to an existing SAMv3 bridge. It creates a separate loopback backend and persists the destination key under `{data_dir}/i2p/site/`; the `.b32.i2p` address remains stable across restarts. No provider means the eepsite is skipped without preventing the main server from starting. `/server/healthz` reports its enabled state, provider, status, and public address.

## API version and base URL

`server.api_version` sets the canonical API version segment (`v1` by default). `server.baseurl` or `--baseurl` places the service below a URL prefix, such as `/gitignore`. The root prefix is `/`. Discovery at `/api/autodiscover` reports the effective version and base path.

## Update channel and optional endpoints

`server.update.branch` accepts `stable`, `beta`, or `devel`; `daily`, `rolling`, `dev`, and `development` normalize to `devel`. `auto_install` controls whether scheduled update checks install a found version, while `defer_days` controls release age for scheduled checks.

`/server/healthz` is always available. The root `/healthz` alias is disabled unless `server.healthz.root.enabled` is true. Metrics use `/server/metrics` and its service-specific Prometheus, Grafana, and Loki routes. Root `/metrics` aliases are controlled by `server.metrics.root.enabled`; each service has a separate bearer token in `server.metrics.auth.tokens.*`. Restrict these internal endpoints to trusted monitoring systems. Language detection follows the request's supported locale and the `lang` preference; the explicit language control is on `/server/preferences`.

Health responses use cached component and dataset checks. Core probes and loaded datasets refresh on a schedule, and relevant data reloads refresh the cache immediately. NVD/CVE health is included as `checks.data_cve_nvd`; `checks.data_trivy_db` appears only when Trivy database support is enabled. Individual dataset states are `ok`, `degraded`, or `error`; the public response contains no paths or error details.
