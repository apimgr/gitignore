# Security

## Public, read-only service

The application serves a curated template catalog embedded in the binary. Catalog APIs and browser pages are public and do not require an account or API key. The server does not accept user-submitted templates. The `/server/contact` page accepts operator messages through a no-JavaScript form protected by a self-hosted arithmetic challenge; delivery requires configured SMTP and a contact recipient. Security reports use the private disclosure routes described below.

## Security and health endpoints

| Endpoint | Purpose |
|---|---|
| `/security.txt` | Security contact document |
| `/.well-known/security.txt` | Well-known alias for the security contact document |
| `/robots.txt` | Crawler rules from `web_robots` configuration |
| `/server/healthz` | Canonical health page/API; output is negotiated by request |
| `/server/security` | Human-readable security policy and reporting information |
| `/server/security/policy` | Security policy page |
| `/server/security/thanks` | Public researcher acknowledgments |
| `/server/security/report/{tracking_id}` | Researcher status page; requires the one-time token emailed with the tracking ID |
| `/server/contact?security_id={id}` | Contact form in security-report mode when the rotating ID is valid |
| `POST /api/{api_version}/server/contact` | Contact or security-report submission; version defaults to `v1` |
| `/api/v1/server/healthz` | Versioned health API |
| `/api/healthz` and `/api/healthz.txt` | Unversioned health aliases |

The root `/healthz` endpoint is optional and disabled by default. Enable it with `server.healthz.root.enabled`. The security contact defaults to `security@apimgr.us` and can be configured under `web_security.admin`.

The contact form is enabled by default and uses the built-in `simple` challenge. Configure `server.contact.general.email` (or the fallback admin recipient) and SMTP settings before expecting messages to be delivered. Captcha challenges are single-use, expire after ten minutes, and are rate limited. If SMTP is unavailable, the form returns an error and does not claim delivery.

## Operational protections

- Place the service behind HTTPS when it is exposed over an untrusted network. TLS can be configured in `server.ssl` or terminated by a trusted reverse proxy.
- Set trusted proxy addresses before relying on forwarded headers. Configure `server.trusted_proxies.additional` for public proxy addresses or CIDRs.
- Debug and profiling routes under `/debug/*` are registered only when the independent debug setting is enabled; application mode alone does not enable them.
- Prometheus metrics are enabled by default at `/metrics`. This endpoint is intended for internal monitoring and should be blocked from public networks. An optional bearer token is configured with `server.metrics.token`.
- Rate limiting is configurable through `server.rate_limit`.
- Keep the server binary and its embedded template catalog current.

See [Configuration](configuration.md) for the relevant settings and [the repository security policy](https://github.com/apimgr/gitignore/blob/main/.github/SECURITY.md) for vulnerability reporting.
