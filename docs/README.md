# GitIgnore API Server Documentation

GitIgnore serves an embedded catalog of `.gitignore` templates through a public
REST API, a read-only GraphQL API, a server-rendered web interface, and a
companion CLI. There are no user accounts or admin web panel.

## Guides

- [Main README](../README.md) — overview and quick start
- [Installation](installation.md) — running the server
- [Configuration](configuration.md) — config file, flags, and environment
- [API Reference](API.md) — REST, GraphQL, and compatibility routes
- [CLI Reference](cli.md) — server and client commands
- [Security](security.md) — public access and hardening
- [Integrations](integrations.md) — discovery and protocol endpoints
- [Development](development.md) — local development workflow

## API overview

| Method | Endpoint | Description |
|---|---|---|
| GET | `/api/v1/templates/{name}` | Retrieve one template |
| GET | `/api/v1/list` | List template names |
| GET | `/api/v1/search?q={query}` | Search the catalog |
| GET | `/api/v1/combine?templates={names}` | Combine templates |
| GET | `/api/v1/categories` | List categories |
| GET | `/api/v1/stats` | Template statistics |
| GET / POST | `/graphql` | GraphQL schema / query endpoint |
| GET | `/api/v1/server/swagger` | OpenAPI document |
| GET | `/healthz` | Health check |

See the [API Reference](API.md) for aliases and response formats.
