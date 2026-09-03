## Project description

Gitignore is a full-stack Go web application for serving `.gitignore` templates. It provides a curated library of templates for languages, frameworks, editors, and tools through a versioned REST API, GraphQL endpoint, and a server-side rendered web UI with a browser and composer. Templates are embedded in the binary at build time. A companion CLI tool enables shell-pipeline use. Deployed as a single self-contained static binary.

## Project variables

project_name: gitignore
project_org: apimgr
internal_name: gitignore
internal_org: apimgr
app_name: GitIgnore API
repo: https://github.com/apimgr/gitignore
license: MIT
binary: gitignore
client_binary: gitignore-cli

## Business logic

### Product scope & non-goals

**In scope:**
- Curated `.gitignore` templates for languages (Go, Rust, Python, Node, etc.), editors (VS Code, JetBrains, Vim, etc.), frameworks, and OS patterns
- Single template retrieval by name (case-insensitive)
- Template composition: merge multiple templates into one output
- List all available template names
- Full web frontend (browser, search, composer) using server-side Go templates, dark/light/auto theme, PWA, mobile-first
- Server pages: `/server/about`, `/server/help`, `/server/healthz`, `/server/privacy`, `/server/terms`
- CLI client (`gitignore-cli`) for shell-pipeline use: `gitignore-cli Go Node > .gitignore`
- OpenAPI/Swagger docs at `/api/{api_version}/server/swagger`
- GraphQL at `/graphql`
- gitignore.io route/API compatibility layer (see "External API Compatibility" below) — this project is a full drop-in replacement for gitignore.io/toptal's gitignore API

**Non-goals:**
- No user accounts, registration, or login of any kind
- No admin web panel (server configured via `server.yml` only)
- No user-submitted or community templates (curated dataset only, updated via releases)
- No paid tiers, no API keys, no rate-limited access tiers

### Roles & permissions

There are no user roles. All endpoints are public and require no authentication.

| Actor | Access |
|-------|--------|
| **Anonymous visitor (browser)** | Full read access to all web pages and API endpoints |
| **Anonymous API client (curl/CLI)** | Full read access to all API endpoints |
| **Server operator** | Configures server via `server.yml` only; no web management interface |

### Data model & sensitivity

**Template record** (embedded at build time, no PII):

| Field | Type | Sensitivity |
|-------|------|-------------|
| `name` | string — template identifier (e.g., `Go`) | Public |
| `fileName` | string — source filename (e.g., `Go.gitignore`) | Public |
| `content` | string — raw `.gitignore` template text | Public |
| `tags` | string[] — category tags (language, editor, os, framework) | Public |

No PII stored or served.

### Trust boundaries & external services

| Boundary | Trust level | Notes |
|----------|-------------|-------|
| Template dataset (embedded at build) | Fully trusted | Static, compiled into binary |
| Incoming HTTP requests | **Untrusted** | Template names validated against known list only |

No external services called at runtime.

### Threat model & abuse cases

**Primary assets:** service availability.

**Attacker/abuser goals:**
- DoS via high-rate requests
- Path traversal via template name parameter (e.g., `../etc/passwd`) — mitigated by lookup against known name list only, no filesystem access

**Defenses:**
- Rate limiting on all endpoints
- Template name lookup is against an in-memory map — no filesystem path construction from user input
- No user accounts eliminates credential stuffing and privilege escalation entirely

### Security decisions & exceptions

- **No authentication on any endpoint**: intentional. Public read-only reference API.
- **All responses include `Access-Control-Allow-Origin: *`**: intentional. Public data API designed for cross-origin browser use.
- **External route compatibility (gitignore.io)**: intentional. This app is a full replacement for gitignore.io — existing scripts, editor plugins, and shell functions written against gitignore.io's API must work unmodified against this server. See "External API Compatibility" below.

### External API Compatibility

**Compatibility target: gitignore.io (toptal.com/developers/gitignore).** This is explicit **route/API compatibility**, not just feature compatibility — the exact external paths, query params, status codes, and response bodies are reproduced verbatim, mounted alongside (not instead of) our own `/api/{api_version}/*` namespace. Behavior verified against the live service on 2026-07-19.

**Compatible routes (unversioned, no `{api_version}` prefix — mounted at the literal gitignore.io paths):**

| Route | Method | Behavior |
|-------|--------|----------|
| `/api/list` | GET | `text/plain; charset=utf-8`, 200. Comma-separated list of all template keys, alphabetically sorted, wrapped across lines for readability. Equivalent to `format=lines` (the gitignore.io default). |
| `/api/list?format=lines` | GET | Identical to `/api/list` with no query param. |
| `/api/list?format=json` | GET | `application/json; charset=utf-8`, 200. Object keyed by lowercase template key: `{"<key>": {"key", "name", "fileName", "contents"}}` for every template. |
| `/api/{name1,name2,...}` | GET | `text/plain; charset=utf-8`, 200 if at least the first name resolves. Body: `# Created by https://<host>/api/{list}` header line, `# Edit at https://<host>/api?templates={list}` line, blank line, then one `### {Name} ###\n{contents}` block per resolved template (in request order), then a blank line and `# End of https://<host>/api/{list}` footer. Template name matching is case-insensitive, reusing the same lookup as our own `/api/{api_version}/templates/{name}` route — no separate dataset. |
| `/api/{unknown}` | GET | `text/plain; charset=utf-8`, 404. Same header/footer wrapper as above, with `#!! ERROR: {name} is undefined. Use list command to see defined gitignore types !!#` in place of the missing template's block. Unresolved names inside an otherwise-valid list get their own `#!! ERROR: ... !!#` line; resolved names in the same request still render normally. |

**What IS compatible:** the four routes above, byte-for-byte body shape and status codes, `text/plain`/`application/json` content types, case-insensitive template names, comma-separated multi-template requests.

**What is NOT compatible (intentionally out of scope):** gitignore.io's web UI routes (`/`, `?templates=...`), its Slack/analytics integrations, and any endpoint not listed above. Our own richer API (`/api/{api_version}/templates`, composer, GraphQL) remains the canonical, documented interface — the gitignore.io routes exist purely as a compatibility shim for existing external tooling.
