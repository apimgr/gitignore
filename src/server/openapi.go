package server

import (
	"net/http"
	"strings"
)

func mergeOperations(left, right map[string]interface{}) map[string]interface{} {
	merged := make(map[string]interface{}, len(left)+len(right))
	for key, value := range left {
		merged[key] = value
	}
	for key, value := range right {
		merged[key] = value
	}
	return merged
}

// openAPISpec builds the OpenAPI 3.0 document describing the public /api/v1
// surface (AI.md PART 14). The server URL is derived from the request so the
// document is correct regardless of host or scheme.
func (s *Server) openAPISpec(r *http.Request) map[string]interface{} {
	base := s.publicServerURL(r)
	api := s.apiBasePath()

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
	graphQLRequestBody := map[string]interface{}{
		"required": true,
		"content": map[string]interface{}{
			"application/json": map[string]interface{}{
				"schema": map[string]interface{}{
					"type":     "object",
					"required": []string{"query"},
					"properties": map[string]interface{}{
						"query":         map[string]interface{}{"type": "string"},
						"operationName": map[string]interface{}{"type": "string"},
						"variables":     map[string]interface{}{"type": "object", "additionalProperties": true},
					},
				},
			},
		},
	}
	graphQLPost := map[string]interface{}{
		"post": map[string]interface{}{
			"summary":     "Execute a read-only GraphQL query",
			"requestBody": graphQLRequestBody,
			"responses": map[string]interface{}{
				"200": map[string]interface{}{
					"description": "GraphQL query result",
					"content": map[string]interface{}{
						"application/json": map[string]interface{}{"schema": map[string]interface{}{"type": "object"}},
					},
				},
				"400": map[string]interface{}{
					"description": "Invalid GraphQL request body",
					"content": map[string]interface{}{
						"application/json": map[string]interface{}{"schema": map[string]interface{}{"type": "object"}},
					},
				},
				"500": map[string]interface{}{
					"description": "GraphQL schema unavailable",
					"content": map[string]interface{}{
						"application/json": map[string]interface{}{"schema": map[string]interface{}{"type": "object"}},
					},
				},
			},
		},
	}
	preferencesGet := get("Read guest preferences", nil)
	preferencesGet["get"].(map[string]interface{})["operationId"] = "get_api_server_preferences"
	preferencesSyncGet := get("Export guest preferences", nil)
	preferencesSyncGet["get"].(map[string]interface{})["operationId"] = "get_api_server_preferences_sync"
	preferencesImportGet := get("Import guest preferences from a shared link", nil)
	preferencesImportGet["get"].(map[string]interface{})["operationId"] = "get_api_server_preferences_sync_import"
	preferencesImportGet["get"].(map[string]interface{})["x-mcp-disabled"] = true
	preferencesSavePost := map[string]interface{}{"post": map[string]interface{}{
		"summary":     "Save guest preferences",
		"operationId": "post_api_server_preferences",
		"requestBody": map[string]interface{}{"required": true, "content": map[string]interface{}{
			"application/json":                  map[string]interface{}{"schema": map[string]interface{}{"type": "object", "additionalProperties": map[string]interface{}{"type": "string"}}},
			"application/x-www-form-urlencoded": map[string]interface{}{"schema": map[string]interface{}{"type": "object", "additionalProperties": map[string]interface{}{"type": "string"}}},
		}},
		"responses": map[string]interface{}{"200": jsonOK, "400": jsonErr},
	}}
	ownerTokenSecurity := []interface{}{map[string]interface{}{"BearerAuth": []interface{}{}}}
	ownerTokensGet := get("List owner tokens; token values are always masked", nil)
	ownerTokensGetOp := ownerTokensGet["get"].(map[string]interface{})
	ownerTokensGetOp["operationId"] = "get_api_server_preferences_tokens"
	ownerTokensGetOp["x-mcp-disabled"] = true
	ownerTokensGetOp["security"] = ownerTokenSecurity
	ownerTokensPost := map[string]interface{}{"post": map[string]interface{}{
		"operationId":    "post_api_server_preferences_tokens",
		"summary":        "Create a linked owner token",
		"security":       ownerTokenSecurity,
		"x-mcp-disabled": true,
		"requestBody": map[string]interface{}{"required": true, "content": map[string]interface{}{
			"application/json": map[string]interface{}{"schema": map[string]interface{}{"type": "object", "required": []string{"name"}, "properties": map[string]interface{}{
				"name": map[string]interface{}{"type": "string", "minLength": 1, "maxLength": 64}, "expiry": map[string]interface{}{"type": "string", "enum": []string{"never", "7d", "30d", "90d", "1y", "custom"}}, "expires_on": map[string]interface{}{"type": "string", "format": "date"},
			}}},
		}},
		"responses": map[string]interface{}{"201": jsonOK, "400": jsonErr, "401": jsonErr, "403": jsonErr, "409": jsonErr},
	}}
	issuePrimaryToken := map[string]interface{}{"post": map[string]interface{}{
		"operationId":    "post_api_server_preferences_tokens_primary",
		"summary":        "Issue an anonymous owner and primary token",
		"x-mcp-disabled": true,
		"responses":      map[string]interface{}{"201": jsonOK, "401": jsonErr, "409": jsonErr, "429": jsonErr},
	}}
	primaryTokenAction := func(operationID, summary string) map[string]interface{} {
		return map[string]interface{}{"post": map[string]interface{}{"operationId": operationID, "summary": summary, "security": ownerTokenSecurity, "x-mcp-disabled": true, "responses": map[string]interface{}{"200": jsonOK, "401": jsonErr, "403": jsonErr, "404": jsonErr, "429": jsonErr}}}
	}
	linkedTokenPath := []interface{}{map[string]interface{}{"name": "id", "in": "path", "required": true, "schema": map[string]interface{}{"type": "integer", "minimum": 1}}}
	linkedTokenAction := func(method, operationID, summary string) map[string]interface{} {
		operation := map[string]interface{}{"operationId": operationID, "summary": summary, "security": ownerTokenSecurity, "x-mcp-disabled": true, "responses": map[string]interface{}{"200": jsonOK, "401": jsonErr, "403": jsonErr, "404": jsonErr, "429": jsonErr}}
		if method == "patch" {
			operation["requestBody"] = map[string]interface{}{"required": true, "content": map[string]interface{}{"application/json": map[string]interface{}{"schema": map[string]interface{}{"type": "object", "required": []string{"name"}, "properties": map[string]interface{}{"name": map[string]interface{}{"type": "string"}, "expiry": map[string]interface{}{"type": "string"}, "expires_on": map[string]interface{}{"type": "string", "format": "date"}}}}}}
		}
		return map[string]interface{}{method: operation}
	}
	linkedTokenPathItem := func(operations ...map[string]interface{}) map[string]interface{} {
		item := map[string]interface{}{"parameters": linkedTokenPath}
		for _, operation := range operations {
			for method, value := range operation {
				item[method] = value
			}
		}
		return item
	}
	mcpPost := map[string]interface{}{
		"post": map[string]interface{}{
			"operationId": "mcp",
			"summary":     "Call the MCP Streamable HTTP endpoint",
			"requestBody": map[string]interface{}{
				"required": true,
				"content":  map[string]interface{}{"application/json": map[string]interface{}{"schema": map[string]interface{}{"type": "object", "required": []string{"jsonrpc", "method"}}}},
			},
			"responses": map[string]interface{}{"200": jsonOK, "400": jsonErr, "403": jsonErr, "405": jsonErr},
		},
	}
	contactPost := map[string]interface{}{
		"post": map[string]interface{}{
			"summary": "Submit a contact message or security report",
			"requestBody": map[string]interface{}{
				"required": true,
				"content": map[string]interface{}{
					"application/json": map[string]interface{}{"schema": map[string]interface{}{
						"type": "object", "required": []string{"name", "email"},
						"properties": map[string]interface{}{
							"name": map[string]interface{}{"type": "string"}, "email": map[string]interface{}{"type": "string", "format": "email"},
							"subject": map[string]interface{}{"type": "string"}, "message": map[string]interface{}{"type": "string"},
							"captcha": map[string]interface{}{"type": "string"}, "challenge_id": map[string]interface{}{"type": "string"},
							"security_id": map[string]interface{}{"type": "string"}, "component": map[string]interface{}{"type": "string"},
							"endpoint": map[string]interface{}{"type": "string"}, "severity": map[string]interface{}{"type": "string"},
							"summary": map[string]interface{}{"type": "string"}, "reproduction": map[string]interface{}{"type": "string"},
							"impact": map[string]interface{}{"type": "string"}, "suggested_fix": map[string]interface{}{"type": "string"},
							"researcher_pubkey": map[string]interface{}{"type": "string"}, "coordinated_disclosure": map[string]interface{}{"type": "boolean"},
						},
					}},
				},
			},
			"responses": map[string]interface{}{"200": jsonOK, "400": jsonErr, "503": jsonErr},
		},
	}

	paths := map[string]interface{}{
		"/api/autodiscover":                        get("Discover server configuration and capabilities", nil),
		"/api/graphql":                             graphQLPost,
		"/api/healthz":                             get("Server health", nil),
		"/api/healthz.txt":                         get("Server health as text", nil),
		"/api/mcp":                                 mcpPost,
		"/api/list":                                get("List gitignore.io-compatible template names", nil),
		"/api/{list}":                              get("Compose gitignore.io-compatible templates", []interface{}{map[string]interface{}{"name": "list", "in": "path", "required": true, "description": "Comma-separated template names", "schema": map[string]interface{}{"type": "string"}}}),
		"/api/swagger":                             get("Current OpenAPI document", nil),
		"/mcp":                                     mcpPost,
		api + "/":                                  get("API information", nil),
		api + "/server/healthz":                    get("Server health", nil),
		api + "/server/healthz.txt":                get("Server health as text", nil),
		api + "/server/swagger":                    get("OpenAPI document", nil),
		api + "/server/graphql":                    graphQLPost,
		api + "/server/mcp":                        mcpPost,
		api + "/server/security":                   get("Security reporting information", nil),
		api + "/server/security/policy":            get("Security disclosure policy", nil),
		api + "/server/security/thanks":            get("Security acknowledgments", nil),
		api + "/server/dpo":                        get("Data protection officer contact", nil),
		api + "/server/about":                      get("About this service", nil),
		api + "/server/privacy":                    get("Privacy information", nil),
		api + "/server/help":                       get("Help content and pages", nil),
		api + "/server/help/mcp":                   get("MCP setup and tool metadata", nil),
		api + "/server/terms":                      get("Terms of use", nil),
		api + "/server/contact":                    contactPost,
		api + "/server/scheduler":                  get("Scheduler status", nil),
		api + "/server/preferences":                mergeOperations(preferencesGet, preferencesSavePost),
		api + "/server/preferences/tokens":         mergeOperations(ownerTokensGet, ownerTokensPost),
		api + "/server/preferences/tokens/primary": issuePrimaryToken,
		api + "/server/preferences/tokens/primary/reveal": primaryTokenAction("post_api_server_preferences_tokens_primary_reveal", "Reveal the primary owner token"),
		api + "/server/preferences/tokens/primary/rotate": primaryTokenAction("post_api_server_preferences_tokens_primary_rotate", "Rotate the primary owner token"),
		api + "/server/preferences/tokens/{id}/reveal":    linkedTokenPathItem(linkedTokenAction("post", "post_api_server_preferences_tokens_id_reveal", "Reveal a linked owner token")),
		api + "/server/preferences/tokens/{id}/rotate":    linkedTokenPathItem(linkedTokenAction("post", "post_api_server_preferences_tokens_id_rotate", "Rotate a linked owner token")),
		api + "/server/preferences/tokens/{id}":           linkedTokenPathItem(linkedTokenAction("patch", "patch_api_server_preferences_tokens_id", "Update a linked owner token"), linkedTokenAction("delete", "delete_api_server_preferences_tokens_id", "Revoke a linked owner token")),
		api + "/server/preferences/sync":                  preferencesSyncGet,
		api + "/server/preferences/sync/import":           preferencesImportGet,
		api + "/list":                                     get("List all templates", nil),
		api + "/list.txt":                                 get("List all template names as text", nil),
		api + "/categories":                               get("List all categories", nil),
		api + "/categories.txt":                           get("List categories as text", nil),
		api + "/categories/{name}":                        get("List templates in a category", []interface{}{templateName}),
		api + "/categories/{name}.txt":                    get("List category templates as text", []interface{}{templateName}),
		api + "/stats":                                    get("Template statistics", nil),
		api + "/stats.txt":                                get("Template statistics as text", nil),
		api + "/templates/{name}":                         get("Get a template by name", []interface{}{templateName}),
		api + "/templates/{name}.txt":                     get("Get a template as text", []interface{}{templateName}),
		api + "/templates/{name}.json":                    get("Get a template as JSON", []interface{}{templateName}),
		api + "/templates.json":                           get("Export all templates as JSON", nil),
		api + "/templates.tar.gz":                         get("Download all templates as a gzip archive", nil),
		api + "/search": get("Search templates", []interface{}{
			map[string]interface{}{"name": "q", "in": "query", "required": true, "description": "Search query", "schema": map[string]interface{}{"type": "string"}},
		}),
		api + "/search.txt": get("Search templates and return names as text", []interface{}{
			map[string]interface{}{"name": "q", "in": "query", "required": true, "description": "Search query", "schema": map[string]interface{}{"type": "string"}},
		}),
		api + "/combine": get("Combine multiple templates", []interface{}{
			map[string]interface{}{"name": "templates", "in": "query", "required": true, "description": "Comma-separated template names", "schema": map[string]interface{}{"type": "string"}},
		}),
		api + "/combine.txt": get("Combine templates and return text", []interface{}{
			map[string]interface{}{"name": "templates", "in": "query", "required": true, "description": "Comma-separated template names", "schema": map[string]interface{}{"type": "string"}},
		}),
		api + "/cli/sh":              get("Generated POSIX shell helper", nil),
		api + "/cli/ps":              get("Generated PowerShell helper", nil),
		api + "/cli/completion/bash": get("Bash completion script", nil),
		api + "/cli/completion/zsh":  get("Zsh completion script", nil),
		api + "/cli/completion/fish": get("Fish completion script", nil),
	}
	for path, raw := range paths {
		if !strings.HasPrefix(path, api+"/") {
			continue
		}
		operations, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		if operation, ok := operations["get"].(map[string]interface{}); ok {
			operation["operationId"] = mcpOperationID(path)
		}
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
		"paths": paths,
		"components": map[string]interface{}{
			"securitySchemes": map[string]interface{}{"BearerAuth": map[string]interface{}{"type": "http", "scheme": "bearer"}},
			"schemas": map[string]interface{}{
				"APIResponse": okEnvelope,
				"APIError":    errEnvelope,
			},
		},
	}
}
