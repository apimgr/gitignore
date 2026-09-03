# API Reference

## Content Negotiation

Every API endpoint negotiates its response format:

- **Plain text** (default): raw `.gitignore` content
- **JSON**: send `Accept: application/json` or append `.json` to the path
- **Text**: append `.txt` to force plain text

## REST API

Base URL: `/api/v1/`

### Template Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/v1/` | GET | API information |
| `/api/v1/templates/{name}` | GET | Fetch a template (negotiated) |
| `/api/v1/templates/{name}.txt` | GET | Fetch a template as plain text |
| `/api/v1/templates/{name}.json` | GET | Fetch a template as JSON |
| `/api/v1/list` | GET | List all templates |
| `/api/v1/list.txt` | GET | List all templates as plain text |
| `/api/v1/search` | GET | Search templates (`?q=`) |
| `/api/v1/combine` | GET | Combine templates (`?templates=go,node`) |
| `/api/v1/categories` | GET | List categories |
| `/api/v1/categories/{name}` | GET | Templates in a category |
| `/api/v1/stats` | GET | Template and server statistics |

Each of the list/search/combine/categories/stats endpoints also has a `.txt`
variant for plain-text output.

### Health & Discovery Aliases

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/healthz` | GET | Health check |
| `/api/healthz` | GET | Health check alias |
| `/api/v1/server/healthz` | GET | Versioned health check |
| `/api/autodiscover` | GET | Client autodiscovery document |

## Swagger UI

- Interactive UI: [/server/docs/swagger](/server/docs/swagger)
- Raw OpenAPI JSON: `/api/swagger` and `/api/v1/server/swagger`

## GraphQL

- Playground: [/server/docs/graphql](/server/docs/graphql)
- Endpoint: `POST /api/graphql` (also `/api/v1/server/graphql`)
- Schema (SDL): `GET /api/graphql`

Example query:

```graphql
{
  template(name: "go") {
    name
    content
  }
}
```
