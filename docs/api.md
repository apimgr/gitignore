# API Reference

The catalog is public and read-only. Canonical API routes use `/api/{api_version}`; the default version is `v1`. The exact configured base path is shown by `/api/autodiscover`. Template files and locale catalogs are embedded in the binaries.

## Template API

| Endpoint | Method | Purpose |
|---|---|---|
| `/api/{api_version}/` | GET | API information and endpoint links |
| `/api/{api_version}/templates/{name}` | GET | Get a template using content negotiation |
| `/api/{api_version}/templates/{name}.txt` | GET | Get raw template text |
| `/api/{api_version}/templates/{name}.json` | GET | Get one template as JSON |
| `/api/{api_version}/list` | GET | List template names |
| `/api/{api_version}/list.txt` | GET | List template names as text |
| `/api/{api_version}/search?q={query}` | GET | Search template names and content |
| `/api/{api_version}/search.txt?q={query}` | GET | Search results as text |
| `/api/{api_version}/combine?templates={names}` | GET | Combine comma-separated template names |
| `/api/{api_version}/combine.txt?templates={names}` | GET | Combined template text |
| `/api/{api_version}/categories` | GET | List catalog categories |
| `/api/{api_version}/categories.txt` | GET | List categories as text |
| `/api/{api_version}/categories/{name}` | GET | List templates in a category |
| `/api/{api_version}/categories/{name}.txt` | GET | List templates in a category as text |
| `/api/{api_version}/stats` | GET | Catalog and server statistics |
| `/api/{api_version}/stats.txt` | GET | Catalog and server statistics as text |
| `/api/{api_version}/templates.json` | GET | Export the catalog as JSON |
| `/api/{api_version}/templates.tar.gz` | GET | Download the catalog as a gzip tar archive |

Text endpoints also have `.txt` representations where listed by the server. Send `Accept: application/json` to request JSON when a route supports negotiation. Template identifiers are case-insensitive and resolved from the embedded catalog; request values are never treated as filesystem paths.

## Server endpoints

| Endpoint | Method | Purpose |
|---|---|---|
| `/api/{api_version}/server/about` | GET | About-page source and format |
| `/api/{api_version}/server/privacy` | GET | Privacy policy data |
| `/api/{api_version}/server/contact` | GET | Public form fields, validation limits, and a fresh challenge; does not reveal recipients |
| `/api/{api_version}/server/contact` | POST | Submit a contact report through review and confirmation; delivery occurs only after confirmation |
| `/api/{api_version}/server/help` | GET | Help content and registered help-page links |
| `/api/{api_version}/server/help/mcp` | GET | MCP help and endpoint metadata |
| `/api/{api_version}/server/dpo` | GET | Data protection officer contact |
| `/api/{api_version}/server/preferences` | GET / POST | Read and update public browser preferences |
| `/api/{api_version}/server/preferences/tokens` | GET / POST | List and create linked owner tokens; requires the owner's Bearer token |
| `/api/{api_version}/server/preferences/tokens/primary` | POST | Issue a primary owner token on request; does not create a resource |
| `/api/{api_version}/server/preferences/tokens/primary/reveal` | POST | Reveal the primary token; requires the primary Bearer token |
| `/api/{api_version}/server/preferences/tokens/primary/rotate` | POST | Rotate the primary token |
| `/api/{api_version}/server/preferences/tokens/{id}/reveal` | POST | Reveal a linked token; requires the owner's Bearer token |
| `/api/{api_version}/server/preferences/tokens/{id}/rotate` | POST | Rotate a linked token; requires the owner's Bearer token |
| `/api/{api_version}/server/preferences/tokens/{id}` | PATCH / DELETE | Update or revoke one linked token |
| `/api/{api_version}/server/preferences/sync` | GET | Export preferences for cross-device sync |
| `/api/{api_version}/server/preferences/sync/import` | GET | Import synchronized preferences |
| `/api/{api_version}/server/terms` | GET | Terms content and format |
| `/api/{api_version}/server/security` | GET | Security contacts, key availability, expiry, and public policy links |
| `/api/{api_version}/server/security/policy` | GET | Disclosure policy |
| `/api/{api_version}/server/security/thanks` | GET | Public researcher acknowledgments |
| `/api/{api_version}/server/healthz` | GET | Health status; JSON by default, with supported text negotiation |
| `/api/{api_version}/server/healthz.txt` | GET | Health status as text |
| `/api/{api_version}/server/scheduler` | GET | Read-only scheduler status |
| `/api/{api_version}/server/metrics` | GET | Prometheus exposition; requires the Prometheus metrics bearer token |
| `/api/{api_version}/server/metrics/prometheus` | GET | Prometheus exposition; requires the Prometheus metrics bearer token |
| `/api/{api_version}/server/metrics/grafana` | GET | Grafana dashboard JSON; requires the Grafana metrics bearer token |
| `/api/{api_version}/server/metrics/loki` | GET | Loki-compatible log stream; requires the Loki metrics bearer token |
| `/api/{api_version}/server/swagger` | GET | OpenAPI document |
| `/api/{api_version}/server/graphql` | POST | GraphQL query endpoint |
| `/api/{api_version}/server/mcp` | POST | Stateless MCP JSON-RPC endpoint; requires the same Bearer token as the REST API and exposes only operations authorized for that token |
| `/api/{api_version}/cli/sh` | GET | Generated POSIX shell helper |
| `/api/{api_version}/cli/ps` | GET | Generated PowerShell helper |
| `/api/{api_version}/cli/completion/{bash,zsh,fish}` | GET | Shell completions |

Native API responses use the documented JSON envelope and errors defined in AI.md PART 14. Health endpoints and external compatibility endpoints follow their specified response formats. The OpenAPI UI is `/server/docs/swagger`; the GraphiQL interface is `/server/docs/graphql`.

## Contact and security reports

The HTML contact form is `/server/contact`. The JSON form description is obtained with `GET /api/{api_version}/server/contact`; the response includes field descriptors and a new captcha challenge, but no recipient address. A submission is reviewed first and delivered only after the confirmation request repeats the fields with its review token. A captcha proof and form token are required. Invalid or expired proofs must be replaced by fetching the form description again.

Security reporting for a deployed instance starts from `/.well-known/security.txt` or `/server/security`. The security form is `/server/contact?security_id={id}` and accepts only a currently valid rotating ID. A successful report receives a tracking ID. Its status link is `/server/security/report/{tracking_id}` and requires the emailed token, which permits one access per day and expires 30 days after the report closes. Repository-source vulnerabilities should use the private reporting instructions in `.github/SECURITY.md`.

## Health and discovery aliases

`/server/healthz` is the canonical browser health page. `/healthz` is an optional direct alias controlled by `server.healthz.root.enabled`. The unversioned `/api/healthz` and `/api/healthz.txt` aliases expose API health. Metrics aliases include `/api/metrics`, `/api/metrics/{prometheus,grafana,loki}`, and the optional root `/metrics` aliases. `GET /api/autodiscover` is intentionally unversioned so clients can discover the configured API version and base path before making versioned requests.

## Gitignore.io compatibility

The unversioned compatibility routes reproduce the gitignore.io API paths alongside the native versioned API:

| Endpoint | Method | Response |
|---|---|---|
| `/api/list` | GET | Alphabetically sorted, comma-separated keys wrapped across lines; `?format=lines` is equivalent |
| `/api/list?format=json` | GET | Compatible JSON catalog keyed by lowercase template key |
| `/api/{name1,name2,...}` | GET | Plain-text template blocks in request order |

Template names are matched case-insensitively. Unknown names produce the compatibility error block; the response is `404` when the first name is unknown, while resolved names in the same request still render. See the [IDEA.md file in the source repository](https://github.com/apimgr/gitignore/blob/main/IDEA.md) for the byte-level compatibility contract and the intentionally unsupported gitignore.io web UI and integrations.

## GraphQL

The read-only GraphQL endpoint accepts JSON POST requests at `/api/graphql` and `/api/{api_version}/server/graphql`. The browser playground is `/server/docs/graphql`. Common query fields include `template(name)`, `list`, `search(q)`, `categories`, `category(name)`, `combine(templates)`, and `stats`.

MCP uses stateless Streamable HTTP JSON-RPC at `/api/{api_version}/server/mcp`, with direct aliases at `/api/mcp` and `/mcp`. Every request requires `Authorization: Bearer <token>`; missing or invalid credentials receive `401`. Requests use protocol revision `2026-07-28` and include matching `MCP-Protocol-Version` and `Mcp-Method` headers; calls to named tools, resources, or prompts also include `Mcp-Name`. The endpoint validates `Origin` before credentials and protocol headers. It uses the REST API's token validation and per-operation authorization. `tools/list` includes only operations allowed to the caller. Token-management and token-issuance operations are never tools. Use a separate linked token for each MCP client; create and manage linked tokens at `/server/preferences/tokens`. The endpoint accepts POST only. See the in-app `/server/help/mcp` page for connection details.

Guest preferences are cookie-only. `GET /api/{api_version}/server/preferences` reads the current values, `POST` applies a partial update, and the sync endpoints export or import a canonical URL-safe preference set. The web UI is at `/server/preferences` and `/server/preferences/sync`; consent, CCPA, and credentials are never included in sync data.

```graphql
{
  template(name: "Go") {
    name
    content
    category
  }
}
```

## OpenAPI

The interactive Swagger UI is `/server/docs/swagger`. The raw OpenAPI JSON is served at `/api/swagger` and `/api/{api_version}/server/swagger`.
