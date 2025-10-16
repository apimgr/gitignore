# 📡 GitIgnore API Documentation

Complete API reference for GitIgnore API Server v1.0

## Base URL

```
http://localhost:PORT/api/v1
```

## Content Negotiation

The API automatically detects the desired response format:

- **Plain Text** (default): Returns raw .gitignore content
- **JSON**: Set `Accept: application/json` header or add `.json` to endpoint
- **HTML**: Detected automatically for web browsers

## Authentication

Public endpoints require no authentication. Admin endpoints require either:

- **Bearer Token**: `Authorization: Bearer YOUR_TOKEN`
- **Basic Auth**: `Authorization: Basic base64(username:password)`

---

## Public Endpoints

### Health Check

#### GET /healthz

Check server health status.

**Response (JSON)**:
```json
{
  "status": "healthy",
  "version": "v1.0.0",
  "timestamp": "2024-01-01T12:00:00Z"
}
```

**Response (Text)**:
```
healthy
```

---

### List Templates

#### GET /api/v1/list

List all available templates.

**Response (JSON)**:
```json
{
  "success": true,
  "data": {
    "templates": ["Go", "Python", "Node", "Java", ...],
    "count": 500
  },
  "timestamp": "2024-01-01T12:00:00Z"
}
```

**Response (Text)**:
```
Go
Python
Node
Java
...
```

---

### Get Template

#### GET /api/v1/template/:name

Get a specific template by name.

**Parameters**:
- `name` (path): Template name (e.g., "Go", "Python")

**Response (Plain Text)**:
```gitignore
# Binaries for programs and plugins
*.exe
*.exe~
*.dll
*.so
*.dylib
...
```

**Response (JSON)**:
```json
{
  "success": true,
  "data": {
    "name": "Go",
    "content": "# Binaries for programs...",
    "category": "Languages",
    "size": 123
  },
  "timestamp": "2024-01-01T12:00:00Z"
}
```

**Errors**:
- `404`: Template not found

---

### Search Templates

#### GET /api/v1/search

Search for templates by query.

**Query Parameters**:
- `q` (string, required): Search query
- `category` (string, optional): Filter by category
- `limit` (int, optional): Maximum results (default: 50)
- `offset` (int, optional): Pagination offset (default: 0)

**Example**:
```bash
curl "http://localhost:8080/api/v1/search?q=python&limit=10"
```

**Response (JSON)**:
```json
{
  "success": true,
  "data": {
    "results": [
      {
        "name": "Python",
        "category": "Languages",
        "score": 100
      },
      {
        "name": "JupyterNotebooks",
        "category": "Languages",
        "score": 75
      }
    ],
    "count": 2,
    "query": "python"
  },
  "timestamp": "2024-01-01T12:00:00Z"
}
```

**Response (Text)**:
```
Python
JupyterNotebooks
```

---

### Combine Templates

#### GET /api/v1/combine

Combine multiple templates into one.

**Query Parameters**:
- `templates` (string, required): Comma-separated template names
- `format` (string, optional): Output format (`text`, `json`)

**Example**:
```bash
curl "http://localhost:8080/api/v1/combine?templates=Go,Python,VSCode"
```

**Response (Plain Text)**:
```gitignore
################################
# Combined .gitignore
# Templates: Go, Python, VSCode
# Generated: 2024-01-01T12:00:00Z
################################

### Go ###
# Binaries for programs and plugins
...

### Python ###
# Byte-compiled / optimized / DLL files
...

### VSCode ###
.vscode/
...
```

**Response (JSON)**:
```json
{
  "success": true,
  "data": {
    "templates": ["Go", "Python", "VSCode"],
    "content": "################################...",
    "size": 1234
  },
  "timestamp": "2024-01-01T12:00:00Z"
}
```

**Errors**:
- `400`: Missing templates parameter
- `404`: One or more templates not found

---

### Categories

#### GET /api/v1/categories

List all template categories.

**Response (JSON)**:
```json
{
  "success": true,
  "data": {
    "categories": [
      {
        "name": "Languages",
        "count": 150
      },
      {
        "name": "Global",
        "count": 50
      },
      {
        "name": "IDEs",
        "count": 30
      }
    ]
  },
  "timestamp": "2024-01-01T12:00:00Z"
}
```

---

#### GET /api/v1/category/:name

Get all templates in a category.

**Parameters**:
- `name` (path): Category name

**Response (JSON)**:
```json
{
  "success": true,
  "data": {
    "category": "Languages",
    "templates": ["Go", "Python", "Java", "JavaScript", ...],
    "count": 150
  },
  "timestamp": "2024-01-01T12:00:00Z"
}
```

---

### Statistics

#### GET /api/v1/stats

Get server statistics.

**Response (JSON)**:
```json
{
  "success": true,
  "data": {
    "total_templates": 500,
    "categories": 10,
    "version": "v1.0.0",
    "uptime_seconds": 3600
  },
  "timestamp": "2024-01-01T12:00:00Z"
}
```

---

### CLI Scripts

#### GET /api/v1/cli/sh

Download POSIX shell (bash/zsh) CLI script.

**Query Parameters**:
- `defaults` (string, optional): Default templates (default: "linux,macos,windows")

**Response**: Shell script with embedded server URL

---

#### GET /api/v1/cli/ps

Download PowerShell CLI script.

**Query Parameters**:
- `defaults` (string, optional): Default templates (default: "windows,visualstudio,vscode")

**Response**: PowerShell script with embedded server URL

---

### Shell Completion

#### GET /api/v1/cli/completion/bash

Download Bash completion script.

**Response**: Bash completion script

---

#### GET /api/v1/cli/completion/zsh

Download Zsh completion script.

**Response**: Zsh completion script

---

#### GET /api/v1/cli/completion/fish

Download Fish completion script.

**Response**: Fish completion script

---

### Documentation

#### GET /api/v1/docs/swagger

Get OpenAPI/Swagger specification.

**Response (JSON)**:
```json
{
  "openapi": "3.0.0",
  "info": {
    "title": "GitIgnore API",
    "version": "1.0.0"
  },
  "paths": { ... }
}
```

---

#### GET /api/v1/docs/graphql

Get GraphQL schema.

**Response (GraphQL Schema)**:
```graphql
type Query {
  templates: [Template!]!
  template(name: String!): Template
  search(query: String!): [Template!]!
  categories: [Category!]!
}

type Template {
  name: String!
  content: String!
  category: String!
}
```

---

## Admin Endpoints

All admin endpoints require authentication.

### Admin Info

#### GET /api/v1/admin

Get admin API information.

**Headers**:
```
Authorization: Bearer YOUR_TOKEN
```

**Response (JSON)**:
```json
{
  "success": true,
  "user": "administrator",
  "endpoints": [
    "/api/v1/admin/settings",
    "/api/v1/admin/database",
    "/api/v1/admin/logs",
    "/api/v1/admin/backup",
    "/api/v1/admin/healthz"
  ],
  "timestamp": "2024-01-01T12:00:00Z"
}
```

---

### Settings

#### GET /api/v1/admin/settings

Get all server settings.

**Response (JSON)**:
```json
{
  "success": true,
  "data": {
    "server.title": "GitIgnore API",
    "server.port": "8080",
    "log.level": "info"
  },
  "timestamp": "2024-01-01T12:00:00Z"
}
```

---

#### PUT /api/v1/admin/settings

Update server settings.

**Request Body**:
```json
{
  "settings": {
    "server.title": "My GitIgnore API",
    "log.level": "debug"
  }
}
```

**Response (JSON)**:
```json
{
  "success": true,
  "message": "Settings updated",
  "timestamp": "2024-01-01T12:00:00Z"
}
```

---

### Database

#### GET /api/v1/admin/database

Get database status.

**Response (JSON)**:
```json
{
  "success": true,
  "status": "connected",
  "type": "sqlite",
  "path": "/data/gitignore.db",
  "timestamp": "2024-01-01T12:00:00Z"
}
```

---

#### POST /api/v1/admin/database/test

Test database connection.

**Response (JSON)**:
```json
{
  "success": true,
  "message": "Database connection successful",
  "latency_ms": 5,
  "timestamp": "2024-01-01T12:00:00Z"
}
```

---

### Logs

#### GET /api/v1/admin/logs

List available logs.

**Response (JSON)**:
```json
{
  "success": true,
  "data": ["access", "error", "audit"],
  "timestamp": "2024-01-01T12:00:00Z"
}
```

---

#### GET /api/v1/admin/logs/:type

Get log content.

**Parameters**:
- `type` (path): Log type (access, error, audit)

**Query Parameters**:
- `lines` (int, optional): Number of lines to return (default: 100)
- `follow` (bool, optional): Stream logs (default: false)

**Response (JSON)**:
```json
{
  "success": true,
  "type": "access",
  "content": "2024-01-01 12:00:00 GET /api/v1/list 200...",
  "timestamp": "2024-01-01T12:00:00Z"
}
```

---

### Backup

#### GET /api/v1/admin/backup

List all backups.

**Response (JSON)**:
```json
{
  "success": true,
  "data": [
    {
      "id": "backup_20240101_120000",
      "size": "1.2MB",
      "created": "2024-01-01T12:00:00Z"
    }
  ],
  "timestamp": "2024-01-01T12:00:00Z"
}
```

---

#### POST /api/v1/admin/backup

Create a new backup.

**Response (JSON)**:
```json
{
  "success": true,
  "message": "Backup created",
  "backup_id": "backup_20240101_120000",
  "size": "1.2MB",
  "timestamp": "2024-01-01T12:00:00Z"
}
```

---

#### DELETE /api/v1/admin/backup/:id

Delete a backup.

**Parameters**:
- `id` (path): Backup ID

**Response (JSON)**:
```json
{
  "success": true,
  "message": "Backup deleted",
  "timestamp": "2024-01-01T12:00:00Z"
}
```

---

### Health (Admin)

#### GET /api/v1/admin/healthz

Get detailed health status.

**Response (JSON)**:
```json
{
  "status": "healthy",
  "version": "v1.0.0",
  "commit": "abc1234",
  "templates": 500,
  "database": "connected",
  "uptime_seconds": 3600,
  "memory_mb": 25,
  "goroutines": 10,
  "timestamp": "2024-01-01T12:00:00Z"
}
```

---

## Error Responses

All errors follow this format:

```json
{
  "success": false,
  "error": {
    "code": "ERROR_CODE",
    "message": "Human-readable error message",
    "field": "fieldName"
  },
  "timestamp": "2024-01-01T12:00:00Z"
}
```

### Error Codes

| Code | Status | Description |
|------|--------|-------------|
| `INVALID_INPUT` | 400 | Invalid request parameters |
| `UNAUTHORIZED` | 401 | Missing or invalid authentication |
| `FORBIDDEN` | 403 | Insufficient permissions |
| `NOT_FOUND` | 404 | Resource not found |
| `METHOD_NOT_ALLOWED` | 405 | HTTP method not allowed |
| `INTERNAL_ERROR` | 500 | Internal server error |
| `SERVICE_UNAVAILABLE` | 503 | Service temporarily unavailable |

---

## Rate Limiting

No rate limiting is enforced by default. To implement rate limiting, use a reverse proxy like nginx or Caddy.

---

## CORS

CORS is disabled by default for security. Enable in development mode with `--dev` flag.

---

## Examples

### cURL

```bash
# List templates
curl http://localhost:8080/api/v1/list

# Get template as JSON
curl -H "Accept: application/json" \
  http://localhost:8080/api/v1/template/Go

# Combine templates
curl "http://localhost:8080/api/v1/combine?templates=Go,Python,VSCode" \
  > .gitignore

# Search
curl "http://localhost:8080/api/v1/search?q=python&limit=5"

# Admin: Get settings
curl -H "Authorization: Bearer YOUR_TOKEN" \
  http://localhost:8080/api/v1/admin/settings
```

### JavaScript

```javascript
// Fetch template
const response = await fetch('http://localhost:8080/api/v1/template/Go', {
  headers: { 'Accept': 'application/json' }
});
const data = await response.json();
console.log(data.data.content);

// Search
const results = await fetch(
  'http://localhost:8080/api/v1/search?q=python'
).then(r => r.json());
console.log(results.data.results);
```

### Python

```python
import requests

# Get template
response = requests.get('http://localhost:8080/api/v1/template/Go')
print(response.text)

# Combine templates
templates = ['Go', 'Python', 'VSCode']
response = requests.get(
    'http://localhost:8080/api/v1/combine',
    params={'templates': ','.join(templates)}
)
with open('.gitignore', 'w') as f:
    f.write(response.text)

# Admin: Update settings
headers = {'Authorization': 'Bearer YOUR_TOKEN'}
data = {
    'settings': {
        'server.title': 'My GitIgnore API'
    }
}
response = requests.put(
    'http://localhost:8080/api/v1/admin/settings',
    json=data,
    headers=headers
)
print(response.json())
```

---

## GraphQL

GraphQL endpoint: `POST /api/v1/graphql`

### Query Examples

```graphql
# Get all templates
query {
  templates {
    name
    category
  }
}

# Get specific template
query {
  template(name: "Go") {
    name
    content
    category
  }
}

# Search templates
query {
  search(query: "python") {
    name
    category
  }
}

# Get categories
query {
  categories {
    name
    count
  }
}
```

---

## WebSocket

WebSocket endpoint: `ws://localhost:PORT/api/v1/ws`

### Events

- `template.updated`: Template was updated
- `settings.changed`: Server settings changed
- `log.entry`: New log entry (admin only)

---

## Versioning

API versioning is implemented via URL path (`/api/v1`). Breaking changes will increment the version number.

Current version: **v1**

---

**For more examples and interactive documentation, visit `/docs` on your running server.**
