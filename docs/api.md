# API Reference

All API routes are public. The canonical REST base is `/api/v1` by default; the configured `server.api_version` value controls that segment. Template content is embedded in the server binary.

## Template REST API

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/v1/` | GET | API and endpoint information |
| `/api/v1/templates/{name}` | GET | Retrieve a template using content negotiation |
| `/api/v1/templates/{name}.txt` | GET | Retrieve raw template text |
| `/api/v1/templates/{name}.json` | GET | Retrieve a template as JSON |
| `/api/v1/list` | GET | List template names |
| `/api/v1/list.txt` | GET | List template names as text |
| `/api/v1/search?q={query}` | GET | Search template names and content |
| `/api/v1/search.txt?q={query}` | GET | Search results as text |
| `/api/v1/combine?templates={names}` | GET | Combine comma-separated templates |
| `/api/v1/combine.txt?templates={names}` | GET | Combined template text |
| `/api/v1/categories` | GET | List categories |
| `/api/v1/categories.txt` | GET | List categories as text |
| `/api/v1/categories/{name}` | GET | List templates in a category |
| `/api/v1/categories/{name}.txt` | GET | Category results as text |
| `/api/v1/stats` | GET | Template and server statistics |
| `/api/v1/stats.txt` | GET | Statistics as text |
| `/api/v1/templates.json` | GET | Export the full catalog as JSON |
| `/api/v1/templates.tar.gz` | GET | Download the full catalog as a gzip tar archive |

Where supported, send `Accept: application/json` for JSON; `.txt` and `.json` suffixes request an explicit representation.

## Server and client endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/v1/server/healthz` | GET | Content-negotiated health information |
| `/api/v1/server/scheduler` | GET | Read-only scheduler status |
| `/api/v1/server/swagger` | GET | OpenAPI JSON |
| `/api/v1/server/graphql` | GET / POST | GraphQL schema / query endpoint |
| `/api/v1/graphql` | GET / POST | Versioned GraphQL alias |
| `/api/v1/cli/sh` | GET | Generated POSIX shell helper |
| `/api/v1/cli/ps` | GET | Generated PowerShell helper |
| `/api/v1/cli/completion/bash` | GET | Bash completion script |
| `/api/v1/cli/completion/zsh` | GET | Zsh completion script |
| `/api/v1/cli/completion/fish` | GET | Fish completion script |

## Health and discovery aliases

`/server/healthz` is the canonical browser-facing health page. The root `/healthz` alias is disabled unless `server.healthz.root.enabled` is true. `/api/healthz` and `/api/healthz.txt` are always available. API health responses support JSON and plain text; browser requests receive HTML.

`GET /api/autodiscover` returns server name, version, build, API version/base, and the OpenAPI, GraphQL, and health endpoints.

## gitignore.io compatibility

The unversioned compatibility routes support existing gitignore.io clients:

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/list` | GET | Comma-separated, alphabetized template keys; `?format=json` returns the compatible JSON catalog |
| `/api/{name1,name2,...}` | GET | Concatenated template text in request order; unknown names produce compatibility error blocks and a 404 if the first name is unknown |

## GraphQL

The read-only GraphQL API accepts JSON POST requests at `/graphql`, `/api/graphql`, `/api/v1/graphql`, and `/api/v1/server/graphql`. GET on these endpoints returns the schema. The GraphiQL page is `/server/docs/graphql`.

Available query fields include `template(name)`, `list`, `search(q)`, `categories`, `category(name)`, `combine(templates)`, and `stats`.

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

The Swagger UI is at `/server/docs/swagger`. Raw OpenAPI JSON is served at `/api/swagger` and `/api/v1/server/swagger`.
