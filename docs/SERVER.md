# Server Operations

GitIgnore is a public, read-only template service. Operators configure the server with `server.yml`, startup environment variables, and CLI flags. There is no web management interface.

## Install and run

See [Installation](installation.md) for Docker, binary, service, and daemon options. The template catalog and locale data are embedded in the server binary.

## Configuration

See [Configuration](configuration.md) for supported paths, settings, and precedence. The server creates a default `server.yml` when it starts without one.

## Browser pages

| Page | Purpose |
|------|---------|
| `/server` | Server information and links to documentation |
| `/server/about` | Product and build information |
| `/server/help` | Browser, CLI, and API usage help |
| `/server/privacy` | Privacy and data handling information |
| `/server/contact` | Public message form with self-hosted challenge; requires configured SMTP for delivery |
| `/server/terms` | Terms of use |
| `/server/healthz` | Content-negotiated server health |
| `/server/security` | Security overview and reporting information |
| `/server/security/policy` | Security policy page |
| `/server/security/thanks` | Security report acknowledgment page |
| `/server/docs/swagger` | Interactive OpenAPI UI |
| `/server/docs/graphql` | GraphiQL playground |

The optional root health alias `/healthz` is disabled by default. The API health routes `/api/healthz`, `/api/healthz.txt`, and `/api/v1/server/healthz` are available independently. See [API Reference](api.md).

## Other public endpoints

- `/api/v1/server/scheduler` returns read-only scheduler status.
- `/security.txt` and `/.well-known/security.txt` expose the configured security contact.
- `/robots.txt` is generated from the `web_robots` settings.
- `/manifest.json`, `/sw.js`, and `/locales/{lang}.json` support the PWA and locale selector.
- `/metrics` is enabled by default and should be firewalled from public access.

## Security

API and browser content are public and require no authentication. Keep debug routes disabled in production, restrict metrics to trusted monitoring systems, and configure trusted proxies accurately. See the [Security](security.md) guide.
