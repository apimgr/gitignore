# Configuration

On first startup, the server creates `{config_dir}/server.yml` and, as needed, the area files under `{config_dir}/yaml.d/` plus `{config_dir}/secrets.yml`. Edit the generated files; the repository does not ship runtime configuration. Unknown or invalid values are handled by the validation rules in AI.md PART 12. The generated files contain the complete settings and comments supported by the running build.

```yaml
# {config_dir}/server.yml — node-specific settings
server:
  address: "[::]"
  port: "64080"
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

The merged view starts with built-in defaults, then reads `server.yml`, ordered `yaml.d/*.yml` drop-ins, and finally `secrets.yml`. Command-line flags and environment variables have higher precedence. Maps merge by key; later scalar or list values replace earlier ones. The server watches these files for changes and reloads valid settings.

`server.yml`, `secrets.yml`, and each drop-in file use mode `0600`; the `yaml.d/` directory uses `0700`. The `secrets.yml` file holds credentials such as the operator token, encryption key, SMTP password, and CAPTCHA secret. The server generates required secrets and never returns them in API responses or prints them in full in configuration output.

`server.yml` contains node-specific settings. Area files are grouped under `yaml.d/`: `branding.yml`, `web.yml`, `pages.yml`, `contact.yml`, `privacy.yml`, `security.yml`, `logs.yml`, `metrics.yml`, `notifications.yml`, `tor.yml`, `i2p.yml`, `maintenance.yml`, and `app.yml`. The merge uses the same dotted setting names in any file, but the documented area file is each setting's normal home. If a legacy single-file `server.yml` contains area settings or credentials, startup migrates those values to their designated files.

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

| Area | Representative settings |
|---|---|
| Listener and routing | `address`, `port`, `fqdn`, `baseurl`, `api_version`, `mode`, `daemonize` |
| Runtime identity | `user`, `group`, `pidfile` |
| Branding and SEO | `branding`, `seo`, `admin` |
| TLS | `ssl.enabled`, `ssl.letsencrypt` and its challenge/provider settings |
| Database and logs | `database`, `logging` |
| Scheduling and protection | `schedule`, `rate_limit`, `maintenance`, `healthz.root.enabled` |
| Metrics and trusted proxies | `metrics`, `trusted_proxies.additional` |
| Backup and notifications | `backup`, `notifications.webui`, `notifications.email` |
| Localization | `i18n.enabled`, `i18n.default_language`, `i18n.fallback_language`, `i18n.available_languages`, `i18n.cookie_name`, `i18n.cookie_max_age` |
| Network services | `tor`, `geoip` |
| Contact recipients | `contact.general.email`, `contact.security.email`, `contact.abuse.email` |
| Contact page | `pages.contact.enabled`, `pages.contact.captcha`, `pages.contact.captcha_site_key`, `pages.contact.captcha_secret_key`, `pages.contact.success_message` |
| Help pages | `pages.help.content`, `pages.help.mcp` |
| Security reports | `server.security.reports.retention_days` and `web.security` disclosure settings; encryption credentials belong in `secrets.yml` |
| Updates | `update.branch`, `update.auto_install`, `update.defer_days` |
| Browser behavior | `web.ui.theme`, `web.cors`, `web_robots.allow`, `web_robots.deny`, `web_security.admin`, `web_security.cors` |

The server reads `SMTP_HOST`, `SMTP_PORT`, `SMTP_USERNAME`, `SMTP_PASSWORD`, `SMTP_TLS`, `SMTP_FROM_NAME`, and `SMTP_FROM_EMAIL` for mail transport. Keep credentials and private key material out of source control and logs.

## Contact form and security reports

The contact page is enabled by default and uses the built-in `simple` challenge. It works without JavaScript. External `recaptcha` and `hcaptcha` providers require their site and secret keys. Configure a recipient under `server.contact.general.email` or the appropriate role-specific contact setting and configure SMTP before expecting message delivery.

The privacy preferences page only displays the CCPA opt-out control when `server.privacy.data.sold` is `true`. Cookie-consent categories and the CCPA choice stay in browser cookies; they are not included in cross-device preference sync.

Abuse, DMCA, and privacy reports use the public contact form. Security reports are available only through a valid rotating `security_id` advertised by the security contact endpoints. Reports are encrypted before persistence. The server creates the encryption key in `secrets.yml`; configure the security report retention policy in its documented area file. See [Security](security.md) for the reporting workflow.

## Environment variables and precedence

`PORT` and `LISTEN` override listener settings; `DOMAIN` and `HOSTNAME` contribute to FQDN discovery. SMTP environment variables override matching SMTP configuration values. Where all sources exist, the precedence is:

1. Command-line flag
2. Environment variable
3. `server.yml`
4. Embedded default

Directory variables are initialization inputs and must be set before startup.

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

`/server/healthz` is always available. The root `/healthz` alias is disabled unless `server.healthz.root.enabled` is true. Prometheus metrics use `/metrics` by default and can be moved with `server.metrics.endpoint`; restrict the endpoint to trusted monitoring systems and configure `server.metrics.token` when needed. Language detection follows the request's supported locale and the `lang` preference; the explicit language control is on `/server/preferences`.
