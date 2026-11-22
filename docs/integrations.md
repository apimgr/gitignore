# Integrations and Discovery

## API tools and discovery

| Endpoint | Purpose |
|----------|---------|
| `/api/autodiscover` | Server and API metadata for clients |
| `/server/docs/swagger` | Interactive OpenAPI UI |
| `/api/swagger` and `/api/v1/server/swagger` | OpenAPI JSON |
| `/server/docs/graphql` | GraphiQL browser playground |
| `/api/{api_version}/server/mcp` | Streamable HTTP Model Context Protocol endpoint |
| `/api/mcp` and `/mcp` | Direct aliases for the MCP endpoint |
| `/server/help/mcp` | Human-readable MCP setup and tool guidance |
| `/api/{api_version}/server/help/mcp` | MCP help metadata and project guidance |
| `/api/graphql` and `/api/{api_version}/server/graphql` | GraphQL query endpoints (POST) |
| `/api/v1/cli/sh` | Generated POSIX shell helper |
| `/api/v1/cli/ps` | Generated PowerShell helper |
| `/api/v1/cli/completion/{bash,zsh,fish}` | Shell completion scripts |

See [API Reference](api.md) for route details and the unversioned gitignore.io compatibility routes.

## Progressive Web App

The browser UI exposes `/manifest.json` and `/sw.js` and embeds its CSS, JavaScript, icon, offline page, Swagger UI, and GraphiQL assets. These resources are served by the application; the browser does not need to download the UI libraries from third-party CDNs.

## Localization

The server embeds English, Arabic, German, Spanish, French, Italian, Japanese, and Chinese locale files and serves the raw locale catalogs at `/locales/{lang}.json`. Unsupported locale values fall back to English. See the [translation guide](translating.md).

## Security and crawler metadata

`/security.txt` and `/.well-known/security.txt` serve the security contact document. `/robots.txt` is generated from the configured allow and deny paths. Health aliases and optional metrics are documented in [Security](security.md) and [Configuration](configuration.md).

## Platform integrations

No OAuth/OIDC identity provider, webhook receiver, native app association file, or federation protocol is enabled by this project. The companion CLI and gitignore.io-compatible routes are also supported integrations. MCP is always enabled, exposes the public read-only catalog operations, and does not require a token. Its endpoint, aliases, tools, and protocol request are described on `/server/help/mcp` and in the generated OpenAPI document.
