# Server Operations

GitIgnore serves the public template catalog through a static server binary or Docker container. There is no user registration or admin web panel. Anonymous owner identities use Bearer tokens for protected operations and MCP; catalog reads remain public. Operators configure the service through the generated `server.yml`, supported startup environment variables, command-line flags, and the documented maintenance commands.

## Install and configure

See [Installation](installation.md) for deployment options and [Configuration](configuration.md) for paths, settings, and precedence. On first start, the server creates its configuration and required runtime directories. When started as root on Unix, it creates the fixed `gitignore` service account if needed, sets runtime-directory ownership, binds the listener, and drops privileges to that account. A non-root start keeps the current identity and uses private runtime directories. The catalog and locale files are embedded in the binaries.

## Server pages

The public server pages share the same layout, navigation, theme, skip links, and footer. They are server-rendered and their core content and forms work without JavaScript.

Operators may configure scheduled service notices under `web.announcements` in `yaml.d/web.yml`. Active notices render after the skip links; visitors can dismiss them with a form POST, and the `dismissed_announcements` cookie records the dismissed IDs. The cookie is disclosed on the Privacy page and is removed by Preferences Reset. See [Configuration](configuration.md) for the fields and time window.

| Page | Purpose |
|---|---|
| `/server` | Permanent redirect to `/server/about` |
| `/server/about` | Product description, version, features, and attribution |
| `/server/help` | Browser, API, CLI, and troubleshooting help |
| `/server/help/mcp` | MCP endpoint setup, tools, and project-specific guidance |
| `/server/preferences` | Public theme, language, and privacy preference controls |
| `POST /server/theme` | No-JavaScript theme switch used by the shared theme control |
| `/server/preferences/tokens` | Anonymous owner-token management; create one linked token per MCP client |
| `/server/preferences/sync` | Cross-device browser-preference export and import |
| `/server/preferences/sync/import` | Shared preference link that applies cookie values and redirects safely |
| `/server/privacy` | Privacy and data-handling information |
| `/server/contact` | Public contact form for abuse, DMCA, privacy, and security reports |
| `/server/terms` | Terms of service |
| `/server/healthz` | Content-negotiated health page and endpoint, including Tor/I2P status when active |
| `/server/security` | Security contact methods and reporting instructions |
| `/server/security/policy` | Coordinated-disclosure policy |
| `/server/security/thanks` | Public researcher acknowledgments |
| `/server/security/report/{tracking_id}` | Researcher status page requiring the emailed token, usable once per day and expiring 30 days after closure |
| `/server/dpo` | Data protection officer contact when configured |
| `/server/docs/swagger` | Interactive OpenAPI UI |
| `/server/docs/graphql` | GraphiQL playground |

`/server/contact` renders a no-JavaScript form with the configured challenge. Its JSON counterpart is `GET /api/{api_version}/server/contact`; submission uses a review and confirmation flow through POST. Security-report mode is enabled by a valid rotating `security_id` obtained from the security contact information. See [Security](security.md) for the reporting and data-protection details.

## Health and discovery

The root `/healthz` alias is disabled by default and can be enabled with `server.healthz.root.enabled`. API health aliases `/api/healthz` and `/api/healthz.txt` are available independently. `GET /api/autodiscover` reports the configured versioned API base and discovery endpoints.

If database initialization or required runtime-directory setup fails, the process stays available in read-only maintenance mode. Health endpoints return HTTP `503` with `status: maintenance`, a safe reason and message, and self-healing attempt metadata. The response includes `Retry-After`, `X-Maintenance-Mode`, and `X-Maintenance-Reason`. State-changing HTTP requests receive the canonical `MAINTENANCE` error with HTTP `503`; GET, HEAD, OPTIONS, and the read-only GraphQL/MCP POST endpoints remain available. The service retries at `server.maintenance.self_healing.retry_interval` (default `30s`) until recovery or `max_attempts` (default `0`, unlimited); after a positive limit is reached, writes remain blocked until the service is restarted after the cause is fixed. On recovery, health is refreshed and scheduled tasks resume.

Other public resources include `/security.txt`, `/.well-known/security.txt`, `/.well-known/llms.txt` and `/llms.txt` for AI discovery, `/.well-known/pgp-key.asc` when a keypair exists and `web.security.publish_pgp_key` is enabled, `/robots.txt`, `/manifest.json`, `/sw.js`, and `/locales/{lang}.json`. Internal metrics are available under `/server/metrics`; root `/metrics` aliases are enabled by default. Restrict metrics endpoints to trusted monitoring systems and use their per-service bearer tokens.

## Security

All catalog APIs and browser pages are public. Place exposed instances behind HTTPS, configure trusted proxies accurately, restrict metrics, and leave debug routes disabled in production. See the [Security guide](security.md).
