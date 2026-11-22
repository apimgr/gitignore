# Server Operations

GitIgnore serves the public template catalog through a static server binary or Docker container. There are no user accounts or admin web panel. Operators configure the service through the generated `server.yml`, supported startup environment variables, command-line flags, and the documented maintenance commands.

## Install and configure

See [Installation](installation.md) for deployment options and [Configuration](configuration.md) for paths, settings, and precedence. On first start, the server creates its configuration and required runtime directories. The catalog and locale files are embedded in the binaries.

## Server pages

The public server pages share the same layout, navigation, theme, skip links, and footer. They are server-rendered and their core content and forms work without JavaScript.

| Page | Purpose |
|---|---|
| `/server` | Server information and public links |
| `/server/about` | Product description, version, features, and attribution |
| `/server/help` | Browser, API, CLI, and troubleshooting help |
| `/server/help/mcp` | MCP endpoint setup, tools, and project-specific guidance |
| `/server/preferences` | Public theme, language, and privacy preference controls |
| `/server/preferences/sync` | Cross-device browser-preference export and import |
| `/server/preferences/sync/import` | Shared preference link that applies cookie values and redirects safely |
| `/server/privacy` | Privacy and data-handling information |
| `/server/contact` | Public contact form for abuse, DMCA, privacy, and security reports |
| `/server/terms` | Terms of service |
| `/server/healthz` | Content-negotiated health page and endpoint |
| `/server/security` | Security contact methods and reporting instructions |
| `/server/security/policy` | Coordinated-disclosure policy |
| `/server/security/thanks` | Public researcher acknowledgments |
| `/server/security/report/{tracking_id}` | Researcher status page requiring the emailed one-shot token |
| `/server/dpo` | Data protection officer contact when configured |
| `/server/docs/swagger` | Interactive OpenAPI UI |
| `/server/docs/graphql` | GraphiQL playground |

`/server/contact` renders a no-JavaScript form with the configured challenge. Its JSON counterpart is `GET /api/{api_version}/server/contact`; submission uses a review and confirmation flow through POST. Security-report mode is enabled by a valid rotating `security_id` obtained from the security contact information. See [Security](security.md) for the reporting and data-protection details.

## Health and discovery

The root `/healthz` alias is disabled by default and can be enabled with `server.healthz.root.enabled`. API health aliases `/api/healthz` and `/api/healthz.txt` are available independently. `GET /api/autodiscover` reports the configured versioned API base and discovery endpoints.

Other public resources include `/security.txt`, `/.well-known/security.txt`, `/robots.txt`, `/manifest.json`, `/sw.js`, and `/locales/{lang}.json`. Prometheus metrics use `/metrics` by default and should be restricted to trusted monitoring systems.

## Security

All catalog APIs and browser pages are public. Place exposed instances behind HTTPS, configure trusted proxies accurately, restrict metrics, and leave debug routes disabled in production. See the [Security guide](security.md).
