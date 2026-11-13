# GitIgnore API Server Documentation

GitIgnore serves an embedded catalog of `.gitignore` templates through a public REST API, read-only GraphQL API, server-rendered browser interface, and companion CLI. It has no user accounts or admin web panel.

## Guides

- [Main README](../README.md) — overview and quick start
- [Installation](installation.md) — running the server
- [Configuration](configuration.md) — config file, environment, and flags
- [API Reference](api.md) — REST, GraphQL, health, and compatibility routes
- [CLI Reference](cli.md) — server and client commands
- [Security](security.md) — public access, endpoints, and deployment
- [Integrations](integrations.md) — discovery and protocol endpoints
- [Development](development.md) — build and test workflow
- [Translation](translating.md) — locale contribution guide

## API overview

| Method | Endpoint | Description |
|---|---|---|
| GET | `/api/v1/templates/{name}` | Retrieve a template |
| GET | `/api/v1/list` | List templates |
| GET | `/api/v1/search?q={query}` | Search templates |
| GET | `/api/v1/combine?templates={names}` | Combine templates |
| GET | `/api/v1/categories` | List categories |
| GET | `/api/v1/stats` | Show catalog statistics |
| GET / POST | `/graphql` | GraphQL schema / query endpoint |
| GET | `/api/v1/server/swagger` | OpenAPI JSON |
| GET | `/server/healthz` | Browser health page and negotiated health API |

See the [API reference](api.md) for the full route list and aliases.
