# Implementation Alignment Tasks

- [ ] Finish server contact and security reporting conformance with AI.md PARTS 11, 12, 14, and 16: PGP key management/report encryption remains absent; the security maintenance workflow now supports root or a hidden `server.token` prompt and covers metadata, but still needs remaining lifecycle checks and verification.
- [ ] Complete server page and API conformance, including shared routing, content negotiation, security headers, translations, and accessibility.
- [ ] Align configuration, database, logging, metrics, GeoIP, scheduler, backup, update, service, Tor/I2P, and operational behavior with their applicable AI.md PARTS; I2P runtime/config/health/status integration is currently absent.
- [ ] Replace the client GUI stub with the required pure-Go gogpu/ui implementation; implement GUI auto-detection, configuration override, and parity with the TUI workflows.
- [ ] Align build, Docker, CI/CD, tests, ReadTheDocs, and supported platform outputs with AI.md PARTS 25–30.
- [ ] Synchronize README, all docs pages, server-rendered page content, OpenAPI/GraphQL output, and translations with the implemented behavior.
