# gitignore

GitIgnore API Server is a full-stack Go web application for serving `.gitignore`
templates. It provides a curated library of templates for languages, frameworks,
editors, and tools through a versioned REST API, a GraphQL endpoint, and a
server-side rendered web UI with a browser and composer. Templates are embedded
in the binary at build time, and a companion CLI tool enables shell-pipeline use.
It ships as a single self-contained static binary.

## Quick Start

```bash
# Docker
docker run --name gitignore -p 127.0.0.1:8080:8080 ghcr.io/apimgr/gitignore:latest

# Binary
./gitignore-linux-amd64 --port 8080
```

Fetch a template:

```bash
curl http://localhost:8080/api/v1/templates/go.txt
```

## Features

- Curated `.gitignore` templates embedded in the binary
- Versioned REST API with plain-text, JSON, and HTML content negotiation
- GraphQL endpoint with an interactive playground
- Server-side rendered web UI: browser, template viewer, and combiner/composer
- Companion CLI (`gitignore-cli`) for shell pipelines
- Health, metrics, security.txt, PWA manifest, and Swagger/GraphQL docs endpoints

## Documentation

- [Installation](installation.md) - How to install and run
- [Configuration](configuration.md) - All configuration options
- [API Reference](api.md) - REST API, Swagger, GraphQL
- [CLI Reference](cli.md) - Server flags and the companion client
- [Security](security.md) - Auth, public endpoints, reporting, and hardening
- [Integrations](integrations.md) - Discovery and platform integrations
- [Development](development.md) - Contributing guide

## Links

- [Repository](https://github.com/apimgr/gitignore)
- [Swagger UI](/server/docs/swagger)
- [GraphQL Playground](/server/docs/graphql)

## License

MIT - See [LICENSE.md](https://github.com/apimgr/gitignore/blob/main/LICENSE.md)
