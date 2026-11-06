# Security

## Public API

The service is intentionally public and read only. It has no user accounts,
login, API keys, admin panel, or user-submitted templates. Requests can retrieve
only the curated catalog embedded in the binary.

## Security endpoints

| Endpoint | Purpose |
|---|---|
| `/security.txt` | Security contact and policy |
| `/.well-known/security.txt` | Well-known security contact |
| `/robots.txt` | Crawler directives |
| `/healthz` | Health check |
| `/api/v1/server/healthz` | Versioned health check |

Use the contact and disclosure process published in `/.well-known/security.txt`
to report vulnerabilities.

## Deployment guidance

- Put the service behind HTTPS when it is exposed over an untrusted network.
- Configure trusted proxy addresses before relying on forwarded request headers.
- Debug and profiling endpoints are enabled only when debug mode is selected.
- Keep the binary and its embedded template catalog current.
