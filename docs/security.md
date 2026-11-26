# Security

## Public service

The catalog and browser pages are public and read-only. The application has no user registration, profile login, or admin web panel. Anonymous owner identities use Bearer tokens; the REST catalog remains public, while MCP and protected owner or operator operations require the corresponding token. Operators configure the service through `server.yml` and the documented command-line maintenance interface. Template names are resolved from the embedded catalog and are never used to construct filesystem paths.

The contact page accepts reports from visitors. It validates field sizes and addresses, requires a single-use challenge, and sends messages only to configured recipients when SMTP is available. Security reports use a separate encrypted-storage and disclosure flow. These submissions are not public catalog data.

Configured webhooks receive summary fields only; submitted contact details and report bodies are never sent to webhook transports. On Tor requests, security pages use only the configured Tor contact address and omit clearnet reporting links and locale hints.

## Security and health endpoints

During critical database or runtime-directory failures, the server stays available in read-only maintenance mode. Health endpoints return HTTP `503` with safe maintenance and retry metadata; write requests return the canonical `MAINTENANCE` response with HTTP `503`. Read-only catalog requests, GraphQL queries, and MCP requests remain available. Recovery retries every `server.maintenance.self_healing.retry_interval` (default `30s`); `max_attempts: 0` means unlimited retries. After a positive limit is exhausted, the server stays read-only until restarted after the cause is fixed. See [Server Operations](SERVER.md#health-and-discovery).

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
| `/api/{api_version}/server/maintenance` | `server.token`-authenticated detailed maintenance status and recovery guidance |
| `/api/{api_version}/server/healthz` | Versioned API health |
| `/api/healthz` and `/api/healthz.txt` | Unversioned API health aliases |
| `/server/metrics`, `/server/metrics/prometheus` | Prometheus exposition; requires the Prometheus bearer token |
| `/server/metrics/grafana` | Grafana dashboard JSON; requires the Grafana bearer token |
| `/server/metrics/loki` | Loki-compatible log stream; requires the Loki bearer token |
| `/api/{api_version}/server/metrics` and `/api/{api_version}/server/metrics/{prometheus,grafana,loki}` | Versioned metrics routes with the matching per-service bearer token |
| `/api/metrics` and `/api/metrics/{prometheus,grafana,loki}` | Unversioned metrics aliases with the matching per-service bearer token |
| `/metrics` and `/metrics/{prometheus,grafana,loki}` | Root metrics aliases when enabled; requires the matching per-service bearer token |
| `/robots.txt` | Crawler directives from configuration |
| `/sitemap.xml` | Dynamically generated public-page map; excludes API, preferences, and contact routes |
| `/feed.xml` and `/rss.xml` | Atom and RSS feeds; empty for the timestamp-free embedded template catalog |

Repository-source vulnerabilities should be reported through [GitHub private vulnerability reporting](https://github.com/apimgr/gitignore/security/advisories/new), as described in [.github/SECURITY.md](https://github.com/apimgr/gitignore/blob/main/.github/SECURITY.md). A deployed-instance vulnerability can be reported through the security form linked from `/server/security` and `security.txt`. Do not file vulnerability details in a public issue.

The security form is enabled by `server.pages.contact.enabled`. It requires the rotating `security_id`, which is valid for the current and previous 48-hour period. The form asks for impact, reproduction steps, severity, and disclosure preferences. Report bodies are encrypted with the project PGP key when available, otherwise with AES-256-GCM using the generated at-rest key. Researcher contact details and any submitted public key are stored separately under AES-256-GCM. Report content is not included in logs or the public status page. The researcher receives a tracking ID and an emailed status token that permits one status-page access per day; access expires 30 days after the report closes. Operators manage report metadata through the `--maintenance security` commands; there is no admin web interface or route for reading report contents.

Operators rotate `installation_secret` or the AES at-rest key with `--maintenance secret rotate installation_secret` or `--maintenance secret rotate encryption_key`. Both commands require the configured operator token or root privileges and require typing `CONFIRM`. Installation-secret rotation re-encrypts the PGP private key and validates in-flight HMACs with the previous secret for seven days. Encryption-key rotation re-encrypts owner-token copies and AES-encrypted report bodies and contacts; PGP-encrypted report bodies are untouched. The previous AES key remains wrapped by the new key for 30 days. Startup resumes interrupted encryption and PGP re-encryption, and daily security cleanup removes expired previous-key material. Secret values are never written to logs.

Browser security reports use a separate flow from vulnerability reports. CSP violations are written to the private `security.log` with query strings, fragments, internal IPs, filesystem URIs, and oversized fields removed. The default Reporting API and NEL endpoints do not echo report data to callers; NEL report bodies are not retained. The `Reporting-Endpoints` and `Report-To` headers advertise `/api/{api_version}/server/reports/default`.

### Project OpenPGP key

The optional project key is managed with `--maintenance pgp generate`, `rotate`, `publish`, `export`, `import`, and `delete`. The private key is encrypted at rest; the public key is served at `/.well-known/pgp-key.asc` when publication is enabled. Stored report bodies use the project PGP key when available and otherwise use AES-256-GCM; a researcher's supplied key is used for the acknowledgment email, which contains no report content. Rotation re-encrypts retained PGP-encrypted reports transactionally and keeps the prior encrypted key for the transition period. Keyserver publication retries on the scheduled maintenance cadence. Private-key operations require operator authorization and typed confirmation; private key material is never exposed by a web endpoint.

The public Contact page also handles abuse, DMCA, and privacy requests. It uses a one-time challenge, request and field size limits, validation, and rate limiting. `GET /api/{api_version}/server/contact` publishes the form field descriptors and a fresh challenge without disclosing the destination address. The POST flow has a review step; a message is sent only after the confirmation step. Configure a recipient and SMTP before expecting delivery.

## Owner data and audit log

The owner-token page lets an owner export or remove their identity by using the CLI. Operators can list token prefixes and metadata, revoke linked tokens, or revoke a primary token to delete its owner and all linked tokens. These maintenance commands require root or the configured `server.token`; confirmations require typing the token prefix or owner ID unless `--yes` is supplied. Exports contain token metadata only, never token values, hashes, or encrypted token material, and are created with mode `0600`.

Audit events are stored as JSON Lines in `audit.log` and copied into the relational `audit_log` table. Each line includes the SHA-256 hash of the prior exact line in `prev`; `audit.anchor` carries the last hash across daily rotation and is stored with mode `0600`. Startup verifies the active chain, reports broken entries without stopping the server, and daily rotation applies the audit retention and compression settings. Compliance reports count events in the active and retained rotated logs. Operators can inspect the integrity result with `--maintenance compliance report`.

The request-flood detector counts requests by client and route class over a sliding window, including requests already rejected by a rate limiter. After the configured multiplier is exceeded, it records a temporary IP block in the database and emits `abuse.request_flood`; the block expires automatically. Addresses on the configured allowlist bypass rate limits and temporary blocks. Optional security alert email uses the existing contact delivery configuration. Permanent blocked IPs, blocklist matches, and country-based denials also produce security audit events.

## Debug and profiling endpoints

The `/debug/*` routes exist only when `--debug` or `DEBUG=true` is explicitly enabled. Every endpoint requires the `server.token` as an `Authorization: Bearer …` credential, including pprof and expvar. The pprof command-line endpoint and expvar `cmdline` value expose only the binary name, never process arguments. Debug mode alone does not expose these routes in development or production.

## Deployment protections

- Use HTTPS on untrusted networks. Configure built-in TLS or terminate TLS at a trusted reverse proxy.
- Set `server.trusted_proxies.additional` to the actual proxy addresses or CIDRs before trusting forwarded headers.
- Keep debug and profiling routes disabled in production. They are enabled only by the explicit debug setting, not by development mode alone.
- Metrics are internal monitoring endpoints and require a per-service bearer token by default. Root aliases use `server.metrics.root.enabled` (enabled by default); keep them behind a firewall or trusted monitoring network. Tokens live in `secrets.yml` as `server.metrics.auth.tokens.prometheus`, `.grafana`, and `.loki`.
- Review rate-limit and security settings, keep the binary current, and protect configuration, database, logs, and backup paths.
- GeoIP is a risk signal and does not replace other security controls; lookup failures fail open.

See [Configuration](configuration.md) for supported settings and [API Reference](api.md) for route behavior.
