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
