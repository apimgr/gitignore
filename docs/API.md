# API Reference

All endpoints are public and read only. The canonical REST API is rooted at
`/api/v1`. Template content is embedded in the server binary.

## REST endpoints

| Method | Path | Purpose |
|---|---|---|
| GET | `/api/v1/` | API information |
| GET | `/api/v1/templates/{name}` | Retrieve a template using content negotiation |
| GET | `/api/v1/templates/{name}.txt` | Retrieve raw template content |
| GET | `/api/v1/templates/{name}.json` | Retrieve a template as JSON |
| GET | `/api/v1/list` | List template names |
| GET | `/api/v1/list.txt` | List template names as text |
| GET | `/api/v1/search?q={query}` | Search templates |
| GET | `/api/v1/search.txt?q={query}` | Search templates as text |
| GET | `/api/v1/combine?templates={names}` | Combine comma-separated templates |
| GET | `/api/v1/combine.txt?templates={names}` | Combined content as text |
| GET | `/api/v1/categories` | List categories |
| GET | `/api/v1/categories/{name}` | List templates in a category |
| GET | `/api/v1/categories/{name}.txt` | Category results as text |
| GET | `/api/v1/stats` | Template statistics |
| GET | `/api/v1/stats.txt` | Template statistics as text |
| GET | `/api/v1/templates.json` | Download the catalog as JSON |
| GET | `/api/v1/templates.tar.gz` | Download the catalog archive |
| GET | `/api/v1/server/healthz` | Health check |
| GET | `/api/v1/server/scheduler` | Scheduler status |
| GET | `/api/v1/server/swagger` | OpenAPI document |
| GET | `/api/v1/cli/sh` | Download shell client script |
| GET | `/api/v1/cli/ps` | Download PowerShell client script |
| GET | `/api/v1/cli/completion/{bash,zsh,fish}` | Download shell completion |

REST responses support plain text, JSON, and HTML where applicable. Append
`.txt` or `.json` when an explicit representation is needed.

## GraphQL

The read-only GraphQL API accepts JSON POST requests at `POST /graphql` and
`POST /api/graphql`. Versioned aliases are available at `POST /api/v1/graphql`
and `POST /api/v1/server/graphql`. The executable schema is served as SDL from
any corresponding GET route. The GraphiQL browser page is
`/server/docs/graphql`.

Query fields are `template(name)`, `list`, `search(q)`, `categories`,
`category(name)`, `combine(templates)`, and `stats`.

```graphql
{
  template(name: "Go") {
    name
    content
    category
  }
}
```

Send a JSON request with a `query` string. `operationName` and `variables` are
optional fields.

## OpenAPI and health aliases

OpenAPI JSON is available at `/api/swagger` and `/api/v1/server/swagger`.
Health checks are available at `/healthz`, `/api/healthz`, and
`/api/v1/server/healthz`.

## gitignore.io compatibility

The compatibility routes are `GET /api/list` and `GET /api/{names}`. They
preserve the legacy plain-text response format for existing clients.
