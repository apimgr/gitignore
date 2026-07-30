package server

import "net/http"

// openAPISpec builds the OpenAPI 3.0 document describing the public /api/v1
// surface (AI.md PART 14). The server URL is derived from the request so the
// document is correct regardless of host or scheme.
func (s *Server) openAPISpec(r *http.Request) map[string]interface{} {
	base := s.detectServerURL(r)
	api := apiBasePath()

	templateName := map[string]interface{}{
		"name":        "name",
		"in":          "path",
		"required":    true,
		"description": "Template name",
		"schema":      map[string]interface{}{"type": "string"},
	}

	okEnvelope := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"ok":   map[string]interface{}{"type": "boolean"},
			"data": map[string]interface{}{},
			"meta": map[string]interface{}{"type": "object"},
		},
	}
	errEnvelope := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"ok":      map[string]interface{}{"type": "boolean"},
			"error":   map[string]interface{}{"type": "string"},
			"message": map[string]interface{}{"type": "string"},
		},
	}

	jsonOK := map[string]interface{}{
		"description": "Success",
		"content": map[string]interface{}{
			"application/json": map[string]interface{}{
				"schema": map[string]interface{}{"$ref": "#/components/schemas/APIResponse"},
			},
		},
	}
	jsonErr := map[string]interface{}{
		"description": "Error",
		"content": map[string]interface{}{
			"application/json": map[string]interface{}{
				"schema": map[string]interface{}{"$ref": "#/components/schemas/APIError"},
			},
		},
	}

	get := func(summary string, params []interface{}) map[string]interface{} {
		op := map[string]interface{}{
			"summary": summary,
			"responses": map[string]interface{}{
				"200": jsonOK,
				"404": jsonErr,
			},
		}
		if len(params) > 0 {
			op["parameters"] = params
		}
		return map[string]interface{}{"get": op}
	}

	return map[string]interface{}{
		"openapi": "3.0.3",
		"info": map[string]interface{}{
			"title":       "GitIgnore API",
			"description": "Comprehensive .gitignore template API.",
			"version":     s.config.Version,
		},
		"servers": []interface{}{
			map[string]interface{}{"url": base},
		},
		"paths": map[string]interface{}{
			api + "/list":              get("List all templates", nil),
			api + "/categories":        get("List all categories", nil),
			api + "/stats":             get("Template statistics", nil),
			api + "/templates/{name}":  get("Get a template by name", []interface{}{templateName}),
			api + "/categories/{name}": get("List templates in a category", []interface{}{templateName}),
			api + "/search": get("Search templates", []interface{}{
				map[string]interface{}{
					"name": "q", "in": "query", "required": true,
					"description": "Search query",
					"schema":      map[string]interface{}{"type": "string"},
				},
			}),
			api + "/combine": get("Combine multiple templates", []interface{}{
				map[string]interface{}{
					"name": "templates", "in": "query", "required": true,
					"description": "Comma-separated template names",
					"schema":      map[string]interface{}{"type": "string"},
				},
			}),
		},
		"components": map[string]interface{}{
			"schemas": map[string]interface{}{
				"APIResponse": okEnvelope,
				"APIError":    errEnvelope,
			},
		},
	}
}

// swaggerUIHTML is a self-contained Swagger UI page bound to the OpenAPI JSON
// endpoint. Assets are self-hosted under /static/vendor/swagger so the strict
// CSP (script-src 'self', no CDN, no inline scripts) is satisfied (AI.md PART
// 11). The spec URL is passed via the data-spec-url attribute and read by the
// external swagger-init.js.
const swaggerUIHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>API Reference — GitIgnore</title>
<link rel="stylesheet" href="/static/vendor/swagger/swagger-ui.css">
</head>
<body>
<div id="swagger-ui" data-spec-url="%s/server/swagger"></div>
<script src="/static/vendor/swagger/swagger-ui-bundle.js"></script>
<script src="/static/vendor/swagger/swagger-init.js"></script>
</body>
</html>`

// graphiQLHTML is a self-contained GraphiQL playground page bound to the
// /graphql endpoint. Assets are self-hosted under /static/vendor/graphiql so
// the strict CSP (script-src 'self', no CDN, no inline scripts) is satisfied
// (AI.md PART 11). The endpoint URL is passed via the data-graphql-url
// attribute and read by the external graphiql-init.js.
const graphiQLHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>GraphiQL — GitIgnore</title>
<link rel="stylesheet" href="/static/vendor/graphiql/graphiql.min.css">
<style>html,body,#graphiql{height:100%;margin:0}</style>
</head>
<body>
<div id="graphiql" data-graphql-url="/api/graphql">Loading…</div>
<script src="/static/vendor/graphiql/react.production.min.js"></script>
<script src="/static/vendor/graphiql/react-dom.production.min.js"></script>
<script src="/static/vendor/graphiql/graphiql.min.js"></script>
<script src="/static/vendor/graphiql/graphiql-init.js"></script>
</body>
</html>`

// graphQLSchema is the SDL description of the API's GraphQL surface.
const graphQLSchema = `type Template {
  name: String!
  fileName: String!
  category: String!
  content: String
  description: String
  tags: [String!]
  size: Int!
}

type Stats {
  totalTemplates: Int!
  categories: Int!
  totalSizeBytes: Int!
}

type Query {
  template(name: String!): Template
  list: [Template!]!
  search(q: String!): [Template!]!
  categories: [String!]!
  category(name: String!): [Template!]!
  combine(templates: [String!]!): String!
  stats: Stats!
}
`
