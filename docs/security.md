# Security

## Public service

The catalog and browser pages are public and read-only. The application has no user registration, profile login, or admin web panel. Anonymous owner identities use Bearer tokens; the REST catalog remains public, while MCP and protected owner or operator operations require the corresponding token. Operators configure the service through `server.yml` and the documented command-line maintenance interface. Template names are resolved from the embedded catalog and are never used to construct filesystem paths.

The contact page accepts reports from visitors. It validates field sizes and addresses, requires a single-use challenge, and sends messages only to configured recipients when SMTP is available. Security reports use a separate encrypted-storage and disclosure flow. These submissions are not public catalog data.

## Security and health endpoints

| Endpoint | Purpose |
|---|---|
| `/.well-known/security.txt` | Machine-readable security contacts and expiry |
| `/.well-known/pgp-key.asc` | Public encryption key when one has been generated |
| `/security.txt` | Security contact alias |
| `/server/security` | Human-readable reporting channels and security information |
| `/server/security/policy` | Coordinated disclosure policy |
| `/server/security/thanks` | Public acknowledgments for credited researchers |
| `/server/contact?security_id={id}` | Security report form using the current rotating token |
| `/server/security/report/{tracking_id}` | Private report status, protected by the emailed one-shot token |
| `/api/{api_version}/server/security` | JSON mirror of public security contact metadata |
| `/api/{api_version}/server/contact` | Contact form description (GET) and confirmed submission (POST) |
| `/server/healthz` | Canonical HTML/API health endpoint, negotiated by request |
| `/api/{api_version}/server/healthz` | Versioned API health |
| `/api/healthz` and `/api/healthz.txt` | Unversioned API health aliases |
| `/robots.txt` | Crawler directives from configuration |

Repository-source vulnerabilities should be reported through [GitHub private vulnerability reporting](https://github.com/apimgr/gitignore/security/advisories/new), as described in [.github/SECURITY.md](https://github.com/apimgr/gitignore/blob/main/.github/SECURITY.md). A deployed-instance vulnerability can be reported through the security form linked from `/server/security` and `security.txt`. Do not file vulnerability details in a public issue.

The security form is enabled by `server.pages.contact.enabled`. It requires the rotating `security_id`, which is valid for the current and previous 48-hour period. The form asks for impact, reproduction steps, severity, and disclosure preferences. Reports are encrypted before persistence; report content is not included in logs or the public status page. The researcher receives a tracking ID and a one-shot status link. Operators manage report metadata through the `--maintenance security` commands; there is no admin web interface or route for reading report contents.

### Project OpenPGP key

The optional project key is managed with `--maintenance pgp generate`, `rotate`, `publish`, `export`, `import`, and `delete`. The private key is encrypted at rest; the public key is served at `/.well-known/pgp-key.asc` when publication is enabled. Security submissions are encrypted to the researcher's supplied key or the project key when available. Rotation re-encrypts retained reports transactionally and keeps the prior encrypted key for the configured transition period. Keyserver publication retries on the scheduled maintenance cadence. Private-key operations require operator authorization and typed confirmation; private key material is never exposed by a web endpoint.

The public Contact page also handles abuse, DMCA, and privacy requests. It uses a one-time challenge, request and field size limits, validation, and rate limiting. `GET /api/{api_version}/server/contact` publishes the form field descriptors and a fresh challenge without disclosing the destination address. The POST flow has a review step; a message is sent only after the confirmation step. Configure a recipient and SMTP before expecting delivery.

## Deployment protections

- Use HTTPS on untrusted networks. Configure built-in TLS or terminate TLS at a trusted reverse proxy.
- Set `server.trusted_proxies.additional` to the actual proxy addresses or CIDRs before trusting forwarded headers.
- Keep debug and profiling routes disabled in production. They are enabled only by the explicit debug setting, not by development mode alone.
- `/metrics` is enabled by default. Restrict it to monitoring systems and configure its bearer token when appropriate.
- Review rate-limit and security settings, keep the binary current, and protect configuration, database, logs, and backup paths.
- GeoIP is a risk signal and does not replace other security controls; lookup failures fail open.

See [Configuration](configuration.md) for supported settings and [API Reference](api.md) for route behavior.
