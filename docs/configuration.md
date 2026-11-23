# Configuration

On first startup, the server creates `{config_dir}/server.yml` and `{config_dir}/secrets.yml` when needed. It creates area files under `{config_dir}/yaml.d/` only when a migration or an operator workflow writes settings. Built-in defaults apply to settings that are not explicitly saved. Invalid values are replaced with their defaults and logged; an invalid setting does not prevent startup.

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

The merged view starts with built-in defaults, then reads `server.yml`, drop-ins in byte-wise filename order, and finally `secrets.yml`. Maps merge recursively by key; later scalar or list values replace earlier ones, and lists are never appended. An explicit `null` is invalid and resets that key to its default. A malformed file is ignored; its last good copy stays active, or defaults are used if there is no last good copy. Invalid values produce one warning naming the file, line, and key, then reset to the default. Configuration errors never prevent startup. Environment variables and command-line flags override only the settings explicitly supported by the running binary; they are not a general override layer for every YAML setting. For listener settings, `--port` overrides `PORT`, which overrides `server.port`, and `--address` overrides `LISTEN`, which overrides `server.address`.

`server.yml`, `secrets.yml`, and each drop-in file use mode `0600`; the `yaml.d/` directory uses `0700`. The `secrets.yml` file holds credentials such as the operator token, encryption key, SMTP password, and CAPTCHA secret. The server generates required secrets and never returns them in API responses or prints them in full in configuration output.

`server.yml` contains node-specific settings. Area files are grouped under `yaml.d/`; the merge accepts any setting in any file, but the documented area file is its canonical home. The canonical layout is:

| File | Settings |
|---|---|
| `server.yml` | `server.port`, `server.address`, `server.fqdn`, `server.mode`, `server.api_version`, `server.baseurl`, `server.pidfile`, `server.daemonize`, `server.debug`, `server.database`, `server.cache`, `server.ssl`, and `server.trusted_proxies` |
| `yaml.d/branding.yml` | `server.branding`, `server.seo`, and `web.ui` |
| `yaml.d/web.yml` | `web.*` except `web.ui`, plus `server.cors`, `server.csrf`, `server.url_detection`, `server.compression`, `server.healthz`, and `server.i18n` |
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

All paths below are dotted YAML keys. Defaults are the built-in values; settings need not be written unless you want to change them. Unless a row says otherwise, invalid values produce one warning and reset to the default without stopping startup. The canonical file column follows the area-file map above.

### Listener, TLS, database, and cache

| Key | File | Default | Accepted values and validation |
|---|---|---|---|
| `server.port` | `server.yml` | Random available port in `64000–64999` (container default: `80`) | `1–65535`; invalid values are replaced by a new random port. A change requires restart. |
| `server.address` | `server.yml` | `"[::]"` | Listener address; invalid values reset to the default. |
| `server.fqdn` | `server.yml` | Detected host name | Host name; `DOMAIN` has highest hostname-resolution priority. |
| `server.mode` | `server.yml` | `production` | `production`, `development`, or `debug` (aliases are documented under Environment variables). |
| `server.debug.pprof` | `server.yml` | `true` in the debug configuration example | Boolean; effective only while the debug flag is enabled. |
| `server.debug.log_queries` | `server.yml` | `true` in the debug configuration example | Boolean; logs SQL queries while debug is enabled. |
| `server.debug.log_cache` | `server.yml` | `true` in the debug configuration example | Boolean; logs cache operations while debug is enabled. |
| `server.debug.log_bodies` | `server.yml` | `false` in the debug configuration example | Boolean; logs request/response bodies while debug is enabled. |
| `server.debug.max_body_log_size` | `server.yml` | `10KB` in the debug configuration example | Maximum body size written to debug logs. |
| `server.debug.block_profile_rate` | `server.yml` | `1` in the debug configuration example | Non-negative profiling rate; `0` disables block profiling. |
| `server.debug.mutex_profile_fraction` | `server.yml` | `1` in the debug configuration example | Non-negative profiling fraction; `0` disables mutex profiling. |
| `server.debug.runtime_endpoints` | `server.yml` | `true` in the debug configuration example | Boolean; effective only while the debug flag is enabled. |
| `server.api_version` | `server.yml` | `v1` | API version path segment. |
| `server.baseurl` | `server.yml` | `/` | URL path prefix; reverse-proxy prefix headers take precedence. |
| `server.user`, `server.group` | `server.yml` | Automatically selected | System account/group names; privilege behavior is platform-specific. |
| `server.pidfile` | `server.yml` | `true` | Boolean. PID files are skipped in containers. |
| `server.daemonize` | `server.yml` | `false` | Boolean; changing it requires restart. |
| `server.ssl.enabled` | `server.yml` | `false` | Boolean; controls TLS on the bound listener. |
| `server.ssl.cert`, `server.ssl.key` | `server.yml` | Empty (auto-detect) | Optional certificate and key paths. |
| `server.ssl.min_version` | `server.yml` | `TLS1.2` | `TLS1.2` or `TLS1.3`. |
| `server.ssl.letsencrypt.enabled` | `server.yml` | `false` | Boolean. |
| `server.ssl.letsencrypt.email` | `server.yml` | `admin@{fqdn}` | ACME account email address. |
| `server.ssl.letsencrypt.challenge` | `server.yml` | `http-01` | `http-01`, `tls-alpn-01`, or `dns-01`. |
| `server.ssl.letsencrypt.staging` | `server.yml` | `false` | Boolean. |
| `server.ssl.letsencrypt.dns_provider` | `server.yml` | Empty | Lowercase lego provider identifier. |
| `server.ssl.letsencrypt.dns_credentials` | `secrets.yml` | `{}` | Flat map of provider environment-variable names to string values; encrypted at rest. lego validates provider requirements at startup and before issuance. |
| `server.database.driver` | `server.yml` | `sqlite` | `sqlite` (aliases `sqlite2`, `sqlite3`) or `libsql` (alias `turso`). A connection URL's scheme selects the driver. |
| `server.database.url` | `server.yml` or `secrets.yml` | `{data_dir}/db/server.db` | SQLite path/`file:`, or `libsql://`/`https://`. Credential-bearing URLs are stored in `secrets.yml`; rejected URL forms are reported and are not replaced with SQLite. |
| `server.database.token` | `secrets.yml` | Empty | libsql credential supplied by the operator. |
| `server.database.pool.max_open` | `server.yml` | The configuration example uses `5`; the pool-sizing table calls `25` the small-deployment default (default unresolved) | Positive connection count; for SQLite this sizes the read pool, while the write pool is fixed at one. |
| `server.database.pool.max_idle` | `server.yml` | The configuration example uses `2`; the pool-sizing table calls `5` the small-deployment default (default unresolved) | Maximum idle connections; must be compatible with `max_open`. |
| `server.database.pool.max_lifetime` | `server.yml` | `5m` in the configuration example | Duration before a connection is retired. |
| `server.database.pool.max_idle_time` | `server.yml` | `1m` in the configuration example | Duration an idle connection may remain open. |
| `server.database.cache.enabled` | `server.yml` | `true` in the local-cache example; default not stated | Boolean; local SQLite cache for remote libsql/Turso. |
| `server.database.cache.path` | `server.yml` | `${DATA_DIR}/db/cache.db` in the example | Local SQLite cache path. |
| `server.database.cache.sync_interval` | `server.yml` | `30s` in the example | Duration between remote synchronization. |
| `server.database.cache.offline_mode` | `server.yml` | `true` in the example | Boolean; enables read-only cached operation when the remote database is unavailable. |
| `server.database.cache.max_age` | `server.yml` | `1h` in the example | Maximum cache age before forcing a remote read. |
| `server.cache.type` | `server.yml` | `memory` | `none`, `memory`, `valkey`, or `redis`. |
| `server.cache.url` | `server.yml` or `secrets.yml` | Empty | `valkey://`, `valkeys://`, `redis://`, or `rediss://`; credential-bearing URLs go in `secrets.yml`. If set, the URL takes precedence over individual connection fields. |
| `server.cache.host` | `server.yml` | `localhost` | Host name or address; used when `url` is empty. |
| `server.cache.port` | `server.yml` | `6379` | Cache service port. |
| `server.cache.username` | `server.yml` | Empty | Optional ACL user. |
| `server.cache.password` | `secrets.yml` | Empty | Optional credential. |
| `server.cache.db` | `server.yml` | `0` | Cache database number. |
| `server.cache.tls` | `server.yml` | `false` | Boolean; a TLS URL scheme also enables TLS. |
| `server.cache.tls_skip_verify` | `server.yml` | `true` when TLS is on | Boolean; set false to verify the certificate chain and host name. |
| `server.cache.tls_ca`, `server.cache.tls_cert`, `server.cache.tls_key` | `server.yml` | Empty | Optional file names inside `{config_dir}/ssl/`. |
| `server.cache.pool_size`, `server.cache.min_idle` | `server.yml` | `10`, `2` | Connection-pool counts. |
| `server.cache.timeout` | `server.yml` | `5s` | Duration. |
| `server.cache.prefix` | `server.yml` | `"{project_name}:"` | Key prefix. |
| `server.cache.ttl` | `server.yml` | `1h` | Duration for default cache entries. |
| `server.trusted_proxies.additional` | `server.yml` | `[]` | IPs, CIDRs, or DNS names; DNS names refresh every five minutes. Private and loopback ranges are trusted automatically. |
| `server.trusted_proxies.client_ip_header` | `server.yml` | `auto` | `auto` or one supported pinned client-IP header name. |

### HTTP, browser, discovery, and localization

| Key | File | Default | Accepted values and validation |
|---|---|---|---|
| `server.limits.max_body_size` | `yaml.d/security.yml` | `10MB` | Positive size with a unit. |
| `server.limits.read_timeout` | `yaml.d/security.yml` | `30s` | Positive duration with a unit. |
| `server.limits.write_timeout` | `yaml.d/security.yml` | `30s` | Positive duration with a unit. |
| `server.limits.idle_timeout` | `yaml.d/security.yml` | `120s` | Positive duration with a unit. Timeout changes use a graceful server reload. |
| `server.compression.enabled` | `yaml.d/web.yml` | `true` | Boolean. |
| `server.compression.level` | `yaml.d/web.yml` | `5` | Integer from `1` through `9`. |
| `server.compression.types` | `yaml.d/web.yml` | `text/html`, `text/css`, `text/javascript`, `application/json`, `application/xml` | List of MIME types. |
| `server.cors.allowed_origins` | `yaml.d/web.yml` | `["*"]` | Origin list; `["*"]` allows any origin without credentials, `[""]` disables CORS headers, or list explicit origins. |
| `server.cors.allow_credentials` | `yaml.d/web.yml` | `true` | Boolean; credentials are emitted only for explicitly listed matching origins, never with `*`. |
| `server.cors.max_age` | `yaml.d/web.yml` | `86400` | Preflight cache age in seconds. |
| `server.csrf.enabled` | `yaml.d/web.yml` | `true` | Boolean; disable only for API-only deployments without browser forms. CSRF applies to mutating frontend routes, not API routes. |
| `server.csrf.token_length` | `yaml.d/web.yml` | `32` | Token length in bytes. |
| `server.csrf.cookie_name` | `yaml.d/web.yml` | `csrf_token` | Cookie name. |
| `server.csrf.header_name` | `yaml.d/web.yml` | `X-CSRF-Token` | Header name. |
| `server.csrf.secure` | `yaml.d/web.yml` | `auto` | `auto`, `true`, or `false`; `auto` follows HTTPS detection. |
| `server.csrf.exempt_paths` | `yaml.d/web.yml` | `[]` | Frontend path glob patterns only. Backend routes are outside CSRF scope. |
| `server.healthz.root.enabled` | `yaml.d/web.yml` | `false` | Boolean; toggles the direct `/healthz` alias. |
| `server.url_detection.learning` | `yaml.d/web.yml` | `true` | Boolean; enables learning from reverse-proxy host patterns. |
| `server.url_detection.min_samples` | `yaml.d/web.yml` | `3` | Minimum request samples before inferring a wildcard domain. |
| `server.url_detection.sample_window` | `yaml.d/web.yml` | `5m` | Duration for pattern analysis. |
| `server.url_detection.log_changes` | `yaml.d/web.yml` | `true` | Boolean; logs detected domain changes. |
| `server.url_detection.live_reload` | `yaml.d/web.yml` | `true` | Boolean; applies learned URL changes without restart. |
| `server.i18n.default_language` | `yaml.d/web.yml` | `en` | Supported locale code. |
| `server.i18n.available_languages` | `yaml.d/web.yml` | Every embedded locale; this project supports `en`, `ar`, `de`, `es`, `fr`, `it`, `ja`, and `zh` | List of supported locale codes; may be narrowed, but never below the required six (`en`, `es`, `fr`, `ar`, `de`, `it`). Unsupported languages fall back to English. |
| `web.api_contact` | `yaml.d/web.yml` | Empty | Public API contact value for discovery. |
| `web.llms.include_endpoints` | `yaml.d/web.yml` | `true` | Boolean; internal metrics, debug, and maintenance routes are excluded. |
| `web.llms.include_schemas` | `yaml.d/web.yml` | `false` | Boolean; controls the OpenAPI schema section. |
| `web.llms.custom_sections` | `yaml.d/web.yml` | `[]` | Markdown sections appended to generated discovery output. |
| `web.announcements.enabled` | `yaml.d/web.yml` | `true` | Boolean. |
| `web.announcements.messages[].id` | `yaml.d/web.yml` | No entries | Unique message identifier. |
| `web.announcements.messages[].type` | `yaml.d/web.yml` | `info` | `info`, `warning`, `error`, or `success`; unknown types are not rendered. |
| `web.announcements.messages[].title`, `message` | `yaml.d/web.yml` | Empty; no messages by default | Plain text; a non-empty message is required. |
| `web.announcements.messages[].start`, `end` | `yaml.d/web.yml` | Unset | Optional RFC 3339 timestamps; outside this UTC window the message is not rendered. |
| `web.announcements.messages[].dismissible` | `yaml.d/web.yml` | `false` | Boolean; a dismissible message has a no-JavaScript POST form and stores its ID in the visitor's `dismissed_announcements` cookie. |

Announcements render in configuration order after the skip links and before the shared header. IDs must be unique, lowercase letters or digits followed by lowercase letters, digits, `_` or `-`, and no more than 64 characters. The preferences page omits the dismissal form so its settings form remains the only page form.

```yaml
web:
  announcements:
    enabled: true
    messages:
      - id: scheduled-maintenance
        type: warning
        title: Scheduled maintenance
        message: Service may be unavailable during the maintenance window.
        start: "2026-12-01T02:00:00Z"
        end: "2026-12-01T04:00:00Z"
        dismissible: true
```

### Branding and SEO

These keys all live in `yaml.d/branding.yml`. Empty image values use embedded defaults. The UI theme accepts `dark`, `light`, or `auto`; accent must be a valid high-contrast `#RRGGBB` color or is ignored.

| Key | Default | Validation or behavior |
|---|---|---|
| `server.branding.title` | `{project_name}` | Display title. |
| `server.branding.tagline`, `server.branding.description` | Empty | Text. |
| `server.branding.favicon`, `server.branding.logo` | Empty | HTTPS URL, data URI, or a file name in `{config_dir}/assets/`. |
| `server.branding.content.about`, `.privacy`, `.terms` | Built-in content | Project page content; `privacy` extends built-in privacy text, `about` extends the page, and `terms` overrides built-in terms. |
| `server.branding.content.footer` | Built-in footer | HTML is sanitized; empty/whitespace disables the custom branding block. |
| `server.seo.keywords` | `[]` | List of keywords. |
| `server.seo.author` | Empty | Author or organization text. |
| `server.seo.og_image` | Empty | Image URL/data URI/local asset name. |
| `server.seo.twitter_handle` | Empty | `@handle`. |
| `server.seo.title_separator` | `" - "` | Text between title and tagline. |
| `server.seo.meta_tags` | `{}` | Map of name to content; at most 20 valid entries, invalid tag names/characters/length are rejected. |
| `server.seo.canonical` | Empty | Absolute HTTPS URL; empty uses request origin. |
| `server.seo.noindex` | `false` | Boolean; sets `noindex,nofollow` on all pages. |
| `server.seo.opengraph.enabled` | `true` | Boolean. |
| `server.seo.opengraph.type` | `website` | OpenGraph type. |
| `server.seo.opengraph.site_name` | Empty | Empty falls back to `server.branding.title`. |
| `server.seo.twitter.enabled` | `true` | Boolean. |
| `server.seo.twitter.card` | `summary_large_image` | `summary` or `summary_large_image`. |
| `server.seo.twitter.creator` | Empty | Creator `@handle`. |
| `server.seo.verification.google`, `.bing`, `.yandex`, `.baidu`, `.pinterest`, `.facebook` | Empty | Verification code; invalid formats are logged and ignored. |
| `server.seo.verification.custom` | `[]` | Additional validated verification tags. |
| `web.ui.theme` | `dark` | `dark`, `light`, or `auto`; user preference cookie takes precedence. |
| `web.ui.accent` | Empty (built-in color) | High-contrast `#RRGGBB`; invalid or low-contrast colors are ignored. |

### Security, abuse controls, and owner tokens

| Key | File | Default | Accepted values and validation |
|---|---|---|---|
| `server.rate_limit.read.requests`, `.window` | `yaml.d/security.yml` | `120`, `60` | Requests and seconds; always active, per IP or valid token. |
| `server.rate_limit.write.requests`, `.window` | `yaml.d/security.yml` | `10`, `60` | Requests and seconds. |
| `server.rate_limit.health.requests`, `.window` | `yaml.d/security.yml` | `120`, `60` | Requests and seconds. |
| `server.rate_limit.strict.requests`, `.window` | `yaml.d/security.yml` | `10`, `900` | Requests and seconds for reveal, rotate, and restore attempts. |
| `server.rate_limit.auth_failures.requests`, `.window` | `yaml.d/security.yml` | `5`, `900` | Failed auth attempts per IP; locks out until window expiration. |
| `server.rate_limit.global_burst` | `yaml.d/security.yml` | `240` | Per-minute total ceiling. There is no `enabled` switch. |
| `server.security.reports.retention_days` | `yaml.d/security.yml` | `365` | Days after closure; open reports are never purged. |
| `server.security.blocklists.enabled` | `yaml.d/security.yml` | `true` | Boolean; built-in `firehol_level1` remains active. |
| `server.security.blocklists.extra_sources[]` | `yaml.d/security.yml` | `[]` | Each source has `id` (unique `a-z0-9_`, not `firehol_level1`), `type` (`ip` or `domain`), HTTPS `url`, and `enabled` (`true` by default). |
| `server.security.allowlist[]` | `yaml.d/security.yml` | `[]` | Entries have `cidr` (IP/CIDR) and optional `description`. |
| `server.security.blocked_ips[]` | `yaml.d/security.yml` | `[]` | Entries have `cidr` and `reason`. |
| `server.geoip.enabled` | `yaml.d/security.yml` | `true` | Boolean. Lookup failures fail open. |
| `server.geoip.deny_countries`, `.allow_countries` | `yaml.d/security.yml` | `[]`, `[]` | Uppercase ISO 3166-1 alpha-2 codes. If both are set, `allow_countries` wins. |
| `server.geoip.presets` | `yaml.d/security.yml` | `{}` | Operator-authored map of preset names to country-code lists; never applied automatically. |
| `data.security_dir` | `yaml.d/security.yml` | `{data_dir}/security` | Directory path; databases under this path are external data, not embedded. |
| `data.cve.source` | `yaml.d/security.yml` | `https://services.nvd.nist.gov/rest/json/cves/2.0` | NVD API URL. |
| `data.cve.filter_by_cpe` | `yaml.d/security.yml` | `false` | Boolean; the default downloads the full unfiltered feed. |
| `data.cve.api_key` | `secrets.yml` | Empty | Optional NVD key. |
| `data.trivy.enabled` | `yaml.d/security.yml` | `false` | Boolean; disabled Trivy data is omitted from health checks. |
| `data.trivy.source` | `yaml.d/security.yml` | `ghcr.io/aquasecurity/trivy-db:2` | OCI image reference for the Trivy database. |
| `server.tokens.max_linked` | `yaml.d/security.yml` | `25` | Maximum active linked tokens per owner. |
| `server.tokens.cookie_max_age` | `yaml.d/security.yml` | `1y` | Duration for sliding owner cookie lifetime. |
| `server.tokens.idle_purge_days` | `yaml.d/security.yml` | `0` (never) | Days; purges only owners with no resources. |

### Pages, contact delivery, privacy, and analytics

| Key | File | Default | Accepted values and validation |
|---|---|---|---|
| `server.pages.contact.enabled` | `yaml.d/pages.yml` | `true` | Boolean; false makes contact GET/POST routes return 404. |
| `server.pages.contact.captcha` | `yaml.d/pages.yml` | `simple` | `simple`, `recaptcha`, or `hcaptcha`; external providers require site and secret keys. |
| `server.pages.contact.captcha_site_key` | `yaml.d/pages.yml` | Empty | Public key for selected external CAPTCHA. |
| `server.pages.contact.captcha_secret_key` | `secrets.yml` | Empty | Secret key for selected external CAPTCHA. |
| `server.pages.contact.success_message` | `yaml.d/pages.yml` | Built-in success text | Text shown after successful submission. |
| `server.pages.contact.antispam.honeypot` | `yaml.d/pages.yml` | `true` | Boolean; silently drops submissions with the hidden field filled. |
| `server.pages.contact.antispam.min_fill_seconds` | `yaml.d/pages.yml` | `3` | Minimum form-open time in seconds. |
| `server.pages.contact.antispam.max_age_seconds` | `yaml.d/pages.yml` | `43200` | Form-token validity in seconds. |
| `server.pages.contact.antispam.per_ip` | `yaml.d/pages.yml` | `5` | Accepted submissions per client IP in the configured window. |
| `server.pages.contact.antispam.per_email` | `yaml.d/pages.yml` | `3` | Accepted submissions per sender email in the configured window. |
| `server.pages.contact.antispam.global` | `yaml.d/pages.yml` | `60` | Accepted submissions globally in the configured window. |
| `server.pages.contact.antispam.window` | `yaml.d/pages.yml` | `1h` | Rate-limit window duration. |
| `server.pages.contact.antispam.max_links` | `yaml.d/pages.yml` | `10` | Maximum links in free-text fields; links must use this site's host. |
| `server.pages.contact.antispam.reject_markup` | `yaml.d/pages.yml` | `true` | Boolean; rejects HTML, entity-encoded angle brackets, and BBCode. |
| `server.pages.contact.antispam.blocked_words` | `yaml.d/pages.yml` | `[]` | Case-insensitive substrings rejected in name, subject, or message. |
| `server.pages.contact.antispam.blocked_email_domains` | `yaml.d/pages.yml` | `[]` | Sender email domains rejected by the filter. |
| `server.pages.contact.antispam.strikes` | `yaml.d/pages.yml` | `3` | Rejections from one IP within the window before form ban; `0` disables. |
| `server.pages.contact.antispam.ban_hours` | `yaml.d/pages.yml` | `24` | Ban duration in hours after reaching the strike threshold. |
| `server.pages.contact.antispam.review_min_seconds` | `yaml.d/pages.yml` | `2` | Minimum time the review step must be displayed before confirmation. |
| `server.pages.contact.antispam.review_max_seconds` | `yaml.d/pages.yml` | `900` | Review-token validity in seconds. |
| `server.pages.contact.antispam.duplicate_window_hours` | `yaml.d/pages.yml` | `24` | Duplicate-message suppression window in hours. |
| `server.pages.help.content`, `server.pages.help.mcp` | `yaml.d/pages.yml` | Built-in help / empty project expansion | Markdown text; MCP content supplements the generated base help. |
| `server.contact.admin.email` | `yaml.d/contact.yml` | Operator email on first install; otherwise required | Private alert destination; empty produces startup warning. |
| `server.contact.security.email` | `yaml.d/contact.yml` | `security@{fqdn}` | Public security contact; empty falls back to admin email. |
| `server.contact.abuse.email` | `yaml.d/contact.yml` | Empty | Public only when configured; fallback is general, then admin. |
| `server.contact.general.email` | `yaml.d/contact.yml` | Empty | Fallback for abuse/DMCA/privacy delivery; then admin. |
| `server.contact.dpo.name`, `.email` | `yaml.d/contact.yml` | Empty | DPO name and address; email display falls back to general, not admin. |
| `server.contact.{admin,security,abuse,general}.webhooks.{name}` | `yaml.d/contact.yml` / `secrets.yml` | Empty map | Named webhook URLs; URLs and generated per-webhook signing secrets are stored in `secrets.yml`. Generic webhook URLs must use HTTPS. |
| `server.tracking.type` | `yaml.d/privacy.yml` | Empty (disabled) | `google`, `matomo`, `piwik`, `owa`, `fathom`, `plausible`, `umami`, `simple`, or `cloudflare`; `none` also disables. |
| `server.tracking.id` | `yaml.d/privacy.yml` | Empty | Provider-specific: Google UA/G ID, Matomo/Piwik integer, Umami UUID, Plausible domain, or non-empty provider token/site ID. |
| `server.tracking.url` | `yaml.d/privacy.yml` | Empty | Required for Matomo, Piwik, OWA, and Umami; optional for Fathom/Plausible; unused by Google, Simple Analytics, and Cloudflare. Provided values must parse as URLs. |
| `server.privacy.data.sold` | `yaml.d/privacy.yml` | `false` | Boolean; controls sale/sharing copy and whether CCPA opt-out appears. |
| `server.privacy.data.stored_on_server` | `yaml.d/privacy.yml` | `true` | Boolean. |
| `server.privacy.data.sharing[]` | `yaml.d/privacy.yml` | Built-in sharing descriptions | Each item has `condition`, `when`, and `data` text. |
| `server.privacy.retention.period` | `yaml.d/privacy.yml` | Built-in retention statement | Text shown on the privacy page. |
| `server.privacy.retention.export_available`, `.deletion_available` | `yaml.d/privacy.yml` | `true`, `true` | Booleans controlling published rights. |
| `server.privacy.consent.show_until_acknowledged` | `yaml.d/privacy.yml` | `true` | Boolean. The consent banner itself is always enabled. |
| `server.privacy.consent.message`, `.message_if_sold` | `yaml.d/privacy.yml` | Built-in consent messages | Text selected by `server.privacy.data.sold`. |
| `server.privacy.consent.policy.text`, `.policy.url` | `yaml.d/privacy.yml` | `Privacy Policy`, `/server/privacy` | Link label and URL. |
| `server.privacy.consent.buttons.decline`, `.accept` | `yaml.d/privacy.yml` | `Decline`, `I Agree` | Button labels. |
| `server.privacy.consent.position` | `yaml.d/privacy.yml` | `bottom` | Banner position. |
| `server.privacy.consent.show_preferences` | `yaml.d/privacy.yml` | `true` | Boolean. |
| `server.privacy.consent.preferences_text` | `yaml.d/privacy.yml` | `Manage Preferences` | Preference control label. |
| `server.privacy.cookies.essential.enabled` | `yaml.d/privacy.yml` | `true` (always on) | Essential cookies cannot be disabled. |
| `server.privacy.cookies.essential.description` | `yaml.d/privacy.yml` | Built-in required-cookie description | Text. |
| `server.privacy.cookies.preferences.enabled` | `yaml.d/privacy.yml` | `true` | Boolean. |
| `server.privacy.cookies.preferences.description` | `yaml.d/privacy.yml` | Built-in preference-cookie description | Text. |
| `server.privacy.cookies.analytics.enabled` | `yaml.d/privacy.yml` | `true` | Boolean; analytics still requires configured tracking and visitor consent. |
| `server.privacy.cookies.analytics.description`, `.description_suffix_not_sold`, `.description_suffix_sold` | `yaml.d/privacy.yml` | Built-in descriptions | Text; sold/not-sold suffix selected by `server.privacy.data.sold`. |
| `server.privacy.third_party.services[]` | `yaml.d/privacy.yml` | `[]` | Each entry has `name`, `purpose`, `data_sent`, and `policy_url`. |

### Notifications, scheduling, metrics, backups, and overlays

| Key | File | Default | Accepted values and validation |
|---|---|---|---|
| `server.notifications.email.smtp.host` | `yaml.d/notifications.yml` | Empty (autodetect) | Host name/IP; configured host is tested at startup and reload. |
| `server.notifications.email.smtp.port` | `yaml.d/notifications.yml` | `587` | SMTP port; `SMTP_PORT` must be a positive integer. |
| `server.notifications.email.smtp.username` | `yaml.d/notifications.yml` | Empty | Optional SMTP username. |
| `server.notifications.email.smtp.password` | `secrets.yml` | Empty | SMTP password; never logged or returned. |
| `server.notifications.email.smtp.tls` | `yaml.d/notifications.yml` | `auto` | `auto`, `starttls`, `tls`, or `none`; invalid values reset to default. |
| `server.notifications.email.from.name` | `yaml.d/notifications.yml` | App title | Sender display name. |
| `server.notifications.email.from.email` | `yaml.d/notifications.yml` | `no-reply@{fqdn}` | Sender address. |
| `server.notifications.email.reply_to` | `yaml.d/notifications.yml` | Empty | Optional Reply-To address. |
| `server.notifications.email.events.{startup,shutdown,backup_complete,backup_failed,ssl_expiring,ssl_renewed,ssl_renewal_failed,security_alert,scheduler_error,update_available,update_installed,update_failed,update_staged}` | `yaml.d/notifications.yml` | Per-event boolean switches; defaults: startup/shutdown/backup_complete/ssl_renewed/update_available false; backup_failed/ssl_expiring/ssl_renewal_failed/security_alert/scheduler_error/update_installed/update_failed/update_staged true | Boolean; email delivery still requires working SMTP. |
| `server.notifications.webui.position` | `yaml.d/notifications.yml` | `top-right` | `top-right`, `top-left`, `bottom-right`, or `bottom-left`. |
| `server.notifications.webui.duration` | `yaml.d/notifications.yml` | `3` seconds | Non-negative integer; applies to success/info toasts and `0` means manual dismissal. |
| `server.notifications.webui.warning_duration` | `yaml.d/notifications.yml` | `5` seconds | Non-negative integer; applies to warnings and `0` means manual dismissal. Errors always require manual dismissal. |
| `server.notifications.disk_low.enabled` | `yaml.d/notifications.yml` | `true` | Boolean. |
| `server.notifications.disk_low.threshold_percent` | `yaml.d/notifications.yml` | `85` | Disk-use percentage. |
| `server.notifications.disk_low.check_interval` | `yaml.d/notifications.yml` | `5m` | Duration. |
| `server.notifications.disk_low.repeat` | `yaml.d/notifications.yml` | `24h` | Duration between repeat alerts. |
| `server.scheduler.timezone` | `yaml.d/maintenance.yml` | `America/New_York` | IANA timezone name; invalid zone warns and uses the default. |
| `server.scheduler.catch_up_window` | `yaml.d/maintenance.yml` | `1h` | Duration for missed-task catch-up. |
| `server.scheduler.tasks.{task}.enabled` | `yaml.d/maintenance.yml` | Built-in tasks enabled except `backup_hourly` | Boolean; scheduler itself is always on. |
| `server.scheduler.tasks.{task}.schedule` | `yaml.d/maintenance.yml` | Built-in cron/`@every` schedule | Valid scheduler expression; invalid settings reset to task defaults. |
| `server.scheduler.tasks.{task}.retry_on_fail`, `.retry_delay` | `yaml.d/maintenance.yml` | Task-specific; update tasks use enabled retry and `1h` delay | Boolean and duration. `backup` may additionally set `retention`. |
| `server.metrics.enabled` | `yaml.d/metrics.yml` | `true` | Boolean. |
| `server.metrics.root.enabled` | `yaml.d/metrics.yml` | `true` | Boolean; toggles root `/metrics` aliases. |
| `server.metrics.auth.allow_unauthenticated` | `yaml.d/metrics.yml` | `false` | Boolean; only for firewalled networks. |
| `server.metrics.auth.tokens.prometheus`, `.grafana`, `.loki` | `secrets.yml` | Generated service-specific `mtr_` tokens | Bearer token values; generated if empty. |
| `server.metrics.include_system`, `.include_runtime` | `yaml.d/metrics.yml` | `true`, `true` | Booleans. |
| `server.metrics.loki.max_entries`, `.max_age` | `yaml.d/metrics.yml` | `1000`, `1h` | Positive entry count and duration. |
| `server.metrics.duration_buckets`, `.size_buckets` | `yaml.d/metrics.yml` | `[0.001,0.005,0.01,0.025,0.05,0.1,0.25,0.5,1,2.5,5,10]`, `[100,1000,10000,100000,1000000,10000000]` | Numeric histogram bucket lists. |
| `server.backup.retention.max_backups` | `yaml.d/maintenance.yml` | `1` | Integer `1–365`; zero is invalid and resets to `1`. |
| `server.backup.retention.keep_weekly`, `.keep_monthly`, `.keep_yearly` | `yaml.d/maintenance.yml` | `0`, `0`, `0` | Integers `0–52`, `0–12`, `0–10`; zero disables that retention tier. |
| `server.backup.retention.max_total_size` | `yaml.d/maintenance.yml` | `10%` | Percent or byte-size; zero disables size cap. |
| `server.backup.encryption_password` | `secrets.yml` | Empty | Optional; required when compliance policy requires encrypted backups. Without it, backups are unencrypted. |
| `server.restore.max_archive_size`, `.max_expanded_size`, `.max_file_size` | `yaml.d/maintenance.yml` | `1G`, `4G`, `512M` | Byte sizes with units. |
| `server.restore.max_files`, `.max_ratio` | `yaml.d/maintenance.yml` | `10000`, `100` | Positive integers. |
| `server.update.branch` | `yaml.d/maintenance.yml` | `stable` | `stable`, `beta`, or `devel`; `daily`, `rolling`, `dev`, `development` normalize to `devel`. |
| `server.update.auto_install` | `yaml.d/maintenance.yml` | `false` | Boolean; scheduled checks notify only unless enabled. |
| `server.update.defer_days` | `yaml.d/maintenance.yml` | `0` | Integer `0–365`; ignored for `devel`. |
| `server.update.cli_min_version` | `yaml.d/maintenance.yml` | Empty | Optional minimum CLI version returned by autodiscovery. |
| `tor.onion_address`, `tor.contact_email` | `yaml.d/tor.yml` | Empty, empty | Optional onion hostname (without scheme) and Tor-only contact email. |
| `server.i2p.enabled` | `yaml.d/i2p.yml` | `true` in containers, `false` elsewhere | Boolean; `--i2p` then `I2P_ENABLED` override the config value. |
| `server.i2p.binary` | `yaml.d/i2p.yml` | Empty (auto-detect) | Optional `i2pd` executable path. |
| `server.i2p.sam_address` | `yaml.d/i2p.yml` | `127.0.0.1:7656` | SAMv3 host:port. |
| `server.i2p.virtual_port` | `yaml.d/i2p.yml` | `80` | I2P destination virtual port. |
| `server.i2p.inbound_length`, `.outbound_length` | `yaml.d/i2p.yml` | `3`, `3` | Positive tunnel hop counts. |
| `server.i2p.inbound_quantity`, `.outbound_quantity` | `yaml.d/i2p.yml` | `5`, `5` | Positive tunnel counts. |
| `server.i2p.signature_type` | `yaml.d/i2p.yml` | `7` | I2P signing algorithm identifier. |
| `server.i2p.bootstrap_timeout` | `yaml.d/i2p.yml` | `300` seconds | Positive startup timeout. |

`server.token`, `server.security.encryption_key`, metrics tokens, webhook signing secrets, runtime-derived `server.notifications.email.enabled`, and paths stored in the database are not ordinary area-file settings. Generated credentials and operator-provided credentials belong in `secrets.yml`. Settings not listed above because the AI.md feature definition does not state their default or validation are called out to the project owner for specification repair; this reference does not infer those values from implementation code.


### Additional public web settings

The following `web.*` settings live in `yaml.d/web.yml`. They are distinct from the `server.*` settings above.

| Key | Default | Accepted values and validation |
|---|---|---|
| `web.hsts.enabled` | `true` | Boolean; disable only for HTTP-only deployments. |
| `web.hsts.max_age_seconds` | `63072000` | HSTS lifetime in seconds (two years). |
| `web.hsts.include_subdomains`, `.preload` | `true`, `true` | Booleans. |
| `web.reports.rate_limit_per_minute` | `60` | Maximum public reports per minute per IP. |
| `web.reports.rate_limit_per_ip_burst` | `10` | Short-burst allowance per IP. |
| `web.headers.coop`, `.coep`, `.corp` | `unsafe-none`, `unsafe-none`, `cross-origin` | Cross-origin policy header values. Defaults preserve general embedding compatibility. |
| `web.headers.origin_agent_cluster` | `true` | Boolean; emits `Origin-Agent-Cluster: ?1`. |
| `web.headers.cross_domain_policies` | `none` | Adobe cross-domain policy value. |
| `web.headers.dns_prefetch_control` | Empty (omit header) | Empty omits the header; `off` disables DNS prefetch. |
| `web.headers.honor_sec_gpc`, `.honor_dnt` | `true`, `false` | Booleans; Sec-GPC is honored by default; DNT is opt-in. |
| `web.headers.sec_fetch_validation` | `true` | Boolean; validates cross-site state-changing frontend requests only. |
| `web.headers.server_timing_in_debug_only` | `true` | Boolean; prevents Server-Timing output in production. |
| `web.headers.clear_site_data.on_token_revocation`, `.on_consent_withdrawal` | `true`, `true` | Booleans controlling Clear-Site-Data on the named actions. |
| `web.headers.clear_site_data.execution_contexts` | `false` | Boolean opt-in to close SPA execution contexts. |
| `web.headers.nel.enabled` | `true` | Boolean. |
| `web.headers.nel.max_age_seconds` | `2592000` | NEL lifetime in seconds (30 days). |
| `web.headers.nel.include_subdomains` | `true` | Boolean. |
| `web.headers.nel.sample_rate` | `1.0` | Number from `0.0` through `1.0`. |
| `web.csp.enabled` | `true` | Boolean. |
| `web.csp.mode` | `enforce` | `enforce` or `report-only`. |
| `web.csp.script_src_extra`, `.style_src_extra`, `.img_src_extra`, `.font_src_extra`, `.connect_src_extra`, `.frame_src_extra`, `.form_action_extra` | Empty | Space-separated source expressions appended to generated directives. |
| `web.csp.script_src_override` | Empty | Optional replacement for the script-src directive. |
| `web.csp.reports_enabled` | `true` | Boolean; sends sampled violations to the server reports endpoint. |
| `web.csp.reports_sample_rate` | `1.0` | Number from `0.0` through `1.0`. |
| `web.robots.allow` | `[/, /api]` | Paths published as crawler `Allow` rules. |
| `web.robots.deny` | `[]` | Paths published as crawler `Disallow` rules; no AI-specific crawler setting exists. |
| `web.security.report_url` | Project GitHub private vulnerability-report URL | Absolute URL used as primary security.txt contact. |
| `web.security.contact` | `security@{fqdn}` | Secondary security contact value. |
| `web.security.expires` | One year from generation | Expiry date is renewed yearly. |
| `web.security.policy` | Empty (built-in safe-harbor policy) | Markdown body for the disclosure policy. |
| `web.well_known.unsupported_behavior` | `404` | Unknown well-known resources never redirect. |
| `web.well_known.webfinger.enabled`, `.openid_configuration.enabled`, `.assetlinks.enabled`, `.apple_app_site_association.enabled`, `.mta_sts.enabled`, `.oauth_protected_resource.enabled`, `.oauth_authorization_server.enabled`, `.gpc.enabled`, `.nodeinfo.enabled`, `.openpgpkey.enabled` | `false` for each | Boolean feature gates; enable only when the corresponding identity, provider, federation, mail, privacy-signal, or key-publication capability is configured. |

### Compliance, maintenance, and logging

| Key | File | Default | Accepted values and validation |
|---|---|---|---|
| `server.compliance.gdpr`, `.ccpa`, `.hipaa`, `.soc2`, `.pci_dss`, `.iso27001`, `.fedramp`, `.lgpd`, `.pipeda`, `.appi`, `.pdpa` | `yaml.d/privacy.yml` | `false` for each | Boolean declarations; compliance mode may require encrypted backups and operator controls. |
| `server.maintenance.cleanup.backup_keep_count` | `yaml.d/maintenance.yml` | `5` | Number of most recent backups kept during cleanup. |
| `server.maintenance.cleanup.disk_threshold` | `yaml.d/maintenance.yml` | `90` | Percentage; cleanup starts above 90% disk use. |
| `server.maintenance.cleanup.log_retention_days` | `yaml.d/maintenance.yml` | `7` | Delete logs older than this many days during cleanup. |
| `server.maintenance.notify.on_enter`, `.on_exit` | `yaml.d/maintenance.yml` | `true`, `true` | Booleans for notifications when entering and exiting maintenance mode. |
| `server.maintenance.self_healing.max_attempts` | `yaml.d/maintenance.yml` | `0` | Non-negative integer; `0` retries without a limit. |
| `server.maintenance.self_healing.retry_interval` | `yaml.d/maintenance.yml` | `30s` | Duration between retry attempts. Self-healing is always on. |
| `server.logs.level` | `yaml.d/logs.yml` | Mode-derived (`info` in production; `debug` in development/debug) | `debug`, `info`, `warn`, or `error`; explicit value overrides mode-derived level. |
| `server.logs.defaults.custom` | `yaml.d/logs.yml` | Empty | Custom format string used when a log kind selects custom format. |
| `server.logs.defaults.rotate` | `yaml.d/logs.yml` | `weekly,50MB` | Rotation by time, size, or both. |
| `server.logs.defaults.keep` | `yaml.d/logs.yml` | `none` | `none`, count, duration, or `forever`. |
| `server.logs.access.format` | `yaml.d/logs.yml` | `apache` | `apache`, `nginx`, `json`, or `custom`. |
| `server.logs.access.rotate` | `yaml.d/logs.yml` | `monthly` | Rotation policy. |
| `server.logs.server.format`, `.error.format` | `yaml.d/logs.yml` | `text`, `text` | `text` or `json`. |
| `server.logs.audit.enabled` | `yaml.d/logs.yml` | `true` | Boolean. |
| `server.logs.audit.format` | `yaml.d/logs.yml` | `json` | JSON only; audit logs must remain machine-parseable. |
| `server.logs.audit.rotate` | `yaml.d/logs.yml` | `daily` | Rotation policy. |
| `server.logs.audit.filename` | `yaml.d/logs.yml` | `audit.log` | Log file name. |
| `server.logs.audit.keep` | `yaml.d/logs.yml` | `none` | Retention policy; `none`, count, days/weeks/months, or `forever`. |
| `server.logs.audit.compress` | `yaml.d/logs.yml` | `false` | Boolean; applies to rotated audit logs retained by `keep`. |
| `server.logs.audit.events.configuration`, `.security`, `.backup`, `.server` | `yaml.d/logs.yml` | `true` for each | Boolean switches for configuration, security, backup/restore, and server event categories. |
| `server.logs.audit.include_user_agent` | `yaml.d/logs.yml` | `true` | Boolean; controls user-agent inclusion. |
| `server.logs.audit.mask_emails` | `yaml.d/logs.yml` | `true` | Boolean; email addresses are masked in audit data. |
| `server.logs.debug.enabled` | `yaml.d/logs.yml` | `false` | Boolean; debug log is troubleshooting-only. |
| `server.logs.debug.format` | `yaml.d/logs.yml` | `text` | `text` or `json`. |
| `server.logs.{kind}.filename`, `.rotate`, `.keep`, `.compress` | `yaml.d/logs.yml` | `{kind}.log`; inherited rotation/keep defaults except access (monthly) and audit (daily); compress false | File name and log-kind overrides. Rotation supports `never`, daily/weekly/monthly/yearly, size such as `50MB`, or combined rules; retention supports `none`, count, days/weeks/months, or `forever`. |

### AI-agent discovery

The server always serves its generated discovery document at `/.well-known/llms.txt` and `/llms.txt`. These entries cannot be disabled. Set these options in `yaml.d/web.yml`:

| Setting | Default | Effect |
|---|---:|---|
| `web.api_contact` | Empty | Public contact value shown for API questions |
| `web.llms.include_endpoints` | `true` | Include endpoint paths discovered from registered API routes; internal metrics, debug, and maintenance routes are excluded |
| `web.llms.include_schemas` | `false` | Include the OpenAPI request and response schema section |
| `web.llms.custom_sections` | `[]` | Append operator-authored Markdown sections to the generated document |

The generated document resolves its base URL for each request, describes the API and its authentication rules, lists the configured rate limit, and includes API and security contact values. It does not advertise internal metrics routes. See [Integrations](integrations.md) for its output and route behavior.

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

Directory variables are read at every startup, with a matching command-line flag taking precedence. `PORT`, `LISTEN`, `APPLICATION_NAME`, `APPLICATION_TAGLINE`, `DATABASE_DRIVER`, `DATABASE_URL`, and `CACHE_URL` seed configuration only on first run; after the connection values are saved, later environment changes do not replace them. Database and cache URLs that contain credentials are stored in `secrets.yml`; credential-free URLs remain in `server.yml`. The database URL scheme selects its driver; `DATABASE_DRIVER` is used when the URL is absent or the database is SQLite.

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

`/server/healthz` is always available. The root `/healthz` alias is disabled unless `server.healthz.root.enabled` is true. Metrics use `/server/metrics` and its service-specific Prometheus, Grafana, and Loki routes. Root `/metrics` aliases are enabled by default and can be disabled with `server.metrics.root.enabled: false`; each service has a separate bearer token in `server.metrics.auth.tokens.{prometheus,grafana,loki}`. Restrict these internal endpoints to trusted monitoring systems. Language detection follows the request's supported locale and the `lang` preference; the explicit language control is on `/server/preferences`.

Health responses use cached component and dataset checks. Core probes and loaded datasets refresh on a schedule, and relevant data reloads refresh the cache immediately. NVD/CVE health is included as `checks.data_cve_nvd`; `checks.data_trivy_db` appears only when Trivy database support is enabled. Individual dataset states are `ok`, `degraded`, or `error`; the public response contains no paths or error details.
