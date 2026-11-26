# Integrations and Discovery

## API tools and discovery

| Endpoint | Purpose |
|----------|---------|
| `/api/autodiscover` | Server and API metadata for clients |
| `/.well-known/llms.txt` and `/llms.txt` | Public AI-agent discovery document; GET and HEAD, no authentication |
| `/.well-known/security.txt` and `/security.txt` | RFC 9116 security contact document |
| `/server/docs/swagger` | Interactive OpenAPI UI |
| `/api/swagger` and `/api/v1/server/swagger` | OpenAPI JSON |
| `/server/docs/graphql` | GraphiQL browser playground |
| `/api/{api_version}/server/mcp` | Streamable HTTP Model Context Protocol endpoint |
| `/api/mcp` and `/mcp` | Direct aliases for the MCP endpoint |
| `/server/help/mcp` | Human-readable MCP setup and tool guidance |
| `/api/{api_version}/server/help/mcp` | MCP help metadata and project guidance |
| `/api/graphql` and `/api/{api_version}/server/graphql` | GraphQL query endpoints (POST and GET); GET accepts `query`, optional JSON `variables`, and `operationName` |
| `/api/v1/cli/sh` | Generated POSIX shell helper |
| `/api/v1/cli/ps` | Generated PowerShell helper |
| `/api/v1/cli/completion/{bash,zsh,fish}` | Shell completion scripts |

See [API Reference](api.md) for route details and the unversioned gitignore.io compatibility routes.

Both GraphQL GET routes provide a plain usage response when `query` is omitted and accept the same query options as POST. The versioned route backs the GraphQL page's no-JavaScript help link; the interactive GraphiQL editor still requires JavaScript.

## Progressive Web App

The browser UI exposes `/manifest.json` and `/sw.js` and embeds its CSS, JavaScript, icon, offline page, Swagger UI, and GraphiQL assets. These resources are served by the application; the browser does not need to download the UI libraries from third-party CDNs.

## Localization

The server embeds English, Arabic, German, Spanish, French, Italian, Japanese, and Chinese locale files and serves the raw locale catalogs at `/locales/{lang}.json`. Unsupported locale values fall back to English. See the [translation guide](translating.md).

## Security and crawler metadata

`/security.txt` and `/.well-known/security.txt` serve the security contact document. `/.well-known/llms.txt` and `/llms.txt` serve the same generated AI-discovery document; these routes are always enabled. `/.well-known/pgp-key.asc` publishes the project public key when a keypair exists and `web.security.publish_pgp_key` is enabled. `/robots.txt` is generated from the configured allow and deny paths. Health aliases and internal metrics endpoints are documented in [Security](security.md) and [Configuration](configuration.md).

The llms.txt document is built per request from the current host and API version, project metadata, rate-limit settings, API/security contact fields, and the registered API routes. Endpoint listing defaults to enabled and excludes internal metrics, debug, and maintenance routes. Set `web.llms.include_endpoints: false` to omit that section; `web.llms.include_schemas: true` adds the OpenAPI schema. `web.llms.custom_sections` appends operator-provided Markdown sections, and `web.api_contact` supplies the API contact shown in the document. MCP is always listed with its Bearer-token requirement; the public catalog REST API remains unauthenticated.

## Platform integrations

No OAuth/OIDC identity provider, webhook receiver, native app association file, or federation protocol is enabled by this project. The companion CLI and gitignore.io-compatible routes are also supported integrations. MCP is always enabled and uses the same Bearer-token authentication and authorization rules as the REST API. Every MCP request requires a valid token; the public catalog REST API remains usable without one. Create a separate linked token for each MCP client at `/server/preferences/tokens`; token management and token issuance are never exposed as MCP tools. Its endpoint, aliases, tools, and protocol request are described on `/server/help/mcp` and in the generated OpenAPI document.

The companion CLI reads `/api/autodiscover` for published platform versions and the optional minimum accepted CLI version. Operators provision files in the configured CLI release directory; the server verifies their SHA-256 values before publishing metadata or streaming a binary. Downloads use HTTPS, are public by default, and can require a Bearer token through `cli.binary_download.require_auth`.
