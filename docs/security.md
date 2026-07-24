# Security

## Authentication

Public endpoints (templates, list, search, combine, health) require no
authentication. Administrative and debug endpoints require either:

- **Bearer token**: `Authorization: Bearer YOUR_TOKEN`
- **Basic auth**: `Authorization: Basic base64(user:pass)`

Tokens are never stored in plaintext; they are hashed before storage.

## Public Security Endpoints

| Endpoint | Purpose |
|----------|---------|
| `/security.txt` | Security contact and policy |
| `/.well-known/security.txt` | RFC 9116 well-known location |
| `/robots.txt` | Crawler directives |
| `/healthz` | Liveness/health probe |
| `/api/v1/server/healthz` | Versioned health probe |

## Security Reporting

Researchers should use the contact details published in
`/.well-known/security.txt`. That file points to the maintainer contact and
disclosure policy for the project.

## Well-Known Namespace

The server serves `/.well-known/security.txt`. Unknown entries under
`/.well-known/*` return `404`. Feature-gated entries (app association files,
provider metadata, and similar) are only served when explicitly enabled; none
are enabled by default in this project.

## Transport & Hardening

- Debug and pprof routes are only mounted when `--debug` is set.
- The server honors `X-Forwarded-*` headers only when the request arrives from a
  trusted proxy, preventing client-URL spoofing.
- All responses set standard security headers via middleware.
