# Integrations

## Discovery & Protocol Endpoints

| Endpoint | Purpose |
|----------|---------|
| `/server/docs/swagger` | Swagger UI |
| `/server/docs/graphql` | GraphiQL playground |
| `/api/swagger` | OpenAPI JSON |
| `/api/graphql` | GraphQL schema (GET) / endpoint (POST) |
| `/api/autodiscover` | Client autodiscovery document |
| `/manifest.json` | PWA web app manifest |
| `/sw.js` | Service worker for offline/PWA support |

## Progressive Web App

The web UI ships a PWA manifest (`/manifest.json`) and a service worker
(`/sw.js`), so the browser surface can be installed as an app and serve cached
assets offline.

## Localization

Locale bundles are served at `/locales/{lang}.json` for client-side
translation. The server ships seven translated locales.

## CLI Integration

The companion `gitignore-cli` client consumes the same REST API and is intended
for editor and shell-pipeline integration (for example, generating a
`.gitignore` on project bootstrap).

## Platform Integrations

No third-party platform association files (Android App Links, Apple
app-site-association, MTA-STS, or federation metadata) are enabled in this
project. If they are enabled in a deployment, document the required
configuration alongside the relevant `/.well-known/*` entry.
