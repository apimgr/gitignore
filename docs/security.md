# Security

## Public service

The catalog and browser pages are public and read-only. The application has no user registration, profile login, or admin web panel. Anonymous owner identities use Bearer tokens; the REST catalog remains public, while MCP and protected owner or operator operations require the corresponding token. Operators configure the service through `server.yml` and the documented command-line maintenance interface. Template names are resolved from the embedded catalog and are never used to construct filesystem paths.

The contact page accepts reports from visitors. It validates field sizes and addresses, requires a single-use challenge, and sends messages only to configured recipients when SMTP is available. Security reports use a separate encrypted-storage and disclosure flow. These submissions are not public catalog data.

## Security and health endpoints

| Endpoint | Purpose |
|---|---|
| `/.well-known/llms.txt` and `/llms.txt` | Public AI-agent discovery document; GET and HEAD, always available without authentication |
| `/.well-known/security.txt` | Machine-readable security contacts and expiry |
| `/.well-known/pgp-key.asc` | Public encryption key when a keypair exists and `web.security.publish_pgp_key` is enabled; otherwise `404` |
| `/security.txt` | Security contact alias |
| `/server/security` | Human-readable reporting channels and security information |
| `/server/security/policy` | Coordinated disclosure policy |
| `/server/security/thanks` | Public acknowledgments for credited researchers |
| `/server/contact?security_id={id}` | Security report form using the current rotating token |
| `/server/security/report/{tracking_id}` | Researcher report status; emailed token allows one access per day and expires 30 days after closure |
| `/api/{api_version}/server/security` | JSON mirror of public security contact metadata |
| `/api/{api_version}/server/contact` | Contact form description (GET) and confirmed submission (POST) |
| `/api/{api_version}/server/reports/{default,csp,nel}` | Public browser report endpoints; return `204 No Content` and share a per-IP limit of 60 reports per minute with a burst capacity of 10 |
| `/server/healthz` | Canonical HTML/API health endpoint, negotiated by request |
| `/api/{api_version}/server/healthz` | Versioned API health |
| `/api/healthz` and `/api/healthz.txt` | Unversioned API health aliases |
| `/server/metrics`, `/server/metrics/prometheus` | Prometheus exposition; requires the Prometheus bearer token |
| `/server/metrics/grafana` | Grafana dashboard JSON; requires the Grafana bearer token |
| `/server/metrics/loki` | Loki-compatible log stream; requires the Loki bearer token |
| `/api/{api_version}/server/metrics` and `/api/{api_version}/server/metrics/{prometheus,grafana,loki}` | Versioned metrics routes with the matching per-service bearer token |
| `/api/metrics` and `/api/metrics/{prometheus,grafana,loki}` | Unversioned metrics aliases with the matching per-service bearer token |
| `/metrics` and `/metrics/{prometheus,grafana,loki}` | Root metrics aliases when enabled; requires the matching per-service bearer token |
| `/robots.txt` | Crawler directives from configuration |

Repository-source vulnerabilities should be reported through [GitHub private vulnerability reporting](https://github.com/apimgr/gitignore/security/advisories/new), as described in [.github/SECURITY.md](https://github.com/apimgr/gitignore/blob/main/.github/SECURITY.md). A deployed-instance vulnerability can be reported through the security form linked from `/server/security` and `security.txt`. Do not file vulnerability details in a public issue.

The security form is enabled by `server.pages.contact.enabled`. It requires the rotating `security_id`, which is valid for the current and previous 48-hour period. The form asks for impact, reproduction steps, severity, and disclosure preferences. Report bodies are encrypted with the project PGP key when available, otherwise with AES-256-GCM using the generated at-rest key. Researcher contact details and any submitted public key are stored separately under AES-256-GCM. Report content is not included in logs or the public status page. The researcher receives a tracking ID and an emailed status token that permits one status-page access per day; access expires 30 days after the report closes. Operators manage report metadata through the `--maintenance security` commands; there is no admin web interface or route for reading report contents.

Browser security reports use a separate flow from vulnerability reports. CSP violations are written to the private `security.log` with query strings, fragments, internal IPs, filesystem URIs, and oversized fields removed. The default Reporting API and NEL endpoints do not echo report data to callers; NEL report bodies are not retained. The `Reporting-Endpoints` and `Report-To` headers advertise `/api/{api_version}/server/reports/default`.

### Project OpenPGP key

The optional project key is managed with `--maintenance pgp generate`, `rotate`, `publish`, `export`, `import`, and `delete`. The private key is encrypted at rest; the public key is served at `/.well-known/pgp-key.asc` when publication is enabled. Stored report bodies use the project PGP key when available and otherwise use AES-256-GCM; a researcher's supplied key is used for the acknowledgment email, which contains no report content. Rotation re-encrypts retained PGP-encrypted reports transactionally and keeps the prior encrypted key for the transition period. Keyserver publication retries on the scheduled maintenance cadence. Private-key operations require operator authorization and typed confirmation; private key material is never exposed by a web endpoint.

The public Contact page also handles abuse, DMCA, and privacy requests. It uses a one-time challenge, request and field size limits, validation, and rate limiting. `GET /api/{api_version}/server/contact` publishes the form field descriptors and a fresh challenge without disclosing the destination address. The POST flow has a review step; a message is sent only after the confirmation step. Configure a recipient and SMTP before expecting delivery.

## Deployment protections

- Use HTTPS on untrusted networks. Configure built-in TLS or terminate TLS at a trusted reverse proxy.
- Set `server.trusted_proxies.additional` to the actual proxy addresses or CIDRs before trusting forwarded headers.
- Keep debug and profiling routes disabled in production. They are enabled only by the explicit debug setting, not by development mode alone.
- Metrics are internal monitoring endpoints and require a per-service bearer token by default. Root aliases use `server.metrics.root.enabled` (enabled by default); keep them behind a firewall or trusted monitoring network. Tokens live in `secrets.yml` as `server.metrics.auth.tokens.prometheus`, `.grafana`, and `.loki`.
- Review rate-limit and security settings, keep the binary current, and protect configuration, database, logs, and backup paths.
- GeoIP is a risk signal and does not replace other security controls; lookup failures fail open.

See [Configuration](configuration.md) for supported settings and [API Reference](api.md) for route behavior.
